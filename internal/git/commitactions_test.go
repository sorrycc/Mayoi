package git

import (
	"path/filepath"
	"strings"
	"testing"
)

func actionHash(t *testing.T, r *Repo, ref string) string {
	t.Helper()
	h, err := r.commitRevision(ref)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestCommitPatchRootMergeAndBinary(t *testing.T) {
	r, git := repo(t)
	patch, err := r.CommitPatch("HEAD")
	if err != nil || !strings.Contains(patch, "new file mode") || !strings.Contains(patch, "+bee") {
		t.Fatalf("root patch %q, %v", patch, err)
	}
	git("switch", "-qc", "side")
	write(t, r.Root, "binary", "\x00\x01\x02\x03")
	git("add", ".")
	git("commit", "-qm", "binary")
	git("switch", "-q", "main")
	git("commit", "--allow-empty", "-qm", "main change")
	git("merge", "--no-ff", "-qm", "merge", "side")
	patch, err = r.CommitPatch("HEAD")
	if err != nil || !strings.Contains(patch, "GIT binary patch") {
		t.Fatalf("merge patch %q, %v", patch, err)
	}
	if _, err := r.run([]byte(patch), "apply", "--reverse", "--check", "--binary", "-"); err != nil {
		t.Fatalf("patch cannot be reversed: %v", err)
	}
}

func TestComparePatchDirection(t *testing.T) {
	r, git := repo(t)
	first := actionHash(t, r, "HEAD")
	write(t, r.Root, "b.txt", "new\n")
	git("add", ".")
	git("commit", "-qm", "second")
	patch, err := r.ComparePatch(first, "HEAD")
	if err != nil || !strings.Contains(patch, "-bee\n+new") {
		t.Fatalf("forward patch %q, %v", patch, err)
	}
	patch, err = r.ComparePatch("HEAD", first)
	if err != nil || !strings.Contains(patch, "-new\n+bee") {
		t.Fatalf("reverse patch %q, %v", patch, err)
	}

	for _, invalid := range []string{"", "--output=/tmp/bad", "HEAD\nHEAD", "missing"} {
		if _, err := r.CommitPatch(invalid); err == nil {
			t.Errorf("accepted revision %q", invalid)
		}
		if _, err := r.ComparePatch("HEAD", invalid); err == nil {
			t.Errorf("accepted comparison revision %q", invalid)
		}
	}
}

func TestRemoteWebURL(t *testing.T) {
	hash := strings.Repeat("a", 40)
	for _, tc := range []struct {
		raw, suffix string
	}{
		{"https://user:secret@github.com/org/repo.git?token=secret#fragment", "/org/repo/commit/"},
		{"git@github.com:org/repo.git", "/org/repo/commit/"},
		{"ssh://git@gitlab.com:2222/group/sub/repo.git", "/group/sub/repo/-/commit/"},
		{"https://gitlab.com/group/repo.git", "/group/repo/-/commit/"},
		{"git@bitbucket.org:org/repo.git", "/org/repo/commits/"},
		{"https://bitbucket.org/org/repo.git", "/org/repo/commits/"},
	} {
		got, err := RemoteWebURL(tc.raw, hash)
		if err != nil || !strings.HasSuffix(got, tc.suffix+hash) || strings.Contains(got, "secret") || !strings.HasPrefix(got, "https://") {
			t.Errorf("%q => %q, %v", tc.raw, got, err)
		}
	}
	for _, raw := range []string{"https://unknown.test/org/repo.git", "file:///tmp/repo", "https://github.com/org/../repo", "https://github.com/repo"} {
		if _, err := RemoteWebURL(raw, hash); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	if _, err := RemoteWebURL("https://github.com/org/repo", "HEAD"); err == nil {
		t.Fatal("accepted non-hash")
	}
}

func TestCommitRemoteURLChoosesDecorationUpstreamAndFallback(t *testing.T) {
	r, git := repo(t)
	hash := actionHash(t, r, "HEAD")
	if _, err := r.CommitRemoteURL(Commit{Hash: hash}); err == nil {
		t.Fatal("accepted repository without remotes")
	}
	git("remote", "add", "aaa", "git@github.com:aaa/repo.git")
	git("remote", "add", "origin", "git@github.com:origin/repo.git")
	git("remote", "add", "upstream", "git@github.com:upstream/repo.git")
	git("remote", "add", "team/fork", "git@github.com:fork/repo.git")
	check := func(cm Commit, owner string) {
		t.Helper()
		got, err := r.CommitRemoteURL(cm)
		want := "https://github.com/" + owner + "/repo/commit/" + hash
		if err != nil || got != want {
			t.Fatalf("URL = %q, %v; want %q", got, err, want)
		}
	}
	check(Commit{Hash: hash}, "origin")
	git("update-ref", "refs/remotes/upstream/main", hash)
	git("config", "branch.main.remote", "upstream")
	git("config", "branch.main.merge", "refs/heads/main")
	check(Commit{Hash: hash}, "upstream")
	check(Commit{Hash: hash, Refs: []Ref{{Kind: RefRemote, Name: "team/fork/main"}}}, "fork")
	check(Commit{Hash: hash, Refs: []Ref{{Kind: RefRemote, Name: "origin/main"}, {Kind: RefRemote, Name: "team/fork/main"}}}, "upstream")
	git("config", "--unset", "branch.main.remote")
	git("remote", "remove", "origin")
	check(Commit{Hash: hash}, "aaa")
}

func TestCommitPatchesRemainApplicableWithForcedColor(t *testing.T) {
	r, git := repo(t)
	write(t, r.Root, "b.txt", "changed\n")
	git("commit", "-qam", "second")
	git("config", "color.diff", "always")
	for _, tc := range []struct {
		name string
		read func() (string, error)
	}{
		{"commit", func() (string, error) { return r.CommitPatch("HEAD") }},
		{"comparison", func() (string, error) { return r.ComparePatch("HEAD^", "HEAD") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			patch, err := tc.read()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(patch, "\x1b") {
				t.Fatal("patch contains terminal color escapes")
			}
			if _, err := r.run([]byte(patch), "apply", "--reverse", "--check", "-"); err != nil {
				t.Fatalf("patch cannot be applied: %v", err)
			}
		})
	}
}

func TestFileWebURL(t *testing.T) {
	hash := strings.Repeat("a", 40)
	for _, tc := range []struct {
		raw, ref, path string
		from, to       int
		want           string
	}{
		{"git@github.com:org/repo.git", hash, "a/b.go", 10, 20, "https://github.com/org/repo/blob/" + hash + "/a/b.go#L10-L20"},
		{"git@github.com:org/repo.git", hash, "a/b.go", 10, 10, "https://github.com/org/repo/blob/" + hash + "/a/b.go#L10"},
		{"https://github.com/org/repo", "feat/x", "my file.md", 0, 0, "https://github.com/org/repo/blob/feat/x/my%20file.md"},
		{"https://gitlab.com/g/s/repo.git", hash, "x.go", 3, 7, "https://gitlab.com/g/s/repo/-/blob/" + hash + "/x.go#L3-7"},
		{"git@bitbucket.org:org/repo.git", "main", "x.go", 3, 7, "https://bitbucket.org/org/repo/src/main/x.go#lines-3:7"},
		{"git@bitbucket.org:org/repo.git", "main", "x.go", 3, 3, "https://bitbucket.org/org/repo/src/main/x.go#lines-3"},
	} {
		got, err := FileWebURL(tc.raw, tc.ref, tc.path, tc.from, tc.to)
		if err != nil || got != tc.want {
			t.Errorf("FileWebURL(%q, %q, %q) = %q, %v; want %q", tc.raw, tc.ref, tc.path, got, err, tc.want)
		}
	}
	for _, tc := range [][3]string{
		{"https://unknown.test/org/repo", "main", "x"},
		{"https://github.com/org/repo", "../x", "x"},
		{"https://github.com/org/repo", "-x", "x"},
		{"https://github.com/org/repo", "main", "../x"},
	} {
		if got, err := FileWebURL(tc[0], tc[1], tc[2], 1, 1); err == nil {
			t.Errorf("accepted %q => %q", tc, got)
		}
	}
}

func TestFileRemoteURL(t *testing.T) {
	r, git := repo(t)
	hash := actionHash(t, r, "HEAD")
	path := filepath.Join(r.Root, "b.txt")
	if _, err := r.FileRemoteURL(path, 1, 1, true); err == nil {
		t.Fatal("accepted repository without remotes")
	}
	git("remote", "add", "origin", "git@github.com:origin/repo.git")
	git("remote", "add", "upstream", "git@github.com:upstream/repo.git")
	check := func(permalink bool, want string) {
		t.Helper()
		got, err := r.FileRemoteURL(path, 2, 4, permalink)
		if err != nil || got != want {
			t.Fatalf("URL = %q, %v; want %q", got, err, want)
		}
	}
	check(true, "https://github.com/origin/repo/blob/"+hash+"/b.txt#L2-L4")
	check(false, "https://github.com/origin/repo/blob/"+hash+"/b.txt#L2-L4")
	git("update-ref", "refs/remotes/upstream/topic/x", hash)
	git("config", "branch.main.remote", "upstream")
	git("config", "branch.main.merge", "refs/heads/topic/x")
	check(false, "https://github.com/upstream/repo/blob/topic/x/b.txt#L2-L4")
	check(true, "https://github.com/upstream/repo/blob/"+hash+"/b.txt#L2-L4")
}
