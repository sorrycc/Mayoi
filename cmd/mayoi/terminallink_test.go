package main

import (
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/remote"
)

func TestTerminalLinkOpensFile(t *testing.T) {
	hs := newHarness(t, true)
	const rel = "docs/designs/2026-10-22-codex-code-mode.md"
	if err := os.MkdirAll(filepath.Dir(hs.path(rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hs.path(rel), []byte("# Design\n\nDetails\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !hs.a.terminalLink(&session{dir: hs.root}, rel) {
		t.Fatal("terminal file link was not handled")
	}
	if d := hs.a.activeEditor(); d == nil || d.path != hs.path(rel) {
		t.Fatalf("terminal file link did not open its file: %v", d)
	}
	for _, tc := range []struct {
		name, dir, link string
		line, col       int
	}{
		{"dot relative", hs.root, "./" + rel, 1, 1},
		{"parent relative", hs.path("docs"), "../" + rel, 1, 1},
		{"absolute", t.TempDir(), hs.path(rel), 1, 1},
		{"line", hs.root, rel + ":3", 3, 1},
		{"line and column", hs.root, rel + ":3:2", 3, 2},
		{"column zero", hs.root, rel + ":3:0", 3, 1},
		{"clamped position", hs.root, rel + ":999:999", 4, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !hs.a.terminalLink(&session{dir: tc.dir}, tc.link) {
				t.Fatalf("did not handle %q", tc.link)
			}
			step(hs.tt)
			d := hs.a.activeEditor()
			if d == nil || d.path != hs.path(rel) {
				t.Fatalf("opened %v", d)
			}
			snap := d.buf.Snapshot()
			at := d.sel[1]
			line := snap.LineAt(at)
			start, _ := snap.LineRange(line)
			if line+1 != tc.line || at-start+1 != tc.col {
				t.Errorf("caret at %d:%d, want %d:%d", line+1, at-start+1, tc.line, tc.col)
			}
		})
	}
	if home, err := filepath.Rel(homeDir(), hs.path(rel)); err == nil {
		if !hs.a.terminalLink(&session{dir: hs.root}, "~/"+filepath.ToSlash(home)) {
			t.Fatal("home-relative file link was not handled")
		}
	}
}

// Exercise the terminal's actual modifier-click path, including a shell
// directory different from the one the terminal started in.
func TestTerminalCommandClickOpensFile(t *testing.T) {
	testTerminalCommandClickFile(t, "internal/a/a.go")
}

func testTerminalCommandClickFile(t *testing.T, rel string) {
	t.Helper()
	if testing.Short() && os.Getenv("MYGO_GHOSTTY_VT") == "" {
		t.Skip("loads libghostty-vt")
	}
	hs := newHarness(t, true)
	if err := os.WriteFile(hs.path(rel), []byte("# Notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &session{dir: t.TempDir()}
	conn, peer := net.Pipe()
	t.Cleanup(func() { peer.Close() })
	term, err := terminal.New(terminal.Options{
		Conn: conn,
		OpenLink: func(link string) bool {
			return hs.a.terminalLink(s, link)
		},
	})
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Close() })
	s.term = term
	tt := ui.NewTester(func(c *ui.Context) { terminal.View(c, term).Fill() }, 800, 200)
	tt.Frame()
	cwd := (&url.URL{Scheme: "file", Host: "localhost", Path: filepath.ToSlash(hs.root)}).String()
	term.Feed([]byte("\x1b]7;" + cwd + "\x07write(" + rel + ")\r\n"))
	tt.Frame()
	cols, rows := term.Size()
	x, y := 7.5*800/float32(cols), 0.5*200/float32(rows)
	tt.ClickAt(x, y)
	if hs.a.activeEditor() != nil {
		t.Fatal("an unmodified click opened a file instead of selecting terminal text")
	}
	tt.Move(x, y)
	tt.HoldModifiers(ui.Cmd)
	if link, path, at := term.HoveredLink(); link != rel || !path || !at.Contains(x, y) {
		t.Fatalf("file path did not become an underlined link on hover: %q, %v, %+v", link, path, at)
	}
	tt.ClickAtWith(ui.Cmd, x, y)
	if d := hs.a.activeEditor(); d == nil || d.path != hs.path(rel) {
		t.Fatalf("modifier-click did not open the file in the shell's directory: %v", d)
	}
}

func TestTerminalLinkSkipsLockedColumn(t *testing.T) {
	hs, left, right := lockedHarness(t)
	hs.a.col = 1
	if !hs.a.terminalLink(&session{dir: hs.root}, "README.md") {
		t.Fatal("file link was not handled")
	}
	if hs.a.column() != left || left.tab().path() != hs.path("README.md") || len(right.tabs) != 0 {
		t.Fatal("terminal file link did not use the file column")
	}
}

func TestTerminalLinkOpensIgnoredFile(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	const rel = ".log-outputs/analysis.md"
	if err := os.WriteFile(hs.path(".gitignore"), []byte("/.log-outputs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(hs.path(rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hs.path(rel), []byte("# Analysis\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("check-ignore", rel)
	if !hs.a.terminalLink(&session{dir: hs.root}, rel) {
		t.Fatal("ignored file link was not handled")
	}
	if d := hs.a.activeEditor(); d == nil || d.path != hs.path(rel) {
		t.Fatalf("ignored file was not opened: %v", d)
	}
}

func TestTerminalLinkLeavesOtherLinks(t *testing.T) {
	hs := newHarness(t, true)
	for _, link := range []string{
		"https://example.com/docs/design.md", "http://localhost:3000/",
		"mailto:me@example.com", "file:///tmp/example.md",
		"docs/missing.md", "README.md:0", "README.md:no-line", "internal/a",
	} {
		if hs.a.terminalLink(&session{dir: hs.root}, link) {
			t.Errorf("unexpectedly handled %q as a file", link)
		}
	}
	if hs.a.terminalLink(&session{dir: remote.Path("offline-test", hs.root)}, hs.path("README.md")) {
		t.Error("a remote terminal opened a path on this machine")
	}
	if len(hs.a.docs) != 0 {
		t.Fatal("opened a document for a rejected link")
	}
}
