package main

import (
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
}

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
	p := w.pal
	if len(w.m.wins) == 0 {
		return
	}
	a := w.m.wins[0].a
	rows := slices.DeleteFunc(k.rows(a), func(r keyRow) bool { return !k.match(r) })
	w.head(c, "keys-head", iconKeyboard, prefSection(prefsShortcuts), func() {
		if k.byKeys {
			k.keysSearch(c)
		} else {
			ui.SearchField(c.Key("keys-search"), &k.query).Label(locales.KeysSearch()).Placeholder(locales.KeysSearch()).Width(260)
		}
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
		}
	})
	ui.Row(c.Key("keys-columns")).Padding(10, 24, 8, 24).Gap(12).AlignItems(ui.Center).Shrink(0).
		BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
		head := func(s string) ui.Element {
			return ui.Text(c, strings.ToUpper(s)).FontSize(10.5).FontWeight(600).LetterSpacing(0.7).TextColor(p.faint).SingleLine()
		}
		head(locales.KeysCommand()).Grow(1).Shrink(1)
		head(locales.KeysKeys()).Width(keysWidth).Shrink(0)
		head(locales.KeysSource()).Width(sourceWidth).Shrink(0)
		ui.Row(c).Width(actionsWidth).Shrink(0)
	})
	ui.Scroll(c.Key("keys-rows")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Padding(6, 12, 16, 12).Gap(1).Children(func() {
			if len(rows) == 0 {
				w.noneFound(c, locales.KeysNoneFound())
			}
			for _, r := range rows {
				k.row(c, r)
			}
		})
	})
}

// The widths of the columns of the shortcuts after the command's.
const (
	keysWidth    = 160
	sourceWidth  = 84
	actionsWidth = 84
)

// keysSearch is the search by keys: what is pressed in it finds the
// commands whose keys start so.
func (k *keysPane) keysSearch(c *ui.Context) {
	p := k.w.pal
	box := ui.ButtonBase(c.Key("keys-search-keys")).Width(260).Height(28).Padding(0, 10).Gap(6).Radius(6).AlignItems(ui.Center).
		Background(c.Theme().Surface).BorderWidth(1, 1, 1, 1).BorderColor(p.accent).Label(locales.KeysPressToSearch()).Focus().
		Transition(popIn)
	box.HandleInput(func(ev ui.InputEvent) bool {
		switch {
		case ev.Kind == ui.InputText:
			return true
		case ev.Kind != ui.InputKeyDown:
			return false
		case ev.Key == ui.KeyEscape && ev.Mods == 0:
			k.byKeys, k.found = false, nil
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
		k.caps(c, k.found, false)
	})
}

// caps shows keys as caps, a cap for each key, the strokes of a chord
// apart: large ones for the dialog that takes them, which pop in as they
// are pressed.
func (k *keysPane) caps(c *ui.Context, keys []keymap.Stroke, large bool) {
	p := k.w.pal
	size, h, minW, gap := float32(11), float32(20), float32(20), float32(3)
	if large {
		size, h, minW, gap = 15, 32, 32, 5
	}
	ui.Row(c).Gap(gap * 3).AlignItems(ui.Center).Shrink(0).Children(func() {
		for i, st := range keys {
			ui.Row(c.Key(i)).Gap(gap).AlignItems(ui.Center).Children(func() {
				for j, cap := range accelCaps(st.Accel(), macKeys()) {
					b := ui.Row(c.Key(j)).MinWidth(minW).Height(h).Padding(0, size/2.5).Radius(h/5).AlignItems(ui.Center).
						Justify(ui.Center).Background(p.surface).BorderWidth(1, 1, 2, 1).BorderColor(p.border)
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

// row is a command with its keys, and the buttons that change them.
func (k *keysPane) row(c *ui.Context, r keyRow) {
	p := k.w.pal
	row := ui.ButtonBase(c.Key("keys-row-"+r.cmd.id)).Padding(7, 12).Gap(12).Radius(7).AlignItems(ui.Center).Label(r.cmd.label)
	hovered := row.Hovered()
	if hovered {
		row.Background(p.hover)
	}
	row.Transition(hoverFade)
	row.Children(func() {
		ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Text(c, r.cmd.label).FontSize(12.5).TextColor(p.text).SingleLine()
			ui.Text(c, r.cmd.id).Font(k.w.monoFont()).FontSize(10.5).TextColor(p.faint).SingleLine()
		})
		ui.Row(c).Width(keysWidth).Shrink(0).Gap(6).AlignItems(ui.Center).Children(func() {
			if len(r.keys) > 0 {
				k.caps(c, r.keys, false)
			} else {
				ui.Text(c, "—").FontSize(12).TextColor(p.faint)
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
			if !hovered {
				return
			}
			k.action(c, "change:"+r.cmd.id, iconPencil, locales.KeysChange(), true, func() { k.record(r.cmd) })
			k.action(c, "remove:"+r.cmd.id, iconMinus, locales.KeysRemove(), len(r.keys) > 0, func() { k.removeKeys(r.cmd) })
			k.action(c, "reset:"+r.cmd.id, iconUndo, locales.KeysReset(), r.source == keyUser || r.source == keyRemoved, func() { k.resetKeys(r.cmd) })
		})
	})
	if row.Clicked() {
		k.record(r.cmd)
	}
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
					k.caps(c, k.strokes, true)
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

// removeKeys takes cmd's keys away.
func (k *keysPane) removeKeys(cmd command) {
	names := commandNames(cmd.id)
	var add []keymap.Entry
	if rm, ok := defaultRemoval(cmd); ok {
		add = append(add, rm)
	}
	k.edit(func(e keymap.Entry) bool { return unconditional(e, names) }, add)
}

// resetKeys gives cmd its own shortcut back.
func (k *keysPane) resetKeys(cmd command) {
	names := commandNames(cmd.id)
	k.edit(func(e keymap.Entry) bool { return unconditional(e, names) }, nil)
}

// edit changes keybindings.json, as keymap.Edit does, and applies it at
// once.
func (k *keysPane) edit(drop func(keymap.Entry) bool, add []keymap.Entry) {
	m := k.w.m
	path := keymap.Path(m.s.Path())
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		k.w.errors = append(k.w.errors, err.Error())
		return
	}
	out, err := keymap.Edit(data, drop, add)
	if err != nil {
		k.w.errors = append(k.w.errors, locales.KeysCouldNotEdit(err.Error()))
		return
	}
	if err := settings.WriteFile(path, out); err != nil {
		k.w.errors = append(k.w.errors, locales.FailSave(path, err.Error()))
		return
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
}
