package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"mayoi/internal/remote"
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
	// Settings is what `mayoi settings` asks of the preferences, which
	// the command does itself rather than the app.
	Settings *Settings `json:"settings,omitempty"`
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

// Settings is what `mayoi settings` does with the preferences: list
// them, read some, or change some.
//
// The mayoi command changes the settings' files itself, so that it works
// while the app is not running, and a Mayoi running reads them again as
// it does the file a person saves.
type Settings struct {
	// Verb is what it does with the preferences: "" or "list" lists them
	// all, and "get" reads Keys. "set" changes the preferences of
	// Changes, an item of a list at a time for a key that ends in + or -,
	// and "unset" forgets them, which takes them back to their value of a
	// first launch. The changes are made in their order.
	Verb string `json:"verb,omitempty"`
	// Keys are the preferences to read.
	Keys []string `json:"keys,omitempty"`
	// Changes are what it changes, in order.
	Changes []Change `json:"changes,omitempty"`
	// Folder acts on the preferences of the folder the command ran in,
	// in its .mayoi/settings.json, rather than on the person's.
	Folder bool `json:"folder,omitempty"`
}

// The operations of a Change.
const (
	OpSet    = "set"    // the value of a preference, or of a field of one
	OpUnset  = "unset"  // a preference, or a field of one, goes, which takes its value of a first launch
	OpAdd    = "add"    // one more item of a list, as set key+=item
	OpRemove = "remove" // one item fewer of a list, as set key-=item
)

// Change is a preference to change: its key in the settings' file, which
// for a look of a project on the rail is a field of it, as
// projectTabStyle.'<folder>'.label, and the value it takes. Item is the
// text of an item of a list, for add and remove, which is no JSON of
// itself.
type Change struct {
	Op    string          `json:"op,omitempty"`
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value,omitempty"`
	Item  string          `json:"item,omitempty"`
}

// Reply is the app's answer: what to print, or what went wrong.
type Reply struct {
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Plain reports whether the request only opens folders and files, which
// starting the app with them does as well.
func (r Request) Plain() bool {
	if r.Window != "" || r.NewWindow || r.Wait || r.Diff != nil || r.Remote != "" || r.Run != nil || r.List || r.Settings != nil {
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
       mayoi settings [list | get <key>... | set <key>=<value>... | unset <key>...]
       mayoi run <command>[=<argument>]...
       mayoi commands

Opens folders and files in Mayoi, starting it if it isn't running.

Options:
  -g, --goto <file:line[:column]>  Open a file at a line and column
  -d, --diff <file> <file>         Compare two files
  -n, --new-window                 Open in a new window
  -w, --wait                       Wait for the files to be closed
  -f, --folder                     Act on the current folder's own settings
      --remote <host:/path>        Open a folder on an SSH host
      --window <folder>            Act on the window that has the folder open
  -h, --help                       Print this help
  -v, --version                    Print the version

Commands:
  settings          List the preferences of settings.json with their values,
                    get them, set them, or forget them, which gives them their
                    value of a first launch again. A value is JSON when it is,
                    as 16, true or ["a"], else text, as claude. With
                    --folder, the preferences of the current folder, in its
                    .mayoi/settings.json, of those a folder chooses.
                    set key+=item adds an item to a list and key-=item takes
                    one away, rather than the list whole, and a look of a
                    project on the rail is told a field at a time, as
                    projectTabStyle.'<folder>'.label=MG.
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
  mayoi settings
  mayoi settings get theme
  mayoi settings set fontSize=16 vim=true
  mayoi settings --folder set autoSave=false
  mayoi settings unset theme
  mayoi settings set 'aiSecretFiles+=*.pem'
  mayoi settings set projectTabStyle.'~/src/mayoi'.label=MG
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
	if len(args) > 0 && (args[0] == "run" || args[0] == "commands" || args[0] == "settings") {
		sub, args = args[0], args[1:]
		r.List = sub == "commands"
		if sub == "settings" {
			r.Settings = &Settings{}
		}
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
			case sub == "settings":
				if err := r.Settings.word(arg); err != nil {
					return r, err
				}
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
		case "-f", "--folder":
			if r.Settings == nil {
				return r, fmt.Errorf("%s takes the settings: mayoi settings --folder set <key>=<value>", name)
			}
			r.Settings.Folder = true
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
	case sub == "settings":
		if r.Paths != nil || r.Diff != nil || r.Remote != "" || r.NewWindow || r.Wait || r.Window != "" {
			return r, errors.New("settings takes only its preferences, and --folder")
		}
		if err := r.Settings.check(); err != nil {
			return r, err
		}
		r.Settings.resolve(abs)
	case sub != "" && (r.Paths != nil || r.Diff != nil || r.Remote != "" || r.NewWindow || r.Wait):
		return r, fmt.Errorf("%s takes only --window", sub)
	case r.Wait && len(r.Paths) == 0:
		return r, errors.New("--wait needs a file to wait for")
	case r.Diff != nil && (r.Paths != nil || r.Remote != ""):
		return r, errors.New("--diff opens nothing else")
	}
	return r, nil
}

// word takes a word of a mayoi settings: what it does with the
// preferences, or else a key to read, or a key and the value to give it.
func (s *Settings) word(arg string) error {
	if s.Verb == "" {
		if len(s.Changes) > 0 {
			return errors.New("a preference needs set or unset before it, as mayoi settings set fontSize=16")
		}
		switch arg {
		case "list", "get", "set", "unset":
			s.Verb = arg
			return nil
		}
		return fmt.Errorf("no settings command %q: mayoi settings lists, gets, sets or unsets them", arg)
	}
	switch s.Verb {
	case "list":
		return fmt.Errorf("list takes no %q: mayoi settings get <key> reads one", arg)
	case "get":
		s.Keys = append(s.Keys, arg)
		return nil
	case "unset":
		if strings.Contains(arg, "=") {
			return fmt.Errorf("unset takes a key, not %q", arg)
		}
		s.Changes = append(s.Changes, Change{Op: OpUnset, Key: arg})
		return nil
	}
	key, val, ok := strings.Cut(arg, "=")
	if !ok || key == "" || key == "+" || key == "-" {
		return fmt.Errorf("set takes key=value, not %q", arg)
	}
	// key+=item adds an item to a list, and key-=item takes one away,
	// rather than the list being written whole.
	switch {
	case strings.HasSuffix(key, "+"):
		return s.item(OpAdd, strings.TrimSuffix(key, "+"), val)
	case strings.HasSuffix(key, "-"):
		return s.item(OpRemove, strings.TrimSuffix(key, "-"), val)
	}
	s.Changes = append(s.Changes, Change{Op: OpSet, Key: key, Value: prefValue(val)})
	return nil
}

// item takes an item of the list of key, as key+=item adds it and
// key-=item takes it away: the text of the item, not the JSON of one.
func (s *Settings) item(op, key, item string) error {
	if item == "" {
		return fmt.Errorf("%s%s= needs an item", key, itemSign[op])
	}
	s.Changes = append(s.Changes, Change{Op: op, Key: key, Item: item})
	return nil
}

// check reports what a mayoi settings lacks to do what it asks.
func (s *Settings) check() error {
	switch s.Verb {
	case "get":
		if len(s.Changes) > 0 {
			return errors.New("get only reads preferences: mayoi settings set changes them")
		}
		if len(s.Keys) == 0 {
			return errors.New("get needs a preference: mayoi settings lists them")
		}
	case "list":
		if len(s.Changes) > 0 {
			return errors.New("list only reads preferences: mayoi settings set changes them")
		}
	case "set", "unset":
		if len(s.Changes) == 0 {
			return fmt.Errorf("%s needs a preference: mayoi settings lists them", s.Verb)
		}
	}
	return nil
}

// resolve makes the folders of the looks on the rail that the keys name
// the ones the app keeps them by: a folder of this machine whole, from
// the current folder or from ~.
func (s *Settings) resolve(abs func(string) string) {
	for i, key := range s.Keys {
		s.Keys[i] = folderKey(key, abs)
	}
	for i, c := range s.Changes {
		s.Changes[i].Key = folderKey(c.Key, abs)
	}
}

// folderKey is key with the folder of a look on the rail made whole, as
// projectTabStyle.'~/src/my.app'.label, and any other key as it is.
func folderKey(key string, abs func(string) string) string {
	segs, err := splitKey(key)
	if err != nil || len(segs) < 2 || segs[0] != "projectTabStyle" || remote.Is(segs[1]) {
		return key
	}
	dir := segs[1]
	if home, err := os.UserHomeDir(); err == nil && (dir == "~" || strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`)) {
		dir = filepath.Join(home, dir[1:])
	}
	quote := "'"
	if strings.Contains(dir, quote) {
		quote = `"`
	}
	segs[1] = quote + abs(dir) + quote
	return strings.Join(segs, ".")
}

// prefValue is the value of a preference as mayoi settings set gives it:
// the JSON where it is JSON, as 16, true or ["a"], else the text as it
// is, which is a text of the settings.
func prefValue(s string) json.RawMessage {
	if json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	data, _ := json.Marshal(s)
	return data
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
