package git

import "testing"

// Incoming returns the commits reachable from HEAD's upstream that HEAD
// lacks, newest first, and nil when there is nothing to compare with.
func TestIncoming(t *testing.T) {
	rt, run := repo(t)
	// No upstream yet: nothing returned.
	if cs := rt.Incoming(10); len(cs) != 0 {
		t.Fatalf("incoming without upstream = %+v", cs)
	}
	run("update-ref", "refs/remotes/origin/main", "HEAD")
	run("config", "branch.main.remote", "origin")
	run("config", "branch.main.merge", "refs/heads/main")
	run("config", "remote.origin.url", "unused")
	run("config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	run("switch", "-q", "-c", "incoming")
	run("commit", "-q", "--allow-empty", "-m", "incoming one")
	run("commit", "-q", "--allow-empty", "-m", "incoming two")
	run("update-ref", "refs/remotes/origin/main", "HEAD")
	run("switch", "-q", "main")
	run("commit", "-q", "--allow-empty", "-m", "local only")
	// A tag sharing the upstream's short name must not shadow @{upstream}.
	run("tag", "origin/main", "HEAD")
	cs := rt.Incoming(10)
	if len(cs) != 2 || cs[0].Subject != "incoming two" || cs[1].Subject != "incoming one" {
		t.Fatalf("incoming = %+v", cs)
	}
	if cs := rt.Incoming(1); len(cs) != 1 || cs[0].Subject != "incoming two" {
		t.Fatalf("incoming(1) = %+v", cs)
	}
	if cs := rt.Incoming(0); len(cs) != 0 {
		t.Fatalf("incoming(0) = %+v", cs)
	}
	// Ahead of upstream only: nothing incoming.
	run("update-ref", "refs/remotes/origin/main", "HEAD")
	if cs := rt.Incoming(10); len(cs) != 0 {
		t.Fatalf("incoming at HEAD = %+v", cs)
	}
	// Gone upstream and detached HEAD both return nil.
	run("update-ref", "-d", "refs/remotes/origin/main")
	if cs := rt.Incoming(10); len(cs) != 0 {
		t.Fatalf("incoming from gone upstream = %+v", cs)
	}
	run("switch", "-q", "--detach")
	if cs := rt.Incoming(10); len(cs) != 0 {
		t.Fatalf("incoming on detached HEAD = %+v", cs)
	}
}
