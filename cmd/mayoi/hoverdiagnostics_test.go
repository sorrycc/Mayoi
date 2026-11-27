package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"mayoi/locales"
)

func TestHoverDiagnosticsWithDocumentation(t *testing.T) {
	hs := withFakeServer(t)
	d := openHover(hs, "diagnostic.fake", "bad\n")
	hs.until("the diagnostic", func() bool { return len(plugins.diagnosticsOf(d.path)) == 1 })
	h := hoverAt(hs, d, 1)
	if !hs.tt.HasText("bad word") || !strings.Contains(h.tree.Text, "**hello**") {
		t.Fatalf("diagnostics and documentation: %q, %q", hs.tt.Texts(), h.tree.Text)
	}
	plugins.setDiagnosticList("test", d.path, "lint", false, []diagnostic{{
		Range: textRange{position{0, 0}, position{0, 3}}, Severity: "warning", Message: "another problem", Code: "S1000",
	}})
	hs.settle()
	if !hs.tt.HasText("another problem") || !hs.tt.HasText("lint (S1000)") || !hs.tt.HasText("bad word") {
		t.Fatalf("overlapping diagnostics: %q", hs.tt.Texts())
	}
	plugins.setDiagnosticList("test", d.path, "lint", false, nil)
	hs.settle()
	if hs.tt.HasText("another problem") || !hs.tt.HasText("bad word") {
		t.Fatalf("cleared diagnostic: %q", hs.tt.Texts())
	}
}

func TestHoverDiagnosticsWithoutProvider(t *testing.T) {
	hs := newHarness(t, false)
	h := withPlugins(hs, map[string]map[string]string{"lint": {"plugin.json": `{"name":"lint"}`}})
	path := hs.path("diagnostic.txt")
	if err := os.WriteFile(path, []byte("call(a, b)\nnext line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hs.a.openFile(path, true)
	hs.settle()
	d := hs.a.activeEditor()
	h.setDiagnosticList("lint", path, "lint", false, []diagnostic{{
		Range: textRange{position{0, 4}, position{1, 4}}, Severity: "warning", Message: "Use **another** call.", Code: "S1000",
	}})
	hs.settle()
	before := d.sel
	// Punctuation and the continuation of a multiline diagnostic both work.
	for _, at := range []int{4, 7, 12} {
		hs.a.hushRest()
		restOn(t, hs, d, at)
		hs.until("the diagnostic hover", func() bool { return hs.a.hover != nil })
		if !hs.tt.HasText("Use **another** call.") || !hs.tt.HasText("lint (S1000)") || d.sel != before {
			t.Fatalf("at %d: caret %v, text %q", at, d.sel, hs.tt.Texts())
		}
	}
	card, version := hs.a.hover, d.buf.Version()
	if err := hs.tt.Click(locales.CopyDiagnostic()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := hs.tt.Clipboard(); got != "Use **another** call.\nlint (S1000)" {
		t.Fatalf("copied diagnostic: %q", got)
	}
	if hs.a.hover != card || d.sel != before || d.buf.Version() != version || !told(hs, locales.CopiedDiagnostic()) {
		t.Fatal("copying moved the caret, changed the text, closed the hover or showed no feedback")
	}
	restOn(t, hs, d, 12)
	h.setDiagnosticList("lint", path, "lint", false, []diagnostic{{
		Range: textRange{position{0, 4}, position{1, 4}}, Severity: "info", Message: "Updated advice",
	}})
	hs.settle()
	if !hs.tt.HasText("Updated advice") || hs.tt.HasText("Use **another** call.") {
		t.Fatalf("updated diagnostic: %q", hs.tt.Texts())
	}
	if err := hs.tt.Click(locales.CopyDiagnostic()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := hs.tt.Clipboard(); got != "Updated advice\nlint" {
		t.Fatalf("copied stale diagnostic: %q", got)
	}
	h.setDiagnosticList("lint", path, "lint", false, nil)
	hs.settle()
	if hs.a.hover != nil {
		t.Fatal("clearing the only diagnostic left an empty hover")
	}
}

func TestHoverDiagnosticsUTF16Range(t *testing.T) {
	hs := newHarness(t, false)
	h := withPlugins(hs, map[string]map[string]string{"lint": {"plugin.json": `{"name":"lint"}`}})
	hs.a.openFile(hs.path("README.md"), true)
	d := hs.a.activeEditor()
	hs.a.queueEdit(d, func(d *editor) { d.edit([2]int{}, textEdit{0, d.buf.Snapshot().Len(), "😀call()\n"}) })
	hs.settle()
	h.setDiagnosticList("lint", d.path, "lint", false, []diagnostic{{
		Range: textRange{position{0, 2}, position{0, 6}}, Message: "the call",
	}})
	for at := 0; at < 7; at++ {
		want := at >= 1 && at < 5
		if got := len(diagnosticsAt(d, at)) > 0; got != want {
			t.Errorf("rune %d: matched %v, want %v", at, got, want)
		}
	}
}

func TestHoverDiagnosticsWhileDocumentationWaits(t *testing.T) {
	hs := withFakeServer(t)
	d := openLate(hs)
	plugins.setDiagnosticList("lint", d.path, "lint", false, []diagnostic{{
		Range: textRange{position{0, 4}, position{0, 8}}, Severity: "warning", Message: "Local advice",
	}})
	hs.place(d, 5, 5)
	hs.a.showHover()
	holdLate(hs)
	hs.settle()
	if hs.a.hover == nil || !hs.tt.HasText("Local advice") {
		t.Fatal("the diagnostic waited for the server's documentation")
	}
	releaseLate(hs)
	if !hs.tt.HasText("Local advice") || !strings.Contains(hs.a.hover.tree.Text, "**hello**") {
		t.Fatalf("the documentation replaced the diagnostic: %q", hs.tt.Texts())
	}
}

func TestHoverDiagnosticsDoNotResyncUnchangedText(t *testing.T) {
	for _, kind := range []string{"caret", "pointer"} {
		t.Run(kind, func(t *testing.T) {
			hs := withFakeServer(t)
			d := openHover(hs, "diagnostic.fake", "bad\n")
			hs.until("the diagnostic", func() bool { return len(plugins.diagnosticsOf(d.path)) == 1 })
			c := hs.a.lspRunningFor(d.document).live()
			before, err := c.Call(context.Background(), "test/snapshot", nil)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "caret" {
				hoverAt(hs, d, 1)
			} else {
				restOn(t, hs, d, 1)
				hs.until("the hover documentation", func() bool { return hs.a.hover != nil && hs.a.hover.tree.Text != "" })
			}
			after, err := c.Call(context.Background(), "test/snapshot", nil)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatalf("hover resynchronized unchanged text and restarted diagnostics:\nbefore %s\nafter %s", before, after)
			}
			if !hs.tt.HasText("bad word") || !strings.Contains(hs.a.hover.tree.Text, "**hello**") {
				t.Fatal("diagnostics and documentation did not show together")
			}
		})
	}
}

// A card above its line, of a diagnostic whose documentation answers
// within readingShows, shows once with both: its copy button never moves.
func TestHoverDiagnosticsHoldStillAsDocumentationArrives(t *testing.T) {
	hs := withFakeServer(t)
	path := hs.path("a.fake")
	os.WriteFile(path, []byte(strings.Repeat("\n", 20)+"use late one\n"), 0o644)
	hs.a.openFile(path, true)
	d := hs.a.activeEditor()
	serverUp(hs)
	plugins.setDiagnosticList("lint", d.path, "lint", false, []diagnostic{{
		Range: textRange{position{20, 4}, position{20, 8}}, Severity: "warning", Message: "Local advice",
	}})
	hs.settle()
	x, y := restAt(t, hs, d, 25)
	hs.tt.Move(x, y)
	step(hs.tt)
	hs.tt.Advance(hoverDwell)
	step(hs.tt)
	// Held without settling, which would pass readingShows.
	hs.held++
	for deadline := time.Now().Add(settleTimeout); string(fakeCall(hs, "test/held")) != "true"; {
		if time.Now().After(deadline) {
			t.Fatal("the documentation was never asked for")
		}
		time.Sleep(time.Millisecond)
	}
	for range 3 {
		step(hs.tt)
	}
	if _, ok := hs.tt.Find(locales.CopyDiagnostic()); ok || hs.a.hover == nil || !hs.a.hover.waiting {
		t.Fatal("the diagnostic showed before documentation could answer")
	}
	releaseLate(hs)
	first, ok := hs.tt.Find(locales.CopyDiagnostic())
	if !ok || !strings.Contains(hs.a.hover.tree.Text, "**hello**") {
		t.Fatalf("diagnostic and documentation: %q", hs.tt.Texts())
	}
	hs.tt.Advance(readingShows)
	hs.settle()
	if again, _ := hs.tt.Find(locales.CopyDiagnostic()); again != first {
		t.Fatalf("copy button moved: %v, then %v", first, again)
	}
}

// An empty range at a line's end underlines the line's last rune, which
// shows its diagnostic too.
func TestHoverDiagnosticsAtLineEnd(t *testing.T) {
	hs := newHarness(t, false)
	h := withPlugins(hs, map[string]map[string]string{"lint": {"plugin.json": `{"name":"lint"}`}})
	path := hs.path("eol.txt")
	os.WriteFile(path, []byte("call(a\nnext\n"), 0o644)
	hs.a.openFile(path, true)
	hs.settle()
	d := hs.a.activeEditor()
	h.setDiagnosticList("lint", path, "lint", false, []diagnostic{
		{Range: textRange{position{0, 6}, position{0, 6}}, Message: "expected )"},
		{Range: textRange{position{1, 2}, position{1, 0}}, Message: "backwards"},
	})
	hs.settle()
	for at, want := range map[int]string{4: "", 5: "expected )", 6: "", 7: "backwards", 8: "backwards", 9: ""} {
		got := diagnosticsAt(d, at)
		if want == "" && len(got) > 0 || want != "" && (len(got) != 1 || got[0].Message != want) {
			t.Errorf("rune %d: %v, want %q", at, got, want)
		}
	}
	restOn(t, hs, d, 5)
	hs.until("the diagnostic hover", func() bool { return hs.tt.HasText("expected )") && hs.a.hover != nil })
}

// Finding the diagnostics at a rune looks at its line alone, however many
// the document has.
func TestHoverDiagnosticsManyAreCheap(t *testing.T) {
	hs := newHarness(t, false)
	h := withPlugins(hs, map[string]map[string]string{"lint": {"plugin.json": `{"name":"lint"}`}})
	path := hs.path("big.txt")
	var b strings.Builder
	var list []diagnostic
	for i := range 20000 {
		fmt.Fprintf(&b, "a fairly long line of text, number %d, of words\n", i)
		list = append(list, diagnostic{Range: textRange{position{i, 2}, position{i, 8}}, Message: "m"})
	}
	os.WriteFile(path, []byte(b.String()), 0o644)
	hs.a.openFile(path, true)
	hs.settle()
	d := hs.a.activeEditor()
	h.setDiagnosticList("lint", path, "lint", false, list)
	plugins.diagnosticsOf(path)
	start := time.Now()
	for range 10 {
		if len(diagnosticsAt(d, 3)) != 1 {
			t.Fatal("the diagnostic of the first line")
		}
	}
	if per := time.Since(start) / 10; per > 20*time.Millisecond {
		t.Fatalf("diagnosticsAt took %v with 20000 diagnostics", per)
	}
}
