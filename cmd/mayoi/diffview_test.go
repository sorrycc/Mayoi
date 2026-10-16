package main

import "testing"

func TestRefreshDiffHandlesShrunkFileList(t *testing.T) {
	a := &app{}
	v := newDiffView(layoutInline)
	v.commit = &commitDiff{loaded: true}
	v.files = []*diffFile{{}, {}, {}, {}}
	v.near = [2]int{10, 18}

	a.refreshDiff(v)
}

func TestDiffAnchorIgnoresStaleFileRow(t *testing.T) {
	v := newDiffView(layoutInline)
	v.files = []*diffFile{{}, {}, {}, {}}
	v.rows = []diffRow{{kind: rowFile, file: 4}}

	if got := v.anchorOf(0); got != (diffAnchor{}) {
		t.Errorf("stale row has anchor %+v", got)
	}
}
