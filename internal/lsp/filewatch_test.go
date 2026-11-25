package lsp

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo"
)

func TestFileWatcherChanges(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "real project")
	alias := URI(filepath.Join(base, "workspace project"))
	c := &Client{folders: []WorkspaceFolder{{URI: alias}},
		toServer: []PathMapping{{ClientURI: alias, ServerURI: URI(root)}}}
	watches := newFileWatches(c)
	defer watches.cancel()
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
