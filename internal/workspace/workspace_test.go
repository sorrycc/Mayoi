package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"mayoi/internal/remote"
	"mayoi/internal/vfs"
)

func mkfiles(t *testing.T, root string, files ...string) {
	for _, f := range files {
		p := filepath.Join(root, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// post runs the tree's functions as a window's main thread would.
type mainThread chan func()

func (m mainThread) post(fn func()) { m <- fn }

func (m mainThread) wait(t *testing.T, until func() bool) {
	deadline := time.After(5 * time.Second)
	for !until() {
		select {
		case fn := <-m:
			fn()
		case <-deadline:
			t.Fatal("timed out")
		}
	}
}

func TestTreeLoadsSortsAndFollowsTheDisk(t *testing.T) {
	root := t.TempDir()
	mkfiles(t, root, "b.go", "A.md", "src/main.go", ".git/HEAD", "zeta/x")
	m := make(mainThread, 64)
	tree := NewTree(root, m.post)
	defer tree.Close()

	if kids := tree.Children(root); kids == nil || len(kids) != 0 {
		t.Fatalf("unread root: %v", kids)
	}
	tree.Load(root)
	m.wait(t, func() bool { return tree.Loaded(root) })
	var names []string
	for _, k := range tree.Children(root) {
		names = append(names, filepath.Base(k))
	}
	if want := []string{"src", "zeta", "A.md", "b.go"}; !slices.Equal(names, want) {
		t.Errorf("entries %v, want %v", names, want)
	}
	if tree.Children(filepath.Join(root, "b.go")) != nil {
		t.Error("a file has children")
	}

	mkfiles(t, root, "new.txt")
	m.wait(t, func() bool { return len(tree.Children(root)) == 5 })
	var changed []string
	tree.OnChange = func(c, r []string) { changed = append(changed, c...) }
	os.WriteFile(filepath.Join(root, "b.go"), []byte("changed"), 0o644)
	m.wait(t, func() bool { return slices.Contains(changed, filepath.Join(root, "b.go")) })
}

func TestTreeKnowsLinks(t *testing.T) {
	root := t.TempDir()
	mkfiles(t, root, "real/a.go")
	if err := os.Symlink("real", filepath.Join(root, "dir")); err != nil {
		t.Skip(err)
	}
	os.Symlink(filepath.Join("real", "a.go"), filepath.Join(root, "file"))
	m := make(mainThread, 64)
	tree := NewTree(root, m.post)
	defer tree.Close()
	tree.Load(root)
	m.wait(t, func() bool { return tree.Loaded(root) })
	dir, file, real := filepath.Join(root, "dir"), filepath.Join(root, "file"), filepath.Join(root, "real")
	if !tree.IsLink(dir) || !tree.IsDir(dir) {
		t.Errorf("dir: link %v, folder %v", tree.IsLink(dir), tree.IsDir(dir))
	}
	if !tree.IsLink(file) || tree.IsDir(file) {
		t.Errorf("file: link %v, folder %v", tree.IsLink(file), tree.IsDir(file))
	}
	if tree.IsLink(real) {
		t.Error("a folder is a link")
	}
}

func TestTookNoticesALinkReplacingAFile(t *testing.T) {
	root := remote.Path("box", "/home/me/app")
	a := remote.Join(root, "a")
	tree := &Tree{
		Root:     root,
		remote:   true,
		children: map[string][]string{root: {a}},
		dirs:     map[string]bool{root: true},
		links:    map[string]bool{},
		closed:   true,
	}
	tree.took(map[string][]vfs.Entry{root: {{Name: "a", Link: true}}}, nil, nil, nil)
	if !tree.IsLink(a) {
		t.Fatal("a is not a link")
	}
}

func TestIndexSkipsAndFuzzyRanks(t *testing.T) {
	root := t.TempDir()
	mkfiles(t, root, "src/main.go", "src/domain/manifest.json", "node_modules/m/index.js", "docs/README.md", "cmd/mayoi/main_test.go")
	files := Index(root)
	slices.Sort(files)
	if want := []string{"cmd/mayoi/main_test.go", "docs/README.md", "src/domain/manifest.json", "src/main.go"}; !slices.Equal(files, want) {
		t.Fatalf("index %v", files)
	}
	got := Fuzzy(files, "main", 10)
	if len(got) < 2 || files[got[0].Index] != "src/main.go" {
		var order []string
		for _, m := range got {
			order = append(order, files[m.Index])
		}
		t.Errorf("order %v", order)
	}
	if got := Fuzzy(files, "rdme", 10); len(got) == 0 || files[got[0].Index] != "docs/README.md" {
		t.Errorf("rdme: %v", got)
	}
	if got := Fuzzy(files, "zzz", 10); len(got) != 0 {
		t.Errorf("zzz: %v", got)
	}
}

func TestIndexLeavesOutWhatGitIgnores(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	mkfiles(t, root, ".gitignore", "main.go", "build/app.bin", "src/a.go", "gone.go")
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte("build/\n"), 0o644)
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "x")
	os.Remove(filepath.Join(root, "gone.go"))
	mkfiles(t, root, "new.go")
	if ig := Ignored(root); !ig[filepath.Join(root, "build")] || len(ig) != 1 {
		t.Errorf("Ignored = %v", ig)
	}
	got := Index(root)
	slices.Sort(got)
	want := []string{".gitignore", "main.go", "new.go", "src/a.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("Index = %v, want %v", got, want)
	}
}

func TestSortEntriesKeepsRemoteSlashPaths(t *testing.T) {
	dir := remote.Path("box", "/home/me/app")
	kids, dirs := SortEntries(dir, []vfs.Entry{{Name: "src", Dir: true}, {Name: "a.go"}})
	wantFile := remote.Join(dir, "a.go")
	wantDir := remote.Join(dir, "src")
	if !slices.Contains(kids, wantFile) || !slices.Contains(kids, wantDir) {
		t.Fatalf("kids %q", kids)
	}
	if !dirs[wantDir] || dirs[wantFile] {
		t.Fatalf("dirs %v", dirs)
	}
	for _, kid := range kids {
		if strings.Contains(kid, `\`) {
			t.Errorf("backslash path %q", kid)
		}
	}
}

func TestRemoteEventNamesTheSlashFolder(t *testing.T) {
	root := remote.Path("box", "/home/me/app")
	tree := &Tree{
		Root:     root,
		remote:   true,
		children: map[string][]string{root: nil},
		dirs:     map[string]bool{root: true},
		closed:   true,
	}
	tree.event(remote.Join(root, "a.go"), true, false, false)
	tree.mu.Lock()
	_, ok := tree.pending[root]
	if tree.timer != nil {
		tree.timer.Stop()
	}
	tree.mu.Unlock()
	if !ok {
		t.Fatalf("pending %#v, want %q", tree.pending, root)
	}
}
