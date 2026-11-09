package main

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/plugin"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// The details of a plugin, which open in place of the Plugins page's list
// as its row is clicked: its head, how it is doing, what it adds to
// Mayoi, its settings, and what it logged last.

// pluginInfo is what the details of a plugin show of it: what it adds,
// worked out as they open or the plugins change, and how it is doing,
// read again every pluginHealthEvery while they show, not as each frame
// draws.
type pluginInfo struct {
	name     string
	gen      uint64
	groups   []contribGroup
	settings []string

	// at is when the health and the log were read last.
	at                         time.Time
	mem, cpu, started, crashes string
	// log is the plugin's last lines as they show, of logN lines in all,
	// the last at logLast, in the colors of logPal; follow scrolls to the
	// end as new lines come while the end shows.
	log        []ui.Span
	logN       int
	logLast    time.Time
	logPal     *palette
	logScroll  ui.ScrollState
	follow     bool
	logReadFor string
}

// contribGroup is a kind of what a plugin adds, as its commands, and
// each it adds of it.
type contribGroup struct {
	key, title string
	icon       *ui.SVG
	rows       []contribRow
}

// contribRow is a thing a plugin adds: its name, what tells it apart, in
// the code's font when mono, and its keys, as caps, or a word of it.
type contribRow struct {
	title, detail string
	mono          bool
	caps          []string
	tag           string
}

// pluginHealthEvery is how often the details read how the plugin is
// doing, while they show: often enough to watch it, seldom enough to
// cost nothing.
const pluginHealthEvery = time.Second

// pluginLogLines is how many of its last lines the details of a plugin
// show of its log.
const pluginLogLines = 40

// pageSlide is how far a page of the Plugins page slides in from as it
// shows: the details from the end, the list back from the start.
const pageSlide = 20

// slideIn brings a page in from dx along the line as it fades in, the
// other way right to left.
func slideIn(c *ui.Context, e ui.Element, dx float32) ui.Element {
	if c.Direction() == ui.RightToLeft {
		dx = -dx
	}
	k := e.Keyframes("show", []ui.Keyframe{{Value: 0}, {At: 180 * time.Millisecond, Value: 1, Ease: ui.EaseOut}})
	return e.Opacity(k).Translate(dx*(1-k), 0)
}

// openDetail shows the details of the plugin of name in place of the
// list.
func (w *prefsWindow) openDetail(name string) {
	pg := &w.plugins
	pg.detail, pg.back, pg.confirm = name, false, ""
	pg.info = pluginInfo{}
	w.invalidate()
}

// closeDetail goes back from the details to the list, where it was.
func (w *prefsWindow) closeDetail() {
	pg := &w.plugins
	pg.detail, pg.back, pg.confirm = "", true, ""
	pg.info = pluginInfo{}
	w.invalidate()
}

// detailInfo is what the details of it show, worked out anew as they
// open and as the plugins change.
func (w *prefsWindow) detailInfo(it *pluginItem) *pluginInfo {
	pg := &w.plugins
	d := &pg.info
	if d.name == it.Name && d.gen == pg.gen {
		return d
	}
	keys := make([]string, 0, len(it.Contributes.Settings))
	for k := range it.Contributes.Settings {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	*d = pluginInfo{name: it.Name, gen: pg.gen, groups: contribGroups(it.Plugin, i18n.Locale()), settings: keys, follow: true}
	return d
}

// contribGroups are what a plugin adds, by kind, in the language of tag:
// the kinds it adds none of left out.
func contribGroups(pl *plugin.Plugin, tag string) []contribGroup {
	ct := &pl.Contributes
	var out []contribGroup
	add := func(key, title string, icon *ui.SVG, rows []contribRow) {
		if len(rows) > 0 {
			out = append(out, contribGroup{key, title, icon, rows})
		}
	}
	titles := map[string]string{}
	var rows []contribRow
	for _, cmd := range ct.Commands {
		title := cmd.TitleIn(tag)
		titles[cmd.ID] = title
		r := contribRow{title: title, detail: cmd.ID, mono: false}
		for _, k := range ct.Keybindings {
			if st, ok := pluginStroke(k); ok && k.Command == cmd.ID {
				r.caps = accelCaps(st.Accel(), macKeys())
				break
			}
		}
		rows = append(rows, r)
	}
	add("commands", locales.PluginsCommands(), iconCommand, rows)

	rows = nil
	for _, where := range slices.Sorted(func(yield func(string) bool) {
		for k := range ct.Menus {
			if !yield(k) {
				return
			}
		}
	}) {
		for _, mc := range ct.Menus[where] {
			title := titles[mc.Command]
			if title == "" {
				title = mc.Command
			}
			rows = append(rows, contribRow{title: title, detail: where})
		}
	}
	add("menus", locales.PluginsMenus(), iconMenu, rows)

	rows = nil
	for _, si := range ct.StatusItems {
		align := si.Align
		if align == "" {
			align = "right"
		}
		rows = append(rows, contribRow{title: si.ID, mono: true, tag: align})
	}
	add("status", locales.PluginsStatusItems(), iconRows, rows)

	rows = nil
	for _, v := range ct.Views {
		where := v.Location
		if where == "" {
			where = "sidebar"
		}
		rows = append(rows, contribRow{title: v.Title, detail: v.ID, tag: where})
	}
	add("views", locales.PluginsViews(), iconSidebar, rows)

	rows = nil
	for _, th := range ct.Themes {
		r := contribRow{title: th.Label, detail: th.ID}
		if th.Dark {
			r.tag = prefOption("appearance", "dark")
		}
		rows = append(rows, r)
	}
	add("themes", locales.PluginsThemes(), iconPalette, rows)

	rows = nil
	for _, l := range ct.Languages {
		rows = append(rows, contribRow{title: l.ID, mono: true, detail: strings.Join(append(slices.Clone(l.Extensions), l.Filenames...), "  ")})
	}
	add("languages", locales.PluginsLanguages(), iconBraces, rows)

	rows = nil
	for _, sn := range ct.Snippets {
		lang := sn.Language
		if lang == "*" {
			lang = locales.PluginsEveryLanguage()
		}
		rows = append(rows, contribRow{title: lang, detail: sn.Path})
	}
	add("snippets", locales.PluginsSnippets(), iconFileCode, rows)

	rows = nil
	for _, f := range ct.Formatters {
		title := f.Title
		if title == "" {
			title = f.ID
		}
		rows = append(rows, contribRow{title: title, detail: strings.Join(f.Languages, ", ")})
	}
	add("formatters", locales.PluginsFormatters(), iconWrap, rows)

	rows = nil
	for _, s := range ct.LanguageServers {
		rows = append(rows, contribRow{title: s.ID, mono: true, detail: strings.Join(append([]string{s.Command}, s.Args...), " "),
			tag: strings.Join(s.Languages, ", ")})
	}
	add("servers", locales.PluginsLanguageServers(), iconServer, rows)
	return out
}

// refresh reads how the plugin is doing, and its last lines.
func (w *prefsWindow) refresh(d *pluginInfo, pl *plugin.Plugin) {
	st := pl.Stats()
	d.mem, d.cpu, d.started, d.crashes = "—", "—", "—", strconv.Itoa(st.Crashes)
	if st.Memory > 0 {
		d.mem = fmt.Sprintf("%.1f MiB", float64(st.Memory)/(1<<20))
	}
	if st.CPU > 0 {
		d.cpu = shortDuration(st.CPU)
	}
	if st.ActivatedIn > 0 {
		d.started = shortDuration(st.ActivatedIn)
	}
	logs := pl.Logs()
	n := len(logs)
	var last time.Time
	if n > 0 {
		last = logs[n-1].Time
	}
	if n == d.logN && last.Equal(d.logLast) && d.logPal == w.pal && d.logReadFor == pl.Name {
		return
	}
	d.logN, d.logLast, d.logPal, d.logReadFor = n, last, w.pal, pl.Name
	p := w.pal
	font := w.monoFont()
	d.log = d.log[:0]
	for i, l := range logs[max(0, n-pluginLogLines):] {
		lvl := p.faint
		switch l.Level {
		case "error":
			lvl = p.dangerFg
		case "warn":
			lvl = p.modifiedFg
		case "info":
			lvl = p.muted
		}
		end := "\n"
		if i == min(n, pluginLogLines)-1 {
			end = ""
		}
		d.log = append(d.log,
			ui.Span{Text: l.Time.Format("15:04:05") + "  ", Font: font, Size: 11.5, Color: p.faint},
			ui.Span{Text: fmt.Sprintf("%-5s ", l.Level), Font: font, Size: 11.5, Color: lvl},
			ui.Span{Text: l.Message + end, Font: font, Size: 11.5, Color: p.text})
	}
	// New lines show as they come, while the end of the log shows.
	if d.follow || d.logScroll.Y >= d.logScroll.MaxY-2 {
		d.logScroll.Y = math.MaxFloat32
	}
	d.follow = false
}

// shortDuration is a while as the details of a plugin show it: in
// milliseconds below a second.
func shortDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%d ms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.2f s", d.Seconds())
	}
	return d.Round(time.Second).String()
}

// pluginDetail is the details of a plugin, in place of the Plugins page's
// list: Esc or the back button goes back to it.
func (w *prefsWindow) pluginDetail(c *ui.Context, it *pluginItem) {
	pg := &w.plugins
	pl := it.Plugin
	d := w.detailInfo(it)
	// How it is doing is read every pluginHealthEvery while the details
	// show, and not once they don't: the next frame asks for no more.
	if now := c.Now(); now.Sub(d.at) >= pluginHealthEvery {
		w.refresh(d, pl)
		d.at = now
		c.After(pluginHealthEvery)
	} else {
		c.After(pluginHealthEvery - now.Sub(d.at))
	}
	// Esc takes back the removal it asks for first, then goes back.
	if c.Shortcut(0, ui.KeyEscape) {
		if pg.confirm == pl.Name {
			pg.confirm = ""
		} else {
			w.closeDetail()
		}
	}
	w.detailHead(c, pl)
	ui.Scroll(c.Key("prefs-rows-plugin-" + pl.Name)).Grow(1).MinHeight(0).Children(func() {
		slideIn(c, ui.Column(c.Key("prefs-page-plugin-"+pl.Name)).Padding(20, 28, 32, 28).Gap(10).AlignSelf(ui.Center).
			WidthPercent(100).MaxWidth(prefsPageWidth), pageSlide).Children(func() {
			w.detailCard(c, it)
			if pl.HasCode() || pl.Script {
				w.sectionLabel(c, "plugin-health-head", locales.PluginsHealth(), 0)
				w.healthCard(c, d)
			}
			n := 0
			for _, g := range d.groups {
				n += len(g.rows)
			}
			w.sectionLabel(c, "plugin-adds-head", locales.PluginsContributes(), n)
			w.card(c, "plugin-adds", func() {
				if len(d.groups) == 0 {
					w.emptyState(c, "adds-none", iconBox, locales.PluginsContributesNone())
				}
				for i := range d.groups {
					w.contribGroup(c, &d.groups[i], i > 0)
				}
			})
			if len(d.settings) > 0 {
				w.sectionLabel(c, "plugin-settings-head", locales.PluginsSettings(), 0)
				w.card(c, "plugin-settings", func() { w.detailSettings(c, pl, d.settings) })
			}
			w.logCard(c, pl, d)
		})
	})
}

// detailHead is the head of the details: the way back to the list, and
// the plugin's name after it.
func (w *prefsWindow) detailHead(c *ui.Context, pl *plugin.Plugin) {
	p := w.pal
	ui.Row(c.Key("prefs-head")).Height(56).Padding(0, 20, 0, 16).Gap(6).AlignItems(ui.Center).Shrink(0).
		BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
		back := ui.ButtonBase(c.Key("plugin-back")).Padding(4, 10, 4, 6).Gap(6).Radius(7).AlignItems(ui.Center).Shrink(0).
			Label(locales.PluginsBack()).Tooltip(locales.PluginsBack())
		col := p.muted
		if back.Hovered() {
			back.Background(p.hover)
			col = p.text
		}
		back.Transition(hoverFade)
		pressPose(back)
		back.Children(func() {
			ui.Box(c.Key("icon")).Size(22, 22).Radius(6).Shrink(0).Justify(ui.Center).AlignItems(ui.Center).
				Background(p.accent.Alpha(0.14)).Children(func() {
				ui.Icon(c, iconChevronLeft).FontSize(13).TextColor(p.accent)
			})
			ui.Text(c, prefSection(settings.CatPlugins)).FontSize(13).FontWeight(500).TextColor(col).SingleLine().Transition(hoverFade)
		})
		if back.Clicked() {
			w.closeDetail()
		}
		ui.Icon(c, iconChevronRight).FontSize(12).TextColor(p.faint).Shrink(0)
		fadeIn(ui.Text(c.Key("title-"+pl.Name), pl.Title()).FontSize(16).FontWeight(600).TextColor(p.text).SingleLine().
			Grow(1).Shrink(1).MinWidth(0).Margin(0, 0, 0, 4))
	})
}

// detailCard is the head of the details of a plugin: its tile, name,
// version and where it came from, how it is doing, Update and Remove,
// and its switch.
func (w *prefsWindow) detailCard(c *ui.Context, it *pluginItem) {
	p := w.pal
	pg := &w.plugins
	pl := it.Plugin
	st := pl.Stats()
	on := !st.Disabled
	busy := pg.busy != ""
	w.card(c, "plugin-detail", func() {
		ui.Column(c).Padding(18, 20).Gap(12).Children(func() {
			ui.Row(c).Gap(16).AlignItems(ui.Start).Children(func() {
				var glyph *ui.SVG
				if pl.Script {
					glyph = iconFileCode
				}
				w.tileOf(c, it.initial, glyph, on, 52)
				ui.Column(c).Gap(4).Grow(1).Shrink(1).MinWidth(0).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(2, 0, 0, 0).Children(func() {
						ui.Text(c.Key("title"), pl.Title()).FontSize(17).FontWeight(600).TextColor(p.text).Shrink(1).SingleLine()
						if pl.Version != "" {
							w.pill(c, "version", pl.Version, p.muted, true)
						}
						if it.fromGit && plugins.updateWaits(pl.Name) {
							w.pill(c, "update-waits", locales.PluginsUpdateWaits(), p.accent, false)
						}
					})
					if pl.Description != "" {
						ui.Text(c, pl.Description).FontSize(12.5).LineHeight(1.45).TextColor(p.muted).Selectable()
					}
					state, dot := w.pluginState(pl, st)
					ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(2, 0, 0, 0).Wrap().Children(func() {
						ui.Box(c.Key("dot")).Size(6, 6).Radius(3).Shrink(0).Background(dot).Transition(hoverFade)
						ui.Text(c, state).FontSize(11.5).TextColor(p.muted).SingleLine().Shrink(0)
						ui.Text(c, "·").FontSize(11.5).TextColor(p.faint)
						ui.Text(c, pl.Name).Font(w.monoFont()).FontSize(11).TextColor(p.faint).SingleLine().Shrink(0).Selectable()
						ui.Text(c, "·").FontSize(11.5).TextColor(p.faint)
						ui.Text(c, it.source).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(1).MinWidth(0)
						if pl.Author != "" {
							ui.Text(c, "·").FontSize(11.5).TextColor(p.faint)
							ui.Text(c, locales.PluginsBy(pl.Author)).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(1).MinWidth(0)
						}
					})
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).MinHeight(30).Children(func() {
					if it.fromGit && !pl.Bundled {
						b := ui.Button(c.Key("update"), locales.PluginsUpdate()).FontSize(12).Padding(3, 10).Disabled(busy)
						pressPose(b)
						if b.Clicked() {
							w.updatePlugin(pl.Name)
						}
					}
					if !pl.Bundled && !pl.Script {
						b := ui.Button(c.Key("remove"), locales.PluginsRemove()).FontSize(12).Padding(3, 10).Disabled(busy)
						if !busy {
							b.TextColor(p.dangerFg)
						}
						pressPose(b)
						if b.Clicked() && !busy {
							pg.confirm = pl.Name
						}
					}
					if w.toggle(c.Key("on"), &on).Label(pl.Title()).Margin(0, 0, 0, 4).Changed() {
						w.setPluginSetting(pl.Name, "enabled", on, pl.DefaultOn())
					}
				})
			})
			if st.Err != "" {
				w.note(c, "plugin-err", iconOctagonAlert, p.dangerFg, st.Err, nil)
			}
			w.busyLine(c, pl.Name, 0)
			w.errNote(c, pl.Name, 0)
			if pg.confirm == pl.Name {
				w.confirmRemove(c, pl, 0)
			}
		})
	})
}

// healthCard is how the plugin is doing: what it holds, how long it ran
// and took to start, and how often it stopped.
func (w *prefsWindow) healthCard(c *ui.Context, d *pluginInfo) {
	p := w.pal
	w.card(c, "plugin-health", func() {
		ui.Row(c).AlignItems(ui.Stretch).Children(func() {
			for i, cell := range [...]struct{ key, label, value string }{
				{"memory", locales.PluginsMemory(), d.mem},
				{"cpu", locales.PluginsCPU(), d.cpu},
				{"started", locales.PluginsStartedIn(), d.started},
				{"crashes", locales.PluginsCrashes(), d.crashes},
			} {
				box := ui.Column(c.Key(cell.key)).Grow(1).Basis(0).MinWidth(0).Padding(12, 16, 14, 16).Gap(5)
				if i > 0 {
					border := p.border
					box.Draw(func(pt *ui.Painter, r ui.Rect) { pt.Fill(ui.Rect{X: r.X, Y: r.Y + 12, W: 1, H: r.H - 24}, border, 0) })
				}
				box.Children(func() {
					ui.Text(c, cell.label).FontSize(11).TextColor(p.faint).SingleLine()
					col := p.text
					if cell.key == "crashes" && cell.value != "0" {
						col = p.dangerFg
					}
					ui.Text(c.Key("value"), cell.value).FontSize(16).FontWeight(600).FontFeatures("tnum").TextColor(col).SingleLine().
						ContentTransition(ui.ContentTransition{Numeric: true})
				})
			}
		})
	})
}

// contribGroup is a kind of what the plugin adds, under its name, with
// the rule above it unless it is the first.
func (w *prefsWindow) contribGroup(c *ui.Context, g *contribGroup, rule bool) {
	p := w.pal
	col := ui.Column(c.Key("adds-"+g.key)).Padding(12, 0, 8, 0)
	if rule {
		border := p.border
		col.Draw(func(pt *ui.Painter, r ui.Rect) { pt.Fill(ui.Rect{X: r.X + 16, Y: r.Y, W: r.W - 32, H: 1}, border, 0) })
	}
	col.Children(func() {
		ui.Row(c).Padding(0, 16, 4, 16).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, g.icon).FontSize(13).TextColor(p.muted).Shrink(0)
			ui.Text(c, g.title).FontSize(12.5).FontWeight(600).TextColor(p.text).SingleLine()
			ui.Text(c, strconv.Itoa(len(g.rows))).FontSize(11).FontFeatures("tnum").TextColor(p.faint)
		})
		for i := range g.rows {
			w.contribRow(c, i, &g.rows[i])
		}
	})
}

// contribIndent is how far the things a plugin adds start in, under the
// name of their kind.
const contribIndent = 16 + 13 + 8

// contribRow is a thing the plugin adds: its name, what tells it apart,
// and its keys or a word of it at the end.
func (w *prefsWindow) contribRow(c *ui.Context, i int, r *contribRow) {
	p := w.pal
	ui.Row(c.Key(i)).Padding(5, 16, 5, contribIndent).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Baseline).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			t := ui.Text(c, r.title).FontSize(12.5).TextColor(p.text).SingleLine().Shrink(0).MaxWidth(320)
			if r.mono {
				t.Font(w.monoFont()).FontSize(12)
			}
			if r.detail != "" {
				ui.Text(c, r.detail).Font(w.monoFont()).FontSize(11).TextColor(p.faint).SingleLine().Shrink(1).MinWidth(0)
			}
		})
		if len(r.caps) > 0 {
			w.caps(c, r.caps)
		}
		if r.tag != "" {
			w.pill(c, "tag", r.tag, p.muted, false)
		}
	})
}

// caps shows a shortcut as caps, a cap for each key, as the Keyboard
// Shortcuts page does.
func (w *prefsWindow) caps(c *ui.Context, caps []string) {
	p := w.pal
	ui.Row(c.Key("caps")).Gap(3).AlignItems(ui.Center).Shrink(0).Children(func() {
		for j, cap := range caps {
			ui.Row(c.Key(j)).MinWidth(20).Height(20).Padding(0, 4.5).Radius(4).AlignItems(ui.Center).Justify(ui.Center).
				Background(p.surface).BorderWidth(1, 1, 2, 1).BorderColor(p.border).Children(func() {
				ui.Text(c, cap).FontSize(11).FontWeight(500).TextColor(p.text).SingleLine()
			})
		}
	})
}

// detailSettings are the plugin's settings, a row each, as the
// preferences show theirs.
func (w *prefsWindow) detailSettings(c *ui.Context, pl *plugin.Plugin, keys []string) {
	p := w.pal
	values := plugins.reg.Settings(pl)
	for i, k := range keys {
		def := pl.Contributes.Settings[k]
		row := ui.Row(c.Key("set-"+k)).Padding(12, 16).Gap(12).AlignItems(ui.Center)
		w.rowHover(row, i > 0)
		row.Children(func() {
			ui.Column(c).Gap(3).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Text(c, k).Font(w.monoFont()).FontSize(12).TextColor(p.text)
				if def.Description != "" {
					ui.Text(c, def.Description).FontSize(12).LineHeight(1.4).TextColor(p.muted)
				}
			})
			ui.Row(c).Shrink(0).Children(func() { w.pluginSettingControl(c, pl.Name, k, def, values[k]) })
		})
	}
}

// logCard is what the plugin logged last, to read and copy, and the
// button that opens all of it.
func (w *prefsWindow) logCard(c *ui.Context, pl *plugin.Plugin, d *pluginInfo) {
	p := w.pal
	ui.Row(c.Key("plugin-log-head")).Padding(10, 4, 0, 4).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, strings.ToUpper(locales.PluginsLog())).FontSize(10.5).FontWeight(600).LetterSpacing(0.7).TextColor(p.faint).SingleLine()
		ui.Spacer(c)
		b := ui.ButtonBase(c.Key("plugin-log-open")).Padding(2, 6).Gap(5).Radius(5).AlignItems(ui.Center).
			Label(locales.PluginsLogOpen()).Tooltip(locales.PluginsLogOpen())
		col := p.muted
		if b.Hovered() {
			b.Background(p.hover)
			col = p.text
		}
		b.Transition(hoverFade)
		pressPose(b)
		b.Children(func() {
			ui.Text(c, locales.PluginsLogOpen()).FontSize(11.5).TextColor(col).SingleLine().Transition(hoverFade)
			ui.Icon(c, iconExternal).FontSize(11).TextColor(p.faint)
		})
		if b.Clicked() {
			w.inFront(func(a *app) { a.showPluginLogOf(pl) })
		}
	})
	w.card(c, "plugin-log", func() {
		if len(d.log) == 0 {
			ui.Text(c.Key("log-none"), locales.PluginsLogNone()).FontSize(12).TextColor(p.faint).Padding(14, 16)
			return
		}
		ui.Scroll(c.Key("log")).MaxHeight(220).TrackScroll(&d.logScroll).Background(p.editor.Alpha(0.5)).Children(func() {
			ui.RichText(c.Key("lines"), d.log...).LineHeight(1.55).Padding(10, 14).Selectable()
		})
	})
}
