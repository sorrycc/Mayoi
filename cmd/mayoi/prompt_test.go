package main

import (
	"strings"
	"testing"
)

// TestConfirmQueuesBehindOpenOne: a question that comes up in the
// background, as a save's conflict, waits for the one being answered,
// and shows once it is.
func TestConfirmQueuesBehindOpenOne(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	ran := false
	a.present(&confirm{title: "First?", ok: "Go On Now", run: func() { ran = true }})
	hs.settle()
	a.saveConflict(a.activeEditor().document, nil, func(error) {})
	hs.settle()
	if a.confirm == nil || a.confirm.title != "First?" {
		t.Fatalf("the question shown is %+v", a.confirm)
	}
	if err := hs.tt.Click("Go On Now"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !ran {
		t.Fatal("the first question did not run")
	}
	if a.confirm == nil || !strings.Contains(a.confirm.title, "main.go") {
		t.Fatalf("the conflict does not follow: %+v", a.confirm)
	}
	// Escape answers it, and nothing is left to ask.
	a.dismiss()
	hs.settle()
	if a.confirm != nil || len(a.asks) != 0 {
		t.Fatalf("still asking: %+v, %d waiting", a.confirm, len(a.asks))
	}
}

// TestBackgroundConfirmWaitsForPicker: a question that comes up in the
// background while a picker is open waits until it closes.
func TestBackgroundConfirmWaitsForPicker(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	a.openQuick("")
	hs.settle()
	a.saveConflict(a.activeEditor().document, nil, func(error) {})
	hs.settle()
	if a.confirm != nil {
		t.Fatalf("asked over quick open: %+v", a.confirm)
	}
	a.closeQuick()
	hs.settle()
	if a.confirm == nil || !strings.Contains(a.confirm.title, "main.go") {
		t.Fatalf("the conflict does not follow quick open: %+v", a.confirm)
	}
}
