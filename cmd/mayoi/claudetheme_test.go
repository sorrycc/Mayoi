package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/claudetheme"
	"mayoi/internal/settings"
)

// The theme of Claude Code of a palette: its base as the palette is dark,
// each color as #rrggbb, the text reading on the terminal's background.
func TestClaudeThemeOfPalette(t *testing.T) {
	hex := regexp.MustCompile(`^#[0-9a-f]{6}$`)
	for _, c := range []struct {
		theme string
		dark  bool
		base  string
	}{{"", false, "light"}, {"", true, "dark"}, {themeMatrix, false, "dark"}, {themeClaude, false, "light"}, {themeQoder, true, "dark"}} {
		p := paletteFor(c.theme, c.dark)
		th := claudeThemeOf(p, "")
		if th.Base != c.base || th.Name != "Mayoi" {
			t.Errorf("%q dark=%v: base %q", c.theme, c.dark, th.Base)
		}
		for k, v := range th.Overrides {
			if !hex.MatchString(v) {
				t.Errorf("%q: %s is %q", c.theme, k, v)
			}
		}
		bg := ui.Hex(th.Overrides["background"])
		if bg != termTheme(p).Background {
			t.Errorf("%q: background %s, the terminal's %v", c.theme, th.Overrides["background"], termTheme(p).Background)
		}
		for k, least := range map[string]float64{"text": 7, "claude": 4.5, "error": 4.5, "success": 4.5, "warning": 4.5, "inactive": 4.5, "subtle": 3} {
			if r := contrast(ui.Hex(th.Overrides[k]), bg); r < least-0.05 {
				t.Errorf("%q dark=%v: %s reads at %.2f", c.theme, c.dark, k, r)
			}
		}
	}
	// A colorblind-friendly theme keeps its base and its colors of what
	// changed.
	th := claudeThemeOf(paletteFor("", true), "light-daltonized")
	if th.Base != "dark-daltonized" {
		t.Errorf("base %q", th.Base)
	}
	for _, k := range []string{"success", "error", "warning", "diffAdded", "diffRemoved"} {
		if _, ok := th.Overrides[k]; ok {
			t.Errorf("%s overridden over a daltonized base", k)
		}
	}
	if th.Overrides["text"] == "" {
		t.Error("the text not overridden")
	}
}

// A plugin's theme gives Claude Code its colors, as Mayoi's terminal has
// them.
func TestClaudeThemeOfPluginTheme(t *testing.T) {
	hs := newHarness(t, false)
	withPlugins(hs, map[string]map[string]string{"night": {
		"plugin.json": nightPlugin, "night.json": nightTheme, "paper.json": paperTheme, "d2.json": d2Snippets,
	}})
	th := claudeThemeOf(paletteFor("night.dark", false), "")
	if th.Base != "dark" || th.Overrides["error"] != "#ff5555" {
		t.Errorf("night: %s %s", th.Base, th.Overrides["error"])
	}
	if th := claudeThemeOf(paletteFor("night.paper", true), ""); th.Base != "light" {
		t.Errorf("paper: %s", th.Base)
	}
}

// claudeTest is a sync of its own over a temporary config folder of
// Claude Code, which never touches the person's.
type claudeTest struct {
	t       *testing.T
	m       *mayoi
	store   claudetheme.Store
	version string
	err     error
}

func newClaudeTest(t *testing.T) *claudeTest {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "claude"))
	ct := &claudeTest{t: t, version: "2.1.330 (Claude Code)\n"}
	ct.m = &mayoi{s: settings.Load(filepath.Join(dir, "mayoi", "settings.json"))}
	store := claudeThemeStore(filepath.Join(dir, "mayoi", "settings.json"))
	ct.store, _ = store()
	ct.install(store)
	oldSync, oldDark := claudeSync, systemDark
	systemDark = func() bool { return false }
	t.Cleanup(func() { claudeSync, systemDark = oldSync, oldDark })
	return ct
}

// install starts a sync anew, as a restart does.
func (ct *claudeTest) install(store func() (claudetheme.Store, bool)) {
	claudeSync = &claudeThemeSync{store: store, probe: func(ctx context.Context) (string, error) { return ct.version, ct.err }}
}

// set changes the preferences, as the settings window does, and waits for
// the sync.
func (ct *claudeTest) set(fn func(p *settings.Prefs)) {
	fn(&ct.m.s.Prefs)
	ct.m.prefsChanged()
	claudeSync.wait()
}

func (ct *claudeTest) settings() map[string]any {
	ct.t.Helper()
	data, err := os.ReadFile(filepath.Join(ct.store.ConfigDir, "settings.json"))
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		ct.t.Fatal(err)
	}
	return m
}

func (ct *claudeTest) theme() *claudetheme.Theme {
	data, err := os.ReadFile(filepath.Join(ct.store.ConfigDir, "themes", "mayoi.json"))
	if err != nil {
		return nil
	}
	var th claudetheme.Theme
	json.Unmarshal(data, &th)
	return &th
}

func (ct *claudeTest) writeSettings(s string) {
	os.MkdirAll(ct.store.ConfigDir, 0o755)
	if err := os.WriteFile(filepath.Join(ct.store.ConfigDir, "settings.json"), []byte(s), 0o644); err != nil {
		ct.t.Fatal(err)
	}
}

// Off by default, Mayoi writes nothing; on, Claude Code takes the theme
// and follows it, and the system's light or dark; after a restart it
// still does; off, Claude Code gets its theme back, every other setting
// kept.
func TestClaudeThemeSync(t *testing.T) {
	ct := newClaudeTest(t)
	ct.writeSettings(`{"model": "opus", "theme": "dark-daltonized", "env": {"A": "1"}}`)
	ct.set(func(p *settings.Prefs) {})
	if ct.theme() != nil || ct.settings()["theme"] != "dark-daltonized" {
		t.Fatal("Mayoi wrote with the setting off")
	}

	ct.set(func(p *settings.Prefs) { p.TerminalClaudeTheme = true })
	if ct.settings()["theme"] != claudetheme.Selection || ct.theme() == nil || ct.theme().Base != "light-daltonized" {
		t.Fatalf("on: %v %+v", ct.settings(), ct.theme())
	}
	if st := claudeSync.current(); st.kind != claudeThemeReplaced || st.arg != "dark-daltonized" {
		t.Errorf("status %+v", st)
	}
	if msg, failed := claudeThemeMessage(claudeSync.current()); !strings.Contains(msg, "dark-daltonized") || failed {
		t.Errorf("message %q", msg)
	}

	// The system turns dark.
	systemDark = func() bool { return true }
	ct.m.syncClaudeTheme()
	claudeSync.wait()
	if ct.theme().Base != "dark-daltonized" {
		t.Errorf("after the system turned dark: %s", ct.theme().Base)
	}
	// Another theme.
	before := ct.theme().Overrides["text"]
	ct.set(func(p *settings.Prefs) { p.Theme = themeMatrix })
	if ct.theme().Overrides["text"] == before {
		t.Error("the theme didn't follow Mayoi's")
	}
	// An appearance of the settings over the system's.
	ct.set(func(p *settings.Prefs) { p.Theme, p.Appearance = "", "light" })
	if ct.theme().Base != "light-daltonized" {
		t.Errorf("light appearance: %s", ct.theme().Base)
	}

	// A restart, which finds Mayoi's theme and keeps what it replaced.
	store := claudeThemeStore(filepath.Join(filepath.Dir(ct.store.StatePath), "settings.json"))
	ct.install(store)
	ct.m.claudeThemeSeen = false
	ct.set(func(p *settings.Prefs) {})
	if ct.settings()["theme"] != claudetheme.Selection {
		t.Fatal("a restart lost the theme")
	}

	ct.set(func(p *settings.Prefs) { p.TerminalClaudeTheme = false })
	s := ct.settings()
	if s["theme"] != "dark-daltonized" || s["model"] != "opus" || s["env"].(map[string]any)["A"] != "1" || ct.theme() != nil {
		t.Errorf("off: %v %+v", s, ct.theme())
	}
	if st := claudeSync.current(); st.kind != claudeThemeOK {
		t.Errorf("status after off %+v", st)
	}
}

// A theme chosen with /theme after Mayoi's wins, until the setting is
// turned off and on, however fast.
func TestClaudeThemeManualChoice(t *testing.T) {
	ct := newClaudeTest(t)
	ct.set(func(p *settings.Prefs) { p.TerminalClaudeTheme = true })
	if ct.settings()["theme"] != claudetheme.Selection {
		t.Fatal("not on")
	}
	ct.writeSettings(`{"theme": "light-ansi"}`)
	ct.set(func(p *settings.Prefs) { p.Theme = themeMatrix })
	if ct.settings()["theme"] != "light-ansi" || claudeSync.current().kind != claudeThemeLost {
		t.Fatalf("manual: %v %+v", ct.settings(), claudeSync.current())
	}
	if msg, failed := claudeThemeMessage(claudeSync.current()); msg == "" || failed {
		t.Errorf("message %q %v", msg, failed)
	}
	// Off and on before the sync sees the off.
	ct.m.s.Prefs.TerminalClaudeTheme = false
	ct.m.prefsChanged()
	ct.m.s.Prefs.TerminalClaudeTheme = true
	ct.m.prefsChanged()
	claudeSync.wait()
	if ct.settings()["theme"] != claudetheme.Selection {
		t.Fatalf("again: %v %+v", ct.settings(), claudeSync.current())
	}
	if st := claudeSync.current(); st.kind != claudeThemeReplaced || st.arg != "light-ansi" {
		t.Errorf("status %+v", st)
	}
	ct.set(func(p *settings.Prefs) { p.TerminalClaudeTheme = false })
	if ct.settings()["theme"] != "light-ansi" {
		t.Errorf("off: %v", ct.settings())
	}
}

// Claude Code missing, too old or hanging, a theme file of the person's
// and a settings.json that isn't JSON each say so, writing nothing.
func TestClaudeThemeFailures(t *testing.T) {
	ct := newClaudeTest(t)
	ct.err = errClaudeNotFound
	ct.set(func(p *settings.Prefs) { p.TerminalClaudeTheme = true })
	if claudeSync.current().kind != claudeThemeNotFound || ct.settings() != nil {
		t.Fatalf("not found: %+v", claudeSync.current())
	}
	if msg, failed := claudeThemeMessage(claudeSync.current()); msg == "" || !failed {
		t.Errorf("message %q", msg)
	}

	ct.err, ct.version = nil, "2.1.100 (Claude Code)"
	ct.set(func(p *settings.Prefs) { p.TerminalClaudeTheme = false })
	ct.set(func(p *settings.Prefs) { p.TerminalClaudeTheme = true })
	if st := claudeSync.current(); st.kind != claudeThemeTooOld || st.arg != "2.1.100" || ct.settings() != nil {
		t.Fatalf("too old: %+v", st)
	}

	// Hangs: the probe gives up.
	defer func(d time.Duration) { claudeProbeFor = d }(claudeProbeFor)
	claudeProbeFor = 50 * time.Millisecond
	claudeSync.probe = func(ctx context.Context) (string, error) { <-ctx.Done(); return "", ctx.Err() }
	ct.set(func(p *settings.Prefs) { p.TerminalClaudeTheme = false })
	ct.set(func(p *settings.Prefs) { p.TerminalClaudeTheme = true })
	if st := claudeSync.current(); st.kind != claudeThemeFailed || ct.settings() != nil {
		t.Fatalf("hang: %+v", st)
	}

	ct.version = "2.1.330"
	claudeSync.probe = func(ctx context.Context) (string, error) { return ct.version, nil }
	os.MkdirAll(filepath.Join(ct.store.ConfigDir, "themes"), 0o755)
	os.WriteFile(filepath.Join(ct.store.ConfigDir, "themes", "mayoi.json"), []byte(`{"name": "Mine"}`), 0o644)
	ct.set(func(p *settings.Prefs) { p.TerminalClaudeTheme = false })
	ct.set(func(p *settings.Prefs) { p.TerminalClaudeTheme = true })
	if st := claudeSync.current(); st.kind != claudeThemeTaken || ct.settings() != nil {
		t.Fatalf("taken: %+v", st)
	}
	os.Remove(filepath.Join(ct.store.ConfigDir, "themes", "mayoi.json"))

	ct.writeSettings(`{"theme": `)
	ct.set(func(p *settings.Prefs) { p.TerminalClaudeTheme = false })
	ct.set(func(p *settings.Prefs) { p.TerminalClaudeTheme = true })
	if st := claudeSync.current(); st.kind != claudeThemeMalformed || ct.theme() != nil {
		t.Fatalf("malformed: %+v", st)
	}
}

func TestClaudeVersion(t *testing.T) {
	for out, ok := range map[string]bool{"2.1.330 (Claude Code)": true, "2.1.119": true, "2.1.118 (Claude Code)": false, "3.0.0": true, "1.9.999": false} {
		v, found := claudeVersion(out)
		if !found || versionAtLeast(v, claudeThemeMin) != ok {
			t.Errorf("%q: %v %v", out, v, found)
		}
	}
	if _, found := claudeVersion("command not found"); found {
		t.Error("a version in no version")
	}
}
