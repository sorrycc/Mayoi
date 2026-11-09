package plugin

import (
	_ "embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// TypesSource is mayoi.d.ts, the types of the mayoi module.
//
//go:embed mayoi.d.ts
var TypesSource string

// AgentsSource is the AGENTS.md mayoi plugin new writes, for agents that
// write plugins.
//
//go:embed agents.md
var AgentsSource string

// watchDirs watches the plugins and scripts folders, and the folders of
// the plugins in them, symlinks followed, and loads the plugins again
// once changes rest for delay.
func watchDirs(r *Registry, delay time.Duration) (func(), error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	watched := map[string]bool{}
	add := func(dir string) {
		mu.Lock()
		defer mu.Unlock()
		if dir == "" || watched[dir] {
			return
		}
		if w.Add(dir) == nil {
			watched[dir] = true
		}
	}
	// addTree watches a plugin's folders, but those of its packages.
	addTree := func(dir string) {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			dir = real
		}
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if d.Name() == "node_modules" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			add(p)
			return nil
		})
	}
	rewatch := func() {
		for _, dir := range []string{r.Dirs.Plugins, r.Dirs.Scripts} {
			if dir == "" {
				continue
			}
			os.MkdirAll(dir, 0o755)
			add(dir)
		}
		if r.Dirs.Plugins != "" {
			entries, _ := os.ReadDir(r.Dirs.Plugins)
			for _, e := range entries {
				addTree(filepath.Join(r.Dirs.Plugins, e.Name()))
			}
		}
	}
	rewatch()
	done := make(chan struct{})
	go func() {
		var timer *time.Timer
		fire := make(chan struct{}, 1)
		for {
			select {
			case <-done:
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if b := filepath.Base(ev.Name); b == RecordsFile || b == SyncedFile || strings.HasPrefix(b, ".install-") || strings.HasSuffix(b, ".old") {
					continue
				}
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(delay, func() {
					select {
					case fire <- struct{}{}:
					default:
					}
				})
			case <-fire:
				rewatch()
				r.Load()
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return func() {
		close(done)
		w.Close()
	}, nil
}
