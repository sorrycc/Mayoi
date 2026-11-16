package git

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"
)

// PRView is a pull request as its tab shows it: what it is, who reviews
// it, whether it can merge, its conversation and its commits.
type PRView struct {
	ID         string
	Number     int
	Title      string
	URL        string
	State      string // OPEN, CLOSED or MERGED
	Draft      bool
	Body       string
	Author     string
	Created    time.Time
	Updated    time.Time
	BaseRef    string
	HeadRef    string
	HeadOid    string
	CrossRepo  bool
	Additions  int
	Deletions  int
	Files      int
	CanUpdate  bool
	ViewerIsIt bool // the viewer opened it
	// Mergeable is MERGEABLE, CONFLICTING or UNKNOWN; MergeState GitHub's
	// mergeStateStatus: CLEAN, BLOCKED, BEHIND, DIRTY, UNSTABLE, DRAFT,
	// HAS_HOOKS or UNKNOWN. Review is APPROVED, CHANGES_REQUESTED,
	// REVIEW_REQUIRED or "".
	Mergeable  string
	MergeState string
	Review     string
	// Methods are the ways the repository lets it merge: MERGE, SQUASH
	// and REBASE, in that order.
	Methods   []string
	Labels    []GHLabel
	Assignees []string
	Milestone string
	Reviewers []PRReviewer
	// Linked are the issues it closes.
	Linked []GHLinked
	// Events are its comments and reviews, the oldest first; AllComments
	// counts its comments, which may be more than those read.
	Events      []PREvent
	AllComments int
	// Commits are its last hundred commits, the oldest first, of
	// AllCommits.
	Commits    []PRCommit
	AllCommits int
	// Avatars are the links of the pictures of the people in it, by login.
	Avatars map[string]string
}

// PRReviewer is someone asked to review a pull request, or who reviewed
// it: State is APPROVED, CHANGES_REQUESTED, COMMENTED or DISMISSED, or
// REQUESTED while they have not yet.
type PRReviewer struct {
	Login string
	State string
	Team  bool
}

// PREvent is a comment on a pull request, or a review of it, whose state
// Review is then: APPROVED, CHANGES_REQUESTED, COMMENTED or DISMISSED.
type PREvent struct {
	Author  string
	Body    string
	Created time.Time
	URL     string
	Review  string
}

// PRCommit is a commit of a pull request.
type PRCommit struct {
	Hash    string
	Subject string
	Author  string
	Login   string
	Date    time.Time
}

const prViewQuery = `query($owner:String!,$name:String!,$n:Int!){repository(owner:$owner,name:$name){
 mergeCommitAllowed squashMergeAllowed rebaseMergeAllowed
 pullRequest(number:$n){id number title url state isDraft body createdAt updatedAt viewerCanUpdate viewerDidAuthor isCrossRepository
  author{login avatarUrl(size:64)} headRefName baseRefName headRefOid additions deletions changedFiles
  mergeable mergeStateStatus reviewDecision
  labels(first:20){nodes{name color}} assignees(first:10){nodes{login avatarUrl(size:64)}} milestone{title}
  reviewRequests(first:20){nodes{requestedReviewer{__typename ... on User{login avatarUrl(size:64)} ... on Bot{login avatarUrl(size:64)} ... on Team{name}}}}
  latestReviews(first:20){nodes{author{login avatarUrl(size:64)} state}}
  closingIssuesReferences(first:5){nodes{number title url state}}
  comments(first:100){totalCount nodes{author{login avatarUrl(size:64)} body createdAt url}}
  reviews(first:100){nodes{author{login avatarUrl(size:64)} state body submittedAt url}}
  commits(last:100){totalCount nodes{commit{oid messageHeadline committedDate author{name user{login avatarUrl(size:64)}}}}}}}}`

// PullRequestView reads pull request n as its tab shows it.
func (r *Repo) PullRequestView(n int) (*PRView, error) {
	out, err := r.gh("api", "graphql", "-F", "owner={owner}", "-F", "name={repo}", "-F", "n="+strconv.Itoa(n), "-f", "query="+prViewQuery)
	if err != nil {
		return nil, err
	}
	return parsePRView(out)
}

func parsePRView(b []byte) (*PRView, error) {
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
				MergeCommitAllowed bool
				SquashMergeAllowed bool
				RebaseMergeAllowed bool
				PullRequest        *struct {
					ID                string
					Number            int
					Title, URL, State string
					IsDraft           bool
					Body              string
					CreatedAt         time.Time
					UpdatedAt         time.Time
					ViewerCanUpdate   bool
					ViewerDidAuthor   bool
					IsCrossRepository bool
					Author            *ghAuthor
					HeadRefName       string
					BaseRefName       string
					HeadRefOid        string
					Additions         int
					Deletions         int
					ChangedFiles      int
					Mergeable         string
					MergeStateStatus  string
					ReviewDecision    string
					Labels            ghNodes[GHLabel]
					Assignees         ghNodes[ghAuthor]
					Milestone         *struct{ Title string }
					ReviewRequests    ghNodes[struct {
						RequestedReviewer *struct {
							Typename  string `json:"__typename"`
							Login     string
							AvatarURL string
							Name      string
						}
					}]
					LatestReviews ghNodes[struct {
						Author *ghAuthor
						State  string
					}]
					Closing  ghNodes[GHLinked] `json:"closingIssuesReferences"`
					Comments ghNodes[comment]
					Reviews  ghNodes[struct {
						Author      *ghAuthor
						State       string
						Body        string
						SubmittedAt time.Time
						URL         string
					}]
					Commits ghNodes[struct {
						Commit struct {
							Oid             string
							MessageHeadline string
							CommittedDate   time.Time
							Author          struct {
								Name string
								User *ghAuthor
							}
						}
					}]
				}
			}
		}
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	if v.Data.Repository == nil || v.Data.Repository.PullRequest == nil {
		if err := v.err(); err != nil && !strings.Contains(err.Error(), "Could not resolve") {
			return nil, err
		}
		return nil, ErrNoPR
	}
	repo, x := v.Data.Repository, v.Data.Repository.PullRequest
	pr := &PRView{ID: x.ID, Number: x.Number, Title: x.Title, URL: x.URL, State: x.State, Draft: x.IsDraft, Body: x.Body,
		Created: x.CreatedAt, Updated: x.UpdatedAt, BaseRef: x.BaseRefName, HeadRef: x.HeadRefName, HeadOid: x.HeadRefOid,
		CrossRepo: x.IsCrossRepository, Additions: x.Additions, Deletions: x.Deletions, Files: x.ChangedFiles,
		CanUpdate: x.ViewerCanUpdate, ViewerIsIt: x.ViewerDidAuthor, Mergeable: x.Mergeable, MergeState: x.MergeStateStatus,
		Review: x.ReviewDecision, Labels: x.Labels.Nodes, Linked: x.Closing.Nodes, AllComments: x.Comments.TotalCount,
		AllCommits: x.Commits.TotalCount, Avatars: map[string]string{}}
	person := func(a *ghAuthor) string {
		if a == nil {
			return ""
		}
		if a.Login != "" && a.AvatarURL != "" {
			pr.Avatars[a.Login] = a.AvatarURL
		}
		return a.Login
	}
	if repo.MergeCommitAllowed {
		pr.Methods = append(pr.Methods, "MERGE")
	}
	if repo.SquashMergeAllowed {
		pr.Methods = append(pr.Methods, "SQUASH")
	}
	if repo.RebaseMergeAllowed {
		pr.Methods = append(pr.Methods, "REBASE")
	}
	pr.Author = person(x.Author)
	for _, a := range x.Assignees.Nodes {
		pr.Assignees = append(pr.Assignees, person(&a))
	}
	if x.Milestone != nil {
		pr.Milestone = x.Milestone.Title
	}
	// Those who reviewed, by their latest review, then those asked to who
	// have not yet.
	for _, rv := range x.LatestReviews.Nodes {
		if login := person(rv.Author); login != "" && rv.State != "PENDING" {
			pr.Reviewers = append(pr.Reviewers, PRReviewer{Login: login, State: rv.State})
		}
	}
	for _, rq := range x.ReviewRequests.Nodes {
		who := rq.RequestedReviewer
		if who == nil {
			continue
		}
		login, team := who.Login, who.Typename == "Team"
		if team {
			login = who.Name
		} else if who.AvatarURL != "" {
			pr.Avatars[login] = who.AvatarURL
		}
		if login == "" || slices.ContainsFunc(pr.Reviewers, func(r PRReviewer) bool { return r.Login == login }) {
			continue
		}
		pr.Reviewers = append(pr.Reviewers, PRReviewer{Login: login, State: "REQUESTED", Team: team})
	}
	for _, c := range x.Comments.Nodes {
		pr.Events = append(pr.Events, PREvent{Author: person(c.Author), Body: c.Body, Created: c.CreatedAt, URL: c.URL})
	}
	// A review that only holds comments on lines shows by those lines, not
	// in the conversation.
	for _, rv := range x.Reviews.Nodes {
		if rv.State == "PENDING" || rv.State == "COMMENTED" && strings.TrimSpace(rv.Body) == "" {
			continue
		}
		pr.Events = append(pr.Events, PREvent{Author: person(rv.Author), Body: rv.Body, Created: rv.SubmittedAt, URL: rv.URL, Review: rv.State})
	}
	slices.SortStableFunc(pr.Events, func(a, b PREvent) int { return a.Created.Compare(b.Created) })
	for _, n := range x.Commits.Nodes {
		c := n.Commit
		pc := PRCommit{Hash: c.Oid, Subject: c.MessageHeadline, Author: c.Author.Name, Date: c.CommittedDate}
		if c.Author.User != nil {
			pc.Login = person(c.Author.User)
		}
		pr.Commits = append(pr.Commits, pc)
	}
	return pr, nil
}

// ReviewPR reviews pull request n: event is approve, request-changes or
// comment, body what the review says, which all but approving need.
func (r *Repo) ReviewPR(n int, event, body string) error {
	args := []string{"pr", "review", strconv.Itoa(n), "--" + event}
	if body != "" {
		args = append(args, "--body", body)
	}
	_, err := r.gh(args...)
	return err
}

// CommentPR adds a comment to the conversation of pull request n.
func (r *Repo) CommentPR(n int, body string) error {
	_, err := r.gh("pr", "comment", strconv.Itoa(n), "--body", body)
	return err
}

// MergePR merges pull request n by method, MERGE, SQUASH or REBASE, then,
// with deleteBranch, deletes its branch on GitHub when it is of the same
// repository. The branch of the work tree stays as it is.
func (r *Repo) MergePR(pr *PRView, method string, deleteBranch bool) error {
	if _, err := r.gh("pr", "merge", strconv.Itoa(pr.Number), "--"+strings.ToLower(method)); err != nil {
		return err
	}
	if deleteBranch && !pr.CrossRepo && pr.HeadRef != "" {
		_, err := r.gh("api", "-X", "DELETE", "repos/{owner}/{repo}/git/refs/heads/"+pr.HeadRef)
		return err
	}
	return nil
}

// ClosePR closes pull request n, or opens it again.
func (r *Repo) ClosePR(n int, reopen bool) error {
	verb := "close"
	if reopen {
		verb = "reopen"
	}
	_, err := r.gh("pr", verb, strconv.Itoa(n))
	return err
}

// ReadyPR marks pull request n ready for review, or, with draft, a draft
// again.
func (r *Repo) ReadyPR(n int, draft bool) error {
	args := []string{"pr", "ready", strconv.Itoa(n)}
	if draft {
		args = append(args, "--undo")
	}
	_, err := r.gh(args...)
	return err
}

// ReplyThread answers a thread of review comments.
func (r *Repo) ReplyThread(id, body string) error {
	const q = `mutation($id:ID!,$body:String!){addPullRequestReviewThreadReply(input:{pullRequestReviewThreadId:$id,body:$body}){comment{id}}}`
	_, err := r.gh("api", "graphql", "-f", "query="+q, "-f", "id="+id, "-f", "body="+body)
	return err
}

// UnresolveThread opens a resolved thread of review comments again.
func (r *Repo) UnresolveThread(id string) error {
	const q = `mutation($id:ID!){unresolveReviewThread(input:{threadId:$id}){thread{isResolved}}}`
	_, err := r.gh("api", "graphql", "-f", "query="+q, "-f", "id="+id)
	return err
}
