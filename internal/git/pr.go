package git

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"mayoi/internal/remote"
	"mayoi/internal/vfs"
)

// PR is the pull request of the branch checked out, as the GitHub CLI has
// it.
type PR struct {
	Number int
	State  string // OPEN, CLOSED or MERGED
	URL    string
	// Review is APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED or "".
	Review string
	// Checks is how the checks of its last commit went, and Passed,
	// Failed and Pending count them.
	Checks                  Checks
	Passed, Failed, Pending int
}

// Checks sums up the checks of a pull request: one failed is a failure,
// else one running is pending.
type Checks int

const (
	ChecksNone Checks = iota
	ChecksPass
	ChecksPending
	ChecksFail
)

var (
	// ErrNoPR is that the branch has no pull request.
	ErrNoPR = errors.New("no pull request")
	// ErrNoGH is that the GitHub CLI cannot tell: it is not installed, not
	// signed in, or the repository is not on GitHub.
	ErrNoGH = errors.New("the GitHub CLI cannot tell")
)

// PullRequest asks the GitHub CLI for the pull request of the branch
// checked out. In a folder of another machine, gh runs there, as git does.
func (r *Repo) PullRequest() (*PR, error) {
	out, err := r.gh("pr", "view", "--json", "number,state,statusCheckRollup,reviewDecision,url")
	if err != nil {
		return nil, err
	}
	return parsePR(out)
}

// gh runs the GitHub CLI in the work tree, where the repository is, and
// says why it failed as ErrNoPR, ErrNoGH or remote.ErrOffline.
func (r *Repo) gh(args ...string) ([]byte, error) {
	return r.ghExits(nil, args...)
}

// ghExits runs gh as gh does, and takes what it printed when it exits with
// one of codes as well, as gh pr checks does once a check failed.
func (r *Repo) ghExits(codes []int, args ...string) ([]byte, error) {
	env := []string{"GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "NO_COLOR=1", "GIT_TERMINAL_PROMPT=0"}
	cmd, err := vfs.Command(r.Root, env, nil, "gh", args...)
	if err != nil {
		return nil, err
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && slices.Contains(codes, ee.ExitCode()) && out.Len() > 0 {
			return out.Bytes(), nil
		}
		if vfs.IsRemote(r.Root) && !remote.IsConnected(r.Root) {
			return nil, remote.ErrOffline
		}
		msg := strings.TrimSpace(errb.String())
		if strings.Contains(msg, "no pull requests found") || strings.Contains(msg, "Could not resolve to a PullRequest") {
			return nil, ErrNoPR
		}
		return nil, fmt.Errorf("%w: %s", ErrNoGH, msg)
	}
	return out.Bytes(), nil
}

func parsePR(b []byte) (*PR, error) {
	var v struct {
		Number            int
		State             string
		URL               string
		ReviewDecision    string
		StatusCheckRollup []struct {
			Typename   string `json:"__typename"`
			Status     string
			Conclusion string
			State      string
		}
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	if v.Number == 0 {
		return nil, ErrNoPR
	}
	pr := &PR{Number: v.Number, State: v.State, URL: v.URL, Review: v.ReviewDecision}
	for _, c := range v.StatusCheckRollup {
		// A check run has a status and, once completed, a conclusion; a
		// commit status a state.
		state := c.State
		if c.Typename != "StatusContext" && c.State == "" {
			state = c.Conclusion
			if c.Status != "COMPLETED" {
				state = "PENDING"
			}
		}
		switch state {
		case "SUCCESS", "NEUTRAL", "SKIPPED":
			pr.Passed++
		case "PENDING", "EXPECTED", "":
			pr.Pending++
		default: // FAILURE, ERROR, CANCELLED, TIMED_OUT, ACTION_REQUIRED, STARTUP_FAILURE, STALE
			pr.Failed++
		}
	}
	switch {
	case pr.Failed > 0:
		pr.Checks = ChecksFail
	case pr.Pending > 0:
		pr.Checks = ChecksPending
	case pr.Passed > 0:
		pr.Checks = ChecksPass
	}
	return pr, nil
}
