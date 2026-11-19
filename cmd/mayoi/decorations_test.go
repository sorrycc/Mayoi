package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// A file is checked for UTF-8 only in its first 8 KB, so a line past it
// can hold bytes that aren't; selecting it must still mark its spaces.
func TestSelectedWhitespaceMalformedUTF8(t *testing.T) {
	hs := newHarness(t, true)
	lines := []string{
		"é a\xff",        // a stray byte ending the line
		"é \xff b\tc",    // one inside it, before a space and a tab
		"x \uFFFD y",     // a real U+FFFD
		"漢字 字\t漢",        // CJK and a tab
		"\xe6\xbc a\xe6", // cut runes at both ends
	}
	head := bytes.Repeat([]byte("abcdefg\n"), 1100)
	data := append(head, strings.Join(lines, "\n")+"\n"...)
	os.WriteFile(hs.path("x.txt"), data, 0o644)
	hs.a.openFile(hs.path("x.txt"), true)
	hs.settle()
	d := hs.a.activeEditor()
	snap := d.buf.Snapshot()
	for n, want := range lines {
		i := 1100 + n
		s, e := snap.LineRange(i)
		if got := snap.Slice(s, e); got != want {
			t.Fatalf("line %d = %q, want %q unchanged", i, got, want)
		}
		d.sel = [2]int{s, e}
		marks := hs.a.selectedWhitespace(d, snap, i, i)
		want := strings.Count(want, " ") + strings.Count(want, "\t")
		if len(marks) != want {
			t.Errorf("line %q: %d marks, want %d", lines[n], len(marks), want)
		}
	}

	// On valid text, a space's mark spans what textWidth measures.
	i := 1103
	s, e := snap.LineRange(i)
	d.sel = [2]int{s, e}
	marks := hs.a.selectedWhitespace(d, snap, i, i)
	if len(marks) != 2 {
		t.Fatalf("%d marks on %q, want 2", len(marks), lines[3])
	}
	text := lines[3]
	sp := strings.Index(text, " ")
	x0, x1 := hs.a.textWidth(text[:sp]), hs.a.textWidth(text[:sp+1])
	if m := marks[0]; m.tab || abs32(m.x0-x0) > 0.01 || abs32(m.x1-x1) > 0.01 {
		t.Errorf("space mark = %+v, want x %v to %v", m, x0, x1)
	}
	if m := marks[1]; !m.tab || m.x1 != hs.a.font.afterTab(m.x0) {
		t.Errorf("tab mark = %+v, want to the next tab stop", m)
	}
}
