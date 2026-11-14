package main

import (
	"crypto/rand"
	"encoding/json"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"mayoi/internal/bg"
	"mayoi/internal/vfs"
	"mayoi/internal/workspace"
	"mayoi/locales"

	"github.com/egoist/mygo/ui"
)

// Typing [[ in a note suggests what to link to, as Obsidian does: the
// files of the vault, then, after #, the headings of the note named, or
// of the note itself for [[#, and after ^ its blocks. The box follows the
// caret under the link; ↑ and ↓ choose, Enter or Tab puts the link in
// place, Shift-Enter keeps it as typed, Escape closes the box. A link
// to a file is written as the vault's settings say, its shortest path by
// default. The suggestions are found away from the UI thread.

// wikiQuery is a wiki link being typed, by rune offsets of the text.
type wikiQuery struct {
	// from is just after its [[, caret where the caret is, and to where
	// what it names ends: at its |, its ]], or the caret.
	from, caret, to int
	// close is where its ]] is, -1 for none.
	close int
	embed bool
	// text is what is typed of it, up to the caret.
	text string
}

// wikiQueryIn finds the wiki link line is typing at rune col, which is
// not in code.
func wikiQueryIn(line []rune, col int) (wikiQuery, bool) {
	var q wikiQuery
	if col > len(line) {
		return q, false
	}
	open := -1
	for i := col - 1; i >= 1; i-- {
		r := line[i]
		if r == ']' || r == '|' {
			return q, false
		}
		if r == '[' {
			if line[i-1] != '[' {
				return q, false
			}
			open = i + 1
			break
		}
	}
	if open < 0 {
		return q, false
	}
	// A ` before it, unclosed, puts it in code.
	if strings.Count(string(line[:open-2]), "`")%2 == 1 {
		return q, false
	}
	q.from, q.caret, q.to, q.close = open, col, col, -1
	q.embed = open >= 3 && line[open-3] == '!'
	q.text = string(line[open:col])
	for j := col; j < len(line); j++ {
		r := line[j]
		if r == '[' {
			break
		}
		if r == ']' {
			if j+1 < len(line) && line[j+1] == ']' {
				if q.to == col {
					q.to = j
				}
				q.close = j
			}
			break
		}
		if r == '|' && q.to == col {
			q.to = j
		}
	}
	if q.close < 0 {
		// Without its ]], it ends at the caret, whatever follows.
		q.to = col
	}
	return q, true
}

// wikiMode is what a wiki link being typed asks to be suggested.
type wikiMode uint8

const (
	wikiFilesMode wikiMode = iota
	wikiHeadingsMode
	wikiBlocksMode
)

// wikiAsk splits what is typed of a wiki link into the note it names,
// what it asks for, and the query of that.
func wikiAsk(text string) (target string, mode wikiMode, query string) {
	if i := strings.IndexByte(text, '#'); i >= 0 {
		rest := text[i+1:]
		if q, ok := strings.CutPrefix(rest, "^"); ok {
			return text[:i], wikiBlocksMode, q
		}
		if j := strings.LastIndexByte(rest, '#'); j >= 0 {
			rest = rest[j+1:]
		}
		return text[:i], wikiHeadingsMode, rest
	}
	if i := strings.IndexByte(text, '^'); i >= 0 {
		return text[:i], wikiBlocksMode, text[i+1:]
	}
	return text, wikiFilesMode, text
}

// wikiItem is a suggestion: what goes between the brackets, how it shows,
// and, for a block without an id, the id to give it, on line of file.
type wikiItem struct {
	link  string
	title string
	// name is the file's name, with its extension, for its icon.
	name      string
	detail    string
	mode      wikiMode
	positions []int
	// For a block without an id: file "" for the note itself.
	newID    string
	file     string
	line     int
	lineText string
}

// wikiSuggest is the box of suggestions of the editor typing a wiki link.
type wikiSuggest struct {
	d    *editor
	open bool
	q    wikiQuery
	// key is what the items are for, and asked what they are being
	// found for.
	key, asked string
	items      []wikiItem
	sel        int
	moved      bool
	loading    bool
	// gen counts the asks; running is set while one is being answered,
	// and again when another came meanwhile.
	gen            int
	running, again bool
	// seen is the text's version and the caret the box last followed.
	seenVersion uint64
	seenCaret   int
	refresh     bool
}

// wikiMost is how many suggestions show at most.
const wikiMost = 50

// wikiRowH is the height of a row of suggestions, and wikiRows how many
// show before they scroll.
const (
	wikiRowH    = 30
	wikiRows    = 8
	wikiBoxW    = 380
	wikiLineMax = 4000
)

// wikiFollow follows the editor's typing: it opens the box as a wiki link
// is typed, finds its suggestions as the query changes, and closes it as
// the caret leaves the link.
func (a *app) wikiFollow(c *ui.Context, d *editor) {
	s := &a.wikiSug
	if s.d != d {
		if !d.vscroll.Focused(c) {
			return
		}
		*s = wikiSuggest{d: d, seenVersion: d.buf.Snapshot().Version(), seenCaret: d.sel[1]}
		return
	}
	snap := d.buf.Snapshot()
	v, caret := snap.Version(), d.sel[1]
	if v == s.seenVersion && caret == s.seenCaret && !s.refresh {
		return
	}
	typed := v != s.seenVersion
	s.seenVersion, s.seenCaret = v, caret
	q, ok := a.wikiQueryAt(c, d, snap)
	if !ok {
		s.open, s.refresh = false, false
		return
	}
	if !s.open && !typed && !s.refresh {
		return
	}
	s.refresh = false
	if !s.open {
		s.open, s.sel, s.items, s.key, s.asked = true, 0, nil, "", "\x00"
	}
	s.q = q
	if q.text != s.asked {
		a.wikiAskFor(d)
	}
}

// wikiQueryAt is the wiki link the caret of d types, when the box may
// show for it.
func (a *app) wikiQueryAt(c *ui.Context, d *editor, snap ui.TextSnapshot) (wikiQuery, bool) {
	if !isMarkdown(d.document) || d.sel[0] != d.sel[1] || d.locked() || !d.vscroll.Focused(c) ||
		a.prefs().Vim && d.vim.mode != vimInsert {
		return wikiQuery{}, false
	}
	return wikiQueryOf(snap, d.sel[1])
}

// wikiQueryOf is the wiki link typed at caret of snap, by offsets of the
// text, outside fenced code.
func wikiQueryOf(snap ui.TextSnapshot, caret int) (wikiQuery, bool) {
	caret = min(caret, snap.Len())
	line := snap.LineAt(caret)
	s, e := snap.LineRange(line)
	if e-s > wikiLineMax || caret-s < 2 {
		return wikiQuery{}, false
	}
	text := snap.Slice(s, e)
	if !strings.Contains(text, "[[") {
		return wikiQuery{}, false
	}
	q, ok := wikiQueryIn([]rune(text), caret-s)
	if !ok || wikiFenced(snap.Slice(0, s)) {
		return wikiQuery{}, false
	}
	q.from += s
	q.caret += s
	q.to += s
	if q.close >= 0 {
		q.close += s
	}
	return q, true
}

// wikiFenced reports whether what follows text, the lines of a note
// before one, is in fenced code. It reads text once, as a string, and
// looks closer only at the lines that start with ` or ~.
func wikiFenced(text string) bool {
	var fence byte
	n := 0
	for len(text) > 0 {
		line := text
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			line, text = text[:i], text[i+1:]
		} else {
			text = ""
		}
		rest := strings.TrimLeft(line, " ")
		if rest == "" || rest[0] != '`' && rest[0] != '~' {
			continue
		}
		rest = strings.TrimRight(rest, "\r")
		switch {
		case fence != 0:
			if fenceCloses(rest, fence, n) {
				fence = 0
			}
		case fenceOpens(rest):
			fence, n = rest[0], fenceRun(rest)
		}
	}
	return fence != 0
}

// wikiAskFor finds the suggestions of the box's query in the background,
// one ask at a time, the last one asked for answered.
func (a *app) wikiAskFor(d *editor) {
	s := &a.wikiSug
	s.gen++
	s.asked = s.q.text
	if s.running {
		s.again = true
		return
	}
	s.running = true
	s.loading = s.key == ""
	gen := s.gen
	in := wikiInput{path: d.path, workspace: a.folder(), recent: slices.Clone(a.recent)}
	in.target, in.mode, in.query = wikiAsk(s.q.text)
	in.target = strings.TrimSpace(in.target)
	if in.mode != wikiFilesMode && in.target == "" {
		in.self = d.buf.Snapshot().String()
	}
	asked := s.q.text
	in.arrived = func() {
		a.post(func() {
			if s.d == d && s.open {
				s.refresh, s.asked = true, "\x00"
				a.invalidate()
			}
		})
	}
	bg.Go(func() {
		defer recoverCrash()
		items := wikiSuggestions(in)
		a.post(func() {
			s.running = false
			if s.d == d && gen == s.gen {
				s.items, s.key, s.loading = items, asked, false
				s.sel, s.moved = 0, false
			}
			if s.again {
				s.again = false
				if s.d == d && s.open {
					a.wikiAskFor(d)
				}
			}
			a.invalidate()
		})
	})
}

// wikiInput is what finding the suggestions of a query takes.
type wikiInput struct {
	path, workspace string
	target          string
	mode            wikiMode
	query           string
	recent          []string
	// self is the note's own text, for its own headings and blocks.
	self    string
	arrived func()
}

// wikiSuggestions finds the suggestions of a query: files, or the
// headings or blocks of the note it names.
func wikiSuggestions(in wikiInput) []wikiItem {
	root := markdownRoot(in.path, in.workspace)
	if root == "" && in.path != "" {
		root = filepath.Dir(in.path)
	}
	if in.mode == wikiFilesMode {
		return wikiFileItems(root, in)
	}
	src, file := in.self, ""
	if in.target != "" {
		if file = wikiLinkFile(root, in.path, in.target, nil); file == "" || !isMarkdownPath(file) {
			return nil
		}
		note, ok := wikiNoteOf(file)
		if !ok {
			return nil
		}
		if in.mode == wikiHeadingsMode {
			return wikiHeadingItems(in.target, note.headings, in.query)
		}
		return wikiBlockItems(in.target, file, note.blocks, in.query)
	}
	if in.mode == wikiHeadingsMode {
		return wikiHeadingItems("", wikiHeadingsOf(src), in.query)
	}
	return wikiBlockItems("", "", wikiBlocksOf(src), in.query)
}

// wikiFileItems are the files of root the query matches, the latest
// changed first of equal matches; without one, the files shown last
// first, then the others, the latest changed first.
func wikiFileItems(root string, in wikiInput) []wikiItem {
	set := wikiFiles(root, in.arrived)
	if set == nil || len(set.files) == 0 {
		return nil
	}
	format := wikiLinkFormat(root)
	item := func(i int, positions []int) wikiItem {
		f := set.files[i]
		name := set.names[i]
		base := strings.LastIndexByte(name, '/') + 1
		it := wikiItem{link: wikiLinkText(set, root, in.path, f, format), title: name[base:], name: pathpkg.Base(f), positions: positions}
		if dir := pathpkg.Dir(f); dir != "." {
			it.detail = dir
		}
		// The positions are of the name's path: those of its base, from it.
		if len(positions) > 0 {
			it.positions = make([]int, 0, len(positions))
			for _, p := range positions {
				if p >= base {
					it.positions = append(it.positions, p-base)
				}
			}
		}
		return it
	}
	query := strings.TrimSpace(in.query)
	var out []wikiItem
	if query == "" {
		seen := map[int]bool{}
		for _, p := range in.recent {
			if len(out) >= wikiMost {
				break
			}
			rel, err := filepath.Rel(root, p)
			if err != nil || !filepath.IsLocal(rel) || filepath.Clean(p) == filepath.Clean(in.path) {
				continue
			}
			if f, ok := set.byPath[strings.ToLower(filepath.ToSlash(rel))]; ok {
				if i, found := slices.BinarySearch(set.files, f); found && !seen[i] {
					seen[i] = true
					out = append(out, item(i, nil))
				}
			}
		}
		for _, i := range set.byMod {
			if len(out) >= wikiMost {
				break
			}
			if !seen[i] {
				out = append(out, item(i, nil))
			}
		}
		return out
	}
	// Matched in the order latest changed first, of equal matches the
	// latest changed ranks first.
	for _, m := range workspace.Fuzzy(set.modNames, query, wikiMost) {
		out = append(out, item(set.byMod[m.Index], m.Positions))
	}
	return out
}

// wikiLinkText is how a link to file f, a path from root with slashes,
// is written from the note at path: as format says, its shortest path
// by default, without .md.
func wikiLinkText(set *wikiFileSet, root, path, f, format string) string {
	link := f
	switch format {
	case "relative":
		if rel, err := filepath.Rel(filepath.Dir(path), filepath.Join(root, filepath.FromSlash(f))); err == nil {
			link = filepath.ToSlash(rel)
		}
	case "absolute":
	default:
		if len(set.byName[strings.ToLower(pathpkg.Base(f))]) == 1 {
			link = pathpkg.Base(f)
		}
	}
	if strings.EqualFold(pathpkg.Ext(link), ".md") {
		link = link[:len(link)-3]
	}
	return link
}

// wikiFormats are the vaults' newLinkFormat, by their roots, and when
// each was read.
var wikiFormats struct {
	sync.Mutex
	m map[string]wikiFormat
}

type wikiFormat struct {
	format string
	at     time.Time
}

// wikiLinkFormat is how the vault at root writes a link to a file, its
// newLinkFormat in .obsidian/app.json: "shortest", "relative" or
// "absolute", read again once wikiIndexAge old.
func wikiLinkFormat(root string) string {
	wikiFormats.Lock()
	f, ok := wikiFormats.m[root]
	wikiFormats.Unlock()
	if ok && time.Since(f.at) < wikiIndexAge {
		return f.format
	}
	f = wikiFormat{format: "shortest", at: time.Now()}
	if data, err := vfs.ReadFileLimit(filepath.Join(root, ".obsidian", "app.json"), 1<<20); err == nil {
		var app struct {
			NewLinkFormat string `json:"newLinkFormat"`
		}
		if json.Unmarshal(data, &app) == nil && app.NewLinkFormat != "" {
			f.format = app.NewLinkFormat
		}
	}
	wikiFormats.Lock()
	if wikiFormats.m == nil {
		wikiFormats.m = map[string]wikiFormat{}
	}
	wikiFormats.m[root] = f
	wikiFormats.Unlock()
	return f.format
}

// wikiHeading is a heading of a note.
type wikiHeading struct {
	text  string
	level int
}

// wikiBlock is a block of a note: a paragraph or an item of a list, by
// its last line, and its id, "" for none.
type wikiBlock struct {
	line     int
	text     string
	lineText string
	id       string
}

// wikiHeadingsOf are the headings of the note src, outside its code.
func wikiHeadingsOf(src string) []wikiHeading {
	var out []wikiHeading
	wikiLines(src, func(i int, line string, fenced bool) {
		if fenced {
			return
		}
		if level := markdownHashes(line); level > 0 {
			t := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
			// A closing run of #, after a space.
			if k := strings.TrimRight(t, "#"); k != t && (k == "" || strings.HasSuffix(k, " ")) {
				t = strings.TrimSpace(k)
			}
			if t != "" {
				out = append(out, wikiHeading{text: t, level: level})
			}
		}
	})
	return out
}

// wikiBlocksOf are the blocks of the note src: its paragraphs and the
// items of its lists, outside its code and headings.
func wikiBlocksOf(src string) []wikiBlock {
	var out []wikiBlock
	var para []string
	last, lastText := -1, ""
	flush := func() {
		if len(para) > 0 {
			out = append(out, wikiBlockOf(last, strings.Join(para, " "), lastText))
		}
		para = para[:0]
	}
	wikiLines(src, func(i int, line string, fenced bool) {
		t := strings.TrimSpace(line)
		switch {
		case fenced || t == "" || markdownHashes(line) > 0 || liveRule.MatchString(line):
			flush()
		case liveListMarker.MatchString(line):
			flush()
			m := liveListMarker.FindStringIndex(line)
			out = append(out, wikiBlockOf(i, line[m[1]:], line))
		case strings.HasPrefix(t, "^") && markdownBlockID([]byte(t)) == 0:
			// The id of the block above, alone on its line.
			if len(para) > 0 {
				b := wikiBlockOf(last, strings.Join(para, " "), lastText)
				b.id = t[1:]
				out = append(out, b)
				para = para[:0]
			} else if n := len(out); n > 0 && out[n-1].id == "" {
				out[n-1].id = t[1:]
			}
		default:
			para = append(para, t)
			last, lastText = i, line
		}
	})
	flush()
	return out
}

// wikiBlockOf is the block of text ending on line i, lineText, with its
// id taken from the end of it.
func wikiBlockOf(i int, text, lineText string) wikiBlock {
	b := wikiBlock{line: i, lineText: lineText}
	text = strings.TrimSpace(text)
	if at := markdownBlockID([]byte(text)); at >= 0 {
		b.id = text[at+1:]
		text = strings.TrimSpace(text[:at])
	}
	b.text = text
	return b
}

// wikiLines calls fn with each line of src after its front matter, by its
// number, and whether it is in fenced code.
func wikiLines(src string, fn func(i int, line string, fenced bool)) {
	body, skip := markdownSkipFrontMatter([]byte(src))
	lines := strings.Split(string(body), "\n")
	var fence byte
	n := 0
	for i, line := range lines {
		line = strings.TrimRight(line, "\r")
		rest := strings.TrimLeft(line, " ")
		switch {
		case fence != 0:
			if fenceCloses(rest, fence, n) {
				fence = 0
			}
			fn(skip+i, line, true)
			continue
		case fenceOpens(rest):
			fence, n = rest[0], fenceRun(rest)
			fn(skip+i, line, true)
			continue
		}
		fn(skip+i, line, false)
	}
}

// wikiHeadingItems are the headings the query matches, of the note
// target names.
func wikiHeadingItems(target string, headings []wikiHeading, query string) []wikiItem {
	texts := make([]string, len(headings))
	for i, h := range headings {
		texts[i] = h.text
	}
	var out []wikiItem
	for _, m := range workspace.Fuzzy(texts, query, wikiMost) {
		h := headings[m.Index]
		out = append(out, wikiItem{link: target + "#" + h.text, title: h.text, detail: "H" + string(rune('0'+h.level)),
			mode: wikiHeadingsMode, positions: m.Positions})
	}
	return out
}

// wikiBlockItems are the blocks the query matches, of the note file
// target names, "" for the note itself; a block without an id is given
// one, as it is chosen.
func wikiBlockItems(target, file string, blocks []wikiBlock, query string) []wikiItem {
	texts := make([]string, len(blocks))
	for i, b := range blocks {
		texts[i] = b.text
	}
	var out []wikiItem
	for _, m := range workspace.Fuzzy(texts, query, wikiMost) {
		b := blocks[m.Index]
		title := b.text
		if utf8.RuneCountInString(title) > 120 {
			title = string([]rune(title)[:120]) + "…"
		}
		it := wikiItem{title: title, mode: wikiBlocksMode, positions: m.Positions, file: file, line: b.line, lineText: b.lineText}
		id := b.id
		if id == "" {
			id = wikiNewID()
			it.newID = id
		} else {
			it.detail = "^" + id
		}
		it.link = target + "#^" + id
		out = append(out, it)
	}
	return out
}

// wikiNewID is an id for a block, six letters and digits, as Obsidian
// gives one.
func wikiNewID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	rand.Read(b)
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

// wikiNote is what a note offers to link to: its headings and blocks.
type wikiNote struct {
	headings []wikiHeading
	blocks   []wikiBlock
	mod      time.Time
	size     int64
}

// wikiNotes are the notes read for their headings and blocks, by path,
// read again when changed.
var wikiNotes struct {
	sync.Mutex
	m map[string]wikiNote
}

// wikiNoteOf is the headings and blocks of the note at file.
func wikiNoteOf(file string) (wikiNote, bool) {
	info, err := vfs.Stat(file)
	if err != nil || info.Size() > 4*markdownEmbedMax {
		return wikiNote{}, false
	}
	wikiNotes.Lock()
	n, ok := wikiNotes.m[file]
	wikiNotes.Unlock()
	if ok && n.mod.Equal(info.ModTime()) && n.size == info.Size() {
		return n, true
	}
	data, err := vfs.ReadFile(file)
	if err != nil {
		return wikiNote{}, false
	}
	src := string(data)
	n = wikiNote{headings: wikiHeadingsOf(src), blocks: wikiBlocksOf(src), mod: info.ModTime(), size: info.Size()}
	wikiNotes.Lock()
	if wikiNotes.m == nil {
		wikiNotes.m = map[string]wikiNote{}
	}
	if len(wikiNotes.m) > 256 {
		clear(wikiNotes.m)
	}
	wikiNotes.m[file] = n
	wikiNotes.Unlock()
	return n, true
}

// wikiKey takes the keys of the box while it shows, before the editor.
func (a *app) wikiKey(d *editor, ev ui.InputEvent) bool {
	s := &a.wikiSug
	if !s.open || s.d != d || ev.Kind != ui.InputKeyDown {
		return false
	}
	switch m := ev.Mods; {
	case ev.Key == ui.KeyEscape && m == 0:
		s.open = false
	case ev.Key == ui.KeyUp && m == 0, ev.Key == ui.KeyP && m == ui.Ctrl:
		if len(s.items) > 0 {
			s.sel, s.moved = (s.sel+len(s.items)-1)%len(s.items), true
		}
	case ev.Key == ui.KeyDown && m == 0, ev.Key == ui.KeyN && m == ui.Ctrl:
		if len(s.items) > 0 {
			s.sel, s.moved = (s.sel+1)%len(s.items), true
		}
	case ev.Key == ui.KeyEnter && m == ui.Shift:
		a.wikiChoose(d, -1)
	case (ev.Key == ui.KeyEnter || ev.Key == ui.KeyTab) && m == 0:
		if s.loading || len(s.items) == 0 {
			if ev.Key == ui.KeyTab {
				return false
			}
			a.wikiChoose(d, -1)
			break
		}
		a.wikiChoose(d, s.sel)
	default:
		return false
	}
	a.invalidate()
	return true
}

// wikiChoose puts suggestion i in place of the wiki link being typed, or
// with i -1 closes the link as typed, the caret after it.
func (a *app) wikiChoose(d *editor, i int) {
	s := &a.wikiSug
	var it *wikiItem
	if i >= 0 && i < len(s.items) {
		item := s.items[i]
		it = &item
	}
	s.open = false
	a.queueEdit(d, func(d *editor) {
		snap := d.buf.Snapshot()
		q, ok := wikiQueryOf(snap, d.sel[1])
		if !ok {
			return
		}
		link := snap.Slice(q.from, q.to)
		if it != nil {
			link = it.link
		}
		edits := []textEdit{{q.from, q.to, link}}
		if q.close < 0 {
			edits[0].text += "]]"
		}
		delta := utf8.RuneCountInString(edits[0].text) - (q.to - q.from)
		caret := q.from + utf8.RuneCountInString(edits[0].text)
		if q.close >= 0 {
			caret = q.close + delta + 2
		}
		if it != nil && it.newID != "" {
			if it.file == "" {
				// A block of the note itself, on another line: its id goes
				// at the end of its line, if the line is still what it was.
				if it.line < snap.LineCount() && it.line != snap.LineAt(q.from) && snap.Line(it.line) == it.lineText {
					_, e := snap.LineRange(it.line)
					edits = append(edits, textEdit{e, e, " ^" + it.newID})
					if e < q.from {
						caret += 2 + len(it.newID)
					}
				}
			} else {
				a.wikiGiveID(it.file, it.line, it.lineText, it.newID)
			}
		}
		d.edit([2]int{caret, caret}, edits...)
	})
}

// wikiGiveID puts ^id at the end of line of the note at file, if the line
// is still text: in its tab when it is open, else on disk.
func (a *app) wikiGiveID(file string, line int, text, id string) {
	for _, doc := range a.docs {
		if doc.path == "" || filepath.Clean(doc.path) != filepath.Clean(file) || doc.unread() {
			continue
		}
		snap := doc.buf.Snapshot()
		if line < snap.LineCount() && snap.Line(line) == text && !doc.locked() {
			_, e := snap.LineRange(line)
			doc.buf.Replace(e, e, " ^"+id)
		}
		return
	}
	bg.Go(func() {
		defer recoverCrash()
		info, data, err := vfs.ReadFileInfo(file, 4*markdownEmbedMax)
		if err != nil {
			return
		}
		lines := strings.SplitAfter(string(data), "\n")
		if line >= len(lines) {
			return
		}
		body := strings.TrimRight(lines[line], "\r\n")
		if body != text {
			return
		}
		lines[line] = body + " ^" + id + lines[line][len(body):]
		if _, err := vfs.WriteFile(file, []byte(strings.Join(lines, "")), info.Mode().Perm()); err != nil {
			a.post(func() { a.fail("%s", locales.FailSave(filepath.Base(file), err.Error())) })
		}
	})
}

// wikiSuggestView draws the box of suggestions under the wiki link the
// editor types, or over it near the bottom of the view.
func (a *app) wikiSuggestView(c *ui.Context, d *editor) {
	a.wikiFollow(c, d)
	s := &a.wikiSug
	if !s.open || s.d != d {
		return
	}
	p, f := a.pal, a.font
	snap := d.buf.Snapshot()
	view := d.lastView
	at := max(0, min(s.q.from-2, snap.Len()))
	var x, top, bottom float32
	if cx, cy, ch, ok := d.lines.Caret(at); ok && a.wraps(d) {
		x = d.textX + cx - d.hscroll.X
		top = editorPadY + cy - d.vscroll.Y
		bottom = top + ch
	} else {
		line := snap.LineAt(at)
		ls, _ := snap.LineRange(line)
		g := a.geometry(d, snap.LineCount())
		x = d.textX + f.width(snap.Slice(ls, at)) - d.hscroll.X
		top = editorPadY + g.top(line) - d.vscroll.Y
		bottom = top + f.lineH
	}
	if bottom < 0 || top > view.H {
		return
	}
	rows := max(1, min(len(s.items), wikiRows))
	h := float32(rows)*wikiRowH + 8 + 28
	w := min(float32(wikiBoxW), max(view.W-16, 160))
	x = max(8, min(x, view.W-w-8))
	box := ui.Column(c.Key("wiki-suggest")).Absolute().Left(x).Width(w).Radius(8).Clip()
	if bottom+4+h > view.H && top-4-h >= 0 {
		box.Top(top - 4 - h)
	} else {
		box.Top(bottom + 4)
	}
	a.floating(c, box, false).Transition(popIn).KeepFocus().Role(ui.RoleList).Label(locales.WikiSuggestLabel())
	box.Children(func() {
		ui.Scroll(c.Key("wiki-suggest-rows")).MaxHeight(float32(wikiRows)*wikiRowH + 8).Padding(4).Children(func() {
			switch {
			case s.loading:
				ui.Text(c, locales.WikiSuggestReading()).FontSize(12.5).TextColor(p.muted).Height(wikiRowH).Padding(0, 10).SingleLine()
			case len(s.items) == 0:
				ui.Text(c, a.wikiNone()).FontSize(12.5).TextColor(p.muted).Height(wikiRowH).Padding(0, 10).SingleLine()
			}
			if s.loading {
				return
			}
			for i, it := range s.items {
				a.wikiRow(c, d, i, it)
			}
		})
		ui.Row(c).Height(28).Padding(0, 12).Gap(6).AlignItems(ui.Center).Background(p.hover).
			BorderWidth(1, 0, 0, 0).BorderColor(p.border).Children(func() {
			ui.Text(c, locales.WikiSuggestHint()).FontSize(11.5).TextColor(p.muted).SingleLine().Shrink(1)
		})
	})
}

// wikiNone says that nothing matches the query.
func (a *app) wikiNone() string {
	_, mode, _ := wikiAsk(a.wikiSug.q.text)
	switch mode {
	case wikiHeadingsMode:
		return locales.WikiSuggestNoHeading()
	case wikiBlocksMode:
		return locales.WikiSuggestNoBlock()
	}
	return locales.WikiSuggestNoFile()
}

// wikiRow is suggestion i of the box, which a click chooses.
func (a *app) wikiRow(c *ui.Context, d *editor, i int, it wikiItem) {
	s := &a.wikiSug
	p := a.pal
	row := ui.ButtonBase(c.Key("wiki-"+it.link)).Height(wikiRowH).Padding(0, 10).Gap(8).Radius(6).
		Justify(ui.Start).AlignItems(ui.Center).Label(it.title)
	sel := i == s.sel
	if !sel && row.Hovered() {
		row.Background(p.hover)
	}
	if sel && s.moved {
		row.ScrollIntoView()
		s.moved = false
	}
	if row.Clicked() {
		a.wikiChoose(d, i)
	}
	row.Children(func() {
		a.pickMark(c, sel)
		icon, col := iconHash, p.muted
		switch it.mode {
		case wikiFilesMode:
			icon, col = fileIcon(p, it.name)
		case wikiBlocksMode:
			icon = iconQuote
		}
		ui.Icon(c, icon).FontSize(14).TextColor(col)
		ui.RichText(c, a.matchSpans(it.title, 0, it.positions, p.text)...).FontSize(13).SingleLine().Shrink(1)
		if it.detail != "" {
			ui.Spacer(c)
			ui.Text(c, it.detail).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(2)
		}
	})
}

// wikiSuggestNow opens the box for the wiki link the caret of d is in,
// as Ctrl-Space asks; false when it is in none.
func (a *app) wikiSuggestNow(d *editor) bool {
	if !isMarkdown(d.document) || d.sel[0] != d.sel[1] {
		return false
	}
	if _, ok := wikiQueryOf(d.buf.Snapshot(), d.sel[1]); !ok {
		return false
	}
	s := &a.wikiSug
	if s.d != d {
		*s = wikiSuggest{d: d}
	}
	s.refresh, s.asked, s.seenVersion = true, "\x00", 0
	a.invalidate()
	return true
}

// wikiTyping is the target of the wiki link being typed in doc, which
// the parse of its Live Preview finds by the index alone, "" for none.
func (a *app) wikiTyping(doc *document) string {
	s := &a.wikiSug
	if !s.open || s.d == nil || s.d.document != doc {
		return ""
	}
	target, _, _ := wikiAsk(s.q.text)
	return strings.TrimSpace(target)
}
