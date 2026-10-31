package main

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/symbols"
	"mayoi/internal/workspace"
	"mayoi/locales"
)

// quickOpen is the state of the quick open box: files by fuzzy name,
// ":n" for a line of the document shown, or "@" for its symbols.
type quickOpen struct {
	syms    symbolCache
	open    bool
	query   string
	results []workspace.Match
	sel     int
	shown   string // the query the results are of
	stale   bool
	moved   bool     // the choice moved by keys, to scroll into view
	used    []string // the commands the palette ran last, the last first
	// shook is when Return found nothing to open, which shakes the box.
	shook time.Time
}

func (a *app) openQuick(query string) {
	q := &a.quick
	q.open, q.query, q.sel, q.stale = true, query, 0, true
	a.indexStale = a.indexStale || a.index == nil
	a.refreshIndex()
}

func (a *app) closeQuick() {
	a.quick.open = false
	if a.focus != paneTerminal {
		a.focusColumn()
	}
}

// gotoLine parses ":n" or ":n:c", 1-based.
func gotoLine(query string) (line, col int, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(query), ":")
	if !found {
		return 0, 0, false
	}
	parts := strings.SplitN(rest, ":", 2)
	n, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, rest == ""
	}
	if len(parts) == 2 {
		col, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
	}
	return n, col, true
}

func (a *app) quickOpenView(c *ui.Context) {
	q := &a.quick
	if !q.open {
		return
	}
	p := a.pal
	line, col, isLine := gotoLine(q.query)
	symQuery, isSym := strings.CutPrefix(strings.TrimLeft(q.query, " "), "@")
	if isSym {
		a.quickSymbols(c, symQuery)
		return
	}
	if cmdQuery, isCmd := strings.CutPrefix(strings.TrimLeft(q.query, " "), ">"); isCmd {
		a.quickCommands(c, cmdQuery)
		return
	}
	if !isLine && (q.stale || q.shown != q.query) {
		q.results = workspace.Fuzzy(a.index, q.query, 60)
		if strings.TrimSpace(q.query) == "" {
			q.results = a.recentFirst(q.results)
		}
		q.shown, q.stale = q.query, false
		q.sel = 0
	}
	// choose opens a file of the results, to the side with side.
	choose := func(i int, side bool) {
		if i < 0 || i >= len(q.results) {
			q.shook = c.Now()
			return
		}
		rel := a.index[q.results[i].Index]
		q.open = false
		if side {
			a.openFileToSide(inDir(a.root, rel))
		} else {
			a.openFile(inDir(a.root, rel), true)
		}
	}
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "quick")
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(600).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Radius(10), true).Clip()
			shake(c, box, q.shook)
			box.
				Role(ui.RoleDialog).Label(locales.QuickOpenLabel())
			box.Children(func() {
				ui.Row(c).Padding(10, 14).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
					ui.Icon(c, iconSearch).FontSize(15).TextColor(p.muted)
					// With a folder, the footer shows the prefixes that
					// change what is searched.
					placeholder := locales.QuickSearchFiles()
					if a.root == "" {
						placeholder = locales.QuickTypeForCommands()
					}
					in := a.textKeys(ui.TextInputBase(c.Key("quick-input"), &q.query)).Grow(1).FontSize(14).AutoFocus().
						Placeholder(placeholder).Label(locales.QuickOpenLabel())
					if in.Shortcut(0, ui.KeyDown) {
						q.sel, q.moved = min(q.sel+1, len(q.results)-1), true
					}
					if in.Shortcut(0, ui.KeyUp) {
						q.sel, q.moved = max(q.sel-1, 0), true
					}
					if !isLine && in.Shortcut(ui.Cmd, ui.KeyEnter) {
						choose(q.sel, true)
					} else if in.Submitted() {
						if isLine {
							if line > 0 {
								q.open = false
								a.goToLine(line, col)
							} else {
								q.shook = c.Now()
							}
						} else {
							choose(q.sel, false)
						}
					}
				})
				a.quickBody(c, isLine, line, col, choose)
				// The prefixes are of use before a query, the side after.
				typed := strings.TrimSpace(q.query) != ""
				a.quickFooter(c, isLine, locales.QuickOpenAction(), "", a.root != "" && !typed, typed && len(q.results) > 0)

			})
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeQuick()
			}
		})
	})
}

// quickBody is what quick open lists: the files found, or what going to
// a line does.
func (a *app) quickBody(c *ui.Context, isLine bool, line, col int, choose func(int, bool)) {
	q := &a.quick
	p := a.pal
	if isLine {
		var hint string
		if d := a.activeEditor(); d == nil {
			hint = locales.QuickLineNoFile()
		} else if line > 0 {
			hint = locales.QuickGoToLineN(line)
			if col > 0 {
				hint = locales.QuickGoToLineCol(line, col)
			}
		} else {
			hint = locales.QuickLineRange(d.buf.LineCount())
		}
		ui.Text(c, hint).FontSize(13).TextColor(p.muted).Padding(12, 16)
		return
	}
	if len(q.results) == 0 {
		msg := locales.QuickNoFiles()
		switch {
		case a.root == "":
			msg = locales.QuickNoFolder()
		case a.indexing && a.index == nil:
			msg = locales.QuickIndexing()
		}
		ui.Text(c, msg).FontSize(13).TextColor(p.muted).Padding(12, 16)
		return
	}
	ui.Scroll(c.Key("quick-results")).MaxHeight(380).Padding(6).Children(func() {
		for i, m := range q.results {
			rel := a.index[m.Index]
			row := ui.ButtonBase(c.Key(rel)).Padding(5, 10).Gap(10).Radius(6).Justify(ui.Start).AlignItems(ui.Center).Label(rel)
			if i != q.sel && row.Hovered() {
				row.Background(p.hover)
			}
			if i == q.sel && q.moved {
				row.ScrollIntoView()
			}
			if row.Clicked() {
				choose(i, false)
			}
			row.Transition(hoverFade)
			row.Children(func() { a.pickMark(c, i == q.sel, "quick"); a.quickRow(c, rel, m.Positions) })
		}
	})
	q.moved = false
}

// quickFooter shows the keys of quick open, and at its end a tip, or with
// prefixes the prefixes that make quick open search something else, or
// with side the key that opens a file to the side.
func (a *app) quickFooter(c *ui.Context, isLine bool, open, tip string, prefixes, side bool) {
	p := a.pal
	ui.Row(c).Height(30).Padding(0, 14).Gap(14).AlignItems(ui.Center).Background(p.hover).
		BorderWidth(1, 0, 0, 0).BorderColor(p.border).Children(func() {
		hint := func(keys, label string) {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				a.keycaps(c, keys)
				ui.Text(c, label).FontSize(11.5).TextColor(p.muted).SingleLine()
			})
		}
		if isLine {
			hint("⏎", locales.QuickGoToLine())
		} else {
			hint("↑↓", locales.QuickNavigate())
			hint("⏎", open)
		}
		hint("esc", locales.QuickClose())
		ui.Spacer(c)
		switch {
		case isLine:
		case tip != "":
			ui.Text(c, tip).FontSize(11.5).TextColor(p.faint).SingleLine()
		case side:
			hint(keyText("CmdOrCtrl+Enter"), locales.QuickOpenToSide())
		case prefixes:
			hint(">", locales.QuickCommands())
			hint(":", locales.QuickLine())
			hint("@", locales.QuickSymbol())
		}
	})
}

// quickRow shows a file of the results: its name with the characters the
// query matched in the accent color, and its folder.
func (a *app) quickRow(c *ui.Context, rel string, positions []int) {
	p := a.pal
	base := strings.LastIndexByte(rel, '/') + 1
	icon, col := fileIcon(p, rel[base:])
	ui.Icon(c, icon).FontSize(15).TextColor(col)
	ui.RichText(c, a.matchSpans(rel[base:], base, positions, p.text)...).FontSize(13).SingleLine().Shrink(0)
	if dir := path.Dir(rel); dir != "." {
		ui.RichText(c, a.matchSpans(dir, 0, positions, p.faint)...).FontSize(12).SingleLine().Shrink(1)
	}
}

// matchSpans is s, which starts at from of the text matched, with the
// characters at positions in the accent color. The positions are byte
// offsets, in order: walk them along the text.
func (a *app) matchSpans(s string, from int, positions []int, normal ui.Color) []ui.Span {
	p := a.pal
	out := make([]ui.Span, 0, 4)
	k := 0
	for i, r := range s {
		for k < len(positions) && positions[k] < from+i {
			k++
		}
		on := k < len(positions) && positions[k] == from+i
		if n := len(out); n > 0 && (out[n-1].Weight == 650) == on {
			out[n-1].Text += string(r)
			continue
		}
		sp := ui.Span{Text: string(r), Color: normal}
		if on {
			sp.Color, sp.Weight = p.accent, 650
		}
		out = append(out, sp)
	}
	return out
}

// goToLine moves the caret to a line (and column) of the document shown,
// 1-based.
func (a *app) goToLine(line, col int) {
	d := a.textEditor()
	if d == nil {
		return
	}
	snap := d.buf.Snapshot()
	l := max(0, min(line-1, snap.LineCount()-1))
	s, e := snap.LineRange(l)
	at := min(s+max(col-1, 0), e)
	d.selectRange(at, at, true)
	d.lightLanding()
	d.vscroll.Focus()
}

// recentFirst puts the files shown last before the others, as quick open
// lists them before anything is typed, the one shown now after the rest of
// them, as it is the one the user is leaving.
func (a *app) recentFirst(results []workspace.Match) []workspace.Match {
	// Find the recent files in the index, without a map of all of it.
	at := make(map[string]int, len(a.recent))
	for _, p := range a.recent {
		at[a.tree.Rel(p)] = -1
	}
	for i, f := range a.index {
		if j, ok := at[f]; ok && j < 0 {
			at[f] = i
		}
	}
	for f, i := range at {
		if i < 0 {
			delete(at, f)
		}
	}
	var out []workspace.Match
	seen := map[int]bool{}
	for n, p := range a.recent {
		if n == 0 && len(a.recent) > 1 {
			continue
		}
		if i, ok := at[a.tree.Rel(p)]; ok && !seen[i] {
			out = append(out, workspace.Match{Index: i})
			seen[i] = true
		}
	}
	if len(a.recent) > 1 {
		if i, ok := at[a.tree.Rel(a.recent[0])]; ok {
			out = append(out, workspace.Match{Index: i})
			seen[i] = true
		}
	}
	for _, m := range results {
		if !seen[m.Index] {
			out = append(out, m)
		}
	}
	return out[:min(len(out), 60)]
}

// quickSymbols is quick open going to a symbol of the document shown: its
// declarations, matched by name, in the order of the document until a name
// is typed.
func (a *app) quickSymbols(c *ui.Context, query string) {
	q := &a.quick
	p := a.pal
	syms, names := a.documentSymbols()
	key := "@" + query
	if q.stale || q.shown != key {
		q.results = workspace.Fuzzy(names, query, 200)
		q.shown, q.stale, q.sel = key, false, 0
	}
	choose := func(i int) {
		if i < 0 || i >= len(q.results) {
			q.shook = c.Now()
			return
		}
		q.open = false
		if d := a.activeEditor(); d != nil {
			sym := syms[q.results[i].Index]
			a.editActive(func(d *editor) {
				// To the line, with the name selected.
				a.jumpToScope(d, sym.Line)
				s, _ := d.buf.Snapshot().LineRange(sym.Line)
				d.selectRange(s+sym.Col, s+sym.Col+utf8.RuneCountInString(sym.Name), false)
			})()
		}
	}
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "quick")
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(600).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Radius(10), true).Clip()
			shake(c, box, q.shook)
			box.
				Role(ui.RoleDialog).Label(locales.SymbolLabel())
			box.Children(func() {
				ui.Row(c).Padding(10, 14).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
					ui.Icon(c, iconHash).FontSize(15).TextColor(p.muted)
					in := a.textKeys(ui.TextInputBase(c.Key("quick-input"), &q.query)).Grow(1).FontSize(14).AutoFocus().
						Placeholder(locales.SymbolPlaceholder()).Label(locales.SymbolLabel())
					if in.Shortcut(0, ui.KeyDown) {
						q.sel, q.moved = min(q.sel+1, len(q.results)-1), true
					}
					if in.Shortcut(0, ui.KeyUp) {
						q.sel, q.moved = max(q.sel-1, 0), true
					}
					if in.Submitted() {
						choose(q.sel)
					}
				})
				switch {
				case a.activeEditor() == nil:
					ui.Text(c, locales.SymbolNoFile()).FontSize(13).TextColor(p.muted).Padding(12, 16)
				case len(q.results) == 0:
					ui.Text(c, locales.SymbolNoMatches()).FontSize(13).TextColor(p.muted).Padding(12, 16)
				default:
					ui.Scroll(c.Key("quick-symbols")).MaxHeight(380).Padding(6).Children(func() {
						for i, m := range q.results {
							sym := syms[m.Index]
							row := ui.ButtonBase(c.Key(fmt.Sprint("sym:", sym.Line))).Padding(5, 10).Gap(10).Radius(6).
								Justify(ui.Start).AlignItems(ui.Center).Label(sym.Name)
							if i != q.sel && row.Hovered() {
								row.Background(p.hover)
							}
							if i == q.sel && q.moved {
								row.ScrollIntoView()
							}
							if row.Clicked() {
								choose(i)
							}
							row.Transition(hoverFade)
							row.Children(func() { a.pickMark(c, i == q.sel, "symbols"); a.symbolRow(c, sym, m.Positions, query == "") })
						}
					})
					q.moved = false
				}
				a.quickFooter(c, false, locales.QuickGoToAction(), "", false, false)
			})
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeQuick()
			}
		})
	})
}

// symbolRow shows a symbol of the results: its kind, its name with the
// characters the query matched in the accent color, its declaration and
// its line. Listed in order, symbols are indented as in the document.
func (a *app) symbolRow(c *ui.Context, sym symbols.Symbol, positions []int, nested bool) {
	p := a.pal
	if nested && sym.Depth > 0 {
		ui.Box(c).Width(float32(min(sym.Depth, 24)) * 4).Shrink(0)
	}
	icon, col := a.symbolIcon(sym.Kind)
	ui.Icon(c, icon).FontSize(14).TextColor(col)
	var spans []ui.Span
	k := 0
	for i, r := range sym.Name {
		for k < len(positions) && positions[k] < i {
			k++
		}
		on := k < len(positions) && positions[k] == i
		if n := len(spans); n > 0 && (spans[n-1].Weight == 650) == on {
			spans[n-1].Text += string(r)
			continue
		}
		sp := ui.Span{Text: string(r), Color: p.text}
		if on {
			sp.Color, sp.Weight = p.accent, 650
		}
		spans = append(spans, sp)
	}
	ui.RichText(c, spans...).FontSize(13).SingleLine().Shrink(0)
	ui.Text(c, sym.Detail).FontSize(12).Font(a.font.family).TextColor(p.faint).SingleLine().Shrink(1).Grow(1).MinWidth(0)
	ui.Text(c, fmt.Sprint(sym.Line+1)).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(0)
}
