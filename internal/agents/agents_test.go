package agents

import (
	"context"
	"errors"
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
	write(t, filepath.Join(home, ".pi/agent/sessions/--u--/2026-11-14T09-00-00-000Z_p1.jsonl"),
		`{"type":"session","version":3,"id":"p1","cwd":"/u"}
{"type":"message","id":"a1","parentId":null,"message":{"role":"user","content":"pi it"}}
{"type":"session_info","name":"First"}
{"type":"session_info","name":"Pi's"}
`)
	// A transcript of nothing asked is no session.
	write(t, filepath.Join(home, ".pi/agent/sessions/--u--/empty.jsonl"),
		`{"type":"session","version":3,"id":"pe","cwd":"/u"}`+"\n")

	e := &Env{Home: home}
	ss := byID(NewScanner().Scan(e))
	if len(ss) != 6 {
		t.Fatalf("sessions %v, want c1 c2 q1 x1 g1 p1", ss)
	}
	want := map[string]Session{
		"c1": {Harness: Claude, Root: filepath.Join(home, ".claude"), Title: "Bug fix", Prompt: "fix the bug", Dir: "/p"},
		"c2": {Harness: Claude, Root: filepath.Join(home, ".claude-work"), Title: "Mine", Prompt: "hello", Dir: "/q"},
		"q1": {Harness: Qoder, Prompt: "hi", Dir: "/r"},
		"x1": {Harness: Codex, Title: "Renamed", Prompt: "analyze it", Dir: "/s"},
		"g1": {Harness: Grok, Title: "Grok's", Dir: "/t"},
		"p1": {Harness: Pi, Title: "Pi's", Prompt: "pi it", Dir: "/u"},
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

// A session's transcript is the file read, or Grok's chat beside its
// summary, none for one of Grok of no chat, and what a List says.
func TestScanFindsTranscripts(t *testing.T) {
	t.Cleanup(func() { Use(nil) })
	home := t.TempDir()
	claude := filepath.Join(home, ".claude/projects/-p/c1.jsonl")
	write(t, claude, `{"type":"user","message":{"role":"user","content":"hi"},"cwd":"/p"}`+"\n")
	write(t, filepath.Join(home, ".grok/sessions/%2Ft/g1/summary.json"), `{"info":{"id":"g1","cwd":"/t"},"generated_title":"Chat"}`)
	chat := filepath.Join(home, ".grok/sessions/%2Ft/g1/chat_history.jsonl")
	write(t, chat, `{}`+"\n")
	write(t, filepath.Join(home, ".grok/sessions/%2Ft/g2/summary.json"), `{"info":{"id":"g2","cwd":"/t"},"generated_title":"No chat"}`)
	Use([]Agent{{ID: "js.agent", List: func(context.Context) ([]Session, error) {
		return []Session{{ID: "j1", Transcript: "/made/up.jsonl"}}, nil
	}}})
	ss := byID(NewScanner().Scan(&Env{Home: home}))
	for id, want := range map[string]string{"c1": claude, "g1": chat, "g2": "", "j1": "/made/up.jsonl"} {
		if got := ss[id].Transcript; got != want {
			t.Errorf("%s transcript %q, want %q", id, got, want)
		}
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

// An agent added is read as those known are: of files a glob matches,
// or of what its List gives; one that fails keeps what it gave before,
// and one gone takes its sessions with it.
func TestUseAddsAgents(t *testing.T) {
	t.Cleanup(func() { Use(nil) })
	home := t.TempDir()
	write(t, filepath.Join(home, ".pi", "2026", "a.json"), `{"meta":{"cwd":"/work/a"},"name":"Fix the build","messages":[{"content":"fix it"}],"at":1700000000000}`)
	write(t, filepath.Join(home, ".pi", "2026", "b.jsonl"), `{"id":"b-1","cwd":"/work/b"}`+"\n"+`{"prompt":[{"type":"text","text":"first ask"}]}`+"\n"+`{"title":"old"}`+"\n"+`{"title":"new","at":"2026-11-01T10:00:00Z"}`+"\n")
	write(t, filepath.Join(home, ".pi", "2026", "skip.txt"), `x`)
	pi := WithPattern(Agent{ID: "pi.agent", Name: "Pi", Resume: ResumeTemplate("pi -r {id} --title {title}")},
		Pattern{Glob: "~/.pi/**/*.json", Format: "json", Folder: "meta.cwd", Title: "name", Prompt: "messages.0.content", Updated: "at"})
	pl := WithPattern(Agent{ID: "pi.lines", Name: "Pi Lines"},
		Pattern{Glob: "~/.pi/*/*.jsonl", Format: "jsonl", ID: "id", Folder: "cwd", Title: "title", Prompt: "prompt", Updated: "at"})
	fail := false
	listed := Agent{ID: "js.agent", Name: "JS", List: func(ctx context.Context) ([]Session, error) {
		if fail {
			return nil, errors.New("broken")
		}
		return []Session{{ID: "j1", Title: "from js", Dir: "/work/j", Updated: time.Unix(10, 0)}, {Title: "no id"}}, nil
	}}
	Use([]Agent{pi, pl, listed, {ID: Claude, Name: "not Claude"}})
	if Claude.Name() != "Claude Code" {
		t.Errorf("an agent added took the id of one known")
	}
	sc := NewScanner()
	e := &Env{Home: home}
	got := byID(sc.Scan(e))
	if a := got["a"]; a.Harness != "pi.agent" || a.Dir != "/work/a" || a.Title != "Fix the build" || a.Prompt != "fix it" || !a.Updated.Equal(time.UnixMilli(1700000000000)) {
		t.Errorf("session of a JSON file: %+v", a)
	}
	if b := got["b-1"]; b.Harness != "pi.lines" || b.Dir != "/work/b" || b.Title != "new" || b.Prompt != "first ask" || b.Updated.Year() != 2026 {
		t.Errorf("session of a file of lines: %+v", b)
	}
	if j := got["j1"]; j.Harness != "js.agent" || j.Title != "from js" || len(got) != 3 {
		t.Errorf("sessions listed: %+v", got)
	}
	if cmd := e.ResumeCommand(got["a"]); cmd != "pi -r a --title 'Fix the build'" {
		t.Errorf("resume command %q", cmd)
	}
	fail = true
	if got := byID(sc.Scan(e)); got["j1"].ID == "" {
		t.Errorf("a List that failed lost what it listed before")
	}
	Use([]Agent{pi})
	if got := byID(sc.Scan(e)); len(got) != 1 || got["a"].ID == "" {
		t.Errorf("the agents gone stay: %+v", got)
	}
}

// A List slower than ListWait does not hold the scan up: what it lists
// comes with the next, and late says so.
func TestSlowListComesLate(t *testing.T) {
	t.Cleanup(func() { Use(nil) })
	release := make(chan struct{})
	Use([]Agent{{ID: "slow.agent", Name: "Slow", List: func(ctx context.Context) ([]Session, error) {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return []Session{{ID: "s1", Title: "slow"}}, nil
	}}})
	sc := NewScanner()
	late := make(chan struct{}, 1)
	sc.OnLate(func() { late <- struct{}{} })
	start := time.Now()
	if got := sc.Scan(&Env{Home: t.TempDir()}); len(got) != 0 {
		t.Errorf("sessions of a List that has not ended: %+v", got)
	}
	if d := time.Since(start); d > ListWait+time.Second {
		t.Errorf("the scan waited %v", d)
	}
	close(release)
	select {
	case <-late:
	case <-time.After(5 * time.Second):
		t.Fatal("late was not called")
	}
	if got := sc.Scan(&Env{Home: t.TempDir()}); len(got) != 1 || got[0].Harness != "slow.agent" {
		t.Errorf("the next scan has %+v", got)
	}
}

// A Pi session named late in a long transcript takes that name.
func TestReadPiNameAtEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	pad := `{"type":"message","message":{"role":"assistant","content":"` + strings.Repeat("x", 1000) + `"}}` + "\n"
	write(t, path, `{"type":"session","version":3,"id":"p1","cwd":"/u"}`+"\n"+
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"go"}]}}`+"\n"+
		`{"type":"session_info","name":"Early"}`+"\n"+
		strings.Repeat(pad, headMax/1000+10)+
		`{"type":"session_info","name":"Late"}`+"\n")
	s, ok := readPi(path)
	if !ok || s.ID != "p1" || s.Dir != "/u" || s.Prompt != "go" || s.Title != "Late" {
		t.Fatalf("readPi = %+v, %v", s, ok)
	}
}
