package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
	"mayoi/locales"
)

// untitledApp is a window of a folder with settings kept in a file, with
// the tabs of cols.
func untitledApp(t *testing.T, s *settings.Settings, root string, h *fakeHost, cols []settings.Column) (*app, *ui.Tester) {
	w := s.NewWindow()
	w.Folder = root
	w.Columns = cols
	a := newApp(h, s, w)
	t.Cleanup(a.shutdown)
	tt := ui.NewTester(a.view, 1200, 760)
	step(tt)
	step(tt)
	return a, tt
}

// Quitting keeps the text of the untitled documents rather than asking
// where to save them, and the next start opens them again, changed, with
// their names; an empty one is not kept.
func TestQuitKeepsUntitledDocuments(t *testing.T) {
	root := workspaceFixture(t)
	s := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	s.AutoSave = false
	h := &fakeHost{folder: root, choice: 2}
	a, tt := untitledApp(t, s, root, h, nil)
	a.newFile()
	step(tt)
	ed := a.activeEditor()
	ed.edit(ed.sel, textEdit{0, 0, "123123"})
	a.newFile()
	step(tt)
	ed = a.activeEditor()
	ed.edit(ed.sel, textEdit{0, 0, "x"})
	ed.edit(ed.sel, textEdit{0, 1, ""})
	step(tt)

	a.keepUntitled()
	if a.anyUnkept() {
		t.Fatal("quitting still loses the untitled documents")
	}
	closed := false
	a.closeQuitting(func() { closed = true }, nil)
	step(tt)
	if !closed || len(h.asked) != 0 {
		t.Fatalf("closed %v, asked %v", closed, h.asked)
	}
	a.keepSession()
	cols := a.window.Columns
	files, _ := os.ReadDir(a.untitledDir())
	if len(files) != 1 {
		t.Fatalf("kept %d files", len(files))
	}

	b, _ := untitledApp(t, s, root, &fakeHost{folder: root}, cols)
	if len(b.docs) != 1 {
		t.Fatalf("tabs %v", allTabNames(b))
	}
	d := b.docs[0]
	if d.buf.String() != "123123" || !d.dirty() || d.name() != locales.UntitledName(1) {
		t.Errorf("restored %q, dirty %v, named %q", d.buf.String(), d.dirty(), d.name())
	}
	if files, _ := os.ReadDir(b.untitledDir()); len(files) != 0 {
		t.Errorf("%d files kept after the start", len(files))
	}
	b.newFile()
	if got := b.activeEditor().name(); got != locales.UntitledName(2) {
		t.Errorf("a new document is %q", got)
	}
}

// An untitled document saved to a file, or its changes discarded, no
// longer has its text kept.
func TestSavedUntitledDropsKeptText(t *testing.T) {
	root := workspaceFixture(t)
	s := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	h := &fakeHost{folder: root, saveTo: filepath.Join(root, "saved.txt")}
	a, tt := untitledApp(t, s, root, h, nil)
	a.newFile()
	step(tt)
	ed := a.activeEditor()
	ed.edit(ed.sel, textEdit{0, 0, "keep me"})
	step(tt)
	a.keepUntitled()
	kept := filepath.Join(a.untitledDir(), ed.kept)
	if _, err := os.Stat(kept); err != nil {
		t.Fatal(err)
	}
	a.saveDoc(ed.document, nil)
	if _, err := os.Stat(kept); !os.IsNotExist(err) {
		t.Errorf("the kept text stays: %v", err)
	}
	if data, _ := os.ReadFile(h.saveTo); string(data) != "keep me" {
		t.Errorf("saved %q", data)
	}

	a.newFile()
	step(tt)
	ed = a.activeEditor()
	ed.edit(ed.sel, textEdit{0, 0, "drop me"})
	step(tt)
	a.keepUntitled()
	kept = filepath.Join(a.untitledDir(), ed.kept)
	h.choice = 1 // don't save
	a.closeTab(a.activeTab(), nil, nil)
	if _, err := os.Stat(kept); !os.IsNotExist(err) {
		t.Errorf("the discarded text stays: %v", err)
	}
}

// Closing a window rather than quitting asks about an untitled document
// still, even one whose text a quit that was cancelled kept.
func TestClosingWindowAsksAboutUntitled(t *testing.T) {
	root := workspaceFixture(t)
	s := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	h := &fakeHost{folder: root, choice: 2}
	a, tt := untitledApp(t, s, root, h, nil)
	a.newFile()
	step(tt)
	ed := a.activeEditor()
	ed.edit(ed.sel, textEdit{0, 0, "text"})
	step(tt)
	a.keepUntitled()
	a.closeKeeping(func() {}, nil)
	step(tt)
	if len(h.asked) != 1 {
		t.Errorf("asked %v", h.asked)
	}
}

// Closing an untitled document whose Save As is put off keeps it open,
// and says so.
func TestCloseTabSaveAsPutOff(t *testing.T) {
	root := workspaceFixture(t)
	s := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	h := &fakeHost{folder: root, choice: 0}
	a, tt := untitledApp(t, s, root, h, nil)
	a.newFile()
	step(tt)
	ed := a.activeEditor()
	ed.edit(ed.sel, textEdit{0, 0, "unsaved"})
	step(tt)
	tab := a.activeTab()
	closed, cancelled := false, false
	a.closeTab(tab, func() { closed = true }, func() { cancelled = true })
	if closed || !cancelled {
		t.Errorf("closed %v, cancelled %v", closed, cancelled)
	}
	if col, _ := a.locate(tab); col == nil {
		t.Error("the tab closed")
	}
}
