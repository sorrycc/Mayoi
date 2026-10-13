package workspace

import "testing"

func TestPollBaselineAndWatchRemoval(t *testing.T) {
	stamps := map[string]string{}
	if observeStamp(stamps, "a", "1:10") {
		t.Fatal("first observation reported a change")
	}
	if observeStamp(stamps, "a", "1:10") {
		t.Fatal("unchanged file reported a change")
	}
	if !observeStamp(stamps, "a", "2:10") {
		t.Fatal("modification missed")
	}
	if !observeStamp(stamps, "a", "gone") {
		t.Fatal("deletion missed")
	}
	if observeStamp(stamps, "a", "gone") {
		t.Fatal("deletion repeated")
	}
	if !observeStamp(stamps, "a", "3:10") {
		t.Fatal("recreation missed")
	}
	tree := &Tree{stop: make(chan struct{}), pollStamps: stamps, pollFiles: map[string]bool{"a": true}}
	tree.SetPollTargets([]string{"root"}, nil)
	if len(tree.pollFiles) != 0 || len(tree.pollStamps) != 0 {
		t.Fatal("closed file remains watched")
	}
	if len(tree.pollDirs) != 1 || !tree.pollDirs["root"] {
		t.Fatal("collapsed directories remain watched")
	}
	tree.SetPollTargets([]string{"root"}, []string{"a"})
	if observeStamp(tree.pollStamps, "a", "3:10") {
		t.Fatal("reopened file inherited a stale baseline")
	}
}
