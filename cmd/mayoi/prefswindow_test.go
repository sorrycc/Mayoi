package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/keymap"
	"mayoi/internal/settings"
)

// prefsHarness is the settings window over the window of a harness, in
// a tester of its own, with the settings in a file of their own.
type prefsHarness struct {
	*harness
	m  *mayoi
	w  *prefsWindow
	pt *ui.Tester
	// c is the window's Context, for its handles.
	c *ui.Context
}

func newPrefsHarness(t *testing.T) *prefsHarness {
	was := setThemeSource
	setThemeSource = func(mygo.ThemeSource) {}
	t.Cleanup(func() { setThemeSource = was })
	hs := newHarness(t, true)
	path := filepath.Join(t.TempDir(), "settings.json")
	s := settings.Load(path)
	s.MarkdownPreview = false
	hs.a.settings = s
	hs.a.window = s.NewWindow()
	hs.a.window.Folder = hs.root
	m := &mayoi{s: s, keys: &keymap.Map{}}
	hs.a.keys = m.keys
	// A frame of no window, which Invalidate leaves be.
	m.wins = []*desktop{{m: m, a: hs.a, f: &frame{m: m, win: &mygo.Window{}}}}
	w := newPrefsWindow(m)
	m.prefs = w
	ph := &prefsHarness{harness: hs, m: m, w: w}
	ph.pt = ui.NewTester(func(c *ui.Context) {
		ph.c = c
		w.view(c)
	}, 940, 680)
	ph.pt.SetDark(true)
	ph.frames()
	return ph
}

// frames draws the settings window as long as its palette takes to fade.
func (ph *prefsHarness) frames() {
	for range 20 {
		step(ph.pt)
	}
}

// Every preference the window shows has its name and what it does, in
// each language, and every option its name.
func TestPrefsTexts(t *testing.T) {
	for _, lang := range []string{"en", "ja", "zh-Hans"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "locales", lang+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var msgs map[string]string
		json.Unmarshal(data, &msgs)
		need := []string{prefKey("prefSection", prefsShortcuts)}
		for _, cat := range settings.Categories {
			need = append(need, prefKey("prefSection", cat))
		}
		for _, f := range settings.Schema {
			if f.Memory {
				continue
			}
			need = append(need, prefKey("prefTitle", f.Key), prefKey("prefDesc", f.Key))
			if f.Kind == settings.KindEnum && f.Key != "locale" {
				for _, o := range f.Options {
					if o == "" {
						need = append(need, prefKey("prefDefault", f.Key))
					} else {
						need = append(need, prefKey(prefKey("prefOpt", f.Key), o))
					}
				}
			}
		}
		for _, k := range need {
			if msgs[k] == "" {
				t.Errorf("%s.json has no %s", lang, k)
			}
		}
	}
}

func TestPrefsShowAndSearch(t *testing.T) {
	ph := newPrefsHarness(t)
	if got := ph.w.shown(false); len(got) == 0 || got[0].Key != "theme" {
		t.Fatalf("appearance shows %v", got)
	}
	ph.w.query = "vim"
	keys := func(fs []settings.Field) []string {
		var out []string
		for _, f := range fs {
			out = append(out, f.Key)
		}
		return out
	}
	if got := keys(ph.w.shown(false)); !slices.Contains(got, "vim") || !slices.Contains(got, "vimCtrlKeys") {
		t.Errorf("found %q", got)
	}
	// The windows' memory, as Source Control's panels, is no preference
	// to show.
	ph.w.query = "scm"
	if got := keys(ph.w.shown(false)); slices.Contains(got, "scmRemotes") {
		t.Errorf("found %q", got)
	}
	ph.w.query = ""
	// A folder shows those it may choose.
	ph.w.section = settings.CatFonts
	if got := ph.w.shown(true); len(got) != 0 {
		t.Errorf("a folder chooses fonts: %q", keys(got))
	}
	for _, sec := range append(slices.Clone(settings.Categories), prefsShortcuts) {
		ph.w.section = sec
		ph.frames()
	}
}

// What the window sets applies in every window, and is written to the
// file; Reset takes it back.
func TestPrefsSet(t *testing.T) {
	ph := newPrefsHarness(t)
	ph.w.set("vim", json.RawMessage(`true`))
	if !ph.m.s.Vim || !ph.a.prefs().Vim {
		t.Fatal("vim is not on")
	}
	ph.w.set("fontSize", json.RawMessage(`99`))
	if ph.m.s.FontSize != 28 {
		t.Errorf("fontSize %v", ph.m.s.FontSize)
	}
	ph.m.s.Flush()
	data, _ := os.ReadFile(ph.m.s.Path())
	if !strings.Contains(string(data), `"vim": true`) {
		t.Errorf("wrote %s", data)
	}
	// A menu changing a preference shows in the window as it is drawn.
	ph.a.toggleVim()
	if ph.m.s.Vim {
		t.Error("the menu left vim on")
	}
	ph.w.set("wordWrap", json.RawMessage(`"x"`))
	if len(ph.w.errors) == 0 {
		t.Error("set a value of the wrong type")
	}
	ph.frames()
}

// A folder's .mayoi/settings.json chooses for its window, over the
// person's, and what the window changes of it goes to the folder's file.
func TestFolderPrefsApply(t *testing.T) {
	ph := newPrefsHarness(t)
	a := ph.a
	if a.folderPrefs == nil {
		t.Fatal("no preferences of the folder")
	}
	os.MkdirAll(filepath.Join(ph.root, settings.FolderDir), 0o755)
	os.WriteFile(settings.FolderPath(ph.root), []byte(`{"vim": true, "fontSize": 20}`), 0o644)
	a.folderPrefs.f.Reload()
	if !a.prefs().Vim || ph.m.s.Vim || a.prefs().FontSize != 13 {
		t.Fatalf("vim %v, the person's %v, fontSize %v", a.prefs().Vim, ph.m.s.Vim, a.prefs().FontSize)
	}
	// The toggle changes the folder's choice, as it is the folder's.
	a.toggleVim()
	if a.prefs().Vim || ph.m.s.Vim {
		t.Error("vim still on")
	}
	data, _ := os.ReadFile(settings.FolderPath(ph.root))
	if !strings.Contains(string(data), `"vim": false`) || !strings.Contains(string(data), `"fontSize": 20`) {
		t.Errorf("the folder's file is %s", data)
	}
	// One the folder doesn't choose is the person's.
	a.toggleBlame()
	if a.prefs().Blame || ph.m.s.Blame {
		t.Error("blame still on")
	}
	if a.folderPrefs.f.Has("blame") {
		t.Error("the folder chose blame")
	}
	// The window shows the folder's, and takes its choice back.
	ph.w.folder = ph.root
	ph.w.section = settings.CatVim
	scope := ph.w.scope()
	if scope == nil {
		t.Fatal("no folder to show")
	}
	ph.frames()
	ph.w.setFolder(scope, "vimCtrlKeys", json.RawMessage(`true`))
	if !a.prefs().VimCtrlKeys || ph.m.s.VimCtrlKeys {
		t.Error("vimCtrlKeys is not the folder's")
	}
	ph.w.setFolder(scope, "vimCtrlKeys", nil)
	if a.prefs().VimCtrlKeys || a.folderPrefs.f.Has("vimCtrlKeys") {
		t.Error("the folder still chooses vimCtrlKeys")
	}
	a.closeFolder()
	ph.settle()
	if a.folderPrefs != nil {
		t.Error("the folder's preferences outlived it")
	}
}

func TestOpenFolderSettings(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.openFolderSettings()
	hs.settle()
	path := settings.FolderPath(hs.root)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if got := hs.a.activeFile(); got != path {
		t.Errorf("shows %q", got)
	}
}

func TestSettingsCommands(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.run("file.settings")
	hs.a.run("file.keybindings")
	if want := []string{":", prefsShortcuts + ":"}; !slices.Equal(hs.h.settingsShown, want) {
		t.Errorf("showed %q", hs.h.settingsShown)
	}
	if got := hs.a.commandID("workbench.action.openSettingsJson"); got != "file.settingsJSON" {
		t.Errorf("openSettingsJson is %q", got)
	}
}

// The shortcuts change keybindings.json, keeping what else it has, and
// apply at once.
func TestPrefsKeys(t *testing.T) {
	dir := t.TempDir()
	m := &mayoi{s: settings.Load(filepath.Join(dir, "settings.json")), keys: &keymap.Map{}}
	w := &prefsWindow{m: m}
	w.keys.w = w
	k := &w.keys
	path := keymap.Path(m.s.Path())
	os.WriteFile(path, []byte("// Mine.\n[\n  { \"key\": \"f6\", \"command\": \"git.fetch\" },\n  { \"key\": \"f7\", \"command\": \"file.save\", \"when\": \"editorFocus\" }\n]\n"), 0o644)
	save := command{id: "file.save", label: "Save", accel: "CmdOrCtrl+S"}
	keys, _ := keymap.ParseKeys("ctrl+k s")
	k.setKeys(save, keys)
	data, _ := os.ReadFile(path)
	text := string(data)
	def, _ := parseAccel("CmdOrCtrl+S")
	for _, want := range []string{"// Mine.", `"git.fetch"`, `"when": "editorFocus"`, `"key": "ctrl+k s", "command": "file.save"`,
		`"key": "` + def.String() + `", "command": "-file.save"`} {
		if !strings.Contains(text, want) {
			t.Errorf("no %s in\n%s", want, text)
		}
	}
	if got := m.keys.Last(commandNames("file.save")...); !slices.Equal(got, keys) {
		t.Errorf("the keys of Save are %v", got)
	}
	// Keys again replace those.
	k.setKeys(save, []keymap.Stroke{def})
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "ctrl+k s") || strings.Contains(string(data), "-file.save") {
		t.Errorf("kept the keys before:\n%s", data)
	}
	k.removeKeys(save)
	if !m.keys.Removes(commandNames("file.save")...) || m.keys.Last(commandNames("file.save")...) != nil {
		t.Error("Save still has keys")
	}
	k.resetKeys(save)
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), `"file.save" }`) || strings.Contains(string(data), "-file.save") || !strings.Contains(string(data), "editorFocus") {
		t.Errorf("reset to\n%s", data)
	}
}

func TestPrefsKeyRows(t *testing.T) {
	ph := newPrefsHarness(t)
	ph.m.keys, _ = keymap.Parse([]byte(`[{"key": "f9", "command": "view.sidebar"}, {"key": "cmd+s", "command": "-file.save"}]`))
	ph.a.keys = ph.m.keys
	k := &ph.w.keys
	rows := k.rows(ph.a)
	find := func(id string) keyRow {
		i := slices.IndexFunc(rows, func(r keyRow) bool { return r.cmd.id == id })
		if i < 0 {
			t.Fatalf("no row of %s", id)
		}
		return rows[i]
	}
	if r := find("view.sidebar"); r.source != keyUser || keymap.KeysString(r.keys) != "f9" {
		t.Errorf("sidebar %v %v", r.source, r.keys)
	}
	if r := find("file.save"); r.source != keyRemoved || r.keys != nil {
		t.Errorf("save %v %v", r.source, r.keys)
	}
	if r := find("file.new"); r.source != keyDefault {
		t.Errorf("new %v", r.source)
	}
	// The search by keys finds the commands of the keys pressed.
	k.byKeys = true
	k.found, _ = keymap.ParseKeys("f9")
	if !k.match(find("view.sidebar")) || k.match(find("file.new")) {
		t.Error("search by keys")
	}
	k.byKeys, k.query = false, "sidebar"
	if !k.match(find("view.sidebar")) || k.match(find("file.new")) {
		t.Error("search by text")
	}
	// Recording finds the other commands of the keys pressed.
	k.query = ""
	newFile := find("file.new")
	k.record(find("file.save").cmd)
	k.strokes = newFile.keys
	if got := k.conflicts(); !slices.Equal(got, []string{newFile.cmd.label}) {
		t.Errorf("conflicts %q", got)
	}
	ph.w.section = prefsShortcuts
	ph.frames()
	k.recording = nil
}

// The sidebar counts what differs from a first launch in each section,
// or what the folder chooses while its own show.
func TestPrefsChangedIn(t *testing.T) {
	ph := newPrefsHarness(t)
	w := ph.w
	if n := w.changedIn(settings.CatVim, nil); n != 0 {
		t.Fatalf("vim has %d changed", n)
	}
	w.set("vim", json.RawMessage(`true`))
	w.set("vimCtrlKeys", json.RawMessage(`true`))
	if n := w.changedIn(settings.CatVim, nil); n != 2 {
		t.Errorf("vim has %d changed", n)
	}
	w.folder = ph.root
	scope := w.scope()
	if scope == nil {
		t.Fatal("no folder to show")
	}
	w.setFolder(scope, "vimCtrlKeys", json.RawMessage(`false`))
	if n := w.changedIn(settings.CatVim, scope); n != 1 {
		t.Errorf("the folder chooses %d of vim", n)
	}
	ph.m.keys, _ = keymap.Parse([]byte(`[{"key": "f9", "command": "view.sidebar"}, {"key": "cmd+s", "command": "-file.save"}, {"key": "f8", "command": "file.save"}, {"key": "f7", "command": "file.new", "when": "editorFocus"}]`))
	if n := w.changedIn(prefsShortcuts, nil); n != 2 {
		t.Errorf("%d commands have the person's keys", n)
	}
	ph.frames()
}

// The dialog that takes the keys fades in each time it shows, and still
// takes them while it does.
func TestPrefsRecordDialog(t *testing.T) {
	ph := newPrefsHarness(t)
	k := &ph.w.keys
	ph.w.section = prefsShortcuts
	ph.frames()
	save := command{id: "file.save", label: "Save", accel: "CmdOrCtrl+S"}
	for range 2 {
		k.record(save)
		step(ph.pt)
		if !k.shown {
			t.Fatal("the dialog did not show")
		}
		if _, ok := ph.pt.Find("Keys for Save"); !ok {
			t.Fatal("no dialog")
		}
		ph.pt.Key(0, ui.KeyEscape)
		ph.frames()
		if k.recording != nil || k.shown {
			t.Fatal("Escape left the dialog")
		}
	}
}

// Resetting a preference shows a toast whose Undo takes the reset back,
// of the person's preferences and of a folder's.
func TestPrefsResetUndo(t *testing.T) {
	ph := newPrefsHarness(t)
	w := ph.w
	w.section = settings.CatFonts
	w.set("fontSize", json.RawMessage(`17`))
	ph.frames()
	w.reset("fontSize", nil)
	if ph.m.s.FontSize != 13 {
		t.Fatalf("reset to %v", ph.m.s.FontSize)
	}
	ph.frames()
	if !ph.pt.HasText("Font Size reset to default") {
		t.Fatal("no toast")
	}
	if err := ph.pt.Click("Undo"); err != nil {
		t.Fatal(err)
	}
	ph.frames()
	if ph.m.s.FontSize != 17 {
		t.Errorf("undid to %v", ph.m.s.FontSize)
	}
	if ph.pt.HasText("Font Size reset to default") {
		t.Error("the toast stayed")
	}
	// A folder's choice comes back.
	w.folder, w.section = ph.root, settings.CatVim
	scope := w.scope()
	if scope == nil {
		t.Fatal("no folder to show")
	}
	w.setFolder(scope, "vim", json.RawMessage(`true`))
	w.reset("vim", scope)
	if ph.a.folderPrefs.f.Has("vim") {
		t.Fatal("the folder still chooses vim")
	}
	w.notes[len(w.notes)-1].OnAction()
	if !ph.a.folderPrefs.f.Has("vim") || string(ph.a.folderPrefs.f.Raw("vim")) != "true" {
		t.Errorf("the folder chooses %s", ph.a.folderPrefs.f.Raw("vim"))
	}
	ph.frames()
}

// Taking a command's keys away, or back to its own, shows a toast whose
// Undo gives it the keys it had.
func TestPrefsKeysUndo(t *testing.T) {
	dir := t.TempDir()
	m := &mayoi{s: settings.Load(filepath.Join(dir, "settings.json")), keys: &keymap.Map{}}
	w := &prefsWindow{m: m}
	w.keys.w = w
	k := &w.keys
	path := keymap.Path(m.s.Path())
	os.WriteFile(path, []byte("[\n  { \"key\": \"f6\", \"command\": \"file.save\" },\n  { \"key\": \"cmd+s\", \"command\": \"-file.save\" }\n]\n"), 0o644)
	save := command{id: "file.save", label: "Save", accel: "CmdOrCtrl+S"}
	names := commandNames("file.save")
	k.resetKeys(save)
	if m.keys.Last(names...) != nil || len(w.notes) != 1 {
		t.Fatalf("reset left %v, %d toasts", m.keys.Last(names...), len(w.notes))
	}
	w.notes[0].OnAction()
	if got := keymap.KeysString(m.keys.Last(names...)); got != "f6" {
		t.Errorf("undid to %q", got)
	}
	w.notes = nil
	k.removeKeys(save)
	if m.keys.Last(names...) != nil || !m.keys.Removes(names...) {
		t.Fatal("Save still has keys")
	}
	w.notes[0].OnAction()
	if got := keymap.KeysString(m.keys.Last(names...)); got != "f6" {
		t.Errorf("undid to %q", got)
	}
}

// A slider shows its value, and the code of the fonts, as it is dragged,
// and applies it once let go.
func TestPrefsSlider(t *testing.T) {
	ph := newPrefsHarness(t)
	w := ph.w
	w.section = settings.CatFonts
	ph.frames()
	value, ok := ph.pt.Find("13 pt")
	if !ok {
		t.Fatal("no value of the font size")
	}
	// The slider ends 10 to the left of its value, and is 168 wide.
	y := value.Y + value.H/2
	right := value.X - 10
	ph.pt.Press(right-160, y)
	step(ph.pt)
	step(ph.pt)
	// Past the slider's end, still dragging.
	ph.pt.Move(right+40, y+30)
	step(ph.pt)
	if w.held != "fontSize" {
		t.Fatalf("held %q", w.held)
	}
	if ph.m.s.FontSize != 13 {
		t.Errorf("applied %v while dragging", ph.m.s.FontSize)
	}
	live := w.liveNumber("fontSize", 13)
	if live != 28 || !ph.pt.HasText(prefNumber("fontSize", live)) {
		t.Errorf("shows %v", live)
	}
	ph.pt.Release(right+40, y+30)
	ph.frames()
	if w.held != "" || float64(ph.m.s.FontSize) != live {
		t.Errorf("held %q, applied %v of %v", w.held, ph.m.s.FontSize, live)
	}
}

// A tile of the themes and a switch of options choose with a click.
func TestPrefsThemeTilesAndSwitch(t *testing.T) {
	ph := newPrefsHarness(t)
	if err := ph.pt.Click("Claude"); err != nil {
		t.Fatal(err)
	}
	ph.frames()
	if ph.m.s.Theme != themeClaude {
		t.Errorf("theme %q", ph.m.s.Theme)
	}
	if err := ph.pt.Click("Dark"); err != nil {
		t.Fatal(err)
	}
	ph.frames()
	if ph.m.s.Appearance != "dark" {
		t.Errorf("appearance %q", ph.m.s.Appearance)
	}
	// The themes that are always dark leave the appearance as it is.
	ph.w.set("theme", json.RawMessage(`"matrix"`))
	ph.frames()
	ph.pt.Click("Light")
	ph.frames()
	if ph.m.s.Appearance != "dark" {
		t.Errorf("appearance %q under The Matrix", ph.m.s.Appearance)
	}
}

func TestPrefNumber(t *testing.T) {
	for _, tc := range []struct {
		key  string
		v    float64
		want string
	}{{"zoomLevel", 0, "100%"}, {"zoomLevel", 1, "120%"}, {"fontSize", 13, "13 pt"}, {"lineHeight", 1.5, "1.50"}, {"fontWeight", 400, "400"}} {
		if got := prefNumber(tc.key, tc.v); got != tc.want {
			t.Errorf("%s %v is %q", tc.key, tc.v, got)
		}
	}
	if !segmentedFits([]string{"System", "Light", "Dark"}) || segmentedFits([]string{"Side by side when wide", "Side by side", "Inline"}) {
		t.Error("segmentedFits")
	}
}

// Every command has the group of its menu, and the list heads each
// group with how many of its commands show.
func TestPrefsKeyGroups(t *testing.T) {
	ph := newPrefsHarness(t)
	rows := ph.w.keys.rows(ph.a)
	for _, r := range rows {
		g := keyGroups[keyGroup(r.cmd.id)]
		if !slices.ContainsFunc(g.prefixes, func(p string) bool { return strings.HasPrefix(r.cmd.id, p) }) {
			t.Errorf("%s is of no menu", r.cmd.id)
		}
	}
	items := keyItems(rows)
	if items[0].head != "File" || items[1].row.cmd.id != "file.new" {
		t.Fatalf("the list starts with %+v, %+v", items[0], items[1])
	}
	heads, n := 0, 0
	for i, it := range items {
		if it.head == "" {
			continue
		}
		heads++
		n += it.count
		j := i + 1
		for j < len(items) && items[j].head == "" {
			j++
		}
		if j-i-1 != it.count {
			t.Errorf("%s heads %d commands, says %d", it.head, j-i-1, it.count)
		}
	}
	if heads != len(keyGroups) || n != len(rows) {
		t.Errorf("%d heads of %d commands, of %d", heads, n, len(rows))
	}
}

// Commands of the same keys mark each other, and the filters show those,
// or those whose keys the person changed.
func TestPrefsKeyConflicts(t *testing.T) {
	ph := newPrefsHarness(t)
	k := &ph.w.keys
	for _, r := range k.rows(ph.a) {
		if len(r.others) > 0 {
			t.Errorf("%s shares its own keys with %q", r.cmd.id, r.others)
		}
	}
	ph.m.keys, _ = keymap.Parse([]byte(`[{"key": "cmd+b", "command": "git.fetch"}, {"key": "f9", "command": "git.pull"}]`))
	ph.a.keys = ph.m.keys
	rows := k.rows(ph.a)
	var conflicting, changed []string
	for _, r := range rows {
		if keysConflicting.shows(r) {
			conflicting = append(conflicting, r.cmd.id)
		}
		if keysChanged.shows(r) {
			changed = append(changed, r.cmd.id)
		}
	}
	if !slices.Equal(conflicting, []string{"view.sidebar", "git.fetch"}) {
		t.Errorf("conflicting %q", conflicting)
	}
	if !slices.Equal(changed, []string{"git.fetch", "git.pull"}) {
		t.Errorf("changed %q", changed)
	}
	ph.w.section = prefsShortcuts
	ph.frames()
	if !ph.pt.HasText("Conflicts") || ph.pt.HasText("No two commands share keys") {
		t.Fatal("no filters")
	}
	if err := ph.pt.Click("Conflicts"); err != nil {
		t.Fatal(err)
	}
	ph.frames()
	if k.filter != keysConflicting || !ph.pt.HasText("Fetch") || ph.pt.HasText("New File") {
		t.Errorf("the filter %v shows %q", k.filter, ph.pt.Texts())
	}
	ph.m.keys, _ = keymap.Parse([]byte(`[]`))
	ph.a.keys = ph.m.keys
	ph.frames()
	if !ph.pt.HasText("No two commands share keys") {
		t.Error("no word of no conflicts")
	}
}

// The keyboard goes from the search to the shortcuts, moves the choice
// among them, changes or takes away the keys of the one chosen, and back
// to the sections; Escape takes the search back.
func TestPrefsKeysKeyboard(t *testing.T) {
	ph := newPrefsHarness(t)
	w, k := ph.w, &ph.w.keys
	w.section = prefsShortcuts
	ph.frames()
	w.find()
	ph.frames()
	if !ph.pt.Focused("Search commands") {
		t.Fatal("Find left the search of the shortcuts")
	}
	ph.pt.Type("save")
	ph.frames()
	ph.pt.Key(0, ui.KeyDown)
	ph.frames()
	if !k.listH.Focused(ph.c) {
		t.Fatal("Down left the list")
	}
	ph.pt.Key(0, ui.KeyDown)
	ph.frames()
	if k.chosen != "file.save" {
		t.Fatalf("chose %q", k.chosen)
	}
	ph.pt.Key(0, ui.KeyEnter)
	ph.frames()
	if k.recording == nil || k.recording.id != "file.save" {
		t.Fatal("Enter did not record the keys of Save")
	}
	ph.pt.Key(0, ui.KeyEscape)
	ph.frames()
	if !k.listH.Focused(ph.c) {
		t.Fatal("the list did not take the keyboard back")
	}
	ph.pt.Key(0, ui.KeyBackspace)
	ph.frames()
	if !ph.m.keys.Removes(commandNames("file.save")...) {
		t.Error("Backspace left the keys of Save")
	}
	ph.pt.Key(0, ui.KeyEscape)
	ph.frames()
	if k.query != "" {
		t.Errorf("Escape left %q", k.query)
	}
	ph.pt.Key(0, ui.KeyLeft)
	ph.frames()
	if !w.sectionsH.Focused(ph.c) {
		t.Fatal("Left left the sections")
	}
	ph.pt.Key(0, ui.KeyUp)
	ph.frames()
	if w.section != settings.CatGeneral {
		t.Errorf("Up chose %q", w.section)
	}
	// Right goes on to the first control of the section, Find back to the
	// search.
	ph.pt.Key(0, ui.KeyRight)
	ph.frames()
	if w.sectionsH.Focused(ph.c) {
		t.Error("Right stayed in the sections")
	}
	ph.m.run("edit.find", w.win)
	ph.frames()
	if !ph.pt.Focused("Search settings") {
		t.Error("Find did not go to the search")
	}
}

// The fonts installed show in a picker of them, which applies the one
// chosen at once, and what is typed as Enter is pressed.
func TestPrefsFontPicker(t *testing.T) {
	ph := newPrefsHarness(t)
	w := ph.w
	w.section = settings.CatFonts
	w.families = []string{"Courier New", "Menlo", "Monaco"}
	ph.frames()
	if err := ph.pt.Click("Show Fonts"); err != nil {
		t.Fatal(err)
	}
	ph.frames()
	for _, f := range []string{"monospace", "Courier New", "Menlo", "Monaco"} {
		if !ph.pt.HasText(f) {
			t.Fatalf("no %s in %q", f, ph.pt.Texts())
		}
	}
	if err := ph.pt.Click("Menlo"); err != nil {
		t.Fatal(err)
	}
	ph.frames()
	if ph.m.s.FontFamily != "Menlo" {
		t.Fatalf("chose %q", ph.m.s.FontFamily)
	}
	// Typing filters the fonts, and applies as Enter is pressed.
	w.firstH.SelectAll()
	ph.frames()
	ph.pt.Type("Iosevka, mono")
	ph.frames()
	if ph.m.s.FontFamily != "Menlo" || ph.pt.HasText("Monaco") {
		t.Errorf("typing applied %q, or showed every font", ph.m.s.FontFamily)
	}
	ph.pt.Key(0, ui.KeyEnter)
	ph.frames()
	if ph.m.s.FontFamily != "Iosevka, mono" {
		t.Errorf("Enter applied %q", ph.m.s.FontFamily)
	}
	// A font chosen after some typing is the one that stays, as the page
	// goes.
	w.firstH.SelectAll()
	ph.frames()
	ph.pt.Type("Mon")
	ph.frames()
	if err := ph.pt.Click("Monaco"); err != nil {
		t.Fatal(err)
	}
	ph.frames()
	w.section = settings.CatVim
	ph.frames()
	if ph.m.s.FontFamily != "Monaco" {
		t.Errorf("left with %q", ph.m.s.FontFamily)
	}
	if got := fontOptions([]string{"Menlo", "Hack", "JetBrains Mono"}, "mon"); !slices.Equal(got, []string{"monospace", "JetBrains Mono"}) {
		t.Errorf("mon finds %q", got)
	}
	if got := monoFamilies([]ui.FontFamily{{Name: "Menlo", Monospaced: true}, {Name: "Helvetica"}, {Name: ".SF NS Mono", Monospaced: true}}); !slices.Equal(got, []string{"Menlo"}) {
		t.Errorf("monoFamilies %q", got)
	}
}

// What is typed in a text field applies as the field is left, also with
// its page.
func TestPrefsTextDraft(t *testing.T) {
	ph := newPrefsHarness(t)
	w := ph.w
	w.section = settings.CatGit
	w.query = "aiModel"
	ph.frames()
	w.firstH.Focus()
	ph.frames()
	ph.pt.Type("qwen")
	ph.frames()
	if ph.m.s.AIModel != "" {
		t.Fatalf("applied %q at a key", ph.m.s.AIModel)
	}
	w.query = ""
	w.section = settings.CatVim
	ph.frames()
	if ph.m.s.AIModel != "qwen" || len(w.drafts) != 0 {
		t.Errorf("left with %q, %d drafts", ph.m.s.AIModel, len(w.drafts))
	}
}

// Once its motion settles, the window asks for no more frames, in every
// section, with less motion too.
func TestPrefsIdle(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		ph := newPrefsHarness(t)
		ph.pt.SetPreferences(ui.Preferences{ReduceMotion: reduce})
		for _, sec := range append(slices.Clone(settings.Categories), prefsShortcuts) {
			ph.w.section = sec
			step(ph.pt)
			if !reduce && !ph.pt.FramePending() {
				t.Errorf("%s rose in without frames", sec)
			}
			for range 60 {
				step(ph.pt)
			}
			if ph.pt.FramePending() {
				t.Errorf("%s keeps drawing (less motion %v)", sec, reduce)
			}
		}
	}
}
