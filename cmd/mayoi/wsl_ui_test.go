package main

import (
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/remote"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// TestWSLEntries is the WSL action: macOS and Linux do not list it, Windows
// lists it when wsl can be run, and mayoi commands does not list the bridge.
func TestWSLEntries(t *testing.T) {
	t.Cleanup(func() { hostOS = "darwin" })
	s := settings.Default()
	h := &fakeHost{}
	a := newApp(h, s, s.NewWindow())
	t.Cleanup(func() {
		a.shutdown()
		remote.Shutdown()
	})
	tt := ui.NewTester(a.view, 1200, 900)
	for range 8 {
		step(tt)
	}
	if strings.Contains(a.commandList(), "mayoi-bridge") {
		t.Fatal("mayoi commands lists the bridge")
	}
	if hasWSL(a) {
		t.Fatal("macOS lists a WSL action")
	}
	hostOS = "linux"
	if hasWSL(a) {
		t.Fatal("Linux lists a WSL action")
	}

	hostOS = "windows"
	t.Setenv("PATH", t.TempDir())
	if hasWSL(a) {
		t.Fatal("Windows lists a WSL action when wsl is missing")
	}
	if !slices.Contains(commandIDs(a), "remote.connect") {
		t.Fatal("Remote SSH is missing when wsl is missing")
	}
	for range 2 {
		step(tt)
	}
	if tt.HasText(locales.CmdWSLOpenFolder()) {
		t.Fatal("welcome shows WSL when wsl is missing")
	}

	installFakeWSL(t)
	hostOS = "linux"
	if hasWSL(a) {
		t.Fatal("Linux lists a WSL action when wsl is on PATH")
	}
	hostOS = "darwin"
	if hasWSL(a) {
		t.Fatal("macOS lists a WSL action when wsl is on PATH")
	}
	hostOS = "windows"
	if !hasWSL(a) {
		t.Fatal("Windows has no WSL action")
	}
	if zh := locales.In("zh-Hans").CmdWSLOpenFolder(); zh == "" || zh == locales.In("en").CmdWSLOpenFolder() {
		t.Fatalf("Chinese label %q", zh)
	}
	if ja := locales.In("ja").CmdWSLOpenFolder(); ja == "" || ja == locales.In("en").CmdWSLOpenFolder() {
		t.Fatalf("Japanese label %q", ja)
	}
	for range 4 {
		step(tt)
	}
	if !tt.HasText(locales.CmdWSLOpenFolder()) {
		t.Fatalf("welcome has no WSL action: %q", tt.Texts())
	}
	wslItem, ssh := wslMenu(a)
	if wslItem == nil {
		t.Fatal("File menu has no WSL action")
	}
	if ssh != nil && slices.ContainsFunc(ssh.Submenu, func(it *mygo.MenuItem) bool {
		return it != nil && it.Label == locales.CmdWSLOpenFolder()
	}) {
		t.Fatal("the WSL action is inside Remote SSH")
	}
	found := false
	for _, e := range a.paletteEntries() {
		if e.id == "wsl.openFolder" && strings.HasPrefix(e.title, locales.MenuWSL()+": ") {
			found = true
		}
	}
	if !found {
		t.Fatal("command palette has no wsl.openFolder")
	}
	if strings.Contains(a.commandList(), "mayoi-bridge") {
		t.Fatal("mayoi commands lists the bridge")
	}

	t.Setenv("MAYOI_FAKE_WSL_NAMES", "")
	a.openWSLPicker()
	waitText(t, tt, locales.WslNoDistro())
	if a.root != "" || len(h.windows) != 0 {
		t.Fatalf("no distribution opened %q windows %v", a.root, h.windows)
	}
	if joined := strings.Join(append(tt.Texts(), a.windowTitle()), "\n"); strings.Contains(joined, "SSH:") {
		t.Fatalf("no distribution mentions SSH: %q", joined)
	}
}

// TestWSLTerminal is a shell in the distribution, and no SSH port forward.
func TestWSLTerminal(t *testing.T) {
	c, dir, ok := remote.Lookup(remote.WSLPath("Ubuntu", "/home/me/app"))
	t.Cleanup(func() {
		remote.Shutdown()
	})
	if !ok || dir != "/home/me/app" || c.Kind() != remote.KindWSL {
		t.Fatalf("lookup ok=%v path=%q kind=%v", ok, dir, c.Kind())
	}
	args := c.TerminalCommand("/home/me/app")
	want := []string{"wsl.exe", "-d", "Ubuntu", "--cd", "/home/me/app"}
	if !slices.Equal(args, want) {
		t.Fatalf("terminal %q", args)
	}
	_, err := c.Forward(8080)
	if err == nil || len(c.Forwards()) != 0 {
		t.Fatal("a WSL folder forwarded a port")
	}
	if strings.Contains(err.Error(), "SSH:") {
		t.Fatalf("forward error %v", err)
	}

	s := settings.Default()
	a := newApp(&fakeHost{}, s, s.NewWindow())
	t.Cleanup(a.shutdown)
	c.Acquire()
	a.conn = c
	a.connUnsub = func() {}
	forward := forwardMenuItem(a)
	if forward == nil || !forward.Disabled {
		t.Fatal("Forward a Port is offered for a WSL folder")
	}

	ssh := &remote.Conn{Host: "box"}
	got := ssh.TerminalCommand("/tmp")
	if len(got) == 0 || got[0] != "ssh" {
		t.Fatalf("ssh terminal %q", got)
	}
	if runtime.GOOS == "windows" && strings.Contains(strings.Join(got, "\n"), "ControlMaster") {
		t.Fatalf("windows ssh terminal %q", got)
	}
}

func hasWSL(a *app) bool {
	return slices.Contains(commandIDs(a), "wsl.openFolder")
}

func commandIDs(a *app) []string {
	var ids []string
	for _, cmd := range a.commands() {
		ids = append(ids, cmd.id)
	}
	return ids
}

func wslMenu(a *app) (wsl, ssh *mygo.MenuItem) {
	for _, menu := range a.menu(false, func(string, *mygo.Window) {}).Items() {
		for _, sub := range menu.Submenu {
			if sub == nil {
				continue
			}
			if sub.Label == locales.CmdWSLOpenFolder() {
				wsl = sub
			}
			if sub.Label == locales.MenuRemoteSSH() {
				ssh = sub
			}
		}
	}
	return wsl, ssh
}

func forwardMenuItem(a *app) *mygo.MenuItem {
	_, ssh := wslMenu(a)
	if ssh == nil {
		return nil
	}
	for _, it := range ssh.Submenu {
		if it != nil && it.Label == locales.CmdRemoteForwardPort() {
			return it
		}
	}
	return nil
}
