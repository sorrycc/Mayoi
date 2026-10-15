package vfs

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW, which the syscall package does not
// name.
const createNoWindow = 0x08000000

// hideWindow keeps Windows from giving cmd a console window of its own.
// Mayoi is a window that has no console, and Windows answers every
// console program a console program starts, git above all, with a new
// console window on the desktop, so that a window opens and closes
// around each command the editor runs. HideWindow covers the window a
// program shows itself, CREATE_NO_WINDOW the console the system would
// otherwise give it. What the program writes is untouched: its standard
// handles are the ones the caller set, as they are on the systems that
// have no console window to hide.
func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
