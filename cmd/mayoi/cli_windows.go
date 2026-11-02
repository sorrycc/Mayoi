package main

import (
	"os"
	"os/exec"
	"path/filepath"

	"mayoi/internal/cli"

	"github.com/egoist/mygo"
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
