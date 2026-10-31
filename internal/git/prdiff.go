package git

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// PRInfo is a pull request as its diff shows it: the commits at the tip
// of its base branch and of its head, as GitHub has them.
type PRInfo struct {
	Number  int
	ID      string // the node ID, which GitHub's GraphQL API names it by
	Title   string
	URL     string
	State   string // OPEN, CLOSED or MERGED
	Draft   bool
	BaseRef string
	HeadRef string
	BaseOid string
	HeadOid string
}

// PRListed is a pull request as the list of those open has it.
type PRListed struct {
	Number  int
	Title   string
	Author  string
	HeadRef string
	Draft   bool
}

// PullRequestInfo asks the GitHub CLI for pull request number, or for that
// of the branch checked out when number is 0.
func (r *Repo) PullRequestInfo(number int) (*PRInfo, error) {
	args := []string{"pr", "view"}
	if number > 0 {
		args = append(args, strconv.Itoa(number))
	}
	out, err := r.gh(append(args, "--json", "id,number,title,url,state,isDraft,baseRefName,baseRefOid,headRefName,headRefOid")...)
	if err != nil {
		return nil, err
	}
	return parsePRInfo(out)
}

func parsePRInfo(b []byte) (*PRInfo, error) {
	var v struct {
		ID, Title, URL, State   string
		Number                  int
		IsDraft                 bool
		BaseRefName, BaseRefOid string
		HeadRefName, HeadRefOid string
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	if v.Number == 0 {
		return nil, ErrNoPR
	}
	if !isCommitID(v.BaseRefOid) || !isCommitID(v.HeadRefOid) {
		return nil, fmt.Errorf("%w: pull request #%d has no commits", ErrNoGH, v.Number)
	}
	return &PRInfo{Number: v.Number, ID: v.ID, Title: v.Title, URL: v.URL, State: v.State, Draft: v.IsDraft,
		BaseRef: v.BaseRefName, HeadRef: v.HeadRefName, BaseOid: v.BaseRefOid, HeadOid: v.HeadRefOid}, nil
}

// PullRequests lists the pull requests open, the latest first.
func (r *Repo) PullRequests() ([]PRListed, error) {
	out, err := r.gh("pr", "list", "--state", "open", "--limit", "50", "--json", "number,title,author,headRefName,isDraft")
	if err != nil {
		return nil, err
	}
	return parsePRList(out)
}

func parsePRList(b []byte) ([]PRListed, error) {
	var vs []struct {
		Number      int
		Title       string
		HeadRefName string
		IsDraft     bool
		Author      struct{ Login string }
	}
	if err := json.Unmarshal(b, &vs); err != nil {
		return nil, err
	}
	prs := make([]PRListed, 0, len(vs))
	for _, v := range vs {
		prs = append(prs, PRListed{Number: v.Number, Title: v.Title, Author: v.Author.Login, HeadRef: v.HeadRefName, Draft: v.IsDraft})
	}
	return prs, nil
}

// PRHead is a pull request open, as the commit at its head shows it.
type PRHead struct {
	Number int
	Title  string
	URL    string
	Draft  bool
}

// PullRequestHeads lists the pull requests open by the commit at their
// head, which the history marks.
func (r *Repo) PullRequestHeads() (map[string][]PRHead, error) {
	out, err := r.gh("pr", "list", "--state", "open", "--limit", "100", "--json", "number,title,url,isDraft,headRefOid")
	if err != nil {
		return nil, err
	}
	return parsePRHeads(out)
}

func parsePRHeads(b []byte) (map[string][]PRHead, error) {
	var vs []struct {
		Number     int
		Title      string
		URL        string
		IsDraft    bool
		HeadRefOid string
	}
	if err := json.Unmarshal(b, &vs); err != nil {
		return nil, err
	}
	heads := map[string][]PRHead{}
	for _, v := range vs {
		if v.Number == 0 || !isCommitID(v.HeadRefOid) {
			continue
		}
		heads[v.HeadRefOid] = append(heads[v.HeadRefOid], PRHead{Number: v.Number, Title: v.Title, URL: v.URL, Draft: v.IsDraft})
	}
	return heads, nil
}

// GitHubReady reports whether the GitHub CLI is installed and signed in
// where the repository is.
func (r *Repo) GitHubReady() error {
	_, err := r.gh("auth", "status")
	return err
}

// PRRef is the private ref a pull request's head, or its base, is
// fetched to, which keeps its commits from being collected while a tab
// shows them.
func PRRef(number int, head bool) string {
	side := "base"
	if head {
		side = "head"
	}
	return "refs/mayoi/pull/" + strconv.Itoa(number) + "/" + side
}

// FetchPullRequest has the commits of a pull request's base and head in
// the repository, fetching them from the remote of the pull request's
// repository when either is missing, to refs of PRRef. It returns the
// commit the changes start at: where the head left the base, as GitHub
// shows them.
func (r *Repo) FetchPullRequest(pr *PRInfo) (string, error) {
	if !r.hasCommit(pr.BaseOid) || !r.hasCommit(pr.HeadOid) {
		if !validBranch(pr.BaseRef) {
			return "", fmt.Errorf("invalid base branch %q", pr.BaseRef)
		}
		name, err := r.prRemote(pr.URL)
		if err != nil {
			return "", err
		}
		n := pr.Number
		if _, err := r.run(nil, "fetch", "--no-tags", "--", name,
			"+refs/pull/"+strconv.Itoa(n)+"/head:"+PRRef(n, true),
			"+refs/heads/"+pr.BaseRef+":"+PRRef(n, false)); err != nil {
			return "", err
		}
		if !r.hasCommit(pr.HeadOid) || !r.hasCommit(pr.BaseOid) {
			return "", fmt.Errorf("the remote %s does not have the commits of pull request #%d", name, n)
		}
	}
	out, err := r.run(nil, "merge-base", pr.BaseOid, pr.HeadOid)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *Repo) hasCommit(hash string) bool {
	if !isCommitID(hash) {
		return false
	}
	_, err := r.run(nil, "cat-file", "-e", hash+"^{commit}")
	return err == nil
}

// prRemote is the remote of the repository a pull request is of, as its
// URL names it, else the current upstream's remote, origin, or the first.
func (r *Repo) prRemote(prURL string) (string, error) {
	names, err := r.Remotes()
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "", fmt.Errorf("this repository has no remote")
	}
	if repo := prRepoPath(prURL); repo != "" {
		for _, name := range names {
			out, err := r.run(nil, "remote", "get-url", "--", name)
			if err != nil {
				continue
			}
			if _, path, err := remoteRepo(strings.TrimSpace(string(out))); err == nil && strings.EqualFold(path, repo) {
				return name, nil
			}
		}
	}
	raw, _, err := r.remoteURL(names, "")
	if err != nil {
		return "", err
	}
	for _, name := range names {
		if out, err := r.run(nil, "remote", "get-url", "--", name); err == nil && strings.TrimSpace(string(out)) == raw {
			return name, nil
		}
	}
	return names[0], nil
}

// prRepoPath is the owner/name of the repository a pull request's URL
// is of, "" when it is not one.
func prRepoPath(prURL string) string {
	u, err := url.Parse(prURL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 || parts[2] != "pull" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// PRViewed returns the files of a pull request the signed in user marked
// viewed, by their paths in the repository, as GitHub has them.
func (r *Repo) PRViewed(id string) (map[string]bool, error) {
	const query = `query($id:ID!,$after:String){node(id:$id){... on PullRequest{files(first:100,after:$after){nodes{path viewerViewedState} pageInfo{hasNextPage endCursor}}}}}`
	viewed := map[string]bool{}
	after := ""
	for range 100 {
		args := []string{"api", "graphql", "-f", "query=" + query, "-f", "id=" + id}
		if after != "" {
			args = append(args, "-f", "after="+after)
		}
		out, err := r.gh(args...)
		if err != nil {
			return nil, err
		}
		next, err := parsePRViewed(out, viewed)
		if err != nil {
			return nil, err
		}
		if next == "" {
			break
		}
		after = next
	}
	return viewed, nil
}

// parsePRViewed adds the files viewed of a page of a pull request's files
// to viewed, and returns the cursor of the next page, "" for none.
func parsePRViewed(b []byte, viewed map[string]bool) (string, error) {
	var v struct {
		Data struct {
			Node struct {
				Files struct {
					Nodes []struct {
						Path              string
						ViewerViewedState string
					}
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					}
				}
			}
		}
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", err
	}
	fs := v.Data.Node.Files
	for _, n := range fs.Nodes {
		if n.ViewerViewedState == "VIEWED" {
			viewed[n.Path] = true
		}
	}
	if fs.PageInfo.HasNextPage {
		return fs.PageInfo.EndCursor, nil
	}
	return "", nil
}

// SetPRViewed marks a file of a pull request viewed on GitHub, or not, as
// the signed in user; path is its path in the repository.
func (r *Repo) SetPRViewed(id, path string, viewed bool) error {
	mutation := "unmarkFileAsViewed"
	if viewed {
		mutation = "markFileAsViewed"
	}
	query := `mutation($id:ID!,$path:String!){` + mutation + `(input:{pullRequestId:$id,path:$path}){clientMutationId}}`
	_, err := r.gh("api", "graphql", "-f", "query="+query, "-f", "id="+id, "-f", "path="+path)
	return err
}
