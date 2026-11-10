package main

import (
	"context"
	"maps"
	"sort"
	"strings"
	"time"

	"mayoi/internal/plugin"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// The plugins installed from Git follow the person to their other
// machines through settings sync: plugins.json is kept in the settings as
// pluginSources, which sync carries, and what comes back is installed.

// recordsOf are the settings' plugin sources as the plugins folder keeps
// them, and sourcesOf the other way.
func recordsOf(src map[string]settings.PluginSource) plugin.Records {
	recs := plugin.Records{}
	for name, s := range src {
		recs[name] = plugin.Source{URL: s.URL, Ref: s.Ref, Commit: s.Commit}
	}
	return recs
}

func sourcesOf(recs plugin.Records) map[string]settings.PluginSource {
	if len(recs) == 0 {
		return nil
	}
	src := map[string]settings.PluginSource{}
	for name, r := range recs {
		src[name] = settings.PluginSource{URL: r.URL, Ref: r.Ref, Commit: r.Commit}
	}
	return src
}

// syncPlugins agrees the plugins folder with the settings' plugin sources
// in the background: what another machine installed or removed is so
// here, and what changed here goes into the settings for sync to send.
func (h *pluginHost) syncPlugins() {
	if !h.syncing {
		return
	}
	h.mu.Lock()
	h.syncRun.Invalidate()
	start := h.syncRun.Start(struct{}{})
	h.mu.Unlock()
	if !start {
		return
	}
	var want plugin.Records
	if p := h.prefsSnap.Load(); p != nil {
		want = recordsOf(p.PluginSources)
	}
	dir := h.reg.Dirs.Plugins
	go func() {
		defer recoverCrash()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		agreed, installed, removed, err := plugin.Sync(ctx, dir, want)
		h.mu.Lock()
		h.records = agreed
		h.mu.Unlock()
		h.onMain(func() {
			if s := h.prefs(); s != nil && !maps.Equal(recordsOf(s.Prefs.PluginSources), agreed) {
				s.Prefs.PluginSources = sourcesOf(agreed)
				s.Changed()
				h.syncPrefs()
			}
			if a := h.front(); a != nil {
				if len(installed) > 0 {
					a.tell(note{message: locales.PluginsSyncInstalled(strings.Join(installed, ", "))})
				}
				if len(removed) > 0 {
					a.tell(note{message: locales.PluginsSyncRemoved(strings.Join(removed, ", "))})
				}
				if err != nil {
					a.fail("%s", locales.PluginsSyncFailed(firstLine(err.Error())))
				}
			}
			h.mu.Lock()
			h.syncRun.Done(struct{}{})
			again := h.syncRun.Stale()
			h.mu.Unlock()
			if again {
				h.syncPlugins()
			}
		})
		if len(installed)+len(removed) > 0 {
			h.reg.Load()
		}
	}()
}

// recordsChanged syncs the plugins when plugins.json changed here: a
// plugin installed, updated or removed.
func (h *pluginHost) recordsChanged() {
	if !h.syncing {
		return
	}
	recs := plugin.ReadRecords(h.reg.Dirs.Plugins)
	h.mu.Lock()
	same := maps.Equal(recs, h.records)
	h.mu.Unlock()
	if !same {
		h.syncPlugins()
	}
}

// pluginUpdateEvery is how often the plugins installed from Git are
// checked for new commits.
const pluginUpdateEvery = time.Hour

// checkPluginUpdates looks for new commits of the plugins installed from
// Git, from now on, and tells of those it finds once.
func (h *pluginHost) checkPluginUpdates() {
	go func() {
		defer recoverCrash()
		timer := time.NewTimer(30 * time.Second)
		for range timer.C {
			h.findPluginUpdates()
			timer.Reset(pluginUpdateEvery)
		}
	}()
}

func (h *pluginHost) findPluginUpdates() {
	recs := plugin.ReadRecords(h.reg.Dirs.Plugins)
	found := map[string]string{}
	for name, src := range recs {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		latest, err := plugin.Latest(ctx, src)
		cancel()
		if err == nil && latest != "" && latest != src.Commit {
			found[name] = latest
		}
	}
	h.mu.Lock()
	var fresh []string
	for name, c := range found {
		if h.updates[name] != c {
			fresh = append(fresh, name)
		}
	}
	h.updates = found
	h.mu.Unlock()
	if len(fresh) == 0 {
		return
	}
	sort.Strings(fresh)
	h.onMain(func() {
		if a := h.front(); a != nil {
			a.tell(note{message: locales.PluginsUpdatesFound(strings.Join(fresh, ", ")), label: locales.CmdPluginsManage(),
				action: func() { a.host.showSettings(prefsPlugins, "") }})
		}
	})
}

// updateWaits reports whether a newer commit of a plugin was found.
func (h *pluginHost) updateWaits(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.updates[name]
	return ok
}

// updated forgets the update found of a plugin, once it is updated.
func (h *pluginHost) updated(name string) {
	h.mu.Lock()
	delete(h.updates, name)
	h.mu.Unlock()
}
