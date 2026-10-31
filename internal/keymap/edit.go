package keymap

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Entry is a shortcut as the file writes it.
type Entry struct {
	Key     string          `json:"key"`
	Command string          `json:"command"`
	When    string          `json:"when,omitempty"`
	Args    json.RawMessage `json:"args,omitempty"`
}

// String is the stroke as the file writes it, as "shift+cmd+p".
func (s Stroke) String() string {
	var parts []string
	if s.Mods&ui.Ctrl != 0 {
		parts = append(parts, "ctrl")
	}
	if s.Mods&ui.Shift != 0 {
		parts = append(parts, "shift")
	}
	if s.Mods&ui.Alt != 0 {
		parts = append(parts, "alt")
	}
	if s.Mods&ui.Super != 0 {
		parts = append(parts, "cmd")
	}
	name := "?"
	for n, k := range keyNames {
		if k == s.Key {
			name = n
			break
		}
	}
	return strings.Join(append(parts, name), "+")
}

// KeysString is a key or a chord as the file writes it, as "cmd+k cmd+s".
func KeysString(keys []Stroke) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k.String()
	}
	return strings.Join(parts, " ")
}

// span is where an entry of the file is, from its brace to after the one
// closing it.
type span struct {
	start, end int
	entry      Entry
}

// Edit returns the shortcuts of data with those drop reports taken out
// and add after the others, keeping the rest of the file as it is: its
// comments, its order and its layout. An empty file is Template first.
func Edit(data []byte, drop func(Entry) bool, add []Entry) ([]byte, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte(Template)
	}
	_, spans, err := scan(data)
	if err != nil {
		return nil, err
	}
	out := slices.Clone(data)
	// From the last, so that the places before stay.
	for i := len(spans) - 1; i >= 0; i-- {
		if s := spans[i]; drop != nil && drop(s.entry) {
			from, to := cutRange(out, s.start, s.end)
			out = slices.Delete(out, from, to)
		}
	}
	if len(add) == 0 {
		return out, nil
	}
	open, spans, err := scan(out)
	if err != nil {
		return nil, err
	}
	indent := "  "
	at, comma := open+1, false
	if len(spans) > 0 {
		last := spans[len(spans)-1]
		indent = lineIndent(out, last.start, indent)
		at, comma = last.end, true
		// After its comma, and a comment on its line.
		j := skipBlanks(out, at)
		if j < len(out) && out[j] == ',' {
			at, comma = j+1, false
		}
		j = skipSpaces(out, at)
		if bytes.HasPrefix(out[j:], []byte("//")) {
			for j < len(out) && out[j] != '\n' {
				j++
			}
			at = j
		}
	}
	var b strings.Builder
	if comma {
		b.WriteString(",")
	}
	for i, e := range add {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("\n" + indent + formatEntry(e))
	}
	rest := out[at:]
	if len(spans) == 0 && !bytes.HasPrefix(bytes.TrimLeft(rest, " \t"), []byte("\n")) {
		b.WriteString("\n")
	}
	res := append(slices.Clone(out[:at]), b.String()...)
	return append(res, rest...), nil
}

// formatEntry writes an entry on a line, as the docs' examples are.
func formatEntry(e Entry) string {
	q := func(s string) string {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		enc.Encode(s)
		return strings.TrimSpace(b.String())
	}
	s := `{ "key": ` + q(e.Key) + `, "command": ` + q(e.Command)
	if e.When != "" {
		s += `, "when": ` + q(e.When)
	}
	if len(e.Args) > 0 {
		var compact bytes.Buffer
		if json.Compact(&compact, e.Args) == nil {
			s += `, "args": ` + compact.String()
		}
	}
	return s + " }"
}

// cutRange is what taking out the entry from start to end takes of data:
// its comma after it and a comment after that on its line, and its line
// when it has the line to itself.
func cutRange(data []byte, start, end int) (from, to int) {
	from, to = start, end
	line := bytes.LastIndexByte(data[:start], '\n') + 1
	alone := len(bytes.TrimSpace(data[line:start])) == 0
	if alone {
		from = line
	}
	j := skipSpaces(data, to)
	if j < len(data) && data[j] == ',' {
		to = j + 1
		j = skipSpaces(data, to)
	}
	if bytes.HasPrefix(data[j:], []byte("//")) {
		for j < len(data) && data[j] != '\n' && data[j] != '\r' {
			j++
		}
		to = j
	}
	if alone && j < len(data) && (data[j] == '\n' || data[j] == '\r') {
		if data[j] == '\r' {
			j++
		}
		if j < len(data) && data[j] == '\n' {
			j++
		}
		to = j
	} else if alone {
		// The next item or the bracket is on the line: it keeps the
		// line's indent.
		from = start
	}
	return from, to
}

// lineIndent is the blanks the line of at starts with, or def when the
// line has more than blanks before at.
func lineIndent(data []byte, at int, def string) string {
	line := bytes.LastIndexByte(data[:at], '\n') + 1
	if lead := data[line:at]; len(bytes.TrimLeft(lead, " \t")) == 0 && len(lead) > 0 {
		return string(lead)
	}
	return def
}

func skipSpaces(data []byte, i int) int {
	for i < len(data) && (data[i] == ' ' || data[i] == '\t') {
		i++
	}
	return i
}

// skipBlanks skips white space and comments.
func skipBlanks(data []byte, i int) int {
	for i < len(data) {
		switch c := data[i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case bytes.HasPrefix(data[i:], []byte("//")):
			for i < len(data) && data[i] != '\n' {
				i++
			}
		case bytes.HasPrefix(data[i:], []byte("/*")):
			end := bytes.Index(data[i+2:], []byte("*/"))
			if end < 0 {
				return len(data)
			}
			i += 2 + end + 2
		default:
			return i
		}
	}
	return i
}

// skipValue returns the end of the value of JSON at i.
func skipValue(data []byte, i int) (int, error) {
	if i >= len(data) {
		return i, errors.New("unexpected end of the file")
	}
	switch data[i] {
	case '"':
		return skipString(data, i), nil
	case '{', '[':
		closing := byte('}')
		if data[i] == '[' {
			closing = ']'
		}
		j := i + 1
		for {
			j = skipBlanks(data, j)
			if j >= len(data) {
				return j, errors.New("unexpected end of the file")
			}
			switch data[j] {
			case closing:
				return j + 1, nil
			case ',', ':':
				j++
				continue
			}
			var err error
			if j, err = skipValue(data, j); err != nil {
				return j, err
			}
		}
	}
	j := i
	for j < len(data) && !bytes.ContainsRune([]byte(" \t\r\n,:]}/"), rune(data[j])) {
		j++
	}
	if j == i {
		return j, errors.New("unexpected " + string(data[i]))
	}
	return j, nil
}

// scan finds the bracket the list of shortcuts opens with, and its
// entries.
func scan(data []byte) (open int, spans []span, err error) {
	i := skipBlanks(data, 0)
	if i >= len(data) || data[i] != '[' {
		return 0, nil, errors.New("keybindings.json is not a list")
	}
	open = i
	i++
	for {
		i = skipBlanks(data, i)
		if i >= len(data) {
			return 0, nil, errors.New("unexpected end of the file")
		}
		switch data[i] {
		case ']':
			return open, spans, nil
		case ',':
			i++
			continue
		}
		end, err := skipValue(data, i)
		if err != nil {
			return 0, nil, err
		}
		if data[i] == '{' {
			var e Entry
			json.Unmarshal(StripJSONC(data[i:end]), &e)
			spans = append(spans, span{i, end, e})
		}
		i = end
	}
}
