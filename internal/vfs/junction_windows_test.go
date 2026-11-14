package vfs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A directory junction, which Windows makes with mklink /J as it does a
// link, is listed as the folder it points to.
func TestAJunctionIsListedAsAFolder(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(dir, "junction"), filepath.Join(dir, "real")).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v: %s", err, out)
	}
	entries, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name == "junction" {
			if !e.Dir {
				t.Errorf("the junction to a folder is listed as a file")
			}
			return
		}
	}
	t.Errorf("the junction is not listed: %+v", entries)
}
