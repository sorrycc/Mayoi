package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/agents"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// A row's menu archives its session, which leaves the list, after the
// sessions are read again and after a restart, and shows under Archived,
// where its menu restores it; a toast undoes each. The transcript is let
// be.
func TestSessionsArchiveAndRestore(t *testing.T) {
	skipOnWindows(t, "the sessions' folders are POSIX paths")
	hs := newHarness(t, true)
	a, tt := hs.a, hs.tt
	home := agentHome(t, hs.root)
	file := filepath.Join(home, ".claude", "projects", "p-there", "there.jsonl")
	before, _ := os.ReadFile(file)
	a.sessions.all = 1
	a.showSessions()
	hs.settle()
	if err := tt.RightClick("fix there"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(locales.SessionsArchive()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := sessionIDs(a.sessionRows()); got != "here" {
		t.Fatalf("listed %q after archiving there, want here", got)
	}
	if _, ok := tt.Find(locales.SessionsArchived(1)); !ok {
		t.Errorf("no toast says so: %q", tt.Texts())
	}
	if err := tt.Click(locales.ButtonUndo()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := sessionIDs(a.sessionRows()); got != "here there" {
		t.Fatalf("listed %q after Undo, want both", got)
	}
	if err := tt.RightClick("fix there"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(locales.SessionsArchive()); err != nil {
		t.Fatal(err)
	}
	// Read again, it stays out.
	a.readSessionsNow()
	v := a.sessions.version
	hs.settle()
	if a.sessions.version == v {
		t.Fatal("the sessions were not read again")
	}
	if got := sessionIDs(a.sessionRows()); got != "here" {
		t.Errorf("listed %q read again, want here", got)
	}
	// As the state is kept and read at the next launch, it stays out.
	data, _ := json.Marshal(a.settings.State)
	again := settings.Default()
	if err := json.Unmarshal(data, &again.State); err != nil {
		t.Fatal(err)
	}
	a.settings = again
	if got := sessionIDs(a.sessionRows()); got != "here" {
		t.Errorf("listed %q after a restart, want here", got)
	}
	// Archived lists it alone, with the title saying so; the query and
	// the folders narrow it too.
	a.toggleArchivedSessions()
	hs.settle()
	if got := sessionIDs(a.sessionRows()); got != "there" {
		t.Fatalf("Archived lists %q, want there", got)
	}
	_, long := tt.Find(a.pal.title(locales.SidebarTitleSessionsArchived()))
	if _, short := tt.Find(a.pal.title(locales.SidebarTitleSessionsArchivedShort())); !long && !short {
		t.Errorf("the title says nothing of Archived: %q", tt.Texts())
	}
	a.sessions.all = 0
	if got := sessionIDs(a.sessionRows()); got != "" {
		t.Errorf("Archived of this project lists %q, want none", got)
	}
	a.sessions.all = 1
	a.sessions.query = "elsewhere"
	if got := sessionIDs(a.sessionRows()); got != "there" {
		t.Errorf("Archived of the query lists %q, want there", got)
	}
	a.sessions.query = "nowhere"
	if got := sessionIDs(a.sessionRows()); got != "" {
		t.Errorf("Archived of another query lists %q, want none", got)
	}
	a.sessions.query = ""
	hs.settle()
	if err := tt.RightClick("fix there"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(locales.SessionsRestore()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := sessionIDs(a.sessionRows()); got != "" {
		t.Errorf("Archived lists %q after Restore, want none", got)
	}
	if _, ok := tt.Find(locales.SessionsNoneArchived()); !ok {
		t.Errorf("Archived empty says nothing: %q", tt.Texts())
	}
	a.toggleArchivedSessions()
	if got := sessionIDs(a.sessionRows()); got != "here there" {
		t.Errorf("listed %q after Restore, want both", got)
	}
	if after, _ := os.ReadFile(file); !bytes.Equal(before, after) {
		t.Error("the transcript changed")
	}
}

// Several sessions chosen go to Archived together with Delete, the
// choice moving to the row left, and Undo brings them all back.
func TestSessionsArchiveSeveral(t *testing.T) {
	hs := newHarness(t, true)
	st := sessionsOfToday(t, hs, 4)
	a, tt := hs.a, hs.tt
	if err := tt.Click("fix s0"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	hs.key(ui.Shift, ui.KeyDown)
	hs.key(ui.Shift, ui.KeyDown)
	if st.selection.Len() != 3 {
		t.Fatalf("chose %d sessions, want 3", st.selection.Len())
	}
	hs.key(0, ui.KeyBackspace)
	hs.settle()
	if got := sessionIDs(a.sessionRows()); got != "s3" {
		t.Fatalf("listed %q, want s3", got)
	}
	if got := chosenID(st); got != "s3" || st.selection.Len() != 0 {
		t.Errorf("the choice on %q with %d chosen, want s3 alone", got, st.selection.Len())
	}
	if _, ok := tt.Find(locales.SessionsArchived(3)); !ok {
		t.Errorf("no toast says so: %q", tt.Texts())
	}
	if err := tt.Click(locales.ButtonUndo()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := sessionIDs(a.sessionRows()); got != "s0 s1 s2 s3" {
		t.Errorf("listed %q after Undo, want all four", got)
	}
}

// A session is archived by its agent, its Claude Code folder and its id:
// another of the same id in another folder, or of another agent, stays.
func TestSessionsArchiveByIdentity(t *testing.T) {
	hs := newHarness(t, true)
	st := sessionsOfToday(t, hs, 0)
	a := hs.a
	now := time.Now()
	st.sessions = []agents.Session{
		{Harness: agents.Claude, Root: "/h/.claude", ID: "same", Title: "fix one", Dir: hs.root, Updated: now},
		{Harness: agents.Claude, Root: "/h/.claude-work", ID: "same", Title: "fix two", Dir: hs.root, Updated: now.Add(-time.Second)},
		{Harness: agents.Codex, ID: "same", Title: "fix three", Dir: hs.root, Updated: now.Add(-2 * time.Second)},
	}
	st.version++
	a.archiveSessions(st.sessions[1:2], false)
	rows := a.sessionRows()
	if len(rows) != 2 || rows[0].Root != "/h/.claude" || rows[1].Harness != agents.Codex {
		t.Errorf("listed %+v, want the others", rows)
	}
}

// Archiving a session that runs in a terminal lets the terminal run;
// resuming it from Archived shows that terminal and brings it back to the
// list, while one whose folder is gone stays archived.
func TestSessionsArchiveLiveAndResume(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	term := hcSession(t, a, "t1")
	a.sessions.running = map[string]hcAgent{liveKey(agents.Claude, "c1"): {addr: "t1", cwd: hs.root, harness: agents.Claude}}
	ses := agents.Session{Harness: agents.Claude, Root: "/h/.claude", ID: "c1", Dir: hs.root}
	n := len(a.terms)
	a.archiveSessions([]agents.Session{ses}, false)
	if !a.settings.IsArchived(sessionRef(ses)) || len(a.terms) != n || term.term == nil {
		t.Fatalf("archived %v, %d terminals; want it archived and its terminal kept", a.settings.IsArchived(sessionRef(ses)), len(a.terms))
	}
	a.resumeSession(ses)
	if a.settings.IsArchived(sessionRef(ses)) || term.tab == nil {
		t.Errorf("resumed: archived %v, its terminal's tab %v; want it back and shown", a.settings.IsArchived(sessionRef(ses)), term.tab)
	}
	gone := agents.Session{Harness: agents.Codex, ID: "x", Dir: filepath.Join(hs.root, "gone")}
	a.archiveSessions([]agents.Session{gone}, false)
	a.resumeSession(gone)
	if !a.settings.IsArchived(sessionRef(gone)) {
		t.Error("a session that could not resume left Archived")
	}
}
