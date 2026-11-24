package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// openTestLog makes a file of the text given and opens it as a log
// document, failing the test when it cannot.
func openTestLog(t *testing.T, text string) *logDoc {
	t.Helper()
	path := filepath.Join(t.TempDir(), "big.log")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := openLogDoc(path)
	if err != nil {
		t.Fatalf("openLogDoc(%q): %v", text, err)
	}
	t.Cleanup(l.close)
	return l
}

// logTextOf returns the whole text of a log document with its edits, for
// tests to compare with.
func logTextOf(t *testing.T, l *logDoc) string {
	t.Helper()
	w, err := l.materialize(0, l.lineCount())
	if err != nil {
		t.Fatal(err)
	}
	// A window is bounded: read the rest in windows over the lines.
	out := w.text
	for first := w.first + w.lines; ; {
		w, err = l.materialize(first, 1)
		if err != nil {
			t.Fatal(err)
		}
		if w.first != first || w.text == "" {
			break
		}
		out += w.text
		first = w.first + w.lines
		if len(out) > 1<<22 {
			t.Fatal("materialize did not advance")
		}
	}
	return out
}

// The index of a file's lines: LF endings, CRLF endings, a last line
// without one, an empty file, and a byte order mark.
func TestLogDocScan(t *testing.T) {
	for _, tc := range []struct {
		name  string
		text  string
		lines []logLine
		end   logLine
		crlf  bool
		bom   bool
	}{
		{name: "empty", text: "", lines: []logLine{{0, 0}}, end: logLine{0, 0}},
		{name: "one line", text: "hello", lines: []logLine{{0, 0}}, end: logLine{5, 5}},
		{name: "lf", text: "a\nb\n", lines: []logLine{{0, 0}, {2, 2}, {4, 4}}, end: logLine{4, 4}},
		{name: "no last lf", text: "a\nb", lines: []logLine{{0, 0}, {2, 2}}, end: logLine{3, 3}},
		{name: "crlf", text: "a\r\nb\r\n", lines: []logLine{{0, 0}, {3, 2}, {6, 4}}, end: logLine{6, 4}, crlf: true},
		{name: "utf8", text: "αα\nβ\n", lines: []logLine{{0, 0}, {5, 3}, {8, 5}}, end: logLine{8, 5}},
		{name: "bom", text: "\xEF\xBB\xBFa\n", lines: []logLine{{3, 0}, {5, 2}}, end: logLine{5, 2}, bom: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := openTestLog(t, tc.text)
			if len(l.file.lines) != len(tc.lines) {
				t.Fatalf("lines %v, want %v", l.file.lines, tc.lines)
			}
			for i, line := range tc.lines {
				if l.file.lines[i] != line {
					t.Fatalf("line %d is %v, want %v", i, l.file.lines[i], line)
				}
			}
			if l.file.end != tc.end {
				t.Fatalf("end %v, want %v", l.file.end, tc.end)
			}
			if l.file.crlf != tc.crlf || l.file.bom != tc.bom {
				t.Fatalf("crlf=%v bom=%v, want %v %v", l.file.crlf, l.file.bom, tc.crlf, tc.bom)
			}
			if n := l.lineCount(); n != len(tc.lines) {
				t.Fatalf("lineCount %d, want %d", n, len(tc.lines))
			}
		})
	}
}

// A file that is not UTF-8 text is not opened in log mode.
func TestLogDocRejectsNotText(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{name: "utf16", text: "\xFF\xFE\x00a\x00\n"},
		{name: "invalid", text: "a\n\xFF\n"},
		{name: "nul", text: "a\n\x00b\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "big.log")
			if err := os.WriteFile(path, []byte(tc.text), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := openLogDoc(path); err == nil {
				t.Fatal("opened what is not UTF-8 text")
			}
		})
	}
}

// Where the lines of a file with CRLF endings are: bytes as the file has
// them, runes as the editor holds them.
func TestLogDocOffsets(t *testing.T) {
	l := openTestLog(t, "a\r\nbb\r\nccc\r\ndd")
	if at := l.lineAt(0); at != 0 {
		t.Fatalf("lineAt(0) = %d", at)
	}
	if at := l.lineAt(2); at != 1 { // the CR LF counts one rune
		t.Fatalf("lineAt(2) = %d, want 1", at)
	}
	if at := l.lineAt(5); at != 2 {
		t.Fatalf("lineAt(5) = %d, want 2", at)
	}
	if s := l.lineStart(1); s != 2 {
		t.Fatalf("lineStart(1) = %d, want 2", s)
	}
	if s := l.lineStart(3); s != 9 {
		t.Fatalf("lineStart(3) = %d, want 9", s)
	}
	// Byte offsets map through the line they are in: rune 5 is the first
	// c of ccc, at byte 7.
	rd, err := l.openLogRead()
	if err != nil {
		t.Fatal(err)
	}
	defer rd.f.Close()
	if b := rd.byteAt(5); b != 7 {
		t.Fatalf("byteAt(5) = %d, want 7", b)
	}
}

// A window of a file holds the lines around a place, bounded by its byte
// budget, whole lines first.
func TestLogDocMaterialize(t *testing.T) {
	lines := make([]string, 20000)
	for i := range lines {
		lines[i] = strings.Repeat("x", 100) + "\n"
	}
	l := openTestLog(t, strings.Join(lines, ""))
	w, err := l.materialize(10000, 50)
	if err != nil {
		t.Fatal(err)
	}
	if w.first != 10000-logMarginLines {
		t.Fatalf("first = %d, want %d", w.first, 10000-logMarginLines)
	}
	// The whole file fits the budget's lines many times over: the window
	// is the budget's worth of them.
	if n := len(w.text); n > logWindowBytes {
		t.Fatalf("window is %d bytes", n)
	}
	if !strings.HasPrefix(w.text, lines[w.first]) {
		t.Fatal("window does not start at its first line")
	}
	// The lines the window holds are as many as its text says.
	if w.lines != strings.Count(w.text, "\n")+1 {
		t.Fatalf("lines = %d, text holds %d", w.lines, strings.Count(w.text, "\n")+1)
	}
	// The rune it starts at is the line's.
	if w.start != l.lineStart(w.first) {
		t.Fatalf("start = %d, want %d", w.start, l.lineStart(w.first))
	}
	// A window of the last lines covers the end of the file.
	w, err = l.materialize(19999, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(w.text, lines[19999]) {
		t.Fatal("window does not end at the last line")
	}
	// A line longer than the budget ends the window within it.
	l = openTestLog(t, "short\n"+strings.Repeat("y", 3*logWindowBytes)+"\nend\n")
	w, err = l.materialize(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(w.text); n > logWindowBytes {
		t.Fatalf("window of a long line is %d bytes", n)
	}
	if !strings.HasPrefix(w.text, "short\n") {
		t.Fatal("window does not start at a line's edge")
	}
}

// testEdit is an edit a test makes: runes start up to end of the text as
// it is when the edit is made, replaced with text.
type testEdit struct {
	start, end int64
	text       string
}

// Edits over the text, undone and made again.
func TestLogDocEdits(t *testing.T) {
	base := "alpha\nbeta\ngamma\ndelta\n"
	for _, tc := range []struct {
		name  string
		base  string
		edits []testEdit // made one after the other, in the text as it is each time
		want  string
	}{
		{
			name: "insert", base: base,
			edits: []testEdit{{6, 6, "XX "}},
			want:  "alpha\nXX beta\ngamma\ndelta\n",
		},
		{
			name: "delete", base: base,
			edits: []testEdit{{6, 10, ""}},
			want:  "alpha\n\ngamma\ndelta\n",
		},
		{
			name: "replace line", base: base,
			edits: []testEdit{{6, 10, "BETA"}},
			want:  "alpha\nBETA\ngamma\ndelta\n",
		},
		{
			name: "add a line", base: base,
			edits: []testEdit{{6, 6, "new\n"}},
			want:  "alpha\nnew\nbeta\ngamma\ndelta\n",
		},
		{
			name: "two edits", base: base,
			edits: []testEdit{{0, 5, "ALPHA"}, {11, 16, "GAMMA"}},
			want:  "ALPHA\nbeta\nGAMMA\ndelta\n",
		},
		{
			name: "edit over an edit", base: base,
			edits: []testEdit{{6, 10, "BETA"}, {6, 10, "B"}},
			want:  "alpha\nB\ngamma\ndelta\n",
		},
		{
			name: "type into an edit", base: base,
			edits: []testEdit{{6, 6, "B"}, {7, 7, "E"}, {8, 8, "T"}, {9, 9, "A"}},
			want:  "alpha\nBETAbeta\ngamma\ndelta\n",
		},
		{
			name: "select the window and replace", base: base,
			edits: []testEdit{{6, 16, "rest"}},
			want:  "alpha\nrest\ndelta\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := openTestLog(t, tc.base)
			var undos []logEdit
			for _, e := range tc.edits {
				u, ok := mustApplyEdit(t, l, e)
				if !ok {
					t.Fatalf("edit %+v changed nothing", e)
				}
				undos = append(undos, u)
			}
			if !l.hasEdits() {
				t.Fatal("edits left no mark")
			}
			if got := logTextOf(t, l); got != tc.want {
				t.Fatalf("text %q, want %q", got, tc.want)
			}
			// Undoing them all, from the last, is the file again.
			redo := mustApplyBase(t, l, undos)
			if got := logTextOf(t, l); got != tc.base {
				t.Fatalf("undone text %q, want %q", got, tc.base)
			}
			if l.hasEdits() {
				t.Fatal("undoing back to the file left it changed")
			}
			// Making them again is the text with the edits again.
			mustApplyBase(t, l, redo)
			if got := logTextOf(t, l); got != tc.want {
				t.Fatalf("remade text %q, want %q", got, tc.want)
			}
		})
	}
}

// mustApplyEdit makes an edit of a log document's text, failing the test
// when it cannot.
func mustApplyEdit(t *testing.T, l *logDoc, e testEdit) (logEdit, bool) {
	t.Helper()
	u, ok, err := l.applyEdit(e.start, e.end, e.text)
	if err != nil {
		t.Fatal(err)
	}
	return u, ok
}

// mustApplyBase makes the replacements of a log document's base, failing
// the test when it cannot.
func mustApplyBase(t *testing.T, l *logDoc, es []logEdit) []logEdit {
	t.Helper()
	undo, err := l.applyBase(es)
	if err != nil {
		t.Fatal(err)
	}
	return undo
}

// A save writes the file with the edits and leaves the parts not edited
// as the bytes they are.
func TestLogDocSave(t *testing.T) {
	for _, tc := range []struct {
		name    string
		text    string
		crlf    bool
		edits   []testEdit
		want    string
		keepRaw bool
	}{
		{
			name: "no edits, lf", text: "a\nb\nc\n",
			want: "a\nb\nc\n", keepRaw: true,
		},
		{
			name: "no edits, crlf", text: "a\r\nb\r\nc\r\n", crlf: true,
			want: "a\r\nb\r\nc\r\n", keepRaw: true,
		},
		{
			name: "no edits, bom", text: "\xEF\xBB\xBFa\nb\n",
			want: "\xEF\xBB\xBFa\nb\n", keepRaw: true,
		},
		{
			name: "edit, lf", text: "a\nb\nc\n",
			edits: []testEdit{{2, 3, "B"}},
			want:  "a\nB\nc\n",
		},
		{
			name: "edit keeps the rest raw", text: "a\r\nb\r\nc\r\n", crlf: true,
			edits: []testEdit{{2, 3, "B"}},
			want:  "a\r\nB\r\nc\r\n",
		},
		{
			name: "edit of a bom file keeps the mark", text: "\xEF\xBB\xBFa\nb\n",
			edits: []testEdit{{2, 3, "B"}},
			want:  "\xEF\xBB\xBFa\nB\n",
		},
		{
			name: "line endings changed", text: "a\nb\n", crlf: true,
			edits: []testEdit{{2, 3, "B"}},
			want:  "a\r\nB\r\n",
		},
		{
			name: "edit adds lines, crlf file", text: "a\r\nb\r\n", crlf: true,
			edits: []testEdit{{2, 2, "x\ny\n"}},
			want:  "a\r\nx\r\ny\r\nb\r\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := openTestLog(t, tc.text)
			for _, e := range tc.edits {
				mustApplyEdit(t, l, e)
			}
			if _, _, err := l.saveTo(l.path, tc.crlf, l.editsCopy()); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(l.path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tc.want {
				t.Fatalf("saved %q, want %q", data, tc.want)
			}
			if tc.keepRaw && string(data) != tc.text {
				t.Fatalf("save rewrote a file not edited: %q", data)
			}
		})
	}
}

// Editing into runes an edit already put in, of more than one byte each:
// the second edit's ends land inside the first's text, on rune edges, and
// a save writes the bytes whole.
func TestLogDocEditsMultibyte(t *testing.T) {
	l := openTestLog(t, "alpha\nbeta\ngamma\n")
	mustApplyEdit(t, l, testEdit{6, 6, "中中"})
	// Between the two 中, which sit at the text's runes 6 and 7.
	mustApplyEdit(t, l, testEdit{7, 7, "文"})
	want := "alpha\n中文中beta\ngamma\n"
	if got := logTextOf(t, l); got != want {
		t.Fatalf("text %q, want %q", got, want)
	}
	if at := l.lineAt(7); at != 1 {
		t.Fatalf("lineAt(7) = %d, want 1", at)
	}
	// A line break within multibyte runes.
	mustApplyEdit(t, l, testEdit{7, 7, "文\n中"})
	want = "alpha\n中文\n中文中beta\ngamma\n"
	if got := logTextOf(t, l); got != want {
		t.Fatalf("text %q, want %q", got, want)
	}
	if n := l.lineCount(); n != 5 {
		t.Fatalf("lines %d, want 5", n)
	}
	if at := l.lineAt(9); at != 2 {
		t.Fatalf("lineAt(9) = %d, want 2", at)
	}
	if _, _, err := l.saveTo(l.path, false, l.editsCopy()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("saved %q, want %q", data, want)
	}
}

// A file that changed under a document with edits is not saved over: the
// save fails, and what was appended to the file and what was taken from it
// stand as they are.
func TestLogDocSaveRefusesChangedFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(t *testing.T, path string)
		want string
	}{
		{
			name: "appended to",
			edit: func(t *testing.T, path string) {
				f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.WriteString("d\n"); err != nil {
					t.Fatal(err)
				}
				f.Close()
			},
			want: "a\nb\nc\nd\n",
		},
		{
			name: "taken short",
			edit: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("a\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: "a\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := openTestLog(t, "a\nb\nc\n")
			mustApplyEdit(t, l, testEdit{2, 3, "B"})
			tc.edit(t, l.path)
			if _, _, err := l.saveTo(l.path, false, l.editsCopy()); err == nil {
				t.Fatal("saved over a file that changed")
			}
			data, err := os.ReadFile(l.path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tc.want {
				t.Fatalf("the file is %q, want %q", data, tc.want)
			}
			if strings.IndexByte(string(data), 0) >= 0 {
				t.Fatal("the file holds NUL bytes")
			}
		})
	}
}

// A saved document's index is the file as written, and the edits made
// while it saved hold over the new base.
func TestLogDocSaveRebase(t *testing.T) {
	l := openTestLog(t, "one\ntwo\nthree\n")
	mustApplyEdit(t, l, testEdit{4, 7, "TWO"})
	// An edit made after the snapshot the save takes.
	saved := l.editsCopy()
	mustApplyEdit(t, l, testEdit{8, 13, "FOUR"})
	_, nf, err := l.saveTo(l.path, false, saved)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "one\nTWO\nthree\n" {
		t.Fatalf("saved %q", got)
	}
	// The document takes the file as written: the post-save edit holds
	// over the new base, and the text keeps it.
	l.takeFile(nf, saved)
	if got := logTextOf(t, l); got != "one\nTWO\nFOUR\n" {
		t.Fatalf("text after save %q", got)
	}
	if n := l.lineCount(); n != 4 {
		t.Fatalf("lines %d, want 4", n)
	}
}

// A large file scans and opens the window of it without reading it all.
func TestLogDocLargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	path := filepath.Join(t.TempDir(), "big.log")
	var b strings.Builder
	for i := range 60000 {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("z", 50))
		b.WriteString(" ")
		b.WriteString(fmtInt(i))
		b.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := openLogDoc(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.close)
	if n := l.lineCount(); n != 60001 {
		t.Fatalf("lines %d, want 60001", n)
	}
	// A window in the middle holds the lines around it, whole.
	w, err := l.materialize(30000, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(w.text, "line "+strings.Repeat("z", 50)+" "+fmtInt(w.first)+"\n") {
		t.Fatalf("window starts %q", w.text[:80])
	}
}

// A line longer than the editor lays out one shows in part, at
// maxLineBytes, and ends the window; the lines after it are of the next
// window.
func TestLogDocLongLine(t *testing.T) {
	var b strings.Builder
	b.WriteString("before the long line\n")
	b.WriteString(strings.Repeat("z", 5*logWindowBytes))
	b.WriteString("\nafter the long line\n")
	for i := range 40 {
		b.WriteString("padding line ")
		b.WriteString(fmtInt(i))
		b.WriteString("\n")
	}
	l := openTestLog(t, b.String())
	w, err := l.materialize(0, 50)
	if err != nil {
		t.Fatal(err)
	}
	// The long line is cut at maxLineBytes and ends the window: the
	// buffer holds a bounded text, not the megabytes of the line.
	if n := len(w.text); n > maxLineBytes+64 {
		t.Fatalf("window of a long line is %d bytes", n)
	}
	if !strings.HasPrefix(w.text, "before the long line\n") {
		t.Fatal("the window does not start at the first line")
	}
	if w.cutLine != 2 {
		t.Fatalf("cut line %d, want 2", w.cutLine)
	}
	if strings.Contains(w.text, "after the long line") {
		t.Fatal("the window went past the line it cuts")
	}
	// The lines after it are of the next window.
	w, err = l.materialize(2, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(w.text, "after the long line\n") {
		t.Fatalf("the window past the long line starts %q", w.text[:40])
	}
	// What the whole text is does not change: the line is whole in it.
	if n := l.lineCount(); n != 44 {
		t.Fatalf("lines %d, want 44", n)
	}
	if at := l.lineAt(l.lineStart(2) + 1); at != 2 {
		t.Fatalf("line 2 is whole in the text, at %d", at)
	}
}

// A save that rewrites the file while windows are built in the background
// races nothing: each read opens the file of its own and takes the index
// as it is. Run with -race to tell.
func TestLogDocSaveAndWindowRace(t *testing.T) {
	var b strings.Builder
	for i := range 40000 {
		b.WriteString("line ")
		b.WriteString(fmtInt(i))
		b.WriteString("\n")
	}
	l := openTestLog(t, b.String())
	mustApplyEdit(t, l, testEdit{6, 11, "EDITED"})
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 8 {
				if _, err := l.materialize(i*4000, 50); err != nil {
					t.Errorf("materialize: %v", err)
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 3 {
			// The edits of the save are the document's as it is: the
			// window builds and the takes move it under the saves.
			saved := l.editsCopy()
			_, nf, err := l.saveTo(l.path, false, saved)
			if err != nil {
				t.Errorf("saveTo: %v", err)
				return
			}
			l.takeFile(nf, saved)
		}
	}()
	wg.Wait()
	// The text is what the last save wrote, plus the edits that are not
	// among the ones it wrote.
	data, err := os.ReadFile(l.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "EDITED") {
		t.Fatal("the saves lost the edit")
	}
	if got := logTextOf(t, l); !strings.HasPrefix(got, "line 0EDITED 1") {
		t.Fatalf("text after the saves %q", got[:40])
	}
}

// fmtInt writes n in decimal, without importing fmt for one use.
func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
