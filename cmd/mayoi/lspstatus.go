package main

import (
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"mayoi/internal/lsp"
	"mayoi/internal/plugin"
	"mayoi/internal/remote"
	"mayoi/locales"
)

// What a person sees of the language servers of a folder: Show Language
// Servers lists them with where they are in their life and where they
// run, and starts again the one chosen; one that could not start says why,
// and offers to start it again.

// classify works out why the server could not start, from err.
func (s *lspServer) classify() lspFailure {
	if s.command == "" {
		return lspFailOther // its plugin's resolver failed, as err says
	}
	if errors.Is(s.err, exec.ErrNotFound) || errors.Is(s.err, fs.ErrNotExist) {
		return lspFailMissing
	}
	if errors.Is(s.err, context.DeadlineExceeded) {
		return lspFailSilent
	}
	var stopped *lsp.StoppedError
	if errors.As(s.err, &stopped) {
		// env on an SSH host says it found no program with 127, as the
		// program, or a script that runs it, may say of itself: check
		// tells which.
		var exit *exec.ExitError
		if remote.Is(s.root) && errors.As(stopped.Err, &exit) && exit.ExitCode() == 127 {
			return lspFailUnchecked
		}
		return lspFailQuit
	}
	return lspFailOther
}

// checkProgram reports whether a program is found on the folder's
// machine, as a server's command started with env and unset is there;
// known is false when it could not tell.
var checkProgram = programOnHost

// check looks on the folder's host for the server's program, which ended
// with 127, for so long, or until the server is stopped or replaced.
func (s *lspServer) check() lspFailure {
	ctx, cancel := context.WithTimeout(s.ctx, lspCheckTimeout)
	defer cancel()
	found, known := checkProgram(ctx, s.root, s.launch.environment(), s.launch.UnsetEnv, s.command)
	switch {
	case !known:
		return lspFailUnchecked
	case found:
		return lspFailQuit
	}
	return lspFailMissing
}

// program is the name of the program the server ran, without its folder.
func (s *lspServer) program() string {
	name := path.Base(filepath.ToSlash(s.command))
	return strings.TrimSuffix(name, ".exe")
}

// reason is why the server could not start, in a few words.
func (s *lspServer) reason() string {
	switch s.failure {
	case lspFailMissing:
		if host, _, ok := remote.Split(s.root); ok {
			return locales.LspNotInstalledOn(s.program(), host)
		}
		return locales.LspNotInstalled(s.program())
	case lspFailQuit:
		var stopped *lsp.StoppedError
		if errors.As(s.err, &stopped) {
			return locales.LspQuit(firstLine(stopped.Err.Error()))
		}
	case lspFailSilent:
		return locales.LspNoAnswer()
	case lspFailUnchecked:
		return locales.LspExitUnchecked(s.program(), remote.HostOf(s.root))
	}
	if s.err == nil {
		return ""
	}
	return firstLine(s.err.Error())
}

// failMessage tells that the server could not start, and why; for a
// program that is missing, how to install it when the index of plugins
// read for the offers says so of the server's plugin and its command.
func (s *lspServer) failMessage() string {
	if s.failure == lspFailMissing {
		if install := serverInstallHint(s.plugin.Name, s.command); install != "" {
			return locales.LspCouldNotStartInstall(s.contrib.ID, s.reason(), install)
		}
	}
	return locales.LspCouldNotStart(s.contrib.ID, s.reason())
}

// serverInstallHint is how to install command, as the index of plugins
// last read says for the plugin of name, which looks for it by that name;
// "" when it does not, as for a command of a folder, or was not read. It
// reads nothing anew.
func serverInstallHint(name, command string) string {
	serverIndex.Lock()
	defer serverIndex.Unlock()
	for _, e := range serverIndex.entries {
		if e.Name == name && strings.TrimSuffix(e.Detect, ".exe") == strings.TrimSuffix(command, ".exe") {
			return e.Install
		}
	}
	return ""
}

// serverByID is the server of id an enabled plugin declares, nil for none.
func serverByID(id string) (*plugin.Plugin, *plugin.ServerContrib) {
	if plugins == nil {
		return nil, nil
	}
	for _, p := range plugins.enabled() {
		for i, sc := range p.Contributes.LanguageServers {
			if sc.ID == id {
				return p, &p.Contributes.LanguageServers[i]
			}
		}
	}
	return nil, nil
}

// restartServer starts the server of key afresh, whatever it is doing and
// however often it was started, once the one before it is gone. A restart
// asked for while one is under way is the same restart.
func (a *app) restartServer(key string) {
	root, id, _ := strings.Cut(key, "\x00")
	p, sc := serverByID(id)
	if p == nil {
		a.tell(note{message: locales.LspGone(id)})
		return
	}
	lspServers.Lock()
	old := lspServers.m[key]
	if old != nil && old.asked && old.state() == lspStarting {
		lspServers.Unlock()
		return
	}
	s := newServer(p, *sc, root, 1)
	s.asked = true
	lspServers.m[key] = s
	lspServers.Unlock()
	a.tell(note{message: locales.LspRestarting(id)})
	go s.start(a, old)
}

// stateText is where the server is in its life, as the list says it.
func (s *lspServer) stateText() string {
	switch s.state() {
	case lspStarting:
		return locales.LspStateStarting()
	case lspRunning:
		return locales.LspStateRunning()
	case lspFailed:
		if s.starts >= maxServerStarts {
			return locales.LspStateGaveUp(s.reason())
		}
		return locales.LspStateFailed(s.reason())
	}
	if s.starts >= maxServerStarts {
		return locales.LspStateStoppedGaveUp()
	}
	return locales.LspStateStopped()
}

// serverPlace is where the servers of root run: the folder, or the SSH
// host and the folder there.
func serverPlace(root string) string {
	if host, rp, ok := remote.Split(root); ok {
		return host + ":" + rp
	}
	return root
}

// showLanguageServers lists the language servers of the window's folder,
// and the one of the file shown when it has not started, and starts again
// the one chosen.
func (a *app) showLanguageServers() {
	type row struct {
		id string
		s  *lspServer
	}
	var rows []row
	lspServers.Lock()
	for _, s := range lspServers.m {
		if s.root == a.root {
			rows = append(rows, row{s.contrib.ID, s})
		}
	}
	lspServers.Unlock()
	if d := a.textEditor(); d != nil && a.root != "" {
		if _, sc := serverContrib(languageOf(d.document)); sc != nil &&
			!slices.ContainsFunc(rows, func(r row) bool { return r.id == sc.ID }) {
			rows = append(rows, row{sc.ID, nil})
		}
	}
	if len(rows) == 0 {
		a.tell(note{message: locales.LspNoneHere()})
		return
	}
	slices.SortFunc(rows, func(x, y row) int { return strings.Compare(x.id, y.id) })
	place := serverPlace(a.root)
	items := make([]pickItem, len(rows))
	for i, r := range rows {
		state := locales.LspStateNotStarted()
		if r.s != nil {
			state = r.s.stateText()
		}
		items[i] = pickItem{Label: r.id, Description: state, Detail: place, Icon: "plug"}
	}
	root := a.root
	a.openPluginPicker(items, locales.LspPickRestart(), func(i int) {
		if i >= 0 {
			a.restartServer(serverKey(root, rows[i].id))
		}
	})
}
