package main

import (
	"fmt"
	"log"
	"mayoi/internal/cli"
	"mayoi/internal/remote"
	"mayoi/locales"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
)

// commandLineClient runs Mayoi as the mayoi command, when it is started as
// one: it asks the running app over its socket, or starts it, and exits
// with the command's status. It returns when the app is to start here.
func commandLineClient() {
	if !commandLine() {
		return
	}
	os.Unsetenv(cliEnv)
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return
	}
	c := cli.Client{Socket: cli.SocketPath(dir), Version: appVersion(), Start: startApp, Inline: runtime.GOOS == "linux"}
	status, handled := c.Run(os.Args[1:], workingDir(), os.Stdout, os.Stderr)
	if handled {
		os.Exit(status)
	}
}

// cliEnv tells the app's executable that the mayoi script ran it, as the
// command.
const cliEnv = "MAYOI_CLI"

// workingDir is the directory the command ran in, as the shell spells
// it, through the links it followed.
func workingDir() string {
	wd, _ := os.Getwd()
	if pwd := os.Getenv("PWD"); filepath.IsAbs(pwd) {
		if a, err := os.Stat(pwd); err == nil {
			if b, err := os.Stat(wd); err == nil && os.SameFile(a, b) {
				return pwd
			}
		}
	}
	return wd
}

// listenCLI answers the mayoi command on a socket in the data folder,
// while the app runs.
func (m *mayoi) listenCLI() {
	if runtime.GOOS == "windows" || os.Getenv("MAYOI_SNAPSHOT") != "" {
		return
	}
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return
	}
	ln, err := cli.Listen(cli.SocketPath(dir))
	if err != nil {
		log.Printf("mayoi: the mayoi command: %v", err)
		return
	}
	mygo.App.OnQuit(func() { ln.Close() })
	go cli.Serve(ln, func(req cli.Request, reply func(cli.Reply)) {
		mygo.RunOnMain(func() { m.serve(req, reply) })
	})
}

// serve does what the mayoi command asks, on the main thread.
func (m *mayoi) serve(req cli.Request, reply func(cli.Reply)) {
	fail := func(format string, args ...any) { reply(cli.Reply{Error: fmt.Sprintf(format, args...)}) }
	switch {
	case req.Remote != "":
		if !remoteSupported {
			fail("remote folders are not supported here")
			return
		}
		host, p, _ := strings.Cut(req.Remote, ":")
		m.openFolder(remote.Path(host, p), !req.NewWindow)
		reply(cli.Reply{})
		return
	case req.Diff != nil:
		for _, p := range req.Diff {
			if info, err := os.Stat(p); err != nil || !info.Mode().IsRegular() {
				fail("%s is not a file", p)
				return
			}
		}
		h, err := m.target(req)
		if err != nil {
			fail("%v", err)
			return
		}
		a := h.a
		h.do(func() { a.compareFiles(req.Diff[0], req.Diff[1]) })
		h.show()
		reply(cli.Reply{})
		return
	case req.List:
		h, err := m.target(req)
		if err != nil {
			fail("%v", err)
			return
		}
		reply(cli.Reply{Output: h.a.commandList()})
		return
	case req.Run != nil:
		h, err := m.target(req)
		if err != nil {
			fail("%v", err)
			return
		}
		runs, err := h.a.cliCommands(req.Run, req.Cwd)
		if err != nil {
			fail("%v", err)
			return
		}
		h.do(func() {
			for _, run := range runs {
				run()
			}
		})
		h.show()
		reply(cli.Reply{})
		return
	}
	m.openTargets(req, reply)
}

// openTargets opens the folders and files of the mayoi command, at their
// lines, and, with --wait, replies once the files are closed.
func (m *mayoi) openTargets(req cli.Request, reply func(cli.Reply)) {
	if req.Window != "" {
		if m.showing(req.Window) == nil {
			reply(cli.Reply{Error: fmt.Sprintf("no window has %s open", req.Window)})
			return
		}
	}
	paths := make([]string, len(req.Paths))
	at := map[string]cli.Target{}
	for i, t := range req.Paths {
		paths[i] = t.Path
		at[filepath.Clean(t.Path)] = t
	}
	folders, files := splitPaths(paths)
	if req.Wait && len(files) == 0 {
		reply(cli.Reply{Error: "no file to wait for"})
		return
	}
	if len(req.Paths) == 0 && !req.NewWindow {
		// As the app is started again: its window to the front.
		if len(m.wins) > 0 {
			m.wins[0].show()
		} else {
			m.openBlankWindow()
		}
		reply(cli.Reply{})
		return
	}
	for _, dir := range folders {
		m.openFolder(dir, !req.NewWindow)
	}
	// Files given to a new window with no folder go to a window of their
	// own.
	var into *desktop
	switch {
	case req.Window != "":
		into = m.showing(req.Window)
	case req.NewWindow && len(folders) == 0:
		into = m.openWindow(m.s.NewWindow())
	}
	left := len(files)
	for _, p := range files {
		t := at[p]
		var closed func()
		if req.Wait {
			closed = func() {
				if left--; left == 0 {
					reply(cli.Reply{})
				}
			}
		}
		m.openFileIn(into, p, func(a *app) {
			if t.Line > 0 {
				a.goToLine(t.Line, t.Col)
			}
			if closed != nil {
				a.waitClosed(closed)
			}
		})
	}
	if !req.Wait {
		reply(cli.Reply{})
	}
}

// target is the project a command acts on: the one of --window, else the
// one whose folder holds the directory it ran in, else the one in front,
// else a new window.
func (m *mayoi) target(req cli.Request) (*desktop, error) {
	if req.Window != "" {
		if h := m.showing(req.Window); h != nil {
			return h, nil
		}
		return nil, fmt.Errorf("no window has %s open", req.Window)
	}
	var best *desktop
	deepest := ""
	for _, w := range m.wins {
		if root := w.a.folder(); root != "" && (root == req.Cwd || inFolder(root, req.Cwd)) && len(root) > len(deepest) {
			best, deepest = w, root
		}
	}
	switch {
	case best != nil:
		return best, nil
	case len(m.wins) > 0:
		return m.wins[0], nil
	}
	return m.openWindow(m.s.NewWindow()), nil
}

// commandList is what mayoi commands prints: a line for each command,
// its identifier, name, shortcut and argument, apart by tabs.
func (a *app) commandList() string {
	cmds := a.commands()
	slices.SortFunc(cmds, func(x, y command) int { return strings.Compare(x.id, y.id) })
	sep := "+"
	if macKeys() {
		sep = ""
	}
	var b strings.Builder
	for _, cmd := range cmds {
		label := cmd.label
		if g := commandGroup(locales.In(i18n.Locale()), cmd.id); g != "" {
			label = g + ": " + label
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", cmd.id, label, strings.Join(a.commandCaps(cmd), sep), commandArgs[cmd.id].hint)
	}
	return b.String()
}

// cliCommands are what runs the commands the mayoi command names, checked
// first: an unknown command, or an argument one does not take, runs none.
func (a *app) cliCommands(want []cli.Command, cwd string) ([]func(), error) {
	cmds := a.commands()
	var runs []func()
	for _, c := range want {
		i := slices.IndexFunc(cmds, func(cmd command) bool { return cmd.id == c.ID })
		if i < 0 {
			return nil, fmt.Errorf("no command %s: mayoi commands lists them", c.ID)
		}
		arg, takes := commandArgs[c.ID]
		switch {
		case c.Arg == "":
			runs = append(runs, cmds[i].run)
		case !takes:
			return nil, fmt.Errorf("%s takes no argument", c.ID)
		default:
			run, err := arg.take(a, c.Arg, cwd)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", c.ID, err)
			}
			runs = append(runs, run)
		}
	}
	return runs, nil
}

// commandArg is what a command takes from the mayoi command in place of
// what it would ask in the window: hint names it, and take checks it and
// returns what runs.
type commandArg struct {
	hint string
	take func(a *app, arg, cwd string) (func(), error)
}

var commandArgs = map[string]commandArg{
	"file.openFolder": {"<folder>", func(a *app, arg, cwd string) (func(), error) {
		dir, err := folderArg(arg, cwd)
		return func() { a.switchFolder(dir) }, err
	}},
	"file.openFolderWindow": {"<folder>", func(a *app, arg, cwd string) (func(), error) {
		dir, err := folderArg(arg, cwd)
		return func() { a.host.newWindow(dir) }, err
	}},
	"file.saveAs": {"<file>", func(a *app, arg, cwd string) (func(), error) {
		ed := a.activeEditor()
		if ed == nil {
			return nil, fmt.Errorf("no file is shown")
		}
		d, to := ed.document, pathArg(arg, cwd)
		// On an SSH host, a path there, from the folder.
		if host, root, ok := remote.Split(a.root); ok {
			if to = arg; !path.IsAbs(to) {
				to = path.Join(root, to)
			}
			to = remote.Path(host, to)
		}
		return func() { a.saveDocAs(d, to) }, nil
	}},
	"go.line": {"<line[:column]>", func(a *app, arg, cwd string) (func(), error) {
		l, c, _ := strings.Cut(arg, ":")
		line, err := strconv.Atoi(l)
		col := 0
		if err == nil && c != "" {
			col, err = strconv.Atoi(c)
		}
		if err != nil || line < 1 || col < 0 {
			return nil, fmt.Errorf("%q is not a line[:column]", arg)
		}
		return func() { a.goToLine(line, col) }, nil
	}},
	"go.quickOpen": {"<text>", func(a *app, arg, cwd string) (func(), error) {
		return func() { a.openQuick(arg) }, nil
	}},
	"view.selectTheme": {"<theme>", func(a *app, arg, cwd string) (func(), error) {
		id := "view.theme." + strings.ToLower(strings.ReplaceAll(arg, " ", ""))
		if !slices.ContainsFunc(a.commands(), func(cmd command) bool { return cmd.id == id }) {
			return nil, fmt.Errorf("no theme %s: mayoi, matrix, claude, qoder or grokbot", arg)
		}
		return func() { a.run(id) }, nil
	}},
	"remote.forwardPort": {"<port>", func(a *app, arg, cwd string) (func(), error) {
		port, err := strconv.Atoi(strings.TrimPrefix(arg, ":"))
		switch {
		case err != nil || port < 1 || port > 65535:
			return nil, fmt.Errorf("%q is not a port", arg)
		case a.conn == nil:
			return nil, fmt.Errorf("the folder is not on an SSH host")
		}
		return func() {
			if _, err := a.forwardPort(port); err != nil {
				a.fail("%v", err)
			}
		}, nil
	}},
}

// pathArg is a path the mayoi command gave, absolute.
func pathArg(p, cwd string) string {
	if rest, ok := strings.CutPrefix(p, "~"+string(filepath.Separator)); ok || p == "~" {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	return filepath.Clean(p)
}

// folderArg is a folder the mayoi command gave, absolute, which is there.
func folderArg(p, cwd string) (string, error) {
	p = pathArg(p, cwd)
	if info, err := os.Stat(p); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s is not a folder", p)
	}
	return p, nil
}

// waitClosed calls done once the document shown is closed, or the project
// is.
func (a *app) waitClosed(done func()) {
	ed := a.activeEditor()
	if ed == nil {
		done()
		return
	}
	a.waits = append(a.waits, docWait{ed.document, done})
}

// docWait is a mayoi --wait waiting for a document to close.
type docWait struct {
	doc  *document
	done func()
}

// docClosed answers the waits for a document that closed, nil for every
// one, as the project closes.
func (a *app) docClosed(d *document) {
	a.waits = slices.DeleteFunc(a.waits, func(w docWait) bool {
		if d == nil || w.doc == d {
			w.done()
			return true
		}
		return false
	})
}
