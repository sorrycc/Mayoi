//go:build darwin || linux

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// startDetached starts Mayoi again with args, and env added to its
// environment, in a session of its own.
func startDetached(args []string, env ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
