package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/settings"
)

// writeBigLog makes a log file of n lines of about line bytes each,
// returning its path and its lines.
func writeBigLog(t *testing.T, dir string, n, lineBytes int) (string, []string) {
	t.Helper()
	lines := make([]string, n)
	for i := range lines {
		s := "line " + itoa(i) + " "
		s += strings.Repeat("x", max(0, lineBytes-1-len(s)))
		lines[i] = s + "\n"
	}
	path := filepath.Join(dir, "big.log")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, lines
}

// itoa writes n in decimal.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
}

// A local file over the threshold opens in log mode: read in a window,
// with the whole file's lines.
func TestLogModeOpen(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.LargeFileModeMB = 1                  // 1 MB
	path, lines := writeBigLog(t, hs.root, 20000, 100) // 2 MB
	hs.a.openFile(path, true)
	hs.settle()
	d := hs.a.activeEditor()
	if d == nil || d.unread() {
		t.Fatal("the file did not open")
	}
	if d.log == nil {
		t.Fatal("the file did not open in log mode")
	}
	if n := d.viewLines(); n != len(lines)+1 { // a file ending in LF counts its empty last line
		t.Fatalf("lines %d, want %d", n, len(lines)+1)
	}
	// The buffer holds a window, not the file.
	if n := d.buf.ByteLen(); n > logWindowBytes+4096 {
		t.Fatalf("the buffer holds %d bytes of a %d byte file", n, len(lines)*100)
	}
	// The window starts at the first line and shows it.
	if got := logFirstLineOf(d.buf.String()); got != lines[0][:len(lines[0])-1] {
		t.Fatalf("the window starts %q", got)
	}
}

// A file under the threshold opens whole, as before.
func TestLogModeUnderThreshold(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.LargeFileModeMB = 1
	path, _ := writeBigLog(t, hs.root, 1000, 100) // 100 KB
	hs.a.openFile(path, true)
	hs.settle()
	d := hs.a.activeEditor()
	if d.log != nil {
		t.Fatal("a small file opened in log mode")
	}
	if n := d.buf.LineCount(); n != 1001 {
		t.Fatalf("lines %d, want 1001", n)
	}
}

// The setting turns log mode off.
func TestLogModeOff(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.LargeFileModeMB = 0
	path, lines := writeBigLog(t, hs.root, 20000, 100)
	hs.a.openFile(path, true)
	hs.settle()
	d := hs.a.activeEditor()
	if d.log != nil {
		t.Fatal("log mode is off")
	}
	if d.buf.LineCount() != len(lines)+1 {
		t.Fatalf("lines %d, want %d", d.buf.LineCount(), len(lines)+1)
	}
}

// Going to a line far past the window moves the window there and shows
// the line's number.
func TestLogModeGoToLine(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.LargeFileModeMB = 1
	path, lines := writeBigLog(t, hs.root, 20000, 100)
	hs.a.openFile(path, true)
	hs.settle()
	hs.a.goToLine(15000, 1)
	hs.settle()
	d := hs.a.activeEditor()
	if d == nil || d.log == nil {
		t.Fatal("log mode lost")
	}
	if !strings.Contains(d.buf.String(), lines[15000-1][:len(lines[15000-1])-1]) {
		t.Fatalf("line 15000 is not in the window")
	}
	if d.log.win.first > 15000-1 || 15000-1 >= d.log.win.first+d.buf.LineCount() {
		t.Fatalf("window at %d of %d lines", d.log.win.first, d.buf.LineCount())
	}
	// The caret is on the line, of the whole text.
	if at := d.log.lineAt(d.log.win.start + int64(d.sel[1])); at != 14999 {
		t.Fatalf("caret on line %d, want 14999", at)
	}
}

// A line longer than the editor lays out one does not wedge the view on
// a window a line big: the line shows in part, the window holds still,
// and the lines past it are had by a jump.
func TestLogModeLongLine(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.LargeFileModeMB = 1
	var b strings.Builder
	b.WriteString("before the long line\n")
	b.WriteString(strings.Repeat("z", 3<<20)) // a 3 MB line
	b.WriteString("\nafter the long line\n")
	for i := range 300 {
		b.WriteString("tail line ")
		b.WriteString(itoa(i))
		b.WriteString("\n")
	}
	path := filepath.Join(hs.root, "long.log")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	hs.a.openFile(path, true)
	hs.settle()
	d := hs.a.activeEditor()
	if d == nil || d.log == nil {
		t.Fatal("the file did not open in log mode")
	}
	if d.sync != syncIdle {
		t.Fatalf("the read did not settle, sync %d", d.sync)
	}
	// The line shows in part, at maxLineBytes, and the window is bounded.
	if d.log.win.cutLine != 2 {
		t.Fatalf("the long line is not cut, cutLine %d", d.log.win.cutLine)
	}
	if got := logFirstLineOf(d.buf.String()); got != "before the long line" {
		t.Fatalf("the window starts %q", got)
	}
	if n := d.buf.ByteLen(); n > maxLineBytes+4096 {
		t.Fatalf("the window holds %d bytes", n)
	}
	// The whole file's line count is had all the same.
	if n := d.viewLines(); n != 304 {
		t.Fatalf("lines %d, want 304", n)
	}
	// The window does not move again on its own, frame after frame.
	settled := d.log.win
	hs.settle()
	if d.log.win != settled || d.log.moving {
		t.Fatal("the window of the cut line is built again")
	}
	// A jump past the long line lands on its line, without the z's.
	hs.a.goToLine(301, 1)
	hs.settle()
	if !strings.Contains(d.buf.String(), "tail line 299") {
		t.Fatal("line 301 is not in the window")
	}
	if strings.Contains(d.buf.String(), "zzz") {
		t.Fatal("the window past the long line holds its cut text")
	}
	if at := d.log.lineAt(d.log.win.start + int64(d.sel[1])); at != 300 {
		t.Fatalf("caret on line %d, want 300", at)
	}
	// The window starts past the long line: it says what stands above it.
	if d.log.win.cutLine != 0 || d.log.win.cutAbove != 2 || d.log.win.cutAboveBytes != 3<<20 {
		t.Fatalf("past the long line: cutLine %d, cutAbove %d of %d bytes",
			d.log.win.cutLine, d.log.win.cutAbove, d.log.win.cutAboveBytes)
	}
	// A jump back to the long line cuts it again, and holds still.
	hs.a.goToLine(2, 1)
	hs.settle()
	if d.log.win.cutLine != 2 {
		t.Fatalf("the long line is not cut, cutLine %d", d.log.win.cutLine)
	}
	settled = d.log.win
	hs.settle()
	if d.log.win != settled || d.log.moving {
		t.Fatal("the window of the cut line is built again")
	}
}

// A file whose first line is itself longer than the editor lays out one
// shows the line in part as well: the window is bounded and says so, and
// the lines after the first are had by a jump.
func TestLogModeFirstLineLong(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.LargeFileModeMB = 1
	var b strings.Builder
	b.WriteString(strings.Repeat("q", 3<<20)) // the first line, 3 MB
	b.WriteString("\nsecond line\nthird line\n")
	path := filepath.Join(hs.root, "firstlong.log")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	hs.a.openFile(path, true)
	hs.settle()
	d := hs.a.activeEditor()
	if d == nil || d.log == nil {
		t.Fatal("the file did not open in log mode")
	}
	// The first line shows in part, at maxLineBytes, and the window is
	// bounded by it.
	if d.log.win.cutLine != 1 {
		t.Fatalf("the first line is not cut, cutLine %d", d.log.win.cutLine)
	}
	if n := d.buf.ByteLen(); n > maxLineBytes+4096 {
		t.Fatalf("the window holds %d bytes", n)
	}
	if d.buf.LineCount() != 1 {
		t.Fatalf("the window holds %d lines, want 1", d.buf.LineCount())
	}
	if n := d.viewLines(); n != 4 {
		t.Fatalf("lines %d, want 4", n)
	}
	// The window does not move again on its own.
	settled := d.log.win
	hs.settle()
	if d.log.win != settled || d.log.moving {
		t.Fatal("the window of the cut first line is built again")
	}
	// The lines after it are had by a jump, and the cut line again after.
	for _, ln := range []int{2, 3, 1} {
		hs.a.goToLine(ln, 1)
		hs.settle()
		if at := d.log.lineAt(d.log.win.start + int64(d.sel[1])); at != ln-1 {
			t.Fatalf("goToLine(%d) left the caret on line %d", ln, at+1)
		}
	}
	if d.log.win.cutLine != 1 {
		t.Fatalf("the jump back left cutLine %d", d.log.win.cutLine)
	}
	if strings.Contains(d.buf.String(), "second line") {
		t.Fatal("the window of the cut first line holds the second line")
	}
	// A window past the cut first line says what stands above it.
	hs.a.goToLine(2, 1)
	hs.settle()
	if d.log.win.cutAbove != 1 || d.log.win.cutAboveBytes != 3<<20 {
		t.Fatalf("cutAbove %d of %d bytes, want 1 of %d", d.log.win.cutAbove, d.log.win.cutAboveBytes, 3<<20)
	}
}

// Going to a line, then to another, lands on the line asked for each
// time: the window a jump leaves does not carry the caret of the jump
// after it past the window the next jump brings.
func TestLogModeGoToLineTwice(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.LargeFileModeMB = 1
	path, _ := writeBigLog(t, hs.root, 60000, 90) // 5.4 MB
	hs.a.openFile(path, true)
	hs.settle()
	d := hs.a.activeEditor()
	if d == nil || d.log == nil {
		t.Fatal("log mode lost")
	}
	for _, ln := range []int{1400, 700, 1200, 5, 700, 59900} {
		hs.a.goToLine(ln, 1)
		hs.settle()
		at := d.log.lineAt(d.log.win.start + int64(d.sel[1]))
		if at != ln-1 {
			t.Fatalf("goToLine(%d) left the caret on line %d", ln, at+1)
		}
		if first := d.log.win.first; ln-1 < first || ln-1 >= first+d.buf.LineCount() {
			t.Fatalf("goToLine(%d) left the window at line %d of %d lines", ln, first, d.buf.LineCount())
		}
	}
}

// Typing in a log document edits the text as a whole, saves it, and the
// file on disk has the edit and nothing else changed.
func TestLogModeEditAndSave(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.AutoSave = false
	hs.a.settings.LargeFileModeMB = 1
	path, lines := writeBigLog(t, hs.root, 20000, 100)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hs.a.openFile(path, true)
	hs.settle()
	// Edit a line far into the file, through the app's own edits.
	hs.a.goToLine(15000, 1)
	hs.settle()
	d := hs.a.activeEditor()
	line, _ := d.buf.Snapshot().LineRange(15000 - 1 - d.log.win.first)
	d.edit([2]int{line + 4, line + 4}, textEdit{line + 4, line + 4, "EDITED "})
	step(hs.tt)
	if !d.dirty() {
		t.Fatal("the edit did not mark the document changed")
	}
	hs.a.saveDoc(d.document, nil)
	hs.settle()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || !strings.Contains(string(data), "EDITED ") {
		t.Fatal("the save did not write the edit")
	}
	// The part not edited is as it was, by the line.
	got := strings.SplitN(string(data), "\n", 16000)
	for i, want := range lines[:15000] {
		if i == 14999 {
			continue // the edited line
		}
		if got[i]+"\n" != want {
			t.Fatalf("line %d changed", i+1)
		}
	}
	if d.dirty() {
		t.Fatal("the save left the document changed")
	}
	_ = before
}

// Undo takes an edit back, redo makes it again.
func TestLogModeUndoRedo(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.AutoSave = false
	hs.a.settings.LargeFileModeMB = 1
	path, _ := writeBigLog(t, hs.root, 20000, 100)
	hs.a.openFile(path, true)
	hs.settle()
	d := hs.a.activeEditor()
	sel := d.sel
	d.edit(sel, textEdit{0, 0, "NOTE "})
	step(hs.tt)
	hs.settle()
	if !strings.HasPrefix(d.buf.String(), "NOTE ") {
		t.Fatalf("text %q", d.buf.String()[:40])
	}
	d.undo()
	hs.settle()
	if strings.HasPrefix(d.buf.String(), "NOTE ") {
		t.Fatal("undo left the edit")
	}
	if d.dirty() {
		t.Fatal("undo to the file's text left the document changed")
	}
	d.redo()
	hs.settle()
	if !strings.HasPrefix(d.buf.String(), "NOTE ") {
		t.Fatal("redo lost the edit")
	}
}

// A file changed on disk under a clean log document is read again; a
// changed one keeps its own text.
func TestLogModeReload(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.AutoSave = false
	hs.a.settings.LargeFileModeMB = 1
	path, _ := writeBigLog(t, hs.root, 20000, 100)
	hs.a.openFile(path, true)
	hs.settle()
	d := hs.a.activeEditor()
	// Clean: the file's text is taken.
	changed := strings.Repeat("y", 500000)
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	hs.a.reloadDoc(d.document)
	hs.settle()
	if first := logFirstLineOf(d.buf.String()); !strings.HasPrefix(changed, first) {
		t.Fatalf("a clean document kept %q", first)
	}
	if n := d.viewLines(); n != 1 {
		t.Fatalf("lines %d, want 1", n)
	}
	// Changed here: the file on disk changes under it, and the text of
	// the document is kept.
	path2, _ := writeBigLog(t, hs.root, 30000, 100)
	hs.a.openFile(path2, true)
	hs.settle()
	e := hs.a.activeEditor()
	if e.path != path2 {
		t.Fatal("the second file did not open")
	}
	e.edit(e.sel, textEdit{0, 0, "MINE "})
	step(hs.tt)
	if err := os.WriteFile(path2, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	hs.a.reloadDoc(e.document)
	hs.settle()
	if !strings.HasPrefix(e.buf.String(), "MINE ") {
		t.Fatal("a changed document took the file's text")
	}
}

// A session keeps a log document's place, of the whole text rather than
// the window.
func TestLogModeSession(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.LargeFileModeMB = 1
	path, lines := writeBigLog(t, hs.root, 20000, 100)
	hs.a.openFile(path, true)
	hs.settle()
	hs.a.goToLine(12345, 1)
	hs.settle()
	hs.a.rememberTabs()
	col := hs.a.cols[hs.a.col]
	var tab *settings.Tab
	for i := range col.tabs {
		if col.tabs[i].ed != nil && col.tabs[i].ed.path == path {
			tab = &hs.a.window.Columns[hs.a.col].Tabs[i]
		}
	}
	if tab == nil {
		t.Fatal("the tab was not kept")
	}
	if tab.Sel[1] == 0 {
		t.Fatal("the place kept is the start")
	}
	// The place is in the whole text: opening the file again lands there.
	d := hs.a.activeEditor()
	if at := d.log.lineAt(d.log.win.start + int64(d.sel[1])); at != 12344 {
		t.Fatalf("the caret is on line %d, want 12344", at)
	}
	_ = lines
}

// Typing in the text area — the keyboard's own edits, which observe sees
// rather than the app's edit — is what a save writes: an insertion, a
// deletion, multibyte runes typed between multibyte runes, and a paste
// longer than the window a change is looked for in.
func TestLogModeTypingSaves(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.AutoSave = false
	hs.a.settings.LargeFileModeMB = 1
	path, lines := writeBigLog(t, hs.root, 20000, 100)
	hs.a.openFile(path, true)
	hs.settle()
	d := hs.a.activeEditor()
	// An insertion at the start of the first line.
	hs.place(d, 0, 0)
	hs.insert("TYPED ")
	// A deletion, as the backspace key makes it.
	hs.place(d, 6, 6)
	hs.key(0, ui.KeyBackspace)
	// Multibyte runes, then one typed between them.
	hs.place(d, 0, 0)
	hs.insert("中中")
	hs.place(d, 1, 1)
	hs.insert("文")
	// A paste longer than the diff window, at the start of the fifth
	// line: line 0 holds 108 runes by then, the lines after it 100 each.
	paste := strings.Repeat("y", 3000)
	hs.place(d, 108+4*100, 108+4*100)
	hs.insert(paste)
	hs.settle()
	if !d.dirty() {
		t.Fatal("typing left the document clean")
	}
	first := "中文中TYPED" + lines[0][:len(lines[0])-1]
	want := first + "\n" + strings.Join(lines[1:5], "") + paste + strings.Join(lines[5:], "")
	hs.a.saveDoc(d.document, nil)
	hs.settle()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("saved %q…, want %q…", logFirstLineOf(string(data))[:40], logFirstLineOf(want)[:40])
	}
	// Typed again, then undone — one step or as many as pauses made them —
	// a save writes the text without what undo took back. The save before
	// cleared the history, which is another matter, that of saving at all.
	hs.place(d, 0, 0)
	hs.insert("MORE ")
	hs.settle()
	if !d.dirty() {
		t.Fatal("typing after the save left the document clean")
	}
	for range 20 {
		if !d.dirty() {
			break
		}
		if !d.undo() {
			break
		}
		hs.settle()
	}
	if d.dirty() {
		t.Fatal("undo left the typing in")
	}
	if strings.Contains(d.buf.String(), "MORE") {
		t.Fatal("undo left the typed text in the window")
	}
	hs.a.saveDoc(d.document, nil)
	hs.settle()
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatal("the save after undo did not write the text without it")
	}
}

// A change made to the buffer far from the caret — further than a change
// is looked for around it — is written as the window's whole run, and a
// save has it.
func TestLogModeFarChangeSaves(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.AutoSave = false
	hs.a.settings.LargeFileModeMB = 1
	path, lines := writeBigLog(t, hs.root, 20000, 100)
	hs.a.openFile(path, true)
	hs.settle()
	d := hs.a.activeEditor()
	// The caret down in the file, the change made at the top.
	hs.place(d, 3000, 3000)
	d.buf.Replace(0, 0, strings.Repeat("y", 5000))
	step(hs.tt) // observe sees the change, made far from the caret
	if !d.dirty() {
		t.Fatal("the change left the document clean")
	}
	hs.a.saveDoc(d.document, nil)
	hs.settle()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Repeat("y", 5000) + logFirstLineOf(lines[0]); !strings.HasPrefix(string(data), want) {
		t.Fatal("the save lost the change made far from the caret")
	}
}

// logFirstLineOf returns the first line of a text, without its line end.
func logFirstLineOf(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}
