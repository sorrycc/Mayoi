package main

import (
	"os"
	"strings"
	"testing"
)

func TestShotDecorations(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	src, _ := os.ReadFile("editor.go")
	for _, dark := range []bool{true, false} {
		hs := newHarness(t, dark)
		os.WriteFile(hs.path("editor.go"), src, 0o644)
		hs.a.openFile(hs.path("editor.go"), true)
		hs.settle()
		d := hs.a.activeEditor()
		i := strings.Index(string(src), "func newCodeFont")
		i = strings.Index(string(src)[i:], "{") + i + 1
		at := len([]rune(string(src)[:i]))
		hs.place(d, at, at)
		name := "deco-light"
		if dark {
			name = "deco-dark"
		}
		hs.shot(name)
		// The word at the caret.
		j := strings.Index(string(src), "family string")
		at = len([]rune(string(src)[:j])) + 2
		hs.place(d, at, at)
		hs.shot(name + "-word")
	}
}

func TestShotSticky(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	src, _ := os.ReadFile("editor.go")
	for _, dark := range []bool{true, false} {
		hs := newHarness(t, dark)
		os.WriteFile(hs.path("editor.go"), src, 0o644)
		hs.a.openFile(hs.path("editor.go"), true)
		hs.settle()
		d := hs.a.activeEditor()
		i := strings.Index(string(src), "for _, w := range deco.words")
		at := len([]rune(string(src)[:i]))
		hs.place(d, at, at)
		d.vscroll.Y = editorPadY + float32(d.buf.Snapshot().LineAt(at)-8)*hs.a.font.lineH
		hs.settle()
		name := "sticky-light"
		if dark {
			name = "sticky-dark"
		}
		hs.shot(name)
	}
}

func TestShotFind(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	src, _ := os.ReadFile("editor.go")
	for _, dark := range []bool{true, false} {
		hs := newHarness(t, dark)
		os.WriteFile(hs.path("editor.go"), src, 0o644)
		hs.a.openFile(hs.path("editor.go"), true)
		hs.settle()
		hs.a.openFind(true)
		hs.a.find.query = "lineH"
		hs.a.find.stale = true
		hs.settle()
		name := "find-light"
		if dark {
			name = "find-dark"
		}
		hs.shot(name)
	}
}

func TestShotSelection(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	hs := newHarness(t, true)
	hs.a.openFile(hs.path("main.go"), true)
	hs.settle()
	os.WriteFile(hs.path("s.py"), []byte("def f():\n    if x:\n        y = 1  # a comment\n\treturn\n"), 0o644)
	hs.a.openFile(hs.path("s.py"), true)
	hs.settle()
	d := hs.a.activeEditor()
	hs.place(d, 0, d.buf.Len())
	hs.shot("selection-dark")
}

func TestShotSymbols(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	src, _ := os.ReadFile("editing.go")
	hs := newHarness(t, true)
	os.WriteFile(hs.path("editing.go"), src, 0o644)
	hs.a.openFile(hs.path("editing.go"), true)
	hs.settle()
	hs.a.openQuick("@")
	hs.settle()
	hs.shot("symbols-dark")
}

// A selection over several lines, at twice the scale for a close look.
func TestShotSelectionShape(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	src, _ := os.ReadFile("editor.go")
	for _, dark := range []bool{true, false} {
		hs := newHarness(t, dark)
		hs.tt.SetScale(2)
		os.WriteFile(hs.path("editor.go"), src, 0o644)
		hs.a.openFile(hs.path("editor.go"), true)
		hs.settle()
		d := hs.a.activeEditor()
		d.vscroll.Focus()
		i := strings.Index(string(src), "func (f codeFont) width")
		j := strings.Index(string(src), "// afterTab returns")
		a0 := len([]rune(string(src)[:i])) + 5
		a1 := len([]rune(string(src)[:j])) + 12
		hs.place(d, a0, a1)
		d.vscroll.Y = editorPadY + float32(d.buf.Snapshot().LineAt(a0)-12)*hs.a.font.lineH
		hs.settle()
		name := "selection-shape-light"
		if dark {
			name = "selection-shape-dark"
		}
		hs.shot(name)
	}
}
