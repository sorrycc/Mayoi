package git

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Check is a check of a pull request, as gh pr checks lists it. A check
// that is a job of GitHub Actions has the job, its run and its repository,
// which its link names; another, as a commit status, only its link.
type Check struct {
	Name     string
	Workflow string
	// Bucket is pass, fail, pending, skipping or cancel.
	Bucket      string
	State       string
	Description string
	Link        string
	Host        string
	Owner, Repo string
	RunID       int64
	JobID       int64
}

// IsJob is whether the check is a job of GitHub Actions, whose log can be
// read and which can run again.
func (c Check) IsJob() bool { return c.JobID != 0 }

// Title is the check as GitHub shows it: its workflow, then its name.
func (c Check) Title() string {
	if c.Workflow == "" || c.Workflow == c.Name {
		return c.Name
	}
	return c.Workflow + " / " + c.Name
}

// repoArg names the repository of the job for gh's -R.
func (c Check) repoArg() string {
	return c.Host + "/" + c.Owner + "/" + c.Repo
}

// PRChecks lists the checks of pull request number, those failed first,
// then those running, then the others.
func (r *Repo) PRChecks(number int) ([]Check, error) {
	// gh exits 1 once a check failed and 8 while one runs, having printed
	// them all the same.
	out, err := r.ghExits([]int{1, 8}, "pr", "checks", strconv.Itoa(number), "--json", "name,state,bucket,link,workflow,description")
	if err != nil {
		if strings.Contains(err.Error(), "no checks reported") {
			return nil, nil
		}
		return nil, err
	}
	return parseChecks(out)
}

func parseChecks(b []byte) ([]Check, error) {
	var vs []struct {
		Name, State, Bucket, Link, Workflow, Description string
	}
	if err := json.Unmarshal(b, &vs); err != nil {
		return nil, err
	}
	rank := map[string]int{"fail": 0, "pending": 1, "cancel": 2}
	order := func(bucket string) int {
		if r, ok := rank[bucket]; ok {
			return r
		}
		return 3
	}
	var out []Check
	for _, v := range vs {
		c := Check{Name: v.Name, Workflow: v.Workflow, Bucket: v.Bucket, State: v.State, Description: v.Description, Link: v.Link}
		c.Host, c.Owner, c.Repo, c.RunID, c.JobID = parseJobLink(v.Link)
		out = append(out, c)
	}
	slices.SortStableFunc(out, func(a, b Check) int { return order(a.Bucket) - order(b.Bucket) })
	return out, nil
}

// parseJobLink reads the repository, run and job of a link to a job of
// GitHub Actions: https://<host>/<owner>/<repo>/actions/runs/<run>/job/<job>.
// Another link gives zeros.
func parseJobLink(link string) (host, owner, repo string, run, job int64) {
	u, err := url.Parse(link)
	if err != nil || u.Host == "" {
		return
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 7 || parts[2] != "actions" || parts[3] != "runs" || parts[5] != "job" {
		return
	}
	r, err1 := strconv.ParseInt(parts[4], 10, 64)
	j, err2 := strconv.ParseInt(parts[6], 10, 64)
	if err1 != nil || err2 != nil || r <= 0 || j <= 0 {
		return
	}
	return u.Host, parts[0], parts[1], r, j
}

// ErrNotJob is that a check is no job of GitHub Actions.
var ErrNotJob = errors.New("the check is not a GitHub Actions job")

// JobLog reads the log of the job of check c, its last lines at most,
// with no colors and no time stamps.
func (r *Repo) JobLog(c Check, lines int) (string, error) {
	if !c.IsJob() {
		return "", ErrNotJob
	}
	path := fmt.Sprintf("repos/%s/%s/actions/jobs/%d/logs", c.Owner, c.Repo, c.JobID)
	// gh holds back a log with the escape codes of colors unless allowed,
	// which a gh older than that does not know of.
	out, err := r.gh("api", "--allow-escape-sequences", "--hostname", c.Host, path)
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		out, err = r.gh("api", "--hostname", c.Host, path)
	}
	if err != nil {
		return "", err
	}
	return cleanLog(out, lines), nil
}

// RerunJob runs the job of check c again.
func (r *Repo) RerunJob(c Check) error {
	if !c.IsJob() {
		return ErrNotJob
	}
	_, err := r.gh("run", "rerun", "--job", strconv.FormatInt(c.JobID, 10), "-R", c.repoArg())
	return err
}

var (
	ansiCodes  = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)
	logStamp   = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?Z ?`)
	logNewline = []byte("\n")
)

// cleanLog keeps the last lines of a log of GitHub Actions, with the
// escape codes of its colors and the time stamp of each line left out.
func cleanLog(b []byte, lines int) string {
	b = bytes.TrimPrefix(b, []byte("\ufeff"))
	b = bytes.TrimRight(bytes.ReplaceAll(b, []byte("\r\n"), logNewline), "\n")
	all := bytes.Split(b, logNewline)
	if lines > 0 && len(all) > lines {
		all = all[len(all)-lines:]
	}
	var sb strings.Builder
	for i, l := range all {
		if i > 0 {
			sb.WriteByte('\n')
		}
		l = ansiCodes.ReplaceAll(l, nil)
		l = logStamp.ReplaceAll(l, nil)
		// A carriage return alone redraws the line: what follows the last
		// one is what was left on it.
		if k := bytes.LastIndexByte(l, '\r'); k >= 0 {
			l = l[k+1:]
		}
		sb.Write(l)
	}
	return sb.String()
}
