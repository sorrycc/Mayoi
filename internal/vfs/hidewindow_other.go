//go:build !windows

package vfs

import "os/exec"

// hideWindow does nothing: only Windows gives a program started without a
// console one of its own.
func hideWindow(*exec.Cmd) {}
