package main

import (
	"os"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"mayoi/locales"
)

// TestFindCountFits checks that find's count of matches has room for
// every text it shows, in every language, as in Chinese it had not for
// one match of five.
func TestFindCountFits(t *testing.T) {
	locale := i18n.Locale()
	t.Cleanup(func() { i18n.SetLocaleFunc(func() string { return locale }) })
	for _, loc := range []string{"en", "ja", "zh-Hans"} {
		i18n.SetLocaleFunc(func() string { return loc })
		m := locales.In(loc)
		a := &app{}
		ui.NewTester(func(c *ui.Context) {
			w := a.findCountWidth(c)
			for _, s := range []string{
				m.FindCount(1, 5), m.FindCount(19999, 19999), m.FindCountMore(maxMatches, maxMatches),
				m.FindInvalid(), m.FindNoResults(),
			} {
				if tw, _ := c.MeasureText(0, ui.Span{Text: s, Size: findCountSize}); tw > w {
					t.Errorf("%s: %q is %.1f wide, the count %.1f", loc, s, tw, w)
				}
			}
		}, 400, 100)
	}
}

// TestFindRevealsAMatchUnderItsBox: a match in the top rows, behind the
// find widget, scrolls down into sight, as one out of view does.
func TestFindRevealsAMatchUnderItsBox(t *testing.T) {
	for _, wrap := range []bool{false, true} {
		hs := newHarness(t, true)
		a, tt := hs.a, hs.tt
		a.settings.WordWrap = wrap
		a.openFile(hs.longFile("long.go", 200), true)
		hs.settle()
		d := a.activeEditor()
		snap := d.buf.Snapshot()
		line := 2 + 50*4 // func f50
		g := a.geometry(d, snap.LineCount())
		d.vscroll.Y = g.top(line)
		hs.settle()
		y := d.vscroll.Y
		a.run("edit.find")
		hs.settle()
		tt.Type("f50()")
		hs.settle()
		if n := len(a.find.matches); n != 1 {
			t.Fatalf("wrap %v: %d matches", wrap, n)
		}
		box, ok := tt.Find(locales.FindLabel())
		if !ok {
			t.Fatal("no find box")
		}
		view := d.lastView
		g = a.geometry(d, d.buf.Snapshot().LineCount())
		if top := view.Y + editorPadY + g.top(line) - d.vscroll.Y; top < box.Y+box.H {
			t.Errorf("wrap %v: the match at %v is under the find box down to %v, scrolled from %v to %v", wrap, top, box.Y+box.H, y, d.vscroll.Y)
		}
	}
}

// TestFindFindsAMultilineSelection: a selection of several lines becomes
// the query of find, and find matches and replaces it whole, on every
// line it takes, as Return steps and does not break the line.
func TestFindFindsAMultilineSelection(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	p := hs.path("multi.txt")
	if err := os.WriteFile(p, []byte("one\ntwo\nthree\none\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFile(p, true)
	hs.settle()
	d := a.activeEditor()
	snap := d.buf.Snapshot()
	from, _ := snap.LineRange(0)
	_, to := snap.LineRange(2)
	d.selectRange(from, to, false)
	hs.settle()
	a.run("edit.replace")
	hs.settle()
	f := &a.find
	if f.query != "one\ntwo\nthree" {
		t.Fatalf("query = %q; want the selection", f.query)
	}
	if len(f.matches) != 2 {
		t.Fatalf("%d matches; want 2", len(f.matches))
	}
	if rects := a.visibleMatches(d, 0, snap.LineCount()-1); len(rects) != 6 {
		t.Fatalf("%d rects; want three lines of each match", len(rects))
	}
	tt.Key(0, ui.KeyEnter)
	hs.settle()
	if f.cur != 1 {
		t.Fatalf("Return went to match %d; want 1", f.cur)
	}
	if f.query != "one\ntwo\nthree" {
		t.Fatalf("Return broke the line of the query: %q", f.query)
	}
	f.with = "1\n2"
	a.replaceAll(d)
	if got := d.buf.Snapshot().String(); got != "1\n2\n1\n2\nfour\n" {
		t.Fatalf("replaced to %q", got)
	}
}

// TestFindShiftEnterGoesBack: Return goes to the next match and
// Shift+Return to the one before, wrapping; in the replace field
// Shift+Return replaces the match and goes to the one before it.
func TestFindShiftEnterGoesBack(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	p := hs.path("many.txt")
	if err := os.WriteFile(p, []byte("a x\nb x\nc x\nd x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFile(p, true)
	hs.settle()
	a.run("edit.replace")
	hs.settle()
	tt.Type("x")
	hs.settle()
	f := &a.find
	if len(f.matches) != 4 {
		t.Fatalf("%d matches", len(f.matches))
	}
	f.cur = 1
	tt.Key(0, ui.KeyEnter)
	hs.settle()
	if f.cur != 2 {
		t.Fatalf("Return went to match %d; want 2", f.cur)
	}
	tt.Key(ui.Shift, ui.KeyEnter)
	hs.settle()
	if f.cur != 1 {
		t.Fatalf("Shift+Return went to match %d; want 1", f.cur)
	}
	tt.Key(ui.Shift, ui.KeyEnter)
	tt.Key(ui.Shift, ui.KeyEnter)
	hs.settle()
	if f.cur != 3 {
		t.Fatalf("Shift+Return from the first went to match %d; want the last", f.cur)
	}

	tt.Click(locales.FindReplace())
	tt.Type("y")
	hs.settle()
	tt.Key(ui.Shift, ui.KeyEnter)
	hs.settle()
	d := a.activeEditor()
	if got := d.buf.Snapshot().Slice(0, d.buf.Snapshot().Len()); got != "a x\nb x\nc x\nd y\n" {
		t.Fatalf("Shift+Return replaced to %q", got)
	}
	if f.cur != 2 {
		t.Errorf("Shift+Return after replacing went to match %d; want 2", f.cur)
	}
}
