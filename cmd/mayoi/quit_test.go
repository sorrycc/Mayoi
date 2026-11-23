package main

import (
	"runtime"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mayoi/locales"
)

// stubQuit counts the quits asked of the app, running then after each,
// as MyGo runs beforeQuit, in place of quitting.
func stubQuit(t *testing.T, then func()) *int {
	t.Helper()
	n := new(int)
	old := appQuit
	appQuit = func() {
		*n++
		if then != nil {
			then()
		}
	}
	t.Cleanup(func() { appQuit = old })
	return n
}

// quitFixture is a window with nothing changed, as the app keeps it.
func quitFixture(t *testing.T) (*mayoi, *app, *ui.Tester) {
	t.Helper()
	f, hs, tt := railFixture(t, workspaceFixture(t))
	f.m.frames = []*frame{f}
	return f.m, hs[0].a, tt
}

func asksToQuit(a *app) bool { return a.confirm != nil && a.confirm.title == locales.QuitTitle() }

// Cmd+Q asks first; pressing it again brings the question back rather
// than asking twice, and Return quits once, the setting as it was.
func TestQuitAsksFirst(t *testing.T) {
	m, a, tt := quitFixture(t)
	quits := stubQuit(t, nil)
	for range 2 {
		m.run("file.quit", nil)
		step(tt)
	}
	if *quits != 0 || !asksToQuit(a) || len(a.asks) != 0 {
		t.Fatalf("quit %d times, asking %+v with %d waiting", *quits, a.confirm, len(a.asks))
	}
	if !tt.HasText(locales.QuitDontAsk()) {
		t.Error("no Don't ask again")
	}
	tt.Key(0, ui.KeyEnter)
	step(tt)
	if *quits != 1 || a.confirm != nil || m.quitAsking != nil {
		t.Fatalf("Return quit %d times, still asking %+v", *quits, a.confirm)
	}
	if !m.s.ConfirmBeforeQuit {
		t.Error("quitting without Don't ask again turned the question off")
	}
}

// Escape quits nothing and keeps the setting, Don't ask again ticked or
// not, and the next Cmd+Q asks again.
func TestQuitCancelKeepsAll(t *testing.T) {
	m, a, tt := quitFixture(t)
	quits := stubQuit(t, nil)
	m.quitApp(m.wins[0])
	step(tt)
	if err := tt.Click(locales.QuitDontAsk()); err != nil {
		t.Fatal(err)
	}
	step(tt)
	if !a.confirm.checked {
		t.Fatal("Don't ask again did not tick")
	}
	tt.Key(0, ui.KeyEscape)
	step(tt)
	if *quits != 0 || a.confirm != nil || m.quitAsking != nil {
		t.Fatalf("Escape quit %d times, still asking %+v", *quits, a.confirm)
	}
	if !m.s.ConfirmBeforeQuit {
		t.Error("Escape turned the question off")
	}
	m.quitApp(m.wins[0])
	step(tt)
	if !asksToQuit(a) || a.confirm.checked {
		t.Errorf("Cmd+Q after Escape asks %+v", a.confirm)
	}
}

// Don't ask again turns the question off as Quit is chosen; Cmd+Q then
// quits at once, until the setting is on again.
func TestQuitDontAskAgain(t *testing.T) {
	m, a, tt := quitFixture(t)
	quits := stubQuit(t, nil)
	m.quitApp(m.wins[0])
	step(tt)
	if err := tt.Click(locales.QuitDontAsk()); err != nil {
		t.Fatal(err)
	}
	step(tt)
	tt.Key(0, ui.KeyEnter)
	step(tt)
	if *quits != 1 || m.s.ConfirmBeforeQuit {
		t.Fatalf("quit %d times, confirmBeforeQuit %v", *quits, m.s.ConfirmBeforeQuit)
	}
	m.quitApp(m.wins[0])
	step(tt)
	if *quits != 2 || a.confirm != nil {
		t.Fatalf("with the question off Cmd+Q quit %d times, asking %+v", *quits, a.confirm)
	}
	m.s.ConfirmBeforeQuit = true
	m.quitApp(m.wins[0])
	step(tt)
	if *quits != 2 || !asksToQuit(a) {
		t.Errorf("with the question on again Cmd+Q quit %d times, asking %+v", *quits, a.confirm)
	}
}

// Quit goes on as quitting does: it asks about a changed document, once,
// without asking whether to quit again, and Cancel there keeps the app.
func TestQuitThenAsksAboutChanges(t *testing.T) {
	f, host, tt := settleFixture(t)
	m := f.m
	a := m.wins[0].a
	quits := stubQuit(t, func() {
		if m.beforeQuit() {
			t.Error("quit with a change not asked about")
		}
	})
	m.quitApp(m.wins[0])
	step(tt)
	if len(host.asked) != 0 || !asksToQuit(a) {
		t.Fatalf("asked %v before Quit", host.asked)
	}
	tt.Key(0, ui.KeyEnter)
	step(tt)
	if *quits != 1 || len(host.asked) != 1 || len(host.held) != 1 || asksToQuit(a) {
		t.Fatalf("Quit asked %v, quitting %d times", host.asked, *quits)
	}
	// Cmd+Q while the question about the change shows brings it back.
	m.quitApp(m.wins[0])
	step(tt)
	if asksToQuit(a) || len(host.asked) != 1 {
		t.Fatalf("Cmd+Q while asking about the change asked %v", host.asked)
	}
	host.held[0](2)
	step(tt)
	if m.life != lifeOpen {
		t.Errorf("after Cancel the app is %v", m.life)
	}
}

// Quitting otherwise, as to update, log out or from the Dock, never asks
// whether to quit.
func TestBeforeQuitNeverAsksToQuit(t *testing.T) {
	m, a, tt := quitFixture(t)
	if !m.beforeQuit() {
		t.Fatal("quitting with nothing changed did not quit")
	}
	step(tt)
	if a.confirm != nil {
		t.Errorf("quitting asked %+v", a.confirm)
	}
}

// On macOS the app menu's Quit is Mayoi's own, with Cmd+Q, rather than
// the Quit role.
func TestQuitMenuItemIsOurs(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("only macOS's app menu has Quit")
	}
	_, a, _ := quitFixture(t)
	menu := a.menu(false, func(string, *mygo.Window) {})
	items := menu.Items()[0].Submenu
	last := items[len(items)-1]
	if last.Role == mygo.RoleQuit || last.Label != locales.CmdFileQuit() || last.Accelerator != "CmdOrCtrl+Q" {
		t.Errorf("the app menu ends with %+v", last)
	}
}
