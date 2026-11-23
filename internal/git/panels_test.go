package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func branchNamed(t *testing.T, r *Repo, name string) Branch {
	t.Helper()
	bs, err := r.PanelBranches()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bs {
		if b.Name == name {
			return b
		}
	}
	t.Fatalf("missing branch %s in %+v", name, bs)
	return Branch{}
}

func TestPanelBranchMetadataAndHistory(t *testing.T) {
	r, git := repo(t)
	git("update-ref", "refs/remotes/origin/main", "HEAD")
	git("config", "branch.main.remote", "origin")
	git("config", "branch.main.merge", "refs/heads/main")
	git("config", "remote.origin.url", filepath.Join(t.TempDir(), "remote.git"))
	git("config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	git("switch", "-q", "-c", "incoming")
	git("commit", "--allow-empty", "-qm", "incoming one")
	git("commit", "--allow-empty", "-qm", "incoming two")
	git("update-ref", "refs/remotes/origin/main", "HEAD")
	git("switch", "-q", "main")
	git("commit", "--allow-empty", "-qm", "local only")
	b := branchNamed(t, r, "main")
	if !b.Current || b.Remote || b.Hash == "" || b.Upstream != "origin/main" || b.Ahead != 1 || b.Behind != 2 || b.Gone {
		t.Fatalf("branch metadata %+v", b)
	}
	incoming, err := r.LogRange("main", "origin/main", 10)
	if err != nil || len(incoming) != 2 || incoming[0].Subject != "incoming two" || incoming[1].Subject != "incoming one" {
		t.Fatalf("incoming %+v, %v", incoming, err)
	}
	outgoing, err := r.LogRange("origin/main", "main", 10)
	if err != nil || len(outgoing) != 1 || outgoing[0].Subject != "local only" {
		t.Fatalf("outgoing %+v, %v", outgoing, err)
	}
	history, err := r.LogRef("origin/main", 2)
	if err != nil || len(history) != 2 || history[0].Hash != incoming[0].Hash {
		t.Fatalf("history %+v, %v", history, err)
	}
	git("update-ref", "-d", "refs/remotes/origin/main")
	if b := branchNamed(t, r, "main"); !b.Gone || b.Upstream != "origin/main" {
		t.Fatalf("gone %+v", b)
	}
}

func TestPanelBranchActions(t *testing.T) {
	r, git := repo(t)
	git("branch", "topic")
	if err := r.RenameBranch("topic", "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteBranch("renamed", false); err != nil {
		t.Fatal(err)
	}
	git("switch", "-qc", "unmerged")
	git("commit", "--allow-empty", "-qm", "unmerged work")
	git("switch", "-q", "main")
	if err := r.DeleteBranch("unmerged", false); err == nil {
		t.Fatal("deleted unmerged branch")
	}
	if err := r.DeleteBranch("unmerged", true); err != nil {
		t.Fatal(err)
	}
	git("update-ref", "refs/remotes/origin/topic", "HEAD")
	git("config", "remote.origin.url", "unused")
	git("config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	if err := r.CheckoutTracking("origin/topic", "tracked"); err != nil {
		t.Fatal(err)
	}
	if b := branchNamed(t, r, "tracked"); !b.Current || b.Upstream != "origin/topic" {
		t.Fatalf("tracking %+v", b)
	}
	if err := r.CheckoutTracking("origin/topic", "main"); err == nil {
		t.Fatal("overwrote local branch")
	}
}

func TestPanelRemoteActions(t *testing.T) {
	r, git := repo(t)
	path := filepath.Join(t.TempDir(), "remote with spaces.git")
	git("init", "-q", "--bare", path)
	if err := r.AddRemote("origin", path); err != nil {
		t.Fatal(err)
	}
	git("push", "-q", "origin", "main")
	if err := r.SetRemoteURL("origin", path+".push", true); err != nil {
		t.Fatal(err)
	}
	git("config", "--add", "remote.origin.pushurl", path+".second")
	rs, err := r.RemoteList()
	if err != nil || len(rs) != 1 {
		t.Fatalf("remotes %+v, %v", rs, err)
	}
	if got := rs[0]; got.Name != "origin" || len(got.FetchURLs) != 1 || got.FetchURLs[0] != path || len(got.PushURLs) != 2 || got.PushURLs[0] != path+".push" {
		t.Fatalf("URLs %+v", got)
	}
	if err := r.SetRemoteURL("origin", path, false); err != nil {
		t.Fatal(err)
	}
	if err := r.FetchRemote("origin", false); err != nil {
		t.Fatal(err)
	}
	git("update-ref", "refs/remotes/origin/stale", "HEAD")
	if err := r.PruneRemote("origin"); err != nil {
		t.Fatal(err)
	}
	bs, err := r.PanelBranches()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bs {
		if b.Name == "origin/stale" {
			t.Fatal("prune kept stale branch")
		}
	}
	git("update-ref", "refs/remotes/origin/stale", "HEAD")
	if err := r.FetchRemote("origin", true); err != nil {
		t.Fatal(err)
	}
	bs, err = r.PanelBranches()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bs {
		if b.Name == "origin/stale" {
			t.Fatal("fetch prune kept stale branch")
		}
	}
	if err := r.RemoveRemote("origin"); err != nil {
		t.Fatal(err)
	}
	rs, err = r.RemoteList()
	if err != nil || len(rs) != 0 {
		t.Fatalf("after removal %+v, %v", rs, err)
	}
}

func TestPanelArgumentsAndMissingRefs(t *testing.T) {
	r, _ := repo(t)
	actions := []struct {
		name string
		run  func() error
	}{
		{"rename", func() error { return r.RenameBranch("main", "--force") }},
		{"delete", func() error { return r.DeleteBranch("--all", false) }},
		{"add", func() error { return r.AddRemote("--mirror", "x") }},
		{"url", func() error { return r.AddRemote("origin", "--upload-pack=bad") }},
		{"remove", func() error { return r.RemoveRemote("--all") }},
		{"set", func() error { return r.SetRemoteURL("origin", "\ninvalid", false) }},
		{"fetch", func() error { return r.FetchRemote("--all", false) }},
		{"prune", func() error { return r.PruneRemote("--help") }},
		{"track", func() error { return r.CheckoutTracking("--bad", "new") }},
	}
	for _, a := range actions {
		t.Run(a.name, func(t *testing.T) {
			if err := a.run(); err == nil {
				t.Fatal("accepted unsafe argument")
			}
		})
	}
	for _, ref := range []string{"--all", "missing", "HEAD\n--all"} {
		if _, err := r.LogRef(ref, 10); err == nil {
			t.Errorf("accepted ref %q", ref)
		}
	}
	if _, err := r.LogRange("main", "--all", 10); err == nil {
		t.Fatal("accepted range option")
	}
	if _, err := r.LogRef("main", 0); err == nil || !strings.Contains(err.Error(), "positive") {
		t.Fatalf("invalid limit: %v", err)
	}
	if b := branchNamed(t, r, "main"); !b.Current {
		t.Fatal("argument validation mutated current branch")
	}
}

func TestPanelUpstreamRefWithSameNamedLocalBranch(t *testing.T) {
	r, git := repo(t)
	// A local origin/main must never replace the configured remote upstream.
	git("branch", "origin/main")
	git("switch", "-qc", "remote-topic")
	git("commit", "--allow-empty", "-qm", "remote incoming")
	git("update-ref", "refs/remotes/origin/main", "HEAD")
	git("switch", "-q", "main")
	git("commit", "--allow-empty", "-qm", "local outgoing")
	git("config", "remote.origin.url", filepath.Join(t.TempDir(), "remote.git"))
	git("config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	git("config", "branch.main.remote", "origin")
	git("config", "branch.main.merge", "refs/heads/main")
	b := branchNamed(t, r, "main")
	if b.UpstreamRef != "refs/remotes/origin/main" || b.Ahead != 1 || b.Behind != 1 {
		t.Fatalf("upstream metadata %+v", b)
	}
	incoming, err := r.LogRange("refs/heads/main", b.UpstreamRef, 10)
	if err != nil || len(incoming) != 1 || incoming[0].Subject != "remote incoming" {
		t.Fatalf("incoming %+v, %v", incoming, err)
	}
	outgoing, err := r.LogRange(b.UpstreamRef, "refs/heads/main", 10)
	if err != nil || len(outgoing) != 1 || outgoing[0].Subject != "local outgoing" {
		t.Fatalf("outgoing %+v, %v", outgoing, err)
	}
}

func TestPanelPullSelectedBranch(t *testing.T) {
	for _, current := range []bool{true, false} {
		t.Run(fmt.Sprint("current=", current), func(t *testing.T) {
			remote, upstream := repo(t)
			r, local := repo(t)
			local("remote", "add", "source", remote.Root)
			local("fetch", "source")
			local("switch", "-C", "selected", "--track", "source/main")
			if !current {
				local("switch", "main")
			}
			head := r.Head()
			upstream("commit", "--allow-empty", "-qm", "new remote commit")
			want := remote.Head()
			if err := r.PullBranch("selected"); err != nil {
				t.Fatal(err)
			}
			if got := branchNamed(t, r, "selected"); got.Hash != want || got.Ahead != 0 || got.Behind != 0 {
				t.Fatalf("branch after pull: %+v", got)
			}
			if !current && r.Head() != head {
				t.Fatal("pull changed active checkout")
			}
			if err := r.PullBranch("selected"); err != nil {
				t.Fatalf("up-to-date pull: %v", err)
			}
			if !current {
				local("switch", "selected")
			}
			local("commit", "--allow-empty", "-qm", "local divergence")
			selected := r.Head()
			if !current {
				local("switch", "main")
			}
			if err := r.PullBranch("selected"); err != nil {
				t.Fatalf("locally ahead pull: %v", err)
			}
			if !current {
				local("switch", "main")
			}
			upstream("commit", "--allow-empty", "-qm", "remote divergence")
			if err := r.PullBranch("selected"); err == nil {
				t.Fatal("diverged pull succeeded")
			}
			if got := branchNamed(t, r, "selected"); got.Hash != selected {
				t.Fatal("diverged pull overwrote local branch")
			}
			if !current && r.Head() != head {
				t.Fatal("failed pull changed active checkout")
			}
		})
	}
}

func TestPanelPullBranchWithConcurrentFetch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Git wrapper needs a POSIX shell")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	remote, upstream := repo(t)
	r, local := repo(t)
	local("remote", "add", "source", remote.Root)
	local("fetch", "source")
	local("branch", "--track", "selected", "source/main")
	upstream("commit", "--allow-empty", "-qm", "upstream update")
	want := remote.Head()
	upstream("switch", "-qc", "other")
	upstream("commit", "--allow-empty", "-qm", "concurrent update")
	other := remote.Head()

	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAYOI_TEST_REAL_GIT", realGit)
	bin := t.TempDir()
	// Run another fetch after the remote fetch succeeds, before PullBranch can read its result.
	script := `#!/bin/sh
"$MAYOI_TEST_REAL_GIT" "$@" || exit $?
if [ "$1" = fetch ]; then
    for arg do
        if [ "$arg" = source ]; then
            exec "$MAYOI_TEST_REAL_GIT" fetch source refs/heads/other
        fi
    done
fi
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := r.PullBranch("selected"); err != nil {
		t.Fatal(err)
	}
	if got := branchNamed(t, r, "selected"); got.Hash != want || got.Ahead != 0 || got.Behind != 0 {
		t.Fatalf("selected branch after concurrent fetch: %+v, want %s with no divergence", got, want)
	}
	if got, err := r.panelCommit("FETCH_HEAD"); err != nil || got != other {
		t.Fatalf("concurrent fetch result: %q, %v, want %s", got, err, other)
	}
	refs, err := r.run(nil, "for-each-ref", "refs/mayoi/pull-branch")
	if err != nil || len(refs) != 0 {
		t.Fatalf("temporary refs after pull: %q, %v", refs, err)
	}
}

func TestPanelBranchWorktreePaths(t *testing.T) {
	r, git := repo(t)
	wt := filepath.Join(t.TempDir(), "linked")
	git("worktree", "add", "-q", "-b", "linked", wt)
	linked, err := Open(wt)
	if err != nil {
		t.Fatal(err)
	}
	// Each side marks the other's branch, never its own.
	for _, c := range []struct {
		r            *Repo
		own, other   string
		otherWantDir string
	}{{r, "main", "linked", "linked"}, {linked, "linked", "main", filepath.Base(r.Root)}} {
		if got := branchNamed(t, c.r, c.own); got.WorktreePath != "" {
			t.Fatalf("%s from its own worktree: %q, want none", c.own, got.WorktreePath)
		}
		if got := branchNamed(t, c.r, c.other); filepath.Base(got.WorktreePath) != c.otherWantDir {
			t.Fatalf("%s from %s: %q, want its worktree", c.other, c.own, got.WorktreePath)
		}
	}
}

func TestPanelPullBranchGuards(t *testing.T) {
	remote, upstream := repo(t)
	r, local := repo(t)
	local("remote", "add", "source", remote.Root)
	local("fetch", "source")
	local("branch", "--track", "selected", "source/main")
	local("worktree", "add", filepath.Join(t.TempDir(), "linked"), "selected")
	before := branchNamed(t, r, "selected").Hash
	upstream("commit", "--allow-empty", "-qm", "incoming")
	if err := r.PullBranch("selected"); err == nil {
		t.Fatal("updated branch checked out in another worktree")
	}
	if branchNamed(t, r, "selected").Hash != before {
		t.Fatal("occupied branch moved")
	}
	for _, name := range []string{"main", "missing", "-option", "selected:other"} {
		if err := r.PullBranch(name); err == nil {
			t.Fatalf("pull %q should fail", name)
		}
	}
	local("branch", "--track", "gone", "source/main")
	upstream("switch", "-c", "replacement")
	upstream("branch", "-D", "main")
	if err := r.PullBranch("gone"); err == nil {
		t.Fatal("deleted upstream pull succeeded")
	}
}

func TestPanelCurrentBranchPrecedesNewerBranches(t *testing.T) {
	r, git := repo(t)
	t.Setenv("GIT_COMMITTER_DATE", time.Now().Add(time.Hour).Format(time.RFC3339))
	git("switch", "-qc", "newer")
	git("commit", "--allow-empty", "-qm", "newer branch")
	git("switch", "-q", "main")
	branches, err := r.PanelBranches()
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 || branches[0].Name != "main" || !branches[0].Current {
		t.Fatalf("current branch not first: %+v", branches)
	}
	// The picker query stays lightweight and does not compute tracking metadata.
	picker, err := r.BranchList()
	if err != nil {
		t.Fatal(err)
	}
	for _, branch := range picker {
		if branch.Hash != "" || branch.UpstreamRef != "" {
			t.Fatalf("picker loaded panel metadata: %+v", branch)
		}
	}
}

func TestConfiguredRemoteURLs(t *testing.T) {
	r, run := repo(t)
	if urls, err := r.ConfiguredRemoteURLs(); err != nil || len(urls) != 0 {
		t.Fatalf("no remotes: %v %v", urls, err)
	}
	run("remote", "add", "origin", "git@github.com:me/app.git")
	run("remote", "add", "my.fork", "https://example.com/fork.git")
	run("remote", "set-url", "--add", "origin", "https://mirror.example.com/app.git")
	urls, err := r.ConfiguredRemoteURLs()
	if err != nil {
		t.Fatal(err)
	}
	if urls["origin"] != "git@github.com:me/app.git" || urls["my.fork"] != "https://example.com/fork.git" || len(urls) != 2 {
		t.Fatalf("urls %v", urls)
	}
}
