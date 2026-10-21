package main

import (
	"fmt"
	"net"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// The scroll bar must not take clicks in the lower half of a terminal's
// chip, including after scrolling or resizing the panel.
func TestPanelTerminalChipsStayClickable(t *testing.T) {
	if testing.Short() {
		t.Skip("loads libghostty-vt")
	}
	for _, dark := range []bool{false, true} {
		for _, width := range []int{1000, 640} {
			t.Run(fmt.Sprintf("dark=%v/width=%d", dark, width), func(t *testing.T) {
				hs := newHarness(t, dark)
				a := hs.a
				t.Cleanup(a.closeTerminals)
				for i := range 2 {
					conn, peer := net.Pipe()
					t.Cleanup(func() { peer.Close() })
					term, err := terminal.New(terminal.Options{Conn: conn})
					if err != nil {
						conn.Close()
						t.Fatal(err)
					}
					a.terms = append(a.terms, &session{
						id: i + 1, term: term,
						title: fmt.Sprintf("terminal-session-%d-with-a-long-title", i+1),
					})
				}
				first, second := a.terms[0], a.terms[1]
				a.panelTerm = second
				// Render the real panel with the app's scroll bar width, with
				// no shell output or sidebar affecting the fixture's size.
				var terminalBox ui.Rect
				tt := ui.NewTester(func(c *ui.Context) {
					a.pal = a.applyTheme(c)
					a.panel(c)
					terminalBox = a.panelTerm.view.Bounds(c)
				}, width, 280)
				tt.SetDark(dark)
				hs.tt = tt
				for range 5 {
					step(tt)
				}
				wantY := float32(panelHeadH + 2)
				if width == 640 {
					wantY += 14
				}
				if terminalBox.Y != wantY {
					t.Errorf("terminal starts at %v, want %v; reserve space only for an overflowing strip", terminalBox.Y, wantY)
				}
				chip := func(s *session) ui.Rect {
					t.Helper()
					box, ok := tt.Find(s.title)
					if !ok {
						t.Fatalf("no chip for %q", s.title)
					}
					return box
				}
				click := func(s, other *session, fraction float32) {
					t.Helper()
					a.panelTerm = other
					step(tt)
					box := chip(s)
					tt.ClickAt(box.X+box.W/2, box.Y+box.H*fraction)
					step(tt)
					if a.panelTerm != s {
						t.Errorf("click at %.0f%% of chip height did not select %q", fraction*100, s.title)
					}
				}
				for _, fraction := range []float32{0.1, 0.5, 0.9} {
					click(first, second, fraction)
				}
				before := chip(first)
				secondX := chip(second).X
				x, y := before.X+before.W/2, before.Y+before.H/2
				tt.Move(x, y)
				tt.Scroll(x, y, 120, 0)
				step(tt)
				after := chip(first)
				hs.shot(fmt.Sprintf("terminal-chips-%v-%d", dark, width))
				if width == 640 {
					if chip(second).X >= secondX {
						t.Fatal("overflowing chips did not scroll horizontally")
					}
					click(second, first, 0.9)
					// The separate bottom lane still takes scroll bar drags.
					tt.Scroll(x, y, -1000, 0)
					step(tt)
					before = chip(first)
					secondX = chip(second).X
					barY := float32(panelHeadH) + 7
					tt.Press(before.X+30, barY)
					tt.Move(before.X+90, barY)
					tt.Release(before.X+90, barY)
					step(tt)
					if chip(second).X >= secondX {
						t.Error("the scroll bar no longer drags the chips")
					}
					click(second, first, 0.5)
				} else if after.X != before.X {
					t.Error("chips scrolled when they fit in the header")
				}
				tt.SetSize(1000, 280)
				for range 3 {
					step(tt)
				}
				click(first, second, 0.9)
				if terminalBox.Y != panelHeadH+2 {
					t.Errorf("widening the panel left an empty scroll bar lane: terminal starts at %v", terminalBox.Y)
				}
				// Narrowing it again restores the lane before a click; titles
				// becoming shorter remove it without another window resize.
				tt.SetSize(640, 280)
				for range 3 {
					step(tt)
				}
				if terminalBox.Y != panelHeadH+14+2 {
					t.Errorf("narrowing the panel did not reserve the scroll bar lane: terminal starts at %v", terminalBox.Y)
				}
				click(first, second, 0.9)
				first.title, second.title = "one", "two"
				for range 3 {
					step(tt)
				}
				if terminalBox.Y != panelHeadH+2 {
					t.Errorf("shorter titles left an empty scroll bar lane: terminal starts at %v", terminalBox.Y)
				}
				click(first, second, 0.9)
			})
		}
	}
}
