package main

import (
	"fmt"
	"mayoi/locales"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// markdownMatch is a match of find in a Markdown preview: the block it is
// in, the cell of a table's row it is in, -1 for none, and where it starts
// and ends in the text markdownFindText makes of them, in bytes.
type markdownMatch struct {
	block, cell int
	start, end  int
}

// markdownHit is a match of find in a text, where it starts and ends in
// the text of its runs, and whether it is the current one.
type markdownHit struct {
	start, end int
	cur        bool
}

// markdownFindRun is the text find looks in of a run: its text, as shown,
// or a line break for a picture or a formula drawn, which no match takes
// in, that matches do not cross.
func markdownFindRun(r markdownInline) string {
	if r.image != "" || r.math != nil && r.math.err == "" {
		return "\n"
	}
	return r.text
}

// markdownFindText is the text find looks in of runs.
func markdownFindText(runs []markdownInline) string {
	var sb strings.Builder
	for _, r := range runs {
		sb.WriteString(markdownFindRun(r))
	}
	return sb.String()
}

// markdownFindTexts are the texts find looks in of block i, as they show:
// the cells of a table's row, a block's runs, or none for a rule, a
// picture, a diagram or a formula drawn.
func (m *markdownPreview) markdownFindTexts(i int) []string {
	b := m.blocks[i]
	switch b.kind {
	case markdownRule, markdownImageBlock, markdownAlertTitle:
		return nil
	case markdownCodeBlock:
		if d := b.diagram; d != nil && d.err == "" && !m.sources[b.line] {
			return nil
		}
	case markdownMathBlock:
		if f := b.formula; f != nil && f.err == "" && f.w > 0 {
			return nil
		}
	case markdownTable, markdownTableHeader:
		texts := make([]string, len(b.cells))
		for k, cell := range b.cells {
			texts[k] = markdownFindText(cell)
		}
		return texts
	}
	return []string{markdownFindText(b.runs)}
}

// activeMarkdown returns the Markdown preview shown in place of the text
// in the column that has the keyboard, nil when it shows none.
func (a *app) activeMarkdown() *viewer {
	if d := a.activeEditor(); d != nil && d.preview != nil && d.preview.markdown != nil {
		return d.preview
	}
	return nil
}

// searchPreview finds the matches of find's query in the text a preview
// shows, unless they were found for its blocks as they are.
func (a *app) searchPreview(m *markdownPreview) {
	f := &a.find
	pattern := f.pattern()
	var blocks *markdownBlock
	if len(m.blocks) > 0 {
		blocks = &m.blocks[0]
	}
	key := fmt.Sprintf("%p|%d|%d", blocks, len(m.blocks), len(m.sources))
	if m.findPattern == pattern && m.findBlocks == key {
		return
	}
	queryChanged := m.findPattern != pattern
	m.findPattern, m.findBlocks = pattern, key
	m.found, m.findErr = m.found[:0], ""
	if f.query == "" {
		m.findCur = -1
		return
	}
	re, err := f.compile()
	if err != nil {
		m.findErr, m.findCur = locales.FindInvalid(), -1
		return
	}
	for i := range m.blocks {
		for cell, text := range m.markdownFindTexts(i) {
			if len(m.blocks[i].cells) == 0 {
				cell = -1
			}
			for _, at := range re.FindAllStringIndex(text, maxMatches-len(m.found)) {
				if at[0] < at[1] {
					m.found = append(m.found, markdownMatch{i, cell, at[0], at[1]})
				}
			}
			if len(m.found) >= maxMatches {
				break
			}
		}
	}
	switch {
	case len(m.found) == 0:
		m.findCur = -1
	case queryChanged:
		// The first match from the top of the preview.
		first, _ := m.list.Visible()
		m.findCur = sort.Search(len(m.found), func(k int) bool { return m.found[k].block >= first })
		if m.findCur == len(m.found) {
			m.findCur = 0
		}
		m.reveal = true
	case m.findCur >= len(m.found):
		m.findCur = len(m.found) - 1
	}
}

// stepPreview goes to the next match in a preview, or the previous, round
// the ends.
func (a *app) stepPreview(m *markdownPreview, by int) {
	a.searchPreview(m)
	if len(m.found) == 0 {
		return
	}
	if m.findCur < 0 {
		m.findCur = 0
	} else {
		m.findCur = (m.findCur + by + len(m.found)) % len(m.found)
	}
	m.reveal, a.find.back = true, by < 0
}

// previewHits are the matches of find in the text of cell of block i of
// a preview, -1 for none, from base on in it, as the text from there
// shows them; nil while find shows none in the preview.
func (a *app) previewHits(m *markdownPreview, i, cell, base int) []markdownHit {
	if m == nil || !a.find.open || a.findPreview != m || len(m.found) == 0 {
		return nil
	}
	k := sort.Search(len(m.found), func(k int) bool { return m.found[k].block >= i })
	var hits []markdownHit
	for ; k < len(m.found) && m.found[k].block == i; k++ {
		if f := m.found[k]; f.cell == cell && f.end > base {
			hits = append(hits, markdownHit{f.start - base, f.end - base, k == m.findCur})
		}
	}
	return hits
}

// previewFindView is the find widget over a Markdown preview, which goes
// to the block of the current match.
func (a *app) previewFindView(c *ui.Context, v *viewer) {
	f, m := &a.find, v.markdown
	if !f.open {
		return
	}
	a.findPreview = m
	a.searchPreview(m)
	if m.reveal && m.findCur >= 0 {
		m.reveal = false
		i := m.found[m.findCur].block
		y, ok := a.markdownRowY(c, v, i)
		if h := v.pane.Bounds(c).H; !ok || y < 0 || i < len(m.sync.rows) && y+m.sync.rows[i].Bounds(c).H > h {
			m.list.ScrollTo(i, ui.Center)
			// The text beside follows the preview, rather than the other way.
			m.sync.placing, m.sync.userAt = false, time.Now()
		}
	}
	a.findBoxView(c, findBox{
		n: len(m.found), cur: m.findCur, err: m.findErr,
		step: func(by int) { a.stepPreview(m, by) },
	})
}

// markdownSpans are the spans of a run, those of the matches of find in
// it highlighted: hits are where they are in the text of the runs, at
// where the run starts in it.
func (a *app) markdownSpans(r markdownInline, color ui.Color, size float32, blockCode bool, hits []markdownHit, at int) []ui.Span {
	span := a.markdownSpan(r, color, size, blockCode)
	if len(hits) == 0 || markdownFindRun(r) != r.text || r.text == "" {
		return []ui.Span{span}
	}
	// Where the run's text is in the span's, past a chip's thin space.
	off := strings.Index(span.Text, r.text)
	if off < 0 {
		return []ui.Span{span}
	}
	end := at + len(r.text)
	var out []ui.Span
	from := 0
	// hit is 0 for text out of the matches, 1 in one, 2 in the current.
	piece := func(to, hit int) {
		if to > from {
			s := span
			s.Text = span.Text[from:to]
			switch hit {
			case 1:
				s.Background, s.Radius = a.pal.match, 0
			case 2:
				// Underlined where the editor rings it, as a span has no
				// border.
				s.Background, s.Radius, s.Underline = a.pal.matchNow, 0, true
				s.DecorationColor, s.DecorationThickness = a.pal.matchRing, 2
			}
			out = append(out, s)
		}
		from = to
	}
	for _, h := range hits {
		if h.end <= at || h.start >= end {
			continue
		}
		hit := 1
		if h.cur {
			hit = 2
		}
		piece(off+max(h.start, at)-at, 0)
		piece(off+min(h.end, end)-at, hit)
	}
	piece(len(span.Text), 0)
	return out
}
