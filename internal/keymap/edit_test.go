package keymap

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestStrokeString(t *testing.T) {
	for _, tc := range []struct {
		s    Stroke
		want string
	}{
		{Stroke{ui.Super | ui.Shift, ui.KeyP}, "shift+cmd+p"},
		{Stroke{ui.Ctrl, ui.KeyBackquote}, "ctrl+`"},
		{Stroke{0, ui.KeyF5}, "f5"},
		{Stroke{ui.Ctrl | ui.Alt, ui.KeyComma}, "ctrl+alt+,"},
	} {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.s, got, tc.want)
		}
		if back, err := ParseStroke(tc.want); err != nil || back != tc.s {
			t.Errorf("%q reads back as %v, %v", tc.want, back, err)
		}
	}
	keys, _ := ParseKeys("ctrl+k ctrl+s")
	if got := KeysString(keys); got != "ctrl+k ctrl+s" {
		t.Errorf("chord %q", got)
	}
}

func TestEdit(t *testing.T) {
	file := `// My shortcuts.
[
  // Indent.
  { "key": "ctrl+. s", "command": "editor.action.indentUsingSpaces" },
  {
    "key": "cmd+1",
    "command": "view.sidebar"
  }, // the sidebar
  { "key": "d", "command": "deleteFile", "when": "filesExplorerFocus" }
]
`
	byCommand := func(name string) func(Entry) bool {
		return func(e Entry) bool { return e.Command == name }
	}
	for _, tc := range []struct {
		name string
		data string
		drop func(Entry) bool
		add  []Entry
		want string
	}{
		{"add", file, nil, []Entry{{Key: "cmd+k", Command: "git.push"}}, `// My shortcuts.
[
  // Indent.
  { "key": "ctrl+. s", "command": "editor.action.indentUsingSpaces" },
  {
    "key": "cmd+1",
    "command": "view.sidebar"
  }, // the sidebar
  { "key": "d", "command": "deleteFile", "when": "filesExplorerFocus" },
  { "key": "cmd+k", "command": "git.push" }
]
`},
		{"drop a line", file, byCommand("editor.action.indentUsingSpaces"), nil, `// My shortcuts.
[
  // Indent.
  {
    "key": "cmd+1",
    "command": "view.sidebar"
  }, // the sidebar
  { "key": "d", "command": "deleteFile", "when": "filesExplorerFocus" }
]
`},
		{"drop many lines", file, byCommand("view.sidebar"), nil, `// My shortcuts.
[
  // Indent.
  { "key": "ctrl+. s", "command": "editor.action.indentUsingSpaces" },
  { "key": "d", "command": "deleteFile", "when": "filesExplorerFocus" }
]
`},
		{"drop the last and add", file, byCommand("deleteFile"), []Entry{{Key: "cmd+d", Command: "-deleteFile", When: "a && b"}}, `// My shortcuts.
[
  // Indent.
  { "key": "ctrl+. s", "command": "editor.action.indentUsingSpaces" },
  {
    "key": "cmd+1",
    "command": "view.sidebar"
  }, // the sidebar
  { "key": "cmd+d", "command": "-deleteFile", "when": "a && b" }
]
`},
		{"empty file", "", nil, []Entry{{Key: "f5", Command: "git.refresh"}, {Key: "f6", Command: "git.fetch", Args: []byte(`{ "a": 1 }`)}},
			Template[:strings.Index(Template, "[")+1] + `
  { "key": "f5", "command": "git.refresh" },
  { "key": "f6", "command": "git.fetch", "args": {"a":1} }
]
`},
		{"one line", `[{"key":"a","command":"x"},{"key":"b","command":"y"}]`, byCommand("x"), []Entry{{Key: "c", Command: "z"}},
			`[{"key":"b","command":"y"},
  { "key": "c", "command": "z" }]`},
		{"empty list", "[]", nil, []Entry{{Key: "c", Command: "z"}}, `[
  { "key": "c", "command": "z" }
]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Edit([]byte(tc.data), tc.drop, tc.add)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("got\n%s\nwant\n%s", got, tc.want)
			}
			if _, err := Parse(got); err != nil {
				t.Errorf("does not parse: %v", err)
			}
		})
	}
	if _, err := Edit([]byte(`{"key": 1}`), nil, nil); err == nil {
		t.Error("edited what is not a list")
	}
}
