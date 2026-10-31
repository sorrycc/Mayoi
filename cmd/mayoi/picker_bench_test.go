package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/locales"
)

// makeRefs writes n branches at HEAD of the repository, in one git
// process, which a benchmark of a long list needs.
func makeRefs(b *testing.B, hs *harness, prefix string, n int) {
	var in bytes.Buffer
	for i := range n {
		fmt.Fprintf(&in, "create refs/heads/%s-%04d HEAD\n", prefix, i)
	}
	cmd := exec.Command("git", "-C", hs.root, "update-ref", "--stdin")
	cmd.Stdin = &in
	if out, err := cmd.CombinedOutput(); err != nil {
		b.Fatalf("update-ref: %v\n%s", err, out)
	}
}

// awaitBox draws frames until the box that opened is read.
func awaitBox(b *testing.B, hs *harness, loading func() bool) {
	b.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for loading() && time.Now().Before(deadline) {
		hs.settle()
	}
	if loading() {
		b.Fatal("the box never finished reading")
	}
}

// benchTyping measures the frame after each key typed into a box that is
// open. The first key of a query is typed before the timing: it takes the
// box from its empty query, with the headings of the groups, to its
// matches, which settles outside the timing. The box is shut and opened
// again when the query is typed through, outside the timing as well.
func benchTyping(b *testing.B, hs *harness, open func(), query string, loading func() bool, shut func()) {
	b.Helper()
	restart := func() {
		open()
		awaitBox(b, hs, loading)
		hs.tt.Type(query[:1])
		hs.settle()
	}
	restart()
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		k := i%(len(query)-1) + 1
		if k == 1 && i > 0 {
			b.StopTimer()
			shut()
			restart()
			b.StartTimer()
		}
		hs.tt.Type(query[k : k+1])
		step(hs.tt)
	}
}

// BenchmarkBranchPickerTyping is a frame of the box of 2000 branches for
// each key typed into its query, as the box is drawn.
func BenchmarkBranchPickerTyping(b *testing.B) {
	hs := newHarness(b, true)
	hs.gitRepo()
	makeRefs(b, hs, "topic", 2000)
	hs.settleGit()
	openSCM(hs)
	benchTyping(b, hs,
		func() {
			if err := hs.tt.Click(locales.BranchLabel("main")); err != nil {
				b.Fatal(err)
			}
			hs.settle()
		},
		"topic-01", func() bool { return hs.a.branches.loading },
		func() { hs.tt.Key(0, ui.KeyEscape); hs.settle() })
}

// BenchmarkWorktreePickerTyping is a frame of the box of 200 worktrees for
// each key typed into its query.
func BenchmarkWorktreePickerTyping(b *testing.B) {
	hs := newHarness(b, true)
	run := hs.gitRepo()
	parent := filepath.Dir(hs.root)
	for i := range 200 {
		folder := filepath.Join(parent, fmt.Sprintf("wt-%03d", i))
		run("worktree", "add", "-q", "--detach", folder)
	}
	hs.a.git.checked = time.Time{}
	hs.settleGit()
	openSCM(hs)
	benchTyping(b, hs,
		func() {
			if err := hs.tt.Click(locales.WorktreeStatusTip(filepath.Base(hs.root))); err != nil {
				b.Fatal(err)
			}
			hs.settle()
		},
		"wt-01", func() bool { return hs.a.worktrees.loading },
		func() { hs.tt.Key(0, ui.KeyEscape); hs.settle() })
}

// BenchmarkIdleFrameRepo is an idle frame of a window in a Git work tree.
func BenchmarkIdleFrameRepo(b *testing.B) {
	hs := newHarness(b, true)
	hs.gitRepo()
	hs.settle()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		step(hs.tt)
	}
}

// BenchmarkBranchPickerFirstKey is the first key typed into the box of 2000
// branches, from its empty query, timed with the frames that Type settles
// through. The tester's clock stands still while it settles, so the view's
// requests for frames run to the tester's cap of 20 frames: this measures
// the cost of those frames, not the latency of a real window. The box is
// opened and shut again, untimed, for each key.
func BenchmarkBranchPickerFirstKey(b *testing.B) {
	hs := newHarness(b, true)
	hs.gitRepo()
	makeRefs(b, hs, "topic", 2000)
	hs.settleGit()
	openSCM(hs)
	open := func() {
		if err := hs.tt.Click(locales.BranchLabel("main")); err != nil {
			b.Fatal(err)
		}
		hs.settle()
		awaitBox(b, hs, func() bool { return hs.a.branches.loading })
	}
	shut := func() { hs.tt.Key(0, ui.KeyEscape); hs.settle() }
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		open()
		b.StartTimer()
		hs.tt.Type("t")
		b.StopTimer()
		shut()
		b.StartTimer()
	}
}
