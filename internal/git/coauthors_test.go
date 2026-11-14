package git

import (
	"slices"
	"testing"
)

func TestParseCoAuthors(t *testing.T) {
	body := "Detail.\n\nCo-authored-by: Claude <noreply@anthropic.com>\nco-authored-by: Jane Doe <jane@x.io>\n" +
		"Co-Authored-By: Claude <NOREPLY@anthropic.com>\nCo-authored-by: Me <me@x.io>\nCo-authored-by: Bob\nCo-authored-by:  "
	got := parseCoAuthors(body, "Me", "me@x.io")
	want := []Person{{"Claude", "noreply@anthropic.com"}, {"Jane Doe", "jane@x.io"}, {"Bob", ""}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := parseCoAuthors("no trailers", "a", "b"); got != nil {
		t.Fatalf("got %v, want none", got)
	}
}

func TestParseNumstat(t *testing.T) {
	got := parseNumstat("3\t1\ta.go\n-\t-\timg.png\n10\t0\tb/c.go\n")
	if want := (CommitStat{Files: 3, Added: 13, Deleted: 1}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
