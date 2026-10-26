// Package cli is the mayoi shell command, which opens folders and files
// in the app and runs its commands: it installs and uninstalls the
// command, reads its arguments, and carries them to the running app over
// a socket.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// bundleID is the app's identifier, as mygo.json gives it.
const bundleID = "dev.mayoi.editor"

// scriptMark is a line of the mayoi command, by which uninstalling knows it.
const scriptMark = "# Installed by Mayoi: File > Install 'mayoi' Command."

// script is the mayoi command. It runs the app's own executable, which,
// told so by MAYOI_CLI, asks the running app over its socket, or starts
// it: the app at app, where it was installed from, else the one the
// system knows by its identifier, so it keeps working when the app moves.
func script(app string) string {
	return `#!/bin/sh
# mayoi [options] [folder or file]... opens them in Mayoi: mayoi --help says more.
` + scriptMark + `
app=` + shellQuote(app) + `
if [ ! -x "$app/Contents/MacOS/Mayoi" ]; then
	app=$(mdfind "kMDItemCFBundleIdentifier == '` + bundleID + `'" 2>/dev/null | head -n 1)
fi
if [ ! -x "$app/Contents/MacOS/Mayoi" ]; then
	echo "mayoi: Mayoi.app is not found" >&2
	exit 1
fi
MAYOI_CLI=1 exec "$app/Contents/MacOS/Mayoi" "$@"
`
}

// Dirs are where the command goes: ~/.local/bin when the shell looks
// there, which needs no password, else /usr/local/bin, which is always on
// the path.
func Dirs(home string, path []string) []string {
	local := filepath.Join(home, ".local", "bin")
	if slices.Contains(path, local) {
		return []string{local, "/usr/local/bin"}
	}
	return []string{"/usr/local/bin"}
}

// ErrCanceled is the user declining to give the password.
var ErrCanceled = errors.New("canceled")

// Install writes the mayoi command of the app at app into the first
// of dirs that takes it, as an administrator for the last, and returns
// where it went.
func Install(dirs []string, app string) (string, error) {
	var err error
	for i, dir := range dirs {
		p := filepath.Join(dir, "mayoi")
		if data, rerr := os.ReadFile(p); rerr == nil && !strings.Contains(string(data), scriptMark) {
			return "", fmt.Errorf("%s is another program's", p)
		}
		if err = write(dir, app); err == nil {
			return p, nil
		}
		if i == len(dirs)-1 && errors.Is(err, os.ErrPermission) {
			if err = writeAsAdmin(dir, app); err == nil {
				return p, nil
			}
		}
	}
	return "", err
}

func write(dir, app string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	p := filepath.Join(dir, "mayoi")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(script(app)), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func writeAsAdmin(dir, app string) error {
	f, err := os.CreateTemp("", "mayoi-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(script(app))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	p := filepath.Join(dir, "mayoi")
	return runAsAdmin(fmt.Sprintf("mkdir -p %s && cp %s %s && chmod 755 %s",
		shellQuote(dir), shellQuote(f.Name()), shellQuote(p), shellQuote(p)))
}

// Refresh writes the command of the app at app over the copies of
// Mayoi's in dirs that an older Mayoi wrote, where no password is needed.
func Refresh(dirs []string, app string) {
	for _, dir := range dirs {
		p := filepath.Join(dir, "mayoi")
		data, err := os.ReadFile(p)
		if err != nil || !strings.Contains(string(data), scriptMark) || string(data) == script(app) {
			continue
		}
		write(dir, app)
	}
}

// Uninstall removes the mayoi command from dirs, where it is Mayoi's,
// and returns where it was.
func Uninstall(dirs []string) ([]string, error) {
	var removed []string
	for _, dir := range dirs {
		p := filepath.Join(dir, "mayoi")
		data, err := os.ReadFile(p)
		if err != nil || !strings.Contains(string(data), scriptMark) {
			continue
		}
		err = os.Remove(p)
		if errors.Is(err, os.ErrPermission) {
			err = runAsAdmin("rm -f " + shellQuote(p))
		}
		if err != nil {
			return removed, err
		}
		removed = append(removed, p)
	}
	return removed, nil
}

// runAsAdmin runs a shell command as an administrator, which macOS asks
// the password for.
func runAsAdmin(cmd string) error {
	script := `do shell script "` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(cmd) + `" with administrator privileges`
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "(-128)") {
			return ErrCanceled
		}
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// ShellPath is where the user's shell looks for commands, which an app
// opened from the Finder does not inherit: the login shell's folders
// first, in its order, then those of PATH it lacks.
func ShellPath() []string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const mark = "MAYOI_PATH="
	out, _ := exec.CommandContext(ctx, sh, "-ilc", `echo "`+mark+`$PATH"`).Output()
	var all []string
	for line := range strings.Lines(string(out)) {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), mark); ok {
			all = append(all, filepath.SplitList(p)...)
		}
	}
	all = append(all, filepath.SplitList(os.Getenv("PATH"))...)
	var path []string
	for _, p := range all {
		if p = filepath.Clean(p); p != "." && !slices.Contains(path, p) {
			path = append(path, p)
		}
	}
	return path
}
