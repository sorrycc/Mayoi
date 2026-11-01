package main

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Find in a terminal with the keyboard searches its output, steps through
// the matches, and Escape ends the search, giving the terminal back the
// keyboard.
func TestTerminalFind(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	if runtime.GOOS != "windows" {
		t.Setenv("SHELL", "/bin/sh")
	}
	hs := newHarness(t, false)
	a, tt := hs.a, hs.tt
	t.Cleanup(a.closeTerminals)
	a.newTerminal()
	hs.settle()
	s := a.panelTerm
	tt.Type("echo needle-$((1+1)); echo needle-$((2+1))")
	tt.Key(0, ui.KeyEnter)
	until := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !ok() && time.Now().Before(deadline) {
			hs.settle()
		}
		if !ok() {
			t.Fatalf("%s: found %+v, text %q", what, s.found, s.term.Text())
		}
	}
	until("the shell printed", func() bool { return strings.Contains(s.term.Text(), "needle-3") })

	a.run("edit.find")
	hs.settle()
	if !s.findOpen || a.find.open {
		t.Fatal("find in the terminal did not open its find bar")
	}
	tt.Type("NEEDLE-")
	// Two in each echo of the command line, which a slow shell may show
	// twice, and the two lines printed.
	want := strings.Count(s.term.Text(), "needle-")
	until("the matches were counted", func() bool { return s.found.Total == want && !s.found.Searching })
	was := s.found.Selected
	tt.Key(0, ui.KeyEnter)
	until("Return went to another match", func() bool { return s.found.Selected != was })
	was = s.found.Selected
	tt.Key(ui.Shift, ui.KeyEnter)
	until("Shift+Return went back", func() bool { return s.found.Selected != was && s.findBack })

	tt.Key(0, ui.KeyEscape)
	hs.settle()
	if s.findOpen || s.term.SearchStatus().Text != "" {
		t.Fatal("Escape did not close the find bar and end the search")
	}
	if a.focus != paneTerminal {
		t.Fatal("closing find did not give the terminal back the keyboard")
	}
}
