package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func byID(ss []Session) map[string]Session {
	m := map[string]Session{}
	for _, s := range ss {
		m[s.ID] = s
	}
	return m
}

func TestScanReadsEachAgent(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".claude/projects/-p/c1.jsonl"),
		`{"type":"mode","mode":"normal"}
{"type":"user","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>x</local-command-caveat>"},"cwd":"/p"}
{"type":"user","message":{"role":"user","content":"fix   the\nbug"},"cwd":"/p"}
{"type":"ai-title","aiTitle":"Bug fix"}
`)
	write(t, filepath.Join(home, ".claude-work/projects/-q/c2.jsonl"),
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]},"cwd":"/q"}
{"type":"ai-title","aiTitle":"Agent's"}
{"type":"custom-title","customTitle":"Mine"}
`)
	// A transcript of nothing asked is no session.
	write(t, filepath.Join(home, ".claude/projects/-p/empty.jsonl"), `{"type":"mode","mode":"normal"}`+"\n")
	// Nor is one of a session's helpers.
	write(t, filepath.Join(home, ".claude/projects/-p/c1/subagents/a.jsonl"), `{"type":"user","message":{"content":"x"},"cwd":"/p"}`+"\n")
	write(t, filepath.Join(home, ".qoder/projects/-r/q1.jsonl"),
		`{"type":"workspace-directories","directories":["/r"]}
{"type":"user","message":{"role":"user","content":"hi"}}
`)
	write(t, filepath.Join(home, ".codex/sessions/2026/11/09/rollout-2026-11-09T20-46-48-x1.jsonl"),
		`{"type":"session_meta","payload":{"id":"x1","cwd":"/s","source":"cli"}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>"},{"type":"input_text","text":"analyze it"}]}}
`)
	write(t, filepath.Join(home, ".codex/sessions/2026/11/09/rollout-2026-11-09T20-46-48-x2.jsonl"),
		`{"type":"session_meta","payload":{"id":"x2","cwd":"/s","source":"exec"}}`+"\n")
	write(t, filepath.Join(home, ".codex/session_index.jsonl"),
		`{"id":"x1","thread_name":"First"}
{"id":"x1","thread_name":"Renamed"}
`)
	write(t, filepath.Join(home, ".grok/sessions/%2Ft/g1/summary.json"),
		`{"info":{"id":"g1","cwd":"/t"},"generated_title":"Grok's","last_active_at":"2026-11-11T04:27:35Z"}`)
	write(t, filepath.Join(home, ".grok/sessions/%2Ft/g2/summary.json"),
		`{"info":{"id":"g2","cwd":"/t"},"generated_title":"Cron","session_kind":"headless"}`)

	e := &Env{Home: home}
	ss := byID(NewScanner().Scan(e))
	if len(ss) != 5 {
		t.Fatalf("sessions %v, want c1 c2 q1 x1 g1", ss)
	}
	want := map[string]Session{
		"c1": {Harness: Claude, Root: filepath.Join(home, ".claude"), Title: "Bug fix", Prompt: "fix the bug", Dir: "/p"},
		"c2": {Harness: Claude, Root: filepath.Join(home, ".claude-work"), Title: "Mine", Prompt: "hello", Dir: "/q"},
		"q1": {Harness: Qoder, Prompt: "hi", Dir: "/r"},
		"x1": {Harness: Codex, Title: "Renamed", Prompt: "analyze it", Dir: "/s"},
		"g1": {Harness: Grok, Title: "Grok's", Dir: "/t"},
	}
	for id, w := range want {
		s := ss[id]
		if s.Harness != w.Harness || s.Root != w.Root || s.Title != w.Title || s.Prompt != w.Prompt || s.Dir != w.Dir {
			t.Errorf("%s = %+v, want %+v", id, s, w)
		}
	}
	if got := ss["g1"].Updated; !got.Equal(time.Date(2026, 11, 11, 4, 27, 35, 0, time.UTC)) {
		t.Errorf("g1 updated %v", got)
	}
}

func TestScanKeepsTitlesOfGrownTranscripts(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude/projects/-p/c1.jsonl")
	head := `{"type":"user","message":{"content":"go"},"cwd":"/p"}` + "\n" + `{"type":"ai-title","aiTitle":"Early"}` + "\n"
	write(t, path, head)
	sc := NewScanner()
	e := &Env{Home: home}
	if s := sc.Scan(e)[0]; s.Title != "Early" {
		t.Fatalf("title %q", s.Title)
	}
	// Grown past what is read of its start and its end, with no title in
	// either, it keeps the title it had.
	pad := `{"type":"assistant","message":{"content":"` + strings.Repeat("x", 1000) + `"}}` + "\n"
	write(t, path, `{"type":"user","message":{"content":"go"},"cwd":"/p"}`+"\n"+strings.Repeat(pad, (headMax+tailMax)/1000+10))
	if s := sc.Scan(e)[0]; s.Title != "Early" || s.Prompt != "go" {
		t.Fatalf("grown = %+v", s)
	}
	// A title at its end is read.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"ai-title","aiTitle":"Late"}` + "\n")
	f.Close()
	if s := sc.Scan(e)[0]; s.Title != "Late" {
		t.Fatalf("title %q, want Late", s.Title)
	}
}

func TestClaudeRoots(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{".claude", ".claude-b", ".claude-a", "other", ".claude-"} {
		os.MkdirAll(filepath.Join(home, d), 0o755)
	}
	write(t, filepath.Join(home, ".claude-file"), "")
	e := &Env{Home: home, ConfigDir: filepath.Join(home, "other"), ClaudeDirs: []string{"~/other", "~/missing", " "}}
	got := e.ClaudeRoots()
	want := []string{".claude", ".claude-a", ".claude-b", "other"}
	if len(got) != len(want) {
		t.Fatalf("roots %v", got)
	}
	for i, w := range want {
		if got[i] != filepath.Join(home, w) {
			t.Errorf("root %d = %s, want %s", i, got[i], w)
		}
	}
}

func TestResumeCommand(t *testing.T) {
	e := &Env{Home: "/h"}
	for _, c := range []struct {
		s    Session
		want string
	}{
		{Session{Harness: Claude, Root: "/h/.claude", ID: "a1"}, "claude --resume a1"},
		{Session{Harness: Claude, Root: "/h/.claude-w x", ID: "a1"}, "CLAUDE_CONFIG_DIR='/h/.claude-w x' claude --resume a1"},
		{Session{Harness: Qoder, ID: "q"}, "qodercli -r q"},
		{Session{Harness: Codex, ID: "c"}, "codex resume c"},
		{Session{Harness: Grok, ID: "g"}, "grok --resume g"},
	} {
		if got := e.ResumeCommand(c.s); got != c.want {
			t.Errorf("%v: %q, want %q", c.s, got, c.want)
		}
	}
}

func TestMatchesAndIn(t *testing.T) {
	s := Session{Harness: Codex, Title: "Fix Login", Dir: "/w/app", ID: "abc"}
	for q, want := range map[string]bool{"": true, "login fix": true, "codex": true, "app": true, "ABC": true, "logout": false} {
		if s.Matches(q) != want {
			t.Errorf("Matches(%q) = %v", q, !want)
		}
	}
	if !s.In("/w/app") || !s.In("/w") || s.In("/w/ap") || s.In("") {
		t.Error("In")
	}
}

func TestClaudeSessionOfPID(t *testing.T) {
	home := t.TempDir()
	a, b := filepath.Join(home, ".claude"), filepath.Join(home, ".claude-b")
	write(t, filepath.Join(b, "sessions/42.json"), `{"pid":42,"sessionId":"s42","cwd":"/p"}`)
	id, root, ok := ClaudeSessionOfPID([]string{a, b}, 42)
	if !ok || id != "s42" || root != b {
		t.Fatalf("%q %q %v", id, root, ok)
	}
	if _, _, ok := ClaudeSessionOfPID([]string{a, b}, 7); ok {
		t.Fatal("found a session of a pid with none")
	}
}

func TestHarnessOfApp(t *testing.T) {
	for app, want := range map[string]Harness{"claude-code": Claude, "qodercli": Qoder, "codex": Codex, "grok": Grok} {
		if h, ok := HarnessOfApp(app); !ok || h != want {
			t.Errorf("%s: %v %v", app, h, ok)
		}
	}
	if _, ok := HarnessOfApp("zsh"); ok {
		t.Error("zsh")
	}
}

// Each agent known has its names, a mark and a way to resume, and is
// found by the name it gives itself; another is named by its id.
func TestKnownAgents(t *testing.T) {
	e := &Env{Home: "/h"}
	for _, ag := range Known {
		if ag.Name == "" || ag.App == "" || ag.Icon == "" {
			t.Errorf("%q lacks a name, an app or a mark: %+v", ag.ID, ag)
		}
		if h, ok := HarnessOfApp(ag.App); !ok || h != ag.ID {
			t.Errorf("HarnessOfApp(%q) = %q, %v", ag.App, h, ok)
		}
		if e.ResumeCommand(Session{Harness: ag.ID, ID: "x"}) == "" {
			t.Errorf("%q resumes with no command", ag.ID)
		}
	}
	if h := Harness("aider"); h.Name() != "aider" || h.Agent().Icon != "" || e.ResumeCommand(Session{Harness: h, ID: "x"}) != "" {
		t.Errorf("an agent not known: %+v", h.Agent())
	}
}
