package remote

import (
	"os"
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW, the same flag vfs uses so a console
// program started from this window does not open a console of its own.
const createNoWindow = 0x08000000

func hideConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}

func consoleHidden(cmd *exec.Cmd) bool {
	return cmd.SysProcAttr != nil && cmd.SysProcAttr.HideWindow && cmd.SysProcAttr.CreationFlags&createNoWindow != 0
}

// exitWhenParentExits ends this bridge client when the editor process is
// gone. Waiting on the parent is what Windows can see: the recorded parent
// id does not change after that process exits.
func exitWhenParentExits() {
	ppid := os.Getppid()
	if ppid <= 0 {
		return
	}
	h, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(ppid))
	if err != nil {
		return
	}
	defer syscall.CloseHandle(h)
	syscall.WaitForSingleObject(h, syscall.INFINITE)
	os.Exit(1)
}
