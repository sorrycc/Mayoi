package git

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// CommitStatus is how the checks of a commit went, as GitHub sums them
// up: Checks as of a pull request's, and Passed, Failed and Pending count
// them.
type CommitStatus struct {
	Checks                  Checks
	Passed, Failed, Pending int
}

// Done is whether every check of the commit ended, which no later read
// changes unless one runs again: one may have failed while others run.
func (s CommitStatus) Done() bool { return s.Pending == 0 }

// commitChecksBatch is how many commits one question to GitHub asks about.
const commitChecksBatch = 50

// CommitChecks asks GitHub, through gh, how the checks of commits went,
// as many at once as one question holds: by hash, those that have checks.
// A commit GitHub does not have, or has no checks of, is left out. The
// repository is the one gh takes the folder for, as gh pr does.
func (r *Repo) CommitChecks(hashes []string) (map[string]CommitStatus, error) {
	out := map[string]CommitStatus{}
	var ids []string
	for _, h := range hashes {
		if isCommitID(h) && !slices.Contains(ids, h) {
			ids = append(ids, h)
		}
	}
	for len(ids) > 0 {
		batch := ids[:min(len(ids), commitChecksBatch)]
		ids = ids[len(batch):]
		var q strings.Builder
		q.WriteString("query($owner:String!,$name:String!){repository(owner:$owner,name:$name){")
		for i, h := range batch {
			fmt.Fprintf(&q, "c%d:object(oid:%q){...S}", i, h)
		}
		q.WriteString("}}fragment S on Commit{statusCheckRollup{contexts(first:0){checkRunCountsByState{state count}statusContextCountsByState{state count}}}}")
		b, err := r.gh("api", "graphql", "-F", "owner={owner}", "-F", "name={repo}", "-f", "query="+q.String())
		if err != nil {
			return nil, err
		}
		got, err := parseCommitChecks(b, batch)
		if err != nil {
			return nil, err
		}
		for h, s := range got {
			out[h] = s
		}
	}
	return out, nil
}

func parseCommitChecks(b []byte, batch []string) (map[string]CommitStatus, error) {
	type count struct {
		State string
		Count int
	}
	var v struct {
		Data struct {
			Repository map[string]*struct {
				StatusCheckRollup *struct {
					Contexts struct {
						CheckRunCountsByState      []count
						StatusContextCountsByState []count
					}
				}
			}
		}
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	out := map[string]CommitStatus{}
	for alias, c := range v.Data.Repository {
		i, err := strconv.Atoi(strings.TrimPrefix(alias, "c"))
		if err != nil || i < 0 || i >= len(batch) || c == nil || c.StatusCheckRollup == nil {
			continue
		}
		var s CommitStatus
		add := func(cs []count) {
			for _, n := range cs {
				switch checkStateBucket(n.State) {
				case "pass", "skipping":
					s.Passed += n.Count
				case "pending":
					s.Pending += n.Count
				default:
					s.Failed += n.Count
				}
			}
		}
		add(c.StatusCheckRollup.Contexts.CheckRunCountsByState)
		add(c.StatusCheckRollup.Contexts.StatusContextCountsByState)
		switch {
		case s.Failed > 0:
			s.Checks = ChecksFail
		case s.Pending > 0:
			s.Checks = ChecksPending
		case s.Passed > 0:
			s.Checks = ChecksPass
		default:
			continue
		}
		out[batch[i]] = s
	}
	return out, nil
}

// checkStateBucket sorts the state of a check run, or of a commit status,
// as gh pr checks does: pass, fail, pending, skipping or cancel.
func checkStateBucket(state string) string {
	switch state {
	case "SUCCESS", "COMPLETED":
		return "pass"
	case "NEUTRAL", "SKIPPED":
		return "skipping"
	case "CANCELLED":
		return "cancel"
	case "FAILURE", "ERROR", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE", "STALE":
		return "fail"
	}
	// PENDING, EXPECTED, QUEUED, IN_PROGRESS, WAITING, REQUESTED.
	return "pending"
}

// CommitCheckList lists the checks of commit hash, as PRChecks lists a
// pull request's: those failed first, then those running, then the others.
func (r *Repo) CommitCheckList(hash string) ([]Check, error) {
	if !isCommitID(hash) {
		return nil, fmt.Errorf("invalid commit %q", hash)
	}
	const q = `query($owner:String!,$name:String!,$oid:GitObjectID!){repository(owner:$owner,name:$name){object(oid:$oid){... on Commit{statusCheckRollup{contexts(first:100){nodes{__typename ... on CheckRun{name status conclusion detailsUrl checkSuite{workflowRun{workflow{name}}}} ... on StatusContext{context state targetUrl description}}}}}}}}`
	b, err := r.gh("api", "graphql", "-F", "owner={owner}", "-F", "name={repo}", "-f", "oid="+hash, "-f", "query="+q)
	if err != nil {
		return nil, err
	}
	return parseCommitCheckList(b)
}

func parseCommitCheckList(b []byte) ([]Check, error) {
	var v struct {
		Data struct {
			Repository struct {
				Object *struct {
					StatusCheckRollup *struct {
						Contexts struct {
							Nodes []struct {
								Typename                 string `json:"__typename"`
								Name, Status, Conclusion string
								DetailsURL               string `json:"detailsUrl"`
								CheckSuite               *struct {
									WorkflowRun *struct {
										Workflow struct{ Name string }
									}
								}
								Context, State, Description string
								TargetURL                   string `json:"targetUrl"`
							}
						}
					}
				}
			}
		}
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	o := v.Data.Repository.Object
	if o == nil || o.StatusCheckRollup == nil {
		return nil, nil
	}
	// Each is set out as gh pr checks prints it, for parseChecks to read.
	type row struct {
		Name        string `json:"name"`
		State       string `json:"state"`
		Bucket      string `json:"bucket"`
		Link        string `json:"link"`
		Workflow    string `json:"workflow"`
		Description string `json:"description"`
	}
	var rows []row
	for _, n := range o.StatusCheckRollup.Contexts.Nodes {
		if n.Typename == "StatusContext" {
			rows = append(rows, row{Name: n.Context, State: n.State, Bucket: checkStateBucket(n.State), Link: n.TargetURL, Description: n.Description})
			continue
		}
		state := n.Conclusion
		if n.Status != "COMPLETED" || state == "" {
			state = n.Status
		}
		wf := ""
		if n.CheckSuite != nil && n.CheckSuite.WorkflowRun != nil {
			wf = n.CheckSuite.WorkflowRun.Workflow.Name
		}
		rows = append(rows, row{Name: n.Name, State: state, Bucket: checkStateBucket(state), Link: n.DetailsURL, Workflow: wf})
	}
	j, err := json.Marshal(rows)
	if err != nil {
		return nil, err
	}
	return parseChecks(j)
}
