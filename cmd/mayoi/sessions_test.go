package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/agents"
	"mayoi/locales"
)

// agentHome makes a home folder of a session of Claude Code in dir, and
// an older one in another folder, and has the tests read it.
func agentHome(t testing.TB, dir string) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	now := time.Now()
	for id, cwd := range map[string]string{"here": dir, "there": "/elsewhere"} {
		p := filepath.Join(home, ".claude", "projects", "p-"+id, id+".jsonl")
		os.MkdirAll(filepath.Dir(p), 0o755)
		line := `{"type":"user","message":{"content":"fix ` + id + `"},"cwd":"` + filepath.ToSlash(cwd) + `"}` + "\n"
		if err := os.WriteFile(p, []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
		// Here is the newer, and the first in the list, every time.
		if id == "there" {
			os.Chtimes(p, now.Add(-time.Hour), now.Add(-time.Hour))
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
// than starting one: in a tab of the column that has the keyboard, moved
// from the panel, and there again the next time.
func TestResumeSessionShowsItsTerminal(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	s := hcSession(t, a, "t1")
	other := hcSession(t, a, "t2")
	a.panelTerm = other
	a.sessions.running = map[string]hcAgent{liveKey(agents.Claude, "c1"): {addr: "t1", cwd: hs.root, harness: agents.Claude}}
	n := len(a.terms)
	ses := agents.Session{Harness: agents.Claude, Root: "/h/.claude", ID: "c1", Dir: hs.root}
	a.resumeSession(ses)
	col := a.column()
	if len(a.terms) != n || s.tab == nil || col.tab() != s.tab || col.active != 1 {
		t.Fatalf("%d terminals, t1's tab %v at %d of the column; want t1 shown after main.go", len(a.terms), s.tab, col.active)
	}
	if a.panelTerm != other || a.focus != paneEditor || s.agent != agents.Claude {
		t.Errorf("panel shows %v, focus %v, agent %q; want t2, the editor, claude", a.panelTerm.hc, a.focus, s.agent)
	}
	a.activate(col, 0, true)
	a.resumeSession(ses)
	if len(col.tabs) != 2 || col.tab() != s.tab {
		t.Errorf("resumed again: %d tabs, shows %v; want t1's tab again", len(col.tabs), col.tab())
	}
}

// Resuming a session starts a terminal of its folder in a tab after the
// one shown in the column that has the keyboard, with the keyboard in it,
// marked with its agent; resuming it again shows that tab.
func TestResumeSessionOpensATab(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	hs := newHarness(t, true)
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	ses := agents.Session{Harness: agents.Codex, ID: "x1", Dir: hs.root}
	a.resumeSession(ses)
	hs.settle()
	col := a.column()
	tab := col.tab()
	if len(col.tabs) != 2 || col.active != 1 || tab.term == nil || tab.term.dir != hs.root {
		t.Fatalf("%d tabs, showing %d; want a terminal of the folder after main.go", len(col.tabs), col.active)
	}
	if a.window.PanelOpen || a.focus != paneEditor {
		t.Errorf("panel open %v, focus %v; want the tab to have the keyboard", a.window.PanelOpen, a.focus)
	}
	if h, ok := termAgent(tab.term); !ok || h != agents.Codex {
		t.Errorf("the tab's agent %q, want codex", h)
	}
	n := len(a.terms)
	a.activate(col, 0, true)
	a.resumeSession(ses)
	if len(a.terms) != n || col.tab() != tab {
		t.Errorf("resumed again: %d terminals, want %d and its tab shown", len(a.terms), n)
	}
	a.closeTerminals()
}

// Every agent known has an icon of its own, which parses; another has the
// neutral one, in the color of quiet text. A terminal is marked with the
// agent resumed in it, or else the one its programs say runs.
func TestAgentIcons(t *testing.T) {
	p := paletteFor("", true)
	seen := map[*ui.SVG]bool{}
	for _, ag := range agents.Known {
		icon, _ := agentIcon(p, ag.ID)
		if icon == iconAgent || seen[icon] {
			t.Errorf("%s has no icon of its own", ag.Name)
		}
		seen[icon] = true
		if again, _ := agentIcon(p, ag.ID); again != icon {
			t.Errorf("%s's icon was made again", ag.Name)
		}
	}
	if icon, tint := agentIcon(p, "aider"); icon != iconAgent || tint != p.muted {
		t.Errorf("an agent not known has icon %p in %v, want the neutral one", icon, tint)
	}
	s := &session{}
	if _, ok := termAgent(s); ok {
		t.Error("a shell has an agent")
	}
	s.status.Records = []terminal.StatusRecord{{State: terminal.StatusWorking, App: "qodercli"}}
	if h, _ := termAgent(s); h != agents.Qoder {
		t.Errorf("agent %q of a terminal running qodercli", h)
	}
	s.agent = agents.Claude
	if h, _ := termAgent(s); h != agents.Claude {
		t.Errorf("agent %q of a terminal Claude Code was resumed in", h)
	}
}

// The rows go under the heads of the days they were used last, each with
// how many it has, and the choice skips the heads.
func TestSessionLinesHeadTheDays(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	now := time.Date(2026, 3, 10, 9, 0, 0, 0, time.Local)
	at := func(id string, t time.Time) agents.Session {
		return agents.Session{Harness: agents.Codex, ID: id, Updated: t}
	}
	rows := []agents.Session{
		at("a", now.Add(-time.Hour)),
		at("b", now.Add(-8*time.Hour)),
		at("c", now.Add(-10*time.Hour)),
		at("d", now.AddDate(0, 0, -6)),
		at("e", now.AddDate(0, 0, -7)),
	}
	lines := a.sessionLines(rows, now)
	var got []string
	for _, l := range lines {
		if l.head {
			got = append(got, "#"+strconv.Itoa(int(l.day))+":"+strconv.Itoa(l.count))
		} else {
			got = append(got, l.s.ID)
		}
	}
	if want := "#0:2 a b #1:1 c #2:1 d #3:1 e"; strings.Join(got, " ") != want {
		t.Errorf("lines %q, want %q", strings.Join(got, " "), want)
	}
	for i, want := range map[int]int{0: 1, 1: 1, 3: 4, 9: 8} {
		if got := sessionAt(lines, i); got != want {
			t.Errorf("sessionAt(%d) = %d, want %d", i, got, want)
		}
	}
	if got := sessionAt([]sessionLine{{head: true}}, 0); got != -1 {
		t.Errorf("sessionAt of heads alone = %d, want -1", got)
	}
}

// Up and Down from the query step over the heads, and stop at the ends.
func TestSessionStepSkipsTheHeads(t *testing.T) {
	lines := []sessionLine{{head: true}, {s: agents.Session{ID: "a"}}, {head: true}, {s: agents.Session{ID: "b"}}}
	for _, c := range []struct{ i, d, want int }{{1, 1, 3}, {3, 1, 3}, {3, -1, 1}, {1, -1, 1}} {
		if got := sessionStep(lines, c.i, c.d); got != c.want {
			t.Errorf("sessionStep(%d, %d) = %d, want %d", c.i, c.d, got, c.want)
		}
	}
}

// The choice stays on its session as a reading moves it, goes to the
// nearest session once it is gone, and the session a reading brings
// fades in, while those of another query show at once.
func TestSessionsChoiceFollowsItsSession(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	st := &a.sessions
	now := time.Date(2026, 3, 10, 9, 0, 0, 0, time.Local)
	at := func(id string, ago time.Duration) agents.Session {
		return agents.Session{Harness: agents.Codex, ID: id, Updated: now.Add(-ago)}
	}
	read := func(ss ...agents.Session) []sessionLine {
		st.rowsKey.version++
		return a.keepChoice(a.sessionLines(ss, now), now)
	}
	lines := read(at("a", time.Hour), at("b", 2*time.Hour))
	st.row = 2
	st.noteChoice(lines)
	lines = read(at("b", time.Minute), at("a", time.Hour))
	if got := lines[st.row].s.ID; got != "b" {
		t.Errorf("the choice went to %q, want b", got)
	}
	if k := st.appear.shown(at("c", 0).Key(), now); k != 1 {
		t.Errorf("a row not read shows at %v", k)
	}
	lines = read(at("c", 0), at("a", time.Hour))
	if got := lines[st.row].s.ID; lines[st.row].head || got == "b" {
		t.Errorf("the choice stays on %q, gone", got)
	}
	if k := st.appear.shown(at("c", 0).Key(), now); k != 0 {
		t.Errorf("the row read anew shows at %v, want 0", k)
	}
	st.rowsKey.query = "other"
	read(at("d", 0))
	if k := st.appear.shown(at("d", 0).Key(), now); k != 1 {
		t.Errorf("a row of another query shows at %v, want 1", k)
	}
}

// Down and Up in the query move the choice among the sessions, and leave
// the keyboard in the query.
func TestSessionsQueryKeysMoveTheChoice(t *testing.T) {
	skipOnWindows(t, "the sessions' folders are POSIX paths")
	hs := newHarness(t, true)
	a := hs.a
	agentHome(t, hs.root)
	a.sessions.all = 1
	a.showSessions()
	hs.settle()
	st := &a.sessions
	if st.row != 1 {
		t.Fatalf("the choice starts on %d, want 1", st.row)
	}
	hs.key(0, ui.KeyDown)
	if st.row != 2 || !hs.tt.Focused(locales.SessionsFind()) {
		t.Errorf("after Down the choice is on %d, the query focused %v; want 2, true", st.row, hs.tt.Focused(locales.SessionsFind()))
	}
	hs.key(0, ui.KeyDown)
	hs.key(0, ui.KeyUp)
	if st.row != 1 {
		t.Errorf("after Down, Down and Up the choice is on %d, want 1", st.row)
	}
}

// The words of the query are marked where a row shows them, ignoring
// case, also where lower case changes a letter's length.
func TestWordSpansMarkTheQuery(t *testing.T) {
	var normal, hit ui.Color
	hit.A = 1
	show := func(spans []ui.Span) string {
		var b strings.Builder
		for _, sp := range spans {
			if sp.Color == hit {
				b.WriteString("[" + sp.Text + "]")
			} else {
				b.WriteString(sp.Text)
			}
		}
		return b.String()
	}
	for _, c := range []struct {
		text  string
		words []string
		want  string
	}{
		{"Fix the Login bug", nil, "Fix the Login bug"},
		{"Fix the Login bug", []string{"login", "fi"}, "[Fi]x the [Login] bug"},
		{"aaa", []string{"aa"}, "[aa]a"},
		{"İstanbul café", []string{"stan", "é"}, "İ[stan]bul caf[é]"},
		{"Codex · mayoi", []string{"mayoi", "nope"}, "Codex · [mayoi]"},
	} {
		if got := show(wordSpans(c.text, c.words, normal, hit)); got != c.want {
			t.Errorf("wordSpans(%q, %q) = %q, want %q", c.text, c.words, got, c.want)
		}
	}
}

// The row chosen while the list has the keyboard shows its buttons, and
// Copy of the Edit menu copies the command that resumes its session.
func TestSessionsListKeysShowAndCopy(t *testing.T) {
	skipOnWindows(t, "the sessions' folders are POSIX paths")
	hs := newHarness(t, true)
	a := hs.a
	agentHome(t, hs.root)
	a.showSessions()
	hs.settle()
	if _, ok := hs.tt.Find(locales.SessionsCopyCommand()); ok {
		t.Fatal("the buttons show with the keyboard in the query")
	}
	// Past the switch of the folders to the list.
	hs.key(0, ui.KeyTab)
	hs.key(0, ui.KeyTab)
	if _, ok := hs.tt.Find(locales.SessionsCopyCommand()); !ok {
		t.Error("the chosen row shows no buttons with the keyboard in the list")
	}
	hs.command("copy")
	if got := hs.tt.Clipboard(); !strings.Contains(got, "--resume") || !strings.Contains(got, "here") {
		t.Errorf("the clipboard has %q, want the command that resumes here", got)
	}
}

// A row's menu copies its transcript as a file and reveals it, and has
// neither once the file is gone.
func TestSessionMenuCopiesTranscript(t *testing.T) {
	skipOnWindows(t, "the sessions' folders are POSIX paths")
	hs := newHarness(t, true)
	a := hs.a
	home := agentHome(t, hs.root)
	file := filepath.Join(home, ".claude", "projects", "p-here", "here.jsonl")
	a.showSessions()
	hs.settle()
	host := a.host.(*fakeHost)
	for _, item := range []string{locales.SessionsCopyTranscript(), revealLabel()} {
		if err := hs.tt.RightClick("fix here"); err != nil {
			t.Fatal(err)
		}
		if err := hs.tt.ChooseMenuItem(item); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(host.clipboard, []string{file}) || !slices.Equal(host.revealed, []string{file}) {
		t.Errorf("copied %v and revealed %v, want %s", host.clipboard, host.revealed, file)
	}
	os.Remove(file)
	if err := hs.tt.RightClick("fix here"); err != nil {
		t.Fatal(err)
	}
	if err := hs.tt.ChooseMenuItem(locales.SessionsOpenTranscript()); err == nil {
		t.Error("the transcript gone opens")
	}
}

// A row says why the query found it when the words are in what it does
// not show: its first prompt under a title, its folder or its id, cut to
// show the word.
func TestSessionWhySaysWhatMatched(t *testing.T) {
	a := newHarness(t, true).a
	a.sessions.all = 0
	s := agents.Session{Harness: agents.Claude, ID: "abc123", Title: "Login bug",
		Prompt: "the sign in page of the account settings shows a blank form", Dir: "/w/mayoi"}
	for _, c := range []struct {
		query, want string
	}{
		{"", ""},
		{"login", ""},
		{"claude", ""},
		{"login blank", "…settings shows a blank form"},
		{"sign", s.Prompt},
		{"mayoi", "/w/mayoi"},
		{"c12", "abc123"},
	} {
		if got := a.sessionWhy(s, strings.Fields(c.query)); got != c.want {
			t.Errorf("sessionWhy(%q) = %q, want %q", c.query, got, c.want)
		}
	}
	// A session of no title shows its prompt as its title.
	s.Title = ""
	if got := a.sessionWhy(s, []string{"blank"}); got != "" {
		t.Errorf("sessionWhy of a prompt shown = %q, want none", got)
	}
}

// A session that runs keeps when it took its state while it stays in it,
// and takes a new time as it changes.
func TestNoteLiveFollowsTheStates(t *testing.T) {
	var st sessionsState
	t0 := time.Unix(1000, 0)
	k := liveKey(agents.Claude, "c1")
	run := func(s terminal.StatusState) map[string]hcAgent { return map[string]hcAgent{k: {state: s}} }
	st.noteLive(run(terminal.StatusWorking), t0)
	st.running = run(terminal.StatusWorking)
	st.noteLive(run(terminal.StatusWorking), t0.Add(time.Second))
	if got := st.liveSince[k]; !got.Equal(t0) {
		t.Errorf("a state kept took %v, want %v", got, t0)
	}
	st.noteLive(run(terminal.StatusBlocked), t0.Add(2*time.Second))
	if got := st.liveSince[k]; !got.Equal(t0.Add(2 * time.Second)) {
		t.Errorf("a state changed took %v, want the time it changed", got)
	}
	st.noteLive(nil, t0.Add(3*time.Second))
	if len(st.liveSince) != 0 {
		t.Errorf("a session stopped is kept: %v", st.liveSince)
	}
}

// The hover of a row fades out with its buttons as the pointer leaves,
// rather than going at once and leaving their background behind.
func TestSessionsHoverFadesWithTheButtons(t *testing.T) {
	skipOnWindows(t, "the sessions' folders are POSIX paths")
	hs := newHarness(t, true)
	a := hs.a
	agentHome(t, hs.root)
	a.sessions.all = 1
	a.showSessions()
	hs.settle()
	row, ok := hs.tt.Find("fix there")
	if !ok {
		t.Fatal("no row of there")
	}
	// Left of the title, in the row's padding.
	x, y := int(row.X+3), int(row.Y+row.H/2)
	plain := hs.tt.Image().RGBAAt(x, y)
	hs.tt.Move(row.X+row.W/2, row.Y+row.H/2)
	hs.settle()
	hovered := hs.tt.Image().RGBAAt(x, y)
	if hovered == plain {
		t.Fatal("the row under the pointer shows no hover")
	}
	hs.tt.Move(row.X, 600)
	hs.tt.Frame()
	hs.tt.Advance(motionHover / 2)
	hs.tt.Frame()
	if got := hs.tt.Image().RGBAAt(x, y); got == plain || got == hovered {
		t.Errorf("halfway out the row shows %v, want between %v and %v", got, hovered, plain)
	}
	hs.settle()
	if got := hs.tt.Image().RGBAAt(x, y); got != plain {
		t.Errorf("the row left shows %v, want %v", got, plain)
	}
}

// A session a reading brings opens its place, and the rows under it slide
// down rather than jump.
func TestSessionsNewRowOpensItsPlace(t *testing.T) {
	skipOnWindows(t, "the sessions' folders are POSIX paths")
	hs := newHarness(t, true)
	a := hs.a
	agentHome(t, hs.root)
	a.sessions.all = 1
	a.showSessions()
	hs.settle()
	st := &a.sessions
	before, _ := hs.tt.Find("fix there")
	s := st.sessions[0]
	s.ID, s.Title, s.Updated = "new", "fix new", time.Now().Add(time.Minute)
	st.sessions = append([]agents.Session{s}, st.sessions...)
	st.version++
	hs.tt.Frame()
	hs.tt.Advance(motionShow / 2)
	hs.tt.Frame()
	mid, _ := hs.tt.Find("fix there")
	hs.tt.Advance(motionShow)
	hs.tt.Frame()
	after, _ := hs.tt.Find("fix there")
	if !(before.Y < mid.Y && mid.Y < after.Y) {
		t.Errorf("the row under the new one is at %v, %v, %v; want it sliding down", before.Y, mid.Y, after.Y)
	}
	if want := before.Y + sessionRowH + sessionRowGap; after.Y != want {
		t.Errorf("the row under the new one settles at %v, want %v", after.Y, want)
	}
}

// As a session starts to run, what it does opens its room after its
// agent's icon, and the agent's name moves over rather than jumps.
func TestSessionsStateWordSlidesIn(t *testing.T) {
	skipOnWindows(t, "herdr has no hc for Windows")
	hs := newHarness(t, true)
	a := hs.a
	home := agentHome(t, hs.root)
	// Of a config folder the row names: "work".
	if err := os.Rename(filepath.Join(home, ".claude"), filepath.Join(home, ".claude-work")); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	runs := false
	useFakeHC(t, func(args []string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case args[0] == "ls" && runs:
			return []byte(`{"terminals":[{"address":"t1","agent":"claude-code","cwd":"/p","state":"live","status":"working"}]}`), nil
		case args[0] == "get":
			return []byte(`{"agent_session":{"agent":"claude-code","id":"here"}}`), nil
		}
		return []byte(`{"terminals":[]}`), nil
	})
	a.settings.TerminalBackend = "hc"
	a.showSessions()
	hs.settle()
	before, ok := hs.tt.Find("Claude Code · work")
	if !ok {
		t.Fatal("no agent in the row")
	}
	mu.Lock()
	runs = true
	mu.Unlock()
	a.sessions.running = map[string]hcAgent{liveKey(agents.Claude, "here"): {addr: "t1", harness: agents.Claude, state: terminal.StatusWorking}}
	hs.tt.Frame()
	hs.tt.Advance(motionShow / 2)
	hs.tt.Frame()
	mid, _ := hs.tt.Find("Claude Code · work")
	hs.settle()
	after, _ := hs.tt.Find("Claude Code · work")
	if !(before.X < mid.X && mid.X < after.X) {
		t.Errorf("the agent is at %v, %v, %v as the session starts to run; want it moving over", before.X, mid.X, after.X)
	}
}

// A row's look is made once for the rows shown, and again for a new query.
func TestSessionLooksAreKept(t *testing.T) {
	a := newHarness(t, true).a
	st := &a.sessions
	l := sessionLine{s: agents.Session{Harness: agents.Codex, ID: "x", Title: "Fix login"}}
	a.sessionLooks()
	first := a.sessionLook(l)
	if a.sessionLooks(); a.sessionLook(l) != first {
		t.Error("the look was made again for the same rows")
	}
	st.query, st.rowsKey.query = "login", "login"
	a.sessionLooks()
	got := a.sessionLook(l)
	if got == first || len(got.title) != 2 || got.title[1].Text != "login" {
		t.Errorf("the look of the query is %+v, want login marked", got.title)
	}
}

// A session a reading drops closes its place as it fades, and the rows
// under it slide up rather than jump; with less motion, it only fades,
// and they move once it is gone.
func TestSessionsDroppedRowClosesItsPlace(t *testing.T) {
	skipOnWindows(t, "the sessions' folders are POSIX paths")
	for _, reduce := range []bool{false, true} {
		hs := newHarness(t, true)
		a := hs.a
		agentHome(t, hs.root)
		hs.tt.SetPreferences(ui.Preferences{ReduceMotion: reduce})
		a.sessions.all = 1
		a.showSessions()
		hs.settle()
		st := &a.sessions
		top, under := st.sessions[0], st.sessions[1]
		before, _ := hs.tt.Find(under.Label())
		st.sessions = []agents.Session{under}
		st.version++
		hs.tt.Frame()
		hs.tt.Advance(motionShow / 2)
		hs.tt.Frame()
		mid, _ := hs.tt.Find(under.Label())
		if !hs.tt.HasText(top.Label()) {
			t.Errorf("reduce %v: the row dropped is gone at once", reduce)
		}
		hs.settle()
		after, _ := hs.tt.Find(under.Label())
		if hs.tt.HasText(top.Label()) {
			t.Errorf("reduce %v: the row dropped stays", reduce)
		}
		if want := before.Y - sessionRowH - sessionRowGap; after.Y != want {
			t.Errorf("reduce %v: the row under the one dropped settles at %v, want %v", reduce, after.Y, want)
		}
		if reduce && mid.Y != before.Y {
			t.Errorf("with less motion the row under the one dropped moves to %v, want it at %v until it goes", mid.Y, before.Y)
		}
		if !reduce && !(after.Y < mid.Y && mid.Y < before.Y) {
			t.Errorf("the row under the one dropped is at %v, %v, %v; want it sliding up", before.Y, mid.Y, after.Y)
		}
	}
}

// The rows a reading drops stay among the lines as they go, after the
// row before them, where neither the choice nor the keys rest; they are
// left out once gone, and at once for another query.
func TestSessionsDroppedRowGoes(t *testing.T) {
	a := newHarness(t, true).a
	st := &a.sessions
	now := time.Date(2026, 3, 10, 9, 0, 0, 0, time.Local)
	at := func(id string, ago time.Duration) agents.Session {
		return agents.Session{Harness: agents.Codex, ID: id, Updated: now.Add(-ago)}
	}
	ids := func(lines []sessionLine) string {
		var out []string
		for _, l := range lines {
			switch {
			case l.head:
				out = append(out, "#"+strconv.Itoa(int(l.day)))
			case !l.gone.IsZero():
				out = append(out, "-"+l.s.ID)
			default:
				out = append(out, l.s.ID)
			}
		}
		return strings.Join(out, " ")
	}
	read := func(at time.Time, ss ...agents.Session) []sessionLine {
		st.rowsKey.version++
		return a.keepChoice(a.sessionLines(ss, now), at)
	}
	lines := read(now, at("a", time.Hour), at("b", 2*time.Hour), at("c", 3*time.Hour), at("old", 30*time.Hour))
	st.row = 2
	st.noteChoice(lines)
	lines = read(now, at("a", time.Hour), at("c", 3*time.Hour))
	if got, want := ids(lines), "#0 a -b c #1 -old"; got != want {
		t.Errorf("the lines as b and old go are %q, want %q", got, want)
	}
	if got := lines[st.row].s.ID; got != "c" {
		t.Errorf("the choice on b went to %q, want c", got)
	}
	if got := lines[sessionStep(lines, st.row, -1)].s.ID; got != "a" {
		t.Errorf("Up from c goes to %q, want a past b", got)
	}
	if k := st.lineShown(lines[2], now.Add(motionLeave/2)); k <= 0 || k >= 1 {
		t.Errorf("b halfway gone shows at %v, want between 0 and 1", k)
	}
	lines = a.keepChoice(st.lines, now.Add(motionLeave/2))
	if got, want := ids(lines), "#0 a -b c #1 -old"; got != want {
		t.Errorf("the lines halfway are %q, want %q", got, want)
	}
	lines = a.keepChoice(st.lines, now.Add(motionLeave))
	if got, want := ids(lines), "#0 a c"; got != want {
		t.Errorf("the lines once gone are %q, want %q", got, want)
	}
	if got := lines[st.row].s.ID; got != "c" {
		t.Errorf("the choice is on %q once b is gone, want c", got)
	}
	st.rowsKey.query = "a"
	if got, want := ids(read(now, at("a", time.Hour))), "#0 a"; got != want {
		t.Errorf("the lines of another query are %q, want %q", got, want)
	}
}

// How long ago a row's session was used is said again only as the minute
// it counts turns, not each frame.
func TestSessionAgoKeptForItsMinute(t *testing.T) {
	updated := time.Date(2026, 3, 10, 9, 0, 0, 0, time.Local)
	lk := &sessionLook{}
	now := updated.Add(5*time.Minute + 10*time.Second)
	if got, want := lk.agoAt(updated, now), ago(updated, now); got != want {
		t.Fatalf("ago %q, want %q", got, want)
	}
	lk.ago = "kept"
	if got := lk.agoAt(updated, now.Add(49*time.Second)); got != "kept" {
		t.Errorf("ago within its minute is %q, made again", got)
	}
	later := now.Add(50 * time.Second)
	if got, want := lk.agoAt(updated, later), ago(updated, later); got != want {
		t.Errorf("ago as its minute turns is %q, want %q", got, want)
	}
	if got, want := lk.agoAt(updated, updated), ago(updated, updated); got != want {
		t.Errorf("ago as the clock goes back is %q, want %q", got, want)
	}
}

// sessionsOfToday shows n sessions of today in the view, s0 the latest, with
// the keyboard in the query.
func sessionsOfToday(t *testing.T, hs *harness, n int) *sessionsState {
	skipOnWindows(t, "the sessions' folders are POSIX paths")
	a := hs.a
	agentHome(t, hs.root)
	a.sessions.all = 1
	a.showSessions()
	hs.settle()
	st := &a.sessions
	now := time.Now()
	ss := make([]agents.Session, n)
	for i := range ss {
		id := "s" + strconv.Itoa(i)
		ss[i] = agents.Session{Harness: agents.Codex, ID: id, Title: "fix " + id, Dir: hs.root, Updated: now.Add(-time.Duration(i) * time.Second)}
	}
	st.sessions = ss
	st.version++
	hs.settle()
	return st
}

// chosenID is the session the choice is on.
func chosenID(st *sessionsState) string {
	if st.row < 0 || st.row >= len(st.shown) {
		return ""
	}
	return st.shown[st.row].s.ID
}

// Page Up and Page Down move the choice by a page, from the query and from
// the list, and Home and End from the query to the first and the last,
// each showing the row chosen.
func TestSessionsPageAndEndKeys(t *testing.T) {
	hs := newHarness(t, true)
	st := sessionsOfToday(t, hs, 60)
	ends := endsMods()
	inView := func(id string) bool {
		r, ok := hs.tt.Find("fix " + id)
		return ok && r.Y >= 0 && r.Y+r.H <= 760
	}
	hs.key(0, ui.KeyPageDown)
	n, _ := strconv.Atoi(strings.TrimPrefix(chosenID(st), "s"))
	if n < 5 || n >= 59 {
		t.Fatalf("Page Down in the query chose %q, want a page on", chosenID(st))
	}
	if !hs.tt.Focused(locales.SessionsFind()) {
		t.Error("Page Down took the keyboard from the query")
	}
	hs.key(ends, ui.KeyEnd)
	if got := chosenID(st); got != "s59" || !inView(got) {
		t.Errorf("End chose %q, in view %v; want s59, shown", got, inView(got))
	}
	hs.key(0, ui.KeyPageUp)
	if m, _ := strconv.Atoi(strings.TrimPrefix(chosenID(st), "s")); m != 59-n {
		t.Errorf("Page Up from the end chose %q, want s%d", chosenID(st), 59-n)
	}
	hs.key(ends, ui.KeyHome)
	if got := chosenID(st); got != "s0" || !inView(got) {
		t.Errorf("Home chose %q, in view %v; want s0, shown", got, inView(got))
	}
	// Into the list, past the switch of the folders.
	hs.key(0, ui.KeyTab)
	hs.key(0, ui.KeyTab)
	hs.key(0, ui.KeyPageDown)
	if got := chosenID(st); got != "s"+strconv.Itoa(n) || !inView(got) {
		t.Errorf("Page Down in the list chose %q, in view %v; want s%d, shown", got, inView(got), n)
	}
	hs.key(0, ui.KeyPageUp)
	if got := chosenID(st); got != "s0" {
		t.Errorf("Page Up in the list chose %q, want s0", got)
	}
}

// Up in the list past a row that goes lands on the row above it at once,
// rather than resting on the one going and coming back.
func TestSessionsUpSkipsALeavingRow(t *testing.T) {
	hs := newHarness(t, true)
	st := sessionsOfToday(t, hs, 3)
	hs.key(0, ui.KeyTab)
	hs.key(0, ui.KeyTab)
	hs.key(0, ui.KeyDown)
	hs.key(0, ui.KeyDown)
	if got := chosenID(st); got != "s2" {
		t.Fatalf("the choice is on %q, want s2", got)
	}
	st.sessions = []agents.Session{st.sessions[0], st.sessions[2]}
	st.version++
	hs.tt.Frame()
	hs.tt.Key(0, ui.KeyUp)
	for range 3 {
		step(hs.tt)
		if got := chosenID(st); got != "s0" {
			t.Fatalf("after Up past s1 going the choice is on %q, want s0", got)
		}
	}
}

// The choice's background glides to the row the keys move it to, starting
// at once, and shows there at once for a key that repeats as it glides,
// for a row far off, and with less motion.
func TestSessionsChoiceGlides(t *testing.T) {
	a := newHarness(t, true).a
	now := time.Date(2026, 3, 10, 9, 0, 0, 0, time.Local)
	var lines []sessionLine
	lines = append(lines, sessionLine{head: true, id: "#"})
	for i := range 8 {
		id := "s" + strconv.Itoa(i)
		lines = append(lines, sessionLine{s: agents.Session{Harness: agents.Codex, ID: id}, id: id})
	}
	a.sessionLooks()
	g := &a.sessions.glide
	*g = choiceGlide{key: "s0", row: 1, at: now.Add(-time.Second)}
	step := float32(sessionRowH + sessionRowGap)
	if y, there := a.glideTo(lines, 2, now, false); y != -step || there != 0 {
		t.Errorf("the choice moved down starts at %v, %v there; want %v, 0", y, there, -step)
	}
	if y, there := a.glideTo(lines, 2, now.Add(motionHover/2), false); !(y < 0 && y > -step/2) || there <= 0.5 {
		t.Errorf("halfway the choice is at %v, %v there; want past half of %v, easing out", y, there, -step)
	}
	if y, _ := a.glideTo(lines, 2, now.Add(motionHover/2), false); y == 0 {
		t.Error("the glide stopped as the row was built again")
	}
	if y, _ := a.glideTo(lines, 3, now.Add(motionHover/2), false); y != 0 {
		t.Errorf("a key repeating as it glides puts it %v off, want 0", y)
	}
	if y, _ := a.glideTo(lines, 2, now.Add(time.Second), false); y != step {
		t.Errorf("Up starts at %v, want %v", y, step)
	}
	if y, _ := a.glideTo(lines, 2, now.Add(time.Second+motionHover), false); y != 0 {
		t.Errorf("the glide ends at %v, want 0", y)
	}
	if y, _ := a.glideTo(lines, 8, now.Add(2*time.Second), false); y != 0 {
		t.Errorf("a row far off is %v off, want 0", y)
	}
	if y, _ := a.glideTo(lines, 7, now.Add(3*time.Second), true); y != 0 {
		t.Errorf("with less motion it is %v off, want 0", y)
	}
}

// The choice shows strongest while the query or the list has the keyboard,
// ringed while the list does, and quieter while neither does.
func TestSessionsChoiceShowsWhereTheKeysAre(t *testing.T) {
	hs := newHarness(t, true)
	sessionsOfToday(t, hs, 3)
	row, ok := hs.tt.Find("fix s0")
	if !ok {
		t.Fatal("no row of s0")
	}
	// Left of the title, in the row's padding, and on its edge.
	at := func() (inside, edge [4]uint8) {
		img := hs.tt.Image()
		in, e := img.RGBAAt(int(row.X+3), int(row.Y+row.H/2)), img.RGBAAt(int(row.X), int(row.Y+row.H/2))
		return [4]uint8{in.R, in.G, in.B, in.A}, [4]uint8{e.R, e.G, e.B, e.A}
	}
	query, queryEdge := at()
	hs.key(0, ui.KeyTab)
	hs.key(0, ui.KeyTab)
	list, listEdge := at()
	if list != query || listEdge == queryEdge {
		t.Errorf("with the list focused the choice is %v, edge %v; want %v, ringed against %v", list, listEdge, query, queryEdge)
	}
	// Away, on the page beside the sidebar.
	hs.tt.ClickAt(700, 600)
	hs.settle()
	away, _ := at()
	if away == query {
		t.Errorf("with the keyboard elsewhere the choice shows as %v, as with it in the query", away)
	}
}

// A row that comes back as it goes comes on from as far as it showed,
// rather than from nothing, and goes over motionLeave, less than it comes.
func TestSessionsReturningRowResumes(t *testing.T) {
	a := newHarness(t, true).a
	st := &a.sessions
	now := time.Date(2026, 3, 10, 9, 0, 0, 0, time.Local)
	at := func(id string, ago time.Duration) agents.Session {
		return agents.Session{Harness: agents.Codex, ID: id, Updated: now.Add(-ago)}
	}
	read := func(t time.Time, ss ...agents.Session) []sessionLine {
		st.rowsKey.version++
		return a.keepChoice(a.sessionLines(ss, now), t)
	}
	if motionLeave >= motionShow {
		t.Errorf("going out takes %v, coming in %v; want it shorter", motionLeave, motionShow)
	}
	read(now, at("a", time.Hour), at("b", 2*time.Hour))
	lines := read(now, at("a", time.Hour))
	mid := now.Add(motionLeave / 2)
	var was float32
	for _, l := range lines {
		if l.s.ID == "b" {
			was = st.lineShown(l, mid)
		}
	}
	if was <= 0 || was >= 1 {
		t.Fatalf("b halfway gone shows at %v", was)
	}
	lines = read(mid, at("a", time.Hour), at("b", 2*time.Hour))
	for _, l := range lines {
		if l.s.ID == "b" {
			if got := st.lineShown(l, mid); got < was-0.01 || got > was+0.01 {
				t.Errorf("b back shows at %v, want %v, where it was", got, was)
			}
		}
	}
}
