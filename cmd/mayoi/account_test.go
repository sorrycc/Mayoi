package main

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/cloudsync"
	"mayoi/internal/keymap"
	"mayoi/internal/mon"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// TestSigninDialog: the code shows until the sign-in is cancelled, by its
// button or Escape, and the dialog waits for one at first.
func TestSigninDialog(t *testing.T) {
	hs := newHarness(t, true)
	cancelled := 0
	hs.a.signin = &signin{cancel: func() { cancelled++ }}
	hs.settle()
	if !hs.tt.HasText(locales.SyncGettingCode()) {
		t.Fatal("the dialog does not say it gets a code")
	}
	hs.a.signin = &signin{code: "NNCD-QHTC", url: "https://mon.sorrycc.dev/device?code=NNCD-QHTC",
		expires: time.Now().Add(14*time.Minute + 30*time.Second), cancel: func() { cancelled++ }}
	hs.settle()
	for _, r := range "NNCDQHTC" {
		if !hs.tt.HasText(string(r)) {
			t.Fatalf("the code's %c does not show", r)
		}
	}
	if !hs.tt.HasText(locales.SyncWaitingExpires("14:30")) && !hs.tt.HasText(locales.SyncWaitingExpires("14:29")) {
		t.Fatal("the dialog does not count down")
	}
	hs.shot("signin")
	if err := hs.tt.Click(locales.ButtonCancel()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if hs.a.signin != nil || cancelled != 1 || hs.tt.HasText(locales.SyncSignInTitle()) {
		t.Fatalf("Cancel: dialog %v, cancelled %d", hs.a.signin, cancelled)
	}
	hs.a.signin = &signin{code: "ABCD-EFGH", url: "https://mon.sorrycc.dev/device", cancel: func() { cancelled++ }}
	hs.settle()
	hs.tt.Key(0, ui.KeyEscape)
	hs.settle()
	if hs.a.signin != nil || cancelled != 2 {
		t.Fatalf("Escape: dialog %v, cancelled %d", hs.a.signin, cancelled)
	}
}

// TestAccountCommands: the menu offers to sign in, or names the account
// with Sync Now and Sign Out, Sync Now signs in when signed out, and
// Sign Out asks first.
func TestAccountCommands(t *testing.T) {
	hs := newHarness(t, true)
	labels := func() []string {
		var out []string
		var walk func(items []*mygo.MenuItem)
		walk = func(items []*mygo.MenuItem) {
			for _, it := range items {
				out = append(out, it.Label)
				walk(it.Submenu)
			}
		}
		walk(hs.a.menu(false, func(string, *mygo.Window) {}).Items())
		return out
	}
	if l := labels(); !slices.Contains(l, locales.CmdAccountSignIn()) || slices.Contains(l, locales.CmdAccountSignOut()) {
		t.Errorf("signed out, the menu has %q", l)
	}
	hs.a.run("account.syncNow")
	hs.a.run("account.signOut")
	hs.a.run("account.show")
	if hs.h.signIns != 2 || hs.h.syncs != 0 || hs.h.signOuts != 0 || hs.a.confirm != nil {
		t.Errorf("signed out: %d sign-ins, %d syncs, %d sign-outs", hs.h.signIns, hs.h.syncs, hs.h.signOuts)
	}
	hs.h.signedIn = "Ada"
	l := labels()
	if slices.Contains(l, locales.CmdAccountSignIn()) || !slices.Contains(l, locales.MenuSyncingAs("Ada")) || !slices.Contains(l, locales.CmdAccountSignOut()) {
		t.Errorf("signed in, the menu has %q", l)
	}
	hs.a.run("account.syncNow")
	hs.a.run("account.signOut")
	hs.settle()
	if hs.h.syncs != 1 || hs.h.signOuts != 0 || !hs.tt.HasText(locales.SyncSignOutTitle("Ada")) {
		t.Fatalf("signed in: %d syncs, %d sign-outs, asked %v", hs.h.syncs, hs.h.signOuts, hs.a.confirm != nil)
	}
	hs.shot("signout")
	if err := hs.tt.Click(locales.SyncSignOut()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if hs.h.signOuts != 1 {
		t.Errorf("Sign Out: %d sign-outs", hs.h.signOuts)
	}
}

// TestSyncStatus: signed in, the status bar has a cloud that opens the
// account's panel, which tells how the last sync went and what syncs,
// and turns the keyboard shortcuts' sync off and on.
func TestSyncStatus(t *testing.T) {
	for _, dark := range []bool{true, false} {
		hs := newHarness(t, dark)
		hs.a.settings = settings.Load(filepath.Join(t.TempDir(), "settings.json"))
		hs.settle()
		if hs.tt.HasText(locales.SyncPanelLabel("Ada Lovelace")) {
			t.Fatal("signed out, the status bar has settings sync")
		}
		if err := hs.tt.Click(locales.CmdAccountSignIn()); err != nil || hs.h.signIns != 1 {
			t.Fatalf("the welcome page does not sign in: %v", err)
		}
		hs.h.signedIn = "Ada Lovelace"
		hs.h.sync = syncInfo{email: "ada@example.com", syncedAt: time.Now().Add(-3 * time.Minute)}
		hs.settle()
		if err := hs.tt.Click(locales.SyncPanelLabel("Ada Lovelace")); err != nil {
			t.Fatal(err)
		}
		hs.settle()
		if hs.tt.HasText(locales.CmdAccountSignIn()) {
			t.Error("signed in, the welcome page offers to sign in")
		}
		if !hs.a.syncOpen || !hs.tt.HasText("ada@example.com") || !hs.tt.HasText(locales.SyncUpToDateTitle()) ||
			!hs.tt.HasText(locales.SyncSyncedAgo(locales.AgoMinutes(3))) {
			t.Fatal("the panel does not show the account and the last sync")
		}
		hs.shot(map[bool]string{true: "sync-panel-dark", false: "sync-panel-light"}[dark])
		// The keyboard shortcuts stay on this machine, and sync again.
		hs.tt.Click(locales.SyncShortcuts())
		hs.settle()
		if !slices.Equal(hs.a.settings.SyncIgnored, []string{settings.SyncKeybindings}) {
			t.Fatalf("syncIgnored = %v", hs.a.settings.SyncIgnored)
		}
		// The switch itself.
		r, _ := hs.tt.Find(locales.SyncShortcuts())
		hs.tt.ClickAt(r.X+r.W-24, r.Y+r.H/2)
		hs.settle()
		if len(hs.a.settings.SyncIgnored) != 0 {
			t.Fatalf("syncIgnored = %v", hs.a.settings.SyncIgnored)
		}
		if err := hs.tt.Click(locales.CmdAccountSyncNow()); err != nil {
			t.Fatal(err)
		}
		if hs.h.syncs != 1 {
			t.Errorf("%d syncs", hs.h.syncs)
		}
		// Failing, it says why and when it tries again; with a conflict, it
		// offers to compare.
		hs.h.sync = syncInfo{failure: locales.SyncOffline("mon.sorrycc.dev"), retryAt: time.Now().Add(25 * time.Second), conflict: true}
		hs.settle()
		if !hs.tt.HasText(locales.SyncCouldNot()) || !hs.tt.HasText(locales.SyncCompare()) {
			t.Fatal("the panel does not tell of the failure and the conflict")
		}
		hs.shot(map[bool]string{true: "sync-failed-dark", false: "sync-failed-light"}[dark])
		hs.tt.Key(0, ui.KeyEscape)
		hs.settle()
		if hs.a.syncOpen {
			t.Error("Escape left the panel open")
		}
		// Signing out elsewhere closes it.
		hs.a.syncOpen = true
		hs.h.signedIn = ""
		hs.settle()
		if hs.a.syncOpen {
			t.Error("the panel stayed open signed out")
		}
	}
}

// TestCompareKeybindings: the account's keybindings.json shows beside
// this machine's, and Keep Mine drops it.
func TestCompareKeybindings(t *testing.T) {
	hs := newHarness(t, true)
	dir := t.TempDir()
	hs.a.settings = settings.Load(filepath.Join(dir, "settings.json"))
	// Before dir goes, so that no late save of the tabs opened lands in it.
	t.Cleanup(hs.a.settings.Flush)
	hs.a.compareKeybindings()
	if len(hs.a.cols) != 1 || hs.h.resolved != 1 {
		t.Fatalf("without a conflict: %d columns, %d resolved", len(hs.a.cols), hs.h.resolved)
	}
	other := filepath.Join(dir, conflictFile)
	os.WriteFile(other, []byte("[2]"), 0o644)
	hs.a.compareKeybindings()
	hs.settle()
	if len(hs.a.cols) != 2 || hs.a.activeFile() != other {
		t.Fatalf("%d columns, showing %q", len(hs.a.cols), hs.a.activeFile())
	}
	if hs.a.cols[0].tab().path() != keymap.Path(hs.a.settings.Path()) {
		t.Errorf("the first column shows %q", hs.a.cols[0].tab().path())
	}
	hs.a.keepMyKeybindings()
	if _, err := os.Stat(other); !os.IsNotExist(err) || hs.h.resolved != 2 {
		t.Errorf("Keep Mine: %v, %d resolved", err, hs.h.resolved)
	}
}

func TestSyncError(t *testing.T) {
	if got := syncError(&mon.Error{Status: 503}); got != locales.SyncServerTrouble(monHost()) {
		t.Errorf("503: %q", got)
	}
	if got := syncError(&net.OpError{Op: "dial", Err: errors.New("refused")}); got != locales.SyncOffline(monHost()) {
		t.Errorf("offline: %q", got)
	}
	if got := countdown(65*time.Second + 300*time.Millisecond); got != "1:06" {
		t.Errorf("countdown = %q", got)
	}
	if syncRetryDelay(0) >= syncRetryDelay(3) || syncRetryDelay(99) != 5*time.Minute {
		t.Error("the retries do not back off")
	}
}

// TestCloudLocalKeybindings: applying the account's shortcuts writes
// keybindings.json, removes it for none, and keeps a conflict beside it.
func TestCloudLocalKeybindings(t *testing.T) {
	dir := t.TempDir()
	m := &mayoi{s: settings.Load(filepath.Join(dir, "settings.json"))}
	l := cloudLocal{&cloud{m: m, onMain: func(fn func()) { fn() }}}
	path := keymap.Path(m.s.Path())
	read, _ := l.Read()
	if err := l.Apply(read, cloudsync.Snapshot{Keybindings: "[1]"}, "[2]"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "[1]" {
		t.Errorf("keybindings.json = %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, conflictFile)); string(data) != "[2]" {
		t.Errorf("%s = %q", conflictFile, data)
	}
	s, err := l.Read()
	if err != nil || s.Keybindings != "[1]" {
		t.Errorf("read %q, %v", s.Keybindings, err)
	}
	if !l.IsDefault(cloudsync.Snapshot{Settings: m.s.Prefs.Synced(), Keybindings: keymap.Template}) {
		t.Error("the defaults are not default")
	}
	if err := l.Apply(s, cloudsync.Snapshot{}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("keybindings.json stayed: %v", err)
	}
}

// TestCloudLocalStale: what changed since it was read, settings.json
// saved, a preference set or keybindings.json saved, Apply leaves be.
func TestCloudLocalStale(t *testing.T) {
	dir := t.TempDir()
	m := &mayoi{s: settings.Load(filepath.Join(dir, "settings.json"))}
	m.s.Flush()
	l := cloudLocal{&cloud{m: m, onMain: func(fn func()) { fn() }}}
	path := keymap.Path(m.s.Path())
	account := cloudsync.Snapshot{Settings: settingsWith(m.s.Prefs, func(p *settings.Prefs) { p.Vim = true }), Keybindings: "[account]"}

	read, _ := l.Read()
	os.WriteFile(m.s.Path(), []byte(`{"fontSize": 17}`), 0o644)
	if err := l.Apply(read, account, ""); !errors.Is(err, cloudsync.ErrStale) {
		t.Fatalf("settings.json saved: %v", err)
	}
	if m.s.FontSize != 17 || m.s.Vim {
		t.Errorf("settings.json saved: font size %v, vim %v", m.s.FontSize, m.s.Vim)
	}
	if read, _ = l.Read(); string(read.Settings["fontSize"]) != "17" {
		t.Errorf("read %s", read.Settings["fontSize"])
	}

	m.s.Blame = !m.s.Blame
	if err := l.Apply(read, account, ""); !errors.Is(err, cloudsync.ErrStale) || m.s.Vim {
		t.Fatalf("a preference set: %v, vim %v", err, m.s.Vim)
	}

	read, _ = l.Read()
	os.WriteFile(path, []byte("[mine]"), 0o644)
	if err := l.Apply(read, account, ""); !errors.Is(err, cloudsync.ErrStale) || m.s.Vim {
		t.Fatalf("keybindings.json saved: %v, vim %v", err, m.s.Vim)
	}
	if data, _ := os.ReadFile(path); string(data) != "[mine]" {
		t.Errorf("keybindings.json = %q", data)
	}

	read, _ = l.Read()
	if err := l.Apply(read, account, ""); err != nil || !m.s.Vim {
		t.Fatalf("as read: %v, vim %v", err, m.s.Vim)
	}
	if data, _ := os.ReadFile(path); string(data) != "[account]" {
		t.Errorf("keybindings.json = %q", data)
	}
}

func settingsWith(p settings.Prefs, change func(*settings.Prefs)) map[string]json.RawMessage {
	change(&p)
	return p.Synced()
}

// TestTokenFile: in development the token is kept in a file only its
// owner reads, and a missing one is as no token kept.
func TestTokenFile(t *testing.T) {
	c := &cloud{tokenFile: filepath.Join(t.TempDir(), tokenKey)}
	if _, err := c.loadToken(); !errors.Is(err, mygo.ErrSecretNotFound) {
		t.Fatalf("loadToken() before saving = %v, want ErrSecretNotFound", err)
	}
	if err := c.saveToken([]byte("tok")); err != nil {
		t.Fatal(err)
	}
	if got, err := c.loadToken(); err != nil || string(got) != "tok" {
		t.Fatalf("loadToken() = %q, %v, want \"tok\"", got, err)
	}
	// Windows has no permission bits; the file is the user's by its folder.
	if fi, err := os.Stat(c.tokenFile); err != nil || runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("the token file's mode = %v, %v, want 0600", fi.Mode().Perm(), err)
	}
	for range 2 {
		if err := c.dropToken(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.loadToken(); !errors.Is(err, mygo.ErrSecretNotFound) {
		t.Fatalf("loadToken() after dropping = %v, want ErrSecretNotFound", err)
	}
}
