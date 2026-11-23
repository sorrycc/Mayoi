//go:build !windows

package vfs

import "os/exec"

// HideWindow does nothing: only Windows gives a program started without a
// console one of its own.
func HideWindow(*exec.Cmd) {}
