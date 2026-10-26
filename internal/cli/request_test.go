package cli

import (
	"errors"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestParse(t *testing.T) {
	cwd := filepath.FromSlash("/w")
	abs := func(p string) string { return filepath.Join(cwd, p) }
	// A path absolute on this system: /x has no drive on Windows.
	x, b := "/x", "/b"
	if runtime.GOOS == "windows" {
		x, b = `C:\x`, `C:\b`
	}
	for _, c := range []struct {
		args []string
		want Request
	}{
		{nil, Request{}},
		{[]string{".", "a b.go", x}, Request{Paths: []Target{{Path: cwd}, {Path: abs("a b.go")}, {Path: x}}}},
		// Colons are part of a name without -g.
		{[]string{"a:3"}, Request{Paths: []Target{{Path: abs("a:3")}}}},
		{[]string{"-g", "a.go:3", "--goto=b:c.go:4:5", "-g", "d.go"}, Request{Paths: []Target{
			{Path: abs("a.go"), Line: 3}, {Path: abs("b:c.go"), Line: 4, Col: 5}, {Path: abs("d.go")},
		}}},
		{[]string{"-n", "-w", "msg"}, Request{NewWindow: true, Wait: true, Paths: []Target{{Path: abs("msg")}}}},
		{[]string{"-d", "a", b}, Request{Diff: []string{abs("a"), b}}},
		{[]string{"--remote", "box:/srv/app"}, Request{Remote: "box:/srv/app"}},
		{[]string{"--", "-n"}, Request{Paths: []Target{{Path: abs("-n")}}}},
		{[]string{"run", "view.terminal", "file.openFolder=..", "--window", "p"}, Request{Window: abs("p"), Run: []Command{
			{ID: "view.terminal"}, {ID: "file.openFolder", Arg: ".."},
		}}},
		{[]string{"commands"}, Request{List: true}},
		// A folder called run, not first.
		{[]string{"-n", "run"}, Request{NewWindow: true, Paths: []Target{{Path: abs("run")}}}},
	} {
		c.want.Cwd = cwd
		got, err := Parse(c.args, cwd)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("Parse(%q) = %+v, %v; want %+v", c.args, got, err, c.want)
		}
	}
	for _, args := range [][]string{
		{"-x"}, {"-g"}, {"-d", "a"}, {"run"}, {"run", "=x"}, {"commands", "x"}, {"run", "a", "-n"},
		{"-w"}, {"--remote", "box"}, {"--remote", "box:rel"}, {"-d", "a", "b", "c"},
	} {
		if _, err := Parse(args, cwd); err == nil {
			t.Errorf("Parse(%q) took it", args)
		}
	}
	if _, err := Parse([]string{"x", "--help"}, cwd); !errors.Is(err, ErrHelp) {
		t.Errorf("--help: %v", err)
	}
	if _, err := Parse([]string{"-v"}, cwd); !errors.Is(err, ErrVersion) {
		t.Errorf("-v: %v", err)
	}
}

func TestPlain(t *testing.T) {
	if !(Request{Paths: []Target{{Path: "/a"}}}).Plain() || !(Request{}).Plain() {
		t.Error("opening paths is plain")
	}
	for _, r := range []Request{
		{Paths: []Target{{Path: "/a", Line: 1}}}, {Wait: true}, {NewWindow: true}, {List: true},
		{Run: []Command{{ID: "x"}}}, {Diff: []string{"a", "b"}}, {Remote: "h:/"}, {Window: "/w"},
	} {
		if r.Plain() {
			t.Errorf("%+v is plain", r)
		}
	}
}
