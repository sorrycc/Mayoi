package git

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Stash is an entry of the stash, newest first. Hash names it for
// applying and dropping: its index moves as stashes come and go.
type Stash struct {
	Hash  string
	Index int
	// Message is what git stash wrote, "On main: wip" or "WIP on main:
	// 1234567 subject", its branch taken out into Branch.
	Message, Branch string
	When            time.Time
}

// Ref is how Git names the stash now, stash@{n}.
func (s Stash) Ref() string { return "stash@{" + strconv.Itoa(s.Index) + "}" }

// StashList returns the newest n stashes.
func (r *Repo) StashList(n int) ([]Stash, error) {
	out, err := r.run(nil, "stash", "list", "-n", strconv.Itoa(n), "--format=%H%x1f%ct%x1f%gs")
	if err != nil {
		return nil, err
	}
	var list []Stash
	for i, line := range outputLines(out) {
		f := strings.SplitN(line, "\x1f", 3)
		if len(f) != 3 {
			continue
		}
		s := Stash{Hash: f[0], Index: i, Message: f[2]}
		if sec, err := strconv.ParseInt(f[1], 10, 64); err == nil {
			s.When = time.Unix(sec, 0)
		}
		s.Branch, s.Message = stashSubject(f[2])
		list = append(list, s)
	}
	return list, nil
}

// stashSubject splits the reflog subject of a stash into its branch and
// message: "On main: wip" is main and wip, "WIP on main: 1234567 subject"
// main and the subject, prefixed WIP.
func stashSubject(s string) (branch, msg string) {
	rest, wip := strings.CutPrefix(s, "WIP on ")
	if !wip {
		var ok bool
		if rest, ok = strings.CutPrefix(s, "On "); !ok {
			return "", s
		}
	}
	branch, msg, ok := strings.Cut(rest, ": ")
	if !ok {
		return "", s
	}
	if wip {
		if _, subject, ok := strings.Cut(msg, " "); ok {
			msg = subject
		}
		msg = "WIP: " + msg
	}
	return branch, msg
}

// ErrNothingToStash is the error of a stash of a work tree without changes.
var ErrNothingToStash = errors.New("no local changes to save")

// Stash puts the changes of the work tree and the index away, with
// message when it is not "", and the untracked files too when untracked,
// and returns the stash made.
func (r *Repo) Stash(message string, untracked bool) (string, error) {
	args := []string{"stash", "push"}
	if untracked {
		args = append(args, "--include-untracked")
	}
	if message = strings.TrimSpace(message); message != "" {
		args = append(args, "-m", message)
	}
	out, err := r.run(nil, args...)
	if err != nil {
		return "", err
	}
	// Git saves nothing, and says so, but succeeds.
	if strings.HasPrefix(string(out), "No local changes to save") {
		return "", ErrNothingToStash
	}
	hash, err := r.run(nil, "rev-parse", "--verify", "-q", "refs/stash")
	return strings.TrimSpace(string(hash)), err
}

// StashApply applies the stash hash to the work tree, keeping it.
func (r *Repo) StashApply(hash string) error {
	ref, err := r.stashRef(hash)
	if err != nil {
		return err
	}
	_, err = r.run(nil, "stash", "apply", ref)
	return err
}

// StashPop applies the stash hash and drops it. Git keeps a stash whose
// changes conflict.
func (r *Repo) StashPop(hash string) error {
	ref, err := r.stashRef(hash)
	if err != nil {
		return err
	}
	_, err = r.run(nil, "stash", "pop", ref)
	return err
}

// StashDrop deletes the stash hash.
func (r *Repo) StashDrop(hash string) error {
	ref, err := r.stashRef(hash)
	if err != nil {
		return err
	}
	_, err = r.run(nil, "stash", "drop", ref)
	return err
}

// stashRef finds where the stash hash is now, which a stash made or
// dropped since it was listed moves.
func (r *Repo) stashRef(hash string) (string, error) {
	out, err := r.run(nil, "stash", "list", "--format=%H")
	if err != nil {
		return "", err
	}
	for i, h := range outputLines(out) {
		if h == hash {
			return Stash{Index: i}.Ref(), nil
		}
	}
	return "", fmt.Errorf("stash %.7s is gone", hash)
}
