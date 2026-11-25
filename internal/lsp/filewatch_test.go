package lsp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func TestFileWatcherChanges(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "real project")
	alias := URI(filepath.Join(base, "workspace project"))
	c := &Client{folders: []WorkspaceFolder{{URI: alias}},
		toServer: []PathMapping{{ClientURI: alias, ServerURI: URI(root)}}}
	watches := newFileWatches(c)
	defer watches.close()
	if !slices.Equal(watches.roots, []string{root}) {
		t.Fatalf("watch roots = %v", watches.roots)
	}
	src := filepath.Join(root, "src")
	relative, _ := json.Marshal(map[string]any{
		"baseUri": WorkspaceFolder{URI: URI(src)}, "pattern": "**/*.{java,properties}",
	})
	absolute, _ := json.Marshal(filepath.ToSlash(filepath.Join(root, "project", "pom.xml")))
	createOrDelete := 5
	watches.rules["sources"] = []fileWatchRule{parseFileWatchRule(relative, &createOrDelete)}
	watches.rules["build"] = []fileWatchRule{
		parseFileWatchRule(absolute, nil),
		parseFileWatchRule(json.RawMessage(`"pom.xml"`), nil),
	}
	events := []mygo.FileEvent{
		{Op: mygo.FileCreate, Path: "src/nested/New.java"},
		{Op: mygo.FileCreate, Path: "src/nested/New.java"},
		{Op: mygo.FileWrite, Path: "src/nested/New.java"},
		{Op: mygo.FileRename, OldPath: "src/old.properties", Path: "src/new.properties"},
		{Op: mygo.FileCreate, Path: "src/New.java/Other.txt"},
		{Op: mygo.FileCreate, Path: "src-other/New.java"},
		{Op: mygo.FileWrite, Path: "other/pom.xml"},
		{Op: mygo.FileWrite, Path: "project/pom.xml"},
	}
	want := []watchedFileChange{
		{URI(filepath.Join(src, "nested", "New.java")), 1},
		{URI(filepath.Join(src, "old.properties")), 3},
		{URI(filepath.Join(src, "new.properties")), 1},
		{URI(filepath.Join(root, "project", "pom.xml")), 2},
	}
	if got := watches.changes(root, events); !slices.Equal(got, want) {
		t.Fatalf("changes = %v, want %v", got, want)
	}
}

// Two servers of a folder share one watch, reached by a symlink as well,
// which ends with the last of them; each hears of changes by its folder.
func TestFileWatchShared(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	root := filepath.Join(base, "project")
	os.MkdirAll(filepath.Join(root, "src"), 0o755)
	link := filepath.Join(base, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skip(err)
	}
	register, _ := json.Marshal(map[string]any{"registrations": []any{map[string]any{
		"id": "w", "method": watchedFiles,
		"registerOptions": map[string]any{"watchers": []any{map[string]any{"globPattern": "**/*.go"}}},
	}}})
	start := func(folder string) (*fileWatches, chan []watchedFileChange) {
		w := newFileWatches(&Client{folders: []WorkspaceFolder{{URI: URI(folder)}}})
		got := make(chan []watchedFileChange, 64)
		w.notify = func(changes []watchedFileChange) error { got <- changes; return nil }
		w.update("client/registerCapability", register)
		return w, got
	}
	a, gotA := start(root)
	b, gotB := start(link)
	defer a.close()
	defer b.close()
	watches := func() int { rootWatches.Lock(); defer rootWatches.Unlock(); return len(rootWatches.m) }
	if n := watches(); n != 1 {
		t.Fatalf("%d watches, want 1", n)
	}
	// expect writes the file, again while the watch may still be looking at
	// the tree, until got hears of it by folder.
	expect := func(got chan []watchedFileChange, name, folder string) {
		t.Helper()
		want := URI(filepath.Join(folder, "src", name))
		deadline := time.After(10 * time.Second)
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		for i := 0; ; i++ {
			os.WriteFile(filepath.Join(root, "src", name), []byte{byte(i)}, 0o644)
			select {
			case changes := <-got:
				for _, c := range changes {
					if c.URI == want {
						return
					}
				}
			case <-tick.C:
			case <-deadline:
				t.Fatalf("no change of %s", want)
			}
		}
	}
	expect(gotA, "one.go", root)
	expect(gotB, "one.go", link)
	a.close()
	if n := watches(); n != 1 {
		t.Fatalf("%d watches after the first closed, want 1", n)
	}
	expect(gotB, "two.go", link)
	b.close()
	if n := watches(); n != 0 {
		t.Fatalf("%d watches after both closed, want 0", n)
	}
}
