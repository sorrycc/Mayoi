package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mayoi/internal/cli"

	"github.com/egoist/mygo"
)

// commandLine reports whether the mayoi script started Mayoi, as the
// command.
func commandLine() bool { return os.Getenv(cliEnv) != "" }

// startApp starts the app the command is in, opening paths, with open, as
// the Finder would; a development build, in no app, runs itself again.
func startApp(paths []string) error {
	app := appBundle()
	if app == "" {
		return startDetached(paths)
	}
	if out, err := exec.Command("open", append([]string{"-a", app}, paths...)...).CombinedOutput(); err != nil {
		return fmt.Errorf("open: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// refreshCLI brings the mayoi command an older Mayoi installed up to
// date, for the options it has now.
func refreshCLI() {
	app := appBundle()
	if app == "" || !mygo.App.IsPackaged() {
		return
	}
	home, _ := os.UserHomeDir()
	go cli.Refresh([]string{filepath.Join(home, ".local", "bin"), "/usr/local/bin"}, app)
}
