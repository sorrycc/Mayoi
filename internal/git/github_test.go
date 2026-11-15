package git

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"
)

func TestParseOverview(t *testing.T) {
	head := "0123456789abcdef0123456789abcdef01234567"
	o, err := parseOverview([]byte(`{"data":{"viewer":{"login":"me"},
		"repository":{"nameWithOwner":"o/r","url":"https://github.com/o/r","viewerPermission":"WRITE","hasIssuesEnabled":true,
			"defaultBranchRef":{"name":"main"},
			"pullRequests":{"totalCount":3,"nodes":[
				{"number":7,"title":"Fix it","url":"https://github.com/o/r/pull/7","isDraft":false,"updatedAt":"2026-11-14T12:00:00Z",
				 "headRefName":"fix","headRefOid":"` + head + `","reviewDecision":"APPROVED","author":{"login":"ann"},
				 "comments":{"totalCount":2},"labels":{"nodes":[{"name":"bug","color":"d73a4a"}]},"assignees":{"nodes":[{"login":"me"}]},
				 "commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE"}}}]}},
				{"number":8,"title":"Draft","isDraft":true,"author":null,"comments":{"totalCount":0},"labels":{"nodes":[]},
				 "assignees":{"nodes":[]},"commits":{"nodes":[{"commit":{"statusCheckRollup":null}}]}}]},
			"issues":{"totalCount":40,"nodes":[
				{"number":3,"title":"Crash","url":"https://github.com/o/r/issues/3","updatedAt":"2026-11-13T00:00:00Z",
				 "author":{"login":"bob"},"comments":{"totalCount":5},"labels":{"nodes":[]},"assignees":{"nodes":[{"login":"ME"}]}}]}},
		"review":{"nodes":[{"number":8},{}]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if o.Viewer != "me" || o.Repo != "o/r" || !o.CanTriage() || !o.IssuesOn || o.DefaultBranch != "main" || o.OpenPRs != 3 || o.OpenIssues != 40 {
		t.Fatalf("overview = %+v", o)
	}
	if len(o.PRs) != 2 || len(o.Issues) != 1 {
		t.Fatalf("%d pull requests, %d issues", len(o.PRs), len(o.Issues))
	}
	pr := o.PRs[0]
	if !pr.PR || pr.Author != "ann" || pr.Checks != ChecksFail || pr.Review != "APPROVED" || pr.Comments != 2 ||
		pr.Labels[0] != (GHLabel{"bug", "d73a4a"}) || !pr.AssignedTo("me") || pr.ReviewAsked || pr.Updated.IsZero() {
		t.Fatalf("pr = %+v", pr)
	}
	if d := o.PRs[1]; !d.Draft || !d.ReviewAsked || d.Checks != ChecksNone || d.Author != "" {
		t.Fatalf("draft = %+v", d)
	}
	if is := o.Issues[0]; is.PR || !is.AssignedTo("me") || is.Comments != 5 {
		t.Fatalf("issue = %+v", is)
	}
	if heads := o.PRHeads(); len(heads) != 1 || heads[head][0].Number != 7 {
		t.Fatalf("heads = %+v", heads)
	}
	if l := o.PRList(); len(l) != 2 || l[1].Number != 8 || !l[1].Draft {
		t.Fatalf("list = %+v", l)
	}
	if _, err := parseOverview([]byte(`{"data":{"repository":null},"errors":[{"message":"Could not resolve to a Repository"}]}`)); !errors.Is(err, ErrNoGH) {
		t.Fatalf("missing repository = %v", err)
	}
}

func TestParseIssue(t *testing.T) {
	is, err := parseIssue([]byte(`{"data":{"repository":{"issueOrPullRequest":{"__typename":"Issue","id":"I_1","number":3,
		"title":"Crash","url":"u","state":"CLOSED","stateReason":"NOT_PLANNED","body":"It **crashes**","createdAt":"2026-11-01T00:00:00Z",
		"viewerCanUpdate":true,"author":{"login":"bob"},"labels":{"nodes":[{"name":"bug","color":"ff0000"}]},
		"assignees":{"nodes":[{"login":"ann"}]},"milestone":{"title":"v1"},
		"comments":{"totalCount":120,"nodes":[{"author":null,"body":"ghost","createdAt":"2026-11-02T00:00:00Z","url":"c"}]},
		"closedByPullRequestsReferences":{"nodes":[{"number":9,"title":"Fix","url":"p","state":"MERGED"}]}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if is.PR || is.State != "CLOSED" || is.StateReason != "NOT_PLANNED" || is.Author != "bob" || is.Milestone != "v1" || !is.CanUpdate ||
		is.AllComments != 120 || len(is.Comments) != 1 || is.Comments[0].Author != "" || len(is.Linked) != 1 || is.Linked[0].Number != 9 {
		t.Fatalf("issue = %+v", is)
	}
	pr, err := parseIssue([]byte(`{"data":{"repository":{"issueOrPullRequest":{"__typename":"PullRequest","number":7,"headRefName":"fix","baseRefName":"main",
		"labels":{"nodes":[]},"assignees":{"nodes":[]},"comments":{"totalCount":0,"nodes":[]}}}}}`))
	if err != nil || !pr.PR || pr.HeadRef != "fix" || pr.BaseRef != "main" || pr.kind() != "pr" {
		t.Fatalf("pr = %+v, %v", pr, err)
	}
	if _, err := parseIssue([]byte(`{"data":{"repository":{"issueOrPullRequest":null}}}`)); !errors.Is(err, ErrNoPR) {
		t.Fatalf("none = %v", err)
	}
}

func TestParseThreads(t *testing.T) {
	ts, err := parseThreads([]byte(`{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
		{"id":"T1","isResolved":false,"isOutdated":false,"path":"a.go","line":12,"startLine":10,"diffSide":"RIGHT",
		 "comments":{"nodes":[{"author":{"login":"ann"},"body":"Rename this","url":"c1"},{"author":{"login":"me"},"body":"OK"}]}},
		{"id":"T2","isResolved":true,"path":"b.go","line":null,"startLine":null,"originalLine":4,"originalStartLine":null,"diffSide":"LEFT",
		 "comments":{"nodes":[]}},
		{"id":"T3","path":"c.go","line":null,"originalLine":null,"comments":{"nodes":[]}}]}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 2 {
		t.Fatalf("%d threads, want 2 (a thread on no line is left out)", len(ts))
	}
	if a := ts[0]; a.ID != "T1" || a.Line != 12 || a.StartLine != 10 || a.OldSide || a.Resolved || a.Outdated || len(a.Comments) != 2 || a.Comments[0].Author != "ann" {
		t.Fatalf("first = %+v", a)
	}
	if b := ts[1]; b.Line != 4 || b.StartLine != 0 || !b.OldSide || !b.Resolved || !b.Outdated {
		t.Fatalf("outdated = %+v", b)
	}
}

func TestRemoteHostPath(t *testing.T) {
	for raw, want := range map[string]string{
		"https://github.com/o/r.git":          "github.com o/r",
		"git@github.com:o/r.git":              "github.com o/r",
		"ssh://git@GHE.corp.example/team/app": "ghe.corp.example team/app",
		"https://gitlab.com/group/sub/r":      "",
		"/local/path":                         "",
		"file:///tmp/r":                       "",
	} {
		host, path, ok := remoteHostPath(raw)
		got := ""
		if ok {
			got = host + " " + path
		}
		if got != want {
			t.Errorf("remoteHostPath(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestGitHubProblem(t *testing.T) {
	for _, c := range []struct {
		err  error
		want GHProblem
	}{
		{nil, GHFine},
		{ErrNoDefaultRepo, GHNoDefault},
		{fmt.Errorf("%w: %w", ErrNoGH, exec.ErrNotFound), GHMissing},
		{fmt.Errorf("%w: bash: gh: command not found", ErrNoGH), GHMissing},
		{fmt.Errorf("%w: To get started with GitHub CLI, please run:  gh auth login", ErrNoGH), GHSignedOut},
		{fmt.Errorf("%w: HTTP 502", ErrNoGH), GHOther},
	} {
		if got := GitHubProblem(c.err); got != c.want {
			t.Errorf("GitHubProblem(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestIssueNumber(t *testing.T) {
	if n, err := issueNumber("Creating issue in o/r\n\nhttps://github.com/o/r/issues/42"); err != nil || n != 42 {
		t.Fatalf("issueNumber = %d, %v", n, err)
	}
	if _, err := issueNumber("nothing"); err == nil {
		t.Fatal("no number read from nothing")
	}
}

func TestWorktreeOfBranch(t *testing.T) {
	r, git := repo(t)
	// A new branch: made, in a worktree of its own.
	p, err := r.WorktreeOfBranch("12-crash", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	// Asked again: the same worktree.
	if again, err := r.WorktreeOfBranch("12-crash", "HEAD"); err != nil || again != p {
		t.Fatalf("again = %q, %v; want %q", again, err, p)
	}
	// The worktree removed, the branch kept: checked out again.
	git("worktree", "remove", p)
	if back, err := r.WorktreeOfBranch("12-crash", "HEAD"); err != nil || back != p {
		t.Fatalf("back = %q, %v; want %q", back, err, p)
	}
}
