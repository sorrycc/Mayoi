//go:build unix

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestKeepStderr points the stderr of a process started without a
// terminal, as from the Finder, at stderr.log: in a process of its own,
// as keepStderr changes the process's stderr.
func TestKeepStderr(t *testing.T) {
	if dir := os.Getenv("MAYOI_TEST_KEEP_STDERR"); dir != "" {
		keepStderr(dir)
		fmt.Fprintln(os.Stderr, "*** Terminating app")
		os.Exit(0)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, crashStderrFile), []byte("of the last run\n"), 0o644)
	cmd := exec.Command(os.Args[0], "-test.run=^TestKeepStderr$")
	cmd.Env = append(os.Environ(), "MAYOI_TEST_KEEP_STDERR="+dir)
	if err := cmd.Run(); err != nil { // stderr: /dev/null
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, crashStderrFile)); err != nil || string(b) != "*** Terminating app\n" {
		t.Errorf("stderr.log = %q, %v", b, err)
	}
}
