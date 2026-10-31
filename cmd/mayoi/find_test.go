package main

import (
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
