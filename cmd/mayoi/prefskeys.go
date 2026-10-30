package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/keymap"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// keysPane is the keyboard shortcuts of the settings window: every
// command with its keys, which a person changes by pressing new ones.
type keysPane struct {
	w     *prefsWindow
	query string
	// byKeys has the search find the commands of the keys pressed in it,
	// found, rather than those of the text typed.
	byKeys bool
	found  []keymap.Stroke
	// recording is the command whose keys are being pressed, nil for none,
	// and strokes what was pressed of them: two for a chord.
	recording *command
	strokes   []keymap.Stroke
	// shown is whether the dialog that takes the keys showed in the frame
	// before, so that it fades in as it starts showing.
	shown bool
	// filter is which commands show, and lists the place of the list of
	// each; chosen is the command chosen in them, by its ID, which sel is
	// the line of in the list shown.
	filter keyFilter
	lists  [keyFilters]ui.ListState
	chosen string
	sel    int
	// searchH, keysH and listH are the search by text, the search by keys
	// and the list, which the keyboard moves between.
	searchH, keysH, listH ui.Handle
}

// keyFilter is which commands the shortcuts show: all of them, those
// whose keys the person changed, or those whose keys another command has
// too.
type keyFilter int

const (
	keysAll keyFilter = iota
	keysChanged
	keysConflicting
	keyFilters
)

// shows reports whether the filter shows the row.
func (f keyFilter) shows(r keyRow) bool {
	switch f {
	case keysChanged:
		return r.changed()
	case keysConflicting:
		return len(r.others) > 0
	}
	return true
}

// keyGroups are the menus whose commands the shortcuts group, in the
// order of the menu bar, by the prefixes of their commands' IDs.
var keyGroups = []struct {
	label    func() string
	prefixes []string
}{
	{locales.MenuFile, []string{"file.", "remote."}},
	{locales.MenuEdit, []string{"edit."}},
	{locales.MenuSelection, []string{"sel."}},
	{locales.MenuView, []string{"view.", "editor.", "image."}},
	{locales.MenuGo, []string{"go."}},
	{locales.MenuGit, []string{"git.", "review."}},
	{locales.MenuAccount, []string{"account."}},
}

// keyGroup is the group of the command of id, of keyGroups: the last for
// one of no menu.
func keyGroup(id string) int {
	for i, g := range keyGroups {
		if slices.ContainsFunc(g.prefixes, func(p string) bool { return strings.HasPrefix(id, p) }) {
			return i
		}
	}
	return len(keyGroups) - 1
}

// keyItem is a line of the list of the shortcuts: the head of a group,
// with how many of its commands show, or a command.
type keyItem struct {
	head  string
	count int
	row   keyRow
}

// key is the identity of the line in the list.
func (it keyItem) key() string {
	if it.head != "" {
		return "head:" + it.head
	}
	return it.row.cmd.id
}

// keyItems are the rows under the heads of their groups, in the order of
// the menus.
func keyItems(rows []keyRow) []keyItem {
	byGroup := make([][]keyRow, len(keyGroups))
	for _, r := range rows {
		g := keyGroup(r.cmd.id)
		byGroup[g] = append(byGroup[g], r)
	}
	var out []keyItem
	for g, rs := range byGroup {
		if len(rs) == 0 {
			continue
		}
		out = append(out, keyItem{head: keyGroups[g].label(), count: len(rs)})
		for _, r := range rs {
			out = append(out, keyItem{row: r})
		}
	}
	return out
}

// keySource is where the keys of a command come from.
type keySource int

const (
	keyNone keySource = iota
	keyDefault
	keyUser
	keyRemoved
)

// keyRow is a command as the shortcuts show it: its keys, where they come
// from, and how many bindings of the file it has with a condition or
// arguments besides.
type keyRow struct {
	cmd    command
	keys   []keymap.Stroke
	source keySource
	more   int
	// others are the labels of the other commands with the same keys.
	others []string
}

// changed reports whether the person changed the row's keys.
func (r keyRow) changed() bool { return r.source == keyUser || r.source == keyRemoved }

// defaultAccel is a command's own shortcut, of its menu or of the view.
func defaultAccel(cmd command) string {
	if cmd.accel != "" {
		return cmd.accel
	}
	return platformAccel(cmd.id, viewKeys[cmd.id])
}

// rows are the commands of a's window with their keys.
func (k *keysPane) rows(a *app) []keyRow {
	km := k.w.m.keys
	var rows []keyRow
	for _, cmd := range a.commands() {
		names := commandNames(cmd.id)
		r := keyRow{cmd: cmd}
		if keys := km.Last(names...); keys != nil {
			r.keys, r.source = keys, keyUser
		} else if st, ok := keymap.ParseAccel(defaultAccel(cmd)); ok {
			r.keys, r.source = []keymap.Stroke{st}, keyDefault
			if km.Removes(names...) {
				r.keys, r.source = nil, keyRemoved
			}
		}
		if km != nil {
			for _, b := range km.Bindings {
				if !b.Removal() && (b.When != nil || len(b.Args) > 0) && slices.Contains(names, b.Command) {
					r.more++
				}
			}
		}
		rows = append(rows, r)
	}
	// The commands that share their keys.
	byKeys := map[string][]int{}
	for i, r := range rows {
		if len(r.keys) > 0 {
			ks := keymap.KeysString(r.keys)
			byKeys[ks] = append(byKeys[ks], i)
		}
	}
	for _, same := range byKeys {
		if len(same) < 2 {
			continue
		}
		for _, i := range same {
			for _, j := range same {
				if i != j {
					rows[i].others = append(rows[i].others, rows[j].cmd.label)
				}
			}
		}
	}
	return rows
}

// match reports whether the search finds the row.
func (k *keysPane) match(r keyRow) bool {
	if k.byKeys {
		return len(k.found) == 0 || len(r.keys) >= len(k.found) && slices.Equal(r.keys[:len(k.found)], k.found)
	}
	text := strings.ToLower(r.cmd.label + " " + r.cmd.id + " " + keymap.KeysString(r.keys))
	for _, word := range strings.Fields(strings.ToLower(k.query)) {
		if !strings.Contains(text, word) {
			return false
		}
	}
	return true
}

func (k *keysPane) view(c *ui.Context) {
	w := k.w
	if len(w.m.wins) == 0 {
		return
	}
	a := w.m.wins[0].a
	all := slices.DeleteFunc(k.rows(a), func(r keyRow) bool { return !k.match(r) })
	var counts [keyFilters]int
	var rows []keyRow
	for _, r := range all {
		for f := range keyFilters {
			if f.shows(r) {
				counts[f]++
			}
		}
		if k.filter.shows(r) {
			rows = append(rows, r)
		}
	}
	w.head(c, "keys-head", iconKeyboard, prefSection(prefsShortcuts), func() { k.searchBar(c) })
	k.filterChips(c, counts)
	// A page of its own for each filter, which rises in as it is chosen
	// and keeps its place.
	riseIn(ui.Column(c.Key(fmt.Sprintf("keys-page-%d", k.filter))).Grow(1).MinHeight(0), 0).Children(func() {
		if len(rows) == 0 {
			text := locales.KeysNoneFound()
			if k.filter == keysConflicting && k.query == "" && !k.byKeys {
				text = locales.KeysNoConflicts()
			}
			w.noneFound(c, text)
			return
		}
		k.list(c, keyItems(rows))
	})
}

// searchBar is the search of the shortcuts, by text or by the keys
// pressed, and the button that switches between them.
func (k *keysPane) searchBar(c *ui.Context) {
	p := k.w.pal
	if k.byKeys {
		k.keysSearch(c)
	} else {
		ui.SearchField(c.Key("keys-search"), &k.query).Label(locales.KeysSearch()).Placeholder(locales.KeysSearch()).Width(260).
			Bind(&k.searchH)
	}
	// Down goes from the search to the commands it found.
	k.searchH.OnShortcut(c, 0, ui.KeyDown, func() { k.listH.Focus(c) })
	by := ui.ButtonBase(c.Key("keys-by-keys")).Size(28, 28).Radius(6).Shrink(0).AlignItems(ui.Center).Justify(ui.Center).
		Label(locales.KeysSearchByKeys()).Tooltip(locales.KeysSearchByKeys()).Selected(k.byKeys)
	switch {
	case k.byKeys:
		by.Background(p.accent.Alpha(0.16))
	case by.Hovered():
		by.Background(p.hover)
	}
	by.Transition(hoverFade)
	pressPose(by)
	by.Children(func() {
		col := p.muted
		if k.byKeys {
			col = p.accent
		}
		ui.Icon(c, iconKeyboard).FontSize(14).TextColor(col)
	})
	if by.Clicked() {
		k.byKeys, k.found = !k.byKeys, nil
		if k.byKeys {
			k.keysH.Focus(c)
		} else {
			k.searchH.Focus(c)
		}
	}
}

// filterChip is the identity of the face under the filter chosen, which
// slides to the one clicked.
type filterChip struct{}

// filterChips choose which commands show, each with how many it shows:
// the face under the one chosen slides to the one clicked, and the
// arrows move the choice while one has the keyboard.
func (k *keysPane) filterChips(c *ui.Context, counts [keyFilters]int) {
	p := k.w.pal
	labels := [keyFilters]string{locales.KeysFilterAll(), locales.KeysFilterChanged(), locales.KeysFilterConflicts()}
	sel := int(k.filter)
	ui.Row(c.Key("keys-filters")).Padding(10, 24).Gap(8).AlignItems(ui.Center).Shrink(0).
		BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
		chips := ui.SegmentedBase(c.Key("chips"), &sel, int(keyFilters))
		chips.Track.Gap(6).Label(locales.KeysFilter()).Children(func() {
			for f := range keyFilters {
				on := int(f) == sel
				chip := chips.Segment(int(f)).Row().Height(26).Padding(0, 6, 0, 11).Gap(7).Radius(13).AlignItems(ui.Center).
					Label(labels[f]).FocusRing(false)
				col, count, countBg := p.muted, p.faint, p.text.Alpha(0.07)
				switch {
				case on:
					col, count, countBg = p.text, p.accent, p.accent.Alpha(0.16)
				case chip.Hovered():
					col = p.text
					chip.Background(p.hover)
				}
				if f == keysConflicting && counts[f] > 0 {
					count, countBg = p.modifiedFg, p.modified.Alpha(0.16)
				}
				chip.Transition(hoverFade)
				pressPose(chip)
				chip.Children(func() {
					if on {
						face := ui.Box(c.Key("face")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Radius(13).
							Background(p.accent.Alpha(0.10)).Border(1, p.accent.Alpha(0.45))
						if chip.FocusVisible() {
							face.Shadow(0, 0, 0, 3, p.accent.Alpha(0.25))
						}
						face.Transition(ui.ElementTransition{Duration: 200 * time.Millisecond, Ease: ui.EaseInOut, Match: filterChip{}})
					}
					if f == keysConflicting {
						ui.Icon(c, iconTriangleAlert).FontSize(11.5).TextColor(col).Shrink(0)
					}
					ui.Text(c, labels[f]).FontSize(12).FontWeight(500).TextColor(col).SingleLine()
					ui.Text(c.Key("count"), fmt.Sprint(counts[f])).FontSize(10.5).FontWeight(600).FontFeatures("tnum").TextColor(count).
						Padding(0, 6).Radius(8).MinWidth(18).TextAlign(ui.Center).Background(countBg).
						ContentTransition(ui.ContentTransition{Numeric: true}).Transition(hoverFade)
				})
			}
		})
	})
	if keyFilter(sel) != k.filter {
		k.filter = keyFilter(sel)
	}
}

// list is the commands under the heads of their menus, whose head stays
// at the top while its commands scroll under it. The keys move the
// choice, Enter changes the keys of the command chosen, Delete takes
// them away, and Left goes back to the sections.
func (k *keysPane) list(c *ui.Context, items []keyItem) {
	w := k.w
	p := w.pal
	k.sel = slices.IndexFunc(items, func(it keyItem) bool { return it.head == "" && it.row.cmd.id == k.chosen })
	s := &k.lists[k.filter]
	s.Selected = &k.sel
	s.Header = func(i int) bool { return i < len(items) && items[i].head != "" }
	s.Key = func(i int) any {
		if i >= len(items) {
			return i
		}
		return items[i].key()
	}
	s.Label = func(i int) string {
		if i >= len(items) || items[i].head != "" {
			return ""
		}
		return items[i].row.cmd.label
	}
	// The list's own marks of the row chosen and hovered give way to the
	// pane's, which fade and slide.
	base := c.Theme()
	quiet := *base
	quiet.Accent, quiet.AccentText, quiet.SurfaceHover = ui.Color{}, p.text, ui.Color{}
	c.SetTheme(&quiet)
	focused := k.listH.FocusWithin(c)
	list := ui.List(c.Key("keys-list"), s, len(items), func(i int) {
		if it := items[i]; it.head != "" {
			k.groupHead(c, it)
		} else {
			k.row(c, it.row, i == k.sel, focused)
		}
	}).Grow(1).Padding(0, 12, 16, 12).Label(prefSection(prefsShortcuts)).FocusRing(false).Bind(&k.listH)
	c.SetTheme(base)
	if k.sel >= 0 && k.sel < len(items) && items[k.sel].head == "" {
		k.chosen = items[k.sel].row.cmd.id
	}
	chosen := func() (keyRow, bool) {
		if k.sel >= 0 && k.sel < len(items) && items[k.sel].head == "" {
			return items[k.sel].row, true
		}
		return keyRow{}, false
	}
	if list.Submitted() {
		if r, ok := chosen(); ok {
			k.record(r.cmd)
		}
	}
	for _, key := range []ui.Key{ui.KeyBackspace, ui.KeyDelete} {
		k.listH.OnShortcut(c, 0, key, func() {
			if r, ok := chosen(); ok && len(r.keys) > 0 {
				k.removeKeys(r.cmd)
			}
		})
	}
	k.listH.OnShortcut(c, 0, ui.KeyLeft, func() { w.sectionsH.Focus(c) })
	k.listH.OnShortcut(c, 0, ui.KeyEscape, func() {
		if k.query != "" || len(k.found) > 0 || k.filter != keysAll {
			k.query, k.found, k.filter = "", nil, keysAll
		}
	})
}

// groupHead is the head of a menu's commands in the list, with how many
// show: opaque, as the commands scroll under it.
func (k *keysPane) groupHead(c *ui.Context, it keyItem) {
	p := k.w.pal
	ui.Row(c.Key("head")).Height(34).Padding(10, 12, 0, 12).Gap(8).AlignItems(ui.Center).Background(p.editor).Children(func() {
		ui.Text(c, strings.ToUpper(it.head)).FontSize(10.5).FontWeight(600).LetterSpacing(0.7).TextColor(p.faint).SingleLine()
		ui.Text(c.Key("count"), fmt.Sprint(it.count)).FontSize(10.5).FontFeatures("tnum").TextColor(p.faint.Alpha(0.7)).SingleLine().
			ContentTransition(ui.ContentTransition{Numeric: true})
		ui.Box(c).Height(1).Grow(1).Background(p.border)
	})
}

// The widths of the columns of the shortcuts after the command's.
const (
	keysWidth    = 168
	sourceWidth  = 84
	actionsWidth = 84
)

// keyMark is the identity of the mark under the command chosen, which
// slides from row to row as the keys move the choice.
type keyMark struct{}

// keysSearch is the search by keys: what is pressed in it finds the
// commands whose keys start so.
func (k *keysPane) keysSearch(c *ui.Context) {
	p := k.w.pal
	box := ui.ButtonBase(c.Key("keys-search-keys")).Width(260).Height(28).Padding(0, 10).Gap(6).Radius(6).AlignItems(ui.Center).
		Background(c.Theme().Surface).BorderWidth(1, 1, 1, 1).BorderColor(p.accent).Label(locales.KeysPressToSearch()).Bind(&k.keysH).
		Transition(popIn)
	box.HandleInput(func(ev ui.InputEvent) bool {
		switch {
		case ev.Kind == ui.InputText:
			return true
		case ev.Kind != ui.InputKeyDown:
			return false
		case ev.Key == ui.KeyTab && ev.Mods&^ui.Shift == 0:
			// Tab leaves it, as it does any field.
			return false
		case ev.Key == ui.KeyEscape && ev.Mods == 0:
			k.byKeys, k.found = false, nil
			k.searchH.Focus()
		case ev.Key == ui.KeyBackspace && ev.Mods == 0:
			k.found = nil
		case keymap.KnownKey(ev.Key):
			k.found = []keymap.Stroke{{Mods: ev.Mods, Key: ev.Key}}
		}
		k.w.invalidate()
		return true
	})
	box.Children(func() {
		ui.Icon(c, iconKeyboard).FontSize(13).TextColor(p.accent).Shrink(0)
		if len(k.found) == 0 {
			ui.Text(c, locales.KeysPressToSearch()).FontSize(12).TextColor(p.faint).SingleLine()
			return
		}
		k.caps(c, k.found, false, false)
	})
}

// caps shows keys as caps, a cap for each key, the strokes of a chord
// apart: large ones for the dialog that takes them, which pop in as they
// are pressed.
func (k *keysPane) caps(c *ui.Context, keys []keymap.Stroke, large, conflict bool) {
	p := k.w.pal
	edge := p.border
	if conflict {
		edge = p.modifiedFg.Alpha(0.6)
	}
	size, h, minW, gap := float32(11), float32(20), float32(20), float32(3)
	if large {
		size, h, minW, gap = 15, 32, 32, 5
	}
	ui.Row(c).Gap(gap * 3).AlignItems(ui.Center).Shrink(0).Children(func() {
		for i, st := range keys {
			ui.Row(c.Key(i)).Gap(gap).AlignItems(ui.Center).Children(func() {
				for j, cap := range accelCaps(st.Accel(), macKeys()) {
					b := ui.Row(c.Key(j)).MinWidth(minW).Height(h).Padding(0, size/2.5).Radius(h/5).AlignItems(ui.Center).
						Justify(ui.Center).Background(p.surface).BorderWidth(1, 1, 2, 1).BorderColor(edge)
					if large {
						b.Transition(dotPop)
					}
					b.Children(func() {
						ui.Text(c, cap).FontSize(size).FontWeight(500).TextColor(p.text).SingleLine()
					})
				}
			})
		}
	})
}

// row is a command with its keys, and the buttons that change them:
// they show as the pointer is over it or it is chosen. The command chosen
// sits on a mark, which rings it while the list has the keyboard.
func (k *keysPane) row(c *ui.Context, r keyRow, chosen, focused bool) {
	p := k.w.pal
	row := ui.Row(c.Key("keys-row-"+r.cmd.id)).Padding(7, 12).Gap(12).Radius(7).AlignItems(ui.Center)
	hovered := row.Hovered()
	if hovered && !chosen {
		row.Background(p.hover)
	}
	row.Transition(hoverFade)
	row.Children(func() {
		if chosen {
			mark := ui.Box(c.Key("mark")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Radius(7)
			if focused {
				mark.Background(p.accent.Alpha(0.12)).Border(1, p.accent.Alpha(0.55))
			} else {
				mark.Background(p.active).Border(1, ui.Color{})
			}
			mark.Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true, Match: keyMark{}})
		}
		ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Text(c, r.cmd.label).FontSize(12.5).TextColor(p.text).SingleLine()
			ui.Text(c, r.cmd.id).Font(k.w.monoFont()).FontSize(10.5).TextColor(p.faint).SingleLine()
		})
		ui.Row(c).Width(keysWidth).Shrink(0).Gap(6).AlignItems(ui.Center).Children(func() {
			if len(r.keys) > 0 {
				k.caps(c, r.keys, false, len(r.others) > 0)
			} else {
				ui.Text(c, "—").FontSize(12).TextColor(p.faint)
			}
			if len(r.others) > 0 {
				tip := locales.KeysConflict(strings.Join(r.others, ", "))
				ui.Icon(c.Key("conflict"), iconTriangleAlert).FontSize(12).TextColor(p.modifiedFg).Shrink(0).
					Label(tip).Tooltip(tip).Transition(dotPop)
			}
			if r.more > 0 {
				ui.Text(c, locales.KeysMore(r.more)).FontSize(10.5).TextColor(p.faint).SingleLine().
					Tooltip(locales.KeysMoreTip())
			}
		})
		ui.Row(c).Width(sourceWidth).Shrink(0).Children(func() {
			switch r.source {
			case keyDefault:
				ui.Text(c, locales.KeysDefault()).FontSize(11.5).TextColor(p.faint).SingleLine()
			case keyUser:
				k.pill(c, locales.KeysUser(), p.accent)
			case keyRemoved:
				k.pill(c, locales.KeysRemoved(), p.modifiedFg)
			}
		})
		ui.Row(c).Width(actionsWidth).Shrink(0).Gap(2).Justify(ui.End).Children(func() {
			if !hovered && !chosen {
				return
			}
			k.action(c, "change:"+r.cmd.id, iconPencil, locales.KeysChange(), true, func() { k.record(r.cmd) })
			k.action(c, "remove:"+r.cmd.id, iconMinus, locales.KeysRemove(), len(r.keys) > 0, func() { k.removeKeys(r.cmd) })
			k.action(c, "reset:"+r.cmd.id, iconUndo, locales.KeysReset(), r.changed(), func() { k.resetKeys(r.cmd) })
		})
	})
}

// pill is where the keys of a row come from, when they are the person's,
// in col.
func (k *keysPane) pill(c *ui.Context, text string, col ui.Color) {
	ui.Text(c.Key("pill"), text).FontSize(10.5).FontWeight(600).TextColor(col).Padding(1, 7).Radius(9).
		Background(col.Alpha(0.14)).SingleLine().Transition(dotPop)
}

// action is a button of a row of the shortcuts.
func (k *keysPane) action(c *ui.Context, key string, icon *ui.SVG, label string, on bool, fn func()) {
	p := k.w.pal
	b := ui.ButtonBase(c.Key("keys-"+key)).Size(24, 24).Radius(5).AlignItems(ui.Center).Justify(ui.Center).
		Label(label).Tooltip(label).Disabled(!on)
	if b.Hovered() && on {
		b.Background(p.surfaceHover)
	}
	b.Transition(revealFade)
	pressPose(b)
	b.Children(func() {
		col := p.muted
		if !on {
			col = p.faint.Alpha(0.5)
		}
		ui.Icon(c, icon).FontSize(12.5).TextColor(col)
	})
	if b.Clicked() && on {
		fn()
	}
}

// record starts taking the keys pressed as the new keys of cmd.
func (k *keysPane) record(cmd command) {
	k.recording, k.strokes = &cmd, nil
	k.w.invalidate()
}

// stroke takes a key pressed for the command recording: a third starts
// the keys again.
func (k *keysPane) stroke(s keymap.Stroke) {
	if len(k.strokes) >= 2 {
		k.strokes = nil
	}
	k.strokes = append(k.strokes, s)
	k.w.invalidate()
}

// recordAccel takes the shortcut of command id of the menus as a key
// pressed while the keys are recorded, as the menus of macOS take their
// shortcuts before the window: it reports whether it took it.
func (k *keysPane) recordAccel(id string) bool {
	if k.recording == nil || len(k.w.m.wins) == 0 {
		return false
	}
	for _, cmd := range k.w.m.wins[0].a.commands() {
		if cmd.id == id {
			if st, ok := keymap.ParseAccel(k.w.m.wins[0].a.menuAccel(cmd)); ok {
				k.stroke(st)
				return true
			}
		}
	}
	return false
}

// conflicts are the other commands whose keys are those pressed.
func (k *keysPane) conflicts() []string {
	if k.recording == nil || len(k.strokes) == 0 || len(k.w.m.wins) == 0 {
		return nil
	}
	var out []string
	for _, r := range k.rows(k.w.m.wins[0].a) {
		if r.cmd.id != k.recording.id && slices.Equal(r.keys, k.strokes) {
			out = append(out, r.cmd.label)
		}
	}
	return out
}

// recordView is the dialog that takes the keys pressed for a command. It
// fades in, growing a little to its size, as it shows.
func (k *keysPane) recordView(c *ui.Context) {
	cmd := k.recording
	if cmd == nil {
		k.shown = false
		return
	}
	p := k.w.pal
	done := func(save bool) {
		if save && len(k.strokes) > 0 {
			k.setKeys(*cmd, k.strokes)
		}
		k.recording, k.strokes = nil, nil
		// Back to the command, in the list.
		k.chosen = cmd.id
		k.listH.Focus(c)
	}
	others := k.conflicts()
	ui.Overlay(c, func() {
		back := ui.Column(c.Key("keys-record-back")).Absolute().Left(0).Right(0).Top(0).Bottom(0).
			AlignItems(ui.Center).Justify(ui.Center).Modal()
		if !k.shown {
			k.shown = true
			back.Animate("in", 0, 0)
		}
		in := back.Animate("in", 1, 140*time.Millisecond)
		back.Background(ui.RGBA(0, 0, 0, 0.28*in))
		back.Children(func() {
			box := ui.Column(c.Key("keys-record")).Width(440).MaxWidthPercent(92).Padding(22).Gap(16).Radius(12).
				Background(c.Theme().Surface).Border(1, p.border).
				Role(ui.RoleDialog).Label(locales.KeysRecordTitle(cmd.label)).Focusable().Focus().FocusRing(false).
				Opacity(in).Scale(0.96+0.04*in, 0.96+0.04*in)
			if p.dark {
				box.Shadow(0, 16, 40, 0, ui.RGBA(0, 0, 0, 0.40))
			} else {
				box.Shadow(0, 12, 32, 0, ui.RGBA(20, 20, 40, 0.16))
			}
			box.HandleInput(func(ev ui.InputEvent) bool {
				switch {
				case ev.Kind == ui.InputText:
					return true
				case ev.Kind != ui.InputKeyDown:
					return false
				case ev.Key == ui.KeyEscape && ev.Mods == 0:
					done(false)
				case ev.Key == ui.KeyEnter && ev.Mods == 0:
					done(true)
				case ev.Key == ui.KeyBackspace && ev.Mods == 0:
					k.strokes = nil
				case keymap.KnownKey(ev.Key):
					k.stroke(keymap.Stroke{Mods: ev.Mods, Key: ev.Key})
				}
				k.w.invalidate()
				return true
			})
			box.Children(func() {
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, locales.KeysRecordTitle(cmd.label)).FontSize(14).FontWeight(600).TextColor(p.text)
					ui.Text(c, cmd.id).Font(k.w.monoFont()).FontSize(11).TextColor(p.faint).SingleLine()
				})
				// The keys pressed, in a well whose edge turns as they are
				// another command's.
				edge := p.accent
				if len(others) > 0 {
					edge = p.modifiedFg
				}
				ui.Row(c.Key("keys-record-keys")).Height(68).Radius(10).AlignItems(ui.Center).Justify(ui.Center).
					Background(p.text.Alpha(0.04)).Border(1, edge.Alpha(0.7)).
					Transition(ui.ElementTransition{Colors: true, Duration: 160 * time.Millisecond}).Children(func() {
					if len(k.strokes) == 0 {
						ui.Text(c.Key("press"), locales.KeysPress()).FontSize(13).TextColor(p.faint).Transition(revealFade)
						return
					}
					k.caps(c, k.strokes, true, len(others) > 0)
				})
				if len(others) > 0 {
					ui.Row(c.Key("keys-record-conflict")).Gap(8).Padding(8, 10).Radius(7).Background(p.modified.Alpha(0.12)).
						AlignItems(ui.Center).Transition(popIn).Children(func() {
						ui.Icon(c, iconTriangleAlert).FontSize(13).TextColor(p.modifiedFg).Shrink(0)
						ui.Text(c, locales.KeysConflict(strings.Join(others, ", "))).FontSize(11.5).TextColor(p.text).Grow(1).Shrink(1)
					})
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, locales.KeysRecordHint()).FontSize(11).LineHeight(1.4).TextColor(p.muted).Grow(1).Shrink(1)
					if ui.Button(c.Key("keys-record-cancel"), locales.ButtonCancel()).Shrink(0).Clicked() {
						done(false)
					}
					if ui.PrimaryButton(c.Key("keys-record-save"), locales.KeysSave()).Shrink(0).Disabled(len(k.strokes) == 0).Clicked() {
						done(true)
					}
				})
			})
		})
	})
}

// unconditional reports whether an entry of the file binds, or takes
// away, the keys of one of names without a condition or arguments.
func unconditional(e keymap.Entry, names []string) bool {
	return e.When == "" && len(e.Args) == 0 && slices.Contains(names, strings.TrimPrefix(e.Command, "-"))
}

// defaultRemoval is the entry that takes cmd's own shortcut away, false
// when it has none.
func defaultRemoval(cmd command) (keymap.Entry, bool) {
	st, ok := keymap.ParseAccel(defaultAccel(cmd))
	if !ok {
		return keymap.Entry{}, false
	}
	return keymap.Entry{Key: st.String(), Command: "-" + cmd.id}, true
}

// setKeys gives cmd the keys in keybindings.json, in place of those it
// had, its own shortcut taken away.
func (k *keysPane) setKeys(cmd command, keys []keymap.Stroke) {
	names := commandNames(cmd.id)
	var add []keymap.Entry
	if rm, ok := defaultRemoval(cmd); ok && rm.Key != keymap.KeysString(keys) {
		add = append(add, rm)
	}
	add = append(add, keymap.Entry{Key: keymap.KeysString(keys), Command: cmd.id})
	k.edit(func(e keymap.Entry) bool { return unconditional(e, names) }, add)
}

// removeKeys takes cmd's keys away, with a toast that undoes it.
func (k *keysPane) removeKeys(cmd command) {
	var add []keymap.Entry
	if rm, ok := defaultRemoval(cmd); ok {
		add = append(add, rm)
	}
	k.replaceKeys(cmd, add, locales.KeysWereRemoved(cmd.label))
}

// resetKeys gives cmd its own shortcut back, with a toast that undoes it.
func (k *keysPane) resetKeys(cmd command) {
	k.replaceKeys(cmd, nil, locales.KeysWereReset(cmd.label))
}

// replaceKeys puts add in place of the entries that bind, or take away,
// cmd's keys without a condition, and shows message with Undo, which puts
// those entries back.
func (k *keysPane) replaceKeys(cmd command, add []keymap.Entry, message string) {
	names := commandNames(cmd.id)
	var had []keymap.Entry
	if !k.edit(func(e keymap.Entry) bool {
		if unconditional(e, names) {
			had = append(had, e)
			return true
		}
		return false
	}, add) {
		return
	}
	k.w.undoable("keys:"+cmd.id, message, func() {
		k.edit(func(e keymap.Entry) bool { return unconditional(e, names) }, had)
	})
}

// edit changes keybindings.json, as keymap.Edit does, and applies it at
// once. It reports whether it did.
func (k *keysPane) edit(drop func(keymap.Entry) bool, add []keymap.Entry) bool {
	m := k.w.m
	path := keymap.Path(m.s.Path())
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		k.w.errors = append(k.w.errors, err.Error())
		return false
	}
	out, err := keymap.Edit(data, drop, add)
	if err != nil {
		k.w.errors = append(k.w.errors, locales.KeysCouldNotEdit(err.Error()))
		return false
	}
	if err := settings.WriteFile(path, out); err != nil {
		k.w.errors = append(k.w.errors, locales.FailSave(path, err.Error()))
		return false
	}
	km, err := keymap.Parse(out)
	if len(m.wins) == 0 {
		if err == nil {
			m.keys = km
		}
	} else {
		m.reloadKeys(km, err)
	}
	k.w.invalidate()
	return true
}
