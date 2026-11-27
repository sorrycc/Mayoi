package main

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

const actionText = "needimports\nbad\n"

func sourceActionHarness(t *testing.T, mode string) (*harness, *editor) {
	t.Helper()
	hs := withFakeServer(t)
	hs.a.settings.AutoSave = false
	options, _ := json.Marshal(map[string]any{"initializationOptions": map[string]any{"sourceActions": true, "actionMode": mode}})
	hs.a.settings.LanguageServers = map[string]json.RawMessage{"fake.server": options}
	plugins.syncPrefs()
	d := openHover(hs, "actions.fake", actionText)
	return hs, d
}

func TestSaveCodeActionsSettings(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		imports, fix, wildcard, auto bool
		want                         string
	}{
		{name: "default", want: actionText},
		{name: "imports", imports: true, want: "// organized\n" + actionText},
		{name: "fixAll", fix: true, want: "needimports\ngood\n"},
		{name: "both", imports: true, fix: true, want: "// organized\nneedimports\ngood\n"},
		{name: "wildcard", imports: true, fix: true, wildcard: true, want: "// organized\nneedimports\ngood\n"},
		{name: "autoSave", imports: true, fix: true, auto: true, want: "edit\n" + actionText},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hs, d := sourceActionHarness(t, "")
			language := "fake"
			if tc.wildcard {
				language = "*"
			}
			hs.a.settings.OrganizeImportsOnSave = map[string]bool{language: tc.imports}
			hs.a.settings.FixAllOnSave = map[string]bool{language: tc.fix}
			var got string
			if tc.auto {
				hs.a.settings.AutoSave = true
				d.edit(d.sel, textEdit{0, 0, "edit\n"})
				hs.a.autoSaveDoc(d.document)
				hs.until("the automatic save", func() bool { return !d.dirty() })
				data, err := os.ReadFile(d.path)
				if err != nil {
					t.Fatal(err)
				}
				got = string(data)
			} else {
				got = saveAndRead(t, hs, d)
			}
			if got != tc.want || d.dirty() {
				t.Fatalf("saved %q, dirty %v; want %q", got, d.dirty(), tc.want)
			}
		})
	}
}

func TestSaveCodeActionsOrder(t *testing.T) {
	hs, d := sourceActionHarness(t, "")
	a := hs.a
	a.settings.OrganizeImportsOnSave = map[string]bool{"fake": true}
	a.settings.FixAllOnSave = map[string]bool{"fake": true}
	a.settings.FormatOnSave = map[string]bool{"fake": true}
	want := "// formatted\n// organized\nneedimports\ngood\n"
	if got := saveAndRead(t, hs, d); got != want {
		t.Fatalf("saved %q, want %q", got, want)
	}
	hs.settle()
	c := serverOf(a.root + "\x00fake.server").live()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	data, err := c.Call(ctx, "test/snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Events     []string
		SavedTexts map[string]string
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	previous := -1
	for _, event := range []string{"source.organizeImports", "source.fixAll", "textDocument/formatting", "textDocument/didSave"} {
		at := slices.Index(snapshot.Events, event)
		if at <= previous {
			t.Fatalf("save order: %v", snapshot.Events)
		}
		previous = at
	}
	if snapshot.SavedTexts[serverURI(d.path)] != want {
		t.Fatalf("didSave saw stale text: %s", data)
	}
}

func TestSaveCodeActionsUndoAndRepeat(t *testing.T) {
	hs, d := sourceActionHarness(t, "")
	hs.a.settings.OrganizeImportsOnSave = map[string]bool{"fake": true}
	hs.a.settings.FixAllOnSave = map[string]bool{"fake": true}
	placeCaret(d, d.buf.Snapshot().Len())
	want := "// organized\nneedimports\ngood\n"
	steps := len(d.hist.undo)
	if got := saveAndRead(t, hs, d); got != want {
		t.Fatal(got)
	}
	if d.sel[1] != len(want) || len(d.hist.undo) != steps+2 {
		t.Fatalf("caret %v, undo steps %d", d.sel, len(d.hist.undo)-steps)
	}
	version := d.buf.Version()
	if got := saveAndRead(t, hs, d); got != want || d.buf.Version() != version {
		t.Fatal("repeated save changed organized text")
	}
	d.undo()
	if got := d.buf.String(); got != "// organized\n"+actionText {
		t.Fatalf("undo fix: %q", got)
	}
	d.undo()
	if got := d.buf.String(); got != actionText {
		t.Fatalf("undo imports: %q", got)
	}
}

func TestSaveCodeActionsUnsupportedEdits(t *testing.T) {
	for _, mode := range []string{"command", "foreign", "outdated", "invalid", "overlap", "error"} {
		t.Run(mode, func(t *testing.T) {
			hs, d := sourceActionHarness(t, mode)
			hs.a.settings.OrganizeImportsOnSave = map[string]bool{"fake": true}
			if got := saveAndRead(t, hs, d); got != actionText {
				t.Fatalf("applied unsupported edit: %q", got)
			}
			if !told(hs, "Could not run Organize Imports") {
				t.Fatalf("failure not shown: %q", hs.tt.Texts())
			}
		})
	}
}

func TestSaveCodeActionsNewerText(t *testing.T) {
	hs, d := sourceActionHarness(t, "late")
	hs.a.settings.OrganizeImportsOnSave = map[string]bool{"fake": true}
	saved := false
	hs.a.saveDoc(d.document, func() { saved = true })
	holdLate(hs)
	d.edit(d.sel, textEdit{0, 0, "newer\n"})
	releaseLate(hs)
	hs.until("the latest text saved", func() bool { return saved })
	data, err := os.ReadFile(d.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "newer\n"+actionText || d.dirty() {
		t.Fatalf("saved %q, dirty %v", data, d.dirty())
	}
}

func TestSaveCodeActionsQueuedSaves(t *testing.T) {
	hs, d := sourceActionHarness(t, "late")
	hs.a.settings.OrganizeImportsOnSave = map[string]bool{"fake": true}
	saved := 0
	hs.a.saveDoc(d.document, func() { saved++ })
	holdLate(hs)
	hs.a.saveDoc(d.document, func() { saved++ })
	data, err := os.ReadFile(d.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != actionText || saved != 0 {
		t.Fatal("saved before the code action finished")
	}
	releaseLate(hs)
	hs.until("both saves", func() bool { return saved == 2 })
	data, err = os.ReadFile(d.path)
	if err != nil || string(data) != "// organized\n"+actionText {
		t.Fatalf("queued saves: %q, %v", data, err)
	}
}

func TestSaveCodeActionsSaveAllAndSaveAs(t *testing.T) {
	hs, first := sourceActionHarness(t, "")
	a := hs.a
	a.settings.OrganizeImportsOnSave = map[string]bool{"fake": true}
	a.settings.FixAllOnSave = map[string]bool{"fake": true}
	second := openHover(hs, "second.fake", actionText)
	hs.settle()
	first.edit(first.sel, textEdit{0, 0, "first\n"})
	second.edit(second.sel, textEdit{0, 0, "second\n"})
	a.saveAll()
	hs.until("all files saved", func() bool { return !first.dirty() && !second.dirty() })
	for _, c := range []struct {
		ed     *editor
		prefix string
	}{{first, "first"}, {second, "second"}} {
		data, err := os.ReadFile(c.ed.path)
		want := "// organized\n" + c.prefix + "\nneedimports\ngood\n"
		if err != nil || string(data) != want {
			t.Fatalf("saved %q, %v; want %q", data, err, want)
		}
	}
	a.newFile()
	d := a.activeEditor()
	d.edit(d.sel, textEdit{0, 0, actionText})
	a.saveDocAs(d.document, hs.path("new.fake"))
	hs.until("saved as", func() bool { return !d.dirty() })
	data, err := os.ReadFile(d.path)
	if err != nil || string(data) != "// organized\nneedimports\ngood\n" {
		t.Fatalf("save as: %q, %v", data, err)
	}
}

func TestSaveCodeActionsDeadline(t *testing.T) {
	hs, d := sourceActionHarness(t, "noReply")
	hs.a.settings.FixAllOnSave = map[string]bool{"fake": true}
	start := time.Now()
	if got := saveAndRead(t, hs, d); got != actionText {
		t.Fatalf("timeout changed text: %q", got)
	}
	if time.Since(start) > formatOnSaveLimit+time.Second {
		t.Fatal("save exceeded its deadline")
	}
	if !told(hs, "did not finish before the save deadline") {
		t.Fatalf("timeout not shown: %q", hs.tt.Texts())
	}
	if strings.Contains(d.buf.String(), "good") {
		t.Fatal("applied a late result")
	}
}

func TestSaveCodeActionsWithoutDiagnostics(t *testing.T) {
	hs, d := sourceActionHarness(t, "noDiagnostics")
	hs.a.settings.FixAllOnSave = map[string]bool{"fake": true}
	fails := hs.a.fails
	if got := saveAndRead(t, hs, d); got != actionText || hs.a.fails != fails {
		t.Fatalf("save without diagnostics: %q, failures %d", got, hs.a.fails-fails)
	}
}
