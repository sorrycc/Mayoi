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
	for _, tc := range []struct{ root, query, want string }{
		{"", config, config},
		{"", "  " + config + "  ", config},
		{"", "~/.ssh/config", config},
		{t.TempDir(), "~/.ssh/../.ssh/config", config},
		{"", "~/.ssh", ""},
		{"", "~/", ""},
		{"", "~/missing.txt", ""},
		{"", ".ssh/config", ""},
		{"", "~.ssh/config", ""},
		{remote.Path("vps", "/srv/app"), config, ""},
	} {
		if got := quickPath(tc.root, tc.query); got != tc.want {
			t.Errorf("quickPath(%q, %q) = %q, want %q", tc.root, tc.query, got, tc.want)
		}
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
