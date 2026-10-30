package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
	"mayoi/locales"
)

// collectContext follows the empty-comment path without a pointer.
func collectContext(a *app, d *editor) {
	if !a.settings.EditorAgentContext || d.sel[0] == d.sel[1] {
		return
	}
	a.commentEditor(d)
	a.saveDraft()
}

func TestAgentContextSelectionActions(t *testing.T) {
	hs, d := editHarness(t, "context.go", "package main\nfirst()\nsecond()\nthird()\n")
	a, tt := hs.a, hs.tt
	snap := d.buf.Snapshot()
	start, _ := snap.LineRange(1)
	end, _ := snap.LineRange(3)
	hs.place(d, end, start)
	if _, ok := tt.Find(locales.AgentContextComment()); ok {
		t.Fatal("gutter action enabled by default")
	}
	a.settings.EditorAgentContext = true
	hs.settle()
	if err := tt.Click(locales.AgentContextComment()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	n := a.review.draft
	if n == nil || !n.context || n.from != 1 || n.to != 3 {
		t.Fatalf("draft %+v", n)
	}
	if d.reviewSource == nil {
		t.Fatal("no inline source layout")
	}
	if _, ok := tt.Find(locales.ReviewCommentLabel()); !ok {
		t.Fatal("no review card")
	}
	before := d.buf.String()
	hs.shot("agent-context-inline")
	if err := tt.Click(locales.AgentContextAdd()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if len(a.review.notes) != 1 || a.review.notes[0] != n || n.text != "" {
		t.Fatal("snippet not collected")
	}
	if d.reviewSource != nil || d.buf.String() != before || d.sel != [2]int{end, start} {
		t.Fatal("source or selection changed after collecting")
	}
	// Selecting the same lines edits the existing entry instead of adding a duplicate.
	a.commentEditor(d)
	hs.settle()
	if a.review.draft != n {
		t.Fatal("duplicate draft")
	}
	tt.Type("Explain these calls")
	hs.key(ui.Cmd, ui.KeyEnter)
	if n.text != "Explain these calls" || len(a.review.notes) != 1 {
		t.Fatalf("comment %q", n.text)
	}
	// Changes above the snippet move its anchor without changing its snapshot.
	d.buf.Replace(0, 0, "// inserted\n")
	hs.settle()
	snap = d.buf.Snapshot()
	start, _ = snap.LineRange(2)
	end, _ = snap.LineRange(4)
	hs.place(d, start, end)
	a.commentEditor(d)
	if a.review.draft != n || n.from != 2 || n.outdated {
		t.Fatalf("relocation %+v", n)
	}
	a.review.text = ""
	a.saveDraft()
	hs.settle()
	if n.text != "" || len(a.review.notes) != 1 {
		t.Fatal("clearing a comment lost its snippet")
	}
	a.settings.EditorAgentContext = false
	hs.settle()
	if _, ok := tt.Find(locales.AgentContextComment()); ok {
		t.Fatal("gutter action remained after disabling")
	}
	if _, ok := tt.Find(locales.AgentContextList()); ok {
		t.Fatal("editor list remained after disabling")
	}
	if len(a.review.notes) != 1 || len(a.window.Review) != 1 {
		t.Fatal("disabling discarded pending context")
	}
}

func TestAgentContextAcrossFiles(t *testing.T) {
	hs, d := editHarness(t, "one.go", "first()\nsecond()\n")
	a, tt := hs.a, hs.tt
	a.settings.EditorAgentContext = true
	hs.place(d, 0, d.buf.Snapshot().Len())
	collectContext(a, d)
	first := a.review.notes[0]
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	d = a.activeEditor()
	hs.place(d, 0, 7)
	collectContext(a, d)
	if len(a.review.notes) != 2 {
		t.Fatalf("items %d", len(a.review.notes))
	}
	// An unsent ordinary review comment shares the same batch.
	a.review.notes = append(a.review.notes, &reviewNote{
		path: hs.path("one.go"), staged: true, from: 1, to: 2, diff: true,
		lines: []string{"second()"}, snippet: []string{"+second()"}, text: "Check the change",
	})
	a.reviewChanged()
	message := a.reviewText()
	for _, part := range []string{"1. one.go:1-2\n```go\nfirst()", "2. README.md:1\n```md\n# Mayoi", "3. one.go:2 (staged)\n```diff\n+second()", "Check the change"} {
		if !strings.Contains(message, part) {
			t.Errorf("missing %q in %q", part, message)
		}
	}
	a.sendReview()
	hs.settle()
	if tt.Clipboard() != message || len(a.review.notes) != 3 {
		t.Fatal("no-terminal fallback lost or changed the batch")
	}
	// The list offers operations for each entry, including removing one file
	// without losing the other snippets or comments.
	if err := tt.Click(locales.AgentContextList()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	label := first.where() + " — " + noteHint(first)
	if err := tt.ChooseMenuItem(label, locales.AgentContextRemove()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if len(a.review.notes) != 2 || slices.Contains(a.review.notes, first) {
		t.Fatal("removed the wrong item")
	}
	if err := tt.Click(locales.ButtonUndo()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if len(a.review.notes) != 3 || a.review.notes[0] != first {
		t.Fatal("undo did not restore the batch order")
	}
	if err := tt.Click(locales.AgentContextList()); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(label, locales.ReviewEditComment()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.review.draft != first || a.review.ed == nil || a.review.ed.path != first.path {
		t.Fatal("list did not open the correct file and comment")
	}
}

func TestAgentContextRestoreAndEdits(t *testing.T) {
	hs, d := editHarness(t, "context.go", "first()\nsecond()\n")
	a := hs.a
	a.settings.EditorAgentContext = true
	hs.place(d, 0, 7)
	collectContext(a, d)
	kept := a.keptReview()
	if len(kept) != 1 || !kept[0].Context || kept[0].Text != "" {
		t.Fatalf("kept %+v", kept)
	}
	// Restoring with the feature off still preserves collected code. Invalid
	// empty legacy comments and malformed context entries are not resurrected.
	a.settings.EditorAgentContext = false
	invalid := kept[0]
	invalid.Context = false
	badSide := kept[0]
	badSide.Staged = true
	a.restoreReview(append(kept, invalid, badSide, settings.ReviewNote{Context: true}))
	if len(a.review.notes) != 1 || !a.review.notes[0].context {
		t.Fatalf("restored %+v", a.review.notes)
	}
	d.buf.Replace(0, 0, "new()\n")
	message := a.reviewText()
	if !strings.HasPrefix(message, "context.go:2\n") {
		t.Fatalf("did not follow unsaved edits: %s", message)
	}
	d.buf.Replace(6, 13, "changed()")
	message = a.reviewText()
	if !a.review.notes[0].outdated || !strings.Contains(message, "outdated:") || !strings.Contains(message, "first()") {
		t.Fatalf("changed snippet lost its original context: %s", message)
	}
}

func TestAgentContextUnnamedAndNoSelection(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.settings.EditorAgentContext = true
	a.newFile()
	hs.settle()
	d := a.activeEditor()
	d.buf.Replace(0, 0, "hello")
	hs.place(d, 0, 5)
	collectContext(a, d)
	if len(a.review.notes) != 0 || a.notes[len(a.notes)-1].message != locales.ReviewSaveFirstItems() {
		t.Fatal("an unnamed file was collected without a path")
	}
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	d = a.activeEditor()
	hs.place(d, 0, 0)
	collectContext(a, d)
	if len(a.review.notes) != 0 {
		t.Fatal("collected an empty selection")
	}
}

func TestAgentContextToolbarBounds(t *testing.T) {
	for _, wrap := range []bool{false, true} {
		t.Run(fmt.Sprint(wrap), func(t *testing.T) {
			hs, d := editHarness(t, "context.txt", strings.Repeat("long text ", 40)+strings.Repeat("\nnext line", 80))
			a, tt := hs.a, hs.tt
			a.settings.EditorAgentContext, a.settings.WordWrap = true, wrap
			hs.place(d, 0, d.buf.Snapshot().Len())
			r, ok := tt.Find(locales.AgentContextComment())
			if !ok || r.W <= 0 || r.X < 0 || r.Y < 0 || r.X+r.W > 1200 || r.Y+r.H > 760 {
				t.Fatalf("toolbar out of bounds: %v, %v", r, ok)
			}
			hs.shot(fmt.Sprintf("agent-context-wrap-%v", wrap))
			if err := tt.Click(locales.AgentContextComment()); err != nil {
				t.Fatal(err)
			}
			hs.settle()
			a.saveDraft()
			hs.settle()
			if len(a.review.notes) != 1 || a.review.notes[0].to != 81 {
				t.Fatal("gutter action did not collect the full selection")
			}
		})
	}
}

func TestAgentContextPointerSelection(t *testing.T) {
	hs, d := editHarness(t, "context.txt", "first line\nsecond line\nthird line\n")
	a, tt := hs.a, hs.tt
	a.settings.EditorAgentContext = true
	tt.SetSize(650, 550)
	hs.settle()
	r, ok := tt.Find(locales.EditorLabel(d.name()))
	if !ok {
		t.Fatal("no editor")
	}
	x, y := r.X+d.gutterW+gutterGap+2, r.Y+editorPadY+a.font.lineH/2
	tt.Press(x, y)
	tt.Move(x+5*a.font.charW, y+a.font.lineH)
	hs.settle()
	if !d.selecting {
		t.Fatal("selection drag was not tracked")
	}
	if _, ok := tt.Find(locales.AgentContextComment()); ok {
		t.Fatal("toolbar obstructs an ongoing drag")
	}
	tt.Release(x+5*a.font.charW, y+a.font.lineH)
	hs.settle()
	if d.selecting {
		t.Fatal("drag did not end")
	}
	button, ok := tt.Find(locales.AgentContextComment())
	if !ok || button.X < r.X || button.X+button.W > 650 || button.Y+button.H > 550 {
		t.Fatalf("toolbar not visible inside the narrow editor: %+v", button)
	}
	hs.shot("agent-context-narrow")
	if err := tt.Click(locales.AgentContextComment()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	a.saveDraft()
	hs.settle()
	if len(a.review.notes) != 1 || a.review.notes[0].from != 0 || a.review.notes[0].to != 2 {
		t.Fatalf("dragged lines not collected: %+v", a.review.notes)
	}
}

func TestAgentContextToTerminal(t *testing.T) {
	featShell(t)
	hs, d := editHarness(t, "context.go", "first()\nsecond()\n")
	a := hs.a
	a.settings.EditorAgentContext = true
	hs.place(d, 0, 7)
	collectContext(a, d)
	a.newTerminal()
	t.Cleanup(a.closeTerminals)
	hs.settle()
	s := a.panelTerm
	hs.until("the prompt", func() bool { return strings.Contains(s.term.Text(), "❯") })
	// The toolbar includes text still being drafted, even with a hidden terminal.
	a.commentEditor(d)
	hs.settle()
	a.review.text = "include this unsaved comment"
	// The toolbar uses the shared send path even when the terminal is hidden.
	a.focusColumn()
	hs.settle()
	if err := hs.tt.Click(locales.ReviewSendTipItems(1)); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	hs.until("context pasted", func() bool { return strings.Contains(s.term.Text(), "context.go:1") })
	if len(a.review.notes) != 0 || len(a.window.Review) != 0 || a.focus != paneTerminal {
		t.Fatal("successful paste did not clear the batch and focus the terminal")
	}
	if text := s.term.Text(); !strings.Contains(text, "first()") || !strings.Contains(text, "include this unsaved comment") || strings.Count(text, "❯") != 1 {
		t.Fatalf("context missing or submitted automatically: %q", text)
	}
}

func TestAgentContextSingleLineAndCancel(t *testing.T) {
	for _, wrap := range []bool{false, true} {
		t.Run(fmt.Sprint(wrap), func(t *testing.T) {
			hs, d := editHarness(t, "context.go", "first()\nsecond()\nthird()\n")
			a, tt := hs.a, hs.tt
			a.settings.EditorAgentContext, a.settings.WordWrap = true, wrap
			hs.settle()
			area, ok := tt.Find(locales.EditorLabel(d.name()))
			if !ok {
				t.Fatal("no source editor")
			}
			tt.Move(area.X+d.gutterW+gutterGap+20, area.Y+editorPadY+2.5*a.font.lineH)
			hs.settle()
			if err := tt.Click(locales.AgentContextComment()); err != nil {
				t.Fatal(err)
			}
			hs.settle()
			n := a.review.draft
			if n == nil || n.from != 2 || n.to != 3 {
				t.Fatalf("hovered line draft: %+v", n)
			}
			before := d.buf.String()
			card, ok := tt.Find(locales.ReviewCommentLabel())
			if !ok || card.H <= a.font.lineH || card.X < area.X || card.X+card.W > area.X+area.W {
				t.Fatalf("inline card bounds: %+v", card)
			}
			tt.Type("A draft that will be cancelled")
			hs.key(0, ui.KeyEscape)
			if a.review.draft != nil || len(a.review.notes) != 0 || d.reviewSource != nil || d.buf.String() != before {
				t.Fatal("cancelling changed the source or retained the draft")
			}
			// The same editor and document resume at the selected line, with
			// their normal editing and undo, not at an offset including the card.
			tt.Type("// ")
			hs.settle()
			if d.buf.String() != "first()\nsecond()\n// third()\n" {
				t.Fatalf("resumed at wrong offset: %q", d.buf.String())
			}
			hs.command("undo")
			if d.buf.String() != before {
				t.Fatal("comment layout disturbed source undo")
			}
			// Hot-disabling the preference while drafting restores the editor
			// without losing the user's unsaved comment.
			a.commentEditor(d)
			hs.settle()
			a.review.text = "keep this draft"
			a.settings.EditorAgentContext = false
			hs.settle()
			if d.reviewSource != nil || a.review.draft == nil || a.review.text != "keep this draft" {
				t.Fatal("disabling lost the draft or retained the inline layout")
			}
		})
	}
}

func TestAgentContextHorizontalScroll(t *testing.T) {
	for _, wrap := range []bool{false, true} {
		t.Run(fmt.Sprint(wrap), func(t *testing.T) {
			hs, d := editHarness(t, "context.txt", strings.Repeat("long code ", 80)+"\nnext line\n")
			a, tt := hs.a, hs.tt
			a.settings.EditorAgentContext, a.settings.WordWrap = true, wrap
			hs.place(d, 0, 9)
			area, ok := tt.Find(locales.EditorLabel(d.name()))
			if !ok {
				t.Fatal("no source editor")
			}
			x, y := area.X+area.W/2, area.Y+editorPadY+a.font.lineH/2
			tt.Scroll(x, y, 120, 0)
			hs.settle()
			initialX := d.hscroll.X
			if !wrap && initialX <= 0 {
				t.Fatal("source did not scroll horizontally")
			}
			a.commentEditor(d)
			hs.settle()
			v := d.reviewSource
			if v == nil {
				t.Fatal("no inline source layout")
			}
			if v.scroll.X != initialX {
				t.Fatalf("draft lost source horizontal offset: %v -> %v", initialX, v.scroll.X)
			}
			card, ok := tt.Find(locales.ReviewCommentLabel())
			if !ok {
				t.Fatal("no review card")
			}
			gutter := func() string {
				img := tt.Image()
				var pixels []byte
				for py := int(area.Y); py < int(area.Y+editorPadY+a.font.lineH); py++ {
					for px := int(area.X); px < int(area.X+d.gutterW); px++ {
						p := img.RGBAAt(px, py)
						pixels = append(pixels, p.R, p.G, p.B)
					}
				}
				return string(pixels)
			}
			beforeGutter := gutter()
			tt.Scroll(x, y, 120, 0)
			hs.settle()
			if wrap {
				if v.scroll.X != 0 || v.scroll.MaxX != 0 {
					t.Fatalf("wrapped source scrolls sideways: %+v", v.scroll)
				}
			} else if v.scroll.MaxX <= 0 || v.scroll.X <= initialX {
				t.Fatalf("draft source cannot scroll sideways: %+v", v.scroll)
			}
			if gutter() != beforeGutter {
				t.Fatal("horizontal scrolling moved the gutter")
			}
			after, ok := tt.Find(locales.ReviewCommentLabel())
			if !ok || after.X != card.X || after.W != card.W {
				t.Fatalf("horizontal scrolling moved the card: %+v -> %+v", card, after)
			}
			hs.key(0, ui.KeyEscape)
			if d.hscroll.X != initialX {
				t.Fatalf("returning to source lost its horizontal offset: %v -> %v", initialX, d.hscroll.X)
			}
		})
	}
}

// Releasing an editor is shared by tabs, columns, diff editors and merge editors.
func TestAgentContextReleaseDraft(t *testing.T) {
	for _, mode := range []string{"tab", "column", "release", "empty legacy"} {
		t.Run(mode, func(t *testing.T) {
			hs, d := editHarness(t, "context.go", "first()\nsecond()\n")
			a := hs.a
			a.settings.EditorAgentContext = mode != "empty legacy"
			if mode == "column" {
				a.splitRight()
				hs.settle()
				d = a.activeEditor()
				if len(d.document.views) != 2 {
					t.Fatal("no shared document")
				}
			}
			hs.place(d, 0, 7)
			a.commentEditor(d)
			hs.settle()
			if mode != "empty legacy" {
				hs.tt.Type("keep my draft")
				hs.settle()
			}
			n := a.review.draft
			switch mode {
			case "release", "empty legacy":
				d.release()
			default:
				a.closeActive()
			}
			hs.settle()
			if a.review.draft != nil || a.review.ed != nil || d.releaseReview != nil || d.reviewSource != nil {
				t.Fatal("released editor retained its draft")
			}
			if mode == "empty legacy" {
				if len(a.review.notes) != 0 {
					t.Fatal("saved an empty ordinary comment")
				}
				return
			}
			if len(a.review.notes) != 1 || a.review.notes[0] != n || n.text != "keep my draft" || a.window.Review[0].Text != n.text {
				t.Fatal("draft was not kept in the persistent queue")
			}
			if mode == "column" && len(a.cols) != 1 {
				t.Fatal("column was not closed")
			}
			if mode == "release" {
				a.closeActive()
				hs.settle()
			}
			a.openFile(hs.path("README.md"), true)
			hs.settle()
			hs.place(a.activeEditor(), 0, 7)
			if _, ok := hs.tt.Find(locales.AgentContextComment()); !ok {
				t.Fatal("draft hid another file's action")
			}
			a.goToNote(n)
			hs.settle()
			a.editNote(n, a.activeEditor())
			hs.settle()
			if a.review.text != "keep my draft" || a.review.ed == d || a.review.ed.path != d.path {
				t.Fatal("could not recover the draft in another editor")
			}
		})
	}
}

func TestAgentContextDraftAcrossTabs(t *testing.T) {
	hs, d := editHarness(t, "context.go", "first()\nsecond()\n")
	a := hs.a
	a.settings.EditorAgentContext = true
	a.commentEditor(d)
	hs.settle()
	hs.tt.Type("keep the first")
	hs.settle()
	n := a.review.draft
	original := a.activeTab()
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	other := a.activeTab()
	a.closeTab(other, nil)
	hs.settle()
	if a.review.draft != n || a.review.ed != d || a.review.text != "keep the first" {
		t.Fatal("closing another tab changed the draft")
	}
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	hs.place(a.activeEditor(), 0, 7)
	if err := hs.tt.Click(locales.AgentContextComment()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if len(a.review.notes) != 1 || n.text != "keep the first" || a.review.ed == d {
		t.Fatal("switching drafts lost the previous comment")
	}
	hs.tt.Type("keep the second")
	hs.settle()
	a.closeTab(original, nil)
	hs.settle()
	if a.review.text != "keep the second" {
		t.Fatal("old editor's release ended the new draft")
	}
}

func TestAgentContextDraftSourceFocus(t *testing.T) {
	for _, key := range []ui.Key{ui.KeyEscape, ui.KeyEnter} {
		t.Run(fmt.Sprint(key), func(t *testing.T) {
			hs, d := editHarness(t, "context.go", "first()\nsecond()\n")
			a, tt := hs.a, hs.tt
			a.settings.EditorAgentContext = true
			area, _ := tt.Find(locales.EditorLabel(d.name()))
			a.commentEditor(d)
			hs.settle()
			tt.Type("before")
			hs.settle()
			tt.ClickAt(area.X+d.gutterW+40, area.Y+editorPadY+a.font.lineH/2)
			hs.settle()
			tt.Type(" after")
			hs.settle()
			if a.review.text != "before after" || d.buf.String() != "first()\nsecond()\n" {
				t.Fatal("source click lost comment typing focus")
			}
			mods := ui.Modifiers(0)
			if key == ui.KeyEnter {
				mods = ui.Cmd
			}
			hs.key(mods, key)
			if a.review.draft != nil || d.reviewSource != nil {
				t.Fatal("source click broke the draft shortcut")
			}
			if key == ui.KeyEnter && (len(a.review.notes) != 1 || a.review.notes[0].text != "before after") {
				t.Fatal("save shortcut lost text")
			}
		})
	}
}

func TestAgentContextGrowingCard(t *testing.T) {
	hs, d := editHarness(t, "context.go", strings.Repeat("next()\n", 100))
	a, tt := hs.a, hs.tt
	a.settings.EditorAgentContext = true
	tt.SetSize(1000, 760)
	hs.settle()
	area, _ := tt.Find(locales.EditorLabel(d.name()))
	line := int((area.H - 150) / a.font.lineH)
	at, _ := d.buf.Snapshot().LineRange(line)
	hs.place(d, at, at)
	a.commentEditor(d)
	hs.settle()
	tt.Type(strings.Repeat("more detail\n", 14))
	hs.settle()
	check := func(bottom float32) {
		t.Helper()
		for _, label := range []string{locales.ButtonCancel(), locales.ReviewComment()} {
			r, ok := tt.Find(label)
			if !ok || r.Y < area.Y || r.Y+r.H > bottom {
				t.Fatalf("%s out of view: %+v; bottom %v", label, r, bottom)
			}
		}
	}
	check(area.Y + area.H)
	tt.SetSize(1000, 500)
	hs.settle()
	check(500 - statusBarH)
	if d.reviewSource.width > maxNoteBoxW {
		t.Fatal("card exceeded its maximum width")
	}
	hs.shot("agent-context-growing-card")
}

func TestAgentContextJumpKeepsDraft(t *testing.T) {
	hs, d := editHarness(t, "context.go", strings.Repeat("line()\n", 100))
	a := hs.a
	a.settings.EditorAgentContext = true
	a.commentEditor(d)
	hs.settle()
	hs.tt.Type("keep before jumping")
	hs.settle()
	a.openFileAt(d.path, 70, true)
	hs.settle()
	if a.review.draft != nil || caretLine(d) != 70 || len(a.review.notes) != 1 || a.review.notes[0].text != "keep before jumping" {
		t.Fatal("jump was delayed or lost the draft")
	}
}

func TestAgentContextSendSavesDraft(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			hs, d := editHarness(t, "context.go", "first()\nsecond()\n")
			a := hs.a
			a.settings.EditorAgentContext = true
			if existing {
				hs.place(d, 0, 7)
				collectContext(a, d)
			}
			hs.place(d, 8, 16)
			a.commentEditor(d)
			hs.settle()
			hs.tt.Type("unsaved comment")
			hs.settle()
			a.run("review.send")
			hs.settle()
			if a.review.draft != nil || !strings.Contains(hs.tt.Clipboard(), "unsaved comment") || !strings.Contains(a.reviewText(), "unsaved comment") {
				t.Fatal("command send lost its draft or fallback queue")
			}
			hs.tt.Type("resume source")
			hs.settle()
			if !strings.Contains(d.buf.String(), "resume source") {
				t.Fatal("clipboard fallback did not return focus to the source")
			}
		})
	}
}

func TestAgentContextUnwrappedResize(t *testing.T) {
	hs, d := editHarness(t, "context.go", strings.Repeat("line()\n", 100000))
	a := hs.a
	a.settings.EditorAgentContext, a.settings.WordWrap = true, false
	a.commentEditor(d)
	hs.settle()
	v := d.reviewSource
	tops := &v.tops[0]
	if v.file.new.lines[90000] != "" {
		t.Fatal("opening a card copied offscreen source")
	}
	hs.tt.SetSize(900, 600)
	hs.settle()
	if tops != &v.tops[0] {
		t.Fatal("unwrapped resize rebuilt all line positions")
	}
}

func TestAgentContextDisabledReviewUI(t *testing.T) {
	hs, d := editHarness(t, "context.go", "first()\nsecond()\n")
	a, tt := hs.a, hs.tt
	a.commentEditor(d)
	hs.settle()
	tt.Type("ordinary review")
	hs.key(ui.Cmd, ui.KeyEnter)
	n := a.review.notes[0]
	for _, cmd := range a.commands() {
		if cmd.id == "review.send" || cmd.id == "review.copy" || cmd.id == "review.clear" {
			if !strings.Contains(cmd.label, "Comments") {
				t.Fatalf("default-off command changed: %s", cmd.label)
			}
		}
	}
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	if err := tt.Click(locales.ReviewStatusTip(1)); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(n.where() + " — " + noteHint(n)); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.activeEditor() != d {
		t.Fatal("default-off review note requires a submenu")
	}
	a.settings.EditorAgentContext = true
	a.editNote(n, d)
	hs.settle()
	a.review.text = "keep while disabling"
	a.settings.EditorAgentContext = false
	hs.settle()
	if a.review.text != "keep while disabling" || d.reviewSource != nil {
		t.Fatal("disabling lost the draft")
	}
	if err := tt.Click(locales.ReviewCommentLabel()); err != nil {
		t.Fatal(err)
	}
	tt.Type(" and save")
	hs.key(ui.Cmd, ui.KeyEnter)
	if n.text != "keep while disabling and save" || a.review.draft != nil {
		t.Fatalf("disabled draft was inaccessible: %q", n.text)
	}
}

func TestAgentContextSharedEditorDraft(t *testing.T) {
	hs, left, right := splitHarness(t, func(hs *harness) string { return hs.path("main.go") })
	a := hs.a
	a.settings.EditorAgentContext = true
	hs.place(right, 0, 7)
	a.commentEditor(right)
	hs.settle()
	hs.tt.Type("keep across editors")
	hs.settle()
	n := a.review.draft
	a.col = 0
	a.focusColumn()
	hs.place(left, 0, 7)
	a.commentEditor(left)
	hs.settle()
	if a.review.draft != n || a.review.ed != left || a.review.text != "keep across editors" || len(a.review.notes) != 1 {
		t.Fatal("same-document draft was duplicated or lost")
	}
	hidden := a.cols[1].tab()
	a.closeTab(hidden, nil)
	hs.settle()
	if a.review.draft != n || a.review.ed != left {
		t.Fatal("closing the previous editor cleared the current draft")
	}
}

func TestAgentContextSourceReclaimsFocus(t *testing.T) {
	hs, left, _ := splitHarness(t, func(hs *harness) string { return hs.path("main.go") })
	a, tt := hs.a, hs.tt
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	right := a.activeEditor()
	a.settings.EditorAgentContext = true
	a.col = 0
	a.focusColumn()
	hs.settle()
	leftArea, _ := tt.Find(locales.EditorLabel(left.name()))
	a.commentEditor(left)
	hs.settle()
	leftText, rightText := left.buf.String(), right.buf.String()
	rightArea, _ := tt.Find(locales.EditorLabel(right.name()))
	tt.ClickAt(rightArea.X+right.gutterW+50, rightArea.Y+editorPadY+a.font.lineH/2)
	hs.settle()
	tt.ClickAt(leftArea.X+left.gutterW+50, leftArea.Y+editorPadY+a.font.lineH/2)
	hs.settle()
	tt.Type("back in the comment")
	hs.settle()
	if a.review.text != "back in the comment" || left.buf.String() != leftText || right.buf.String() != rightText {
		t.Fatal("clicking draft source did not reclaim typing from the other column")
	}
	hs.key(0, ui.KeyEscape)
	if a.review.draft != nil || a.activeEditor() != left {
		t.Fatal("Escape did not return to the draft's editor")
	}
}
