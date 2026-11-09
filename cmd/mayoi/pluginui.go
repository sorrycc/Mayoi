package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/plugin"
	"mayoi/locales"
)

// handlerRef is a function of a plugin's, as its tree names it: the
// plugin is called back with the id.
type handlerRef struct {
	H string `json:"$h"`
}

// callHandler calls a plugin's function back with a value, without
// waiting for it.
func (h *pluginHost) callHandler(p *plugin.Plugin, ref *handlerRef, value any) {
	if ref == nil || ref.H == "" || p == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), plugin.BusyLimit)
		defer cancel()
		if _, err := p.Request(ctx, "handler", map[string]any{"id": ref.H, "value": value}); err != nil {
			p.Request(context.Background(), "event", map[string]any{"name": "", "data": nil})
		}
	}()
}

// pluginStatus is an item of the status bar a plugin filled.
type pluginStatus struct {
	plugin   *plugin.Plugin
	id       string
	right    bool
	priority int

	Text    string      `json:"text"`
	Tooltip string      `json:"tooltip"`
	Icon    string      `json:"icon"`
	Command string      `json:"command"`
	OnClick *handlerRef `json:"onClick"`
}

// setStatus fills an item of the status bar a plugin declares, or hides it
// for null.
func (h *pluginHost) setStatus(p *plugin.Plugin, id string, raw json.RawMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	defer h.redraw()
	if len(raw) == 0 || string(raw) == "null" {
		delete(h.status, id)
		return
	}
	var st pluginStatus
	if json.Unmarshal(raw, &st) != nil {
		return
	}
	st.plugin, st.id, st.right = p, id, true
	for _, c := range p.Contributes.StatusItems {
		if c.ID == id {
			st.right, st.priority = c.Align != "left", c.Priority
		}
	}
	h.status[id] = st
}

// pluginStatusItems puts the plugins' items of one side in the status bar,
// with item, and the work they said they do.
func (a *app) pluginStatusItems(c *ui.Context, right bool, item func(key, label, tip string, icon *ui.SVG, fn func()), text func(string) ui.Element) {
	h := plugins
	if h == nil {
		return
	}
	h.mu.Lock()
	var items []pluginStatus
	for _, st := range h.status {
		if st.right == right && st.plugin.Started() {
			items = append(items, st)
		}
	}
	progress := slices.Clone(h.progress)
	h.mu.Unlock()
	sort.Slice(items, func(i, j int) bool {
		if items[i].priority != items[j].priority {
			return items[i].priority > items[j].priority
		}
		return items[i].id < items[j].id
	})
	if !right {
		for i, title := range progress {
			ui.Row(c.Key("status:plugin-progress:"+fmt.Sprint(i))).Height(statusBarH).Padding(0, 6).Gap(5).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(11, 11)
				text(title)
			})
		}
		a.diagnosticsStatus(item)
	}
	for _, st := range items {
		var icon *ui.SVG
		if st.Icon != "" {
			icon = namedIcon(st.Icon)
		}
		var fn func()
		switch {
		case st.OnClick != nil:
			fn = func() { h.callHandler(st.plugin, st.OnClick, nil) }
		case st.Command != "":
			cmd := st.Command
			fn = func() { a.runAny(cmd) }
		}
		item("plugin:"+st.id, st.Text, st.Tooltip, icon, fn)
	}
}

// runAny runs a command of Mayoi's or of a plugin's by its id.
func (a *app) runAny(id string) {
	if plugins != nil && plugins.reg.CommandPlugin(id) != nil {
		plugins.runCommand(a, id, "")
		return
	}
	a.run(id)
}

// diagnostic is a problem a plugin found in a file.
type diagnostic struct {
	Range    textRange `json:"range"`
	Severity string    `json:"severity"`
	Message  string    `json:"message"`
	Code     string    `json:"code"`
	source   string
}

// severityRank orders diagnostics, the worst first.
func severityRank(s string) int {
	switch s {
	case "error", "":
		return 0
	case "warning":
		return 1
	case "info":
		return 2
	}
	return 3
}

// setDiagnostics replaces a plugin's diagnostics of a file, or clears all
// of the plugin's.
func (h *pluginHost) setDiagnostics(owner, path, source string, clear bool, items json.RawMessage) {
	var list []diagnostic
	json.Unmarshal(items, &list)
	h.setDiagnosticList(owner, path, source, clear, list)
}

func (h *pluginHost) setDiagnosticList(owner, path, source string, clear bool, list []diagnostic) {
	if source == "" {
		source = owner
	}
	for i := range list {
		list[i].source = source
	}
	h.mu.Lock()
	if clear {
		for p, by := range h.diags {
			delete(by, owner)
			if len(by) == 0 {
				delete(h.diags, p)
			}
		}
	} else if path != "" {
		by := h.diags[path]
		if by == nil {
			by = map[string][]diagnostic{}
			h.diags[path] = by
		}
		if len(list) == 0 {
			delete(by, owner)
			if len(by) == 0 {
				delete(h.diags, path)
			}
		} else {
			by[owner] = list
		}
	}
	h.diagsGen++
	h.mu.Unlock()
	h.redraw()
}

// diagnosticsOf are the diagnostics of a file, of every plugin, the worst
// first, then by place.
func (h *pluginHost) diagnosticsOf(path string) []diagnostic {
	if h == nil || path == "" {
		return nil
	}
	h.mu.Lock()
	var out []diagnostic
	for _, list := range h.diags[path] {
		out = append(out, list...)
	}
	h.mu.Unlock()
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := out[i].Range.Start, out[j].Range.Start; a.Line != b.Line {
			return a.Line < b.Line
		}
		return severityRank(out[i].Severity) < severityRank(out[j].Severity)
	})
	return out
}

// diagRun is a diagnostic's run on a line in view, in runes of the line.
type diagRun struct {
	lineRun
	severity string
}

// diagRuns are where the diagnostics of a document are on lines first up
// to last.
func diagRuns(d *document, snap interface {
	LineCount() int
	Line(int) string
}, first, last int) []diagRun {
	if plugins == nil || d.path == "" {
		return nil
	}
	var out []diagRun
	for _, dg := range plugins.diagnosticsOf(d.path) {
		s, e := dg.Range.Start, dg.Range.End
		if e.Line < s.Line || (e.Line == s.Line && e.Character < s.Character) {
			s, e = e, s
		}
		for l := max(s.Line, first); l <= min(e.Line, last, snap.LineCount()-1); l++ {
			text := snap.Line(l)
			n := len([]rune(text))
			from, to := 0, n
			if l == s.Line {
				from = utf16ToRunes(text, s.Character)
			}
			if l == e.Line {
				to = utf16ToRunes(text, e.Character)
			}
			if to <= from {
				// An empty range marks the word, or the line's end.
				to = min(from+1, n)
				if to <= from {
					from, to = max(n-1, 0), n
				}
			}
			out = append(out, diagRun{lineRun{l, from, to}, dg.Severity})
		}
	}
	return out
}

// utf16ToRunes is the rune offset in s of a UTF-16 offset.
func utf16ToRunes(s string, units int) int {
	n, runes := 0, 0
	for _, r := range s {
		if n >= units {
			break
		}
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
		runes++
	}
	return runes
}

// diagColor is the color of a severity's underline.
func (p *palette) diagColor(severity string) ui.Color {
	switch severity {
	case "warning":
		return p.modified
	case "info", "hint":
		return p.accent
	}
	return p.danger
}

// paintSquiggle draws a wavy underline from x0 to x1 at the bottom of a
// line at y.
func paintSquiggle(pt *ui.Painter, x0, x1, y float32, col ui.Color) {
	var path ui.Path
	const step = 2.5
	up := true
	path.MoveTo(x0, y)
	for x := x0; x < x1; x += step {
		dy := float32(1.5)
		if up {
			dy = -1.5
		}
		path.LineTo(min(x+step, x1), y+dy)
		up = !up
	}
	pt.StrokePath(&path, 1, col)
}

// diagnosticsStatus is the status bar's count of the problems of the
// files, which opens Problems, and the message of the one at the caret.
func (a *app) diagnosticsStatus(item func(key, label, tip string, icon *ui.SVG, fn func())) {
	h := plugins
	if h == nil {
		return
	}
	errs, warns := 0, 0
	h.mu.Lock()
	for _, by := range h.diags {
		for _, list := range by {
			for _, dg := range list {
				switch severityRank(dg.Severity) {
				case 0:
					errs++
				case 1:
					warns++
				}
			}
		}
	}
	h.mu.Unlock()
	if errs+warns > 0 {
		item("problems", fmt.Sprintf("%d  %d", errs, warns), a.withKey(locales.ProblemsShow(), "plugins.problems"), iconTriangleAlert, a.showProblems)
	}
	if d := a.activeEditor(); d != nil {
		snap := d.buf.Snapshot()
		caretLine := snap.LineAt(min(d.caret, snap.Len()))
		for _, dg := range h.diagnosticsOf(d.path) {
			if caretLine >= dg.Range.Start.Line && caretLine <= dg.Range.End.Line {
				item("problem-here", truncate(firstLine(dg.Message), 90), dg.source+": "+dg.Message, nil, nil)
				break
			}
		}
	}
}

// showProblems lists the problems of every file in a box, to go to one.
func (a *app) showProblems() {
	h := plugins
	if h == nil {
		return
	}
	h.mu.Lock()
	paths := make([]string, 0, len(h.diags))
	for p := range h.diags {
		paths = append(paths, p)
	}
	h.mu.Unlock()
	sort.Strings(paths)
	type problem struct {
		path string
		dg   diagnostic
	}
	var probs []problem
	var items []pickItem
	for _, p := range paths {
		for _, dg := range h.diagnosticsOf(p) {
			probs = append(probs, problem{p, dg})
			icon := "error"
			switch severityRank(dg.Severity) {
			case 1:
				icon = "warning"
			case 2, 3:
				icon = "info"
			}
			items = append(items, pickItem{Label: firstLine(dg.Message), Icon: icon,
				Description: fmt.Sprintf("%s:%d", a.relOrPath(p), dg.Range.Start.Line+1), Detail: dg.source})
		}
	}
	if len(items) == 0 {
		a.tell(note{message: locales.ProblemsNone()})
		return
	}
	a.openPluginPicker(items, locales.ProblemsPlaceholder(), func(i int) {
		if i < 0 {
			return
		}
		pr := probs[i]
		a.openFileAt(pr.path, pr.dg.Range.Start.Line, true)
		if d := a.activeEditor(); d != nil && d.path == pr.path {
			snap := d.buf.Snapshot()
			at := posRune(snap, pr.dg.Range.Start)
			d.selectRange(at, at, true)
		}
	})
}

// formatterFor is the formatter of a document's language: the one
// formatOnSave or formatters names for it, or the first a plugin offers.
func (a *app) formatterFor(d *document) (*plugin.Plugin, string) {
	if plugins == nil {
		return nil, ""
	}
	lang := languageOf(d)
	want := a.prefs().Formatters[lang]
	var first *plugin.Plugin
	firstID := ""
	for _, p := range plugins.enabled() {
		for _, f := range p.Contributes.Formatters {
			if !slices.Contains(f.Languages, lang) && !slices.Contains(f.Languages, "*") {
				continue
			}
			if want != "" && (f.ID == want || p.Name == want) {
				return p, f.ID
			}
			if first == nil {
				first, firstID = p, f.ID
			}
		}
	}
	if want != "" {
		return nil, ""
	}
	return first, firstID
}

// format formats a document with its formatter, within limit, and calls
// then on the main thread, formatted or not.
func (a *app) format(d *editor, limit time.Duration, quiet bool, then func()) {
	p, id := a.formatterFor(d.document)
	if p == nil && a.lspFormat(d, limit, then) {
		return
	}
	if p == nil {
		if !quiet {
			a.fail("%s", locales.FormatNone(languageOf(d.document)))
		}
		if then != nil {
			then()
		}
		return
	}
	snap := d.buf.Snapshot()
	version := snap.Version()
	doc := docJSON(d.document)
	opts := map[string]any{"tabSize": max(d.indent.size, 1), "insertSpaces": !d.indent.tabs}
	if d.indent.tabs {
		opts["tabSize"] = tabWidth
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), limit)
		defer cancel()
		res, err := p.Request(ctx, "format", map[string]any{"id": id, "document": doc, "options": opts})
		a.post(func() {
			defer func() {
				if then != nil {
					then()
				}
			}()
			if err != nil {
				if !quiet || !errors.Is(err, context.DeadlineExceeded) {
					a.fail("%s", locales.FormatFailed(p.Title(), firstLine(err.Error())))
				}
				return
			}
			if d.buf.Version() != version {
				return // typed since: the result is of other text
			}
			a.applyFormat(d, snap, res)
		})
	}()
}

// applyFormat makes what a formatter returned: the whole new text, or
// edits, as one step of undo.
func (a *app) applyFormat(d *editor, snap ui.TextSnapshot, res json.RawMessage) {
	var text string
	if json.Unmarshal(res, &text) == nil {
		if text == "" || text == snap.String() {
			return
		}
		// The least of the text that changed, so the caret and the marks
		// keep their places around it.
		old := snap.String()
		s, e1, e2 := commonEnds(old, text)
		edit := textEdit{len([]rune(old[:s])), len([]rune(old[:e1])), text[s:e2]}
		sel := d.sel
		moved := func(x int) int { return movedBy(x, edit.start, edit.end, len([]rune(edit.text))) }
		d.edit([2]int{moved(sel[0]), moved(sel[1])}, edit)
		return
	}
	var edits []struct {
		Range textRange `json:"range"`
		Text  string    `json:"text"`
	}
	if json.Unmarshal(res, &edits) != nil || len(edits) == 0 {
		return
	}
	var tes []textEdit
	for _, e := range edits {
		s, en := posRune(snap, e.Range.Start), posRune(snap, e.Range.End)
		tes = append(tes, textEdit{min(s, en), max(s, en), e.Text})
	}
	if editsOverlap(tes) {
		return
	}
	sel := d.sel
	moved := func(x int) int {
		for _, e := range sortedEdits(tes) {
			x = movedBy(x, e.start, e.end, len([]rune(e.text)))
		}
		return x
	}
	d.edit([2]int{moved(sel[0]), moved(sel[1])}, tes...)
}

// commonEnds finds where a and b first differ, s, and where their common
// ends start in each, e1 and e2: b[s:e2] replaces a[s:e1]. Offsets are
// of bytes, on rune boundaries.
func commonEnds(a, b string) (s, e1, e2 int) {
	for s < len(a) && s < len(b) && a[s] == b[s] {
		s++
	}
	for s > 0 && !isRuneStart(a, s) {
		s--
	}
	e1, e2 = len(a), len(b)
	for e1 > s && e2 > s && a[e1-1] == b[e2-1] {
		e1--
		e2--
	}
	for e1 < len(a) && !isRuneStart(a, e1) {
		e1++
		e2++
	}
	return s, e1, e2
}

func isRuneStart(s string, i int) bool { return i >= len(s) || s[i]&0xC0 != 0x80 }

// formatActive formats the document shown.
func (a *app) formatActive() {
	if d := a.textEditor(); d != nil {
		a.format(d, 10*time.Second, false, nil)
	}
}

// formatsOnSave reports whether saving a document formats it first.
func (a *app) formatsOnSave(d *document) bool {
	if plugins == nil || d.path == "" {
		return false
	}
	return a.prefs().FormatOnSave[languageOf(d)] || a.prefs().FormatOnSave["*"]
}

// pluginLanguageOf is the language a plugin declares for a file, by its
// name or extension, nil for none.
func pluginLanguageOf(path string) *plugin.LanguageContrib {
	if plugins == nil || path == "" {
		return nil
	}
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(base))
	for _, p := range plugins.enabled() {
		for i, l := range p.Contributes.Languages {
			if slices.Contains(l.Filenames, base) {
				return &p.Contributes.Languages[i]
			}
			for _, e := range l.Extensions {
				if strings.ToLower(e) == ext {
					return &p.Contributes.Languages[i]
				}
			}
		}
	}
	return nil
}

// pluginLexer is the lexer a plugin's language names for a file, "" for
// none: highlight.LexerName.
func pluginLexer(path string) string {
	if l := pluginLanguageOf(path); l != nil {
		return l.Lexer
	}
	return ""
}

// pluginComment is the line comment a plugin's language has, "" for none.
func pluginComment(path string) string {
	if l := pluginLanguageOf(path); l != nil {
		return l.Comment
	}
	return ""
}

// pluginLanguage is the language a plugin declares for a file, by its
// name or extension, "" for none.
func pluginLanguage(path string) string {
	if l := pluginLanguageOf(path); l != nil {
		return l.ID
	}
	return ""
}
