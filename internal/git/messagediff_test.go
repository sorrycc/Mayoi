package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMessageDiff(t *testing.T) {
	r, git := repo(t)
	write(t, r.Root, "a.txt", "one\n2\nthree\n")
	write(t, r.Root, "new.txt", "fresh\n")
	write(t, r.Root, "web/package-lock.json", "{\"lockfileVersion\": 3}\n")
	write(t, r.Root, ".env", "SECRET=abc\n")
	write(t, r.Root, "certs/server.pem", "PRIVATE KEY\n")

	// Nothing staged: every change, the new files' too, but a lockfile's
	// lines and a secret file's.
	d, staged, err := r.MessageDiff(false, 0)
	if err != nil || staged {
		t.Fatalf("staged %v err %v", staged, err)
	}
	for _, want := range []string{"-two", "+2", "New files:", " new.txt", "+fresh", " web/package-lock.json", " .env", " certs/server.pem"} {
		if !strings.Contains(d, want) {
			t.Errorf("all changes lack %q:\n%s", want, d)
		}
	}
	if strings.Contains(d, "lockfileVersion") {
		t.Errorf("all changes show a lockfile's lines:\n%s", d)
	}
	if strings.Contains(d, "SECRET=abc") || strings.Contains(d, "PRIVATE KEY") {
		t.Errorf("all changes show a secret file's lines:\n%s", d)
	}

	// Staged: only what is.
	git("add", "new.txt", "web/package-lock.json")
	d, staged, err = r.MessageDiff(false, 0)
	if err != nil || !staged {
		t.Fatalf("staged %v err %v", staged, err)
	}
	if !strings.Contains(d, "+fresh") || !strings.Contains(d, "package-lock.json") || strings.Contains(d, "-two") ||
		strings.Contains(d, "lockfileVersion") {
		t.Errorf("staged changes:\n%s", d)
	}

	// Amending: the last commit's changes and the staged ones.
	git("commit", "-q", "-m", "second")
	write(t, r.Root, "b.txt", "bee\nbuzz\n")
	git("add", "b.txt")
	d, staged, err = r.MessageDiff(true, 0)
	if err != nil || !staged || !strings.Contains(d, "+fresh") || !strings.Contains(d, "+buzz") || strings.Contains(d, "-two") {
		t.Errorf("amend: staged %v err %v\n%s", staged, err, d)
	}

	// Cut at a line's end.
	d, _, _ = r.MessageDiff(false, 80)
	if len(d) > 80+len("[… the rest of the diff is left out]\n") || !strings.HasSuffix(d, "\n[… the rest of the diff is left out]\n") {
		t.Errorf("cut diff:\n%q", d)
	}

	// Nothing changed.
	os.RemoveAll(filepath.Join(r.Root, ".env"))
	os.RemoveAll(filepath.Join(r.Root, "certs"))
	git("commit", "-q", "-am", "third")
	if d, _, err := r.MessageDiff(false, 0); d != "" || err != nil {
		t.Errorf("no changes: %q %v", d, err)
	}
}

func TestMessageDiffBeforeTheFirstCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	write(t, dir, "a.txt", "first\n")
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d, staged, err := r.MessageDiff(false, 0); err != nil || staged || !strings.Contains(d, "+first") {
		t.Errorf("unstaged: %v %v\n%s", staged, err, d)
	}
	exec.Command("git", "-C", dir, "add", "a.txt").Run()
	if d, staged, err := r.MessageDiff(false, 0); err != nil || !staged || !strings.Contains(d, "+first") {
		t.Errorf("staged: %v %v\n%s", staged, err, d)
	}
}
