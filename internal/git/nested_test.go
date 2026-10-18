package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A folder of projects, each its own work tree, as in #17.
func TestNested(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"a",                // a work tree
		"group/b",          // two deep
		"a/inner",          // inside a: not looked for
		"group/x/c",        // three deep: past the depth
		".hidden/d",        // hidden
		"node_modules/e",   // dependencies
		"plain/not-a-repo", // no .git
	} {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if rel == "plain/not-a-repo" {
			continue
		}
		if out, err := exec.Command("git", "-C", p, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", rel, err, out)
		}
	}
	// A .git that is not one is passed over.
	os.MkdirAll(filepath.Join(dir, "broken", ".git"), 0o755)
	repos, err := Nested(dir, NestedDepth)
	if err != nil {
		t.Fatal(err)
	}
	var roots []string
	for _, r := range repos {
		rel, _ := filepath.Rel(dir, r.Root)
		roots = append(roots, filepath.ToSlash(rel))
	}
	if len(roots) != 2 || roots[0] != "a" || roots[1] != "group/b" {
		t.Errorf("roots %q", roots)
	}
}
