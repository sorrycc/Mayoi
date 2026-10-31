package git

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestParsePRInfo(t *testing.T) {
	base, head := strings.Repeat("a", 40), strings.Repeat("b", 40)
	pr, err := parsePRInfo([]byte(`{"id":"PR_1","number":9,"title":"Fix","url":"https://github.com/o/r/pull/9","state":"OPEN","isDraft":true,
		"baseRefName":"main","baseRefOid":"` + base + `","headRefName":"fix","headRefOid":"` + head + `"}`))
	if err != nil || pr.Number != 9 || pr.ID != "PR_1" || !pr.Draft || pr.BaseRef != "main" || pr.HeadOid != head || pr.BaseOid != base {
		t.Fatalf("pr = %+v, %v", pr, err)
	}
	if _, err := parsePRInfo([]byte(`{"number":9,"baseRefOid":"x","headRefOid":"y"}`)); !errors.Is(err, ErrNoGH) {
		t.Fatalf("no commits = %v", err)
	}
	if _, err := parsePRInfo([]byte(`{}`)); !errors.Is(err, ErrNoPR) {
		t.Fatalf("empty = %v", err)
	}
}

func TestParsePRList(t *testing.T) {
	prs, err := parsePRList([]byte(`[{"number":3,"title":"T","headRefName":"h","isDraft":false,"author":{"login":"me"}}]`))
	if err != nil || len(prs) != 1 || prs[0] != (PRListed{Number: 3, Title: "T", Author: "me", HeadRef: "h"}) {
		t.Fatalf("prs = %+v, %v", prs, err)
	}
}

func TestParsePRViewed(t *testing.T) {
	viewed := map[string]bool{}
	next, err := parsePRViewed([]byte(`{"data":{"node":{"files":{"nodes":[{"path":"a.go","viewerViewedState":"VIEWED"},
		{"path":"b.go","viewerViewedState":"DISMISSED"},{"path":"c.go","viewerViewedState":"UNVIEWED"}],
		"pageInfo":{"hasNextPage":true,"endCursor":"Y3Vy"}}}}}`), viewed)
	if err != nil || next != "Y3Vy" || len(viewed) != 1 || !viewed["a.go"] {
		t.Fatalf("next %q viewed %v, %v", next, viewed, err)
	}
	next, _ = parsePRViewed([]byte(`{"data":{"node":{"files":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":"Z"}}}}}`), viewed)
	if next != "" {
		t.Fatalf("last page next %q", next)
	}
}

func TestPRRepoPath(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/o/r/pull/9":        "o/r",
		"https://ghe.example.com/o/r/pull/9/x": "o/r",
		"https://github.com/o/r/issues/9":      "",
		"not a url\x00":                        "",
	} {
		if got := prRepoPath(in); got != want {
			t.Errorf("prRepoPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// A pull request's head, which only refs/pull has, is fetched to the
// private refs, and its changes start where it left the base.
func TestFetchPullRequest(t *testing.T) {
	up, git := repo(t)
	dir := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", up.Root, dir).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	clone, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	fork, _ := up.ResolveCommit("HEAD")
	git("switch", "-q", "-c", "feature")
	write(t, up.Root, "a.txt", "one\nTWO\nthree\n")
	git("commit", "-qam", "feature")
	head, _ := up.ResolveCommit("HEAD")
	git("update-ref", "refs/pull/7/head", head)
	git("switch", "-q", "main")
	write(t, up.Root, "b.txt", "bee 2\n")
	git("commit", "-qam", "main moves on")
	git("branch", "-D", "feature")
	base, _ := up.ResolveCommit("main")

	pr := &PRInfo{Number: 7, URL: "https://github.com/o/r/pull/7", BaseRef: "main", BaseOid: base, HeadOid: head}
	from, err := clone.FetchPullRequest(pr)
	if err != nil {
		t.Fatal(err)
	}
	if from != fork {
		t.Fatalf("from %s, want the fork point %s", from, fork)
	}
	if got, _ := clone.ResolveCommit(PRRef(7, true)); got != head {
		t.Fatalf("head ref %q, want %s", got, head)
	}
	if got, _ := clone.ResolveCommit(PRRef(7, false)); got != base {
		t.Fatalf("base ref %q, want %s", got, base)
	}
	// Once it has them, it does not fetch again.
	pr.BaseRef = "-bad"
	if _, err := clone.FetchPullRequest(pr); err != nil {
		t.Fatalf("fetched again: %v", err)
	}
}
