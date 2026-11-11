package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
	"mayoi/locales"
)

// Long paths must not draw through the counts, or beyond the column,
// including when a file was renamed or has not finished loading.
func TestDiffFileHeaderNarrow(t *testing.T) {
	for _, dark := range []bool{false, true} {
		hs := newHarness(t, dark)
		a := hs.a
		for _, actions := range []bool{false, true} {
			buttonSpace := float32(0)
			if actions {
				buttonSpace = 76
			}
			for _, width := range []int{180, 320, 480, 720} {
				for _, kind := range []string{"changed", "renamed", "loading", "binary"} {
					t.Run(fmt.Sprintf("dark=%v/actions=%v/%s/%d", dark, actions, kind, width), func(t *testing.T) {
						f := &diffFile{loaded: kind != "loading", binary: kind == "binary", letter: 'M'}
						if kind == "renamed" {
							f.base, f.letter = diffStaged, 'R'
						}
						r := ui.Rect{X: 20, Y: 10, W: float32(width), H: a.font.lineH}
						render := func(path string) *image.RGBA {
							f.path = hs.path(path)
							f.entry = git.Entry{Path: f.path}
							if kind == "renamed" {
								f.entry.Orig = hs.path("old/" + path)
							}
							tt := ui.NewTester(func(c *ui.Context) {
								ui.Box(c).Fill().Background(a.pal.editor).Draw(func(pt *ui.Painter, _ ui.Rect) {
									a.paintFileHeader(pt, f, r, actions, buttonSpace, func(x, y float32, s string, col ui.Color) float32 {
										span := a.font.span(s, col)
										w, h := pt.MeasureText(0, span)
										pt.RichText(x, y+(r.H-h)/2, 0, span)
										return w
									})
								})
							}, width+180, 60)
							tt.SetDark(dark)
							step(tt)
							if path != "x.ts" {
								shot := &harness{t: t, tt: tt}
								shot.shot(fmt.Sprintf("diff-header-%v-%v-%s-%d", dark, actions, kind, width))
							}
							return tt.Image()
						}
						short := render("x.ts")
						long := render(filepath.FromSlash("packages/runtime/src/utils/session-import.scan.test.ts"))
						// Compare the reserved statistics area and the adjacent column
						// against a short path with identical counts and file status.
						left := int(r.X + r.W - a.fileHeaderRight(f) - buttonSpace)
						for y := int(r.Y + 1); y < int(r.Y+r.H-1); y++ {
							for x := left; x < long.Bounds().Max.X; x++ {
								if short.RGBAAt(x, y) != long.RGBAAt(x, y) {
									t.Fatalf("long path overlaps statistics or adjacent column at (%d,%d)", x, y)
								}
							}
						}
					})
				}
			}
		}
	}
}

// Both the regular and sticky file headers must keep their actions
// reachable when the diff's column is narrow.
func TestDiffFileHeaderNarrowActions(t *testing.T) {
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	git := hs.gitRepo()
	path := hs.path("packages/runtime/src/utils/session-import.scan.test.ts")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for i := range 200 {
		fmt.Fprintf(&body, "line %d\n", i)
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "long path")
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(body.String(), "0\n", "zero\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	a.showChanges(diffTarget{path: path, base: diffIndex}, true)
	hs.settleGit()
	v := a.activeTab().diff
	a.window.SidebarWidth = 280
	tt.SetSize(780, 500)
	hs.settle()
	if len(v.heads) != 1 {
		t.Fatalf("file headers: %v", v.heads)
	}
	head := v.heads[0]
	for _, sticky := range []bool{false, true} {
		v.scroll.Y = 0
		if sticky {
			v.scroll.Y = editorPadY + float32(v.tops[head]+10)*a.font.lineH
		}
		hs.settle()
		pane, ok := tt.Find(locales.DiffPaneLabel())
		if !ok {
			t.Fatal("no diff pane")
		}
		y := pane.Y + editorPadY + float32(v.tops[head])*a.font.lineH + a.font.lineH/2
		if sticky {
			y = pane.Y + a.font.lineH/2
		}
		tt.Move(pane.X+60, y)
		hs.settle()
		for _, label := range []string{openLabel(v.files[0].entry), locales.StageChanges(), locales.DiscardChangesEllipsis()} {
			button, ok := tt.Find(label)
			if !ok || button.X < pane.X || button.X+button.W > pane.X+pane.W || button.Y > y || button.Y+button.H < y {
				t.Fatalf("sticky=%v: file action %q outside header: %+v", sticky, label, button)
			}
		}
		hs.shot(fmt.Sprintf("diff-narrow-actions-sticky-%v", sticky))
	}
	if err := tt.Click(locales.StageChanges()); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	if staged := git("diff", "--cached", "--name-only"); !strings.Contains(staged, "session-import.scan.test.ts") {
		t.Fatalf("sticky header action did not stage the file: %q", staged)
	}
}
