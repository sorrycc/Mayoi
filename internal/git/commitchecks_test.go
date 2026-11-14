package git

import "testing"

func TestParseCommitChecks(t *testing.T) {
	a, b, c, d := "0123456789abcdef0123456789abcdef01234567", "89abcdef0123456789abcdef0123456789abcdef",
		"1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222"
	got, err := parseCommitChecks([]byte(`{"data":{"repository":{
		"c0":{"statusCheckRollup":{"contexts":{
			"checkRunCountsByState":[{"state":"SUCCESS","count":20},{"state":"FAILURE","count":1},{"state":"IN_PROGRESS","count":2}],
			"statusContextCountsByState":[{"state":"SUCCESS","count":1}]}}},
		"c1":{"statusCheckRollup":{"contexts":{
			"checkRunCountsByState":[{"state":"SKIPPED","count":1},{"state":"QUEUED","count":1}],
			"statusContextCountsByState":[{"state":"PENDING","count":0}]}}},
		"c2":null,
		"c3":{"statusCheckRollup":null},
		"c9":{"statusCheckRollup":{"contexts":{"checkRunCountsByState":[{"state":"SUCCESS","count":1}]}}}}}}`), []string{a, b, c, d})
	if err != nil || len(got) != 2 {
		t.Fatalf("got = %+v, %v", got, err)
	}
	if s := got[a]; s != (CommitStatus{Checks: ChecksFail, Passed: 21, Failed: 1, Pending: 2}) || s.Done() {
		t.Errorf("a = %+v", s)
	}
	if s := got[b]; s != (CommitStatus{Checks: ChecksPending, Passed: 1, Pending: 1}) || s.Done() {
		t.Errorf("b = %+v", s)
	}
	got, _ = parseCommitChecks([]byte(`{"data":{"repository":{"c0":{"statusCheckRollup":{"contexts":{
		"checkRunCountsByState":[{"state":"SUCCESS","count":2},{"state":"NEUTRAL","count":1}]}}}}}}`), []string{a})
	if s := got[a]; s != (CommitStatus{Checks: ChecksPass, Passed: 3}) || !s.Done() {
		t.Errorf("passed = %+v", s)
	}
	got, _ = parseCommitChecks([]byte(`{"data":{"repository":{"c0":{"statusCheckRollup":{"contexts":{
		"checkRunCountsByState":[{"state":"SUCCESS","count":0}]}}}}}}`), []string{a})
	if len(got) != 0 {
		t.Errorf("none = %+v", got)
	}
}

func TestParseCommitCheckList(t *testing.T) {
	checks, err := parseCommitCheckList([]byte(`{"data":{"repository":{"object":{"statusCheckRollup":{"contexts":{"nodes":[
		{"__typename":"CheckRun","name":"Lint","status":"COMPLETED","conclusion":"SUCCESS","detailsUrl":"https://github.com/o/r/actions/runs/5/job/6","checkSuite":{"workflowRun":{"workflow":{"name":"Test"}}}},
		{"__typename":"CheckRun","name":"Build","status":"IN_PROGRESS","conclusion":null,"detailsUrl":"https://github.com/o/r/actions/runs/5/job/7","checkSuite":{"workflowRun":{"workflow":{"name":"Test"}}}},
		{"__typename":"CheckRun","name":"Unit","status":"COMPLETED","conclusion":"FAILURE","detailsUrl":"https://github.com/o/r/actions/runs/5/job/8","checkSuite":{"workflowRun":{"workflow":{"name":"Test"}}}},
		{"__typename":"StatusContext","context":"ci/other","state":"SUCCESS","targetUrl":"https://ci.example/1","description":"Fine"}]}}}}}}`))
	if err != nil || len(checks) != 4 {
		t.Fatalf("checks = %+v, %v", checks, err)
	}
	if c := checks[0]; c.Name != "Unit" || c.Bucket != "fail" || c.Title() != "Test / Unit" || c.JobID != 8 || c.RunID != 5 || c.Owner != "o" {
		t.Errorf("first = %+v", c)
	}
	if c := checks[1]; c.Name != "Build" || c.Bucket != "pending" || c.State != "IN_PROGRESS" {
		t.Errorf("second = %+v", c)
	}
	if c := checks[3]; c.Name != "ci/other" || c.Bucket != "pass" || c.IsJob() || c.Description != "Fine" || c.Link != "https://ci.example/1" {
		t.Errorf("status = %+v", c)
	}
	if checks, err := parseCommitCheckList([]byte(`{"data":{"repository":{"object":null}}}`)); err != nil || checks != nil {
		t.Errorf("missing = %+v, %v", checks, err)
	}
}
