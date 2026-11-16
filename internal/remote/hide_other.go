//go:build !windows

package remote

import (
	"os"
	"os/exec"
	"time"
)

func hideConsole(*exec.Cmd) {}

func consoleHidden(*exec.Cmd) bool { return true }

// exitWhenParentExits ends this bridge client once it has been reparented.
func exitWhenParentExits() {
	ppid := os.Getppid()
	if ppid <= 1 {
		return
	}
	for {
		time.Sleep(50 * time.Millisecond)
		if os.Getppid() != ppid {
			os.Exit(1)
		}
	}
}
