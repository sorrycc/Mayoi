package main

import (
	"context"
	"os/exec"
	"slices"
	"sync"
	"time"

	"mayoi/internal/plugin"
	"mayoi/internal/remote"
	"mayoi/internal/vfs"
	"mayoi/locales"
)

// A file of a language no plugin has a server for offers the plugin of
// the index that brings one, once the server's program is found on the
// folder's machine; or says how to install the program, once, when it is
// not there.

// serverIndex is the index of plugins as the offers last read it.
var serverIndex = struct {
	sync.Mutex
	entries []plugin.IndexEntry
	at      time.Time // when it was read, zero for never
	reading bool
}{}

// fetchServerIndex reads the index of plugins, for the offers.
var fetchServerIndex = plugin.FetchIndex

// serverIndexAge is how long the index read is taken as it is.
const serverIndexAge = time.Hour

// serverChecks are the folders and languages whose servers were looked
// for, this run.
var serverChecks = struct {
	sync.Mutex
	m map[string]bool
}{m: map[string]bool{}}

// indexForServers is the index of plugins, read again when it is old; nil
// while it cannot be read.
func indexForServers(ctx context.Context) []plugin.IndexEntry {
	serverIndex.Lock()
	if !serverIndex.at.IsZero() && time.Since(serverIndex.at) < serverIndexAge || serverIndex.reading {
		defer serverIndex.Unlock()
		return serverIndex.entries
	}
	serverIndex.reading = true
	serverIndex.Unlock()
	entries, err := fetchServerIndex(ctx, nil)
	serverIndex.Lock()
	defer serverIndex.Unlock()
	serverIndex.reading = false
	if err == nil {
		serverIndex.entries, serverIndex.at = entries, time.Now()
	}
	return serverIndex.entries
}

// serverEntry is the plugin of the index that brings a server for lang,
// with a program to look for.
func serverEntry(entries []plugin.IndexEntry, lang string) (plugin.IndexEntry, bool) {
	for _, e := range entries {
		if e.Detect != "" && slices.Contains(e.Languages, lang) {
			return e, true
		}
	}
	return plugin.IndexEntry{}, false
}

// serverInstalled reports whether a program is on the path of the folder's
// machine, as a server's command is started there.
func serverInstalled(ctx context.Context, root, name string) bool {
	if !remote.Is(root) {
		_, err := exec.LookPath(name)
		return err == nil
	}
	cmd, err := vfs.Command(root, nil, nil, "sh", "-c", `command -v "$1" >/dev/null`, "sh", name)
	if err != nil {
		return false
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	select {
	case err := <-done:
		return err == nil
	case <-ctx.Done():
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return false
	}
}

// offered reports whether Mayoi told of key, a language or "missing:" and
// one, and has it told of once only when it did not.
func (a *app) offered(key string) bool {
	if slices.Contains(a.settings.ServerOffers, key) {
		return true
	}
	a.settings.ServerOffers = append(a.settings.ServerOffers, key)
	a.settings.Changed()
	return false
}

// offerServer offers the plugin of the index that brings a server for the
// language of a document that opened, which none has, in the background.
func (a *app) offerServer(d *document) {
	lang, root := languageOf(d), a.root
	if lang == "" || root == "" || plugins == nil || d.path == "" {
		return
	}
	if slices.Contains(a.settings.ServerOffers, lang) && slices.Contains(a.settings.ServerOffers, "missing:"+lang) {
		return
	}
	serverChecks.Lock()
	key := root + "\x00" + lang
	seen := serverChecks.m[key]
	serverChecks.m[key] = true
	serverChecks.Unlock()
	if seen {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		e, ok := serverEntry(indexForServers(ctx), lang)
		if !ok {
			return
		}
		found := serverInstalled(ctx, root, e.Detect)
		a.post(func() {
			// Installed, though off, or installed since.
			if plugins.reg.Plugin(e.Name) != nil {
				return
			}
			name := e.DisplayName
			if name == "" {
				name = e.Name
			}
			switch {
			case found && !a.offered(lang):
				a.tell(note{message: locales.LspOffer(e.Detect, name), label: locales.LspOfferInstall(),
					action: func() { a.installServerPlugin(e) }})
			case !found && e.Install != "" && !a.offered("missing:"+lang):
				a.tell(note{message: locales.LspMissing(name, e.Detect, e.Install)})
			}
		})
	}()
}

// installServerPlugin installs a plugin of the index, whose servers start
// for the files open as it loads.
func (a *app) installServerPlugin(e plugin.IndexEntry) {
	a.tell(note{message: locales.PluginsInstalling(e.Name)})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_, err := plugin.Install(ctx, plugin.UserDirs().Plugins, e.Repository, "")
		a.post(func() {
			if err != nil {
				a.fail("%s", locales.LspInstallFailed(e.Name, firstLine(err.Error())))
				return
			}
			a.tell(note{message: locales.PluginsInstalled(e.Name)})
			if plugins != nil {
				go plugins.reg.Load()
			}
		})
	}()
}
