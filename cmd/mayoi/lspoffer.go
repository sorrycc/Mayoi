package main

import (
	"context"
	"errors"
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
// not there, and offers the plugin as a file opens after it is installed.

// serverIndex is the index of plugins as the offers last read it.
var serverIndex = struct {
	sync.Mutex
	entries  []plugin.IndexEntry
	at       time.Time     // when it was read, zero for never
	failedAt time.Time     // when it last could not be read
	reading  chan struct{} // closed as the read under way ends, nil for none
}{}

// fetchServerIndex reads the index of plugins, for the offers.
var fetchServerIndex = plugin.FetchIndex

// installServer installs a plugin of the index, for the offers.
var installServer = plugin.Install

// serverIndexAge is how long the index read is taken as it is, and
// serverIndexRetry how long after it could not be read it is not asked for
// again.
const (
	serverIndexAge   = time.Hour
	serverIndexRetry = 5 * time.Minute
)

// serverRecheck is how long after its program was not found a file of a
// language opening looks for it again, on the folder's machine.
var serverRecheck = time.Minute

// serverChecks are the folders and languages whose servers were looked
// for, this run: by when they may be looked for again, zero for never.
var serverChecks = struct {
	sync.Mutex
	m map[string]time.Time
}{m: map[string]time.Time{}}

// indexForServers is the index of plugins, read again when it is old; ok
// is false while it was never read. A read under way is waited for, and
// one that failed is not tried again for a while.
func indexForServers(ctx context.Context) (entries []plugin.IndexEntry, ok bool) {
	serverIndex.Lock()
	for {
		fresh := !serverIndex.at.IsZero() && time.Since(serverIndex.at) < serverIndexAge
		failed := !serverIndex.failedAt.IsZero() && time.Since(serverIndex.failedAt) < serverIndexRetry
		if fresh || serverIndex.reading == nil && failed {
			defer serverIndex.Unlock()
			return serverIndex.entries, !serverIndex.at.IsZero()
		}
		if serverIndex.reading == nil {
			break
		}
		wait := serverIndex.reading
		serverIndex.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, false
		}
		serverIndex.Lock()
	}
	done := make(chan struct{})
	serverIndex.reading = done
	serverIndex.Unlock()
	read, err := fetchServerIndex(ctx, nil)
	serverIndex.Lock()
	defer serverIndex.Unlock()
	serverIndex.reading = nil
	close(done)
	if err != nil {
		serverIndex.failedAt = time.Now()
	} else {
		serverIndex.entries, serverIndex.at, serverIndex.failedAt = read, time.Now(), time.Time{}
	}
	return serverIndex.entries, !serverIndex.at.IsZero()
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
// machine, as a server's command is started there; known is false when it
// could not tell, as when the host did not answer.
func serverInstalled(ctx context.Context, root, name string) (found, known bool) {
	if !remote.Is(root) {
		// On the path of the user's shell, not the one Mayoi started with.
		select {
		case <-shellPathReady:
		case <-ctx.Done():
			return false, false
		}
		_, err := exec.LookPath(name)
		return err == nil, true
	}
	cmd, err := vfs.Command(root, nil, nil, "sh", "-c", `command -v "$1" >/dev/null`, "sh", name)
	if err != nil {
		return false, false
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	select {
	case err := <-done:
		// ssh says it could not reach the host with 255.
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return false, exit.ExitCode() != 255
		}
		return err == nil, err == nil
	case <-ctx.Done():
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return false, false
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
	key := root + "\x00" + lang
	serverChecks.Lock()
	until, seen := serverChecks.m[key]
	if seen && (until.IsZero() || time.Now().Before(until)) {
		serverChecks.Unlock()
		return
	}
	serverChecks.m[key] = time.Time{}
	serverChecks.Unlock()
	// again lets a file of the language opening look again: at once when
	// nothing could be told, after a while when the program was missing.
	again := func(after time.Duration) {
		serverChecks.Lock()
		if after == 0 {
			delete(serverChecks.m, key)
		} else {
			serverChecks.m[key] = time.Now().Add(after)
		}
		serverChecks.Unlock()
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		entries, ok := indexForServers(ctx)
		if !ok {
			again(0)
			return
		}
		e, ok := serverEntry(entries, lang)
		if !ok {
			return
		}
		found, known := serverInstalled(ctx, root, e.Detect)
		switch {
		case !known:
			again(0)
			return
		case !found:
			again(serverRecheck)
		}
		a.post(func() {
			// Installed, though off, or installed since.
			if plugins.reg.Plugin(e.Name) != nil {
				return
			}
			switch {
			case found && !a.offered(lang):
				a.tell(note{message: locales.LspOffer(e.Detect, serverName(e)), label: locales.LspOfferInstall(),
					action: func() { a.installServerPlugin(e, lang) }})
			case !found && e.Install != "" && !a.offered("missing:"+lang):
				a.tell(note{message: locales.LspMissing(serverName(e), e.Detect, e.Install)})
			}
		})
	}()
}

// serverName is the name of a plugin of the index, as the offers say it.
func serverName(e plugin.IndexEntry) string {
	if e.DisplayName != "" {
		return e.DisplayName
	}
	return e.Name
}

// installServerPlugin installs a plugin of the index offered for lang,
// whose servers start for the files open as it loads. One that could not
// install may be tried again, now or as the offer comes again.
func (a *app) installServerPlugin(e plugin.IndexEntry, lang string) {
	a.tell(note{message: locales.PluginsInstalling(e.Name)})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_, err := installServer(ctx, plugin.UserDirs().Plugins, e.Repository, "")
		a.post(func() {
			if err != nil {
				if i := slices.Index(a.settings.ServerOffers, lang); i >= 0 {
					a.settings.ServerOffers = slices.Delete(slices.Clone(a.settings.ServerOffers), i, i+1)
					a.settings.Changed()
				}
				a.tell(note{message: locales.LspInstallFailed(e.Name, firstLine(err.Error())),
					label: locales.PluginsIndexRetry(), action: func() { a.installServerPlugin(e, lang) }})
				return
			}
			a.tell(note{message: locales.PluginsInstalled(e.Name)})
			if h := plugins; h != nil {
				go func() {
					h.reg.Load()
					h.onMain(h.startForOpen)
				}()
			}
		})
	}()
}
