package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"mayoi/internal/remote"
	"mayoi/internal/settings"

	"github.com/egoist/mygo/ui"
)

func TestRetryDelay(t *testing.T) {
	want := []time.Duration{2, 4, 8, 16, 30, 30, 30}
	for n, w := range want {
		if got := retryDelay(n); got != w*time.Second {
			t.Errorf("retryDelay(%d) = %v, want %v", n, got, w*time.Second)
		}
	}
}

func TestExpandHome(t *testing.T) {
	for _, c := range []struct{ query, want string }{
		{"~", "/home/me/"},
		{"~/src", "/home/me/src"},
		{"~/", "/home/me/"},
		{"/srv/~", "/srv/~"},
		{"~other", "~other"},
	} {
		if got := expandHome(c.query, "/home/me"); got != c.want {
			t.Errorf("expandHome(%q) = %q, want %q", c.query, got, c.want)
		}
	}
	if got := expandHome("~/x", ""); got != "~/x" {
		t.Errorf("without a home: %q", got)
	}
}

// TestRemoteReconnectsByItself: a connection lost, not ended on purpose,
// says so and connects again by itself; one ended on purpose waits.
func TestRemoteReconnectsByItself(t *testing.T) {
	if os.Getenv("MAYOI_SSH_TEST_HOST") != "" {
		t.Skip("drops the fake ssh's connection")
	}
	hs, c, _ := remoteHarness(t)
	a := hs.a
	// The network goes: the master ends, though no one ended it. Other
	// test processes use the same fake host, so only kill our own child.
	pattern := "ControlMaster=yes.* " + regexp.QuoteMeta(c.Host) + "$"
	if out, err := exec.Command("pkill", "-P", strconv.Itoa(os.Getpid()), "-f", pattern).CombinedOutput(); err != nil {
		t.Fatalf("could not stop the fake ssh master: %v: %s", err, out)
	}
	hs.untilLong("the loss", func() bool { return a.connState == remote.Offline && a.retry != nil })
	if !hs.tt.HasText("Reconnect Now") {
		t.Errorf("the banner does not offer to reconnect: %q", hs.tt.Texts())
	}
	hs.shot("remote-05-lost")
	hs.untilLong("the connection again", func() bool { return a.connState == remote.Connected })
	if a.retry != nil || a.retries != 0 {
		t.Errorf("still retrying: %v, %d", a.retry, a.retries)
	}
	// Disconnected on purpose: no retry.
	a.disconnect()
	hs.untilLong("the disconnection", func() bool { return a.connState == remote.Offline })
	hs.settle()
	if a.retry != nil {
		t.Error("a connection ended on purpose is retried")
	}
}

// TestConnectBoxRefused: a host that refuses says why, in the box of the
// hosts, which lets go of it.
func TestConnectBoxRefused(t *testing.T) {
	useFakeSSH(t)
	hs := newHarness(t, true)
	a := hs.a
	a.openRemotePicker()
	hs.settle()
	a.chooseHost("refused.invalid")
	hs.untilLong("the refusal", func() bool { return a.remotePick.stage == pickHost && a.remotePick.err != "" })
	r := &a.remotePick
	if !strings.Contains(r.err, "Permission denied") || !strings.Contains(r.err, "not passwords") {
		t.Errorf("err = %q", r.err)
	}
	if r.conn != nil || r.query != "refused.invalid" {
		t.Errorf("conn = %v, query = %q", r.conn, r.query)
	}
}

// TestConnectBoxCancel: Escape while connecting goes back to the hosts,
// and ends the connection.
func TestConnectBoxCancel(t *testing.T) {
	useFakeSSH(t)
	hs := newHarness(t, true)
	a := hs.a
	a.openRemotePicker()
	hs.settle()
	a.chooseHost("slow.invalid")
	hs.settle()
	if a.remotePick.stage != pickConnecting || !hs.tt.HasText("Signing in to slow.invalid") {
		t.Fatalf("stage = %v: %q", a.remotePick.stage, hs.tt.Texts())
	}
	hs.tt.Key(0, ui.KeyEscape)
	hs.settle()
	r := &a.remotePick
	if !r.open || r.stage != pickHost || r.query != "slow.invalid" || r.conn != nil {
		t.Fatalf("open = %v, stage = %v, query = %q", r.open, r.stage, r.query)
	}
	hs.untilLong("the end of the connection", func() bool {
		s, _ := remote.For("slow.invalid").State()
		return s == remote.Offline
	})
}

// TestShotRemotePicker draws the box of the hosts, as ~/.ssh/config and
// the recent folders list them, filtered, connecting and refused.
func TestShotRemotePicker(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	useFakeSSH(t)
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(`Host dev-box
  HostName 10.0.0.12
Host gpu-server gpu-backup
  User ml
Host staging
Host *.internal
`), 0o600)
	t.Setenv("HOME", home)
	for _, dark := range []bool{true, false} {
		name := "light"
		if dark {
			name = "dark"
		}
		hs := newHarness(t, dark)
		a := hs.a
		a.settings = settings.Default()
		a.settings.AddRecent(remote.Path("prod.example.com", "/srv/www/shop"))
		a.settings.AddRecent(remote.Path("me@pi.local", "/home/me/garden"))
		a.closeFolder()
		hs.settle()
		hs.shot("remote-welcome-" + name)
		a.openRemotePicker()
		hs.untilLong("the hosts resolved", func() bool { return len(a.remotePick.resolved) >= 6 })
		hs.settle()
		hs.shot("remote-hosts-" + name)
		hs.tt.Type("gpu")
		hs.settle()
		hs.shot("remote-hosts-filter-" + name)
		a.chooseHost("slow.invalid")
		a.remotePick.since = time.Now().Add(-7 * time.Second)
		hs.settle()
		hs.shot("remote-connecting-" + name)
		a.cancelConnect()
		a.chooseHost("refused.invalid")
		hs.untilLong("the refusal", func() bool { return a.remotePick.err != "" })
		hs.settle()
		hs.shot("remote-refused-" + name)
		a.closeRemotePicker()
	}
}

// TestShotRemoteWindow draws the window of a remote folder whose host is
// connecting, and one whose host refused.
func TestShotRemoteWindow(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	useFakeSSH(t)
	for _, host := range []string{"slow.invalid", "refused.invalid"} {
		s := settings.Default()
		w := s.NewWindow()
		w.Folder = remote.Path(host, "/srv/app")
		a := newApp(&fakeHost{}, s, w)
		t.Cleanup(a.shutdown)
		tt := ui.NewTester(a.view, 1200, 760)
		hs := &harness{t: t, a: a, h: &fakeHost{}, tt: tt, root: w.Folder}
		t.Cleanup(a.shutdown)
		hs.settle()
		if host == "refused.invalid" {
			hs.untilLong("the refusal", func() bool { return a.connErr != nil })
			hs.settle()
		}
		hs.shot("remote-window-" + strings.TrimSuffix(host, ".invalid"))
	}
}

// TestFolderBoxKeys: in the box of the folders, ~ types the home folder,
// .. goes up, and Tab goes into the only folder that matches.
func TestFolderBoxKeys(t *testing.T) {
	hs, c, dir := remoteHarness(t)
	dir = filepath.Clean(dir)
	a := hs.a
	a.openRemoteFolder()
	hs.untilLong("the box of the folders", func() bool { return a.remotePick.stage == pickFolder })
	r := &a.remotePick
	home := c.Info().Home
	r.query = ""
	hs.settle()
	hs.tt.Type("~")
	hs.settle()
	hs.tt.Type("x")
	hs.settle()
	if want := strings.TrimSuffix(home, "/") + "/x"; r.query != want {
		t.Fatalf("query = %q, want %q", r.query, want)
	}
	// Up from the folder of the test, then Tab back into it.
	r.query = dir + "/"
	hs.untilLong("the listing", func() bool { return r.dir == dir })
	rows := a.pickRows()
	if len(rows) < 2 || rows[1] != rowParent {
		t.Fatalf("rows = %q", rows)
	}
	a.pickFolderRow(1)
	if r.query != filepath.Dir(dir)+"/" {
		t.Fatalf("up: %q", r.query)
	}
	r.query = filepath.Dir(dir) + "/" + filepath.Base(dir)[:len(filepath.Base(dir))-1]
	hs.untilLong("the listing of the parent", func() bool { return r.dir == filepath.Dir(dir) })
	hs.tt.Key(0, ui.KeyTab)
	hs.settle()
	if r.query != dir+"/" {
		t.Fatalf("tab: %q, rows %q", r.query, a.pickRows())
	}
}
