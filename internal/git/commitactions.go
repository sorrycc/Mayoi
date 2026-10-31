package git

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// CommitPatch returns a binary-capable patch against the first parent. A root
// commit is compared with the empty tree.
func (r *Repo) CommitPatch(hash string) (string, error) {
	hash, err := r.commitRevision(hash)
	if err != nil {
		return "", err
	}
	parents, err := r.run(nil, "rev-list", "--parents", "-n", "1", hash, "--")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(parents))
	args := []string{"diff-tree", "--no-color", "--root", "--no-commit-id", "--binary", "--no-ext-diff", "--no-textconv", "-p", "-r"}
	if len(fields) > 1 {
		args = append(args, fields[1])
	}
	args = append(args, hash, "--")
	out, err := r.run(nil, args...)
	return string(out), err
}

// ComparePatch returns binary-capable changes from one commit to another.
func (r *Repo) ComparePatch(from, to string) (string, error) {
	a, err := r.commitRevision(from)
	if err != nil {
		return "", err
	}
	b, err := r.commitRevision(to)
	if err != nil {
		return "", err
	}
	args := []string{"diff", "--no-color", "--binary", "--no-ext-diff", "--no-textconv", a, b}
	out, err := r.run(nil, append(args, "--")...)
	return string(out), err
}

// RemoteWebURL maps known public hosting services to credential-free HTTPS
// links. hash is a commit ID, never a branch name or a URL path.
func RemoteWebURL(rawURL, hash string) (string, error) {
	if !isCommitID(hash) {
		return "", fmt.Errorf("invalid commit ID")
	}
	host, path, err := remoteRepo(rawURL)
	if err != nil {
		return "", err
	}
	segment := "/commit/"
	switch host {
	case "gitlab.com":
		segment = "/-" + segment
	case "bitbucket.org":
		segment = "/commits/"
	}
	return (&url.URL{Scheme: "https", Host: host, Path: path + segment + hash}).String(), nil
}

// FileWebURL links a file of the repository at ref, a commit ID or a
// branch name, on its hosting service, with the lines from to to marked,
// from 1; none are when from is 0.
func FileWebURL(rawURL, ref, rel string, from, to int) (string, error) {
	if !isCommitID(ref) && !validBranch(ref) {
		return "", fmt.Errorf("invalid revision %q", ref)
	}
	rel = strings.Trim(rel, "/")
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid file path")
		}
	}
	host, path, err := remoteRepo(rawURL)
	if err != nil {
		return "", err
	}
	segment, frag := "/blob/", ""
	switch host {
	case "gitlab.com":
		segment = "/-/blob/"
	case "bitbucket.org":
		segment = "/src/"
	}
	if from > 0 {
		to = max(to, from)
		switch {
		case host == "bitbucket.org" && to > from:
			frag = fmt.Sprintf("lines-%d:%d", from, to)
		case host == "bitbucket.org":
			frag = fmt.Sprintf("lines-%d", from)
		case to == from:
			frag = fmt.Sprintf("L%d", from)
		case host == "gitlab.com":
			frag = fmt.Sprintf("L%d-%d", from, to)
		default:
			frag = fmt.Sprintf("L%d-L%d", from, to)
		}
	}
	return (&url.URL{Scheme: "https", Host: host, Path: path + segment + ref + "/" + rel, Fragment: frag}).String(), nil
}

func isCommitID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// validBranch accepts a branch name as it may go in a URL's path: no
// part of it that walks up, nor one Git refuses.
func validBranch(name string) bool {
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "\x00\r\n ~^:?*[\\") || strings.Contains(name, "..") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." {
			return false
		}
	}
	return true
}

// remoteRepo returns the host of a known public hosting service a remote
// URL is of, and the repository's path there, without credentials.
func remoteRepo(rawURL string) (host, path string, err error) {
	if !strings.Contains(rawURL, "://") {
		left, right, ok := strings.Cut(rawURL, ":")
		if !ok || !strings.Contains(left, "@") {
			return "", "", fmt.Errorf("unsupported remote URL")
		}
		rawURL = "ssh://" + left + "/" + right
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid remote URL")
	}
	if u.Scheme != "https" && u.Scheme != "ssh" {
		return "", "", fmt.Errorf("unsupported remote URL scheme")
	}
	host = strings.ToLower(u.Hostname())
	if host != "github.com" && host != "gitlab.com" && host != "bitbucket.org" {
		return "", "", fmt.Errorf("unsupported remote hosting service")
	}
	path = strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid repository path")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", "", fmt.Errorf("invalid repository path")
		}
	}
	return host, path, nil
}

// commitRevision resolves one revision before it enters diff arguments.
// ResolveCommit returns the full hash of the commit a revision names, as
// HEAD or a short hash.
func (r *Repo) ResolveCommit(rev string) (string, error) { return r.commitRevision(rev) }

func (r *Repo) commitRevision(ref string) (string, error) {
	if ref == "" || strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, "\x00\r\n") {
		return "", fmt.Errorf("invalid revision %q", ref)
	}
	out, err := r.run(nil, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	return strings.TrimSpace(string(out)), err
}

// CommitRemoteURL prefers a unique decoration, the current upstream, origin,
// then the first remote. Only the chosen remote's fetch URL is read.
func (r *Repo) CommitRemoteURL(cm Commit) (string, error) {
	names, err := r.Remotes()
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "", fmt.Errorf("this repository has no remote")
	}
	decorated := map[string]bool{}
	for _, ref := range cm.Refs {
		if ref.Kind != RefRemote {
			continue
		}
		match := ""
		for _, name := range names {
			if strings.HasPrefix(ref.Name, name+"/") && len(name) > len(match) {
				match = name
			}
		}
		if match != "" {
			decorated[match] = true
		}
	}
	chosen := ""
	if len(decorated) == 1 {
		for name := range decorated {
			chosen = name
		}
	}
	raw, _, err := r.remoteURL(names, chosen)
	if err != nil {
		return "", err
	}
	return RemoteWebURL(raw, cm.Hash)
}

// remoteURL reads the fetch URL of chosen, else of the current upstream's
// remote, origin, then the first remote; and the upstream's branch on its
// remote, "" when the branch has none there.
func (r *Repo) remoteURL(names []string, chosen string) (raw, upstream string, err error) {
	out, err := r.run(nil, "for-each-ref", "--format=%(HEAD)%1f%(upstream:remotename)%1f%(upstream:remoteref)", "refs/heads")
	if err != nil {
		return "", "", err
	}
	upRemote := ""
	for _, row := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(row, "*\x1f"); ok {
			name, ref, _ := strings.Cut(rest, "\x1f")
			if slices.Contains(names, name) {
				upRemote = name
				upstream = strings.TrimPrefix(ref, "refs/heads/")
			}
		}
	}
	if chosen == "" {
		chosen = upRemote
	}
	if chosen == "" {
		chosen = names[0]
		if slices.Contains(names, "origin") {
			chosen = "origin"
		}
	}
	if chosen != upRemote {
		upstream = ""
	}
	out, err = r.run(nil, "remote", "get-url", "--", chosen)
	if err != nil {
		return "", "", err
	}
	return strings.TrimSuffix(string(out), "\n"), upstream, nil
}

// FileRemoteURL links the file at path on the hosting service of the
// current upstream's remote, else origin's, or the first remote's, with
// the lines from to to marked (none for 0). A permalink is to the commit
// checked out; else the link is to the upstream branch, or to that commit
// when the branch has no upstream.
func (r *Repo) FileRemoteURL(path string, from, to int, permalink bool) (string, error) {
	names, err := r.Remotes()
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "", fmt.Errorf("this repository has no remote")
	}
	raw, upstream, err := r.remoteURL(names, "")
	if err != nil {
		return "", err
	}
	ref := upstream
	if permalink || ref == "" {
		if ref, err = r.commitRevision("HEAD"); err != nil {
			return "", err
		}
	}
	return FileWebURL(raw, ref, r.Rel(path), from, to)
}
