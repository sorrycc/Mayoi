package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/keymap"
	"mayoi/internal/plugin"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// pluginsPage is what the Plugins page of the settings window keeps: what
// is typed to install, the index as last read, and the work going on.
type pluginsPage struct {
	source  string
	busy    string // what is being done, "" for nothing
	index   []plugin.IndexEntry
	indexOK bool
	reading bool
	filter  string
	err     string
}

// pluginsPane is the Plugins page: the plugins installed, bundled and
// scripts, each with its switch and its settings; installing from Git or
// the index; and the shortcuts plugins could not take.
func (w *prefsWindow) pluginsPane(c *ui.Context) {
	p := w.pal
	w.head(c, "prefs-head", iconPlug, prefSection(settings.CatPlugins), func() {
		if ui.Button(c.Key("plugins-reload"), locales.CmdPluginsReload()).FontSize(12).Padding(3, 10).Clicked() && plugins != nil {
			go plugins.reg.ReloadAll()
		}
		if ui.Button(c.Key("plugins-folder"), locales.CmdPluginsOpenFolder()).FontSize(12).Padding(3, 10).Clicked() {
			w.inFront(func(a *app) { a.openPluginsFolder() })
		}
	})
	ui.Scroll(c.Key("prefs-rows-plugins")).Grow(1).MinHeight(0).Children(func() {
		fadePage(ui.Column(c.Key("prefs-page-plugins")).Padding(20, 28, 32, 28).Gap(10).AlignSelf(ui.Center).
			WidthPercent(100).MaxWidth(prefsPageWidth)).Children(func() {
			if plugins == nil {
				w.noneFound(c, locales.PluginsNotStarted())
				return
			}
			if plugins.reg.Safe() {
				ui.Text(c.Key("plugins-safe"), locales.PluginsSafeMode()).FontSize(12).TextColor(p.dangerFg)
			}
			w.installCard(c)
			list := plugins.reg.Plugins()
			recs := plugin.ReadRecords(plugins.reg.Dirs.Plugins)
			w.card(c, "plugins-list", func() {
				if len(list) == 0 {
					ui.Text(c, locales.PluginsNone()).FontSize(12.5).TextColor(p.muted).Padding(14, 16)
				}
				for i, pl := range list {
					if i > 0 {
						ui.Row(c.Key("plugins-rule-"+pl.Name)).Height(1).Margin(0, 16).Background(p.border).Shrink(0)
					}
					w.pluginRow(c, pl, recs)
				}
			})
			if skipped := w.skippedPluginKeys(); len(skipped) > 0 {
				w.sectionLabel(c, "plugins-keys-head", locales.PluginsSkippedKeys())
				w.card(c, "plugins-keys", func() {
					for i, k := range skipped {
						if i > 0 {
							ui.Row(c.Key(fmt.Sprintf("plugins-key-rule-%d", i))).Height(1).Margin(0, 16).Background(p.border).Shrink(0)
						}
						w.skippedKeyRow(c, i, k)
					}
				})
			}
			w.indexCard(c)
			w.sectionLabel(c, "plugins-settings-head", locales.PluginsMayoiSettings())
			w.card(c, "prefs-card-plugins", func() {
				first := true
				for _, f := range settings.Schema {
					if f.Category != settings.CatPlugins || f.Key == "plugins" || f.Memory {
						continue
					}
					if !first {
						ui.Row(c.Key("prefs-rule-"+f.Key)).Height(1).Margin(0, 16).Background(p.border).Shrink(0)
					}
					first = false
					w.row(c, f, nil)
				}
			})
		})
	})
}

// sectionLabel is the label above a card of the Plugins page.
func (w *prefsWindow) sectionLabel(c *ui.Context, key, text string) {
	ui.Text(c.Key(key), strings.ToUpper(text)).FontSize(10.5).FontWeight(600).LetterSpacing(0.7).TextColor(w.pal.faint).
		Padding(10, 4, 0, 4).SingleLine()
}

// installCard installs a plugin from a Git URL, owner/repo, or a name of
// the index.
func (w *prefsWindow) installCard(c *ui.Context) {
	p := w.pal
	pg := &w.plugins
	w.card(c, "plugins-install", func() {
		ui.Column(c).Padding(14, 16).Gap(8).Children(func() {
			ui.Text(c, locales.PluginsInstallTitle()).FontSize(13).FontWeight(500).TextColor(p.text)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				in := ui.TextInput(c.Key("plugins-source"), &pg.source).Grow(1).FontSize(12.5).
					Placeholder(locales.PluginsInstallPlaceholder()).Label(locales.PluginsInstallTitle())
				go_ := ui.PrimaryButton(c.Key("plugins-install-go"), locales.PluginsInstall()).FontSize(12).Padding(4, 12).
					Disabled(strings.TrimSpace(pg.source) == "" || pg.busy != "")
				if (go_.Clicked() || in.Submitted()) && strings.TrimSpace(pg.source) != "" && pg.busy == "" {
					w.installPlugin(strings.TrimSpace(pg.source))
				}
			})
			if pg.busy != "" {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(11, 11)
					ui.Text(c, pg.busy).FontSize(12).TextColor(p.muted)
				})
			}
			if pg.err != "" {
				ui.Text(c.Key("plugins-err"), pg.err).FontSize(12).TextColor(p.dangerFg).Selectable()
			}
		})
	})
}

// installPlugin installs a plugin in the background, as the command does.
func (w *prefsWindow) installPlugin(spec string) {
	pg := &w.plugins
	pg.busy, pg.err = locales.PluginsInstalling(spec), ""
	dir := plugin.UserDirs().Plugins
	index := pg.index
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		url, ref, byName := plugin.SplitSpec(spec)
		var err error
		if byName {
			if len(index) == 0 {
				index, err = plugin.FetchIndex(ctx, nil)
			}
			found := false
			for _, e := range index {
				if e.Name == url {
					url, found = e.Repository, true
				}
			}
			if err == nil && !found {
				err = fmt.Errorf("%s", locales.PluginsNotInIndex(url))
			}
		}
		var m *plugin.Manifest
		if err == nil {
			m, err = plugin.Install(ctx, dir, url, ref)
		}
		w.post(func() {
			pg.busy = ""
			if err != nil {
				pg.err = err.Error()
				return
			}
			pg.source = ""
			w.notes = append(w.notes, ui.Toast{ID: "installed-" + m.Name, Title: locales.PluginsInstalled(m.Name)})
			if plugins != nil {
				go plugins.reg.Load()
			}
		})
	}()
}

// post runs fn on the main thread and draws the window.
func (w *prefsWindow) post(fn func()) {
	mygo.RunOnMain(func() {
		fn()
		w.invalidate()
	})
}

// pluginRow is a plugin of the list: its name, what it is, its state, its
// switch, and its settings.
func (w *prefsWindow) pluginRow(c *ui.Context, pl *plugin.Plugin, recs plugin.Records) {
	p := w.pal
	st := pl.Stats()
	kind := ""
	src, fromGit := recs[pl.Name]
	switch {
	case pl.Bundled:
		kind = locales.PluginsBundled()
	case pl.Script:
		kind = locales.PluginScripts()
	case fromGit:
		kind = strings.TrimPrefix(strings.TrimPrefix(src.URL, "https://"), "github.com/")
	default:
		if fi, err := os.Lstat(pl.Dir); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			kind = locales.PluginsDev()
		} else {
			kind = locales.PluginsLocal()
		}
	}
	ui.Column(c.Key("plugin-row-"+pl.Name)).Padding(14, 16).Gap(8).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Gap(3).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Row(c).Gap(7).AlignItems(ui.Baseline).Children(func() {
					ui.Text(c, pl.Title()).FontSize(13).FontWeight(500).TextColor(p.text).Shrink(0)
					if pl.Version != "" {
						ui.Text(c, pl.Version).FontSize(11).TextColor(p.faint)
					}
					ui.Text(c, kind).FontSize(11).TextColor(p.faint).SingleLine().Shrink(1)
				})
				if pl.Description != "" {
					ui.Text(c, pl.Description).FontSize(12).LineHeight(1.4).TextColor(p.muted)
				}
				state := locales.PluginsIdle()
				switch {
				case st.Disabled:
					state = locales.PluginsOff()
				case st.Crashed:
					state = locales.PluginsStopped()
				case st.Active:
					state = locales.PluginsRunning(st.ActivatedIn.Round(time.Millisecond).String())
				case !pl.HasCode() && !pl.Script:
					state = locales.PluginsDeclares()
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, pl.Name).Font(w.monoFont()).FontSize(10.5).TextColor(p.faint).SingleLine().Shrink(0)
					ui.Text(c, "·").FontSize(11).TextColor(p.faint)
					ui.Text(c, state).FontSize(11).TextColor(p.faint).SingleLine().Shrink(1)
				})
				if st.Err != "" {
					ui.Text(c.Key("err"), firstLine(st.Err)).FontSize(11.5).TextColor(p.dangerFg).Selectable()
				}
			})
			if fromGit && !pl.Bundled {
				if plugins.updateWaits(pl.Name) {
					ui.Text(c.Key("update-waits"), locales.PluginsUpdateWaits()).FontSize(11.5).FontWeight(600).TextColor(p.accent)
				}
				if ui.Button(c.Key("update"), locales.PluginsUpdate()).FontSize(12).Padding(3, 10).Disabled(w.plugins.busy != "").Clicked() {
					w.updatePlugin(pl.Name)
				}
			}
			if !pl.Bundled && !pl.Script {
				if ui.Button(c.Key("remove"), locales.PluginsRemove()).FontSize(12).Padding(3, 10).Clicked() {
					name := pl.Name
					if err := plugin.Remove(plugin.UserDirs().Plugins, name); err != nil {
						w.errors = append(w.errors, err.Error())
					} else {
						go plugins.reg.Load()
					}
				}
			}
			on := !st.Disabled
			if ui.Switch(c.Key("on"), &on).Label(pl.Title()).Changed() {
				w.setPluginSetting(pl.Name, "enabled", on, on)
			}
		})
		if len(pl.Contributes.Settings) > 0 && !st.Disabled {
			w.pluginSettings(c, pl)
		}
	})
}

// updatePlugin updates a plugin installed from Git in the background.
func (w *prefsWindow) updatePlugin(name string) {
	pg := &w.plugins
	pg.busy, pg.err = locales.PluginsUpdating(name), ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		changed, err := plugin.Update(ctx, plugin.UserDirs().Plugins, name)
		w.post(func() {
			pg.busy = ""
			switch {
			case err != nil:
				pg.err = err.Error()
			case changed:
				plugins.updated(name)
				w.notes = append(w.notes, ui.Toast{ID: "updated-" + name, Title: locales.PluginsUpdated(name)})
				go plugins.reg.Load()
			default:
				w.notes = append(w.notes, ui.Toast{ID: "uptodate-" + name, Title: locales.PluginsUpToDate(name)})
			}
		})
	}()
}

// pluginSettings are the controls of a plugin's own settings.
func (w *prefsWindow) pluginSettings(c *ui.Context, pl *plugin.Plugin) {
	p := w.pal
	values := plugins.reg.Settings(pl)
	keys := make([]string, 0, len(pl.Contributes.Settings))
	for k := range pl.Contributes.Settings {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	ui.Column(c.Key("settings")).Gap(8).Padding(4, 0, 0, 12).BorderWidth(0, 0, 0, 2).BorderColor(p.border).Children(func() {
		for _, k := range keys {
			def := pl.Contributes.Settings[k]
			raw := values[k]
			ui.Row(c.Key("set-" + k)).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
					ui.Text(c, k).Font(w.monoFont()).FontSize(11.5).TextColor(p.text)
					if def.Description != "" {
						ui.Text(c, def.Description).FontSize(11.5).TextColor(p.muted)
					}
				})
				w.pluginSettingControl(c, pl.Name, k, def, raw)
			})
		}
	})
}

// pluginSettingControl edits a setting of a plugin by its type.
func (w *prefsWindow) pluginSettingControl(c *ui.Context, name, key string, def plugin.SettingContrib, raw json.RawMessage) {
	k := c.Key("control")
	switch {
	case def.Type == "boolean":
		var on bool
		json.Unmarshal(raw, &on)
		if ui.Switch(k, &on).Label(key).Changed() {
			w.setPluginSetting(name, key, on, nil)
		}
	case len(def.Enum) > 0:
		var v string
		json.Unmarshal(raw, &v)
		sel := max(slices.Index(def.Enum, v), 0)
		if n, changed := w.segmented(c, "seg-"+name+"-"+key, sel, def.Enum, key, false); changed {
			w.setPluginSetting(name, key, def.Enum[n], nil)
		}
	default:
		id := "plugin:" + name + "." + key
		var s string
		if json.Unmarshal(raw, &s) != nil {
			s = strings.TrimSpace(string(raw))
		}
		buf := w.textBuffer(id, s)
		in := ui.TextInput(k, buf).Width(200).FontSize(12.5).Label(key)
		w.draft(id, s, buf, in.Focused(), in.Submitted(), func(v any) {
			text, _ := v.(string)
			var out any = text
			if def.Type == "number" {
				var f float64
				if _, err := fmt.Sscan(text, &f); err == nil {
					out = f
				}
			}
			w.setPluginSetting(name, key, out, nil)
		})
	}
}

// setPluginSetting sets plugins.<name>.<key> in settings.json, or takes it
// away when v is deflt, as "enabled": true is.
func (w *prefsWindow) setPluginSetting(name, key string, v, deflt any) {
	prefs := w.m.s.Prefs
	all := map[string]json.RawMessage{}
	for k, raw := range prefs.Plugins {
		all[k] = raw
	}
	var obj map[string]json.RawMessage
	json.Unmarshal(all[name], &obj)
	if obj == nil {
		obj = map[string]json.RawMessage{}
	}
	if deflt != nil && v == deflt {
		delete(obj, key)
	} else {
		data, _ := json.Marshal(v)
		obj[key] = data
	}
	if len(obj) == 0 {
		delete(all, name)
	} else {
		data, _ := json.Marshal(obj)
		all[name] = data
	}
	data, _ := json.Marshal(all)
	w.set("plugins", data)
	if plugins != nil {
		plugins.syncPrefs()
		plugins.Changed()
		plugins.reg.Emit("settings.changed", map[string]any{})
	}
}

// skippedPluginKeys are the shortcuts of plugins that keys Mayoi or the
// user bind took.
func (w *prefsWindow) skippedPluginKeys() []plugin.KeyContrib {
	if len(w.m.wins) == 0 {
		return nil
	}
	return w.m.wins[0].a.skippedKeys()
}

// skippedKeyRow offers a shortcut a plugin could not take, which a click
// writes to keybindings.json, where it wins.
func (w *prefsWindow) skippedKeyRow(c *ui.Context, i int, k plugin.KeyContrib) {
	p := w.pal
	ui.Row(c.Key(fmt.Sprintf("plugins-key-%d", i))).Padding(10, 16).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, k.Command).Font(w.monoFont()).FontSize(11.5).TextColor(p.text).Grow(1).Shrink(1).SingleLine()
		ui.Text(c, k.Key).Font(w.monoFont()).FontSize(11.5).TextColor(p.muted)
		if ui.Button(c.Key("use"), locales.PluginsUseShortcut()).FontSize(12).Padding(3, 10).Clicked() {
			path := keymap.Path(w.m.s.Path())
			data, _ := os.ReadFile(path)
			if len(data) == 0 {
				data = []byte("[]\n")
			}
			out, err := keymap.Edit(data, nil, []keymap.Entry{{Key: k.Key, Command: k.Command, When: k.When}})
			if err == nil {
				err = os.WriteFile(path, out, 0o644)
			}
			if err != nil {
				w.errors = append(w.errors, err.Error())
			}
		}
	})
}

// indexCard lists the plugins of the index, read as it first shows, to
// install one.
func (w *prefsWindow) indexCard(c *ui.Context) {
	p := w.pal
	pg := &w.plugins
	w.sectionLabel(c, "plugins-index-head", locales.PluginsIndex())
	if !pg.indexOK && !pg.reading {
		pg.reading = true
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			entries, err := plugin.FetchIndex(ctx, nil)
			w.post(func() {
				pg.reading, pg.indexOK = false, true
				pg.index = entries
				if err != nil {
					pg.err = locales.PluginsIndexFailed(err.Error())
				}
			})
		}()
	}
	w.card(c, "plugins-index", func() {
		if pg.reading {
			ui.Row(c).Padding(14, 16).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(11, 11)
				ui.Text(c, locales.PluginsIndexReading()).FontSize(12).TextColor(p.muted)
			})
			return
		}
		if len(pg.index) == 0 {
			ui.Text(c, locales.PluginsIndexEmpty()).FontSize(12).TextColor(p.muted).Padding(14, 16)
			return
		}
		ui.Row(c).Padding(10, 16, 4, 16).Children(func() {
			ui.SearchField(c.Key("plugins-index-filter"), &pg.filter).Grow(1).Placeholder(locales.PluginsIndexFilter()).Label(locales.PluginsIndexFilter())
		})
		q := strings.ToLower(strings.TrimSpace(pg.filter))
		installed := map[string]bool{}
		for _, pl := range plugins.reg.Plugins() {
			installed[pl.Name] = true
		}
		for _, e := range pg.index {
			hay := strings.ToLower(e.Name + " " + e.DisplayName + " " + e.Description + " " + strings.Join(e.Tags, " "))
			if q != "" && !strings.Contains(hay, q) {
				continue
			}
			ui.Row(c.Key("index-"+e.Name)).Padding(8, 16).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
					title := e.DisplayName
					if title == "" {
						title = e.Name
					}
					ui.Text(c, title).FontSize(12.5).FontWeight(500).TextColor(p.text)
					ui.Text(c, e.Description).FontSize(11.5).TextColor(p.muted)
				})
				if installed[e.Name] {
					ui.Text(c, locales.PluginsInstalledMark()).FontSize(11.5).TextColor(p.faint)
				} else if ui.Button(c.Key("install"), locales.PluginsInstall()).FontSize(12).Padding(3, 10).Disabled(pg.busy != "").Clicked() {
					w.installPlugin(e.Repository)
				}
			})
		}
	})
}
