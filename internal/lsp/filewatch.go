package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo"

	"mayoi/internal/workspace"
)

const watchedFiles = "workspace/didChangeWatchedFiles"

type fileWatches struct {
	client *Client
	roots  []string
	ctx    context.Context
	cancel context.CancelFunc

	mu    sync.Mutex
	rules map[string][]fileWatchRule
	stop  context.CancelFunc
}

type fileWatchRule struct {
	base     string // a relative pattern's base, else the workspace folder
	absolute bool
	glob     workspace.Globs
	kind     int
}

func newFileWatches(c *Client) *fileWatches {
	var roots []string
	seen := map[string]bool{}
	for _, folder := range c.folders {
		root := Path(mappedURI(folder.URI, c.toServer, true))
		if !filepath.IsAbs(root) {
			continue
		}
		root = filepath.Clean(root)
		if !seen[root] {
			roots = append(roots, root)
			seen[root] = true
		}
	}
	if len(roots) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &fileWatches{client: c, roots: roots, ctx: ctx, cancel: cancel, rules: map[string][]fileWatchRule{}}
}

// update takes a server's registration or removal. The protocol spells the
// removal field "unregisterations". Watch setup finishes before the reply.
func (w *fileWatches) update(method string, params json.RawMessage) {
	var request struct {
		Registrations []struct {
			ID              string
			Method          string
			RegisterOptions struct {
				Watchers []struct {
					GlobPattern json.RawMessage
					Kind        *int
				}
			}
		}
		Unregistrations []struct {
			ID     string
			Method string
		} `json:"unregisterations"`
	}
	if json.Unmarshal(params, &request) != nil {
		return
	}
	w.mu.Lock()
	if method == "client/registerCapability" {
		for _, reg := range request.Registrations {
			if reg.Method != watchedFiles {
				continue
			}
			var rules []fileWatchRule
			for _, watch := range reg.RegisterOptions.Watchers {
				if rule := parseFileWatchRule(watch.GlobPattern, watch.Kind); len(rule.glob) > 0 && rule.kind != 0 {
					rules = append(rules, rule)
				}
			}
			delete(w.rules, reg.ID)
			if len(rules) > 0 {
				w.rules[reg.ID] = rules
			}
		}
	} else {
		for _, reg := range request.Unregistrations {
			if reg.Method == watchedFiles {
				delete(w.rules, reg.ID)
			}
		}
	}
	var start context.Context
	switch {
	case len(w.rules) > 0 && w.stop == nil:
		start, w.stop = context.WithCancel(w.ctx)
	case len(w.rules) == 0 && w.stop != nil:
		w.stop()
		w.stop = nil
	}
	w.mu.Unlock()
	if start != nil {
		for _, root := range w.roots {
			w.watch(start, root)
		}
	}
}

func parseFileWatchRule(raw json.RawMessage, kind *int) fileWatchRule {
	rule := fileWatchRule{kind: 7}
	if kind != nil {
		rule.kind = *kind & 7
	}
	var pattern string
	if json.Unmarshal(raw, &pattern) != nil {
		var relative struct {
			BaseURI json.RawMessage
			Pattern string
		}
		if json.Unmarshal(raw, &relative) != nil {
			return fileWatchRule{}
		}
		var uri string
		if json.Unmarshal(relative.BaseURI, &uri) != nil {
			var folder WorkspaceFolder
			json.Unmarshal(relative.BaseURI, &folder)
			uri = folder.URI
		}
		rule.base = Path(uri)
		if !filepath.IsAbs(rule.base) {
			return fileWatchRule{}
		}
		pattern = relative.Pattern
	} else {
		rule.absolute = filepath.IsAbs(filepath.FromSlash(pattern))
	}
	if pattern != "" {
		// The leading slash anchors the file pattern at its base. Unlike
		// Search, a bare glob does not implicitly match nested directories.
		rule.glob = workspace.ParseGlobs("/" + strings.TrimPrefix(pattern, "/"))
	}
	return rule
}

func (r fileWatchRule) matches(rel, name string, kind int) bool {
	if r.kind&(1<<(kind-1)) == 0 {
		return false
	}
	switch {
	case r.absolute:
		return r.glob.MatchPath(strings.TrimPrefix(filepath.ToSlash(name), "/"))
	case r.base != "":
		relative, err := filepath.Rel(r.base, name)
		return err == nil && filepath.IsLocal(relative) && r.glob.MatchPath(filepath.ToSlash(relative))
	default:
		return r.glob.MatchPath(rel)
	}
}

func (w *fileWatches) watch(ctx context.Context, root string) {
	watcher, err := mygo.WatchFiles(ctx, root, mygo.FileWatchOptions{
		Recursive: true,
		Ignore:    func(rel string, dir bool) bool { return dir && workspace.Skipped(path.Base(rel)) },
	})
	if err != nil {
		w.logError(ctx, root, err)
		return
	}
	go func() {
		defer watcher.Close()
		for {
			events, err := watcher.Next(ctx)
			if err != nil {
				w.logError(ctx, root, err)
				return
			}
			changes := w.changes(root, events)
			if len(changes) != 0 && ctx.Err() == nil {
				if w.client.send(ctx, watchedFiles, map[string]any{"changes": changes}, nil) != nil {
					return
				}
			}
		}
	}()
}

type watchedFileChange struct {
	URI  string `json:"uri"`
	Type int    `json:"type"`
}

func (w *fileWatches) changes(root string, events []mygo.FileEvent) []watchedFileChange {
	w.mu.Lock()
	defer w.mu.Unlock()
	var changes []watchedFileChange
	seen := map[watchedFileChange]bool{}
	add := func(rel string, kind int) {
		name := filepath.Join(root, filepath.FromSlash(rel))
		for _, rules := range w.rules {
			for _, rule := range rules {
				if rule.matches(rel, name, kind) {
					change := watchedFileChange{URI(name), kind}
					if !seen[change] {
						changes = append(changes, change)
						seen[change] = true
					}
					return
				}
			}
		}
	}
	for _, event := range events {
		switch event.Op {
		case mygo.FileCreate:
			add(event.Path, 1)
		case mygo.FileWrite:
			add(event.Path, 2)
		case mygo.FileRemove:
			add(event.Path, 3)
		case mygo.FileRename:
			add(event.OldPath, 3)
			add(event.Path, 1)
		}
	}
	return changes
}

func (w *fileWatches) logError(ctx context.Context, root string, err error) {
	if ctx.Err() == nil && !errors.Is(err, io.EOF) && w.client.h.Log != nil {
		w.client.h.Log("warn", "file watcher: "+root+": "+err.Error())
	}
}
