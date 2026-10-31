package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Every preference of the file is in the schema, once, and the schema has
// no other.
func TestSchemaCoversThePrefs(t *testing.T) {
	var keys []string
	for _, f := range Schema {
		if slices.Contains(keys, f.Key) {
			t.Errorf("%s twice", f.Key)
		}
		keys = append(keys, f.Key)
		if !slices.Contains(Categories, f.Category) {
			t.Errorf("%s: no category %q", f.Key, f.Category)
		}
		switch f.Kind {
		case KindEnum:
			if len(f.Options) < 2 || f.Options[0] != "" {
				t.Errorf("%s: options %q", f.Key, f.Options)
			}
		case KindNumber:
			if f.Min >= f.Max || f.Step <= 0 {
				t.Errorf("%s: bounds %v to %v by %v", f.Key, f.Min, f.Max, f.Step)
			}
		}
	}
	slices.Sort(keys)
	want := slices.Sorted(slices.Values(PrefKeys))
	if !slices.Equal(keys, want) {
		t.Errorf("schema has %q, the preferences %q", keys, want)
	}
}

// What the window sets of a preference is kept in its bounds, as the
// file's is.
func TestSetAPreference(t *testing.T) {
	p := Default().Prefs
	if err := p.Set("fontSize", json.RawMessage(`40`)); err != nil || p.FontSize != 28 {
		t.Errorf("fontSize %v, %v", p.FontSize, err)
	}
	if err := p.Set("vim", json.RawMessage(`true`)); err != nil || !p.Vim {
		t.Errorf("vim %v, %v", p.Vim, err)
	}
	if err := p.Set("fontFeatures", json.RawMessage(`["calt=0", " "]`)); err != nil || !slices.Equal(p.FontFeatures, []string{"calt=0"}) {
		t.Errorf("fontFeatures %q, %v", p.FontFeatures, err)
	}
	if err := p.Set("fontFeatures", json.RawMessage(`["ss01"]`)); err != nil || !slices.Equal(p.FontFeatures, []string{"ss01"}) {
		t.Errorf("a list replaced %q, %v", p.FontFeatures, err)
	}
	if p.Set("nope", json.RawMessage(`1`)) == nil || p.Set("vim", json.RawMessage(`"x"`)) == nil {
		t.Error("set what is not a preference")
	}
	if got := string(p.Raw("theme")); got != `""` {
		t.Errorf("theme %s", got)
	}
	if p.IsDefaultKey("vim") || !p.IsDefaultKey("theme") || !p.IsDefaultKey("syncIgnored") {
		t.Error("defaults")
	}
	p.Reset("vim")
	p.Reset("fontSize")
	if p.Vim || p.FontSize != 13 {
		t.Errorf("reset to %v and %v", p.Vim, p.FontSize)
	}
}

// docSection is the section of docs/SETTINGS.md under heading.
func docSection(t *testing.T, heading string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "SETTINGS.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	at := strings.Index(doc, "\n"+heading+"\n")
	if at < 0 {
		t.Fatalf("no %q in SETTINGS.md", heading)
	}
	doc = doc[at+len(heading)+2:]
	// Up to the next heading of its level or above.
	level := len(strings.SplitN(heading, " ", 2)[0])
	next := regexp.MustCompile(fmt.Sprintf("(?m)^#{1,%d} ", level))
	if end := next.FindStringIndex(doc); end != nil {
		doc = doc[:end[0]]
	}
	return doc
}

// The table of docs/SETTINGS.md has every preference the window shows,
// with its default, and the list of those a folder may choose is the
// schema's.
func TestTheDocsKeepUp(t *testing.T) {
	row := regexp.MustCompile("(?m)^\\| `([a-zA-Z]+)` +\\| ([^|]+?) +\\|")
	d := Default().Prefs
	var documented []string
	for _, m := range row.FindAllStringSubmatch(docSection(t, "## settings.json"), -1) {
		key, def := m[1], strings.Trim(m[2], "` ")
		documented = append(documented, key)
		f, ok := FieldOf(key)
		if !ok {
			t.Errorf("SETTINGS.md has %s, which is no preference", key)
			continue
		}
		if f.Memory {
			t.Errorf("SETTINGS.md has %s, which the windows remember", key)
		}
		if def != "none" && !jsonEqual(json.RawMessage(def), d.Raw(key)) {
			t.Errorf("SETTINGS.md says %s is %s by default, not %s", key, def, d.Raw(key))
		}
	}
	for _, f := range Schema {
		if !f.Memory && !slices.Contains(documented, f.Key) {
			t.Errorf("SETTINGS.md's table leaves out %s", f.Key)
		}
	}
	keys := regexp.MustCompile("`([a-zA-Z]+)`").FindAllStringSubmatch(docSection(t, "### Folder settings"), -1)
	var folder []string
	for _, k := range keys {
		if _, ok := FieldOf(k[1]); ok && !slices.Contains(folder, k[1]) {
			folder = append(folder, k[1])
		}
	}
	if want := FolderKeys(); !slices.Equal(folder, want) {
		t.Errorf("SETTINGS.md says a folder may choose %q, not %q", folder, want)
	}
}

func TestFolderPrefs(t *testing.T) {
	dir := t.TempDir()
	f := LoadFolder(dir)
	if len(f.Keys()) != 0 || f.Gen() != 0 {
		t.Fatalf("a folder without a file chooses %q", f.Keys())
	}
	os.MkdirAll(filepath.Join(dir, FolderDir), 0o755)
	os.WriteFile(FolderPath(dir), []byte(`{"vim": true, "fontSize": 20, "wordWrap": "yes", "other": 1}`), 0o644)
	if !f.Reload() || f.Reload() {
		t.Fatal("reloads")
	}
	// fontSize is the user's alone; a value of the wrong type is let be.
	if got := f.Keys(); !slices.Equal(got, []string{"wordWrap", "vim"}) {
		t.Errorf("keys %q", got)
	}
	user := Default().Prefs
	user.WordWrap = true
	p := f.Apply(user)
	if !p.Vim || p.FontSize != 13 || !p.WordWrap {
		t.Errorf("applied %v %v %v", p.Vim, p.FontSize, p.WordWrap)
	}
	if user.Vim {
		t.Error("the user's preferences changed")
	}
	gen := f.Gen()
	if err := f.Set("vim", nil); err != nil {
		t.Fatal(err)
	}
	if err := f.Set("diffContext", json.RawMessage(`10`)); err != nil {
		t.Fatal(err)
	}
	if f.Gen() == gen || f.Has("vim") || !f.Has("diffContext") {
		t.Errorf("set: %q", f.Keys())
	}
	// Its own write is not a change, and keeps what it doesn't know.
	if f.Reload() {
		t.Error("its own write reloaded")
	}
	var back map[string]json.RawMessage
	data, _ := os.ReadFile(FolderPath(dir))
	json.Unmarshal(data, &back)
	if _, ok := back["other"]; !ok || string(back["diffContext"]) != "10" {
		t.Errorf("wrote %s", data)
	}
	// A file that does not parse is let be.
	os.WriteFile(FolderPath(dir), []byte(`{`), 0o644)
	if f.Reload() || !f.Has("diffContext") {
		t.Error("took a broken file")
	}
	var nilFolder *Folder
	if nilFolder.Apply(user).Vim != user.Vim || nilFolder.Has("vim") {
		t.Error("no folder")
	}
}

func TestWatchTheFolder(t *testing.T) {
	dir := t.TempDir()
	f := LoadFolder(dir)
	got := make(chan bool, 4)
	stop, err := f.Watch(func() { got <- true })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	// The folder of the file is made after the watch starts.
	os.MkdirAll(filepath.Join(dir, FolderDir), 0o755)
	time.Sleep(150 * time.Millisecond)
	os.WriteFile(FolderPath(dir), []byte(`{"blame": false}`), 0o644)
	select {
	case <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("no change told")
	}
	if !f.Has("blame") {
		t.Errorf("keys %q", f.Keys())
	}
}

// Changed counts the changes of the preferences, not of the state, and
// tells of them.
func TestPrefsGen(t *testing.T) {
	s := Load(filepath.Join(t.TempDir(), "settings.json"))
	told := 0
	s.OnPrefsChanged(func() { told++ })
	s.Changed()
	gen := s.Gen()
	s.Recent = []string{"/tmp"}
	s.Changed()
	if s.Gen() != gen {
		t.Error("a change of the state counted")
	}
	s.Vim = true
	s.Changed()
	if s.Gen() == gen || told != 2 {
		t.Errorf("gen %d from %d, told %d", s.Gen(), gen, told)
	}
	p := s.Prefs
	p.Blame = false
	gen = s.Gen()
	s.SetPrefs(p)
	if s.Gen() == gen {
		t.Error("SetPrefs did not count")
	}
}

// A reload of the file while the folder sets a preference, as its watcher
// does, never takes the change back.
func TestFolderReloadDuringSet(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, FolderDir), 0o755)
	f := LoadFolder(dir)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				f.Reload()
			}
		}
	}()
	defer close(done)
	for i := range 200 {
		on := i%2 == 0
		f.Set("vim", json.RawMessage(strconv.FormatBool(on)))
		if p := f.Apply(Prefs{}); p.Vim != on {
			t.Fatalf("set %d: vim is %v", i, p.Vim)
		}
	}
}
