package main

import (
	"fmt"
	"image"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"

	"mayoi/internal/settings"
)

// TestShotProgramStatus shoots the pill over a terminal whose programs
// say what they do (OSC 7501), in each state, dark and light.
func TestShotProgramStatus(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip("SHOTS is not set")
	}
	cases := []struct {
		name string
		recs []terminal.StatusRecord
	}{
		{"working", []terminal.StatusRecord{{State: terminal.StatusWorking, App: "cargo", Message: "Compiling mayoi v0.16.2", Progress: -1}}},
		{"progress", []terminal.StatusRecord{{State: terminal.StatusWorking, App: "cargo", Message: "Compiling mayoi v0.16.2", Progress: 42}}},
		{"blocked", []terminal.StatusRecord{{State: terminal.StatusBlocked, Kind: terminal.KindPermission, App: "claude-code", Progress: -1}}},
		{"many", []terminal.StatusRecord{
			{ID: "", State: terminal.StatusWorking, App: "codex", Title: "Refactor the renderer to batch glyph uploads across frames", Message: "Reading cmd/mayoi/termstatus.go and twenty other files in the project before it writes anything", Progress: 87},
			{ID: "a", State: terminal.StatusWorking, App: "codex", Progress: -1},
			{ID: "b", State: terminal.StatusIdle, App: "codex", Progress: -1},
		}},
		{"done", []terminal.StatusRecord{{State: terminal.StatusDone, App: "codex", Message: "Fixed the bug", Progress: -1}}},
		{"failed", []terminal.StatusRecord{{State: terminal.StatusError, App: "deploy", Title: "eu-west", Progress: -1}}},
	}
	for _, dark := range []bool{true, false} {
		for _, tc := range cases {
			hs := newHarness(t, dark)
			a := hs.a
			s := hcSession(t, a, "")
			s.title = "zsh"
			a.window.Panels.On(settings.SideBottom).Open = true
			a.programStatus(s, s.term, terminal.ProgramStatus{Records: []terminal.StatusRecord{{State: terminal.StatusWorking, App: "x", Progress: -1}}})
			a.programStatus(s, s.term, terminal.ProgramStatus{Records: tc.recs})
			hs.tt.SetScale(2)
			for range 12 {
				hs.tt.Advance(16 * time.Millisecond)
				hs.tt.Frame()
			}
			name := fmt.Sprintf("prog-%s-%v", tc.name, dark)
			hs.shotRect(name, image.Rect(560, 470, 1200, 560), 2)
		}
	}
}
