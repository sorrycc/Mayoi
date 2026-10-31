package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// Request is what the mayoi command asks the running app.
type Request struct {
	// Cwd is the directory the command ran in, which picks the project
	// it acts on.
	Cwd string `json:"cwd"`
	// Window is the folder of the project to act on, "" for the one
	// whose folder holds Cwd, else the one in front.
	Window string `json:"window,omitempty"`
	// Paths are the folders and files to open, absolute.
	Paths []Target `json:"paths,omitempty"`
	// NewWindow opens them in a new window.
	NewWindow bool `json:"newWindow,omitempty"`
	// Wait holds the answer until the files of Paths are closed.
	Wait bool `json:"wait,omitempty"`
	// Diff is two files to compare.
	Diff []string `json:"diff,omitempty"`
	// Remote is a folder on an SSH host, host:/path.
	Remote string `json:"remote,omitempty"`
	// Run are commands to run, in order.
	Run []Command `json:"run,omitempty"`
	// List asks for the commands.
	List bool `json:"list,omitempty"`
}

// Target is a folder or file, and with -g a line and column of it,
// 1-based, 0 when not given.
type Target struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
	Col  int    `json:"col,omitempty"`
}

// Command is a command of the app by its identifier, as the command
// palette and keybindings.json know it, with what it takes, if anything.
type Command struct {
	ID  string `json:"id"`
	Arg string `json:"arg,omitempty"`
}

// Reply is the app's answer: what to print, or what went wrong.
type Reply struct {
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Plain reports whether the request only opens folders and files, which
// starting the app with them does as well.
func (r Request) Plain() bool {
	if r.Window != "" || r.NewWindow || r.Wait || r.Diff != nil || r.Remote != "" || r.Run != nil || r.List {
		return false
	}
	for _, t := range r.Paths {
		if t.Line != 0 {
			return false
		}
	}
	return true
}

// ErrHelp and ErrVersion are -h and -v, which the command answers itself.
var (
	ErrHelp    = errors.New("help")
	ErrVersion = errors.New("version")
)

// Usage is what mayoi --help prints.
const Usage = `Usage: mayoi [options] [folder or file]...
       mayoi run <command>[=<argument>]...
       mayoi commands

Opens folders and files in Mayoi, starting it if it isn't running.

Options:
  -g, --goto <file:line[:column]>  Open a file at a line and column
  -d, --diff <file> <file>         Compare two files
  -n, --new-window                 Open in a new window
  -w, --wait                       Wait for the files to be closed
      --remote <host:/path>        Open a folder on an SSH host
      --window <folder>            Act on the window that has the folder open
  -h, --help                       Print this help
  -v, --version                    Print the version

Commands:
  run <command>...  Run commands by their identifiers, as keybindings.json
                    names them, on the window of the current folder, else the
                    one in front. Some take an argument after =, as
                    file.openFolder=<folder> or go.line=<line[:column]>.
  commands          List the commands, their names, shortcuts and arguments

Examples:
  mayoi .
  mayoi -g main.go:42:7
  mayoi -d old.txt new.txt
  git config core.editor "mayoi -w"
  mayoi run view.terminal git.pull
  mayoi run view.theme.claude
`

// Parse reads the mayoi command's arguments, run in cwd.
func Parse(args []string, cwd string) (Request, error) {
	r := Request{Cwd: cwd}
	abs := func(p string) string {
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		return filepath.Clean(p)
	}
	sub := ""
	if len(args) > 0 && (args[0] == "run" || args[0] == "commands") {
		sub, args = args[0], args[1:]
		r.List = sub == "commands"
	}
	value := func(i *int, flag string) (string, error) {
		if *i+1 >= len(args) || args[*i+1] == "" {
			return "", fmt.Errorf("%s needs a value", flag)
		}
		*i++
		return args[*i], nil
	}
	rest := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if rest || arg == "-" || !strings.HasPrefix(arg, "-") {
			switch {
			case arg == "":
			case sub == "run":
				id, val, _ := strings.Cut(arg, "=")
				if id == "" {
					return r, fmt.Errorf("no command in %q", arg)
				}
				r.Run = append(r.Run, Command{ID: id, Arg: val})
			case sub == "commands":
				return r, fmt.Errorf("commands takes no %q", arg)
			default:
				r.Paths = append(r.Paths, Target{Path: abs(arg)})
			}
			continue
		}
		name, val, hasVal := strings.Cut(arg, "=")
		var err error
		get := func() string {
			if hasVal {
				return val
			}
			v, e := value(&i, name)
			err = e
			return v
		}
		switch name {
		case "--":
			rest = true
		case "-h", "--help":
			return r, ErrHelp
		case "-v", "--version":
			return r, ErrVersion
		case "-n", "--new-window":
			r.NewWindow = true
		case "-w", "--wait":
			r.Wait = true
		case "-g", "--goto":
			v := get()
			if err == nil {
				t := parseGoto(v)
				t.Path = abs(t.Path)
				r.Paths = append(r.Paths, t)
			}
		case "-d", "--diff":
			if i+2 >= len(args) {
				return r, errors.New("--diff needs two files")
			}
			r.Diff = []string{abs(args[i+1]), abs(args[i+2])}
			i += 2
		case "--remote":
			v := get()
			if err == nil {
				if host, p, ok := strings.Cut(v, ":"); !ok || host == "" || !strings.HasPrefix(p, "/") {
					err = fmt.Errorf("--remote takes host:/path, not %q", v)
				}
				r.Remote = v
			}
		case "--window":
			if v := get(); err == nil {
				r.Window = abs(v)
			}
		default:
			return r, fmt.Errorf("unknown option %s", name)
		}
		if err != nil {
			return r, err
		}
	}
	switch {
	case sub == "run" && len(r.Run) == 0:
		return r, errors.New("run needs a command: mayoi commands lists them")
	case sub != "" && (r.Paths != nil || r.Diff != nil || r.Remote != "" || r.NewWindow || r.Wait):
		return r, fmt.Errorf("%s takes only --window", sub)
	case r.Wait && len(r.Paths) == 0:
		return r, errors.New("--wait needs a file to wait for")
	case r.Diff != nil && (r.Paths != nil || r.Remote != ""):
		return r, errors.New("--diff opens nothing else")
	}
	return r, nil
}

// parseGoto splits file:line:column, keeping colons that are part of the
// name.
func parseGoto(s string) Target {
	t := Target{Path: s}
	rest, last, ok := cutLastNumber(s)
	if !ok {
		return t
	}
	if path, line, ok := cutLastNumber(rest); ok {
		t.Path, t.Line, t.Col = path, line, last
		return t
	}
	t.Path, t.Line = rest, last
	return t
}

// cutLastNumber splits a trailing :N off s.
func cutLastNumber(s string) (string, int, bool) {
	i := strings.LastIndexByte(s, ':')
	if i <= 0 {
		return s, 0, false
	}
	n, err := strconv.Atoi(s[i+1:])
	if err != nil || n < 1 {
		return s, 0, false
	}
	return s[:i], n, true
}
