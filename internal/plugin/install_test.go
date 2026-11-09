package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writePlugin(t, dir, "", files)
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "one"}} {
		if _, err := git(context.Background(), dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestInstallUpdateRemove(t *testing.T) {
	_, _, dirs := newRegistry(t)
	repo := gitRepo(t, map[string]string{"plugin.json": `{"name": "hello", "version": "1.0.0"}`})
	m, err := Install(context.Background(), dirs.Plugins, repo, "")
	if err != nil || m.Name != "hello" {
		t.Fatalf("install: %v %v", m, err)
	}
	recs := ReadRecords(dirs.Plugins)
	if recs["hello"].URL != repo || len(recs["hello"].Commit) != 40 {
		t.Fatalf("records = %+v", recs)
	}
	if changed, err := Update(context.Background(), dirs.Plugins, "hello"); err != nil || changed {
		t.Fatalf("update with nothing new: %v %v", changed, err)
	}
	writePlugin(t, repo, "", map[string]string{"plugin.json": `{"name": "hello", "version": "1.1.0"}`})
	git(context.Background(), repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qam", "two")
	if latest, _ := Latest(context.Background(), recs["hello"]); latest == recs["hello"].Commit {
		t.Fatal("Latest did not see the new commit")
	}
	if changed, err := Update(context.Background(), dirs.Plugins, "hello"); err != nil || !changed {
		t.Fatalf("update: %v %v", changed, err)
	}
	if m, _ := ReadManifest(filepath.Join(dirs.Plugins, "hello")); m.Version != "1.1.0" {
		t.Fatalf("version after update = %s", m.Version)
	}
	// Another machine's records install what is missing and remove the rest.
	other := t.TempDir()
	installed, removed, err := SyncInstall(context.Background(), other, ReadRecords(dirs.Plugins))
	if err != nil || len(installed) != 1 || len(removed) != 0 {
		t.Fatalf("sync: %v %v %v", installed, removed, err)
	}
	_, removed, _ = SyncInstall(context.Background(), other, Records{})
	if len(removed) != 1 {
		t.Fatalf("sync removal: %v", removed)
	}
	if err := Remove(dirs.Plugins, "hello"); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadRecords(dirs.Plugins)["hello"]; ok {
		t.Fatal("record kept")
	}
}

func TestSyncAgreesBothWays(t *testing.T) {
	ctx := context.Background()
	repoA := gitRepo(t, map[string]string{"plugin.json": `{"name": "alpha"}`})
	repoB := gitRepo(t, map[string]string{"plugin.json": `{"name": "beta"}`})
	here, there := t.TempDir(), t.TempDir()
	// Installed here, synced: the account gets it.
	if _, err := Install(ctx, here, repoA, ""); err != nil {
		t.Fatal(err)
	}
	account, _, _, err := Sync(ctx, here, Records{})
	if err != nil || len(account) != 1 {
		t.Fatalf("first sync here: %v %v", account, err)
	}
	// The other machine, with beta of its own, gets alpha and keeps beta.
	if _, err := Install(ctx, there, repoB, ""); err != nil {
		t.Fatal(err)
	}
	account, installed, _, err := Sync(ctx, there, account)
	if err != nil || len(account) != 2 || len(installed) != 1 || installed[0] != "alpha" {
		t.Fatalf("sync there: %v %v %v", account, installed, err)
	}
	// Back here, beta comes too.
	account, installed, _, _ = Sync(ctx, here, account)
	if len(installed) != 1 || installed[0] != "beta" || len(account) != 2 {
		t.Fatalf("sync here again: %v %v", account, installed)
	}
	// Removed here: removed there at its next sync, not installed back here.
	Remove(here, "alpha")
	account, _, _, _ = Sync(ctx, here, account)
	if _, ok := account["alpha"]; ok {
		t.Fatalf("alpha came back: %v", account)
	}
	_, _, removed, _ := Sync(ctx, there, account)
	if len(removed) != 1 || removed[0] != "alpha" {
		t.Fatalf("removed there: %v", removed)
	}
}

func TestFetchIndexNotFound(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	old := IndexURL
	IndexURL = srv.URL
	defer func() { IndexURL = old }()
	entries, err := FetchIndex(context.Background(), nil)
	if err != nil || len(entries) != 0 {
		t.Fatalf("FetchIndex = %v, %v; want no entries and no error", entries, err)
	}
}

func TestSplitSpec(t *testing.T) {
	if url, ref, byName := SplitSpec("me/thing@v2"); url != "https://github.com/me/thing" || ref != "v2" || byName {
		t.Fatalf("SplitSpec = %s %s %v", url, ref, byName)
	}
	if url, ref, _ := SplitSpec("git@github.com:me/thing.git"); url != "git@github.com:me/thing.git" || ref != "" {
		t.Fatalf("SplitSpec ssh = %s %s", url, ref)
	}
	if _, _, byName := SplitSpec("prettier"); !byName {
		t.Fatal("a bare name is of the index")
	}
}

func TestCLINewAndDev(t *testing.T) {
	r, _, dirs := newRegistry(t)
	work := filepath.Join(t.TempDir(), "greet")
	var out, errb strings.Builder
	if code := RunCLI([]string{"new", "greet", work}, dirs, &out, &errb); code != 0 {
		t.Fatalf("new: %s", errb.String())
	}
	if data, _ := os.ReadFile(filepath.Join(work, ManifestFile)); !strings.Contains(string(data), `"author":`) {
		t.Errorf("the scaffold's manifest has no author: %s", data)
	}
	if code := RunCLI([]string{"dev", work}, dirs, &out, &errb); code != 0 {
		t.Fatalf("dev: %s", errb.String())
	}
	out.Reset()
	RunCLI([]string{"list"}, dirs, &out, &errb)
	if !strings.Contains(out.String(), "greet") || !strings.Contains(out.String(), "dev:") {
		t.Fatalf("list = %s", out.String())
	}
	r.Load()
	if _, err := r.Run("greet.hello", nil); err != nil {
		t.Fatalf("the scaffold's command: %v", err)
	}
	if code := RunCLI([]string{"remove", "greet"}, dirs, &out, &errb); code != 0 {
		t.Fatalf("remove: %s", errb.String())
	}
	if _, err := os.Stat(work); err != nil {
		t.Fatal("removing the link removed the folder")
	}
}
