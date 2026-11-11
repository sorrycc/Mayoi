package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mayoi/internal/agents"
	"mayoi/internal/settings"
)

// piPlugin adds two agents: one whose sessions its function lists, one
// of the files a glob matches. broken adds one whose function throws.
var piPlugins = map[string]map[string]string{
	"pi": {
		"plugin.json": `{"name": "pi", "main": "main.ts", "contributes": {"agents": [
			{"id": "pi.agent", "name": "Pi", "icon": "sparkles", "tint": "#ff00aa", "resume": "pi --resume {id}"},
			{"id": "pi.files", "name": "Pi Files", "icon": "<circle cx=\"12\" cy=\"12\" r=\"9\"/>", "resume": "pi-files {id}",
			 "sessions": {"glob": "~/.pi/*.json", "id": "id", "title": "name", "folder": "cwd", "updated": "at"}}]}}`,
		"main.ts": `import { agents } from "mayoi";
agents.register("pi.agent", async () => [
  { id: "p1", title: "Fix the pie", folder: "FOLDER", updated: Date.now(), prompt: "fix it" },
  { title: "no id, left out" },
]);`,
	},
	"broken": {
		"plugin.json": `{"name": "broken", "main": "main.ts", "contributes": {"agents": [{"id": "broken.agent", "name": "Broken", "resume": "b {id}"}]}}`,
		"main.ts":     `import { agents } from "mayoi"; agents.register("broken.agent", () => { throw new Error("no sessions here"); });`,
	},
}

// The agents of plugins list their sessions in the Sessions view with
// their icons, and resume as those Mayoi knows do; one that fails says so
// in its plugin's log, once, and the others still show. Turning a plugin
// off takes its agents and their sessions, which leave as they look.
func TestPluginAgentSessions(t *testing.T) {
	skipOnWindows(t, "the sessions' folders are POSIX paths")
	hs := newHarness(t, true)
	a := hs.a
	home := agentHome(t, hs.root)
	os.MkdirAll(filepath.Join(home, ".pi"), 0o755)
	os.WriteFile(filepath.Join(home, ".pi", "f1.json"), []byte(`{"id":"f1","name":"Bake the files","cwd":"`+hs.root+`","at":"2026-11-01T10:00:00Z"}`), 0o644)
	files := map[string]map[string]string{"broken": piPlugins["broken"], "pi": {
		"plugin.json": piPlugins["pi"]["plugin.json"],
		"main.ts":     strings.Replace(piPlugins["pi"]["main.ts"], "FOLDER", hs.root, 1),
	}}
	h := withPlugins(hs, files)
	a.showSessions()
	has := func(ids ...string) bool {
		got := " " + sessionIDs(a.sessionRows()) + " "
		for _, id := range ids {
			if !strings.Contains(got, " "+id+" ") {
				return false
			}
		}
		return true
	}
	hs.until("the plugins' sessions", func() bool { return has("here", "p1", "f1") })

	byID := map[string]agents.Session{}
	for _, s := range a.sessionRows() {
		byID[s.ID] = s
	}
	if s := byID["p1"]; s.Harness != "pi.agent" || s.Title != "Fix the pie" || s.Prompt != "fix it" || s.Updated.IsZero() {
		t.Errorf("the session a plugin listed: %+v", s)
	}
	if s := byID["f1"]; s.Harness != "pi.files" || s.Label() != "Bake the files" || s.Updated.Year() != 2026 {
		t.Errorf("the session of a plugin's files: %+v", s)
	}
	if cmd := a.agentEnv().ResumeCommand(byID["p1"]); cmd != "pi --resume p1" {
		t.Errorf("resume command %q", cmd)
	}
	p := a.pal
	if icon, tint := agentIcon(p, "pi.agent"); icon != iconSparkles || tint != langColor(p, "#ff00aa", "#ff00aa") {
		t.Errorf("Pi's icon %p in %v, want the sparkles in its tint", icon, tint)
	}
	if icon, _ := agentIcon(p, "pi.files"); icon == iconAgent {
		t.Error("the icon of an SVG's shapes did not parse")
	}
	if got := a.sessionMeta(byID["p1"]); !strings.HasPrefix(got, "Pi") {
		t.Errorf("a plugin session's meta %q names no agent", got)
	}

	failures := func() int {
		n := 0
		for _, l := range h.reg.Plugin("broken").Logs() {
			if strings.Contains(l.Message, "could not list the sessions of Broken") && strings.Contains(l.Message, "no sessions here") {
				n++
			}
		}
		return n
	}
	a.readSessionsNow()
	hs.until("a reading again", func() bool { return failures() > 0 && !a.sessions.read.Running() })
	a.readSessionsNow()
	hs.settle()
	if n := failures(); n != 1 {
		t.Errorf("the failure was logged %d times, want once", n)
	}

	w := &prefsWindow{m: &mayoi{s: a.settings}}
	w.setPluginSetting("pi", "enabled", false, true)
	// Frames, with no time passing, until the reading without them shows.
	for deadline := time.Now().Add(5 * time.Second); has("p1") || has("f1"); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the plugin's sessions going")
		}
		hs.tt.Frame()
	}
	hs.tt.Frame()
	if _, ok := agents.HarnessOfApp("pi.agent"); ok {
		t.Error("the agent of a plugin turned off stays")
	}
	going := 0
	for _, l := range a.sessions.shown {
		if !l.gone.IsZero() && l.s.Harness == "pi.agent" {
			going++
			if look := a.sessionLook(l); look.icon != iconSparkles {
				t.Errorf("the row of %s lost its agent's icon as it goes", l.s.ID)
			}
		}
	}
	if going == 0 {
		t.Errorf("the plugin's sessions left without going: %d lines shown", len(a.sessions.shown))
	}
	hs.until("the plugin's sessions gone", func() bool { return len(a.sessions.shown) == len(a.sessions.lines) })
	if !has("here") {
		t.Error("the sessions of Claude Code went with the plugin")
	}
}

// A tab resumed is titled after its session until the agent names it,
// whatever the shell calls it meanwhile; a terminal of herdr-core keeps
// its agent and session for the next start.
func TestResumedSessionTabTitleAndAgentKept(t *testing.T) {
	s := &session{title: "zsh", sessionTitle: "Fix the build", program: "claude"}
	for _, title := range []string{"zsh", "~/src/app", "/tmp", "me@host: ~/src", "claude --resume abc", "CLAUDE_CONFIG_DIR=x claude --resume abc"} {
		s.retitle(title)
		if s.label() != "Fix the build" {
			t.Errorf("titled %q, the tab is %q; want the session's", title, s.label())
		}
	}
	s.retitle("✳ Fixing the build")
	if s.label() != "✳ Fixing the build" {
		t.Errorf("named by the agent, the tab is %q", s.label())
	}
	s.retitle("zsh")
	if s.label() != "zsh" {
		t.Errorf("the session's title came back: %q", s.label())
	}
	if got := programOf("CLAUDE_CONFIG_DIR=/x claude --resume a"); got != "claude" {
		t.Errorf("programOf %q", got)
	}

	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	ts := hcSession(t, a, "t1")
	ts.agent, ts.resumed = agents.Qoder, "qoder\x00\x00q1"
	a.terminalInColumn(ts, false)
	a.rememberTabs()
	var kept *settings.Tab
	for _, col := range a.window.Columns {
		for i := range col.Tabs {
			if col.Tabs[i].HC == "t1" {
				kept = &col.Tabs[i]
			}
		}
	}
	if kept == nil || kept.Agent != "qoder" || kept.Session != ts.resumed {
		t.Errorf("the terminal's tab kept %+v, want its agent and session", kept)
	}
}
