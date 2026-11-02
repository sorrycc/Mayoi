package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
)

func TestProgLine(t *testing.T) {
	for _, tc := range []struct {
		r    terminal.StatusRecord
		want string
	}{
		{terminal.StatusRecord{State: terminal.StatusWorking, App: "cargo", Message: "Compiling"}, "cargo: Compiling"},
		{terminal.StatusRecord{State: terminal.StatusBlocked, Kind: terminal.KindPermission, App: "claude-code"}, "claude-code: Waiting for your approval"},
		{terminal.StatusRecord{State: terminal.StatusDone, App: "deploy", Title: "eu-west"}, "eu-west: Done"},
		{terminal.StatusRecord{State: terminal.StatusError}, "Failed"},
	} {
		if got := progLine(tc.r); got != tc.want {
			t.Errorf("progLine(%+v) = %q, want %q", tc.r, got, tc.want)
		}
	}
}

// TestProgramStatusAlert: a program that finishes marks the project on
// the rail; one that goes on working, or says the same again, does not.
func TestProgramStatusAlert(t *testing.T) {
	a := &app{}
	s := &session{}
	working := terminal.ProgramStatus{Records: []terminal.StatusRecord{{State: terminal.StatusWorking, App: "codex"}}}
	a.programStatus(s, nil, working)
	if a.alert != "" {
		t.Fatalf("working alerted %q", a.alert)
	}
	if r, busy := progStatus(s); !busy || r.App != "codex" {
		t.Fatalf("progStatus = %+v, %v", r, busy)
	}
	done := terminal.ProgramStatus{Records: []terminal.StatusRecord{{State: terminal.StatusDone, App: "codex", Message: "Fixed the bug"}}}
	a.programStatus(s, nil, done)
	if a.alert != "codex: Fixed the bug" {
		t.Fatalf("alert %q", a.alert)
	}
	a.alert = ""
	a.programStatus(s, nil, done)
	if a.alert != "" {
		t.Fatalf("the same status alerted again: %q", a.alert)
	}
	idle := terminal.ProgramStatus{Records: []terminal.StatusRecord{{State: terminal.StatusIdle, App: "codex"}}}
	a.programStatus(s, nil, idle)
	if _, busy := progStatus(s); busy {
		t.Fatal("an idle program shows a badge")
	}
}

// TestProgParts: what a program says goes on one line, and its label
// comes before its name.
func TestProgParts(t *testing.T) {
	who, what := progParts(terminal.StatusRecord{State: terminal.StatusWorking, App: "codex", Title: " fix\tbug ", Message: "Reading\nfiles  now"})
	if who != "fix bug" || what != "Reading files now" {
		t.Fatalf("progParts = %q, %q", who, what)
	}
}

// TestProgTip: the icon's tooltip lists the records not idle, the one
// most in need of the user first, up to progTipMax, then how many more.
func TestProgTip(t *testing.T) {
	st := terminal.ProgramStatus{Records: []terminal.StatusRecord{
		{ID: "", State: terminal.StatusWorking, App: "codex", Message: "Reading"},
		{ID: "a", State: terminal.StatusIdle, App: "codex"},
		{ID: "b", State: terminal.StatusBlocked, Kind: terminal.KindQuestion, App: "codex"},
	}}
	if got, want := progTip(st), "codex: Waiting for your answer\ncodex: Reading"; got != want {
		t.Fatalf("progTip = %q, want %q", got, want)
	}
	if n := progOthers(st); n != 1 {
		t.Fatalf("progOthers = %d, want 1", n)
	}
	st.Records = nil
	for i := range progTipMax + 3 {
		st.Records = append(st.Records, terminal.StatusRecord{ID: string(rune('a' + i)), State: terminal.StatusWorking, App: "make"})
	}
	lines := strings.Split(progTip(st), "\n")
	if len(lines) != progTipMax+1 || lines[progTipMax] != "+3" {
		t.Fatalf("progTip of %d records = %q", len(st.Records), lines)
	}
}

// TestStatusIn: of many statuses a program says before the app takes
// them, the first is posted and the last is taken.
func TestStatusIn(t *testing.T) {
	var in statusIn
	posts := 0
	for i := range 100 {
		if in.put(terminal.ProgramStatus{Records: []terminal.StatusRecord{{State: terminal.StatusWorking, Progress: i}}}) {
			posts++
		}
	}
	st, ok := in.take()
	if posts != 1 || !ok || st.Records[0].Progress != 99 {
		t.Fatalf("posts %d, take = %+v, %v", posts, st, ok)
	}
	if _, ok := in.take(); ok {
		t.Fatal("took a status twice")
	}
	if !in.put(terminal.ProgramStatus{}) {
		t.Fatal("a status after a take is not posted")
	}
}

// TestProgPilled: the pill shows work going on and, for a moment, work
// that finished as it was shown; not work that finished unseen.
func TestProgPilled(t *testing.T) {
	a := &app{}
	s := &session{}
	rec := func(st terminal.StatusState) terminal.ProgramStatus {
		return terminal.ProgramStatus{Records: []terminal.StatusRecord{{State: st, App: "codex", Progress: -1}}}
	}
	a.programStatus(s, nil, rec(terminal.StatusWorking))
	if _, ok := progPilled(s); !ok {
		t.Fatal("working is not shown")
	}
	a.programStatus(s, nil, rec(terminal.StatusDone))
	if r, ok := progPilled(s); !ok || r.State != terminal.StatusDone {
		t.Fatalf("done after working: %+v, %v", r, ok)
	}
	s.statusSince = s.statusSince.Add(-progFlashFor)
	if _, ok := progPilled(s); ok {
		t.Fatal("done is shown after progFlashFor")
	}
	s = &session{}
	a.programStatus(s, nil, rec(terminal.StatusError))
	if _, ok := progPilled(s); ok {
		t.Fatal("failed work never shown working is shown")
	}
}
