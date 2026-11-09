package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RecordsFile is the file in the plugins folder where Mayoi notes where
// each plugin it installed came from.
const RecordsFile = "plugins.json"

// Source is where an installed plugin came from.
type Source struct {
	URL    string `json:"url"`
	Ref    string `json:"ref,omitempty"`
	Commit string `json:"commit,omitempty"`
}

// Records are the sources of the installed plugins, by name.
type Records map[string]Source

// ReadRecords reads plugins.json of a plugins folder.
func ReadRecords(dir string) Records {
	recs := Records{}
	if data, err := os.ReadFile(filepath.Join(dir, RecordsFile)); err == nil {
		json.Unmarshal(data, &recs)
	}
	return recs
}

// WriteRecords writes plugins.json.
func WriteRecords(dir string, recs Records) error {
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, RecordsFile), append(data, '\n'), 0o644)
}

// IndexURL is the index of plugins, from the mayoi-editor organisation.
var IndexURL = "https://raw.githubusercontent.com/mayoi-editor/plugins/main/index.json"

// IndexEntry is a plugin the index lists.
type IndexEntry struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"displayName,omitempty"`
	Description string   `json:"description"`
	Author      string   `json:"author,omitempty"`
	Repository  string   `json:"repository"`
	Tags        []string `json:"tags,omitempty"`
}

// FetchIndex reads the index of plugins.
func FetchIndex(ctx context.Context, client *http.Client) ([]IndexEntry, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, "GET", IndexURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// No index is published yet: there are no plugins to list.
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("the index answered %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var entries []IndexEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		var wrapped struct {
			Plugins []IndexEntry `json:"plugins"`
		}
		if json.Unmarshal(data, &wrapped) != nil {
			return nil, fmt.Errorf("the index does not read: %v", err)
		}
		entries = wrapped.Plugins
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// SplitSpec reads what mayoi plugin install takes: a Git URL, owner/repo
// of GitHub, or a name of the index, each with @ref after it or not.
func SplitSpec(spec string) (url, ref string, byName bool) {
	if i := strings.LastIndex(spec, "@"); i > 0 && i > strings.LastIndexAny(spec, "/:") {
		spec, ref = spec[:i], spec[i+1:]
	}
	switch {
	case strings.Contains(spec, "://") || strings.HasPrefix(spec, "git@") || strings.HasPrefix(spec, "/") || strings.HasPrefix(spec, "."):
		return spec, ref, false
	case strings.Count(spec, "/") == 1:
		return "https://github.com/" + spec, ref, false
	}
	return spec, ref, true
}

// git runs git in dir, returning what it printed or why it failed.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// Install clones a plugin into the plugins folder, checks its manifest,
// and records where it came from. A plugin of the same name is replaced.
func Install(ctx context.Context, dir, url, ref string) (*Manifest, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(dir, ".install-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	args := []string{"clone", "--depth", "1", "--quiet"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	if _, err := git(ctx, dir, append(args, url, tmp)...); err != nil {
		if ref == "" {
			return nil, err
		}
		// A commit, which --branch does not take.
		if _, err2 := git(ctx, dir, "clone", "--quiet", url, tmp); err2 != nil {
			return nil, err
		}
		if _, err := git(ctx, tmp, "checkout", "--quiet", ref); err != nil {
			return nil, err
		}
	}
	m, err := ReadManifest(tmp)
	if err != nil {
		return nil, fmt.Errorf("%s is not a Mayoi plugin: %v", url, err)
	}
	commit, _ := git(ctx, tmp, "rev-parse", "HEAD")
	dest := filepath.Join(dir, m.Name)
	if fi, err := os.Lstat(dest); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s is linked for development by mayoi plugin dev: mayoi plugin remove %s first", m.Name, m.Name)
	}
	old := dest + ".old"
	os.RemoveAll(old)
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, old); err != nil {
			return nil, err
		}
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Rename(old, dest)
		return nil, err
	}
	os.RemoveAll(old)
	recs := ReadRecords(dir)
	recs[m.Name] = Source{URL: url, Ref: ref, Commit: commit}
	return m, WriteRecords(dir, recs)
}

// Update brings an installed plugin up to its ref, or its branch's last
// commit: it reinstalls it from its source. It reports whether the
// commit changed.
func Update(ctx context.Context, dir, name string) (bool, error) {
	recs := ReadRecords(dir)
	src, ok := recs[name]
	if !ok {
		return false, fmt.Errorf("%s was not installed from Git", name)
	}
	m, err := Install(ctx, dir, src.URL, src.Ref)
	if err != nil {
		return false, err
	}
	return ReadRecords(dir)[m.Name].Commit != src.Commit, nil
}

// Latest is the last commit of an installed plugin's source, without
// installing it, to tell whether an update is waiting.
func Latest(ctx context.Context, src Source) (string, error) {
	ref := src.Ref
	if ref == "" {
		ref = "HEAD"
	}
	out, err := git(ctx, "", "ls-remote", src.URL, ref)
	if err != nil {
		return "", err
	}
	commit, _, _ := strings.Cut(out, "\t")
	if commit == "" {
		// A commit for its ref: it never moves.
		return src.Commit, nil
	}
	return commit, nil
}

// Remove deletes an installed plugin, or the link of one in development,
// and its record.
func Remove(dir, name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	dest := filepath.Join(dir, name)
	fi, err := os.Lstat(dest)
	if err != nil {
		return fmt.Errorf("%s is not installed", name)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		err = os.Remove(dest)
	} else {
		err = os.RemoveAll(dest)
	}
	if err != nil {
		return err
	}
	recs := ReadRecords(dir)
	if _, ok := recs[name]; ok {
		delete(recs, name)
		return WriteRecords(dir, recs)
	}
	return nil
}

// Link links a folder in development into the plugins folder, so that
// Mayoi loads it from where it is, and again on each change.
func Link(dir, folder string) (*Manifest, error) {
	abs, err := filepath.Abs(folder)
	if err != nil {
		return nil, err
	}
	m, err := ReadManifest(abs)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	dest := filepath.Join(dir, m.Name)
	if fi, err := os.Lstat(dest); err == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			return nil, fmt.Errorf("%s is installed: mayoi plugin remove %s first", m.Name, m.Name)
		}
		os.Remove(dest)
	}
	return m, os.Symlink(abs, dest)
}

// SyncInstall installs the plugins of records that the plugins folder
// lacks, at their commits, and removes those it has from records that
// are no longer in them: settings sync from another machine. It returns
// the names it installed and removed.
func SyncInstall(ctx context.Context, dir string, want Records) (installed, removed []string, err error) {
	have := ReadRecords(dir)
	var errs []error
	for name, src := range want {
		if h, ok := have[name]; ok && h.Commit == src.Commit {
			continue
		}
		if fi, err := os.Lstat(filepath.Join(dir, name)); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			continue // in development here
		}
		ref := src.Commit
		if ref == "" {
			ref = src.Ref
		}
		if _, err := Install(ctx, dir, src.URL, ref); err != nil {
			errs = append(errs, fmt.Errorf("%s: %v", name, err))
			continue
		}
		// Keep the ref it follows, not the commit it was installed at.
		recs := ReadRecords(dir)
		r := recs[name]
		r.Ref = src.Ref
		recs[name] = r
		WriteRecords(dir, recs)
		installed = append(installed, name)
	}
	for name := range have {
		if _, ok := want[name]; !ok {
			if err := Remove(dir, name); err == nil {
				removed = append(removed, name)
			}
		}
	}
	sort.Strings(installed)
	sort.Strings(removed)
	return installed, removed, errors.Join(errs...)
}

// Scaffold writes a new plugin into dir: its manifest, an entry module
// with a command, the types of the mayoi module, and notes for agents.
func Scaffold(dir, name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	if _, err := os.Stat(dir); err == nil {
		if entries, _ := os.ReadDir(dir); len(entries) > 0 {
			return fmt.Errorf("%s is not empty", dir)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	title := humanize(strings.ReplaceAll(name, "-", " "))
	files := map[string]string{
		ManifestFile: fmt.Sprintf(`{
  "name": %q,
  "displayName": %q,
  "version": "0.1.0",
  "description": "",
  "main": "main.ts",
  "activation": ["onCommand:%s.hello"],
  "contributes": {
    "commands": [{ "id": "%s.hello", "title": "Say Hello" }]
  }
}
`, name, title, name, name),
		"main.ts": fmt.Sprintf(`import { commands, editor, window } from "mayoi";

commands.register("%s.hello", async () => {
  const ed = await editor.active();
  const name = ed?.document.name ?? "Mayoi";
  await window.notify(`+"`Hello from %s, in ${name}`"+`);
});
`, name, title),
		"mayoi.d.ts": TypesSource,
		"tsconfig.json": `{
  "compilerOptions": { "target": "es2022", "module": "es2022", "strict": true, "noEmit": true },
  "include": ["*.ts", "mayoi.d.ts"]
}
`,
		"AGENTS.md":  AgentsSource,
		".gitignore": "node_modules/\n",
	}
	for f, body := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Watch loads the plugins again a moment after their folders change, and
// a plugin linked for development as its own files change.
func (r *Registry) Watch() (stop func(), err error) {
	return watchDirs(r, 300*time.Millisecond)
}
