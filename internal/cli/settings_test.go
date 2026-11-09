package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mayoi/internal/settings"
)

// prefs lines are what mayoi settings prints: a key, a tab, a value of
// JSON.
func prefsLines(t *testing.T, out string) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("%q is no line of a key and a value", line)
		}
		m[key] = val
	}
	return m
}

// putFile puts data in the file at path, and makes its folder.
func putFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	putFile(t, path, `{"fontSize": 16, "theme": "claude"}`)
	out, err := Settings{}.Do(path)
	if err != nil {
		t.Fatal(err)
	}
	got := prefsLines(t, out)
	if len(got) != len(settings.Schema) {
		t.Errorf("%d lines, want one for each of %d preferences", len(got), len(settings.Schema))
	}
	for key, want := range map[string]string{"fontSize": "16", "theme": `"claude"`, "vim": "false", "fontFamily": `"monospace"`} {
		if got[key] != want {
			t.Errorf("%s is %s, want %s", key, got[key], want)
		}
	}
	// The first line is of the first preference the settings window shows.
	if first := strings.SplitN(out, "\t", 2)[0]; first != "theme" {
		t.Errorf("the first line is %s", first)
	}
}

// A file that is not there lists the preferences of a first launch.
func TestSettingsListNoFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	out, err := Settings{Verb: "list"}.Do(path)
	if err != nil {
		t.Fatal(err)
	}
	got := prefsLines(t, out)
	if got["fontSize"] != "13" || got["autoSave"] != "true" || got["theme"] != `""` {
		t.Errorf("got %v", got)
	}
}

func TestSettingsGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	// A value the file holds to the wrong type is the default, as Mayoi
	// has it, and a value out of its bounds is in them.
	putFile(t, path, `{"fontSize": 100, "theme": "claude", "vim": "yes"}`)
	out, err := Settings{Verb: "get", Keys: []string{"theme", "fontSize", "vim"}}.Do(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "\"claude\"\n28\nfalse\n"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
	if _, err := (Settings{Verb: "get", Keys: []string{"theme", "nope"}}).Do(path); err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("get of a key Mayoi has none of: %v", err)
	}
}

func TestSettingsSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	putFile(t, path, `{"theme": "claude", "mine": 1}`)
	s := Settings{Verb: "set", Changes: []Change{
		{Op: OpSet, Key: "fontSize", Value: json.RawMessage("16")},
		{Op: OpSet, Key: "vim", Value: json.RawMessage("true")},
		{Op: OpSet, Key: "fontFamily", Value: json.RawMessage(`"Fira Code"`)},
		{Op: OpSet, Key: "fontFeatures", Value: json.RawMessage(`["calt=0"]`)},
		{Op: OpSet, Key: "appearance", Value: json.RawMessage(`""`)},
		// A text that reads as JSON is the text.
		{Op: OpSet, Key: "aiModel", Value: prefValue("123")},
	}}
	if _, err := s.Do(path); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"theme": "claude", "mine": float64(1), "fontSize": float64(16), "vim": true,
		"fontFamily": "Fira Code", "fontFeatures": []any{"calt=0"}, "appearance": "",
		"aiModel": "123",
	}
	if len(got) != len(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if g, ok := got[k]; !ok || !jsonEqual(g, v) {
			t.Errorf("%s is %v, want %v", k, g, v)
		}
	}
}

// jsonEqual reports whether two values of JSON are the same.
func jsonEqual(x, y any) bool {
	a, _ := json.Marshal(x)
	b, _ := json.Marshal(y)
	return bytes.Equal(a, b)
}

// A value that is no value of its preference changes nothing in the file.
func TestSettingsSetWrong(t *testing.T) {
	for _, c := range []struct{ key, value, want string }{
		{"nope", "1", `no preference "nope"`},
		{"fontSize", `"big"`, "a number from 9 to 28"},
		{"fontSize", "100", "a number from 9 to 28"},
		{"vim", "1", "true or false"},
		{"theme", "nope", `"", "matrix", "claude", "qoder", "grokbot", "cursor", "purplegold", "pumpkin", "witchsbrew" or "bloodmoon"`},
		{"fontFeatures", "calt=0", `a list of text`},
		{"projectTabStyle", "[]", "the looks of the projects on the rail"},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		data := `{"theme": "claude"}`
		putFile(t, path, data)
		s := Settings{Verb: "set", Changes: []Change{{Op: OpSet, Key: c.key, Value: json.RawMessage(c.value)}}}
		if _, err := s.Do(path); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s=%s: %v, want %q", c.key, c.value, err, c.want)
		}
		// Nothing of it is written.
		if got, _ := os.ReadFile(path); string(got) != data {
			t.Errorf("%s=%s wrote %s", c.key, c.value, got)
		}
	}
}

// A value is JSON where it is, else the text set gives it.
func TestPrefValue(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"16", "16"}, {"true", "true"}, {`["a"]`, `["a"]`}, {`""`, `""`}, {"claude", `"claude"`},
		{`Fira Code`, `"Fira Code"`}, {"", `""`}, {"calt=0", `"calt=0"`},
	} {
		if got := string(prefValue(c.in)); got != c.want {
			t.Errorf("prefValue(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

// A change that changes nothing leaves the file as it is.
func TestSettingsNoChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	data := `{
  "vim": true
}`
	putFile(t, path, data)
	if _, err := (Settings{Verb: "set", Changes: []Change{{Op: OpSet, Key: "vim", Value: json.RawMessage("true")}}}).Do(path); err != nil {
		t.Fatal(err)
	}
	if _, err := (Settings{Verb: "unset", Changes: []Change{{Op: OpUnset, Key: "theme"}}}).Do(path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != data {
		t.Errorf("the file is %s", got)
	}
	if _, err := (Settings{Verb: "nope"}).Do(path); err == nil {
		t.Error("a verb Mayoi has none of took the file")
	}
}

func TestSettingsUnset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	putFile(t, path, `{"theme": "claude", "fontSize": 16}`)
	if _, err := (Settings{Verb: "unset", Changes: []Change{{Op: OpUnset, Key: "theme"}}}).Do(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["theme"]; ok || !jsonEqual(got["fontSize"], 16) {
		t.Errorf("got %s", data)
	}
	if _, err := (Settings{Verb: "unset", Changes: []Change{{Op: OpUnset, Key: "nope"}}}).Do(path); err == nil {
		t.Error("unset of a key Mayoi has none of took it")
	}
}

// A file that does not parse is left as it is, and says so.
func TestSettingsBadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	putFile(t, path, "{oops")
	s := Settings{Verb: "set", Changes: []Change{{Op: OpSet, Key: "vim", Value: json.RawMessage("true")}}}
	if _, err := s.Do(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("set on a file that does not parse: %v", err)
	}
	if out, err := (Settings{}).Do(path); err == nil || out != "" {
		t.Errorf("list on it: %q, %v", out, err)
	}
	if got, _ := os.ReadFile(path); string(got) != "{oops" {
		t.Errorf("the file is %s", got)
	}
	// A file of JSON that is no object says so, not what Go reads it into.
	putFile(t, path, "[1]")
	if _, err := (Settings{}).Do(path); err == nil || !strings.Contains(err.Error(), "no object of preferences") {
		t.Errorf("list on a list: %v", err)
	}
}

// --folder in a project's subfolder acts on the project's file, the one
// the app reads, rather than making one of the subfolder's.
func TestFolderFile(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "src", "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, want := folderFile(sub), filepath.Join(sub, ".mayoi", "settings.json"); got != want {
		t.Errorf("with no file: %s, want %s", got, want)
	}
	path := filepath.Join(dir, ".mayoi", "settings.json")
	putFile(t, path, "{}")
	if got := folderFile(sub); got != path {
		t.Errorf("in a subfolder: %s, want %s", got, path)
	}
	if got := folderFile(dir); got != path {
		t.Errorf("in the folder: %s, want %s", got, path)
	}
}

// A folder's file holds the preferences it chooses, of those a folder may
// choose, and those others in it are left as they are.
func TestSettingsFolder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mayoi", "settings.json")
	putFile(t, path, `{"vim": true, "mine": "kept"}`)
	if out, err := (Settings{Folder: true}).Do(path); err != nil {
		t.Fatal(err)
	} else if got := prefsLines(t, out); len(got) != 1 || got["vim"] != "true" {
		t.Errorf("list: %q", out)
	}
	// A preference no folder chooses is the person's alone.
	if _, err := (Settings{Folder: true, Verb: "set", Changes: []Change{{Op: OpSet, Key: "theme", Value: json.RawMessage(`"claude"`)}}}).Do(path); err == nil {
		t.Error("a folder took theme")
	}
	s := Settings{Folder: true, Verb: "set", Changes: []Change{{Op: OpSet, Key: "autoSave", Value: json.RawMessage("false")}}}
	if _, err := s.Do(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(got["autoSave"], false) || !jsonEqual(got["vim"], true) || !jsonEqual(got["mine"], "kept") {
		t.Errorf("got %s", data)
	}
	// A preference the folder does not choose prints null, so each line
	// is still its key's.
	if out, err := (Settings{Folder: true, Verb: "get", Keys: []string{"nope", "vim"}}).Do(path); err == nil {
		t.Errorf("get of a key Mayoi has none of: %q", out)
	}
	if out, err := (Settings{Folder: true, Verb: "get", Keys: []string{"wordWrap", "vim"}}).Do(path); err != nil || out != "null\ntrue\n" {
		t.Errorf("get of one the folder does not choose: %q, %v", out, err)
	}
	if _, err := (Settings{Folder: true, Verb: "unset", Changes: []Change{{Op: OpUnset, Key: "vim"}}}).Do(path); err != nil {
		t.Fatal(err)
	}
	if out, err := (Settings{Folder: true}).Do(path); err != nil {
		t.Fatal(err)
	} else if got := prefsLines(t, out); len(got) != 1 || got["autoSave"] != "false" {
		t.Errorf("after unset: %q", out)
	}
	// A folder without a file chooses nothing, and its file is made.
	other := filepath.Join(dir, "other")
	if out, err := (Settings{Folder: true}).Do(filepath.Join(other, ".mayoi", "settings.json")); err != nil || out != "" {
		t.Errorf("list of a folder without a file: %q, %v", out, err)
	}
	path = filepath.Join(other, ".mayoi", "settings.json")
	if _, err := (Settings{Folder: true, Verb: "set", Changes: []Change{{Op: OpSet, Key: "vim", Value: json.RawMessage("true")}}}).Do(path); err != nil {
		t.Fatal(err)
	}
	if out, err := (Settings{Folder: true}).Do(path); err != nil || !strings.Contains(out, "vim\ttrue") {
		t.Errorf("after set: %q, %v", out, err)
	}
}

// The command of the preferences reads and writes the file it is given.
func TestClientSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	c := Client{SettingsPath: path}
	var out, errs bytes.Buffer
	if status, handled := c.Run([]string{"settings", "set", "fontSize=16", "theme=claude"}, dir, &out, &errs); status != 0 || !handled {
		t.Fatalf("set: %d, %v, %s", status, handled, errs.String())
	}
	if out.Len() != 0 || errs.Len() != 0 {
		t.Errorf("set printed %q, %q", out.String(), errs.String())
	}
	out.Reset()
	if status, _ := c.Run([]string{"settings", "get", "fontSize", "theme"}, dir, &out, &errs); status != 0 {
		t.Fatalf("get: %d, %s", status, errs.String())
	}
	if want := "16\n\"claude\"\n"; out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
	out.Reset()
	if status, _ := c.Run([]string{"settings", "--folder", "set", "autoSave=false"}, dir, &out, &errs); status != 0 {
		t.Fatalf("folder set: %d, %s", status, errs.String())
	}
	if got, err := os.ReadFile(filepath.Join(dir, ".mayoi", "settings.json")); err != nil || !strings.Contains(string(got), `"autoSave": false`) {
		t.Errorf("the folder's file: %s, %v", got, err)
	}
	// The status 1 of a preference that fails, and its words.
	errs.Reset()
	if status, _ := c.Run([]string{"settings", "set", "nope=1"}, dir, &out, &errs); status != 1 {
		t.Errorf("a wrong preference: %d", status)
	}
	if !strings.HasPrefix(errs.String(), "mayoi: no preference \"nope\"") {
		t.Errorf("said %q", errs.String())
	}
}

// A list takes an item at a time, which may be there already, and gives
// it back, as set fontFeatures+=calt=0 does.
func TestSettingsListItems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	putFile(t, path, `{"fontFeatures": ["liga=0"], "aiSecretFiles": null}`)
	do := func(c ...Change) {
		t.Helper()
		if _, err := (Settings{Verb: "set", Changes: c}).Do(path); err != nil {
			t.Fatal(err)
		}
	}
	do(Change{Op: OpAdd, Key: "fontFeatures", Item: "calt=0"})
	// An item a list has is no item more of it.
	do(Change{Op: OpAdd, Key: "fontFeatures", Item: "calt=0"})
	do(Change{Op: OpAdd, Key: "aiSecretFiles", Item: "*.pem"})
	out, err := (Settings{Verb: "get", Keys: []string{"fontFeatures", "aiSecretFiles"}}).Do(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[\"liga=0\",\"calt=0\"]\n[\"*.pem\"]\n"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
	do(Change{Op: OpRemove, Key: "fontFeatures", Item: "liga=0"})
	// The last item of a list leaves no list behind.
	do(Change{Op: OpRemove, Key: "aiSecretFiles", Item: "*.pem"})
	out, err = (Settings{Verb: "get", Keys: []string{"fontFeatures", "aiSecretFiles"}}).Do(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[\"calt=0\"]\nnull\n"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
	// A preference that is no list takes no item.
	for _, c := range []Change{
		{Op: OpAdd, Key: "vim", Value: json.RawMessage("x")},
		{Op: OpAdd, Key: "nope", Value: json.RawMessage("x")},
		{Op: OpRemove, Key: "fontSize", Value: json.RawMessage("x")},
	} {
		if _, err := (Settings{Verb: "set", Changes: []Change{c}}).Do(path); err == nil {
			t.Errorf("%s took an item", c.Key)
		}
	}
}

// A look of a project on the rail is told a field at a time, and the whole
// of it at once.
func TestSettingsStyles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	putFile(t, path, `{"theme": "claude"}`)
	do := func(verb string, c ...Change) string {
		t.Helper()
		out, err := (Settings{Verb: verb, Changes: c}).Do(path)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	do("set", Change{Op: OpSet, Key: `projectTabStyle.'/src/my.app'.label`, Value: json.RawMessage(`"MG"`)})
	do("set", Change{Op: OpSet, Key: `projectTabStyle.'/src/my.app'.color`, Value: json.RawMessage(`"cyan"`)})
	get := func(key string) string {
		t.Helper()
		out, err := (Settings{Verb: "get", Keys: []string{key}}).Do(path)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got, want := get(`projectTabStyle.'/src/my.app'`), "{\"label\":\"MG\",\"color\":\"cyan\"}\n"; got != want {
		t.Errorf("the look: %q, want %q", got, want)
	}
	if got, want := get(`projectTabStyle.'/src/my.app'.label`), "\"MG\"\n"; got != want {
		t.Errorf("its label: %q, want %q", got, want)
	}
	// An icon is no field of a look the command knows.
	if out, err := (Settings{Verb: "get", Keys: []string{`projectTabStyle.'/src/my.app'.icon`}}).Do(path); err == nil {
		t.Errorf("reading an icon: %q", out)
	}
	// A field left empty takes the project's own, which is no look.
	do("set", Change{Op: OpSet, Key: `projectTabStyle.'/src/my.app'.color`, Value: json.RawMessage(`""`)})
	if got, want := get("projectTabStyle"), "{\"/src/my.app\":{\"label\":\"MG\"}}\n"; got != want {
		t.Errorf("after the color goes: %q, want %q", got, want)
	}
	// Unset takes the field asked for, and leaves the other fields.
	do("set", Change{Op: OpSet, Key: `projectTabStyle.'/src/my.app'.color`, Value: json.RawMessage(`"cyan"`)})
	do("unset", Change{Op: OpUnset, Key: `projectTabStyle.'/src/my.app'.color`})
	if got, want := get(`projectTabStyle.'/src/my.app'`), "{\"label\":\"MG\"}\n"; got != want {
		t.Errorf("after unset of the color: %q, want %q", got, want)
	}
	// The last field of a look takes the whole look away.
	do("unset", Change{Op: OpUnset, Key: `projectTabStyle.'/src/my.app'.label`})
	if got, want := get("projectTabStyle"), "null\n"; got != want {
		t.Errorf("after the label goes: %q, want %q", got, want)
	}
	// The whole of the look, and its folders with dots of their own.
	do("set", Change{Op: OpSet, Key: "projectTabStyle", Value: json.RawMessage(`{"/a": {"label": "A", "color": "red"}}`)})
	do("set", Change{Op: OpSet, Key: `projectTabStyle.'/src/my.app'`, Value: json.RawMessage(`{"label": "MG"}`)})
	out := get("projectTabStyle")
	if !strings.Contains(out, `"/a"`) || !strings.Contains(out, `"/src/my.app"`) || !strings.Contains(out, `"MG"`) {
		t.Errorf("the looks: %q", out)
	}
	// A label that reads as JSON is its text.
	do("set", Change{Op: OpSet, Key: `projectTabStyle.'/a'.label`, Value: prefValue("12")})
	if got, want := get(`projectTabStyle.'/a'.label`), "\"12\"\n"; got != want {
		t.Errorf("a label of digits: %q, want %q", got, want)
	}
	// What a look does not take.
	for _, c := range []Change{
		{Op: OpSet, Key: `projectTabStyle.'/a'.label`, Value: json.RawMessage(`"MGG"`)},
		{Op: OpSet, Key: `projectTabStyle.'/a'.color`, Value: json.RawMessage(`"pink"`)},
		{Op: OpSet, Key: `projectTabStyle.'/a'.nope`, Value: json.RawMessage(`"x"`)},
		{Op: OpSet, Key: `projectTabStyle.'/a'.label.x`, Value: json.RawMessage(`"x"`)},
		{Op: OpSet, Key: "projectTabStyle", Value: json.RawMessage(`{"/a": {"color": "pink"}}`)},
		{Op: OpSet, Key: "fontSize.label", Value: json.RawMessage(`"x"`)},
		{Op: OpSet, Key: `projectTabStyle.'/a`, Value: json.RawMessage(`"x"`)},
		{Op: OpSet, Key: `projectTabStyle.'/a.label`, Value: json.RawMessage(`"x"`)},
	} {
		if _, err := (Settings{Verb: "set", Changes: []Change{c}}).Do(path); err == nil {
			t.Errorf("%s=%s took it", c.Key, c.Value)
		}
	}
}

// A label counts the characters a person sees, as the rail does, and an
// icon is no part of a look the command sets or reads.
func TestSettingsLookRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	do := func(c Change) error {
		t.Helper()
		_, err := (Settings{Verb: "set", Changes: []Change{c}}).Do(path)
		return err
	}
	// A family emoji is one character, as the rail's own prompt counts it.
	if err := do(Change{Op: OpSet, Key: `projectTabStyle.'/a'.label`, Value: mustJSON("👨‍👩‍👧")}); err != nil {
		t.Errorf("a family emoji as a label: %v", err)
	}
	// The emoji and the x are the two characters, so it goes.
	if err := do(Change{Op: OpSet, Key: `projectTabStyle.'/a'.label`, Value: mustJSON("👨‍👩‍👧x")}); err != nil {
		t.Errorf("two characters as a label: %v", err)
	}
	if err := do(Change{Op: OpSet, Key: `projectTabStyle.'/a'.label`, Value: mustJSON("🚀xy")}); err == nil {
		t.Error("three characters took a label")
	}
	// An icon is a name the app made, which the command leaves alone.
	if err := do(Change{Op: OpSet, Key: `projectTabStyle.'/a'.icon`, Value: mustJSON("abc.png")}); err == nil {
		t.Error("an icon took a name")
	}
	if err := do(Change{Op: OpUnset, Key: `projectTabStyle.'/a'.icon`}); err == nil {
		t.Error("an icon was unset")
	}
	if err := do(Change{Op: OpSet, Key: `projectTabStyle.'/a'`, Value: json.RawMessage(`{"icon": "abc.png"}`)}); err == nil {
		t.Error("a whole look took an icon's name")
	}
	// The icon a look has stays, whichever field of it the command sets
	// or takes away: only the rail's menu sets one.
	putFile(t, path, `{"projectTabStyle": {"/a": {"icon": "abc.png"}}}`)
	for _, c := range []Change{
		{Op: OpSet, Key: `projectTabStyle.'/a'.label`, Value: mustJSON("MG")},
		{Op: OpSet, Key: `projectTabStyle.'/a'`, Value: json.RawMessage(`{"color": "red"}`)},
		{Op: OpUnset, Key: `projectTabStyle.'/a'`},
	} {
		putFile(t, path, `{"projectTabStyle": {"/a": {"icon": "abc.png"}}}`)
		if err := do(c); err != nil {
			t.Fatal(err)
		}
		if out, err := (Settings{Verb: "get", Keys: []string{`projectTabStyle.'/a'`}}).Do(path); err != nil || !strings.Contains(out, `"icon":"abc.png"`) {
			t.Errorf("%s %s: the icon went: %q, %v", c.Op, c.Key, out, err)
		}
	}
}

// The file keeps the keys in the app's order, so that its own save does
// not rewrite them.
func TestSettingsKeyOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	putFile(t, path, `{"mine": 1, "vim": true}`)
	if _, err := (Settings{Verb: "set", Changes: []Change{{Op: OpSet, Key: "fontSize", Value: json.RawMessage("16")}}}).Do(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	// The app's keys in its order, a key it does not know after them.
	font, vim, mine := strings.Index(got, `"fontSize"`), strings.Index(got, `"vim"`), strings.Index(got, `"mine"`)
	if font < 0 || vim < 0 || mine < 0 || !(font < vim && vim < mine) {
		t.Errorf("the order: %s", got)
	}
}

// A theme an older file kept as an appearance goes with the theme it is.
func TestSettingsLegacyAppearance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	putFile(t, path, `{"appearance": "matrix", "fontSize": 15}`)
	if _, err := (Settings{Verb: "unset", Changes: []Change{{Op: OpUnset, Key: "theme"}}}).Do(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "matrix") {
		t.Errorf("the old appearance stays: %s", data)
	}
	// An appearance of light or dark is no theme, and stays.
	putFile(t, path, `{"appearance": "dark", "theme": "claude"}`)
	if _, err := (Settings{Verb: "unset", Changes: []Change{{Op: OpUnset, Key: "theme"}}}).Do(path); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), `"appearance": "dark"`) {
		t.Errorf("the appearance of a light or dark window went: %s", data)
	}
}
