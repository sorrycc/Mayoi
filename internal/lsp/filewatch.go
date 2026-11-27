package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
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
	// notify sends the server its changes.
	notify func(changes []watchedFileChange) error

	mu    sync.Mutex
	rules map[string][]fileWatchRule
	// unwatch ends the subscriptions to the watches of roots, nil when
	// none run.
	unwatch []func()
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
	w := &fileWatches{client: c, roots: roots, ctx: ctx, cancel: cancel, rules: map[string][]fileWatchRule{}}
	w.notify = func(changes []watchedFileChange) error {
		return c.send(ctx, watchedFiles, map[string]any{"changes": changes}, nil)
	}
	return w
}

// close ends the watches, as the server stops.
func (w *fileWatches) close() {
	w.mu.Lock()
	w.cancel()
	unwatch := w.unwatch
	w.unwatch = nil
	w.mu.Unlock()
	for _, f := range unwatch {
		f()
	}
}

// update takes a server's registration or removal. The protocol spells the
// removal field "unregisterations". The watches start in the background,
// so that the reply, and what the server says after it, don't wait for them
// to look at the tree.
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
	var stop []func()
	switch {
	case len(w.rules) > 0 && w.unwatch == nil && w.ctx.Err() == nil:
		for _, root := range w.roots {
			w.unwatch = append(w.unwatch, watchRoot(root, w))
		}
	case len(w.rules) == 0 && w.unwatch != nil:
		stop, w.unwatch = w.unwatch, nil
	}
	w.mu.Unlock()
	for _, f := range stop {
		f()
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

// rootWatch is the watch of a root, which every server of a folder there
// shares: two servers of a folder cost one snapshot of its tree.
type rootWatch struct {
	key    string // the root, its symlinks resolved
	cancel context.CancelFunc
	subs   map[*fileWatches]string // with the root each knows it by
}

// rootWatches are the watches that run, by key; the lock guards their subs.
var rootWatches = struct {
	sync.Mutex
	m map[string]*rootWatch
}{m: map[string]*rootWatch{}}

// watchRoot subscribes w to the watch of root, starting it for the first,
// and returns what ends the subscription: the last one stops the watch.
func watchRoot(root string, w *fileWatches) func() {
	key := root
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		key = resolved
	}
	rootWatches.Lock()
	defer rootWatches.Unlock()
	rw := rootWatches.m[key]
	if rw == nil {
		ctx, cancel := context.WithCancel(context.Background())
		rw = &rootWatch{key: key, cancel: cancel, subs: map[*fileWatches]string{}}
		rootWatches.m[key] = rw
		go rw.run(ctx)
	}
	rw.subs[w] = root
	return func() {
		rootWatches.Lock()
		defer rootWatches.Unlock()
		if _, ok := rw.subs[w]; !ok {
			return
		}
		delete(rw.subs, w)
		if len(rw.subs) == 0 {
			rw.cancel()
			if rootWatches.m[key] == rw {
				delete(rootWatches.m, key)
			}
		}
	}
}

// run watches the root and tells each subscriber what changed.
func (rw *rootWatch) run(ctx context.Context) {
	watcher, err := mygo.WatchFiles(ctx, rw.key, mygo.FileWatchOptions{
		Recursive: true,
		Ignore:    func(rel string, dir bool) bool { return dir && workspace.Skipped(path.Base(rel)) },
	})
	if err == nil {
		defer watcher.Close()
		for {
			var events []mygo.FileEvent
			if events, err = watcher.Next(ctx); err != nil {
				break
			}
			for w, root := range rw.subscribers() {
				if changes := w.changes(root, events); len(changes) != 0 && w.ctx.Err() == nil {
					w.notify(changes)
				}
			}
		}
	}
	rootWatches.Lock()
	if rootWatches.m[rw.key] == rw {
		delete(rootWatches.m, rw.key)
	}
	subs := maps.Clone(rw.subs)
	rootWatches.Unlock()
	if ctx.Err() == nil {
		for w, root := range subs {
			w.logError(root, err)
		}
	}
}

func (rw *rootWatch) subscribers() map[*fileWatches]string {
	rootWatches.Lock()
	defer rootWatches.Unlock()
	return maps.Clone(rw.subs)
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

func (w *fileWatches) logError(root string, err error) {
	if w.ctx.Err() == nil && !errors.Is(err, io.EOF) && w.client.h.Log != nil {
		w.client.h.Log("warn", "file watcher: "+root+": "+err.Error())
	}
}
