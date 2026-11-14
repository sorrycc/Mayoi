package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Copying a directory junction, which Windows makes with mklink /J,
// copies the folder it points to with all it has.
func TestCopyIntoCopiesAJunctionAsAFolder(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "a.md"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(dir, "junction")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, real).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v: %s", err, out)
	}
	dst := filepath.Join(dir, "dst")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	to, err := copyInto(junction, dst)
	if err != nil {
		t.Fatal(err)
	}
	if to != filepath.Join(dst, "junction") {
		t.Errorf("the junction is copied to %s", to)
	}
	got, err := os.ReadFile(filepath.Join(to, "a.md"))
	if err != nil || string(got) != "a" {
		t.Errorf("the copy of the junction has not the folder's files: %q, %v", got, err)
	}
}
