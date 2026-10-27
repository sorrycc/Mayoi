package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	s := Load(path)
	w := s.NewWindow()
	w.Folder, w.Columns = "/tmp/x", []Column{{Tabs: []Tab{{File: "/tmp/x/a.go"}}}}
	w.SidebarWidth = 300
	w.ScmChangesRatio = 0.65
	// Laid out as the window focused last, then as it chooses.
	y := s.NewWindow()
	y.Folder = "/tmp/y"
	y.SidebarView = "git"
	s.Flush()
	got := Load(path)
	if len(got.Windows) != 2 || got.Windows[0].Folder != "/tmp/y" || got.Windows[0].ID != 1 ||
		got.Windows[1].Folder != "/tmp/x" || len(got.Windows[1].Columns) != 1 {
		t.Errorf("loaded %+v", got)
	}
	if x, y := got.Windows[1].Layout, got.Windows[0].Layout; x.SidebarWidth != 300 || x.SidebarView != "" || x.ScmChangesRatio != 0.65 ||
		y.SidebarWidth != 300 || y.SidebarView != "git" || y.PanelHeight != DefaultLayout.PanelHeight {
		t.Errorf("layouts %+v and %+v", x, y)
	}
}

func TestZoomLevel(t *testing.T) {
	for _, tc := range []struct {
		json string
		want float32
	}{
		{`{}`, 0},
		{`{"zoomLevel": 1.5}`, 1.5},
		{`{"zoomLevel": -0.75}`, -0.75},
		{`{"zoomLevel": -100}`, MinZoomLevel},
		{`{"zoomLevel": 100}`, MaxZoomLevel},
	} {
		t.Run(tc.json, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, []byte(tc.json), 0o644); err != nil {
				t.Fatal(err)
			}
			s := Load(path)
			if s.ZoomLevel != tc.want {
				t.Fatalf("loaded zoom %v, want %v", s.ZoomLevel, tc.want)
			}
			s.Flush()
			if got := Load(path).ZoomLevel; got != tc.want {
				t.Fatalf("saved zoom %v, want %v", got, tc.want)
			}
			if err := os.WriteFile(path, []byte(`{"zoomLevel": 0.25}`), 0o644); err != nil {
				t.Fatal(err)
			}
			p, ok := s.Edited()
			if !ok || p.ZoomLevel != 0.25 {
				t.Fatalf("reloaded zoom %v, changed %v", p.ZoomLevel, ok)
			}
		})
	}
}

// The layout an older version kept for every window becomes each
// window's, and each session's.
func TestMigrateTheLayout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"sidebarOpen": false, "sidebarView": "git",
		"sidebarWidth": 320, "panelHeight": 400, "recent": ["/tmp/x", "/tmp/y"],
		"windows": [{"id": 0, "folder": "/tmp/x"}],
		"sessions": {"/tmp/y": {"expanded": ["src"]}}}`), 0o644)
	s := Load(path)
	// The panel's own width is new here, so it takes the default.
	want := DefaultLayout
	want.SidebarView, want.SidebarClosed, want.SidebarWidth, want.PanelHeight = "git", true, 320, 400
	if len(s.Windows) != 1 || s.Windows[0].Layout != want || s.Sessions["/tmp/y"] == nil || s.Sessions["/tmp/y"].Layout != want {
		t.Fatalf("windows %+v, sessions %+v", s.Windows, s.Sessions)
	}
	_, state := s.Encode()
	if strings.Contains(string(state), `"sidebarOpen"`) {
		t.Errorf("state %s", state)
	}
}

// The window of a version with one, and the tabs of one without columns,
// become the first window.
func TestMigrateTheWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	old := map[string]any{
		"sidebarWidth": 300, "folder": "/tmp/x", "panelOpen": true, "terminalOpen": true,
		"openFiles": []string{"/tmp/x/a.go", "/tmp/x/b.go"}, "activeFile": "/tmp/x/b.go",
	}
	data, _ := json.Marshal(old)
	os.WriteFile(path, data, 0o644)
	s := Load(path)
	if len(s.Windows) != 1 || s.Windows[0].SidebarWidth != 300 {
		t.Fatalf("windows %+v", s.Windows)
	}
	w := s.Windows[0]
	if w.ID != 0 || w.Folder != "/tmp/x" || !w.PanelOpen || !w.TerminalOpen || len(w.Columns) != 1 ||
		len(w.Columns[0].Tabs) != 2 || w.Columns[0].Active != 1 {
		t.Fatalf("window %+v", w)
	}
	data, _ = os.ReadFile(path)
	for _, key := range []string{`"folder"`, `"openFiles"`, `"sidebarWidth"`} {
		if strings.Contains(string(data), key) {
			t.Errorf("%s still kept in the settings", key)
		}
	}
	if got := Load(path); len(got.Windows) != 1 || got.Windows[0].SidebarWidth != 300 {
		t.Errorf("loaded again: windows %+v", got.Windows)
	}
}

// The settings' file keeps the preferences, and state.json beside it the
// state: the state an older version kept in the settings moves there,
// once.
func TestStateMovesToItsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{"fontSize": 15, "sidebarWidth": 320, "recent": ["/tmp/x"],
		"windows": [{"id": 0, "folder": "/tmp/x", "columns": [{"tabs": [{"file": "/tmp/x/a.go"}], "active": 0, "weight": 1}]}]}`), 0o644)
	s := Load(path)
	if s.FontSize != 15 || len(s.Windows) != 1 || s.Windows[0].SidebarWidth != 320 || len(s.Recent) != 1 {
		t.Fatalf("loaded %+v", s)
	}
	prefs, _ := os.ReadFile(path)
	state, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(prefs), "windows") || strings.Contains(string(prefs), "recent") ||
		!strings.Contains(string(prefs), `"fontSize": 15`) {
		t.Errorf("settings %s", prefs)
	}
	if !strings.Contains(string(state), `"/tmp/x/a.go"`) || strings.Contains(string(state), "fontSize") {
		t.Errorf("state %s", state)
	}
	// A change of the state only leaves the settings' file be.
	info, _ := os.Stat(path)
	os.Chtimes(path, info.ModTime().Add(-time.Hour), info.ModTime().Add(-time.Hour))
	before, _ := os.Stat(path)
	s.NewWindow().Folder = "/tmp/y"
	s.Flush()
	if after, _ := os.Stat(path); !after.ModTime().Equal(before.ModTime()) {
		t.Error("the settings' file was written for a change of the state")
	}
	if got := Load(path); len(got.Windows) != 2 || got.FontSize != 15 {
		t.Errorf("loaded again: windows %d, font %v", len(got.Windows), got.FontSize)
	}
}

// A window that closes keeps its folder's session while the folder is a
// recent one, which the next window of the folder takes.
func TestSessions(t *testing.T) {
	// Folders as the system spells them, with backslashes on Windows.
	x, y := filepath.FromSlash("/tmp/x"), filepath.FromSlash("/tmp/y")
	s := Default()
	s.AddRecent(x)
	w := s.NewWindow()
	w.Folder = x + string(filepath.Separator)
	w.Columns = []Column{{Tabs: []Tab{{File: filepath.Join(x, "a.go"), Sel: [2]int{3, 5}}}}}
	w.Expanded = []string{"internal"}
	s.Forget(w)
	if len(s.Windows) != 0 || s.Sessions[x] == nil {
		t.Fatalf("windows %d, sessions %v", len(s.Windows), s.Sessions)
	}
	ses := s.TakeSession(x)
	if ses == nil || ses.Columns[0].Tabs[0].Sel != [2]int{3, 5} || ses.Expanded[0] != "internal" || len(s.Sessions) != 0 {
		t.Fatalf("taken %+v, left %v", ses, s.Sessions)
	}
	// Not a recent folder, or a window with nothing to keep.
	other := s.NewWindow()
	other.Folder = y
	other.Columns = w.Columns
	s.Forget(other)
	if s.Sessions[y] != nil {
		t.Error("kept the session of a folder not recent")
	}
	s.Stash(w)
	empty := &Window{Folder: x, Session: Session{Columns: []Column{{}}}}
	s.Stash(empty)
	if s.Sessions[x] != nil {
		t.Error("an empty window kept its folder's session")
	}
	// Folders no longer recent, and clearing them, let theirs go.
	s.Stash(w)
	for i := range 10 {
		s.AddRecent(filepath.Join(filepath.FromSlash("/tmp/z"), string(rune('a'+i))))
	}
	if len(s.Sessions) != 0 {
		t.Errorf("kept %v", s.Sessions)
	}
	s.AddRecent(x)
	s.Stash(w)
	s.ClearRecent()
	if s.Sessions != nil {
		t.Errorf("cleared, kept %v", s.Sessions)
	}
}

// The windows are kept the one focused last first; a new one takes the
// first ID that is free, and the one that closes lets its own go.
func TestKeepTheWindowsInOrder(t *testing.T) {
	s := Default()
	a, b, c := s.NewWindow(), s.NewWindow(), s.NewWindow()
	if ids := []int{a.ID, b.ID, c.ID}; !slices.Equal(ids, []int{0, 1, 2}) || s.Windows[0] != c {
		t.Fatalf("IDs %v", ids)
	}
	s.Focused(a)
	if !slices.Equal(s.Windows, []*Window{a, c, b}) {
		t.Fatalf("order after focusing the first")
	}
	s.Forget(c)
	if d := s.NewWindow(); d.ID != 2 || s.Windows[0] != d || len(s.Windows) != 3 {
		t.Fatalf("new window %d of %d", d.ID, len(s.Windows))
	}
}

// The rail of project tabs keeps its own order, whichever window was
// focused last; windows it has not seen go after those it has.
func TestRailOrder(t *testing.T) {
	s := Default()
	a, b, c := s.NewWindow(), s.NewWindow(), s.NewWindow()
	s.SetRail([]*Window{a, b})
	s.Focused(b)
	if got := s.InRail(); !slices.Equal(got, []*Window{a, b, c}) {
		t.Fatalf("rail %v", got)
	}
	s.SetRail([]*Window{c, a, b})
	if got := s.InRail(); !slices.Equal(got, []*Window{c, a, b}) || !slices.Equal(s.Rail, []int{2, 0, 1}) {
		t.Fatalf("rail %v, ids %v", got, s.Rail)
	}
}

func TestMigrateDiffSplit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"diffSplit": true}`), 0o644)
	if s := Load(path); s.DiffLayout != "split" {
		t.Errorf("layout %q", s.DiffLayout)
	}
	os.WriteFile(path, []byte(`{}`), 0o644)
	if s := Load(path); s.DiffLayout != "" {
		t.Errorf("layout %q", s.DiffLayout)
	}
}

// The recent folders leave out the one given and those gone from the
// disk, which they keep; Clear forgets them all.
func TestRecentExcept(t *testing.T) {
	a, b, gone := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "gone")
	s := Default()
	for _, d := range []string{gone, b, a} {
		s.AddRecent(d)
	}
	if got := s.RecentExcept(a); !slices.Equal(got, []string{b}) {
		t.Errorf("except %s: %v", a, got)
	}
	if got := s.RecentExcept(""); !slices.Equal(got, []string{a, b}) {
		t.Errorf("all: %v", got)
	}
	if len(s.Recent) != 3 {
		t.Errorf("a folder gone was forgotten: %v", s.Recent)
	}
	s.ClearRecent()
	if len(s.Recent) != 0 || len(s.RecentExcept("")) != 0 {
		t.Errorf("cleared: %v", s.Recent)
	}
}

// The font preferences keep in their bounds, and take their defaults
// where the file leaves them out.
func TestFontPrefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if s := Load(path); s.FontWeight != 400 || s.LineHeight != 1.55 || s.TerminalFontSize != 0 || s.FontFeatures != nil {
		t.Fatalf("defaults %+v", s.Prefs)
	}
	os.WriteFile(path, []byte(`{"fontFamily": " ", "fontWeight": 1000, "lineHeight": 9, "terminalFontSize": 4,
		"fontFeatures": ["calt=0", "", "ss01"]}`), 0o644)
	s := Load(path)
	if s.FontFamily != "monospace" || s.FontWeight != 900 || s.LineHeight != 3 || s.TerminalFontSize != 9 ||
		!slices.Equal(s.FontFeatures, []string{"calt=0", "ss01"}) {
		t.Fatalf("loaded %+v", s.Prefs)
	}
}

// The terminal copies what is selected unless the file turns it off.
func TestTerminalCopyOnSelect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if !Load(path).TerminalCopyOnSelect {
		t.Fatal("off by default")
	}
	os.WriteFile(path, []byte(`{"fontSize": 14}`), 0o644)
	if !Load(path).TerminalCopyOnSelect {
		t.Fatal("off in a file that leaves it out")
	}
	os.WriteFile(path, []byte(`{"terminalCopyOnSelect": false}`), 0o644)
	if Load(path).TerminalCopyOnSelect {
		t.Fatal("on in a file that turns it off")
	}
}

// An edit of the file changes the preferences it watches, but not the
// windows; the settings' own writes and a file that does not parse
// change nothing.
func TestWatchTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := Load(path)
	s.NewWindow().Folder = "/tmp/x"
	s.Flush()
	got := make(chan Prefs, 4)
	stop, err := s.Watch(func(p Prefs) { got <- p })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	s.FontSize = 15
	s.Flush()
	os.WriteFile(path, []byte(`{"fontSize": 1`), 0o644)
	select {
	case p := <-got:
		t.Fatalf("changed by its own write or a broken file: %+v", p)
	case <-time.After(300 * time.Millisecond):
	}
	os.WriteFile(path, []byte(`{"fontSize": 16, "fontWeight": 500, "vim": true, "wordWrap": true, "windows": []}`), 0o644)
	select {
	case p := <-got:
		if p.FontSize != 16 || p.FontWeight != 500 || !p.Vim || p.FontFamily != "monospace" {
			t.Fatalf("prefs %+v", p)
		}
		gen := s.WrapGen
		if old := s.SetPrefs(p); old.FontSize != 15 || s.WrapGen != gen+1 || len(s.Windows) != 1 {
			t.Fatalf("old %+v, wrap gen %d, windows %d", old, s.WrapGen, len(s.Windows))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no change seen")
	}
}

// The themes were appearances once: a file of then keeps its theme.
func TestMigrateTheme(t *testing.T) {
	for _, theme := range []string{"matrix", "claude"} {
		path := filepath.Join(t.TempDir(), "settings.json")
		os.WriteFile(path, []byte(`{"appearance":"`+theme+`"}`), 0o644)
		if s := Load(path); s.Theme != theme || s.Appearance != "" {
			t.Errorf("theme %q, appearance %q", s.Theme, s.Appearance)
		}
	}
}

// A diff shows the words that changed unless it is told otherwise, three
// unchanged lines around a change, and its sides scroll together. The
// file keeps them in their bounds.
func TestDiffPrefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := Load(path)
	if s.DiffHighlight != "" || s.DiffContext != 3 || !s.DiffSyncScroll {
		t.Fatalf("defaults %+v", s.Prefs)
	}
	os.WriteFile(path, []byte(`{"diffHighlight": "char", "diffContext": 400, "diffSyncScroll": false}`), 0o644)
	s = Load(path)
	if s.DiffHighlight != "char" || s.DiffContext != 100 || s.DiffSyncScroll {
		t.Fatalf("loaded %+v", s.Prefs)
	}
	os.WriteFile(path, []byte(`{"diffHighlight": "banana", "diffContext": -2}`), 0o644)
	s = Load(path)
	if s.DiffHighlight != "" || s.DiffContext != 0 {
		t.Fatalf("reset %+v", s.Prefs)
	}
}

// A new terminal opens in the panel unless the file says "editor".
func TestTerminalLocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	for in, want := range map[string]string{"": "", "editor": "editor", "panel": "", "editorArea": ""} {
		os.WriteFile(path, []byte(`{"terminalLocation": "`+in+`"}`), 0o644)
		if got := Load(path).TerminalLocation; got != want {
			t.Errorf("%q loaded as %q, want %q", in, got, want)
		}
	}
}

func TestScmRepositoryPreferencesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := Load(path)
	if s.ScmRepositoryOpen {
		t.Fatal("repository panel must default collapsed")
	}
	s.ScmRepositoryOpen, s.ScmRemotes = true, true
	s.Changed()
	s.Flush()
	got := Load(path)
	if !got.ScmRepositoryOpen || !got.ScmRemotes {
		t.Fatal("repository panel preferences were not saved")
	}
}

func TestSyncedPrefs(t *testing.T) {
	p := Default().Prefs
	if !p.IsDefault() {
		t.Error("the defaults are not default")
	}
	p.FontSize, p.Theme, p.ScmRemotes, p.ScmRepositoryOpen = 15, "claude", true, true
	if p.IsDefault() {
		t.Error("changed preferences are default")
	}
	m := p.Synced()
	if _, ok := m["scmRemotes"]; ok {
		t.Error("scmRemotes syncs")
	}
	if string(m["fontSize"]) != "15" || string(m["theme"]) != `"claude"` {
		t.Errorf("synced = %s", m)
	}
	// Another machine's: the font size is out of range, and the panels of
	// source control stay as this machine has them.
	m["fontSize"] = json.RawMessage("40")
	delete(m, "theme")
	q, err := p.WithSynced(m)
	if err != nil {
		t.Fatal(err)
	}
	if q.FontSize != 28 || q.Theme != "" || !q.ScmRemotes || !q.ScmRepositoryOpen || !q.AutoSave {
		t.Errorf("prefs = %+v", q)
	}
	if !slices.Contains(PrefKeys, "fontSize") || !slices.Contains(PrefKeys, "locale") {
		t.Errorf("PrefKeys = %v", PrefKeys)
	}
}

// TestSyncIgnoredPrefs: what the person keeps on the machine stays as it
// has it, and syncIgnored itself does not sync.
func TestSyncIgnoredPrefs(t *testing.T) {
	p := Default().Prefs
	p.FontSize, p.Vim, p.SyncIgnored = 18, true, []string{"fontSize", "vim", "keybindings.json", "nonsense"}
	keys, keybindings := p.SyncKept()
	if !slices.Equal(keys, []string{"fontSize", "vim"}) || !keybindings {
		t.Errorf("kept %v, keybindings %v", keys, keybindings)
	}
	all := Prefs{SyncIgnored: []string{SyncSettings}}
	if keys, keybindings := all.SyncKept(); len(keys) < 10 || slices.Contains(keys, "scmRemotes") || keybindings {
		t.Errorf("settings.json keeps %v, keybindings %v", keys, keybindings)
	}
	if got := all.SyncIgnoring(SyncKeybindings, true); !slices.Equal(got, []string{SyncSettings, SyncKeybindings}) {
		t.Errorf("ignoring = %v", got)
	}
	if got := all.SyncIgnoring(SyncSettings, false); got != nil {
		t.Errorf("not ignoring = %v", got)
	}
	if _, ok := p.Synced()["syncIgnored"]; ok {
		t.Error("syncIgnored syncs")
	}
	q, err := p.WithSynced(map[string]json.RawMessage{"fontSize": json.RawMessage("12"), "theme": json.RawMessage(`"qoder"`)})
	if err != nil {
		t.Fatal(err)
	}
	if q.FontSize != 18 || !q.Vim || q.Theme != "qoder" || len(q.SyncIgnored) != 4 {
		t.Errorf("prefs = %+v", q)
	}
}

func TestOnPrefsWritten(t *testing.T) {
	s := Load(filepath.Join(t.TempDir(), "settings.json"))
	written := make(chan struct{}, 4)
	s.OnPrefsWritten(func() { written <- struct{}{} })
	s.Flush()
	<-written
	// The state only.
	s.AddRecent("/tmp")
	s.Flush()
	s.Vim = true
	s.Flush()
	<-written
	select {
	case <-written:
		t.Error("a change of the state told of the preferences")
	default:
	}
}

// TestAnOlderWriteNeverLandsLast: a write of an older encoding, as a
// timer that fired as Flush ran, leaves the newer one's file.
func TestAnOlderWriteNeverLandsLast(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := Load(path)
	s.FontSize = 14
	old, oldState := s.Encode()
	s.FontSize = 15
	s.Flush()
	s.write(0, old, oldState)
	if p, ok := s.edited(); ok {
		t.Fatalf("the file is not the settings' own: %+v", p)
	}
	q := Load(path)
	if q.FontSize != 15 {
		t.Errorf("font size %v, want 15", q.FontSize)
	}
	if tmps, _ := filepath.Glob(path + ".*.tmp"); len(tmps) != 0 {
		t.Errorf("left %v", tmps)
	}
}

// TestEdited: a save of the file is the person's once, until it changes
// again.
func TestEdited(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := Load(path)
	s.Flush()
	if _, ok := s.Edited(); ok {
		t.Fatal("its own write is an edit")
	}
	os.WriteFile(path, []byte(`{"fontSize": 18}`), 0o644)
	if p, ok := s.Edited(); !ok || p.FontSize != 18 {
		t.Fatalf("edit %v, %v", p.FontSize, ok)
	}
	if _, ok := s.Edited(); ok {
		t.Error("an edit taken is an edit again")
	}
}
