package git

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GHLabel is a label of an issue or a pull request, its color as GitHub
// has it: six hex digits, no "#".
type GHLabel struct {
	Name, Color string
}

// GHItem is an issue or a pull request open, as the GitHub panel lists it.
type GHItem struct {
	Number    int
	Title     string
	URL       string
	Author    string
	Updated   time.Time
	Comments  int
	Labels    []GHLabel
	Assignees []string
	// PR is set for a pull request, which has the rest.
	PR      bool
	Draft   bool
	HeadRef string
	HeadOid string
	// BaseRef is the branch it goes into; Added, Deleted and Files how much
	// it changes.
	BaseRef        string
	Added, Deleted int
	Files          int
	// Review is APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED or "", and
	// Checks how the checks of its last commit went, which CheckCounts
	// counts.
	Review      string
	Checks      Checks
	CheckCounts CommitStatus
	// ReviewAsked is set when the review of the signed in user is asked
	// for, as theirs or their team's.
	ReviewAsked bool
	// Reviewers are those who reviewed a pull request, but its author, and
	// those whose review is asked for, each once.
	Reviewers []GHReviewer
}

// GHReviewer is a person who reviewed a pull request, or whose review is
// asked for, and how: APPROVED, CHANGES_REQUESTED or COMMENTED, by their
// latest review, or REQUESTED while it is asked for again or not given.
// Again is set for a review asked for again after one was given.
type GHReviewer struct {
	Login, State string
	Again        bool
}

// AskedAgain reports whether the review of login is asked for again,
// after they gave one.
func (it GHItem) AskedAgain(login string) bool {
	return login != "" && slices.ContainsFunc(it.Reviewers, func(r GHReviewer) bool { return r.Again && strings.EqualFold(r.Login, login) })
}

// AssignedTo reports whether login is among the item's assignees.
func (it GHItem) AssignedTo(login string) bool {
	return login != "" && slices.ContainsFunc(it.Assignees, func(a string) bool { return strings.EqualFold(a, login) })
}

// GHOverview is what the GitHub panel shows of a repository: who is
// signed in, what they may do, and the issues and pull requests open, the
// latest updated first.
type GHOverview struct {
	Viewer        string
	Repo          string // owner/name
	URL           string
	Permission    string // ADMIN, MAINTAIN, WRITE, TRIAGE, READ or ""
	IssuesOn      bool
	DefaultBranch string
	PRs, Issues   []GHItem
	// OpenPRs and OpenIssues count those open, which may be more than
	// those listed.
	OpenPRs, OpenIssues int
	// Avatars are the links of the pictures of the people of the items, by
	// login: their authors, assignees and reviewers.
	Avatars map[string]string
}

// CanTriage reports whether the signed in user may close, reopen and
// label the repository's issues.
func (o *GHOverview) CanTriage() bool {
	switch o.Permission {
	case "ADMIN", "MAINTAIN", "WRITE", "TRIAGE":
		return true
	}
	return false
}

// Same reports whether p tells what o does, as a refresh that found
// nothing changed: the view keeps o, and builds nothing again.
func (o *GHOverview) Same(p *GHOverview) bool {
	return o == p || o != nil && p != nil && reflect.DeepEqual(*o, *p)
}

// GHProblem is why the GitHub CLI cannot answer for a repository.
type GHProblem int

const (
	GHFine GHProblem = iota
	// GHMissing is that gh is not installed where the repository is.
	GHMissing
	// GHSignedOut is that gh is not signed in to the repository's host.
	GHSignedOut
	// GHNoDefault is that the repository has remotes of several
	// repositories and gh has not been told which one is the one.
	GHNoDefault
	// GHBusy is that GitHub did not answer in time, or broke off its
	// answer: asking again soon is likely to do.
	GHBusy
	// GHOther is any other failure, which the error says.
	GHOther
)

// ErrNoDefaultRepo is that gh has no default repository among several.
var ErrNoDefaultRepo = fmt.Errorf("%w: no default remote repository has been set", ErrNoGH)

// GitHubProblem says why err, of a call of the GitHub CLI, failed.
func GitHubProblem(err error) GHProblem {
	switch {
	case err == nil:
		return GHFine
	case errors.Is(err, ErrNoDefaultRepo):
		return GHNoDefault
	case errors.Is(err, exec.ErrNotFound):
		return GHMissing
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "executable file not found"), strings.Contains(msg, "command not found"),
		strings.Contains(msg, "gh: not found"), strings.Contains(msg, "no such file or directory") && strings.Contains(msg, "gh"):
		return GHMissing
	case strings.Contains(msg, "gh auth login"), strings.Contains(msg, "not logged in"),
		strings.Contains(msg, "authentication required"), strings.Contains(msg, "bad credentials"):
		return GHSignedOut
	case strings.Contains(msg, "no default remote repository"):
		return GHNoDefault
	case ghBusy(msg):
		return GHBusy
	}
	return GHOther
}

// ghBusy reports whether msg, of a failure of gh in lower case, is that
// GitHub did not answer in time or broke off its answer, as it does when
// a query takes it longer than it allows.
func ghBusy(msg string) bool {
	for _, s := range []string{"http 502", "http 503", "http 504", ": eof", "unexpected end of json input",
		"timeout", "connection reset"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// ghHosts are the hosts other than github.com that gh was asked whether it
// is signed in to, and what it said, for the life of the process.
var ghHosts = struct {
	sync.Mutex
	signedIn map[string]bool
}{signedIn: map[string]bool{}}

// GitHubRemote is a remote of a repository on GitHub, as its URL names it.
type GitHubRemote struct {
	Name string
	Host string
	Path string // owner/name
}

// GitHubRemotes lists the remotes of the repository that are on GitHub:
// on github.com, or on another host gh is signed in to, as GitHub
// Enterprise is. Only a host not seen before asks gh, once.
func (r *Repo) GitHubRemotes() ([]GitHubRemote, error) {
	urls, err := r.ConfiguredRemoteURLs()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(urls))
	for name := range urls {
		names = append(names, name)
	}
	slices.Sort(names)
	var out []GitHubRemote
	for _, name := range names {
		host, path, ok := remoteHostPath(urls[name])
		if !ok || !r.ghHost(host) {
			continue
		}
		out = append(out, GitHubRemote{Name: name, Host: host, Path: path})
	}
	return out, nil
}

// ghHost reports whether host is GitHub's: github.com, or a host gh is
// signed in to.
func (r *Repo) ghHost(host string) bool {
	switch host {
	case "github.com", "www.github.com":
		return true
	case "gitlab.com", "bitbucket.org", "":
		return false
	}
	ghHosts.Lock()
	in, seen := ghHosts.signedIn[host]
	ghHosts.Unlock()
	if seen {
		return in
	}
	_, err := r.gh("auth", "status", "--hostname", host)
	in = err == nil
	if GitHubProblem(err) != GHMissing {
		ghHosts.Lock()
		ghHosts.signedIn[host] = in
		ghHosts.Unlock()
	}
	return in
}

// remoteHostPath reads the host and the owner/name of the repository of
// a remote URL, of any host: https://host/o/r(.git), ssh://git@host/o/r or
// git@host:o/r.
func remoteHostPath(raw string) (host, path string, ok bool) {
	if !strings.Contains(raw, "://") {
		left, right, found := strings.Cut(raw, ":")
		if !found || !strings.Contains(left, "@") {
			return "", "", false
		}
		raw = "ssh://" + left + "/" + right
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", "", false
	}
	switch u.Scheme {
	case "https", "http", "ssh", "git":
	default:
		return "", "", false
	}
	parts := strings.Split(strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return strings.ToLower(u.Hostname()), parts[0] + "/" + parts[1], true
}

// DefaultRepo is the repository gh was told is the one of the work tree,
// "" when it was not.
func (r *Repo) DefaultRepo() string {
	out, err := r.gh("repo", "set-default", "--view")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// SetDefaultRepo tells gh which repository, owner/name, is the one of
// the work tree.
func (r *Repo) SetDefaultRepo(path string) error {
	_, err := r.gh("repo", "set-default", path)
	return err
}

const overviewQuery = `query($owner:String!,$name:String!,$review:String!){
 viewer{login}
 repository(owner:$owner,name:$name){
  nameWithOwner url viewerPermission hasIssuesEnabled defaultBranchRef{name}
  pullRequests(states:OPEN,first:50,orderBy:{field:UPDATED_AT,direction:DESC}){totalCount nodes{
   number title url isDraft updatedAt headRefName headRefOid baseRefName additions deletions changedFiles
   reviewDecision author{login avatarUrl(size:64)} comments{totalCount}
   labels(first:10){nodes{name color}} assignees(first:5){nodes{login avatarUrl(size:64)}}
   latestReviews(first:10){nodes{state author{login avatarUrl(size:64)}}}
   reviewRequests(first:10){nodes{requestedReviewer{... on User{login avatarUrl(size:64)}}}}
   commits(last:1){nodes{commit{statusCheckRollup{state
    contexts(first:0){checkRunCountsByState{state count}statusContextCountsByState{state count}}}}}}}}
  issues(states:OPEN,first:100,orderBy:{field:UPDATED_AT,direction:DESC}){totalCount nodes{
   number title url updatedAt author{login avatarUrl(size:64)} comments{totalCount}
   labels(first:10){nodes{name color}} assignees(first:5){nodes{login avatarUrl(size:64)}}}}
 }
 review: search(query:$review,type:ISSUE,first:100){nodes{... on PullRequest{number}}}
}`

// GitHubOverview asks GitHub, in one query, for what the GitHub panel
// shows of the repository gh takes the work tree's to be. With remotes of
// several repositories, gh must have been told which one it is, as its
// commands for pull requests need: else it fails with ErrNoDefaultRepo.
func (r *Repo) GitHubOverview() (*GHOverview, error) {
	if remotes, err := r.GitHubRemotes(); err == nil && distinctRepos(remotes) > 1 && r.DefaultRepo() == "" {
		return nil, ErrNoDefaultRepo
	}
	o, err := r.gitHubOverview()
	if GitHubProblem(err) == GHBusy {
		// GitHub breaks off a query of a busy repository now and then:
		// once more is likely to do.
		o, err = r.gitHubOverview()
	}
	return o, err
}

func (r *Repo) gitHubOverview() (*GHOverview, error) {
	out, err := r.gh("api", "graphql", "-F", "owner={owner}", "-F", "name={repo}",
		"-F", "review=repo:{owner}/{repo} is:pr is:open review-requested:@me", "-f", "query="+overviewQuery)
	if err != nil {
		return nil, err
	}
	return parseOverview(out)
}

func distinctRepos(remotes []GitHubRemote) int {
	seen := map[string]bool{}
	for _, rm := range remotes {
		seen[strings.ToLower(rm.Host+"/"+rm.Path)] = true
	}
	return len(seen)
}

// ghAuthor is someone GitHub names, with the link of their picture
// where it was asked for, 64 pixels across.
type ghAuthor struct{ Login, AvatarURL string }

type ghNodes[T any] struct {
	TotalCount int
	Nodes      []T
}

type ghListed struct {
	Number         int
	Title          string
	URL            string
	IsDraft        bool
	UpdatedAt      time.Time
	HeadRefName    string
	HeadRefOid     string
	BaseRefName    string
	Additions      int
	Deletions      int
	ChangedFiles   int
	ReviewDecision string
	Author         *ghAuthor
	Comments       struct{ TotalCount int }
	Labels         ghNodes[GHLabel]
	Assignees      ghNodes[ghAuthor]
	LatestReviews  ghNodes[struct {
		State  string
		Author *ghAuthor
	}]
	ReviewRequests ghNodes[struct{ RequestedReviewer *ghAuthor }]
	Commits        ghNodes[struct {
		Commit struct {
			StatusCheckRollup *struct {
				State    string
				Contexts struct {
					CheckRunCountsByState      []checkCount
					StatusContextCountsByState []checkCount
				}
			}
		}
	}]
}

// item is the item listed, the links of its people's pictures put in
// avatars.
func (v ghListed) item(pr bool, avatars map[string]string) GHItem {
	it := GHItem{Number: v.Number, Title: v.Title, URL: v.URL, Updated: v.UpdatedAt, Comments: v.Comments.TotalCount,
		Labels: v.Labels.Nodes, PR: pr, Draft: v.IsDraft, HeadRef: v.HeadRefName, HeadOid: v.HeadRefOid, Review: v.ReviewDecision,
		BaseRef: v.BaseRefName, Added: v.Additions, Deleted: v.Deletions, Files: v.ChangedFiles}
	person := func(a *ghAuthor) string {
		if a == nil {
			return ""
		}
		if a.Login != "" && a.AvatarURL != "" {
			avatars[a.Login] = a.AvatarURL
		}
		return a.Login
	}
	it.Author = person(v.Author)
	for _, a := range v.Assignees.Nodes {
		it.Assignees = append(it.Assignees, person(&a))
	}
	// Each reviewer once, by their latest review; asked again, as asked.
	at := map[string]int{}
	for _, r := range v.LatestReviews.Nodes {
		switch who := person(r.Author); {
		case who == "" || who == it.Author:
		case r.State == "APPROVED", r.State == "CHANGES_REQUESTED", r.State == "COMMENTED":
			at[who] = len(it.Reviewers)
			it.Reviewers = append(it.Reviewers, GHReviewer{Login: who, State: r.State})
		}
	}
	for _, q := range v.ReviewRequests.Nodes {
		who := person(q.RequestedReviewer)
		if who == "" {
			continue // a team's, whose name the query does not read
		}
		if i, ok := at[who]; ok {
			it.Reviewers[i].State, it.Reviewers[i].Again = "REQUESTED", true
			continue
		}
		at[who] = len(it.Reviewers)
		it.Reviewers = append(it.Reviewers, GHReviewer{Login: who, State: "REQUESTED"})
	}
	if n := len(v.Commits.Nodes); n > 0 {
		if roll := v.Commits.Nodes[n-1].Commit.StatusCheckRollup; roll != nil {
			it.Checks = rollupChecks(roll.State)
			it.CheckCounts = countChecks(roll.Contexts.CheckRunCountsByState, roll.Contexts.StatusContextCountsByState)
		}
	}
	return it
}

// rollupChecks reads the state GitHub sums the checks of a commit up as.
func rollupChecks(state string) Checks {
	switch state {
	case "SUCCESS":
		return ChecksPass
	case "PENDING", "EXPECTED":
		return ChecksPending
	case "FAILURE", "ERROR":
		return ChecksFail
	}
	return ChecksNone
}

// ghErrors is the errors of an answer of GitHub's GraphQL API.
type ghErrors struct {
	Errors []struct{ Message string }
}

func (e ghErrors) err() error {
	if len(e.Errors) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrNoGH, e.Errors[0].Message)
}

func parseOverview(b []byte) (*GHOverview, error) {
	var v struct {
		ghErrors
		Data struct {
			Viewer     ghAuthor
			Repository *struct {
				NameWithOwner    string
				URL              string
				ViewerPermission string
				HasIssuesEnabled bool
				DefaultBranchRef *struct{ Name string }
				PullRequests     ghNodes[ghListed]
				Issues           ghNodes[ghListed]
			}
			Review struct {
				Nodes []struct{ Number int }
			}
		}
	}
	if err := json.Unmarshal(b, &v); err != nil {
		// gh printed an answer GitHub broke off.
		return nil, fmt.Errorf("%w: %w", ErrNoGH, err)
	}
	repo := v.Data.Repository
	if repo == nil {
		if err := v.err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: the repository was not found", ErrNoGH)
	}
	o := &GHOverview{Viewer: v.Data.Viewer.Login, Repo: repo.NameWithOwner, URL: repo.URL, Permission: repo.ViewerPermission,
		IssuesOn: repo.HasIssuesEnabled, OpenPRs: repo.PullRequests.TotalCount, OpenIssues: repo.Issues.TotalCount,
		Avatars: map[string]string{}}
	if repo.DefaultBranchRef != nil {
		o.DefaultBranch = repo.DefaultBranchRef.Name
	}
	asked := map[int]bool{}
	for _, n := range v.Data.Review.Nodes {
		asked[n.Number] = true
	}
	for _, n := range repo.PullRequests.Nodes {
		it := n.item(true, o.Avatars)
		it.ReviewAsked = asked[it.Number]
		o.PRs = append(o.PRs, it)
	}
	for _, n := range repo.Issues.Nodes {
		o.Issues = append(o.Issues, n.item(false, o.Avatars))
	}
	return o, nil
}

// PRHeads are the pull requests of the overview by the commit at their
// head, as PullRequestHeads lists them.
func (o *GHOverview) PRHeads() map[string][]PRHead {
	heads := map[string][]PRHead{}
	for _, it := range o.PRs {
		if isCommitID(it.HeadOid) {
			heads[it.HeadOid] = append(heads[it.HeadOid], PRHead{Number: it.Number, Title: it.Title, URL: it.URL, Draft: it.Draft})
		}
	}
	return heads
}

// PRList is the pull requests of the overview as PullRequests lists them.
func (o *GHOverview) PRList() []PRListed {
	out := make([]PRListed, 0, len(o.PRs))
	for _, it := range o.PRs {
		out = append(out, PRListed{Number: it.Number, Title: it.Title, Author: it.Author, HeadRef: it.HeadRef, Draft: it.Draft})
	}
	return out
}

// GHComment is a comment of an issue or a pull request.
type GHComment struct {
	Author  string
	Body    string
	Created time.Time
	URL     string
}

// GHLinked is a pull request that closes an issue.
type GHLinked struct {
	Number int
	Title  string
	URL    string
	State  string
}

// GHIssue is an issue, or the conversation of a pull request, as its tab
// shows it.
type GHIssue struct {
	ID          string
	Number      int
	PR          bool
	Title       string
	URL         string
	State       string // OPEN, CLOSED or MERGED
	StateReason string // COMPLETED, NOT_PLANNED, REOPENED or ""
	Author      string
	Body        string
	Created     time.Time
	Labels      []GHLabel
	Assignees   []string
	Milestone   string
	CanUpdate   bool
	Comments    []GHComment
	// Avatars are the links of the pictures of its author, assignees and
	// those who commented, by login.
	Avatars map[string]string
	// AllComments counts them, which may be more than those read.
	AllComments int
	Linked      []GHLinked
	// HeadRef and BaseRef are a pull request's branches.
	HeadRef, BaseRef string
}

const issueQuery = `query($owner:String!,$name:String!,$n:Int!){repository(owner:$owner,name:$name){issueOrPullRequest(number:$n){
 __typename
 ... on Issue{id number title url state stateReason body createdAt viewerCanUpdate author{login avatarUrl(size:64)}
  labels(first:20){nodes{name color}} assignees(first:10){nodes{login avatarUrl(size:64)}} milestone{title}
  comments(first:100){totalCount nodes{author{login avatarUrl(size:64)} body createdAt url}}
  closedByPullRequestsReferences(first:5){nodes{number title url state}}}
 ... on PullRequest{id number title url state body createdAt viewerCanUpdate author{login avatarUrl(size:64)} headRefName baseRefName
  labels(first:20){nodes{name color}} assignees(first:10){nodes{login avatarUrl(size:64)}} milestone{title}
  comments(first:100){totalCount nodes{author{login avatarUrl(size:64)} body createdAt url}}}
}}}`

// Issue reads issue number n, or the conversation of pull request n,
// with its first hundred comments.
func (r *Repo) Issue(n int) (*GHIssue, error) {
	out, err := r.gh("api", "graphql", "-F", "owner={owner}", "-F", "name={repo}", "-F", "n="+strconv.Itoa(n), "-f", "query="+issueQuery)
	if err != nil {
		return nil, err
	}
	return parseIssue(out)
}

func parseIssue(b []byte) (*GHIssue, error) {
	type comment struct {
		Author    *ghAuthor
		Body      string
		CreatedAt time.Time
		URL       string
	}
	var v struct {
		ghErrors
		Data struct {
			Repository *struct {
				IssueOrPullRequest *struct {
					Typename        string `json:"__typename"`
					ID              string
					Number          int
					Title, URL      string
					State           string
					StateReason     string
					Body            string
					CreatedAt       time.Time
					ViewerCanUpdate bool
					Author          *ghAuthor
					HeadRefName     string
					BaseRefName     string
					Labels          ghNodes[GHLabel]
					Assignees       ghNodes[ghAuthor]
					Milestone       *struct{ Title string }
					Comments        ghNodes[comment]
					ClosedBy        ghNodes[GHLinked] `json:"closedByPullRequestsReferences"`
				}
			}
		}
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	if v.Data.Repository == nil || v.Data.Repository.IssueOrPullRequest == nil {
		if err := v.err(); err != nil && !strings.Contains(err.Error(), "Could not resolve") {
			return nil, err
		}
		return nil, ErrNoPR
	}
	x := v.Data.Repository.IssueOrPullRequest
	is := &GHIssue{ID: x.ID, Number: x.Number, PR: x.Typename == "PullRequest", Title: x.Title, URL: x.URL, State: x.State,
		StateReason: x.StateReason, Body: x.Body, Created: x.CreatedAt, Labels: x.Labels.Nodes, CanUpdate: x.ViewerCanUpdate,
		AllComments: x.Comments.TotalCount, Linked: x.ClosedBy.Nodes, HeadRef: x.HeadRefName, BaseRef: x.BaseRefName,
		Avatars: map[string]string{}}
	person := func(a *ghAuthor) string {
		if a == nil {
			return ""
		}
		if a.Login != "" && a.AvatarURL != "" {
			is.Avatars[a.Login] = a.AvatarURL
		}
		return a.Login
	}
	is.Author = person(x.Author)
	for _, a := range x.Assignees.Nodes {
		is.Assignees = append(is.Assignees, person(&a))
	}
	if x.Milestone != nil {
		is.Milestone = x.Milestone.Title
	}
	for _, c := range x.Comments.Nodes {
		is.Comments = append(is.Comments, GHComment{Author: person(c.Author), Body: c.Body, Created: c.CreatedAt, URL: c.URL})
	}
	return is, nil
}

// kind is the gh command of an issue, or of a pull request.
func (is *GHIssue) kind() string {
	if is.PR {
		return "pr"
	}
	return "issue"
}

// Comment adds a comment to an issue, or to the conversation of a pull
// request.
func (r *Repo) Comment(is *GHIssue, body string) error {
	_, err := r.gh(is.kind(), "comment", strconv.Itoa(is.Number), "--body", body)
	return err
}

// CloseIssue closes an issue as completed, or as not planned.
func (r *Repo) CloseIssue(n int, notPlanned bool) error {
	reason := "completed"
	if notPlanned {
		reason = "not planned"
	}
	_, err := r.gh("issue", "close", strconv.Itoa(n), "--reason", reason)
	return err
}

// ReopenIssue opens a closed issue again.
func (r *Repo) ReopenIssue(n int) error {
	_, err := r.gh("issue", "reopen", strconv.Itoa(n))
	return err
}

// CreateIssue opens a new issue and returns its number.
func (r *Repo) CreateIssue(title, body string) (int, error) {
	out, err := r.gh("issue", "create", "--title", title, "--body", body)
	if err != nil {
		return 0, err
	}
	return issueNumber(strings.TrimSpace(string(out)))
}

// issueNumber reads the number at the end of the link gh prints of an
// issue it made: https://github.com/o/r/issues/12.
func issueNumber(link string) (int, error) {
	lines := strings.Split(link, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	i := strings.LastIndex(last, "/")
	n, err := strconv.Atoi(last[i+1:])
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("gh did not say the new issue's number: %q", last)
	}
	return n, nil
}

// CheckoutPR checks the branch of pull request n out in the work tree.
func (r *Repo) CheckoutPR(n int) error {
	_, err := r.gh("pr", "checkout", strconv.Itoa(n))
	return err
}

// GHThread is a thread of review comments on lines of a pull request's
// files. Line and StartLine are those of the side, from 1, StartLine 0
// for one line; an outdated thread has those of the commit it was made
// on.
type GHThread struct {
	ID        string
	Path      string
	Line      int
	StartLine int
	OldSide   bool
	Resolved  bool
	Outdated  bool
	Comments  []GHComment
}

const threadsQuery = `query($owner:String!,$name:String!,$n:Int!){repository(owner:$owner,name:$name){pullRequest(number:$n){
 reviewThreads(first:100){nodes{id isResolved isOutdated path line startLine originalLine originalStartLine diffSide
  comments(first:50){nodes{author{login} body createdAt url}}}}}}}`

// ReviewThreads reads the threads of review comments of pull request n.
func (r *Repo) ReviewThreads(n int) ([]GHThread, error) {
	out, err := r.gh("api", "graphql", "-F", "owner={owner}", "-F", "name={repo}", "-F", "n="+strconv.Itoa(n), "-f", "query="+threadsQuery)
	if err != nil {
		return nil, err
	}
	return parseThreads(out)
}

func parseThreads(b []byte) ([]GHThread, error) {
	var v struct {
		ghErrors
		Data struct {
			Repository *struct {
				PullRequest *struct {
					ReviewThreads ghNodes[struct {
						ID                string
						IsResolved        bool
						IsOutdated        bool
						Path              string
						Line              *int
						StartLine         *int
						OriginalLine      *int
						OriginalStartLine *int
						DiffSide          string
						Comments          ghNodes[struct {
							Author    *ghAuthor
							Body      string
							CreatedAt time.Time
							URL       string
						}]
					}]
				}
			}
		}
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	if v.Data.Repository == nil || v.Data.Repository.PullRequest == nil {
		if err := v.err(); err != nil {
			return nil, err
		}
		return nil, ErrNoPR
	}
	num := func(p *int) int {
		if p == nil {
			return 0
		}
		return *p
	}
	var out []GHThread
	for _, t := range v.Data.Repository.PullRequest.ReviewThreads.Nodes {
		th := GHThread{ID: t.ID, Path: t.Path, OldSide: t.DiffSide == "LEFT", Resolved: t.IsResolved, Outdated: t.IsOutdated,
			Line: num(t.Line), StartLine: num(t.StartLine)}
		if th.Line == 0 {
			th.Line, th.StartLine, th.Outdated = num(t.OriginalLine), num(t.OriginalStartLine), true
		}
		for _, c := range t.Comments.Nodes {
			gc := GHComment{Body: c.Body, Created: c.CreatedAt, URL: c.URL}
			if c.Author != nil {
				gc.Author = c.Author.Login
			}
			th.Comments = append(th.Comments, gc)
		}
		if th.Path != "" && th.Line > 0 {
			out = append(out, th)
		}
	}
	return out, nil
}

// ResolveThread marks a thread of review comments resolved on GitHub.
func (r *Repo) ResolveThread(id string) error {
	const q = `mutation($id:ID!){resolveReviewThread(input:{threadId:$id}){thread{isResolved}}}`
	_, err := r.gh("api", "graphql", "-f", "query="+q, "-f", "id="+id)
	return err
}

// FetchDefaultBranch fetches the default branch of the repository at
// repoURL from its remote, and returns the remote's ref of it, which a
// worktree for an issue starts from.
func (r *Repo) FetchDefaultBranch(repoURL, branch string) (string, error) {
	if !validBranch(branch) {
		return "", fmt.Errorf("invalid default branch %q", branch)
	}
	name, err := r.prRemote(strings.TrimSuffix(repoURL, "/") + "/pull/0")
	if err != nil {
		return "", err
	}
	if _, err := r.run(nil, "fetch", "--no-tags", "--", name, "+refs/heads/"+branch+":refs/remotes/"+name+"/"+branch); err != nil {
		return "", err
	}
	return "refs/remotes/" + name + "/" + branch, nil
}
