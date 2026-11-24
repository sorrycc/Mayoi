package main

import (
	"bytes"
	"encoding/json"
	"image"
	"math"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/locales"
)

func columnGuideFrame(hs *harness, columns []int) *image.RGBA {
	p := hs.a.settings.Prefs
	p.ColumnGuides = columns
	hs.a.settings.SetPrefs(p)
	hs.settle()
	im := *hs.tt.Image()
	im.Pix = bytes.Clone(im.Pix)
	return &im
}

// Measure a separate text element, rather than deriving the expected
// boundary from the editor's cell-width formula.
func columnGuideTextWidth(t *testing.T, f codeFont, column int) float32 {
	t.Helper()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).AlignItems(ui.Start).Children(func() {
			ui.Text(c, strings.Repeat("0", column)).Font(f.family).FontSize(f.size).FontWeight(f.weight).
				FontFeatures(f.featureList()...).SingleLine().Label("column-boundary")
		})
	}, 4000, 100)
	r, ok := tt.Find("column-boundary")
	if !ok {
		t.Fatal("no independent text layout")
	}
	return r.W
}

func assertColumnGuideRow(t *testing.T, hs *harness, d *editor, off, on *image.RGBA, columns []int, scale float32) {
	t.Helper()
	z := float32(math.Pow(1.2, float64(hs.a.settings.ZoomLevel)))
	v := d.lastView
	if _, ok := hs.tt.Find(locales.EditorLabel(d.name())); !ok {
		t.Fatal("no editor")
	}
	zs := z * scale
	left, right := (v.X+d.textX)*zs, (v.X+v.W-textPadR)*zs
	var want []float32
	for _, column := range columns {
		x := (v.X + d.textX - d.hscroll.X + columnGuideTextWidth(t, hs.a.font, column)) * zs
		if x >= left && x < right {
			want = append(want, x)
		}
	}
	row := max(2, int((v.H-80-editorPadY)/hs.a.font.lineH))
	y := int((v.Y + editorPadY + float32(row)*hs.a.font.lineH) * zs)
	seen := make([]float64, len(want))
	for x := max(0, int(v.X*zs)); x < min(on.Bounds().Max.X, int((v.X+v.W)*zs)); x++ {
		before, after := off.RGBAAt(x, y), on.RGBAAt(x, y)
		if before == after {
			continue
		}
		matched := false
		for i, at := range want {
			if float32(x) >= at-1 && float32(x) <= at+zs+1 {
				seen[i] = max(seen[i], contrast(ui.RGBA(before.R, before.G, before.B, 1), ui.RGBA(after.R, after.G, after.B, 1)))
				matched = true
			}
		}
		if !matched {
			t.Fatalf("unexpected guide pixel (%d,%d), boundaries %v, view %+v", x, y, want, v)
		}
	}
	least := 1.0
	if hs.a.pal.dark {
		least = 1.5
	}
	for i, ratio := range seen {
		if ratio < least {
			t.Errorf("guide at independently laid-out boundary %.2f on row %d has contrast %.2f, want at least %.1f", want[i], y, ratio, least)
		}
	}
}

func TestColumnGuidesRender(t *testing.T) {
	for _, tc := range []struct {
		name, text              string
		dark, wrap              bool
		width                   int
		font, zoom, scale, x, y float32
		columns                 []int
	}{
		{"empty-light", "", false, false, 1600, 13, 0, 1, 0, 0, []int{80, 120}},
		{"mixed-dark", "\t中文🙂 test\n\n", true, false, 1600, 13, 0, 2, 0, 0, []int{80, 120}},
		{"horizontal", strings.Repeat("0", 300) + "\n\n", true, false, 1600, 13, 0, 1, 700, 0, []int{80, 120}},
		{"vertical", strings.Repeat("\n", 200), true, false, 1600, 13, 0, 1, 0, 800, []int{80, 120}},
		{"wrapped", strings.Repeat("hello 中文 ", 200), true, true, 1200, 13, 0, 1, 0, 0, []int{40, 80, 120}},
		{"narrow", strings.Repeat("word ", 200), false, true, 480, 13, 0, 1, 0, 0, []int{20, 80, 120}},
		{"font-zoom", "text\n\n", false, false, 1600, 17, 0.5, 2, 0, 0, []int{80, 120}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hs, d := editHarness(t, "guides.txt", tc.text)
			hs.a.window.SidebarClosed = true
			hs.tt.SetSize(tc.width, 760)
			hs.tt.SetDark(tc.dark)
			hs.tt.SetScale(tc.scale)
			p := hs.a.settings.Prefs
			p.FontSize, p.ZoomLevel, p.WordWrap = tc.font, tc.zoom, tc.wrap
			hs.a.settings.SetPrefs(p)
			hs.settle()
			d.hscroll.X, d.vscroll.Y = tc.x, tc.y
			off := columnGuideFrame(hs, nil)
			before := columnGuideState(hs.a, d)
			on := columnGuideFrame(hs, tc.columns)
			assertColumnGuideRow(t, hs, d, off, on, tc.columns, tc.scale)
			if after := columnGuideState(hs.a, d); after != before {
				t.Errorf("guides changed editing state: before %+v, after %+v", before, after)
			}
			hs.shot("column-guides-" + tc.name)
		})
	}
}

type guideEditorState struct {
	version            uint64
	dirty              bool
	sel                [2]int
	undo, redo, widest int
	scroll             [4]float32
	height             float32
}

func columnGuideState(a *app, d *editor) guideEditorState {
	snap := d.buf.Snapshot()
	return guideEditorState{snap.Version(), d.dirty(), d.sel, len(d.hist.undo), len(d.hist.redo), d.widest,
		[4]float32{d.hscroll.X, d.hscroll.MaxX, d.vscroll.Y, d.vscroll.MaxY}, a.geometry(d, snap.LineCount()).height()}
}

func TestColumnGuidesSplitAndTabs(t *testing.T) {
	hs, d := editHarness(t, "guides.txt", "text\n\n")
	hs.a.window.SidebarClosed = true
	hs.a.run("view.splitRight")
	hs.settle()
	right := hs.a.activeEditor()
	off := columnGuideFrame(hs, nil)
	on := columnGuideFrame(hs, []int{20, 40})
	for _, ed := range []*editor{d, right} {
		assertColumnGuideRow(t, hs, ed, off, on, []int{20, 40}, 1)
	}
	hs.a.run("file.new")
	hs.settle()
	untitled := hs.a.activeEditor()
	off = columnGuideFrame(hs, nil)
	on = columnGuideFrame(hs, []int{20, 40})
	assertColumnGuideRow(t, hs, untitled, off, on, []int{20, 40}, 1)
	hs.a.openFile(d.path, true)
	hs.settle()
	off = columnGuideFrame(hs, nil)
	on = columnGuideFrame(hs, []int{20, 40})
	assertColumnGuideRow(t, hs, hs.a.activeEditor(), off, on, []int{20, 40}, 1)
}

func TestColumnGuidesLivePreview(t *testing.T) {
	hs, d := editHarness(t, "guides.md", "# Heading\n\nText\n")
	hs.a.run("view.toggleLivePreview")
	hs.settle()
	if !hs.a.livePreview(d) {
		t.Fatal("no Live Preview")
	}
	off := columnGuideFrame(hs, nil)
	on := columnGuideFrame(hs, []int{20, 40})
	assertColumnGuideRow(t, hs, d, off, on, nil, 1)
	hs.a.run("view.toggleLivePreview")
	hs.settle()
	off = columnGuideFrame(hs, nil)
	on = columnGuideFrame(hs, []int{20, 40})
	assertColumnGuideRow(t, hs, d, off, on, []int{20, 40}, 1)
}

func TestColumnGuidesSelectionAndFolds(t *testing.T) {
	hs, d := editHarness(t, "guides.go", "package p\n\nfunc f() {\n\tprintln(\"test\")\n}\n")
	hs.a.settings.AutoSave = false
	hs.tt.Type("// typed\n")
	hs.settle()
	if !d.dirty() || len(d.hist.undo) == 0 {
		t.Fatal("no unsaved edit to preserve")
	}
	hs.place(d, 0, 9)
	hs.a.run("edit.foldAll")
	hs.settle()
	if len(d.folding.marks) == 0 {
		t.Fatal("no folded lines")
	}
	off := columnGuideFrame(hs, nil)
	before := columnGuideState(hs.a, d)
	on := columnGuideFrame(hs, []int{20, 40, 10000})
	assertColumnGuideRow(t, hs, d, off, on, []int{20, 40, 10000}, 1)
	if columnGuideState(hs.a, d) != before {
		t.Fatal("guides changed the folded editor or its selection")
	}
}

func BenchmarkColumnGuidesIdleFrame(b *testing.B) {
	for _, tc := range []struct {
		name    string
		columns []int
	}{{"off", nil}, {"on", []int{80, 120}}} {
		b.Run(tc.name, func(b *testing.B) {
			hs := benchDoc(b)
			hs.a.settings.ColumnGuides = tc.columns
			hs.a.activeEditor().vscroll.Y = 4000
			hs.settle()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				step(hs.tt)
			}
		})
	}
}

func TestColumnGuidesFolderRendering(t *testing.T) {
	hs, d := editHarness(t, "guides.txt", "text\n\n")
	f := hs.a.folderPrefs.f
	for _, tc := range []struct {
		raw     string
		columns []int
	}{
		{`[20,40]`, []int{20, 40}},
		{`[]`, nil},
		{`"bad"`, []int{80}},
	} {
		if err := f.Set("columnGuides", nil); err != nil {
			t.Fatal(err)
		}
		off := columnGuideFrame(hs, nil)
		if err := f.Set("columnGuides", json.RawMessage(tc.raw)); err != nil {
			t.Fatal(err)
		}
		on := columnGuideFrame(hs, []int{80})
		assertColumnGuideRow(t, hs, d, off, on, tc.columns, 1)
	}
}
