//go:build unix

package main

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// keepStderr points stderr at stderr.log in dir, when it is not a
// terminal's: started from the Finder or the Dock, Mayoi's stderr goes
// nowhere, and with it why the system aborted it, as an Objective-C
// exception's reason. keepCrashes adds the end of it to the log of a crash.
func keepStderr(dir string) {
	if _, err := unix.IoctlGetWinsize(int(os.Stderr.Fd()), unix.TIOCGWINSZ); err == nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, crashStderrFile), os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	unix.Dup2(int(f.Fd()), int(os.Stderr.Fd()))
}
