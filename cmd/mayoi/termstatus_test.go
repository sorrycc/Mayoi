package main

import (
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
