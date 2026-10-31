package main

import (
	"path/filepath"
	"strings"
	"testing"

	"mayoi/internal/cli"
)

// A command acts on the project whose folder holds the directory it ran
// in, else the one in front, or that of --window.
func TestCommandTarget(t *testing.T) {
	root, other := workspaceFixture(t), workspaceFixture(t)
	_, hs, _ := railFixture(t, root, other)
	m := hs[0].m
	for _, c := range []struct {
		req  cli.Request
		want *desktop
	}{
		{cli.Request{Cwd: other}, hs[1]},
		{cli.Request{Cwd: filepath.Join(other, "internal", "a")}, hs[1]},
		{cli.Request{Cwd: t.TempDir()}, hs[0]},
		{cli.Request{Cwd: other, Window: root}, hs[0]},
	} {
		if got, err := m.target(c.req); err != nil || got != c.want {
			t.Errorf("%+v: got %v, %v", c.req, got, err)
		}
	}
	if _, err := m.target(cli.Request{Window: t.TempDir()}); err == nil {
		t.Error("a --window of no project")
	}
}

// mayoi run checks every command before running any, and takes the
// arguments of those that ask for one.
func TestCLICommands(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	for _, want := range [][]cli.Command{
		{{ID: "view.zoomIn"}, {ID: "no.such"}},
		{{ID: "view.zoomIn", Arg: "x"}},
		{{ID: "go.line", Arg: "x"}},
		{{ID: "file.openFolder", Arg: "nowhere"}},
		{{ID: "view.selectTheme", Arg: "nope"}},
		{{ID: "file.saveAs", Arg: "x.go"}},
	} {
		if _, err := a.cliCommands(want, hs.root); err == nil {
			t.Errorf("%+v ran", want)
		}
	}
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	runs, err := a.cliCommands([]cli.Command{{ID: "go.line", Arg: "6:2"}, {ID: "view.selectTheme", Arg: "Claude"}}, hs.root)
	if err != nil || len(runs) != 2 {
		t.Fatalf("%d runs: %v", len(runs), err)
	}
	for _, run := range runs {
		run()
	}
	hs.settle()
	d := a.textEditor()
	if s, _ := d.buf.Snapshot().LineRange(5); d.sel[1] != s+1 {
		t.Errorf("caret at %d, not %d", d.sel[1], s+1)
	}
	if a.settings.Theme != themeClaude {
		t.Errorf("theme %q", a.settings.Theme)
	}
	list := a.commandList()
	if !strings.Contains(list, "file.openFolder\t") || !strings.Contains(list, "\t<folder>\n") {
		t.Errorf("commands:\n%s", list)
	}
}

// mayoi --wait answers once the file closes.
func TestWaitClosed(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	done := 0
	a.waitClosed(func() { done++ })
	a.openFile(hs.path("go.mod"), true)
	hs.settle()
	a.waitClosed(func() { done++ })
	a.closeActive()
	hs.settle()
	if done != 1 {
		t.Fatalf("%d done after closing one", done)
	}
	a.shutdown()
	if done != 2 {
		t.Errorf("%d done as the project closes", done)
	}
}
