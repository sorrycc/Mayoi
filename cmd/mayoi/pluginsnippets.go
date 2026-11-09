package main

import (
	"encoding/json"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"mayoi/internal/keymap"
	"mayoi/locales"
)

// snippet is a snippet of a plugin's, in VS Code's format.
type snippet struct {
	name, prefix, description string
	body                      string
}

// snippetsFor are the snippets the plugins have for a language, and for
// every language, by prefix.
func snippetsFor(lang string) []snippet {
	if plugins == nil {
		return nil
	}
	var out []snippet
	for _, p := range plugins.enabled() {
		for _, sc := range p.Contributes.Snippets {
			if sc.Language != lang && sc.Language != "*" {
				continue
			}
			data, err := fs.ReadFile(p.FS(), path.Clean(sc.Path))
			if err != nil {
				continue
			}
			var file map[string]struct {
				Prefix      json.RawMessage `json:"prefix"`
				Body        json.RawMessage `json:"body"`
				Description string          `json:"description"`
			}
			if json.Unmarshal(keymap.StripJSONC(data), &file) != nil {
				continue
			}
			for name, s := range file {
				body := stringOrLines(s.Body, "\n")
				for _, prefix := range stringsOf(s.Prefix) {
					out = append(out, snippet{name: name, prefix: prefix, description: s.Description, body: body})
				}
				if len(stringsOf(s.Prefix)) == 0 {
					out = append(out, snippet{name: name, prefix: name, description: s.Description, body: body})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].prefix < out[j].prefix })
	return out
}

func stringsOf(raw json.RawMessage) []string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []string{s}
	}
	var list []string
	json.Unmarshal(raw, &list)
	return list
}

func stringOrLines(raw json.RawMessage, sep string) string {
	return strings.Join(stringsOf(raw), sep)
}

// insertSnippet asks for a snippet of the document's language and puts it
// at the selection.
func (a *app) insertSnippet() {
	d := a.textEditor()
	if d == nil {
		return
	}
	list := snippetsFor(languageOf(d.document))
	if len(list) == 0 {
		a.tell(note{message: locales.SnippetsNone(languageOf(d.document))})
		return
	}
	items := make([]pickItem, len(list))
	for i, s := range list {
		items[i] = pickItem{Label: s.prefix, Description: s.name, Detail: s.description, Icon: "braces"}
	}
	a.openPluginPicker(items, locales.SnippetsPlaceholder(), func(i int) {
		if i >= 0 {
			a.queueEdit(d, func(d *editor) { expandSnippet(d, list[i].body, a.snippetVars(d)) })
		}
	})
}

// snippetVars are the values of a snippet's variables for an editor.
func (a *app) snippetVars(d *editor) map[string]string {
	snap := d.buf.Snapshot()
	s, e := min(d.sel[0], d.sel[1]), max(d.sel[0], d.sel[1])
	now := time.Now()
	base := filepath.Base(d.path)
	return map[string]string{
		"TM_SELECTED_TEXT":     snap.Slice(s, e),
		"SELECTION":            snap.Slice(s, e),
		"TM_FILENAME":          base,
		"TM_FILENAME_BASE":     strings.TrimSuffix(base, filepath.Ext(base)),
		"TM_DIRECTORY":         filepath.Dir(d.path),
		"TM_FILEPATH":          d.path,
		"TM_LINE_NUMBER":       strconv.Itoa(snap.LineAt(s) + 1),
		"WORKSPACE_FOLDER":     a.root,
		"CURRENT_YEAR":         now.Format("2006"),
		"CURRENT_YEAR_SHORT":   now.Format("06"),
		"CURRENT_MONTH":        now.Format("01"),
		"CURRENT_DATE":         now.Format("02"),
		"CURRENT_HOUR":         now.Format("15"),
		"CURRENT_MINUTE":       now.Format("04"),
		"CURRENT_SECOND":       now.Format("05"),
		"CURRENT_DAY_NAME":     now.Format("Monday"),
		"CURRENT_MONTH_NAME":   now.Format("January"),
		"LINE_COMMENT":         commentFor(d.document),
		"CURRENT_SECONDS_UNIX": strconv.FormatInt(now.Unix(), 10),
	}
}

// expandSnippet puts a snippet's body in place of the selection, indented
// as the caret's line is, with the first tab stop selected.
func expandSnippet(d *editor, body string, vars map[string]string) {
	text, stop := snippetText(body, vars)
	snap := d.buf.Snapshot()
	s, e := min(d.sel[0], d.sel[1]), max(d.sel[0], d.sel[1])
	ls, _ := snap.LineRange(snap.LineAt(s))
	indent := leading(snap.Slice(ls, s))
	unit := d.indent.unit()
	// Tabs of the body are the document's indentation, and its lines after
	// the first are indented as the caret's line.
	var b strings.Builder
	offsets := map[int]int{} // byte offset in text -> in the result
	for i, r := range text {
		offsets[i] = b.Len()
		switch r {
		case '\t':
			b.WriteString(unit)
		case '\n':
			b.WriteString("\n" + indent)
		default:
			b.WriteRune(r)
		}
	}
	offsets[len(text)] = b.Len()
	out := b.String()
	from := s + utf8.RuneCountInString(out[:offsets[stop[0]]])
	to := s + utf8.RuneCountInString(out[:offsets[stop[1]]])
	d.edit([2]int{from, to}, textEdit{s, e, out})
}

// snippetText is a snippet's body with its tab stops, placeholders,
// choices and variables made text, and where its first tab stop is, in
// bytes: $1, else $0, else the end.
func snippetText(body string, vars map[string]string) (string, [2]int) {
	var b strings.Builder
	type stop struct{ from, to int }
	stops := map[int]stop{}
	var parse func(s string, depth int)
	parse = func(s string, depth int) {
		for i := 0; i < len(s); i++ {
			c := s[i]
			switch {
			case c == '\\' && i+1 < len(s) && strings.ContainsRune(`$}\,|`, rune(s[i+1])):
				b.WriteByte(s[i+1])
				i++
			case c == '$' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9':
				j := i + 1
				for j < len(s) && s[j] >= '0' && s[j] <= '9' {
					j++
				}
				n, _ := strconv.Atoi(s[i+1 : j])
				if _, ok := stops[n]; !ok {
					stops[n] = stop{b.Len(), b.Len()}
				}
				i = j - 1
			case c == '$' && i+1 < len(s) && s[i+1] == '{':
				end := matchBrace(s, i+1)
				if end < 0 {
					b.WriteByte(c)
					continue
				}
				inner := s[i+2 : end]
				i = end
				name, rest := inner, ""
				if k := strings.IndexAny(inner, ":|"); k >= 0 {
					name, rest = inner[:k], inner[k:]
				}
				if n, err := strconv.Atoi(name); err == nil {
					from := b.Len()
					switch {
					case strings.HasPrefix(rest, ":"):
						parse(rest[1:], depth+1)
					case strings.HasPrefix(rest, "|"):
						choices := strings.TrimSuffix(strings.TrimPrefix(rest, "|"), "|")
						first, _, _ := strings.Cut(choices, ",")
						b.WriteString(first)
					}
					if _, ok := stops[n]; !ok {
						stops[n] = stop{from, b.Len()}
					}
					continue
				}
				if v, ok := vars[name]; ok && v != "" {
					b.WriteString(v)
				} else if strings.HasPrefix(rest, ":") {
					parse(rest[1:], depth+1)
				}
			case c == '$' && i+1 < len(s) && (s[i+1] == '_' || s[i+1] >= 'A' && s[i+1] <= 'Z'):
				j := i + 1
				for j < len(s) && (s[j] == '_' || s[j] >= 'A' && s[j] <= 'Z' || s[j] >= '0' && s[j] <= '9') {
					j++
				}
				b.WriteString(vars[s[i+1:j]])
				i = j - 1
			default:
				b.WriteByte(c)
			}
		}
	}
	parse(body, 0)
	out := b.String()
	for _, n := range []int{1, 0} {
		if st, ok := stops[n]; ok {
			return out, [2]int{st.from, st.to}
		}
	}
	return out, [2]int{len(out), len(out)}
}

// matchBrace is where the brace that closes the one at open is, -1 for
// none.
func matchBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
