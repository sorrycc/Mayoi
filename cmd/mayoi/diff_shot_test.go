package main

import (
	"os"
	"strings"
	"testing"
)

const diffShotOld = `package store

import (
	"errors"
	"sync"
)

// Store keeps values by their keys.
type Store struct {
	mu   sync.Mutex
	vals map[string]string
}

// Get returns the value of key.
func (s *Store) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.vals[key]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

// Put sets the value of key.
func (s *Store) Put(key, val string) {
	s.mu.Lock()
	s.vals[key] = val
	s.mu.Unlock()
}

// Len is how many values the store keeps.
func (s *Store) Len() int {
	return len(s.vals)
}
`

const diffShotNew = `package store

import (
	"errors"
	"fmt"
	"sync"
)

// Store keeps values by their keys, safe for use by many goroutines.
type Store struct {
	mu   sync.RWMutex
	vals map[string]string
}

// ErrNotFound is the error of a key the store lacks.
var ErrNotFound = errors.New("not found")

// Get returns the value of key.
func (s *Store) Get(key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.vals[key]
	if !ok {
		return "", fmt.Errorf("%q: %w", key, ErrNotFound)
	}
	return v, nil
}

// Put sets the value of key.
func (s *Store) Put(key, val string) {
	s.mu.Lock()
	s.vals[key] = val
	s.mu.Unlock()
}
`

// The diff's interactions, for the eye: its sides, its arrows, the
// editing of its working tree side, a comparison of two files and a merge.
func TestShotDiffInteractions(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	for _, dark := range []bool{true, false} {
		name := "dfx-light"
		if dark {
			name = "dfx-dark"
		}
		hs := newHarness(t, dark)
		git := hs.gitRepo()
		a := hs.a
		os.WriteFile(hs.path("store.go"), []byte(diffShotOld), 0o644)
		git("add", "store.go")
		git("commit", "-qm", "store")
		os.WriteFile(hs.path("store.go"), []byte(diffShotNew), 0o644)
		hs.settleGit()
		a.showChanges(diffTarget{path: hs.path("store.go"), base: diffIndex}, true)
		v := a.activeTab().diff
		hs.settleDiff(v)
		a.setDiffLayout(v, layoutSplit)
		hs.settle()
		hs.shot(name + "-split")
		// The pointer over a change's arrow.
		from, to := v.hunkRows(1)
		y := titleBarH + tabStripH + crumbsH + editorPadY + float32(v.tops[from]+v.tops[to])/2*a.font.lineH - v.scroll.Y
		hs.tt.Move(a.window.SidebarWidth+v.viewW/2, y)
		hs.settle()
		hs.shot(name + "-arrow")
		// Editing the working tree side.
		if row := diffEditRow(v, hs.path("store.go"), 19); row >= 0 && a.beginDiffEdit(v, row) {
			v.pane.Focus()
			hs.settle()
			hs.shot(name + "-edit")
			a.endDiffEdit(v)
		}
		a.settings.DiffAlignedRows = false
		hs.settle()
		hs.shot(name + "-streams")
		// Scrolled, the right side follows the left.
		v.scroll.Y = 120
		hs.settle()
		hs.shot(name + "-streams-scrolled")
		v.scroll.Y = 0
		a.settings.DiffAlignedRows = true
		a.setDiffLayout(v, layoutInline)
		hs.settle()
		hs.shot(name + "-inline")

		// Two files compared, each side writable.
		left, right := hs.path("left.go"), hs.path("right.go")
		os.WriteFile(left, []byte(diffShotOld), 0o644)
		os.WriteFile(right, []byte(strings.Replace(diffShotNew, "Put sets", "Put stores", 1)), 0o644)
		lv := newLocalCompareView(layoutSplit, left, right)
		col := a.column()
		at := min(col.active+1, len(col.tabs))
		a.addTab(col, at, &colTab{diff: lv})
		a.activate(col, at, true)
		hs.settleDiff(lv)
		hs.settle()
		hs.shot(name + "-compare")

		// A merge stopped for conflicts.
		hs2 := newHarness(t, dark)
		hs2.mergeRepo(
			"package main\n\nfunc main() {\n\tprintln(\"a\")\n\tprintln(\"b\")\n\tprintln(\"c\")\n}\n\nfunc helper() int {\n\treturn 1\n}\n",
			"package main\n\nfunc main() {\n\tprintln(\"a\")\n\tprintln(\"ours\")\n\tprintln(\"c\")\n}\n\nfunc helper() int {\n\treturn 1\n}\n",
			"package main\n\nfunc main() {\n\tprintln(\"a\")\n\tprintln(\"theirs\")\n\tprintln(\"c\")\n}\n\nfunc helper() int {\n\treturn 2\n}\n")
		mv := hs2.mergeOf()
		hs2.settle()
		hs2.shot(name + "-merge")
		hs2.a.takeMergeSide(mv, 0, true)
		hs2.settle()
		hs2.shot(name + "-merge-taken")
	}
}
