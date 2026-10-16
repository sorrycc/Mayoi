package main

import "testing"

// A diff whose files went down to fewer than those near its view, as when
// changes are committed while it is scrolled to the last of them, reads
// what is left rather than slicing past it.
func TestRefreshDiffHandlesShrunkFileList(t *testing.T) {
	a := &app{}
	v := newDiffView(layoutInline)
	v.commit = &commitDiff{loaded: true}
	v.files = []*diffFile{{}, {}, {}, {}}
	v.near = [2]int{10, 18}

	a.refreshDiff(v)
}

// A row laid out for a file no longer in the diff anchors nothing.
func TestDiffAnchorIgnoresStaleFileRow(t *testing.T) {
	v := newDiffView(layoutInline)
	v.files = []*diffFile{{}, {}, {}, {}}
	v.rows = []diffRow{{kind: rowFile, file: 4}}

	if got := v.anchorOf(0); got != (diffAnchor{}) {
		t.Errorf("stale row has anchor %+v", got)
	}
}
