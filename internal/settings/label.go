package settings

import (
	"strings"
	"unicode"
)

// MaxLabel is as many characters as a project's own label on the rail
// holds.
const MaxLabel = 2

// RailLabel is a project's own label as the rail shows it: its first two
// characters, "" for none.
func RailLabel(s string) string {
	cs := LabelChars(strings.TrimSpace(s))
	return strings.Join(cs[:min(len(cs), MaxLabel)], "")
}

// LabelTooLong reports whether s is more characters than a label on the
// rail holds, as a person sees them.
func LabelTooLong(s string) bool { return len(LabelChars(s)) > MaxLabel }

// LabelChars splits s into the characters a person sees: a letter with
// its accents, an emoji with its modifiers and those it joins, a flag of
// its two letters.
func LabelChars(s string) []string {
	var out []string
	var prev rune
	start, flag := 0, false
	for i, r := range s {
		joined := i > 0 && (unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) ||
			r == 0x200d || prev == 0x200d || r == 0xfe0f || r == 0xfe0e ||
			r >= 0x1f3fb && r <= 0x1f3ff || r >= 0xe0020 && r <= 0xe007f)
		regional := r >= 0x1f1e6 && r <= 0x1f1ff
		if regional && flag {
			joined, flag = true, false
		} else {
			flag = regional
		}
		if i > 0 && !joined {
			out = append(out, s[start:i])
			start = i
		}
		prev = r
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
