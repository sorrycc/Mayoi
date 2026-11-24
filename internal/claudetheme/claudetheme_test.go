package claudetheme

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newStore(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	return Store{ConfigDir: filepath.Join(dir, "claude"), StatePath: filepath.Join(dir, "mayoi", "claude-theme.json")}
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func theme(accent string) func(string) Theme {
	return func(string) Theme {
		return Theme{Name: "Mayoi", Base: "dark", Overrides: map[string]string{"claude": accent}}
	}
}

// Turning the sync on writes the theme and chooses it, keeping the other
// settings as they were; an update rewrites the theme; turning it off
// chooses the theme before it again and removes Mayoi's file.
func TestApplyUpdateRestore(t *testing.T) {
	s := newStore(t)
	orig := "{\n  \"model\": \"opus\",\n  \"theme\": \"dark-daltonized\",\n  \"env\": {\"A\": \"1\"}\n}\n"
	write(t, s.settingsPath(), orig)
	var gotPrev string
	r, err := s.Apply(func(prev string) Theme { gotPrev = prev; return theme("#ff0000")(prev) })
	if err != nil || r.Previous != "dark-daltonized" || gotPrev != "dark-daltonized" || r.Lost {
		t.Fatalf("apply %+v %v, build saw %q", r, err, gotPrev)
	}
	want := "{\n  \"model\": \"opus\",\n  \"theme\": \"custom:mayoi\",\n  \"env\": {\"A\": \"1\"}\n}\n"
	if got := read(t, s.settingsPath()); got != want {
		t.Errorf("settings %q", got)
	}
	var th Theme
	if err := json.Unmarshal([]byte(read(t, s.themePath())), &th); err != nil || th.Overrides["claude"] != "#ff0000" || th.Base != "dark" {
		t.Errorf("theme %+v %v", th, err)
	}
	// An update, as the palette changes, and a restart, which reads the
	// state again.
	if _, err := s.Apply(theme("#00ff00")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, s.themePath()), "#00ff00") {
		t.Error("the theme was not updated")
	}
	if err := (Store{ConfigDir: s.ConfigDir, StatePath: s.StatePath}).Restore(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, s.settingsPath()); got != orig {
		t.Errorf("restored %q", got)
	}
	if exists(s.themePath()) || exists(s.StatePath) {
		t.Error("Mayoi's theme or state stayed")
	}
	// Once more, which does nothing.
	if err := s.Restore(); err != nil {
		t.Fatal(err)
	}
}

// A settings.json with no theme gets none back; one that is missing is
// made.
func TestNoPreviousTheme(t *testing.T) {
	s := newStore(t)
	if _, err := s.Apply(theme("#ff0000")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, s.settingsPath()); !strings.Contains(got, `"theme": "custom:mayoi"`) {
		t.Errorf("settings %q", got)
	}
	if err := s.Restore(); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(read(t, s.settingsPath())), &m); err != nil || len(m) != 0 {
		t.Errorf("restored %v %v", m, err)
	}

	write(t, s.settingsPath(), `{"model": "opus"}`)
	if _, err := s.Apply(theme("#ff0000")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, s.settingsPath()); got != `{"model": "opus", "theme": "custom:mayoi"}` {
		t.Errorf("settings %q", got)
	}
	if err := s.Restore(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, s.settingsPath()); got != `{"model": "opus"}` {
		t.Errorf("restored %q", got)
	}
}

// A theme chosen in Claude Code after Mayoi's stops the sync, and turning
// the sync off leaves it.
func TestManualChoiceWins(t *testing.T) {
	s := newStore(t)
	write(t, s.settingsPath(), `{"theme": "light"}`)
	if _, err := s.Apply(theme("#ff0000")); err != nil {
		t.Fatal(err)
	}
	write(t, s.settingsPath(), `{"theme": "dark-ansi"}`)
	r, err := s.Apply(theme("#00ff00"))
	if err != nil || !r.Lost {
		t.Fatalf("apply %+v %v", r, err)
	}
	if strings.Contains(read(t, s.themePath()), "#00ff00") || read(t, s.settingsPath()) != `{"theme": "dark-ansi"}` {
		t.Error("Mayoi wrote after a manual choice")
	}
	if r, _ := s.Apply(theme("#00ff00")); !r.Lost {
		t.Error("the sync came back by itself")
	}
	if err := s.Restore(); err != nil {
		t.Fatal(err)
	}
	if read(t, s.settingsPath()) != `{"theme": "dark-ansi"}` {
		t.Error("turning off undid the manual choice")
	}
	if exists(s.themePath()) {
		t.Error("Mayoi's unused theme stayed")
	}
	// Turning it on again takes over, and remembers the manual choice.
	if r, err := s.Apply(theme("#0000ff")); err != nil || r.Lost || r.Previous != "dark-ansi" {
		t.Fatalf("again %+v %v", r, err)
	}
}

// A theme file of Mayoi's name that Mayoi did not write, and a
// settings.json that is not JSON, stay as they are.
func TestLeavesWhatIsNotMayois(t *testing.T) {
	s := newStore(t)
	write(t, s.themePath(), `{"name": "Mine", "base": "light", "overrides": {}}`)
	_, err := s.Apply(theme("#ff0000"))
	var taken *TakenError
	if !errors.As(err, &taken) {
		t.Fatalf("err %v", err)
	}
	if exists(s.settingsPath()) || exists(s.StatePath) || !strings.Contains(read(t, s.themePath()), "Mine") {
		t.Error("Mayoi wrote over a theme of the person's")
	}

	s = newStore(t)
	write(t, s.settingsPath(), `{"theme": "dark",`)
	if _, err := s.Apply(theme("#ff0000")); !errors.Is(err, ErrMalformed) {
		t.Fatalf("err %v", err)
	}
	if read(t, s.settingsPath()) != `{"theme": "dark",` || exists(s.themePath()) {
		t.Error("Mayoi wrote over a malformed settings.json")
	}
	write(t, s.settingsPath(), `[]`)
	if _, err := s.Apply(theme("#ff0000")); !errors.Is(err, ErrMalformed) {
		t.Fatalf("an array: %v", err)
	}
}

// A change cut short, the theme written but not chosen, finishes the next
// time, still going back to the theme before.
func TestRecovers(t *testing.T) {
	s := newStore(t)
	write(t, s.settingsPath(), `{"theme": "light"}`)
	if _, err := s.Apply(theme("#ff0000")); err != nil {
		t.Fatal(err)
	}
	// As if the state were saved with the next theme, which was never
	// written, before a crash.
	var st state
	json.Unmarshal([]byte(read(t, s.StatePath)), &st)
	st.Next = "0000"
	data, _ := json.Marshal(st)
	write(t, s.StatePath, string(data))
	if _, err := s.Apply(theme("#00ff00")); err != nil {
		t.Fatal(err)
	}
	// Written but not chosen.
	write(t, s.settingsPath(), `{"theme": "light"}`)
	json.Unmarshal([]byte(read(t, s.StatePath)), &st)
	st.Selected = false
	data, _ = json.Marshal(st)
	write(t, s.StatePath, string(data))
	if r, err := s.Apply(theme("#00ff00")); err != nil || r.Lost || r.Previous != "light" {
		t.Fatalf("recover %+v %v", r, err)
	}
	if err := s.Restore(); err != nil {
		t.Fatal(err)
	}
	if read(t, s.settingsPath()) != `{"theme": "light"}` {
		t.Error(read(t, s.settingsPath()))
	}
	// A state file cut short holds nothing.
	write(t, s.StatePath, `{"configDir": `)
	if err := s.Restore(); err != nil {
		t.Fatal(err)
	}
}

// Rapid turning on and off leaves the settings as they were.
func TestRapidToggles(t *testing.T) {
	s := newStore(t)
	orig := `{"theme": "custom:mine", "x": 1}`
	write(t, s.settingsPath(), orig)
	for range 20 {
		if _, err := s.Apply(theme("#ff0000")); err != nil {
			t.Fatal(err)
		}
		if err := s.Restore(); err != nil {
			t.Fatal(err)
		}
	}
	if read(t, s.settingsPath()) != orig || exists(s.themePath()) {
		t.Error(read(t, s.settingsPath()))
	}
}

// A new config folder takes Mayoi's theme from the old one first.
func TestMovesFolder(t *testing.T) {
	s := newStore(t)
	write(t, s.settingsPath(), `{"theme": "light"}`)
	if _, err := s.Apply(theme("#ff0000")); err != nil {
		t.Fatal(err)
	}
	other := Store{ConfigDir: filepath.Join(filepath.Dir(s.ConfigDir), "other"), StatePath: s.StatePath}
	if _, err := other.Apply(theme("#ff0000")); err != nil {
		t.Fatal(err)
	}
	if read(t, s.settingsPath()) != `{"theme": "light"}` || exists(s.themePath()) || !exists(other.themePath()) {
		t.Error("the old folder kept Mayoi's theme")
	}
}

// A folder that cannot be written fails, leaving settings.json as it was.
func TestUnwritable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permissions")
	}
	s := newStore(t)
	write(t, s.settingsPath(), `{"theme": "light"}`)
	if err := os.Chmod(s.ConfigDir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(s.ConfigDir, 0o755)
	if _, err := s.Apply(theme("#ff0000")); err == nil {
		t.Fatal("no error")
	}
	if read(t, s.settingsPath()) != `{"theme": "light"}` {
		t.Error("settings changed")
	}
}

func TestSetTopKey(t *testing.T) {
	for _, c := range []struct{ in, val, want string }{
		{`{}`, `"x"`, "{\n  \"theme\": \"x\"\n}"},
		{`{"theme":"a"}`, ``, `{}`},
		{`{"a":1,"theme":"t","b":2}`, ``, `{"a":1,"b":2}`},
		{"{\n  \"theme\": \"t\",\n  \"b\": 2\n}", ``, "{\n  \"b\": 2\n}"},
		{`{"a":{"theme":"inner"}}`, `"x"`, `{"a":{"theme":"inner"}, "theme": "x"}`},
		{`{"a":1,"theme":"t"}`, `"u"`, `{"a":1,"theme":"u"}`},
	} {
		var val []byte
		if c.val != "" {
			val = []byte(c.val)
		}
		got, err := setTopKey([]byte(c.in), "theme", val)
		if err != nil || string(got) != c.want {
			t.Errorf("%s with %s: %q %v", c.in, c.val, got, err)
		}
		if !json.Valid(got) {
			t.Errorf("%s: invalid %q", c.in, got)
		}
	}
}
