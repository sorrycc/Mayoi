package git

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Remote retains every configured fetch and push URL, including multiple URLs.
type Remote struct {
	Name                string
	FetchURLs, PushURLs []string
	// URL is its first URL as configured, before any rewriting, read for
	// every remote at once, shown where its details are not.
	URL string
}

// PullBranch fast-forwards the selected local branch from its configured
// upstream. Fetch's non-forced destination protects inactive branches from
// divergence and from updates while checked out in another worktree.
func (r *Repo) PullBranch(name string) error {
	if err := r.panelBranch(name); err != nil {
		return err
	}
	out, err := r.run(nil, "for-each-ref", "--format=%(HEAD)%1f%(upstream:remotename)%1f%(upstream:remoteref)%1f%(refname)", "refs/heads/"+name)
	if err != nil {
		return err
	}
	f := strings.Split(strings.TrimSuffix(string(out), "\n"), "\x1f")
	if len(f) != 4 || f[3] != "refs/heads/"+name || f[1] == "" || f[2] == "" {
		return fmt.Errorf("branch %q has no upstream", name)
	}
	if err := panelArgument("upstream remote", f[1]); err != nil {
		return err
	}
	if err := panelArgument("upstream ref", f[2]); err != nil {
		return err
	}
	if _, err := r.run(nil, "check-ref-format", f[2]); err != nil {
		return err
	}
	if f[0] == "*" {
		return r.Pull()
	}
	if _, err := r.run(nil, "fetch", "--", f[1], f[2]); err != nil {
		return err
	}
	incoming, err := r.panelCommit("FETCH_HEAD")
	if err != nil {
		return err
	}
	if _, err := r.run(nil, "merge-base", "--is-ancestor", incoming, "refs/heads/"+name); err == nil {
		return nil // A locally ahead branch is already up to date.
	}
	_, err = r.run(nil, "fetch", "--", ".", incoming+":refs/heads/"+name)
	return err
}

func (r *Repo) RemoteList() ([]Remote, error) {
	names, err := r.Remotes()
	if err != nil {
		return nil, err
	}
	var remotes []Remote
	for _, name := range names {
		remote, err := r.RemoteURLs(name)
		if err != nil {
			return nil, err
		}
		remotes = append(remotes, remote)
	}
	return remotes, nil
}

// ConfiguredRemoteURLs returns the first URL configured for each remote,
// as written, in one read of the config.
func (r *Repo) ConfiguredRemoteURLs() (map[string]string, error) {
	out, err := r.run(nil, "config", "-z", "--get-regexp", `^remote\..*\.url$`)
	if e := (*Error)(nil); errors.As(err, &e) && len(out) == 0 && e.Stderr == "" {
		// No remote has a URL: config exits 1, saying nothing, for no match.
		return map[string]string{}, nil
	} else if err != nil {
		return nil, err
	}
	urls := map[string]string{}
	for _, rec := range strings.Split(string(out), "\x00") {
		key, url, ok := strings.Cut(rec, "\n")
		if !ok {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".url")
		if _, seen := urls[name]; !seen {
			urls[name] = url
		}
	}
	return urls, nil
}

// RemoteURLs resolves URL rewriting only when a remote's details are shown.
func (r *Repo) RemoteURLs(name string) (Remote, error) {
	if err := r.panelRemote(name); err != nil {
		return Remote{}, err
	}
	fetch, err := r.run(nil, "remote", "get-url", "--all", name)
	if err != nil {
		return Remote{}, err
	}
	push, err := r.run(nil, "remote", "get-url", "--push", "--all", name)
	if err != nil {
		return Remote{}, err
	}
	return Remote{Name: name, FetchURLs: outputLines(fetch), PushURLs: outputLines(push)}, nil
}

func outputLines(out []byte) []string {
	s := strings.TrimSuffix(string(out), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// panelArgument rejects option-like values and line/control separators before
// handing positional user input to Git. Spaces remain valid in local URLs.
func panelArgument(kind, value string) error {
	if value == "" || strings.HasPrefix(value, "-") || strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("invalid %s %q", kind, value)
	}
	return nil
}

func (r *Repo) panelBranch(name string) error {
	if err := panelArgument("branch", name); err != nil {
		return err
	}
	_, err := r.run(nil, "check-ref-format", "refs/heads/"+name)
	return err
}

func (r *Repo) panelRemote(name string) error {
	if err := panelArgument("remote", name); err != nil {
		return err
	}
	_, err := r.run(nil, "check-ref-format", "refs/remotes/"+name+"/branch")
	return err
}

// panelCommit resolves a single revision before constructing ranges; revision
// expressions supplied by the caller never become log options or extra refs.
func (r *Repo) panelCommit(ref string) (string, error) {
	if err := panelArgument("revision", ref); err != nil {
		return "", err
	}
	out, err := r.run(nil, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *Repo) panelLog(revision string, n int) ([]Commit, error) {
	if n <= 0 {
		return nil, fmt.Errorf("commit limit must be positive")
	}
	out, err := r.run(nil, "log", "-z", "--date-order", "--decorate=full", "-n", strconv.Itoa(n), commitFormat, revision, "--")
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, rec := range strings.Split(string(out), "\x00") {
		if c, ok := parseCommit(rec); ok {
			commits = append(commits, c)
		}
	}
	return commits, nil
}

// LogRef returns real history reachable from ref, including merges.
func (r *Repo) LogRef(ref string, n int) ([]Commit, error) {
	hash, err := r.panelCommit(ref)
	if err != nil {
		return nil, err
	}
	return r.panelLog(hash, n)
}

// LogRange returns commits reachable from to but not from from.
func (r *Repo) LogRange(from, to string, n int) ([]Commit, error) {
	a, err := r.panelCommit(from)
	if err != nil {
		return nil, err
	}
	b, err := r.panelCommit(to)
	if err != nil {
		return nil, err
	}
	return r.panelLog(a+".."+b, n)
}

func (r *Repo) RenameBranch(oldName, newName string) error {
	if err := r.panelBranch(oldName); err != nil {
		return err
	}
	if err := r.panelBranch(newName); err != nil {
		return err
	}
	_, err := r.run(nil, "branch", "-m", "--", oldName, newName)
	return err
}

// DeleteBranch refuses unmerged branches unless force is explicitly requested.
func (r *Repo) DeleteBranch(name string, force bool) error {
	if err := r.panelBranch(name); err != nil {
		return err
	}
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := r.run(nil, "branch", flag, "--", name)
	return err
}

func (r *Repo) AddRemote(name, url string) error {
	if err := r.panelRemote(name); err != nil {
		return err
	}
	if err := panelArgument("remote URL", url); err != nil {
		return err
	}
	_, err := r.run(nil, "remote", "add", "--", name, url)
	return err
}

func (r *Repo) RemoveRemote(name string) error {
	if err := r.panelRemote(name); err != nil {
		return err
	}
	_, err := r.run(nil, "remote", "remove", "--", name)
	return err
}

// SetRemoteURL updates the primary URL; other configured URLs are retained.
func (r *Repo) SetRemoteURL(name, url string, push bool) error {
	if err := r.panelRemote(name); err != nil {
		return err
	}
	if err := panelArgument("remote URL", url); err != nil {
		return err
	}
	args := []string{"remote", "set-url"}
	if push {
		args = append(args, "--push")
	}
	_, err := r.run(nil, append(args, "--", name, url)...)
	return err
}

func (r *Repo) FetchRemote(name string, prune bool) error {
	if err := r.panelRemote(name); err != nil {
		return err
	}
	args := []string{"fetch"}
	if prune {
		args = append(args, "--prune")
	}
	_, err := r.run(nil, append(args, "--", name)...)
	return err
}

func (r *Repo) PruneRemote(name string) error {
	if err := r.panelRemote(name); err != nil {
		return err
	}
	_, err := r.run(nil, "remote", "prune", "--", name)
	return err
}

// CheckoutTracking creates a local branch tracking an existing remote ref.
// Git refuses an existing local name or a checkout that would discard changes.
func (r *Repo) CheckoutTracking(remoteBranch, localName string) error {
	if err := r.panelBranch(localName); err != nil {
		return err
	}
	if err := panelArgument("remote branch", remoteBranch); err != nil {
		return err
	}
	ref := "refs/remotes/" + remoteBranch
	if _, err := r.run(nil, "check-ref-format", ref); err != nil {
		return err
	}
	if _, err := r.run(nil, "show-ref", "--verify", ref); err != nil {
		return err
	}
	return r.switchOrCheckout(
		[]string{"switch", "--track", "-c", localName, "--", ref},
		[]string{"checkout", "--track", "-b", localName, ref, "--"},
	)
}

// PanelBranches includes tracking counts for the expanded repository panel.
func (r *Repo) PanelBranches() ([]Branch, error) {
	out, err := r.run(nil, "for-each-ref", "--sort=-committerdate",
		"--format=%(refname)%1f%(committerdate:unix)%1f%(objectname)%1f%(upstream:short)%1f%(upstream:track)%1f%(HEAD)%1f%(symref)%1f%(upstream)%1f%(subject)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	var bs []Branch
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.SplitN(line, "\x1f", 9)
		if len(f) != 9 || f[6] != "" {
			continue
		}
		b := Branch{Hash: f[2], Upstream: f[3], UpstreamRef: f[7], Current: f[5] == "*", Subject: f[8]}
		switch {
		case strings.HasPrefix(f[0], "refs/heads/"):
			b.Name = strings.TrimPrefix(f[0], "refs/heads/")
		case strings.HasPrefix(f[0], "refs/remotes/"):
			b.Name, b.Remote = strings.TrimPrefix(f[0], "refs/remotes/"), true
		default:
			continue
		}
		if seconds, err := strconv.ParseInt(f[1], 10, 64); err == nil {
			b.Time = time.Unix(seconds, 0)
		}
		b.Gone = f[4] == "[gone]"
		for _, track := range strings.Split(strings.Trim(f[4], "[]"), ", ") {
			direction, count, _ := strings.Cut(track, " ")
			switch direction {
			case "ahead":
				b.Ahead, _ = strconv.Atoi(count)
			case "behind":
				b.Behind, _ = strconv.Atoi(count)
			}
		}
		bs = append(bs, b)
	}
	sort.SliceStable(bs, func(i, j int) bool { return bs[i].Current && !bs[j].Current })
	return bs, nil
}
