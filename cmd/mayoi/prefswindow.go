package main

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
	"mayoi/locales"
)

var iconPalette = lucide(`<path d="M12 22a1 1 0 0 1 0-20 10 9 0 0 1 10 9 5 5 0 0 1-5 5h-2.25a1.75 1.75 0 0 0-1.4 2.8l.3.4a1.75 1.75 0 0 1-1.4 2.8z"/><circle cx="13.5" cy="6.5" r=".5" fill="currentColor"/><circle cx="17.5" cy="10.5" r=".5" fill="currentColor"/><circle cx="6.5" cy="12.5" r=".5" fill="currentColor"/><circle cx="8.5" cy="7.5" r=".5" fill="currentColor"/>`)

// prefsShortcuts is the section of the settings window of the keyboard
// shortcuts, after the categories of the preferences.
const prefsShortcuts = "shortcuts"

// prefsIcons are the icons of the sections of the settings window.
var prefsIcons = map[string]*ui.SVG{
	settings.CatAppearance: iconPalette, settings.CatFonts: iconType, settings.CatEditor: iconFileCode,
	settings.CatMarkdown: iconFileText, settings.CatTerminal: iconTerminal, settings.CatDiff: iconDiff,
	settings.CatGit: iconBranch, settings.CatVim: iconCommand, settings.CatGeneral: iconSettings,
	prefsShortcuts: iconKeyboard,
}

// prefsWindow is the settings window, of which the app has one: the
// preferences by their categories, the folders' own, and the keyboard
// shortcuts.
type prefsWindow struct {
	m    *mayoi
	win  *mygo.Window
	pal  *palette
	fade paletteFade
	// section is the category shown, or prefsShortcuts, and query what
	// the search finds the preferences of, in every category.
	section string
	query   string
	// folder is the folder whose own preferences show, "" for the
	// person's.
	folder string
	// jump is the preference to bring into view and mark, as a link to
	// it asks, from jumpAt.
	jump   string
	jumpAt time.Time
	// texts and numbers are what the fields of the preferences hold, by
	// key, and editing the keys of those that have the keyboard: they
	// follow the preferences while they don't.
	texts   map[string]*string
	numbers map[string]*float64
	editing map[string]bool
	errors  []string
	keys    keysPane
}

// showPrefs opens the settings window, or brings it to the front, at
// section and the preference of key, "" for none: of the folder of from
// when key is a preference a folder chooses there.
func (m *mayoi) showPrefs(section, key string, from *desktop) {
	w := m.prefs
	if w == nil {
		w = &prefsWindow{m: m, section: settings.CatAppearance,
			texts: map[string]*string{}, numbers: map[string]*float64{}, editing: map[string]bool{}}
		w.keys.w = w
		w.win = mygo.NewWindow(mygo.WindowOptions{
			Title: locales.PrefsTitle(), Width: 940, Height: 680, MinWidth: 640, MinHeight: 420,
			StateKey: "settings", BackgroundColor: "#131317", Content: ui.View(w.view),
		})
		w.win.OnClosed(func() {
			if m.prefs == w {
				m.prefs = nil
			}
		})
		m.prefs = w
	}
	if section != "" {
		w.section, w.query = section, ""
	}
	if key != "" {
		if f, ok := settings.FieldOf(key); ok {
			w.section, w.query = f.Category, ""
			w.folder = ""
			if from != nil && f.Folder && from.a.folderPrefs != nil && from.a.folderPrefs.f.Has(key) {
				w.folder = from.a.root
			}
		}
		w.jump, w.jumpAt = key, time.Now()
	}
	if w.win.IsMinimized() {
		w.win.Restore()
	}
	w.win.Show()
	w.win.Focus()
	w.win.Invalidate()
}

// prefsChanged draws the settings window anew, as the preferences or the
// shortcuts changed.
func (m *mayoi) prefsChanged() {
	if m.prefs != nil {
		m.prefs.invalidate()
	}
}

// closePrefs closes the settings window.
func (m *mayoi) closePrefs() {
	if m.prefs != nil && m.prefs.win != nil {
		m.prefs.win.Close()
	}
}

// invalidate draws the window anew.
func (w *prefsWindow) invalidate() {
	if w.win != nil {
		w.win.Invalidate()
	}
}

// prefText is a message of the settings window by its key, as those of
// each preference are: "prefTitleFontSize" for fontSize's title.
func prefText(prefix, name string) string {
	return locales.Catalog.Format(i18n.Locale(), prefKey(prefix, name), nil)
}

// prefKey is the key of the message of prefix for name, a key of the
// preferences or a category: "prefTitle" and "fontSize" are
// "prefTitleFontSize".
func prefKey(prefix, name string) string {
	name = strings.NewReplacer("-", "", ".", "").Replace(name)
	if name == "" {
		return prefix
	}
	return prefix + strings.ToUpper(name[:1]) + name[1:]
}

func prefTitle(key string) string    { return prefText("prefTitle", key) }
func prefDesc(key string) string     { return prefText("prefDesc", key) }
func prefSection(name string) string { return prefText("prefSection", name) }

// prefOption is how an option of a preference of options reads: the
// languages in themselves.
func prefOption(key, opt string) string {
	if key == "locale" && opt != "" {
		return i18n.Autonym(opt)
	}
	if opt == "" {
		return prefText("prefDefault", key)
	}
	return prefText(prefKey("prefOpt", key), opt)
}

// prefFolder is a folder whose own preferences the window can show: an
// open window's.
type prefFolder struct {
	dir string
	h   *desktop
	f   *settings.Folder
}

// prefFolders are the folders whose own preferences the window can show.
func (w *prefsWindow) prefFolders() []prefFolder {
	var out []prefFolder
	for _, h := range w.m.wins {
		if fp := h.a.folderPrefs; fp != nil && !slices.ContainsFunc(out, func(o prefFolder) bool { return o.dir == h.a.root }) {
			out = append(out, prefFolder{h.a.root, h, fp.f})
		}
	}
	slices.SortFunc(out, func(x, y prefFolder) int { return strings.Compare(x.dir, y.dir) })
	return out
}

// scope is the folder whose own preferences show, nil for the person's.
func (w *prefsWindow) scope() *prefFolder {
	if w.folder == "" {
		return nil
	}
	for _, f := range w.prefFolders() {
		if f.dir == w.folder {
			return &f
		}
	}
	w.folder = ""
	return nil
}

// shown are the preferences the window shows: those of its category, or
// those the search finds, of a folder those it may choose.
func (w *prefsWindow) shown(folder bool) []settings.Field {
	words := strings.Fields(strings.ToLower(w.query))
	var out []settings.Field
	for _, f := range settings.Schema {
		if f.Memory || folder && !f.Folder {
			continue
		}
		if len(words) == 0 {
			if f.Category == w.section {
				out = append(out, f)
			}
			continue
		}
		text := strings.ToLower(f.Key + " " + prefTitle(f.Key) + " " + prefDesc(f.Key) + " " + prefSection(f.Category))
		if !slices.ContainsFunc(words, func(word string) bool { return !strings.Contains(text, word) }) {
			out = append(out, f)
		}
	}
	return out
}

func (w *prefsWindow) view(c *ui.Context) {
	m := w.m
	if m.s == nil {
		return
	}
	w.pal = applyTheme(c, &m.s.Prefs, &w.fade)
	p := w.pal
	c.SetZoom(float32(math.Pow(1.2, float64(m.s.ZoomLevel))))
	if title := locales.PrefsTitle(); w.win != nil && w.win.Title() != title {
		w.win.SetTitle(title)
	}
	// As a window of Mayoi's, Close closes it: the menu bar of macOS
	// takes its shortcut first.
	if !macKeys() && c.Shortcut(ui.Cmd, ui.KeyW) {
		w.m.closePrefs()
	}
	ui.Row(c).Fill().Background(p.editor).AlignItems(ui.Stretch).Children(func() {
		w.sidebar(c)
		ui.Column(c.Key("prefs-main")).Grow(1).MinWidth(0).Children(func() {
			if w.section == prefsShortcuts && w.query == "" {
				w.keys.view(c)
			} else {
				w.prefsPane(c)
			}
		})
	})
	w.keys.recordView(c)
	for _, msg := range w.errors {
		c.Toast(msg)
	}
	w.errors = w.errors[:0]
}

// sidebar is the window's search and its sections.
func (w *prefsWindow) sidebar(c *ui.Context) {
	p := w.pal
	ui.Column(c.Key("prefs-sidebar")).Width(220).Shrink(0).Padding(14, 10, 10, 10).Gap(10).
		Background(p.chrome).BorderWidth(0, 1, 0, 0).BorderColor(p.border).Children(func() {
		ui.SearchField(c.Key("prefs-search"), &w.query).Label(locales.PrefsSearch()).Placeholder(locales.PrefsSearch()).AutoFocus()
		// No section is chosen while the search shows what it finds of
		// them all.
		section := w.section
		if w.query != "" {
			section = ""
		}
		// The sections sit on the sidebar itself, not on a box of their
		// own, as the app's sidebar lists sit on its chrome.
		scope := w.scope()
		ui.Sidebar(c.Key("prefs-sections-list"), &section, func() {
			for _, cat := range append(slices.Clone(settings.Categories), prefsShortcuts) {
				item := ui.SidebarItem(c.Key("prefs-section-"+cat), cat, prefsIcons[cat], prefSection(cat))
				if n := w.changedIn(cat, scope); n > 0 {
					item.Children(func() {
						ui.Text(c.Key("count"), fmt.Sprint(n)).FontSize(10.5).FontWeight(600).TextColor(p.muted).
							Padding(0, 6).Radius(8).MinWidth(18).Shrink(0).TextAlign(ui.Center).Background(p.text.Alpha(0.07)).
							Tooltip(locales.PrefsChangedCount(n)).ContentTransition(ui.ContentTransition{Numeric: true}).Transition(dotPop)
					})
				}
			}
		}).Background(ui.Color{}).Padding(0).Gap(1).Grow(1).MinHeight(0)
		if section != "" && (section != w.section || w.query != "") {
			w.section, w.query = section, ""
		}
		ui.Column(c.Key("prefs-links")).Gap(1).Padding(8, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(p.border).Children(func() {
			w.footLink(c, "json", "settings.json", locales.CmdFileSettingsJSON(), func() { w.inFront(func(a *app) { a.openSettings() }) })
			w.footLink(c, "keys-json", "keybindings.json", locales.CmdFileKeybindingsJSON(), func() { w.inFront(func(a *app) { a.openKeybindings() }) })
		})
	})
}

// changedIn is how many of the person's preferences of the category are
// not as a first launch has them, or the folder chooses while its own
// show, or of the shortcuts how many commands have keys of the person's.
func (w *prefsWindow) changedIn(cat string, scope *prefFolder) int {
	n := 0
	if cat == prefsShortcuts {
		if km := w.m.keys; km != nil {
			seen := map[string]bool{}
			for _, b := range km.Bindings {
				name := strings.TrimPrefix(b.Command, "-")
				if b.When == nil && len(b.Args) == 0 && !seen[name] {
					seen[name] = true
					n++
				}
			}
		}
		return n
	}
	user := w.m.s.Prefs
	for _, f := range settings.Schema {
		if f.Category != cat || f.Memory {
			continue
		}
		if scope != nil && f.Folder && scope.f.Has(f.Key) || scope == nil && !user.IsDefaultKey(f.Key) {
			n++
		}
	}
	return n
}

// footLink is a link of the foot of the sidebar to the file name, which
// label says what opening does.
func (w *prefsWindow) footLink(c *ui.Context, key, name, label string, fn func()) {
	p := w.pal
	b := ui.ButtonBase(c.Key("prefs-link-"+key)).Padding(6, 8).Gap(8).Radius(6).AlignItems(ui.Center).Label(label).Tooltip(label)
	col := p.muted
	if b.Hovered() {
		b.Background(p.hover)
		col = p.text
	}
	b.Transition(hoverFade)
	b.Children(func() {
		ui.Icon(c, iconFileJSON).FontSize(13).TextColor(col).Shrink(0)
		ui.Text(c, name).Font(w.monoFont()).FontSize(11.5).TextColor(col).SingleLine().Grow(1).Shrink(1)
		ui.Icon(c, iconExternal).FontSize(11).TextColor(p.faint).Shrink(0)
	})
	if b.Clicked() {
		fn()
	}
}

// inFront runs fn on the window in front, which comes forward.
func (w *prefsWindow) inFront(fn func(a *app)) {
	if len(w.m.wins) == 0 {
		return
	}
	h := w.m.wins[0]
	h.do(func() { fn(h.a) })
	h.show()
}

// prefsPane is the preferences of the section, or those the search
// finds, with the choice of whose they are.
func (w *prefsWindow) prefsPane(c *ui.Context) {
	p := w.pal
	scope := w.scope()
	fields := w.shown(scope != nil)
	title, icon := prefSection(w.section), prefsIcons[w.section]
	if w.query != "" {
		title, icon = locales.PrefsFound(len(fields)), iconSearch
	}
	w.head(c, "prefs-head", icon, title, func() { w.scopePicker(c, scope) })
	if scope != nil {
		ui.Row(c.Key("prefs-folder-note")).Padding(10, 24).Gap(8).AlignItems(ui.Center).Background(p.accent.Alpha(0.08)).
			BorderWidth(0, 0, 1, 0).BorderColor(p.border).Transition(popIn).Children(func() {
			ui.Icon(c, iconFolder).FontSize(13).TextColor(p.accent).Shrink(0)
			ui.Text(c, locales.PrefsFolderNote(settings.FolderDir+"/"+settings.FolderFile)).FontSize(12).TextColor(p.text).Grow(1).Shrink(1)
			if ui.Button(c.Key("prefs-folder-open"), locales.PrefsOpenFile()).FontSize(12).Padding(3, 10).Clicked() {
				h := scope.h
				h.do(h.a.openFolderSettings)
				h.show()
			}
		})
	}
	// A page of its own for each section and for the search, so that it
	// rises in as it shows, and keeps where it was scrolled to.
	page := w.section
	if w.query != "" {
		page = "search"
	}
	ui.Scroll(c.Key("prefs-rows-" + page)).Grow(1).MinHeight(0).Children(func() {
		riseIn(ui.Column(c.Key("prefs-page-"+page)).Padding(20, 28, 32, 28).Gap(8).AlignSelf(ui.Center).
			WidthPercent(100).MaxWidth(prefsPageWidth), 0).Children(func() {
			if len(fields) == 0 {
				w.noneFound(c, locales.PrefsNoneFound())
				return
			}
			for len(fields) > 0 {
				// The search finds them by category, each under its name.
				n := 1
				for n < len(fields) && fields[n].Category == fields[0].Category {
					n++
				}
				if w.query == "" {
					n = len(fields)
				} else {
					cat := fields[0].Category
					ui.Row(c.Key("prefs-cat-"+cat)).Gap(6).AlignItems(ui.Center).Padding(10, 4, 0, 4).
						Transition(revealFade).Children(func() {
						ui.Icon(c, prefsIcons[cat]).FontSize(11.5).TextColor(p.faint)
						ui.Text(c, strings.ToUpper(prefSection(cat))).FontSize(10.5).FontWeight(600).
							LetterSpacing(0.7).TextColor(p.faint).SingleLine()
					})
				}
				group := fields[:n]
				fields = fields[n:]
				w.card(c, "prefs-card-"+group[0].Category, func() {
					for i, f := range group {
						if i > 0 {
							ui.Row(c.Key("prefs-rule-"+f.Key)).Height(1).Margin(0, 16).Background(p.border).Shrink(0)
						}
						w.row(c, f, scope)
					}
				})
			}
		})
	})
}

// prefsPageWidth is how wide the preferences grow, at most: wider, a line
// of them is too long to read across.
const prefsPageWidth = 760

// head is the head of a pane of the window: its icon and title, and the
// controls fn adds at its end.
func (w *prefsWindow) head(c *ui.Context, key string, icon *ui.SVG, title string, fn func()) {
	p := w.pal
	ui.Row(c.Key(key)).Height(56).Padding(0, 20, 0, 24).Gap(10).AlignItems(ui.Center).Shrink(0).
		BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
		ui.Box(c.Key("icon")).Size(26, 26).Radius(7).Shrink(0).Justify(ui.Center).AlignItems(ui.Center).
			Background(p.accent.Alpha(0.14)).Children(func() {
			ui.Icon(c, icon).FontSize(14).TextColor(p.accent)
		})
		ui.Text(c.Key("title"), title).FontSize(16).FontWeight(600).TextColor(p.text).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		fn()
	})
}

// card is a box of rows of the window, as a group of System Settings.
func (w *prefsWindow) card(c *ui.Context, key string, fn func()) {
	p := w.pal
	ui.Column(c.Key(key)).Radius(10).Clip().Background(p.text.Alpha(0.025)).Border(1, p.border).Children(fn)
}

// noneFound says a search found nothing.
func (w *prefsWindow) noneFound(c *ui.Context, text string) {
	p := w.pal
	ui.Column(c.Key("none-found")).Padding(48, 16).Gap(10).AlignItems(ui.Center).Children(func() {
		popUp(ui.Box(c.Key("badge")).Size(44, 44).Radius(22).Background(p.text.Alpha(0.06)).Justify(ui.Center).AlignItems(ui.Center)).Children(func() {
			ui.Icon(c, iconSearch).FontSize(19).TextColor(p.muted)
		})
		ui.Text(c, text).FontSize(13).TextColor(p.muted).TextAlign(ui.Center)
	})
}

// scopePicker chooses whose preferences show: the person's, or a
// folder's own, of the folders open.
func (w *prefsWindow) scopePicker(c *ui.Context, scope *prefFolder) {
	folders := w.prefFolders()
	if len(folders) == 0 {
		return
	}
	labels := []string{locales.PrefsUser()}
	chosen := labels[0]
	for _, f := range folders {
		label := locales.PrefsFolder(shortenHome(f.dir))
		labels = append(labels, label)
		if scope != nil && f.dir == scope.dir {
			chosen = label
		}
	}
	if ui.Select(c.Key("prefs-scope"), &chosen, labels).Label(locales.PrefsScope()).Width(260).Changed() {
		w.folder = ""
		if i := slices.Index(labels, chosen); i > 0 {
			w.folder = folders[i-1].dir
		}
	}
}

// row is a preference: its name and what it does, and its control, with
// a mark while it is not as a first launch has it, or a folder chooses
// it, and the button that takes that back.
func (w *prefsWindow) row(c *ui.Context, f settings.Field, scope *prefFolder) {
	p := w.pal
	m := w.m
	user := m.s.Prefs
	eff, changed := user, !user.IsDefaultKey(f.Key)
	if scope != nil {
		eff, changed = scope.f.Apply(user), scope.f.Has(f.Key)
	}
	raw := eff.Raw(f.Key)
	row := ui.Column(c.Key("prefs-row-"+f.Key)).Padding(14, 16).Gap(10).Transition(ui.ElementTransition{
		Colors: true, Duration: 160 * time.Millisecond, Enter: &ui.Motion{Y: 4},
	})
	if f.Key == w.jump {
		row.ScrollIntoView()
		// The row a link asked for glows, and fades back as it is seen.
		if since := c.Now().Sub(w.jumpAt); since < 1600*time.Millisecond && !c.Preferences().ReduceMotion {
			row.Background(p.accent.Alpha(0.14 * float32(1-since.Seconds()/1.6)))
			c.AnimationFrame()
		} else {
			w.jump = ""
		}
	}
	wide := f.Kind == settings.KindList || f.Kind == settings.KindStyles
	row.Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Gap(3).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
					ui.Text(c, prefTitle(f.Key)).FontSize(13).FontWeight(500).TextColor(p.text).Shrink(1)
					if changed {
						ui.Box(c.Key("changed")).Size(6, 6).Radius(3).Shrink(0).Background(p.accent).Transition(dotPop)
					}
				})
				ui.Text(c, prefDesc(f.Key)).FontSize(12).LineHeight(1.4).TextColor(p.muted)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, f.Key).Font(w.monoFont()).FontSize(10.5).TextColor(p.faint).SingleLine().Shrink(1)
					if scope != nil && !changed {
						ui.Text(c, "·").FontSize(11).TextColor(p.faint)
						ui.Text(c, locales.PrefsInherited()).FontSize(11).TextColor(p.faint).SingleLine().Shrink(1)
					}
				})
			})
			w.resetButton(c, f, scope, changed)
			if !wide {
				ui.Row(c).Shrink(0).Children(func() { w.control(c, f, raw, scope) })
			}
		})
		if wide {
			w.control(c, f, raw, scope)
		}
	})
}

// monoFont is the code's font, for the keys of the preferences.
func (w *prefsWindow) monoFont() string { return w.m.s.FontFamily }

// resetButton takes the preference back to its default, or the folder's
// choice of it away: it shows while there is something to take back.
func (w *prefsWindow) resetButton(c *ui.Context, f settings.Field, scope *prefFolder, changed bool) {
	if !changed {
		return
	}
	p := w.pal
	label := locales.PrefsReset()
	if scope != nil {
		label = locales.PrefsInherit()
	}
	b := ui.ButtonBase(c.Key("prefs-reset-"+f.Key)).Size(26, 26).Radius(6).Shrink(0).
		AlignItems(ui.Center).Justify(ui.Center).Label(label).Tooltip(label)
	col := p.faint
	if b.Hovered() {
		b.Background(p.hover)
		col = p.text
	}
	b.Transition(revealFade)
	pressPose(b)
	b.Children(func() {
		ui.Icon(c, iconUndo).FontSize(13).TextColor(col)
	})
	if b.Clicked() {
		if scope != nil {
			w.setFolder(scope, f.Key, nil)
		} else {
			d := settings.Default().Prefs
			w.set(f.Key, d.Raw(f.Key))
		}
	}
}

// control is the control that changes a preference.
func (w *prefsWindow) control(c *ui.Context, f settings.Field, raw json.RawMessage, scope *prefFolder) {
	k := c.Key("prefs-control-" + f.Key)
	apply := func(v any) {
		data, err := json.Marshal(v)
		if err != nil {
			return
		}
		if scope != nil {
			w.setFolder(scope, f.Key, data)
		} else {
			w.set(f.Key, data)
		}
	}
	switch f.Kind {
	case settings.KindBool:
		var on bool
		json.Unmarshal(raw, &on)
		if ui.Switch(k, &on).Label(prefTitle(f.Key)).Changed() {
			apply(on)
		}
	case settings.KindEnum:
		var v string
		json.Unmarshal(raw, &v)
		labels := make([]string, len(f.Options))
		chosen := ""
		for i, o := range f.Options {
			labels[i] = prefOption(f.Key, o)
			if o == v {
				chosen = labels[i]
			}
		}
		if chosen == "" {
			// A value of the file the window doesn't offer, as "zh".
			chosen = v
			labels = append(labels, v)
		}
		sel := ui.Select(k, &chosen, labels).Label(prefTitle(f.Key)).MinWidth(180)
		if f.Key == "appearance" && alwaysDark(w.m.s.Theme) {
			sel.Disabled(true)
		}
		if sel.Changed() {
			if i := slices.Index(labels, chosen); i >= 0 && i < len(f.Options) {
				apply(f.Options[i])
			}
		}
	case settings.KindNumber:
		var v float64
		json.Unmarshal(raw, &v)
		buf := w.numbers[f.Key]
		if buf == nil {
			buf = new(float64)
			w.numbers[f.Key] = buf
		}
		if !w.editing[f.Key] {
			*buf = v
		}
		in := ui.NumberInput(k, buf, f.Min, f.Max, f.Step).Label(prefTitle(f.Key)).Width(132)
		w.editing[f.Key] = in.Focused()
		if in.Changed() && *buf != v {
			apply(*buf)
		}
	case settings.KindText:
		var v string
		json.Unmarshal(raw, &v)
		buf := w.texts[f.Key]
		if buf == nil {
			buf = new(string)
			w.texts[f.Key] = buf
		}
		if !w.editing[f.Key] {
			*buf = v
		}
		in := ui.TextInput(k, buf).Label(prefTitle(f.Key)).Width(240)
		if ph := prefText("prefHint", f.Key); ph != prefKey("prefHint", f.Key) {
			in.Placeholder(ph)
		}
		w.editing[f.Key] = in.Focused()
		if in.Changed() {
			apply(*buf)
		}
	case settings.KindList:
		var tokens []string
		json.Unmarshal(raw, &tokens)
		if ui.TokenField(k, &tokens, w.listSuggestions(f.Key)).Label(prefTitle(f.Key)).Changed() {
			if tokens == nil {
				tokens = []string{}
			}
			apply(tokens)
		}
	case settings.KindStyles:
		w.stylesControl(c, raw)
	}
}

// listSuggestions are what a list of a preference offers.
func (w *prefsWindow) listSuggestions(key string) []string {
	switch key {
	case "syncIgnored":
		return append([]string{settings.SyncSettings, settings.SyncKeybindings}, settings.PrefKeys...)
	case "fontFeatures":
		return []string{"calt=0", "liga=0", "zero", "ss01", "ss02", "ss03", "ss04", "ss05", "cv01", "cv02"}
	}
	return nil
}

// stylesControl edits the looks of the projects on the rail: a label and
// a color for each folder that has one, and what takes it away.
func (w *prefsWindow) stylesControl(c *ui.Context, raw json.RawMessage) {
	p := w.pal
	var styles map[string]settings.TabStyle
	json.Unmarshal(raw, &styles)
	dirs := slices.Sorted(func(yield func(string) bool) {
		for d := range styles {
			if !yield(d) {
				return
			}
		}
	})
	ui.Column(c.Key("prefs-styles")).Gap(6).Children(func() {
		if len(dirs) == 0 {
			ui.Text(c, locales.PrefsNoStyles()).FontSize(12).TextColor(p.faint)
			return
		}
		colors, colorLabels := []string{""}, []string{locales.PrefsStyleOwnColor()}
		for _, rc := range railColors {
			colors, colorLabels = append(colors, rc.name), append(colorLabels, rc.label())
		}
		for _, dir := range dirs {
			st := styles[dir]
			ui.Row(c.Key("prefs-style-"+dir)).Gap(8).AlignItems(ui.Center).Padding(6, 10).Radius(6).
				Background(p.hover).Children(func() {
				ui.Text(c, shortenHome(dir)).FontSize(12).TextColor(p.text).SingleLine().Grow(1).Shrink(1).MinWidth(0)
				buf := w.texts["style:"+dir]
				if buf == nil {
					buf = new(string)
					w.texts["style:"+dir] = buf
				}
				if !w.editing["style:"+dir] {
					*buf = st.Label
				}
				in := ui.TextInput(c.Key("prefs-style-label-"+dir), buf).Label(locales.PrefsStyleLabel()).
					Placeholder(locales.PrefsStyleLabel()).Width(72)
				w.editing["style:"+dir] = in.Focused()
				if in.Changed() {
					st.Label = railLabel(*buf)
					w.setTabStyle(dir, st)
				}
				chosen := colorLabels[0]
				if i := slices.Index(colors, st.Color); i >= 0 {
					chosen = colorLabels[i]
				}
				if ui.Select(c.Key("prefs-style-color-"+dir), &chosen, colorLabels).Label(locales.PrefsStyleColor()).Width(130).Changed() {
					if i := slices.Index(colorLabels, chosen); i >= 0 {
						st.Color = colors[i]
						w.setTabStyle(dir, st)
					}
				}
				if st.Icon != "" {
					ui.Icon(c, iconImage).FontSize(13).TextColor(p.muted).Tooltip(st.Icon)
				}
				x := ui.ButtonBase(c.Key("prefs-style-x-" + dir)).Width(24).Height(24).Radius(5).AlignItems(ui.Center).
					Justify(ui.Center).Label(locales.PrefsStyleRemove()).Tooltip(locales.PrefsStyleRemove())
				if x.Hovered() {
					x.Background(p.surfaceHover)
				}
				x.Children(func() { ui.Icon(c, iconX).FontSize(12).TextColor(p.muted) })
				if x.Clicked() {
					w.setTabStyle(dir, settings.TabStyle{})
				}
			})
		}
	})
}

// setTabStyle gives the project of dir its look on the rail, letting go
// of the icon it no longer shows.
func (w *prefsWindow) setTabStyle(dir string, st settings.TabStyle) {
	old := w.m.s.ProjectTabStyle[dir].Icon
	w.m.s.SetTabStyle(dir, st)
	if st.Icon != old {
		w.m.forgetIcon(old)
	}
	for _, h := range w.m.wins {
		h.a.invalidate()
	}
}

// set makes raw the person's preference of key, in every window at once.
func (w *prefsWindow) set(key string, raw json.RawMessage) {
	p := w.m.s.Prefs
	if err := p.Set(key, raw); err != nil {
		w.errors = append(w.errors, err.Error())
		return
	}
	w.m.reload(p)
	w.m.s.Changed()
}

// setFolder has the folder choose raw for the preference of key, or no
// longer choose it for nil.
func (w *prefsWindow) setFolder(scope *prefFolder, key string, raw json.RawMessage) {
	if err := scope.f.Set(key, raw); err != nil {
		w.errors = append(w.errors, locales.FailSave(scope.f.Path, err.Error()))
	}
	a := scope.h.a
	a.post(a.folderPrefsChanged)
	w.invalidate()
}
