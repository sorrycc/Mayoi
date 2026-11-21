package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"mayoi/internal/plugin"
)

func TestPluginBufferDiff(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.settings.AutoSave = false
	a.newFile()
	left := a.activeTab()
	left.ed.buf.Set("one\nleft\n")
	a.newFile()
	right := a.activeTab()
	right.ed.buf.Set("one\nright\n")
	h := withPlugins(hs, map[string]map[string]string{"compare": {
		"plugin.json": `{"name":"compare","main":"main.ts","contributes":{"commands":[{"id":"compare.open","title":"Compare"}]}}`,
		"main.ts":     `import {window} from "mayoi"; export default () => {}; import {commands} from "mayoi"; commands.register("compare.open", arg => {const refs = JSON.parse(arg); return window.showDiff(refs[0], refs[1]);});`,
	}})
	refs := []plugin.DocumentRef{a.documentRef(left.ed.document), a.documentRef(right.ed.document)}
	pluginRun(t, hs, h, "compare.open", mustJSON(t, refs), nil)
	v := a.activeTab().diff
	if v == nil {
		t.Fatal("no native diff")
	}
	hs.settleDiff(v)
	f := v.files[0]
	if f.old.text != "one\nleft\n" || f.new.text != "one\nright\n" || len(f.hunks) != 1 {
		t.Fatalf("wrong buffer contents: %#v / %#v", f.old, f.new)
	}
	if len(hunkArrows(f)) != 0 {
		t.Fatal("buffer comparison must be read-only")
	}
	left.ed.buf.Set("one\nright\n")
	hs.settleDiff(v)
	if len(f.hunks) != 0 {
		t.Fatal("comparison did not refresh")
	}
	left.ed.buf.Set("final unsaved\n")
	a.dropTab(left)
	hs.settleDiff(v)
	if f.old.text != "final unsaved\n" || !f.buffers.closed[0] || !strings.Contains(v.name(), "Closed") {
		t.Fatalf("source close lost final snapshot: %q, %v", f.old.text, f.buffers.closed)
	}
	a.newFile()
	a.activeEditor().buf.Set("replacement")
	a.activate(a.column(), 1, false)
	hs.settleDiff(v)
	if f.old.text != "final unsaved\n" {
		t.Fatal("closed source rebound")
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPluginBufferDiffRejectsInvalidSources(t *testing.T) {
	hs, other := newHarness(t, false), newHarness(t, false)
	a, b := hs.a, other.a
	a.newFile()
	b.newFile()
	h := withPlugins(hs, nil)
	h.apps = func() []*app { return []*app{a, b} }
	left, right := a.documentRef(a.activeEditor().document), b.documentRef(b.activeEditor().document)
	for _, refs := range [][2]plugin.DocumentRef{{left, right}, {left, left}, {left, {WindowID: left.WindowID, DocumentID: 9999}}} {
		reply := pluginCall(t, hs, h, "window.showDiff", map[string]any{"left": refs[0], "right": refs[1]})
		if reply.err == nil {
			t.Fatalf("accepted invalid sources: %v", refs)
		}
	}
}

func TestPluginBufferDiffSavedSources(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.settings.AutoSave = false
	leftPath, rightPath := hs.path("left.txt"), hs.path("right.txt")
	for _, path := range []string{leftPath, rightPath} {
		if err := os.WriteFile(path, []byte("disk\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a.openFile(leftPath, true)
	hs.settle()
	left := a.activeEditor()
	a.openFile(rightPath, true)
	hs.settle()
	right := a.activeEditor()
	left.buf.Set("unsaved left\n")
	right.buf.Set("unsaved right\n")
	h := withPlugins(hs, nil)
	result := pluginCall(t, hs, h, "window.showDiff", map[string]any{"left": a.documentRef(left.document), "right": a.documentRef(right.document), "layout": "split"})
	if result.err != nil {
		t.Fatal(result.err)
	}
	v := a.activeTab().diff
	hs.settleDiff(v)
	f := v.files[0]
	if f.old.text != "unsaved left\n" || f.new.text != "unsaved right\n" {
		t.Fatal("read disk instead of buffers")
	}
	for _, path := range []string{leftPath, rightPath} {
		data, _ := os.ReadFile(path)
		if string(data) != "disk\n" {
			t.Fatal("comparison wrote source files")
		}
	}
	renamed := hs.path("renamed.go")
	a.saveDocAs(left.document, renamed)
	hs.settleDiff(v)
	if f.buffers.ids[0] != left.id || !strings.Contains(v.name(), "renamed.go") {
		t.Fatal("Save As lost source identity or name")
	}
	// Queue a comparison, then change the source before the main thread accepts it.
	left.buf.Set("first job\n")
	a.refreshDiffFile(v, f)
	left.buf.Set("newest\n")
	hs.settleDiff(v)
	if f.old.text != "newest\n" {
		t.Fatal("old job overwrote a newer buffer")
	}
	left.edit([2]int{}, textEdit{0, left.buf.Snapshot().Len(), "redo text\n"})
	hs.settleDiff(v)
	if !left.undo() {
		t.Fatal("source edit cannot undo")
	}
	hs.settleDiff(v)
	if f.old.text != "newest\n" {
		t.Fatal("undo did not refresh comparison")
	}
	if !left.redo() {
		t.Fatal("source edit cannot redo")
	}
	hs.settleDiff(v)
	if f.old.text != "redo text\n" {
		t.Fatal("redo did not refresh comparison")
	}
	a.captureBuffers(f)
	versions := [2]uint64{left.buf.Version(), right.buf.Version()}
	left.buf.Set("changed again\n")
	if a.bufferVersionsCurrent(f.buffers, versions) {
		t.Fatal("stale source version accepted")
	}
	// A reopened file is a different document, even at the same path.
	tab := a.column().tabs[0]
	a.dropTab(tab)
	a.openFile(renamed, true)
	hs.settle()
	if a.activeEditor().id == f.buffers.ids[0] {
		t.Fatal("reused closed document ID")
	}
	a.activate(a.column(), 1, false)
	hs.settleDiff(v)
	if !f.buffers.closed[0] || f.old.text != "changed again\n" {
		t.Fatal("reopen rebound closed source")
	}
}
