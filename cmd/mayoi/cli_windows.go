package main

import (
	"os"
	"os/exec"
	"path/filepath"

	"mayoi/internal/cli"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

// commandLine reports whether the mayoi batch file started Mayoi, as the
// command.
func commandLine() bool { return os.Getenv(cliEnv) != "" }

// startApp starts Mayoi apart from the command, opening paths. A Windows
// GUI process has a window of its own and does not hold the console open.
func startApp(paths []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, paths...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// attachConsole connects the GUI-subsystem executable to its parent's
// console. Redirected handles stay as they are; only missing ones open the
// console directly. Attaching is best effort when there is no console.
func attachConsole() {
	const attachParentProcess = 0xffffffff
	windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole").Call(attachParentProcess)
	os.Stdout = consoleFile(windows.STD_OUTPUT_HANDLE, os.Stdout, "/dev/stdout")
	os.Stderr = consoleFile(windows.STD_ERROR_HANDLE, os.Stderr, "/dev/stderr")
}

// consoleFile keeps a valid current or redirected handle, and opens the
// attached console only when the standard handle is absent or invalid.
func consoleFile(id uint32, current *os.File, name string) *os.File {
	if handle, err := windows.GetStdHandle(id); err == nil && handle != 0 {
		if current != nil && current.Fd() == uintptr(handle) {
			return current
		}
		return os.NewFile(uintptr(handle), name)
	}
	if file, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		return file
	}
	return current
}

// refreshCLI brings the mayoi command an older Mayoi installed up to
// date, for the options it has now.
func refreshCLI() {
	app := appLocation()
	if app == "" || !mygo.App.IsPackaged() {
		return
	}
	home, _ := os.UserHomeDir()
	go func() { cli.Refresh(cli.InstalledDirs(home, filepath.SplitList(os.Getenv("PATH"))), app) }()
}
