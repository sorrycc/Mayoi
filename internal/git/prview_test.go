package git

import (
	"errors"
	"testing"
)

func TestParsePRView(t *testing.T) {
	pr, err := parsePRView([]byte(`{"data":{"repository":{"mergeCommitAllowed":false,"squashMergeAllowed":true,"rebaseMergeAllowed":true,
		"pullRequest":{"id":"PR_1","number":9,"title":"Fix","state":"OPEN","isDraft":false,"headRefName":"fix","baseRefName":"main",
		"author":{"login":"ann","avatarUrl":"a.png"},"mergeable":"CONFLICTING","mergeStateStatus":"DIRTY","reviewDecision":"CHANGES_REQUESTED",
		"labels":{"nodes":[]},"assignees":{"nodes":[]},
		"reviewRequests":{"nodes":[{"requestedReviewer":{"__typename":"User","login":"bob"}},{"requestedReviewer":{"__typename":"Team","name":"core"}},
			{"requestedReviewer":{"__typename":"User","login":"cy"}}]},
		"latestReviews":{"nodes":[{"author":{"login":"cy"},"state":"CHANGES_REQUESTED"}]},
		"closingIssuesReferences":{"nodes":[{"number":3,"title":"Bug"}]},
		"comments":{"totalCount":1,"nodes":[{"author":{"login":"bob"},"body":"Looks off","createdAt":"2026-01-02T00:00:00Z"}]},
		"reviews":{"nodes":[{"author":{"login":"cy"},"state":"CHANGES_REQUESTED","body":"Rename","submittedAt":"2026-01-01T00:00:00Z"},
			{"author":{"login":"cy"},"state":"COMMENTED","body":"","submittedAt":"2026-01-03T00:00:00Z"}]},
		"commits":{"totalCount":1,"nodes":[{"commit":{"oid":"abc","messageHeadline":"Fix it","author":{"name":"Ann","user":{"login":"ann"}}}}]}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 9 || pr.Mergeable != "CONFLICTING" || pr.Avatars["ann"] != "a.png" || len(pr.Linked) != 1 {
		t.Fatalf("pr = %+v", pr)
	}
	if len(pr.Methods) != 2 || pr.Methods[0] != "SQUASH" {
		t.Fatalf("methods = %v", pr.Methods)
	}
	// The reviewer who reviewed first, then those asked who have not.
	want := []PRReviewer{{Login: "cy", State: "CHANGES_REQUESTED"}, {Login: "bob", State: "REQUESTED"}, {Login: "core", State: "REQUESTED", Team: true}}
	if len(pr.Reviewers) != len(want) {
		t.Fatalf("reviewers = %+v", pr.Reviewers)
	}
	for i, r := range want {
		if pr.Reviewers[i] != r {
			t.Fatalf("reviewer %d = %+v, want %+v", i, pr.Reviewers[i], r)
		}
	}
	// The review of line comments only is left out; the rest by time.
	if len(pr.Events) != 2 || pr.Events[0].Review != "CHANGES_REQUESTED" || pr.Events[1].Body != "Looks off" {
		t.Fatalf("events = %+v", pr.Events)
	}
	if len(pr.Commits) != 1 || pr.Commits[0].Login != "ann" || pr.Commits[0].Subject != "Fix it" {
		t.Fatalf("commits = %+v", pr.Commits)
	}
	if _, err := parsePRView([]byte(`{"data":{"repository":{"pullRequest":null}}}`)); !errors.Is(err, ErrNoPR) {
		t.Fatalf("none = %v", err)
	}
}
