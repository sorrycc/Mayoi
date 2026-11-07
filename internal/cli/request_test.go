package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestParse(t *testing.T) {
	home, _ := os.UserHomeDir()
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
		{[]string{"settings"}, Request{Settings: &Settings{}}},
		{[]string{"settings", "list"}, Request{Settings: &Settings{Verb: "list"}}},
		{[]string{"settings", "get", "fontSize", "theme"}, Request{Settings: &Settings{Verb: "get", Keys: []string{"fontSize", "theme"}}}},
		{[]string{"settings", "set", "fontSize=16", "fontFamily=A B", "theme="}, Request{Settings: &Settings{Verb: "set",
			Changes: []Change{{Op: OpSet, Key: "fontSize", Value: json.RawMessage("16")}, {Op: OpSet, Key: "fontFamily", Value: json.RawMessage(`"A B"`)},
				{Op: OpSet, Key: "theme", Value: json.RawMessage(`""`)}}}}},
		{[]string{"settings", "--folder", "set", "autoSave=false"}, Request{Settings: &Settings{Verb: "set", Folder: true,
			Changes: []Change{{Op: OpSet, Key: "autoSave", Value: json.RawMessage("false")}}}}},
		{[]string{"settings", "unset", "theme"}, Request{Settings: &Settings{Verb: "unset", Changes: []Change{
			{Op: OpUnset, Key: "theme"}}}}},
		// An item of a list, and a field of a look on the rail.
		{[]string{"settings", "set", "fontFeatures+=calt=0", "aiSecretFiles-=*.pem"}, Request{Settings: &Settings{Verb: "set",
			Changes: []Change{{Op: OpAdd, Key: "fontFeatures", Item: "calt=0"},
				{Op: OpRemove, Key: "aiSecretFiles", Item: "*.pem"}}}}},
		// The folder of a look is the app's, whole: x is absolute on each
		// system, as /src is not on Windows.
		{[]string{"settings", "set", `projectTabStyle.'` + x + `'.label=MG`}, Request{Settings: &Settings{Verb: "set",
			Changes: []Change{{Op: OpSet, Key: `projectTabStyle.'` + x + `'.label`, Value: json.RawMessage(`"MG"`)}}}}},
		// The folder of a look is the app's, whole, from cwd or ~.
		{[]string{"settings", "get", `projectTabStyle.'.'`, `projectTabStyle."~/my.app".label`, `projectTabStyle.'/ssh:box/srv'`}, Request{Settings: &Settings{Verb: "get",
			Keys: []string{`projectTabStyle.'` + cwd + `'`, `projectTabStyle.'` + filepath.Join(home, "my.app") + `'.label`, `projectTabStyle.'/ssh:box/srv'`}}}},
		// A folder called settings, not first.
		{[]string{"-n", "settings"}, Request{NewWindow: true, Paths: []Target{{Path: abs("settings")}}}},
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
		{"settings", "gets"}, {"settings", "get"}, {"settings", "set"}, {"settings", "set", "fontSize"},
		{"settings", "list", "theme"}, {"settings", "get", "theme", "-n"}, {"settings", "--window", "p"},
		{"-f", "x"}, {"settings", "-g", "a.go"}, {"settings", "unset"}, {"settings", "list", "a=b"},
		{"settings", "--add", "a=b"}, {"--add", "a=b"}, {"settings", "set", "fontFeatures+="}, {"settings", "set", "+=x"},
		{"settings", "set", "-=x"},
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
		{Settings: &Settings{}},
	} {
		if r.Plain() {
			t.Errorf("%+v is plain", r)
		}
	}
}
