package git

import (
	"errors"
	"testing"
)

func TestParsePR(t *testing.T) {
	pr, err := parsePR([]byte(`{"number":123,"state":"OPEN","url":"https://github.com/o/r/pull/123","reviewDecision":"APPROVED",
		"statusCheckRollup":[
			{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"},
			{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SKIPPED"},
			{"__typename":"CheckRun","status":"IN_PROGRESS","conclusion":""},
			{"__typename":"StatusContext","state":"SUCCESS"}]}`))
	if err != nil || pr.Number != 123 || pr.Review != "APPROVED" || pr.URL != "https://github.com/o/r/pull/123" {
		t.Fatalf("pr = %+v, %v", pr, err)
	}
	if pr.Checks != ChecksPending || pr.Passed != 3 || pr.Pending != 1 || pr.Failed != 0 {
		t.Fatalf("checks = %+v", pr)
	}
	pr, _ = parsePR([]byte(`{"number":1,"statusCheckRollup":[{"__typename":"CheckRun","status":"IN_PROGRESS"},{"__typename":"StatusContext","state":"ERROR"}]}`))
	if pr.Checks != ChecksFail || pr.Failed != 1 {
		t.Fatalf("failed = %+v", pr)
	}
	pr, _ = parsePR([]byte(`{"number":1,"statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"}]}`))
	if pr.Checks != ChecksPass {
		t.Fatalf("passed = %+v", pr)
	}
	pr, _ = parsePR([]byte(`{"number":1,"statusCheckRollup":[]}`))
	if pr.Checks != ChecksNone {
		t.Fatalf("none = %+v", pr)
	}
	if _, err := parsePR([]byte(`{}`)); !errors.Is(err, ErrNoPR) {
		t.Fatalf("empty = %v", err)
	}
}

func TestParsePRHeads(t *testing.T) {
	a, b := "0123456789abcdef0123456789abcdef01234567", "89abcdef0123456789abcdef0123456789abcdef"
	heads, err := parsePRHeads([]byte(`[
		{"number":7,"title":"Fix","url":"https://github.com/o/r/pull/7","isDraft":false,"headRefOid":"` + a + `"},
		{"number":9,"title":"Try","url":"https://github.com/o/r/pull/9","isDraft":true,"headRefOid":"` + a + `"},
		{"number":3,"title":"Other","headRefOid":"` + b + `"},
		{"number":4,"title":"Broken","headRefOid":""}]`))
	if err != nil || len(heads) != 2 {
		t.Fatalf("heads = %+v, %v", heads, err)
	}
	if got := heads[a]; len(got) != 2 || got[0] != (PRHead{Number: 7, Title: "Fix", URL: "https://github.com/o/r/pull/7"}) || !got[1].Draft {
		t.Errorf("at a = %+v", got)
	}
	if got := heads[b]; len(got) != 1 || got[0].Number != 3 {
		t.Errorf("at b = %+v", got)
	}
	if heads, err := parsePRHeads([]byte(`[]`)); err != nil || len(heads) != 0 {
		t.Errorf("empty = %+v, %v", heads, err)
	}
}
