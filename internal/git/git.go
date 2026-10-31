// Package git runs the git command for the editor: the state of a work
// tree's files, the text of a file at HEAD or in the index, staging and
// committing, branches, blame and the remotes.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"mayoi/internal/remote"
	"mayoi/internal/vfs"
)

// Repo is a Git work tree. Root is spelled as the folder it was found
// from, so that its paths compare with the editor's, which may go through
// a symlink that Git resolves.
type Repo struct {
	Root string
	// Dir is the work tree's own Git folder, which has its index and its
	// HEAD; Common the folder its worktrees share, which has the refs.
	Dir, Common string
}

// Open finds the work tree that dir is in.
func Open(dir string) (*Repo, error) {
	out, err := run(dir, nil, "rev-parse", "--show-cdup", "--absolute-git-dir", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) < 3 {
		return nil, errors.New("not a work tree")
	}
	// The folders of a remote work tree are as its host spells them.
	r := &Repo{Root: filepath.Clean(filepath.Join(dir, lines[0])), Dir: remote.On(dir, lines[1]), Common: lines[2]}
	if !strings.HasPrefix(r.Common, "/") && !filepath.IsAbs(r.Common) {
		r.Common = filepath.Join(dir, r.Common)
	} else {
		r.Common = remote.On(dir, r.Common)
	}
	return r, nil
}

// Error is a git command that failed, with what it said.
type Error struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	if e.Stderr != "" {
		return e.Stderr
	}
	return fmt.Sprintf("git %s: %v", strings.Join(e.Args, " "), e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// run runs git in dir, with stdin, and returns what it wrote. It never
// asks for a password, nor opens an editor for a message: there is no
// terminal to ask in, and the message git proposes is taken. In a folder
// of another machine, it runs git there.
func run(dir string, stdin []byte, args ...string) ([]byte, error) {
	env := []string{"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C", "GIT_EDITOR=true"}
	if vfs.IsRemote(dir) || os.Getenv("GIT_SSH_COMMAND") == "" {
		env = append(env, "GIT_SSH_COMMAND=ssh -oBatchMode=yes")
	}
	var in io.Reader
	if stdin != nil {
		in = bytes.NewReader(stdin)
	}
	cmd, err := vfs.Command(dir, env, in, "git", args...)
	if err != nil {
		return nil, &Error{Args: args, Err: err}
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if vfs.IsRemote(dir) && !remote.IsConnected(dir) {
			err = remote.ErrOffline
		}
		return out.Bytes(), &Error{Args: args, Stderr: strings.TrimSpace(errb.String()), Err: err}
	}
	return out.Bytes(), nil
}

func (r *Repo) run(stdin []byte, args ...string) ([]byte, error) { return run(r.Root, stdin, args...) }

// Rel returns path relative to the root, with slashes, as Git names it.
func (r *Repo) Rel(path string) string {
	rel, err := filepath.Rel(r.Root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// Abs returns the path of a file Git names relative to the root.
func (r *Repo) Abs(rel string) string { return filepath.Join(r.Root, filepath.FromSlash(rel)) }

// Contains reports whether path is in the work tree.
func (r *Repo) Contains(path string) bool {
	return path == r.Root || strings.HasPrefix(path, r.Root+string(filepath.Separator))
}

// Entry is a file whose state differs from HEAD, or that Git does not
// track. X is its state in the index against HEAD and Y in the work tree
// against the index, as git status says them: '.' unchanged, 'M', 'A',
// 'D', 'R', 'C', 'T', 'U' for a conflict, and '?' for both of a file not
// tracked.
type Entry struct {
	Path string // absolute
	Orig string // where a renamed file was, absolute
	X, Y byte
	// Sub is a submodule's state as git status says it: 'S', then 'C'
	// when its commit moved, 'M' when its files changed and 'U' when it
	// has files Git does not track, '.' for each that did not. It is ""
	// for a file.
	Sub string
}

// Submodule reports whether the entry is a submodule, not a file.
func (e Entry) Submodule() bool { return e.Sub != "" }

// Dirty reports whether a submodule's files differ from its commit.
func (e Entry) Dirty() bool { return len(e.Sub) == 4 && (e.Sub[2] == 'M' || e.Sub[3] == 'U') }

// Untracked reports whether Git does not track the file.
func (e Entry) Untracked() bool { return e.X == '?' }

// Conflicted reports whether a merge left the file in conflict.
func (e Entry) Conflicted() bool {
	return e.X == 'U' || e.Y == 'U' || e.X == 'A' && e.Y == 'A' || e.X == 'D' && e.Y == 'D'
}

// Staged reports whether the index has changes of the file.
func (e Entry) Staged() bool { return !e.Untracked() && !e.Conflicted() && e.X != '.' }

// Unstaged reports whether the work tree has changes of the file the
// index has not.
func (e Entry) Unstaged() bool { return !e.Untracked() && !e.Conflicted() && e.Y != '.' }

// Letter is how the file shows in a list: as its work tree is, or else
// as its index is.
func (e Entry) Letter() byte {
	switch {
	case e.Conflicted():
		return 'C'
	case e.Untracked():
		return 'U'
	case e.Y != '.':
		return e.Y
	}
	return e.X
}

// Status is the state of the work tree: its branch and the files that
// differ.
type Status struct {
	Branch   string // "" when HEAD is detached
	Head     string // the commit checked out, "" before the first
	Upstream string
	Ahead    int
	Behind   int
	Entries  []Entry
	// Op is the merge, rebase, cherry-pick or revert that stopped for
	// its conflicts, "" while none did.
	Op Op
}

// Equal reports whether two statuses say the same.
func (s *Status) Equal(o *Status) bool {
	if s == nil || o == nil {
		return s == o
	}
	return s.Branch == o.Branch && s.Head == o.Head && s.Upstream == o.Upstream && s.Ahead == o.Ahead &&
		s.Behind == o.Behind && s.Op == o.Op && slices.Equal(s.Entries, o.Entries)
}

// statusFields is how many fields come before the path in a record of
// git status --porcelain=v2: 8 of an ordinary file, 9 of a renamed one,
// and 10 of a conflict.
var statusFields = [256]int{'1': 8, '2': 9, 'u': 10}

// Status reads the state of the work tree.
func (r *Repo) Status() (*Status, error) {
	out, err := r.run(nil, "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	st := parseStatus(r, out)
	st.Op = r.InProgress()
	return st, nil
}

func parseStatus(r *Repo, out []byte) *Status {
	st := &Status{}
	recs := strings.Split(string(out), "\x00")
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		if len(rec) < 2 {
			continue
		}
		switch rec[0] {
		case '#':
			key, val, _ := strings.Cut(rec[2:], " ")
			switch key {
			case "branch.oid":
				if val != "(initial)" {
					st.Head = val
				}
			case "branch.head":
				if val != "(detached)" {
					st.Branch = val
				}
			case "branch.upstream":
				st.Upstream = val
			case "branch.ab":
				if a, b, ok := strings.Cut(val, " "); ok {
					st.Ahead, _ = strconv.Atoi(strings.TrimPrefix(a, "+"))
					st.Behind, _ = strconv.Atoi(strings.TrimPrefix(b, "-"))
				}
			}
		case '1', '2', 'u':
			n := statusFields[rec[0]]
			f := strings.SplitN(rec, " ", n+1)
			if len(f) < n+1 || len(f[1]) != 2 {
				continue
			}
			e := Entry{Path: r.Abs(f[n]), X: f[1][0], Y: f[1][1]}
			if strings.HasPrefix(f[2], "S") {
				e.Sub = f[2]
			}
			if rec[0] == '2' && i+1 < len(recs) {
				i++
				e.Orig = r.Abs(recs[i])
			}
			st.Entries = append(st.Entries, e)
		case '?':
			st.Entries = append(st.Entries, Entry{Path: r.Abs(rec[2:]), X: '?', Y: '?'})
		}
	}
	return st
}

// Show returns the text of path at rev: "HEAD", or "" for the index. ok
// is false when it has no such file. A submodule's text is its commit, as
// git diff says it.
func (r *Repo) Show(rev, path string) (text string, ok bool, err error) {
	// ":0:" names the index: after a bare ":", a path that starts with
	// "." reads as relative to the current folder, and git then fails on
	// a file not in the index with "ambiguous argument".
	spec := rev + ":" + r.Rel(path)
	if rev == "" {
		spec = ":0:" + r.Rel(path)
	}
	out, err := r.run(nil, "show", spec)
	if err != nil {
		// The commit of a submodule is in its own repository, not here.
		if hash := r.gitlink(rev, path); hash != "" {
			return subproject(hash, false), true, nil
		}
		var ge *Error
		if errors.As(err, &ge) {
			for _, s := range []string{"exists on disk, but not in", "does not exist in", "neither on disk nor in the index", "invalid object name", "bad revision", "is in the index, but not at stage"} {
				if strings.Contains(ge.Stderr, s) {
					return "", false, nil
				}
			}
		}
		return "", false, err
	}
	// Git 2.55 and later show the commit of a submodule whose repository
	// it can reach, as a log entry and its diff.
	if bytes.HasPrefix(out, []byte("commit ")) {
		if hash := r.gitlink(rev, path); hash != "" {
			return subproject(hash, false), true, nil
		}
	}
	return strings.ReplaceAll(string(out), "\r\n", "\n"), true, nil
}

// gitlink returns the commit a submodule at path is at in rev, "" when
// path is not a submodule there.
func (r *Repo) gitlink(rev, path string) string {
	args := []string{"ls-tree", rev, "--", r.Rel(path)}
	if rev == "" {
		args = []string{"ls-files", "-s", "--", r.Rel(path)}
	}
	out, err := r.run(nil, args...)
	if err != nil {
		return ""
	}
	// "160000 commit <hash>\t<path>" of a tree, "160000 <hash> 0\t<path>"
	// of the index.
	f := strings.Fields(string(out))
	switch {
	case rev != "" && len(f) >= 3 && f[0] == "160000":
		return f[2]
	case rev == "" && len(f) >= 2 && f[0] == "160000":
		return f[1]
	}
	return ""
}

// subproject is the text of a submodule at a commit, as git diff says it.
func subproject(hash string, dirty bool) string {
	if dirty {
		hash += "-dirty"
	}
	return "Subproject commit " + hash + "\n"
}

// WorkSubproject returns the text of the submodule at path in the work
// tree: the commit it has checked out, and whether its files differ from
// it. ok is false when it is not checked out.
func (r *Repo) WorkSubproject(path string, dirty bool) (text string, ok bool, err error) {
	// Without its own .git, git in the folder would find this repository.
	if _, err := vfs.Lstat(filepath.Join(path, ".git")); err != nil {
		return "", false, nil
	}
	out, err := run(path, nil, "rev-parse", "--verify", "-q", "HEAD")
	if err != nil {
		return "", false, err
	}
	return subproject(strings.TrimSpace(string(out)), dirty), true, nil
}

func (r *Repo) rels(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = r.Rel(p)
	}
	return out
}

// Stage puts the files as they are in the index, removed ones too.
func (r *Repo) Stage(paths ...string) error {
	_, err := r.run(nil, append([]string{"add", "-A", "--"}, r.rels(paths)...)...)
	return err
}

// Unstage takes the files' changes out of the index, which has them as
// HEAD does again.
func (r *Repo) Unstage(paths ...string) error {
	if _, err := r.run(nil, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		// Before the first commit there is no HEAD to restore from.
		_, err := r.run(nil, append([]string{"rm", "--cached", "-r", "-q", "--"}, r.rels(paths)...)...)
		return err
	}
	return r.runOr(
		append([]string{"restore", "--staged", "--"}, r.rels(paths)...),
		append([]string{"reset", "-q", "--"}, r.rels(paths)...),
	)
}

// Discard makes the tracked files in the work tree as the index has them.
func (r *Repo) Discard(paths ...string) error {
	return r.runOr(
		append([]string{"restore", "--worktree", "--"}, r.rels(paths)...),
		append([]string{"checkout", "--"}, r.rels(paths)...),
	)
}

// StageText puts text in the index as the file at path.
func (r *Repo) StageText(path, text string) error {
	mode := "100644"
	if out, err := r.run(nil, "ls-files", "-s", "--", r.Rel(path)); err == nil {
		if f := strings.Fields(string(out)); len(f) > 0 {
			mode = f[0]
		}
	}
	sha, err := r.run([]byte(text), "hash-object", "-w", "--stdin", "--path", r.Rel(path))
	if err != nil {
		return err
	}
	_, err = r.run(nil, "update-index", "--add", "--cacheinfo", mode+","+strings.TrimSpace(string(sha))+","+r.Rel(path))
	return err
}

// Commit commits the index with message, or amends the last commit.
func (r *Repo) Commit(message string, amend bool) error {
	args := []string{"commit", "-F", "-"}
	if amend {
		args = append(args, "--amend")
	}
	_, err := r.run([]byte(message), args...)
	return err
}

// UndoCommit takes back the last commit, keeping its changes in the index
// and the work tree: HEAD goes to its parent, or before the first commit
// when it has none. A commit whose parents a shallow clone lacks is not
// taken back.
func (r *Repo) UndoCommit() error {
	out, err := r.run(nil, "log", "-1", "--format=%P", "HEAD", "--")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != "" {
		_, err = r.run(nil, "reset", "--soft", "HEAD^")
		return err
	}
	shallow, err := r.run(nil, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(shallow)) != "false" {
		return errors.New("the last commit's parent is not in this shallow clone, so it cannot be undone")
	}
	_, err = r.run(nil, "update-ref", "-d", "HEAD")
	return err
}

// Op is a command that changes HEAD by commits and can stop part way, for
// conflicts to be resolved: it goes on with Continue, or Abort undoes it.
type Op string

const (
	OpMerge      Op = "merge"
	OpRebase     Op = "rebase"
	OpCherryPick Op = "cherry-pick"
	OpRevert     Op = "revert"
)

// Title is how an Op is named in a menu, as "Cherry-Pick".
func (o Op) Title() string {
	switch o {
	case OpCherryPick:
		return "Cherry-Pick"
	case "":
		return ""
	}
	return strings.ToUpper(string(o[:1])) + string(o[1:])
}

// InProgress returns the command that stopped part way in the work tree,
// "" when none did, by the files it leaves in the work tree's Git folder.
// A rebase picks commits as a cherry-pick does, so it is looked for
// first.
func (r *Repo) InProgress() Op {
	names := []string{"rebase-merge", "rebase-apply", "CHERRY_PICK_HEAD", "REVERT_HEAD", "MERGE_HEAD"}
	paths := make([]string, len(names))
	for i, n := range names {
		paths[i] = filepath.Join(r.Dir, n)
	}
	// At one round trip to a remote host.
	there, _ := vfs.StatMany(paths)
	exists := func(name string) bool {
		_, ok := there[filepath.Join(r.Dir, name)]
		return ok
	}
	switch {
	case exists("rebase-merge"), exists("rebase-apply"):
		return OpRebase
	case exists("CHERRY_PICK_HEAD"):
		return OpCherryPick
	case exists("REVERT_HEAD"):
		return OpRevert
	case exists("MERGE_HEAD"):
		return OpMerge
	}
	return ""
}

// Merge merges rev into the branch checked out, with the message git
// proposes.
func (r *Repo) Merge(rev string) error {
	_, err := r.run(nil, "merge", "--no-edit", rev)
	return err
}

// Rebase moves the commits of the branch checked out that onto lacks on
// top of onto.
func (r *Repo) Rebase(onto string) error {
	_, err := r.run(nil, "rebase", onto)
	return err
}

// CherryPick makes a commit of the changes hash made, on HEAD; those of
// a merge against its first parent.
func (r *Repo) CherryPick(hash string, merge bool) error {
	args := []string{"cherry-pick"}
	if merge {
		args = append(args, "-m", "1")
	}
	_, err := r.run(nil, append(args, hash)...)
	return err
}

// Revert makes a commit that takes back the changes hash made; those of
// a merge against its first parent.
func (r *Repo) Revert(hash string, merge bool) error {
	args := []string{"revert", "--no-edit"}
	if merge {
		args = append(args, "-m", "1")
	}
	_, err := r.run(nil, append(args, hash)...)
	return err
}

// ResetMode is what a reset does to the index and the work tree: soft
// keeps both, mixed keeps the work tree, hard keeps neither.
type ResetMode string

const (
	ResetSoft  ResetMode = "soft"
	ResetMixed ResetMode = "mixed"
	ResetHard  ResetMode = "hard"
)

// Reset moves the branch checked out, or HEAD when detached, to hash.
func (r *Repo) Reset(hash string, mode ResetMode) error {
	_, err := r.run(nil, "reset", "--"+string(mode), hash)
	return err
}

// Continue goes on with op once its conflicts are resolved and staged,
// with the message it proposes.
func (r *Repo) Continue(op Op) error {
	_, err := r.run(nil, string(op), "--continue")
	return err
}

// Abort undoes op, bringing back HEAD, the index and the work tree as
// they were before it.
func (r *Repo) Abort(op Op) error {
	_, err := r.run(nil, string(op), "--abort")
	return err
}

// MergeMessage returns the message git proposes for the commit that ends
// a merge, cherry-pick or revert stopped part way, without its comments.
func (r *Repo) MergeMessage() string {
	b, err := vfs.ReadFile(filepath.Join(r.Dir, "MERGE_MSG"))
	if err != nil {
		return ""
	}
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(l, "#") {
			lines = append(lines, l)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// Head returns the hash of the commit checked out, "" before the first.
func (r *Repo) Head() string {
	out, err := r.run(nil, "rev-parse", "--verify", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Count returns how many commits to has that from has not.
func (r *Repo) Count(from, to string) int {
	out, err := r.run(nil, "rev-list", "--count", from+".."+to)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// ShortHash returns the first 7 characters of a commit's hash.
func ShortHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// LastMessage returns the message of the last commit, "" before the
// first.
func (r *Repo) LastMessage() string {
	out, err := r.run(nil, "log", "-1", "--format=%B")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Branches returns the local branches, by name.
func (r *Repo) Branches() ([]string, error) {
	out, err := r.run(nil, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, err
	}
	var bs []string
	for _, b := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if b != "" {
			bs = append(bs, b)
		}
	}
	return bs, nil
}

// Branch is a branch, local or of a remote, and its last commit.
type Branch struct {
	Name          string // as "main", or "origin/main" for a remote's
	Remote        bool
	Time          time.Time // when its last commit was made
	Author        string    // the author of its last commit
	Subject       string    // the subject of its last commit
	Hash          string
	Upstream      string // display name
	UpstreamRef   string // full ref used for revision operations
	Ahead, Behind int
	Gone, Current bool
}

// BranchList returns the local branches and those of the remotes, the
// one committed to last first.
func (r *Repo) BranchList() ([]Branch, error) {
	out, err := r.run(nil, "for-each-ref", "--sort=-committerdate",
		"--format=%(refname)%1f%(committerdate:unix)%1f%(authorname)%1f%(subject)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	var bs []Branch
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.SplitN(line, "\x1f", 4)
		if len(f) < 4 {
			continue
		}
		var b Branch
		switch {
		case strings.HasPrefix(f[0], "refs/heads/"):
			b.Name = strings.TrimPrefix(f[0], "refs/heads/")
		case strings.HasPrefix(f[0], "refs/remotes/"):
			b.Name, b.Remote = strings.TrimPrefix(f[0], "refs/remotes/"), true
			if strings.HasSuffix(b.Name, "/HEAD") {
				continue
			}
		default:
			continue
		}
		if s, err := strconv.ParseInt(f[1], 10, 64); err == nil {
			b.Time = time.Unix(s, 0)
		}
		b.Author, b.Subject = f[2], f[3]
		bs = append(bs, b)
	}
	return bs, nil
}

// Remotes returns the names of the remotes.
func (r *Repo) Remotes() ([]string, error) {
	out, err := r.run(nil, "remote")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// Checkout checks out a commit, detaching HEAD at it.
func (r *Repo) Checkout(rev string) error {
	return r.runOr([]string{"switch", "--detach", rev}, []string{"checkout", "--detach", rev, "--"})
}

// CreateBranch makes a branch at a commit, without checking it out.
func (r *Repo) CreateBranch(name, at string) error {
	_, err := r.run(nil, "branch", name, at)
	return err
}

// CreateTag tags a commit: annotated with message, lightweight without.
func (r *Repo) CreateTag(name, at, message string) error {
	args := []string{"tag", name, at}
	if message != "" {
		args = []string{"tag", "-a", "-m", message, name, at}
	}
	_, err := r.run(nil, args...)
	return err
}

// Tags returns the names of the tags.
func (r *Repo) Tags() ([]string, error) {
	out, err := r.run(nil, "tag", "--list")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// Switch checks out a branch, making it first when create is set.
func (r *Repo) Switch(branch string, create bool) error {
	args := []string{"switch", branch}
	checkout := []string{"checkout", branch, "--"}
	if create {
		args = []string{"switch", "-c", branch}
		checkout = []string{"checkout", "-b", branch, "--"}
	}
	err := r.runOr(args, checkout)
	if err != nil && !create {
		if path, lookupErr := r.WorktreeForBranch(branch); lookupErr == nil && path != "" && path != r.Root {
			return &WorktreeInUse{Branch: branch, Path: path}
		}
	}
	return err
}

// runOr runs args, then old when the host's Git is older than its
// command: switch and restore came in Git 2.23. Only a missing command is
// retried, so a conflict or an occupied branch keeps the original error.
// A checkout of a branch ends in -- so a missing branch cannot restore a
// file.
func (r *Repo) runOr(args, old []string) error {
	_, err := r.run(nil, args...)
	var gitErr *Error
	if errors.As(err, &gitErr) && strings.HasPrefix(gitErr.Stderr, "git: '"+args[0]+"' is not a git command.") {
		_, err = r.run(nil, old...)
	}
	return err
}

// Blame is who last changed a line, and when.
type Blame struct {
	Commit  string // "" for a line not committed yet
	Author  string
	Time    time.Time
	Summary string
}

// BlameLine blames line (from 0) of the file at path, its text being
// text, which may have changes not saved.
func (r *Repo) BlameLine(path string, line int, text string) (Blame, error) {
	n := strconv.Itoa(line + 1)
	out, err := r.run([]byte(text), "blame", "--porcelain", "-L", n+","+n, "--contents", "-", "--", r.Rel(path))
	if err != nil {
		return Blame{}, err
	}
	return parseBlame(out), nil
}

func parseBlame(out []byte) Blame {
	var b Blame
	for i, l := range strings.Split(string(out), "\n") {
		if i == 0 {
			if f := strings.Fields(l); len(f) > 0 && strings.Trim(f[0], "0") != "" {
				b.Commit = f[0]
			}
			continue
		}
		key, val, _ := strings.Cut(l, " ")
		switch key {
		case "author":
			b.Author = val
		case "author-time":
			if s, err := strconv.ParseInt(val, 10, 64); err == nil {
				b.Time = time.Unix(s, 0)
			}
		case "summary":
			b.Summary = val
		}
	}
	return b
}

// Commit is a commit of the history.
type Commit struct {
	Hash    string
	Parents []string
	Author  string
	Email   string
	Time    time.Time
	Subject string
	Body    string // the message after the subject, trimmed
	Refs    []Ref  // the branches and tags at the commit
}

// RefKind is what a ref names.
type RefKind int

const (
	RefBranch RefKind = iota // a local branch
	RefRemote                // a branch of a remote, as "origin/main"
	RefTag
	RefHead // HEAD, detached at the commit
)

// Ref is a branch or tag at a commit: Head is set for the branch checked
// out.
type Ref struct {
	Name string
	Kind RefKind
	Head bool
}

// parseRefs reads the refs git's %D prints with --decorate=full, as
// "HEAD -> refs/heads/main, tag: refs/tags/v1, refs/remotes/origin/main",
// leaving out the HEADs of remotes.
func parseRefs(s string) []Ref {
	var refs []Ref
	for _, f := range strings.Split(s, ", ") {
		f = strings.TrimSpace(f)
		head := false
		if rest, ok := strings.CutPrefix(f, "HEAD -> "); ok {
			f, head = rest, true
		}
		f = strings.TrimPrefix(f, "tag: ")
		var r Ref
		switch {
		case f == "HEAD":
			r = Ref{Name: "HEAD", Kind: RefHead}
		case strings.HasPrefix(f, "refs/heads/"):
			r = Ref{Name: strings.TrimPrefix(f, "refs/heads/"), Kind: RefBranch, Head: head}
		case strings.HasPrefix(f, "refs/remotes/"):
			r = Ref{Name: strings.TrimPrefix(f, "refs/remotes/"), Kind: RefRemote}
			if strings.HasSuffix(r.Name, "/HEAD") {
				continue
			}
		case strings.HasPrefix(f, "refs/tags/"):
			r = Ref{Name: strings.TrimPrefix(f, "refs/tags/"), Kind: RefTag}
		default:
			continue
		}
		refs = append(refs, r)
	}
	return refs
}

// Short returns the first 7 characters of the commit's hash.
func (c Commit) Short() string { return ShortHash(c.Hash) }

// commitFormat is how Log and CommitInfo have git print a commit: its
// fields, separated by unit separators.
const commitFormat = "--format=%H%x1f%P%x1f%an%x1f%ae%x1f%at%x1f%s%x1f%D%x1f%b"

func parseCommit(rec string) (Commit, bool) {
	f := strings.SplitN(strings.TrimLeft(rec, "\n"), "\x1f", 8)
	if len(f) < 8 || f[0] == "" {
		return Commit{}, false
	}
	c := Commit{Hash: f[0], Parents: strings.Fields(f[1]), Author: f[2], Email: f[3], Subject: f[5],
		Refs: parseRefs(f[6]), Body: strings.TrimSpace(f[7])}
	if s, err := strconv.ParseInt(f[4], 10, 64); err == nil {
		c.Time = time.Unix(s, 0)
	}
	return c, true
}

// Log returns the last n commits of HEAD's history, or of every branch
// and tag when all is set, newest first and each after the commits made
// on it: none before the first commit.
func (r *Repo) Log(n int, all bool) ([]Commit, error) {
	if _, err := r.run(nil, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		return nil, nil
	}
	args := []string{"log", "-z", "--date-order", "--decorate=full", "-n", strconv.Itoa(n), commitFormat, "HEAD"}
	if all {
		args = append(args, "--branches", "--remotes", "--tags")
	}
	out, err := r.run(nil, append(args, "--")...)
	if err != nil {
		return nil, err
	}
	var cs []Commit
	for _, rec := range strings.Split(string(out), "\x00") {
		if c, ok := parseCommit(rec); ok {
			cs = append(cs, c)
		}
	}
	return cs, nil
}

// Unpushed returns the commits, up to n of them, that HEAD has and its
// upstream has not, by hash; without an upstream, or one gone, those no
// branch of a remote has.
func (r *Repo) Unpushed(n int) map[string]bool {
	out, err := r.run(nil, "rev-list", "-n", strconv.Itoa(n), "@{upstream}..HEAD")
	if err != nil {
		out, err = r.run(nil, "rev-list", "-n", strconv.Itoa(n), "HEAD", "--not", "--remotes")
	}
	if err != nil {
		return nil
	}
	m := map[string]bool{}
	for _, h := range strings.Fields(string(out)) {
		m[h] = true
	}
	return m
}

// CommitInfo returns the commit rev names.
func (r *Repo) CommitInfo(rev string) (Commit, error) {
	out, err := r.run(nil, "show", "-s", "--decorate=full", commitFormat, rev, "--")
	if err != nil {
		return Commit{}, err
	}
	c, ok := parseCommit(strings.TrimRight(string(out), "\n"))
	if !ok {
		return Commit{}, fmt.Errorf("no commit %s", rev)
	}
	return c, nil
}

// CommitFile is a file a commit changed from its first parent: Status is
// 'A', 'M', 'D', 'R' or 'T', as git diff says it, and Orig where a file
// renamed was, both absolute.
type CommitFile struct {
	Path, Orig string
	Status     byte
}

// CommitFiles returns the files a commit changed from its first parent,
// or that the first commit added, by path.
func (r *Repo) CommitFiles(c Commit) ([]CommitFile, error) {
	args := []string{"diff-tree", "-r", "-z", "-M", "--name-status", "--no-commit-id"}
	if len(c.Parents) == 0 {
		args = append(args, "--root", c.Hash)
	} else {
		args = append(args, c.Parents[0], c.Hash)
	}
	out, err := r.run(nil, args...)
	if err != nil {
		return nil, err
	}
	var fs []CommitFile
	recs := strings.Split(string(out), "\x00")
	for i := 0; i+1 < len(recs); i++ {
		st := recs[i]
		if st == "" {
			continue
		}
		f := CommitFile{Status: st[0]}
		switch st[0] {
		case 'R', 'C':
			if i+2 >= len(recs) {
				return fs, nil
			}
			f.Orig, f.Path = r.Abs(recs[i+1]), r.Abs(recs[i+2])
			if st[0] == 'C' {
				f.Status, f.Orig = 'A', ""
			}
			i += 2
		default:
			f.Path = r.Abs(recs[i+1])
			i++
		}
		fs = append(fs, f)
	}
	slices.SortFunc(fs, func(x, y CommitFile) int { return strings.Compare(x.Path, y.Path) })
	return fs, nil
}

// Fetch fetches from the remotes.
func (r *Repo) Fetch() error {
	_, err := r.run(nil, "fetch", "--all", "--prune")
	return err
}

// Pull merges the upstream's commits, when that is a fast-forward.
func (r *Repo) Pull() error {
	_, err := r.run(nil, "pull", "--ff-only")
	return err
}

// Sync fast-forwards from the current branch's upstream, then pushes
// any local commits back to that same branch, regardless of push defaults.
func (r *Repo) Sync() error {
	out, err := r.run(nil, "for-each-ref", "--format=%(HEAD)%1f%(refname)%1f%(upstream:remotename)%1f%(upstream:remoteref)", "refs/heads")
	if err != nil {
		return err
	}
	var branch, remote, upstream string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, "\x1f")
		if len(f) == 4 && f[0] == "*" {
			branch, remote, upstream = f[1], f[2], f[3]
			break
		}
	}
	if branch == "" {
		return errors.New("check out a branch with a commit before synchronizing")
	}
	if remote == "" || upstream == "" {
		return errors.New("the current branch has no upstream to synchronize with")
	}
	if _, err := r.run(nil, "pull", "--ff-only", "--", remote, upstream); err != nil {
		return err
	}
	// FETCH_HEAD includes remote changes not yet known when sync began.
	out, err = r.run(nil, "rev-list", "--count", "FETCH_HEAD.."+branch)
	if err != nil || strings.TrimSpace(string(out)) == "0" {
		return err
	}
	_, err = r.run(nil, "push", "--", remote, branch+":"+upstream)
	return err
}

// Push pushes the branch to its upstream, or to a remote of the same name
// that becomes its upstream when it has none.
func (r *Repo) Push(branch string, hasUpstream bool) error {
	if hasUpstream {
		_, err := r.run(nil, "push")
		return err
	}
	remotes, err := r.Remotes()
	if err != nil {
		return err
	}
	if len(remotes) == 0 {
		return errors.New("the repository has no remote to push to")
	}
	remote := remotes[0]
	for _, rm := range remotes {
		if rm == "origin" {
			remote = rm
		}
	}
	_, err = r.run(nil, "push", "-u", remote, branch)
	return err
}

// NeedsAuth reports whether a remote command failed for want of a login,
// which a terminal can ask for.
func NeedsAuth(err error) bool {
	var ge *Error
	if !errors.As(err, &ge) {
		return false
	}
	s := strings.ToLower(ge.Stderr)
	for _, k := range []string{"terminal prompts disabled", "could not read username", "could not read password",
		"authentication failed", "permission denied", "host key verification failed"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// Init makes dir a Git repository.
func Init(dir string) error {
	_, err := run(dir, nil, "init")
	return err
}

// Stage is one of the versions Git keeps of a file in conflict.
type Stage struct {
	Text string
	Ok   bool // the stage has the file
}

// Stages returns the three versions Git keeps of a file in conflict: the
// common base, ours, and theirs. A stage has no file when that version
// has none, as the base of a file both sides added, or the side that
// deleted it.
func (r *Repo) Stages(path string) (base, ours, theirs Stage, err error) {
	base, err = r.stage(1, path)
	if err != nil {
		return base, ours, theirs, err
	}
	ours, err = r.stage(2, path)
	if err != nil {
		return base, ours, theirs, err
	}
	theirs, err = r.stage(3, path)
	return base, ours, theirs, err
}

// stage returns the text of one stage of a conflicted file.
func (r *Repo) stage(n int, path string) (Stage, error) {
	text, ok, err := r.Show(":"+strconv.Itoa(n), path)
	return Stage{Text: text, Ok: ok}, err
}

// MergeSide is one side of a conflict: the ref it reads, and its name as
// the view shows it.
type MergeSide struct {
	Ref  string // the commit or ref the version comes from, "" for the base
	Name string // how the view names it, as "main" or "HEAD"
}

// MergeSides names the two sides of a conflict under the command stopped
// for it: ours is HEAD, theirs the commit the command brings in. Without
// one, theirs is still named.
func (r *Repo) MergeSides(op Op) (ours, theirs MergeSide) {
	ours = MergeSide{Ref: "HEAD", Name: "HEAD"}
	if out, err := run(r.Root, nil, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		ours.Name = strings.TrimSpace(string(out))
	}
	switch op {
	case OpMerge:
		theirs = r.sideOf("MERGE_HEAD")
	case OpCherryPick:
		theirs = r.sideOf("CHERRY_PICK_HEAD")
	case OpRevert:
		theirs = r.sideOf("REVERT_HEAD")
	case OpRebase:
		theirs = r.sideOf("REBASE_HEAD")
		if base := r.rebaseOnto(); base.Ref != "" {
			ours = base
		} else if b := r.rebaseBranch(); b != "" {
			ours.Name = b
		}
	default:
		theirs = MergeSide{Name: "Theirs"}
	}
	return ours, theirs
}

// sideOf names the commit the head file of a command stopped for has.
func (r *Repo) sideOf(name string) MergeSide {
	b, err := vfs.ReadFile(filepath.Join(r.Dir, name))
	if err != nil {
		return MergeSide{Name: "Theirs"}
	}
	hash := strings.TrimSpace(string(b))
	if hash == "" {
		return MergeSide{Name: "Theirs"}
	}
	return r.sideRef(hash)
}

func (r *Repo) sideRef(hash string) MergeSide {
	s := MergeSide{Ref: hash, Name: ShortHash(hash)}
	if out, err := run(r.Root, nil, "name-rev", "--name-only", "--exclude", "refs/remotes/*", hash); err == nil {
		if name := strings.TrimSpace(string(out)); name != "" && name != "undefined" {
			s.Name = name
		}
	}
	return s
}

// rebaseBranch returns the branch a rebase stopped part way moves, "" for
// a detached one.
func (r *Repo) rebaseBranch() string {
	b, err := vfs.ReadFile(filepath.Join(r.Dir, "rebase-merge", "head-name"))
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(string(b)), "refs/heads/")
}

// rebaseOnto returns the commit currently checked out while rebase replays
// the branch named by rebaseBranch.
func (r *Repo) rebaseOnto() MergeSide {
	b, err := vfs.ReadFile(filepath.Join(r.Dir, "rebase-merge", "onto"))
	if err != nil {
		return MergeSide{}
	}
	hash := strings.TrimSpace(string(b))
	if hash == "" {
		return MergeSide{}
	}
	return r.sideRef(hash)
}

// RestoreConflict puts a file back in conflict, as the command stopped
// part way makes it: Git merges its two sides again, which brings the
// stages and the markers of the conflict back. A file the command merged
// with no conflict of its own comes out as the index has it.
func (r *Repo) RestoreConflict(path string) error {
	_, err := r.run(nil, "checkout", "-m", "--", r.Rel(path))
	return err
}
