package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/remote"
)

func TestQuickPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	config := filepath.Join(home, ".ssh", "config")
	writeFile(t, config, nil)
	spaced := filepath.Join(home, "my notes", "a.txt")
	writeFile(t, spaced, nil)
	cases := []struct {
		root, query, want string
		line, col         int
	}{
		{root: "", query: config, want: config},
		{root: "", query: "  " + config + "  ", want: config},
		{root: "", query: "~/.ssh/config", want: config},
		{root: t.TempDir(), query: "~/.ssh/../.ssh/config", want: config},
		{root: "", query: "~/.ssh/config:12", want: config, line: 12},
		{root: "", query: config + ":12:7", want: config, line: 12, col: 7},
		{root: "", query: config + ":0", want: ""},
		{root: "", query: "'" + spaced + "'", want: spaced},
		{root: "", query: `"~/my notes/a.txt"`, want: spaced},
		{root: "", query: "~/.ssh", want: ""},
		{root: "", query: "~/", want: ""},
		{root: "", query: "~/missing.txt", want: ""},
		{root: "", query: ".ssh/config", want: ""},
		{root: "", query: "~.ssh/config", want: ""},
		{root: remote.Path("vps", "/srv/app"), query: config, want: ""},
	}
	// A backslash escapes as in a shell only where it isn't the separator.
	if filepath.Separator != '\\' {
		cases = append(cases, struct {
			root, query, want string
			line, col         int
		}{root: "", query: `~/my\ notes/a.txt`, want: spaced})
	}
	for _, tc := range cases {
		got, line, col := quickPath(tc.root, tc.query)
		if got != tc.want || line != tc.line || col != tc.col {
			t.Errorf("quickPath(%q, %q) = %q, %d, %d; want %q, %d, %d", tc.root, tc.query, got, line, col, tc.want, tc.line, tc.col)
		}
	}
}

// A path with a line opens the file at that line, and lists the file as
// the query names it, with ~ for the home folder.
func TestQuickOpenPathLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeFile(t, filepath.Join(home, "a.txt"), []byte("one\ntwo\nthree\n"))
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	a.openQuick("")
	hs.settle()
	tt.Type(filepath.Join(home, "a.txt") + ":3:2")
	hs.settle()
	if !tt.HasText("~" + string(filepath.Separator) + "a.txt") {
		t.Fatalf("the path is not offered under ~: %q", tt.Texts())
	}
	hs.key(0, ui.KeyEnter)
	d := a.activeEditor()
	if d == nil || d.path != filepath.Join(home, "a.txt") {
		t.Fatalf("editor = %v", d)
	}
	// "one\ntwo\n" and a character of "three".
	if d.caret != 9 {
		t.Fatalf("caret at %d, want 9", d.caret)
	}
}

// A path that names no file searches the folder as any query does, so
// Return opens nothing rather than a folder or a missing file.
func TestQuickOpenPathNotAFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeFile(t, filepath.Join(home, ".ssh", "config"), nil)
	for _, query := range []string{"~/.ssh", "~/missing.txt", filepath.Join(home, "missing.txt")} {
		hs := newHarness(t, true)
		a, tt := hs.a, hs.tt
		a.openFile(hs.path("main.go"), true)
		a.openQuick("")
		hs.settle()
		tt.Type(query)
		hs.settle()
		hs.key(0, ui.KeyEnter)
		if d := a.activeEditor(); !a.quick.open || d == nil || d.path != hs.path("main.go") {
			t.Fatalf("%q: quick open = %v, editor = %v", query, a.quick.open, d)
		}
	}
}

func TestQuickOpenExternalPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	config := filepath.Join(home, ".ssh", "config")
	const text = "Host dev\n  HostName example.test\n"
	writeFile(t, config, []byte(text))
	spaced := filepath.Join(home, "my notes", "日本語 notes.txt")
	writeFile(t, spaced, []byte("outside the project\n"))

	for _, tc := range []struct {
		name, query, want string
		blank, side       bool
	}{
		{name: "absolute", query: config, want: config},
		{name: "home", query: "~/.ssh/config", want: config},
		{name: "spaces and unicode", query: spaced, want: spaced},
		{name: "without a project", query: config, want: config, blank: true},
		{name: "home without a project", query: "~/.ssh/config", want: config, blank: true},
		{name: "to the side", query: config, want: config, side: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hs := newHarness(t, true)
			a, tt := hs.a, hs.tt
			if tc.blank {
				a.closeFolder()
			} else {
				a.openFile(hs.path("main.go"), true)
			}
			a.openQuick("")
			hs.settle()
			tt.Type(tc.query)
			hs.settle()
			if !tt.HasText(filepath.Base(tc.want)) {
				t.Fatalf("the path is not offered: %q", tt.Texts())
			}
			hs.shot("quick-external-" + tc.name)
			mods := ui.Modifiers(0)
			if tc.side {
				mods = ui.Cmd
			}
			hs.key(mods, ui.KeyEnter)
			d := a.activeEditor()
			if a.quick.open || d == nil || d.path != tc.want {
				t.Fatalf("quick open = %v, editor = %v; want %q", a.quick.open, d, tc.want)
			}
			data, err := os.ReadFile(tc.want)
			if err != nil || d.buf.String() != string(data) {
				t.Fatalf("opened text = %q, read error = %v", d.buf.String(), err)
			}
			if tc.side && (len(a.cols) != 2 || len(a.cols[0].tabs) != 1) {
				t.Fatal("opening to the side did not preserve the first column")
			}
			// A path result must not replace the project's fuzzy matches.
			a.openQuick("maingo")
			hs.settle()
			if !tc.blank {
				hs.key(0, ui.KeyEnter)
				if d := a.activeEditor(); d == nil || d.path != hs.path("main.go") {
					t.Fatalf("project search after a path opened %v", d)
				}
			}
		})
	}
}
