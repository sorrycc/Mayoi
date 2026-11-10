package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"slices"
	"strconv"
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
// is typed to install, the index as last read, the plugins as last
// listed, and the work going on.
type pluginsPage struct {
	source string
	// busy says what is being done, "" for nothing, and target is the
	// plugin it is done to, whose row shows it: "" for one typed in.
	busy, target string
	// err is what the last install, update or removal failed with, which
	// the row of errFor shows, or the install card for "".
	err, errFor string
	// confirm is the plugin whose removal waits for a second click.
	confirm string

	index    []indexItem
	indexOK  bool
	reading  bool
	indexErr string
	filter   string
	// shown are the entries of the index that match shownFor, the filter
	// they were found for.
	shown    []int
	shownFor string

	// items are the plugins as the change gen of them listed them, and
	// installed their names, which the index marks.
	items     []pluginItem
	installed map[string]bool
	gen       uint64
	listed    bool

	// detail is the plugin whose details show in place of the list, ""
	// for the list, and info what they show of it. back is set as the
	// list comes back from them, which slides it in from where they were;
	// listScroll keeps where the list was scrolled to meanwhile.
	detail     string
	info       pluginInfo
	back       bool
	listScroll ui.ScrollState
}

// pluginItem is an installed plugin as its row shows it: what is told of
// it beside its manifest, worked out as the plugins change, not as each
// frame draws.
type pluginItem struct {
	*plugin.Plugin
	initial, source string
	fromGit         bool
}

// indexItem is an entry of the index as its row shows it, and what the
// filter looks in, lowercased once.
type indexItem struct {
	plugin.IndexEntry
	title, initial, source, hay string
}

// pluginItems are the plugins installed, listed anew once they changed.
func (w *prefsWindow) pluginItems() []pluginItem {
	pg := &w.plugins
	plugins.mu.Lock()
	gen := plugins.gen
	plugins.mu.Unlock()
	if pg.listed && pg.gen == gen {
		return pg.items
	}
	list := plugins.reg.Plugins()
	pg.items, pg.installed = make([]pluginItem, len(list)), make(map[string]bool, len(list))
	for i, pl := range list {
		o := plugins.reg.Origin(pl.Name)
		it := pluginItem{Plugin: pl, initial: initial(pl.Title()), fromGit: o.FromGit}
		switch {
		case pl.Bundled:
			it.source = locales.PluginsBundled()
		case pl.Script:
			it.source = locales.PluginScripts()
		case o.FromGit:
			it.source = repoName(o.URL)
		case o.Linked:
			it.source = locales.PluginsDev()
		default:
			it.source = locales.PluginsLocal()
		}
		pg.items[i] = it
		pg.installed[pl.Name] = true
	}
	pg.gen, pg.listed = gen, true
	return pg.items
}

// initial is the first letter of a name, as its tile shows it.
func initial(name string) string {
	for _, r := range name {
		return strings.ToUpper(string(r))
	}
	return "?"
}

// repoName is a Git URL as short as it reads: owner/repo of GitHub.
func repoName(url string) string {
	url = strings.TrimSuffix(strings.TrimPrefix(url, "https://"), ".git")
	return strings.TrimPrefix(url, "github.com/")
}

// pluginRowMotion is how a row of a plugin comes and goes, as one is
// installed or removed: it opens and closes rather than jumps.
var pluginRowMotion = ui.ElementTransition{Duration: 160 * time.Millisecond, Colors: true,
	Enter: &ui.Motion{Collapse: true}, Exit: &ui.Motion{Collapse: true}}

// pluginsPane is the Plugins page: the plugins installed, bundled and
// scripts, each with its switch and the way to its details; the
// shortcuts plugins could not take; and installing from the index or Git.
func (w *prefsWindow) pluginsPane(c *ui.Context) {
	p := w.pal
	pg := &w.plugins
	if pg.detail != "" && plugins != nil {
		items := w.pluginItems()
		for i := range items {
			if items[i].Name == pg.detail {
				w.pluginDetail(c, &items[i])
				return
			}
		}
		// Removed while its details showed.
		w.closeDetail()
	}
	w.head(c, "prefs-head", iconPlug, prefSection(settings.CatPlugins), func() {
		if ui.Button(c.Key("plugins-reload"), locales.CmdPluginsReload()).FontSize(12).Padding(3, 10).Clicked() && plugins != nil {
			go plugins.reg.ReloadAll()
		}
		if ui.Button(c.Key("plugins-folder"), locales.CmdPluginsOpenFolder()).FontSize(12).Padding(3, 10).Clicked() {
			w.inFront(func(a *app) { a.openPluginsFolder() })
		}
	})
	ui.Scroll(c.Key("prefs-rows-plugins")).Grow(1).MinHeight(0).TrackScroll(&pg.listScroll).Children(func() {
		page := ui.Column(c.Key("prefs-page-plugins")).Padding(20, 28, 32, 28).Gap(10).AlignSelf(ui.Center).
			WidthPercent(100).MaxWidth(prefsPageWidth)
		if pg.back {
			slideIn(c, page, -pageSlide)
		} else {
			fadePage(page)
		}
		page.Children(func() {
			if plugins == nil {
				w.emptyState(c, "plugins-none", iconPlug, locales.PluginsNotStarted())
				return
			}
			if plugins.reg.Safe() {
				w.note(c, "plugins-safe", iconTriangleAlert, p.modifiedFg, locales.PluginsSafeMode(), nil)
			}
			items := w.pluginItems()
			w.sectionLabel(c, "plugins-installed-head", locales.PluginsInstalledMark(), len(items))
			w.card(c, "plugins-list", func() {
				if len(items) == 0 {
					w.emptyState(c, "plugins-none", iconPlug, locales.PluginsNone())
				}
				for i := range items {
					w.pluginRow(c, &items[i], i > 0)
				}
			})
			if skipped := w.skippedPluginKeys(); len(skipped) > 0 {
				w.sectionLabel(c, "plugins-keys-head", locales.PluginsSkippedKeys(), 0)
				w.card(c, "plugins-keys", func() {
					for i, k := range skipped {
						w.skippedKeyRow(c, i, k)
					}
				})
			}
			w.indexCard(c)
			w.installCard(c)
			w.sectionLabel(c, "plugins-settings-head", locales.PluginsMayoiSettings(), 0)
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

// sectionLabel is the label above a card of the Plugins page, with how
// many the card lists when n is more than 0.
func (w *prefsWindow) sectionLabel(c *ui.Context, key, text string, n int) {
	p := w.pal
	ui.Row(c.Key(key)).Padding(10, 4, 0, 4).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, strings.ToUpper(text)).FontSize(10.5).FontWeight(600).LetterSpacing(0.7).TextColor(p.faint).SingleLine()
		if n > 0 {
			ui.Text(c.Key("count"), strconv.Itoa(n)).FontSize(10).FontWeight(600).TextColor(p.muted).Padding(0, 6).Radius(8).
				MinWidth(18).TextAlign(ui.Center).Background(p.text.Alpha(0.07)).ContentTransition(ui.ContentTransition{Numeric: true})
		}
	})
}

// emptyState says a card has nothing to list, under a badge of icon.
func (w *prefsWindow) emptyState(c *ui.Context, key string, icon *ui.SVG, text string) {
	p := w.pal
	ui.Column(c.Key(key)).Padding(28, 24).Gap(10).AlignItems(ui.Center).Children(func() {
		popUp(ui.Box(c.Key("badge")).Size(40, 40).Radius(20).Background(p.text.Alpha(0.06)).Justify(ui.Center).AlignItems(ui.Center)).Children(func() {
			ui.Icon(c, icon).FontSize(17).TextColor(p.muted)
		})
		ui.Text(c, text).FontSize(12.5).LineHeight(1.4).TextColor(p.muted).TextAlign(ui.Center).MaxWidth(420)
	})
}

// note is a line that stands out of the page, in col, as a warning or an
// error does: the text can be chosen to copy, and dismiss, when set, is
// what its close button does.
func (w *prefsWindow) note(c *ui.Context, key string, icon *ui.SVG, col ui.Color, text string, dismiss func()) ui.Element {
	p := w.pal
	return ui.Row(c.Key(key)).Padding(8, 8, 8, 12).Gap(9).Radius(8).AlignItems(ui.Start).Background(col.Alpha(0.08)).
		Border(1, col.Alpha(0.28)).Transition(popIn).Children(func() {
		ui.Icon(c, icon).FontSize(13).TextColor(col).Shrink(0).Margin(2, 0, 0, 0)
		ui.Text(c, text).FontSize(12).LineHeight(1.4).TextColor(p.text).Grow(1).Shrink(1).MinWidth(0).Selectable().Margin(1, 0, 0, 0)
		if dismiss == nil {
			return
		}
		x := ui.ButtonBase(c.Key("dismiss")).Size(20, 20).Radius(5).AlignItems(ui.Center).Justify(ui.Center).Shrink(0).
			Label(locales.PrefsDismiss()).Tooltip(locales.PrefsDismiss())
		if x.Hovered() {
			x.Background(col.Alpha(0.14))
		}
		x.Transition(hoverFade)
		pressPose(x)
		x.Children(func() { ui.Icon(c, iconX).FontSize(10.5).TextColor(p.muted) })
		if x.Clicked() {
			dismiss()
		}
	})
}

// errNote shows what the work on the plugin of target failed with, if it
// did, with the button that dismisses it, indent in from the start.
func (w *prefsWindow) errNote(c *ui.Context, target string, indent float32) {
	pg := &w.plugins
	if pg.err == "" || pg.errFor != target {
		return
	}
	w.note(c, "plugins-err", iconOctagonAlert, w.pal.dangerFg, pg.err, func() { pg.err, pg.errFor = "", "" }).Margin(0, 0, 0, indent)
}

// busyLine shows what is being done to the plugin of target, if it is,
// indent in from the start.
func (w *prefsWindow) busyLine(c *ui.Context, target string, indent float32) {
	pg := &w.plugins
	if pg.busy == "" || pg.target != target {
		return
	}
	ui.Row(c.Key("plugins-busy")).Gap(8).AlignItems(ui.Center).Margin(0, 0, 0, indent).Transition(revealFade).Children(func() {
		ui.Spinner(c).Size(11, 11)
		ui.Text(c, pg.busy).FontSize(12).TextColor(w.pal.muted).SingleLine().Shrink(1)
	})
}

// tileIndent is how far the text of a row of a plugin starts from its
// tile's start, where what is under the text lines up.
const tileIndent = 34 + 12

// tile is the square of a plugin's initial at the start of its row, in
// the accent while it is on.
func (w *prefsWindow) tile(c *ui.Context, letter string, icon *ui.SVG, on bool) {
	w.tileOf(c, letter, icon, on, 34)
}

// tileOf is a plugin's tile of side size, its corners and letter in
// proportion.
func (w *prefsWindow) tileOf(c *ui.Context, letter string, icon *ui.SVG, on bool, size float32) {
	p := w.pal
	bg, fg := p.accent.Alpha(0.13), p.accent
	if !on {
		bg, fg = p.text.Alpha(0.06), p.muted
	}
	glyph := size * 0.44
	ui.Box(c.Key("tile")).Size(size, size).Radius(size * 0.265).Shrink(0).Justify(ui.Center).AlignItems(ui.Center).Background(bg).
		Transition(hoverFade).Children(func() {
		if icon != nil {
			ui.Icon(c.Key("glyph"), icon).FontSize(glyph).TextColor(fg).Transition(hoverFade)
		} else {
			ui.Text(c.Key("glyph"), letter).FontSize(glyph).FontWeight(600).TextColor(fg).Transition(hoverFade)
		}
	})
}

// pill is a short word beside a plugin's name, as its version, in col.
func (w *prefsWindow) pill(c *ui.Context, key, text string, col ui.Color, mono bool) {
	t := ui.Text(c.Key(key), text).FontSize(10.5).TextColor(col).Padding(0, 6).Radius(5).Background(col.Alpha(0.12)).
		SingleLine().Shrink(0)
	if mono {
		t.Font(w.monoFont())
	} else {
		t.FontWeight(600)
	}
}

// rowHover gives a row of a card the pointer's tint, and the rule above
// it, inset, unless it is the first.
func (w *prefsWindow) rowHover(row ui.Element, rule bool) {
	p := w.pal
	if row.Hovered() {
		row.Background(p.text.Alpha(0.035))
	}
	if rule {
		border := p.border
		row.Draw(func(pt *ui.Painter, r ui.Rect) {
			pt.Fill(ui.Rect{X: r.X + 16, Y: r.Y, W: r.W - 32, H: 1}, border, 0)
		})
	}
}

// iconButton is a small button of a row that shows only its icon, which
// tints with col under the pointer.
func (w *prefsWindow) iconButton(c *ui.Context, key string, icon *ui.SVG, label string, col ui.Color, disabled bool) bool {
	p := w.pal
	b := ui.ButtonBase(c.Key(key)).Size(26, 26).Radius(6).AlignItems(ui.Center).Justify(ui.Center).Shrink(0).
		Label(label).Tooltip(label).Disabled(disabled)
	fg := p.muted
	switch {
	case disabled:
		fg = p.faint.Alpha(0.5)
	case b.Hovered():
		b.Background(col.Alpha(0.12))
		fg = col
	}
	b.Transition(hoverFade)
	pressPose(b)
	b.Children(func() { ui.Icon(c.Key("icon"), icon).FontSize(13).TextColor(fg).Transition(hoverFade) })
	return b.Clicked() && !disabled
}

// primaryButton is a button in the accent, of what a card is for, as
// Install: disabled, it lays its accent aside for a quiet face, rather
// than show it dimmed, which reads as all but enabled on a dark window.
func (w *prefsWindow) primaryButton(c *ui.Context, key, label string, disabled bool) ui.Element {
	p := w.pal
	b := ui.PrimaryButton(c.Key(key), label).FontSize(12).Disabled(disabled)
	if disabled {
		b.Background(p.text.Alpha(0.06)).TextColor(p.faint).Border(1, p.border)
	}
	return b.Transition(hoverFade)
}

// dangerButton is a button that loses something, as a removal's confirm.
func (w *prefsWindow) dangerButton(c *ui.Context, key, label string) bool {
	p := w.pal
	b := ui.PrimaryButton(c.Key(key), label).FontSize(12).Padding(3, 10)
	bg := p.dangerFill
	switch {
	case b.Pressed():
		bg = p.dangerPressed
	case b.Hovered():
		bg = p.dangerHover
	}
	b.Background(bg).TextColor(p.dangerText).Transition(hoverFade)
	return b.Clicked()
}

// pluginState is what a plugin's row says of how it is doing, and the
// color of the dot before it.
func (w *prefsWindow) pluginState(st plugin.Stats) (string, ui.Color) {
	return pluginStateOf(w.pal, st)
}

// pluginStateOf is how a plugin is doing, in words, and the color of its
// dot.
func pluginStateOf(p *palette, st plugin.Stats) (string, ui.Color) {
	switch st.State {
	case plugin.StateOff:
		return locales.PluginsOff(), p.faint
	case plugin.StateCrashed:
		return locales.PluginsStopped(), p.dangerFg
	case plugin.StateRunning:
		return locales.PluginsRunning(st.ActivatedIn.Round(time.Millisecond).String()), p.addedFg
	case plugin.StateStatic:
		return locales.PluginsDeclares(), p.accent
	}
	// Starting, or waiting for its first call: in trouble when it has
	// failed, or cannot be read.
	if st.Err != "" {
		return locales.PluginsIdle(), p.dangerFg
	}
	return locales.PluginsIdle(), p.faint
}

// pluginRow is a plugin of the list: its tile, name, version and what it
// is; how it is doing and where it came from; its buttons and switch; and
// the way to its settings. rule draws the line above it.
func (w *prefsWindow) pluginRow(c *ui.Context, it *pluginItem, rule bool) {
	p := w.pal
	pg := &w.plugins
	pl := it.Plugin
	st := pl.Stats()
	on := !st.Disabled
	busy := pg.busy != ""
	row := ui.Column(c.Key("plugin-row-"+pl.Name)).Padding(14, 16).Gap(10).Transition(pluginRowMotion)
	w.rowHover(row, rule)
	row.Children(func() {
		// The head of the row opens the plugin's details; its buttons and
		// switch take their own clicks.
		head := ui.Row(c.Key("head")).Gap(12).AlignItems(ui.Start).Focusable().Label(locales.PluginsDetails(pl.Title()))
		if head.Clicked() {
			w.openDetail(pl.Name)
		}
		hovered := row.Hovered()
		head.Children(func() {
			var glyph *ui.SVG
			if pl.Script {
				glyph = iconFileCode
			}
			w.tile(c, it.initial, glyph, on)
			ui.Column(c).Gap(3).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
					title := p.text
					if !on {
						title = p.muted
					}
					ui.Text(c.Key("title"), pl.Title()).FontSize(13).FontWeight(600).TextColor(title).Shrink(1).SingleLine().
						Transition(hoverFade)
					if pl.Version != "" {
						w.pill(c, "version", pl.Version, p.muted, true)
					}
					if it.fromGit && plugins.updateWaits(pl.Name) {
						w.pill(c, "update-waits", locales.PluginsUpdateWaits(), p.accent, false)
					}
				})
				if pl.Description != "" {
					ui.Text(c, pl.Description).FontSize(12).LineHeight(1.4).TextColor(p.muted)
				}
				state, dot := w.pluginState(st)
				ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(1, 0, 0, 0).Children(func() {
					ui.Box(c.Key("dot")).Size(6, 6).Radius(3).Shrink(0).Background(dot).Transition(hoverFade)
					ui.Text(c, state).FontSize(11).TextColor(p.muted).SingleLine().Shrink(0)
					ui.Text(c, "·").FontSize(11).TextColor(p.faint)
					ui.Text(c, pl.Name).Font(w.monoFont()).FontSize(10.5).TextColor(p.faint).SingleLine().Shrink(0)
					ui.Text(c, "·").FontSize(11).TextColor(p.faint)
					ui.Text(c, it.source).FontSize(11).TextColor(p.faint).SingleLine().Shrink(1).MinWidth(0)
					if pl.Author != "" {
						ui.Text(c, "·").FontSize(11).TextColor(p.faint)
						ui.Text(c, locales.PluginsBy(pl.Author)).FontSize(11).TextColor(p.faint).SingleLine().Shrink(1).MinWidth(0)
					}
				})
			})
			ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).MinHeight(34).Children(func() {
				if it.fromGit && !pl.Bundled {
					if ui.Button(c.Key("update"), locales.PluginsUpdate()).FontSize(12).Padding(3, 10).Disabled(busy).Clicked() {
						w.updatePlugin(pl.Name)
					}
				}
				if !pl.Bundled && !pl.Script {
					if w.iconButton(c, "remove", iconTrash, locales.PluginsRemove(), p.dangerFg, busy) {
						pg.confirm = pl.Name
					}
				}
				if w.toggle(c.Key("on"), &on).Label(pl.Title()).Changed() {
					w.setPluginSetting(pl.Name, "enabled", on, pl.DefaultOn())
				}
				// The way into the details, which leans toward them under
				// the pointer.
				col, pose := p.faint, ui.Pose{}
				if hovered {
					col, pose.X = p.muted, 2
				}
				ui.Icon(c.Key("more"), iconChevronRight).FontSize(13).TextColor(col).Margin(0, 0, 0, 2).Transition(hoverFade).
					AnimatePose(pose, ui.Snappy)
			})
		})
		if st.Err != "" {
			w.note(c, "plugin-err", iconOctagonAlert, p.dangerFg, firstLine(st.Err), nil).Margin(0, 0, 0, tileIndent)
		}
		w.busyLine(c, pl.Name, tileIndent)
		w.errNote(c, pl.Name, tileIndent)
		if pg.confirm == pl.Name {
			w.confirmRemove(c, pl, tileIndent)
		}
		if n := len(pl.Contributes.Settings); n > 0 {
			w.settingsLink(c, pl, n)
		}
	})
}

// settingsLink says how many settings a plugin has, under its row, and
// opens its details, where they are set.
func (w *prefsWindow) settingsLink(c *ui.Context, pl *plugin.Plugin, n int) {
	p := w.pal
	b := ui.ButtonBase(c.Key("settings-link")).Margin(0, 0, 0, tileIndent-6).Padding(3, 6).Gap(6).Radius(6).AlignSelf(ui.Start).
		AlignItems(ui.Center).Label(locales.PluginsDetails(pl.Title()))
	col, pose := p.muted, ui.Pose{}
	if b.Hovered() {
		b.Background(p.hover)
		col, pose.X = p.text, 2
	}
	b.Transition(hoverFade)
	pressPose(b)
	b.Children(func() {
		ui.Icon(c, iconSettings).FontSize(12).TextColor(p.faint)
		ui.Text(c.Key("label"), locales.PluginsSettingsCount(n)).FontSize(11.5).TextColor(col).SingleLine().Transition(hoverFade)
		ui.Icon(c.Key("go"), iconChevronRight).FontSize(10.5).TextColor(p.faint).AnimatePose(pose, ui.Snappy)
	})
	if b.Clicked() {
		w.openDetail(pl.Name)
	}
}

// confirmRemove asks to remove a plugin, as its button was clicked,
// indent in from the start.
func (w *prefsWindow) confirmRemove(c *ui.Context, pl *plugin.Plugin, indent float32) {
	p := w.pal
	pg := &w.plugins
	ui.Row(c.Key("confirm-remove")).Padding(8, 8, 8, 12).Margin(0, 0, 0, indent).Gap(8).Radius(8).AlignItems(ui.Center).
		Background(p.dangerFg.Alpha(0.08)).Border(1, p.dangerFg.Alpha(0.28)).Transition(popIn).Children(func() {
		ui.Icon(c, iconTrash).FontSize(13).TextColor(p.dangerFg).Shrink(0)
		ui.Text(c, locales.PluginsRemoveAsk(pl.Title())).FontSize(12).TextColor(p.text).Grow(1).Shrink(1).MinWidth(0)
		if ui.Button(c.Key("cancel"), locales.ButtonCancel()).FontSize(12).Padding(3, 10).Clicked() {
			pg.confirm = ""
		}
		if w.dangerButton(c, "remove-yes", locales.PluginsRemove()) {
			pg.confirm = ""
			w.removePlugin(pl.Name)
		}
	})
}

// removePlugin removes a plugin's folder in the background.
func (w *prefsWindow) removePlugin(name string) {
	pg := &w.plugins
	pg.busy, pg.target, pg.err, pg.errFor = locales.PluginsRemoving(name), name, "", ""
	go func() {
		err := plugin.Remove(plugin.UserDirs().Plugins, name)
		w.post(func() {
			pg.busy, pg.target = "", ""
			if err != nil {
				pg.err, pg.errFor = err.Error(), name
				return
			}
			w.notes = append(w.notes, ui.Toast{ID: "removed-" + name, Title: locales.PluginsRemoved(name)})
			go plugins.reg.Load()
		})
	}()
}

// installCard installs a plugin from a Git URL, owner/repo, or a name of
// the index.
func (w *prefsWindow) installCard(c *ui.Context) {
	p := w.pal
	pg := &w.plugins
	w.card(c, "plugins-install", func() {
		ui.Column(c).Padding(14, 16).Gap(10).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconDownload).FontSize(13).TextColor(p.muted).Shrink(0)
				ui.Text(c, locales.PluginsInstallTitle()).FontSize(13).FontWeight(500).TextColor(p.text)
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ready := strings.TrimSpace(pg.source) != "" && pg.busy == ""
				in := ui.TextInput(c.Key("plugins-source"), &pg.source).Grow(1).FontSize(12.5).
					Placeholder(locales.PluginsInstallPlaceholder()).Label(locales.PluginsInstallTitle())
				go_ := w.primaryButton(c, "plugins-install-go", locales.PluginsInstall(), !ready).Padding(4, 12)
				pressPose(go_)
				if (go_.Clicked() || in.Submitted()) && ready {
					w.installPlugin(strings.TrimSpace(pg.source), "")
				}
			})
			w.busyLine(c, "", 0)
			w.errNote(c, "", 0)
		})
	})
}

// installPlugin installs a plugin in the background, as the command does;
// target is the entry of the index whose row shows it, "" for one typed.
func (w *prefsWindow) installPlugin(spec, target string) {
	pg := &w.plugins
	pg.busy, pg.target, pg.err, pg.errFor = locales.PluginsInstalling(spec), target, "", ""
	dir := plugin.UserDirs().Plugins
	index := make([]plugin.IndexEntry, len(pg.index))
	for i, e := range pg.index {
		index[i] = e.IndexEntry
	}
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
			pg.busy, pg.target = "", ""
			if err != nil {
				pg.err, pg.errFor = err.Error(), target
				return
			}
			if target == "" {
				pg.source = ""
			}
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

// updatePlugin updates a plugin installed from Git in the background.
func (w *prefsWindow) updatePlugin(name string) {
	pg := &w.plugins
	pg.busy, pg.target, pg.err, pg.errFor = locales.PluginsUpdating(name), name, "", ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		changed, err := plugin.Update(ctx, plugin.UserDirs().Plugins, name)
		w.post(func() {
			pg.busy, pg.target = "", ""
			switch {
			case err != nil:
				pg.err, pg.errFor = err.Error(), name
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

// pluginSettingControl edits a setting of a plugin by its type.
func (w *prefsWindow) pluginSettingControl(c *ui.Context, name, key string, def plugin.SettingContrib, raw json.RawMessage) {
	k := c.Key("control")
	switch {
	case def.Type == "boolean":
		var on bool
		json.Unmarshal(raw, &on)
		if w.toggle(k, &on).Label(key).Changed() {
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
// away when v is deflt, as "enabled" is when it matches the plugin's default.
func (w *prefsWindow) setPluginSetting(name, key string, v, deflt any) {
	w.set("plugins", withPluginSetting(w.m.s.Prefs.Plugins, name, key, v, deflt))
	if plugins != nil {
		plugins.syncPrefs()
		plugins.Changed()
		plugins.reg.Emit("settings.changed", map[string]any{})
	}
}

// withPluginSetting is the plugins' settings all, with the setting of key
// of the plugin of name v, or left out as deflt.
func withPluginSetting(all map[string]json.RawMessage, name, key string, v, deflt any) json.RawMessage {
	all = maps.Clone(all)
	if all == nil {
		all = map[string]json.RawMessage{}
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
	return data
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
	row := ui.Row(c.Key(fmt.Sprintf("plugins-key-%d", i))).Padding(10, 16).Gap(10).AlignItems(ui.Center)
	w.rowHover(row, i > 0)
	row.Children(func() {
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
// install one: placeholders while it is read, what reading it failed with
// and a button to try again, or what the filter finds of it.
func (w *prefsWindow) indexCard(c *ui.Context) {
	p := w.pal
	pg := &w.plugins
	if !pg.indexOK && !pg.reading {
		w.readIndex()
	}
	w.sectionLabel(c, "plugins-index-head", locales.PluginsIndex(), len(pg.index))
	w.card(c, "plugins-index", func() {
		switch {
		case pg.reading:
			w.indexPlaceholders(c)
			return
		case pg.indexErr != "":
			ui.Column(c.Key("index-failed")).Padding(14, 16).Gap(10).AlignItems(ui.Start).Children(func() {
				w.note(c, "index-err", iconCloudAlert, p.dangerFg, pg.indexErr, nil)
				if ui.Button(c.Key("index-retry"), locales.PluginsIndexRetry()).FontSize(12).Padding(3, 10).Clicked() {
					w.readIndex()
				}
			})
			return
		case len(pg.index) == 0:
			w.emptyState(c, "index-empty", iconBox, locales.PluginsIndexEmpty())
			return
		}
		ui.Row(c).Padding(12, 16, 6, 16).Children(func() {
			ui.SearchField(c.Key("plugins-index-filter"), &pg.filter).Grow(1).Placeholder(locales.PluginsIndexFilter()).Label(locales.PluginsIndexFilter())
		})
		shown := w.indexShown()
		fadeIn(ui.Column(c.Key("index-list"))).Children(func() {
			if len(shown) == 0 {
				ui.Text(c.Key("index-none"), locales.PluginsIndexNoMatch(strings.TrimSpace(pg.filter))).FontSize(12).
					TextColor(p.muted).Padding(18, 16, 22, 16).AlignSelf(ui.Center).Transition(revealFade)
			}
			for i, n := range shown {
				w.indexRow(c, &pg.index[n], i > 0)
			}
		})
	})
}

// readIndex reads the index in the background.
func (w *prefsWindow) readIndex() {
	pg := &w.plugins
	pg.reading, pg.indexErr = true, ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		entries, err := plugin.FetchIndex(ctx, nil)
		items := indexItems(entries)
		w.post(func() {
			pg.reading, pg.indexOK = false, true
			pg.index, pg.shownFor, pg.shown = items, "", nil
			if err != nil {
				pg.indexErr = locales.PluginsIndexFailed(err.Error())
			}
		})
	}()
}

// indexItems are the entries of the index as their rows show them.
func indexItems(entries []plugin.IndexEntry) []indexItem {
	items := make([]indexItem, len(entries))
	for i, e := range entries {
		title := e.DisplayName
		if title == "" {
			title = e.Name
		}
		items[i] = indexItem{IndexEntry: e, title: title, initial: initial(title), source: repoName(e.Repository),
			hay: strings.ToLower(e.Name + " " + e.DisplayName + " " + e.Description + " " + e.Author + " " + strings.Join(e.Tags, " "))}
	}
	return items
}

// indexShown are the entries of the index the filter finds, found anew
// as it changes, not as each frame draws.
func (w *prefsWindow) indexShown() []int {
	pg := &w.plugins
	q := strings.ToLower(strings.TrimSpace(pg.filter))
	if pg.shown != nil && pg.shownFor == q {
		return pg.shown
	}
	pg.shown, pg.shownFor = pg.shown[:0], q
	if pg.shown == nil {
		pg.shown = make([]int, 0, len(pg.index))
	}
	for i := range pg.index {
		if q == "" || strings.Contains(pg.index[i].hay, q) {
			pg.shown = append(pg.shown, i)
		}
	}
	return pg.shown
}

// indexRow is an entry of the index: its tile, name, what it does and
// where it is, and Install, or a mark once it is installed.
func (w *prefsWindow) indexRow(c *ui.Context, e *indexItem, rule bool) {
	p := w.pal
	pg := &w.plugins
	installed := pg.installed[e.Name]
	row := ui.Column(c.Key("index-"+e.Name)).Padding(10, 16).Gap(8)
	w.rowHover(row, rule)
	row.Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			w.tile(c, e.initial, nil, installed)
			ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Text(c, e.title).FontSize(12.5).FontWeight(600).TextColor(p.text).SingleLine()
				if e.Description != "" {
					ui.Text(c, e.Description).FontSize(12).LineHeight(1.4).TextColor(p.muted)
				}
				ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(1, 0, 0, 0).Children(func() {
					ui.Text(c, e.source).FontSize(11).TextColor(p.faint).SingleLine().Shrink(1).MinWidth(0)
					if e.Author != "" {
						ui.Text(c, "·").FontSize(11).TextColor(p.faint)
						ui.Text(c, locales.PluginsBy(e.Author)).FontSize(11).TextColor(p.faint).SingleLine().Shrink(1).MinWidth(0)
					}
					for _, tag := range e.Tags {
						w.pill(c, "tag-"+tag, tag, p.muted, false)
					}
				})
			})
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(func() {
				switch {
				case pg.busy != "" && pg.target == e.Name:
					ui.Spinner(c.Key("installing")).Size(13, 13).Margin(0, 8).Label(pg.busy)
				case installed:
					ui.Row(c.Key("installed")).Gap(5).AlignItems(ui.Center).Transition(revealFade).Children(func() {
						ui.Icon(c, iconCheck).FontSize(12).TextColor(p.addedFg)
						ui.Text(c, locales.PluginsInstalledMark()).FontSize(11.5).TextColor(p.muted)
					})
				default:
					b := ui.Button(c.Key("install"), locales.PluginsInstall()).FontSize(12).Padding(3, 10).Disabled(pg.busy != "")
					pressPose(b)
					if b.Clicked() {
						w.installPlugin(e.Repository, e.Name)
					}
				}
			})
		})
		w.errNote(c, e.Name, tileIndent)
	})
}

// indexPlaceholders stand where the index's rows will, while it is read:
// a tile and the lines of a row, which glow and fade, or keep still with
// less motion.
func (w *prefsWindow) indexPlaceholders(c *ui.Context) {
	p := w.pal
	glow := float32(0.5)
	if !c.Preferences().ReduceMotion {
		t := float64(c.Now().UnixMilli()%1400) / 1400
		glow = float32(0.5 - 0.5*math.Cos(2*math.Pi*t))
		c.AnimationFrame()
	}
	col := p.text.Alpha(0.05 + 0.04*glow)
	ui.Column(c.Key("index-reading")).Label(locales.PluginsIndexReading()).Transition(revealFade).Children(func() {
		for i, width := range []float32{0.42, 0.56, 0.34} {
			ui.Box(c.Key(fmt.Sprintf("placeholder-%d", i))).Height(62).Draw(func(pt *ui.Painter, r ui.Rect) {
				if i > 0 {
					pt.Fill(ui.Rect{X: r.X + 16, Y: r.Y, W: r.W - 32, H: 1}, p.border, 0)
				}
				pt.Fill(ui.Rect{X: r.X + 16, Y: r.Y + 14, W: 34, H: 34}, col, 9)
				x, span := r.X+62, r.W-62-16
				pt.Fill(ui.Rect{X: x, Y: r.Y + 17, W: span * width, H: 10}, col, 5)
				pt.Fill(ui.Rect{X: x, Y: r.Y + 35, W: span * (width + 0.3), H: 8}, col, 4)
			})
		}
	})
}
