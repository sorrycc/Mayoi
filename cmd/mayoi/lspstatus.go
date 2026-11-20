package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os/exec"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"mayoi/internal/lsp"
	"mayoi/internal/plugin"
	"mayoi/internal/remote"
	"mayoi/internal/vfs"
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
	// It answered why: typescript-language-server, with no TypeScript to
	// run in the folder.
	var refused *lsp.ResponseError
	if errors.As(s.err, &refused) && strings.Contains(refused.Message, "Could not find a valid TypeScript installation") {
		return lspFailNoTypeScript
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

// tscVersion is what tsc --version says on the folder's machine, as a
// server's command started with env and unset is there; "" when it could
// not tell.
var tscVersion = tscVersionOnHost

func tscVersionOnHost(ctx context.Context, root string, env, unset []string) string {
	if !remote.Is(root) {
		select {
		case <-shellPathReady:
		case <-ctx.Done():
			return ""
		}
	}
	cmd, err := vfs.CommandEnvironment(root, env, unset, nil, "tsc", "--version")
	if err != nil {
		return ""
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if cmd.Start() != nil {
		return ""
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return ""
		}
		return strings.TrimSpace(out.String())
	case <-ctx.Done():
		cmd.Process.Kill()
		<-done
		return ""
	}
}

// checkNativeTSC reports whether the folder's tsc is TypeScript 7 or
// later, "Version 7.0.2" as it says, for so long.
func (s *lspServer) checkNativeTSC() bool {
	ctx, cancel := context.WithTimeout(s.ctx, lspCheckTimeout)
	defer cancel()
	v := strings.TrimPrefix(tscVersion(ctx, s.root, s.launch.environment(), s.launch.UnsetEnv), "Version ")
	major, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(major)
	return err == nil && n >= 7
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
func (s *lspServer) program() string { return programName(s.command) }

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
			// Its command ended with 0, as it would asked to stop.
			if stopped.Err == nil {
				return locales.LspQuitClean()
			}
			return locales.LspQuit(firstLine(stopped.Err.Error()))
		}
	case lspFailSilent:
		return locales.LspNoAnswer()
	case lspFailUnchecked:
		return locales.LspExitUnchecked(s.program(), remote.HostOf(s.root))
	case lspFailNoTypeScript:
		if s.nativeTSC {
			return locales.LspNoTypeScriptNative()
		}
		return locales.LspNoTypeScript()
	}
	if s.err == nil {
		return ""
	}
	return firstLine(s.err.Error())
}

// failMessage tells that the server could not start, and why; for a
// program that is missing, how to install it when the server says so, or
// the index of plugins read for the offers says so of the server's plugin
// and its command.
func (s *lspServer) failMessage() string {
	if s.failure == lspFailMissing {
		if install := cmp.Or(s.contrib.Install, serverInstallHint(s.plugin.Name, s.command)); install != "" {
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

// changedInitOptions are the ids of the servers whose initializationOptions
// of settings.json's languageServers differ from old to now: set, taken
// away or changed, but not only written otherwise.
func changedInitOptions(old, now map[string]json.RawMessage) []string {
	var ids []string
	for id := range maps.Keys(old) {
		if _, ok := now[id]; !ok {
			if _, set := userInitOptions(old[id]); set {
				ids = append(ids, id)
			}
		}
	}
	for id, prefs := range now {
		a, aSet := userInitOptions(old[id])
		b, bSet := userInitOptions(prefs)
		if aSet != bSet || !sameJSON(a, b) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// sameJSON reports whether a and b are the same JSON value, however each
// is written.
func sameJSON(a, b json.RawMessage) bool {
	if bytes.Equal(a, b) {
		return true
	}
	var va, vb any
	return decodeJSON(a, &va) == nil && decodeJSON(b, &vb) == nil && reflect.DeepEqual(va, vb)
}

// restartServersOf restarts the servers of ids, in every folder, each
// as its Restart would, so that their new initializationOptions reach
// them; the others are left as they are.
func (m *mayoi) restartServersOf(ids []string) {
	if len(ids) == 0 || len(m.wins) == 0 {
		return
	}
	lspServers.Lock()
	var servers []*lspServer
	for _, s := range lspServers.m {
		if slices.Contains(ids, s.contrib.ID) {
			servers = append(servers, s)
		}
	}
	lspServers.Unlock()
	slices.SortFunc(servers, func(x, y *lspServer) int { return strings.Compare(x.key, y.key) })
	for _, s := range servers {
		a := m.wins[0].a
		if w := m.showing(s.root); w != nil {
			a = w.a
		}
		a.restartServer(s.key)
	}
}
