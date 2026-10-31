package git

import "testing"

func TestParseChecks(t *testing.T) {
	b := []byte(`[
		{"name":"lint","state":"SUCCESS","bucket":"pass","link":"https://github.com/o/r/actions/runs/11/job/21","workflow":"CI"},
		{"name":"deploy","state":"PENDING","bucket":"pending","link":"https://vercel.com/o/r/abc","workflow":""},
		{"name":"test","state":"FAILURE","bucket":"fail","link":"https://github.com/o/r/actions/runs/12/job/22","workflow":"CI"},
		{"name":"docs","state":"SKIPPED","bucket":"skipping","link":"https://github.com/o/r/actions/runs/13/job/23","workflow":"Docs"}
	]`)
	cs, err := parseChecks(b)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range cs {
		names = append(names, c.Name)
	}
	if got := len(names); got != 4 || names[0] != "test" || names[1] != "deploy" || names[2] != "lint" || names[3] != "docs" {
		t.Fatalf("order = %v", names)
	}
	test := cs[0]
	if !test.IsJob() || test.Host != "github.com" || test.Owner != "o" || test.Repo != "r" || test.RunID != 12 || test.JobID != 22 {
		t.Errorf("test = %+v", test)
	}
	if test.Title() != "CI / test" || test.repoArg() != "github.com/o/r" {
		t.Errorf("title %q, repo %q", test.Title(), test.repoArg())
	}
	if cs[1].IsJob() || cs[1].Title() != "deploy" {
		t.Errorf("deploy = %+v", cs[1])
	}
}

func TestParseJobLink(t *testing.T) {
	for _, tc := range []struct {
		link string
		job  int64
	}{
		{"https://github.com/o/r/actions/runs/1/job/2", 2},
		{"https://ghe.corp/o/r/actions/runs/1/job/3?pr=4", 3},
		{"https://github.com/o/r/runs/2", 0},
		{"https://github.com/o/r/actions/runs/1", 0},
		{"", 0},
	} {
		if _, _, _, _, job := parseJobLink(tc.link); job != tc.job {
			t.Errorf("%s: job %d, want %d", tc.link, job, tc.job)
		}
	}
}

func TestCleanLog(t *testing.T) {
	log := "\ufeff2026-10-29T09:12:33.1234567Z ##[group]Run go test\r\n" +
		"2026-10-29T09:12:34.0000000Z \x1b[36;1mgo test ./...\x1b[0m\r\n" +
		"2026-10-29T09:12:35.0000000Z progress 10%\rprogress 100%\n" +
		"2026-10-29T09:12:36.0000000Z \x1b]8;;https://x\x07link\x1b]8;;\x07 FAIL\n"
	if got, want := cleanLog([]byte(log), 0), "##[group]Run go test\ngo test ./...\nprogress 100%\nlink FAIL"; got != want {
		t.Errorf("cleanLog = %q, want %q", got, want)
	}
	if got := cleanLog([]byte(log), 2); got != "progress 100%\nlink FAIL" {
		t.Errorf("last 2 = %q", got)
	}
}
