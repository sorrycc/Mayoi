package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"

	"mayoi/internal/agents"
)

// agentHome makes a home folder of a session of Claude Code in dir, and
// one in another folder, and has the tests read it.
func agentHome(t *testing.T, dir string) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	for id, cwd := range map[string]string{"here": dir, "there": "/elsewhere"} {
		p := filepath.Join(home, ".claude", "projects", "p-"+id, id+".jsonl")
		os.MkdirAll(filepath.Dir(p), 0o755)
		line := `{"type":"user","message":{"content":"fix ` + id + `"},"cwd":"` + filepath.ToSlash(cwd) + `"}` + "\n"
		if err := os.WriteFile(p, []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func sessionIDs(ss []agents.Session) string {
	ids := make([]string, len(ss))
	for i, s := range ss {
		ids[i] = s.ID
	}
	return strings.Join(ids, " ")
}

// The view lists the sessions of the window's folder, or of every
// folder, as a query narrows them.
func TestSessionsViewListsTheFolders(t *testing.T) {
	skipOnWindows(t, "the sessions' folders are POSIX paths")
	hs := newHarness(t, true)
	a := hs.a
	agentHome(t, hs.root)
	a.showSessions()
	hs.settle()
	if !a.sessions.loaded {
		t.Fatal("the sessions were not read")
	}
	if got := sessionIDs(a.sessionRows()); got != "here" {
		t.Errorf("this folder's sessions %q, want here", got)
	}
	a.sessions.all = 1
	if got := sessionIDs(a.sessionRows()); !strings.Contains(got, "here") || !strings.Contains(got, "there") {
		t.Errorf("every folder's sessions %q", got)
	}
	a.sessions.query = "FIX there"
	if got := sessionIDs(a.sessionRows()); got != "there" {
		t.Errorf("sessions of the query %q, want there", got)
	}
}

// A session whose folder is gone says so, and starts no terminal.
func TestResumeSessionOfAFolderGone(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	n := len(a.terms)
	a.resumeSession(agents.Session{Harness: agents.Codex, ID: "x", Dir: filepath.Join(hs.root, "gone")})
	if len(a.terms) != n {
		t.Error("a terminal started")
	}
	if len(a.notes) == 0 || !strings.Contains(a.notes[len(a.notes)-1].message, "gone") {
		t.Errorf("notes %+v", a.notes)
	}
}

// hc says which terminals run which sessions: as the agent reported it,
// or for Claude Code, as its process says in its config folder.
func TestHCAgentsTiesTerminalsToSessions(t *testing.T) {
	skipOnWindows(t, "herdr has no hc for Windows")
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "sessions"), 0o755)
	os.WriteFile(filepath.Join(root, "sessions", "42.json"), []byte(`{"pid":42,"sessionId":"c1"}`), 0o644)
	useFakeHC(t, func(args []string) ([]byte, error) {
		switch {
		case args[0] == "ls":
			return []byte(`{"terminals":[
				{"address":"t1","agent":"claude-code","cwd":"/p","state":"live","status":"working"},
				{"address":"t2","agent":"codex","cwd":"/q","state":"live","status":"done"},
				{"address":"t3","agent":"zsh","cwd":"/r","state":"live"},
				{"address":"t4","agent":"qodercli","cwd":"/s","state":"live"},
				{"address":"m/t5","agent":"claude-code","cwd":"/t","state":"live"},
				{"address":"t6","agent":"claude-code","cwd":"/u","state":"finished"}]}`), nil
		case args[0] == "get" && args[2] == "t1":
			return []byte(`{"foreground":{"pid":42},"status":{"app":"claude-code","state":"blocked"}}`), nil
		case args[0] == "get" && args[2] == "t2":
			return []byte(`{"agent_session":{"agent":"codex","id":"x1"},"foreground":{"pid":7}}`), nil
		case args[0] == "get":
			return []byte(`{"foreground":{"pid":9}}`), nil
		}
		return nil, errors.New("unexpected")
	})
	got := hcAgents([]string{root})
	if len(got) != 2 {
		t.Fatalf("running %+v, want c1 and x1", got)
	}
	if r := got[liveKey(agents.Claude, "c1")]; r.addr != "t1" || r.state != terminal.StatusBlocked {
		t.Errorf("c1 %+v", r)
	}
	if r := got[liveKey(agents.Codex, "x1")]; r.addr != "t2" || r.state != terminal.StatusDone || r.cwd != "/q" {
		t.Errorf("x1 %+v", r)
	}
}

// Resuming a session that runs in herdr-core shows its terminal, rather
// than starting one.
func TestResumeSessionShowsItsTerminal(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	s := hcSession(t, a, "t1")
	other := hcSession(t, a, "t2")
	a.panelTerm = other
	a.sessions.running = map[string]hcAgent{liveKey(agents.Claude, "c1"): {addr: "t1", cwd: hs.root, harness: agents.Claude}}
	n := len(a.terms)
	a.resumeSession(agents.Session{Harness: agents.Claude, Root: "/h/.claude", ID: "c1", Dir: hs.root})
	if len(a.terms) != n || a.panelTerm != s {
		t.Errorf("the panel shows %v of %d terminals, want t1 of %d", a.panelTerm.hc, len(a.terms), n)
	}
}
