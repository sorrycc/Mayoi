package main

import (
	"slices"
	"testing"
	"time"

	"mayoi/internal/git"
)

func TestCommitChecksDue(t *testing.T) {
	a := &app{}
	g := &a.git
	r := &git.Repo{Root: "/r"}
	now := time.Now()
	g.repo, g.commitChecksRepo = r, r
	g.history = []git.Commit{
		{Hash: "local", Time: now},
		{Hash: "new", Time: now.Add(-time.Minute)},
		{Hash: "running", Time: now.Add(-time.Hour)},
		{Hash: "passed", Time: now.Add(-48 * time.Hour)},
		{Hash: "quiet", Time: now.Add(-time.Hour)},
		{Hash: "old", Time: now.Add(-48 * time.Hour)},
	}
	g.unpushed = map[string]bool{"local": true}
	g.commitChecks = map[string]git.CommitStatus{
		"running": {Checks: git.ChecksPending, Pending: 1},
		"passed":  {Checks: git.ChecksPass, Passed: 3},
	}
	g.commitChecksRead = map[string]time.Time{
		"running": now.Add(-time.Minute),
		"passed":  now.Add(-time.Minute),
		"quiet":   now.Add(-3 * time.Minute),
		"old":     now.Add(-3 * time.Minute),
	}
	// Never asked, still running, or of today with none yet: not those
	// unpushed, finished a minute ago, or of long ago with none.
	if got := a.repository().commitChecksDue(now); !slices.Equal(got, []string{"new", "running", "quiet"}) {
		t.Errorf("due = %v", got)
	}
	// Long after, the finished ones are asked again, in case a job ran again.
	later := now.Add(commitChecksDoneEvery)
	if got := a.repository().commitChecksDue(later); !slices.Equal(got, []string{"new", "running", "passed", "quiet", "old"}) {
		t.Errorf("due later = %v", got)
	}
	// A commit a tab shows goes first.
	g.checksWanted = map[string]bool{"tab": true}
	if got := a.repository().commitChecksDue(now); len(got) == 0 || got[0] != "tab" {
		t.Errorf("wanted = %v", got)
	}
	if st, ok := a.repository().checksOf("passed"); !ok || st.Passed != 3 {
		t.Errorf("checksOf = %+v, %v", st, ok)
	}
	// Those read of another repository say nothing of this one's.
	g.repo = &git.Repo{Root: "/o"}
	if _, ok := a.repository().checksOf("passed"); ok || a.repository().hasCommitChecks() {
		t.Error("checks of another repository")
	}
}

func TestHeadChecksTell(t *testing.T) {
	h := &fakeHost{}
	a := &app{host: h}
	g := &a.git
	g.status = &git.Status{Head: "h"}
	g.commitChecks = map[string]git.CommitStatus{"h": {Checks: git.ChecksPending, Pending: 2}}
	a.repository().headChecksChanged()
	if len(a.notes) != 0 || g.headChecksRunning != "h" {
		t.Fatalf("running: notes %v, watching %q", a.notes, g.headChecksRunning)
	}
	// Ended, once: a failure offers to show them.
	g.commitChecks["h"] = git.CommitStatus{Checks: git.ChecksFail, Failed: 1, Passed: 1}
	a.repository().headChecksChanged()
	a.repository().headChecksChanged()
	if len(a.notes) != 1 || a.notes[0].action == nil || a.notes[0].message != "1 check failed on h" {
		t.Fatalf("ended: %+v", a.notes)
	}
	// Checks that were done when first seen say nothing.
	g.status = &git.Status{Head: "k"}
	g.commitChecks["k"] = git.CommitStatus{Checks: git.ChecksPass, Passed: 1}
	a.repository().headChecksChanged()
	if len(a.notes) != 1 {
		t.Fatalf("done at once: %+v", a.notes)
	}
	if got := checksSummary(git.CommitStatus{Passed: 2, Failed: 1}); got != "2 checks passed · 1 check failed" {
		t.Errorf("summary = %q", got)
	}
	got := formatCheckLog(git.Check{Name: "Unit", Workflow: "Test"}, "commit abc", "boom")
	if want := "The CI check \"Test / Unit\" failed on commit abc\nThe last 1 lines of its log:\n```\nboom\n```\n"; got != want {
		t.Errorf("log = %q", got)
	}
}
