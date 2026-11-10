package main

import (
	"fmt"
	"maps"
	"regexp"
	"sync/atomic"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/locales"
)

// diffMatch is a match of find in a diff: in line of a side of file, 0 the
// old and 1 the new, from byte start up to end. A line each side has the
// same, unchanged, is the new side's.
type diffMatch struct {
	file       *diffFile
	side, line int
	start, end int
}

// diffLineKey is a line of a side of a file of a diff.
type diffLineKey struct {
	file       *diffFile
	side, line int
}

// diffFind is what find found in the lines a diff shows, those of every
// file, folded under its header or not, as its context lays them out.
type diffFind struct {
	// on is set while find shows over the diff with a query, which reads
	// every file of the diff to look in it.
	on bool
	// key is what found was found for: the query and its options, the
	// diff's generation and its context; pattern the query and options
	// alone. pending is the key being searched for in the background, ""
	// for none, which stop stops.
	key, pattern, pending string
	stop                  *atomic.Bool
	found                 []diffMatch
	at                    map[diffLineKey][]int
	cur                   int // the current match, -1 for none
	err                   string
	// reveal goes to the current match once its line shows.
	reveal bool
}

// diffFindFile is a file of a diff as find looks in it, away from the
// frames: its lines, its rows, and the runs of them that fold.
type diffFindFile struct {
	f        *diffFile
	old, new []string
	all      []diffRow
	open     map[int]bool
	full     bool
	context  int
}

// activeDiff returns the diff of the tab of the column with the keyboard,
// nil for none.
func (a *app) activeDiff() *diffView {
	if t := a.activeTab(); t != nil && t.diff != nil {
		return t.diff
	}
	return nil
}

// diffMatches returns the matches of re in the lines files show, in the
// order the unified layout shows them, up to maxMatches; nil once stop is
// set.
func diffMatches(re *regexp.Regexp, files []diffFindFile, stop *atomic.Bool) []diffMatch {
	var found []diffMatch
	for _, ff := range files {
		if stop.Load() {
			return nil
		}
		folded := func(int) bool { return false }
		if !ff.full {
			_, _, folds, _ := foldSpans(ff.all, ff.open, ff.context)
			folded = func(i int) bool {
				for _, s := range folds {
					if i >= s.from && i < s.to {
						return true
					}
				}
				return false
			}
		}
		for i, r := range ff.all {
			if folded(i) {
				continue
			}
			n, newSide := r.line()
			lines := ff.old
			if newSide {
				lines = ff.new
			}
			if n < 0 || n >= len(lines) {
				continue
			}
			for _, m := range re.FindAllStringIndex(lines[n], maxMatches-len(found)) {
				if m[0] < m[1] {
					found = append(found, diffMatch{ff.f, btoi(newSide), n, m[0], m[1]})
				}
			}
			if len(found) >= maxMatches {
				return found
			}
		}
	}
	return found
}

// searchDiff finds the matches of find's query in the lines a diff shows,
// in the background, unless they were found, or are being found, for the
// diff as it is: the matches found before show until the new ones come.
func (a *app) searchDiff(v *diffView) {
	f, d := &a.find, &v.find
	pattern := f.pattern()
	context := a.diffContextOf()
	key := fmt.Sprintf("%s|%d|%v|%d", pattern, v.gen, v.full, context)
	if d.key == key || d.pending == key {
		return
	}
	if d.stop != nil {
		d.stop.Store(true)
	}
	d.pending, d.stop = "", nil
	queryChanged := d.pattern != pattern
	none := func(err string) {
		d.key, d.pattern = key, pattern
		d.found, d.at, d.cur, d.err, d.reveal = nil, nil, -1, err, false
	}
	if f.query == "" {
		none("")
		return
	}
	re, err := f.compile()
	if err != nil {
		none(locales.FindInvalid())
		return
	}
	files := make([]diffFindFile, 0, len(v.files))
	for _, fl := range v.files {
		if !fl.loaded || fl.binary || fl.err != "" {
			continue
		}
		files = append(files, diffFindFile{f: fl, old: fl.old.lines, new: fl.new.lines, all: fl.unified,
			open: maps.Clone(fl.open), full: v.full, context: context})
	}
	stop := new(atomic.Bool)
	d.pending, d.stop = key, stop
	bg.Go(func() {
		defer recoverCrash()
		found := diffMatches(re, files, stop)
		if stop.Load() {
			return
		}
		a.post(func() {
			if d.pending != key {
				return // superseded
			}
			d.pending, d.stop = "", nil
			a.applyDiffMatches(v, key, pattern, found, queryChanged)
		})
	})
}

// applyDiffMatches takes the matches found in a diff for key: the current
// match stays the one it was, or with the query changed, it is the first
// in the file at the top of the view or after, and the diff goes to it.
func (a *app) applyDiffMatches(v *diffView, key, pattern string, found []diffMatch, queryChanged bool) {
	d := &v.find
	var was *diffMatch
	if !queryChanged && d.cur >= 0 && d.cur < len(d.found) {
		m := d.found[d.cur]
		was = &m
	}
	d.key, d.pattern, d.found, d.err = key, pattern, found, ""
	d.at = make(map[diffLineKey][]int, len(found))
	for i, m := range found {
		k := diffLineKey{m.file, m.side, m.line}
		d.at[k] = append(d.at[k], i)
	}
	switch {
	case len(found) == 0:
		d.cur = -1
	case was != nil:
		d.cur = min(d.cur, len(found)-1)
		for i, m := range found {
			if m == *was {
				d.cur = i
				break
			}
		}
	case queryChanged:
		d.cur = 0
		if top := v.focusRow(); top >= 0 && top < len(v.rows) {
			order := map[*diffFile]int{}
			for i, fl := range v.files {
				order[fl] = i
			}
			for i, m := range found {
				if order[m.file] >= v.rows[top].file {
					d.cur = i
					break
				}
			}
		}
		d.reveal = true
	default:
		d.cur = min(max(d.cur, 0), len(found)-1)
	}
	a.invalidate()
}

// stepDiff goes to the next match of find in a diff, or the previous,
// round the ends.
func (a *app) stepDiff(v *diffView, by int) {
	d := &v.find
	a.searchDiff(v)
	if len(d.found) == 0 {
		return
	}
	if d.cur < 0 {
		d.cur = 0
	} else {
		d.cur = (d.cur + by + len(d.found)) % len(d.found)
	}
	d.reveal = true
}

// matchRow returns the row of a diff that shows the line of a match, -1
// for none.
func (v *diffView) matchRow(m diffMatch) int {
	for i := range v.rows {
		r := &v.rows[i]
		if r.kind != rowLine || v.files[r.file] != m.file {
			continue
		}
		// The old side's matches are of lines changed: those of the lines
		// unchanged are the new side's.
		if m.side == 1 && r.new == m.line || m.side == 0 && r.old == m.line && r.hunk >= 0 {
			return i
		}
	}
	return -1
}

// revealDiffMatch goes to the current match of find in a diff: its file
// unfolds, and once its line shows, the row is chosen and scrolled to,
// and sideways to the match where the lines do not wrap.
func (a *app) revealDiffMatch(v *diffView) {
	d := &v.find
	if !d.reveal || d.cur < 0 || d.cur >= len(d.found) {
		return
	}
	m := d.found[d.cur]
	if m.file.folded {
		m.file.folded = false
		v.place = nil
		v.changed()
		return
	}
	i := v.matchRow(m)
	if i < 0 {
		if v.built.gen == v.gen {
			d.reveal = false // its line does not show
		}
		return
	}
	d.reveal = false
	v.anchor = -1
	v.setCursor(i)
	v.glideRow(i, jumpThird)
	if v.built.wrap {
		return
	}
	l := a.diffGeometry(v, v.split, v.viewW)
	w := v.viewW - l.textX
	if v.split {
		w = l.half - l.textX
		if m.side == 1 {
			w = v.viewW - l.right() - l.textX
		}
	}
	text := m.file.side(m.side == 1).lines[m.line]
	if m.end > len(clipLine(text)) {
		return
	}
	shown, at := expandTabs(text)
	x0, x1 := a.font.width(shown[:at(m.start)]), a.font.width(shown[:at(m.end)])
	if x0 < v.scroll.X || x1 > v.scroll.X+w-a.font.charW {
		v.scroll.X = max(0, x0-w/3)
	}
}

// diffFindView is the find widget over a diff, which finds in the lines
// of each side of every file it shows.
func (a *app) diffFindView(c *ui.Context, v *diffView) {
	f := &a.find
	v.find.on = f.open && f.query != ""
	if !f.open {
		return
	}
	a.searchDiff(v)
	a.revealDiffMatch(v)
	a.findBoxView(c, findBox{
		n: len(v.find.found), cur: v.find.cur, err: v.find.err,
		step: func(by int) { a.stepDiff(v, by) },
	})
}

// paintDiffHits paints the matches of find in the line of side of row of
// a diff, -1 for the one the unified layout shows, under its text at x,
// y. The old side of a line unchanged shows those of the new.
func (a *app) paintDiffHits(pt *ui.Painter, v *diffView, f *diffFile, row *diffRow, side int, x, y float32, cols int) {
	d, p := &v.find, a.pal
	if !d.on || !a.find.open || len(d.found) == 0 {
		return
	}
	n, newSide := row.line()
	if side >= 0 {
		n, newSide = row.old, side == 1
		if newSide {
			n = row.new
		}
	}
	key := diffLineKey{f, btoi(newSide), n}
	if !newSide && row.hunk < 0 && row.new >= 0 {
		// Unchanged: the matches are the new side's, which show on the
		// old as well when the text is the same.
		if row.old >= len(f.old.lines) || row.new >= len(f.new.lines) || f.old.lines[row.old] != f.new.lines[row.new] {
			return
		}
		key = diffLineKey{f, 1, row.new}
	}
	hits := d.at[key]
	if len(hits) == 0 {
		return
	}
	text := clipLine(f.side(newSide).lines[n])
	for _, k := range hits {
		m := d.found[k]
		if m.end > len(text) {
			continue
		}
		from := utf8.RuneCountInString(text[:m.start])
		to := from + utf8.RuneCountInString(text[m.start:m.end])
		for _, rc := range diffSelRects(a.font, text, from, to, x, y, cols, v.built.wrap) {
			box := ui.Rect{X: rc.X - 1, Y: rc.Y + 1, W: rc.W + 2, H: rc.H - 2}
			if k == d.cur {
				pt.Fill(box, p.matchNow, 3)
				pt.Stroke(box, p.matchRing, 3, 1)
			} else {
				pt.Fill(box, p.match, 3)
			}
		}
	}
}
