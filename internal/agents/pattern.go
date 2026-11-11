package agents

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Pattern is where an agent of no reader of its own keeps its sessions,
// as a plugin declares it: the files Glob matches, a session each, of
// JSON (Format "json") or of a JSON object a line ("jsonl"), and where in
// them each field is, as a path of keys and array indexes by dots, as
// "meta.cwd" or "messages.0.content". A file's id is its name without its
// extension unless ID names one, and its time the file's unless Updated
// does. Of a file of lines, each field is the first line's that has it,
// but for the title and the time, the last's of its start or its end.
type Pattern struct {
	Glob, Format                       string
	ID, Title, Folder, Updated, Prompt string
}

// patternMax is how many files a Pattern matches, at most.
const patternMax = 20000

// WithPattern is ag with its sessions listed by p.
func WithPattern(ag Agent, p Pattern) Agent {
	h := ag.ID
	ag.Files = func(e *Env) []File { return files(p.files(e)) }
	ag.Read = func(path string) (Session, bool) { return p.read(path, h) }
	return ag
}

// files are the files the glob matches, ~ the home folder, ** any folders.
func (p Pattern) files(e *Env) []string {
	g := p.Glob
	if rest, ok := strings.CutPrefix(g, "~"); ok && (rest == "" || rest[0] == '/' || rest[0] == filepath.Separator) {
		g = e.Home + rest
	}
	if !filepath.IsAbs(g) {
		return nil
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(g)), "/")
	i := 0
	for i < len(parts) && !strings.ContainsAny(parts[i], "*?[") {
		i++
	}
	root := strings.Join(parts[:i], "/")
	if root == "" {
		root = "/"
	}
	root = filepath.FromSlash(root)
	if i == len(parts) {
		if fi, err := os.Stat(root); err == nil && !fi.IsDir() {
			return []string{root}
		}
		return nil
	}
	pat := parts[i:]
	var out []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		switch {
		case len(out) >= patternMax:
			return filepath.SkipAll
		case err != nil && d != nil && d.IsDir():
			return filepath.SkipDir
		case err != nil:
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		names := strings.Split(filepath.ToSlash(rel), "/")
		if d.IsDir() {
			if !matchPrefix(pat, names) {
				return filepath.SkipDir
			}
			return nil
		}
		if matchParts(pat, names) {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// matchParts reports whether the names of a path match the parts of a
// glob, ** any of them.
func matchParts(pat, names []string) bool {
	if len(pat) == 0 {
		return len(names) == 0
	}
	if pat[0] == "**" {
		for k := 0; k <= len(names); k++ {
			if matchParts(pat[1:], names[k:]) {
				return true
			}
		}
		return false
	}
	if len(names) == 0 {
		return false
	}
	ok, _ := path.Match(pat[0], names[0])
	return ok && matchParts(pat[1:], names[1:])
}

// matchPrefix reports whether a folder of names may hold what the glob
// matches.
func matchPrefix(pat, names []string) bool {
	for i, n := range names {
		if i >= len(pat) {
			return false
		}
		if pat[i] == "**" {
			return true
		}
		if ok, _ := path.Match(pat[i], n); !ok {
			return false
		}
	}
	return true
}

// read reads a session of a file the glob matched.
func (p Pattern) read(file string, h Harness) (Session, bool) {
	s := Session{Harness: h}
	var id, folder, prompt, title, updated json.RawMessage
	take := func(v map[string]json.RawMessage, first bool) {
		get := func(field string, into *json.RawMessage, first bool) {
			if field == "" || (first && *into != nil) {
				return
			}
			if raw := pick(v, field); raw != nil && string(raw) != "null" && string(raw) != `""` {
				*into = raw
			}
		}
		get(p.ID, &id, first)
		get(p.Folder, &folder, first)
		get(p.Prompt, &prompt, first)
		get(p.Title, &title, false)
		get(p.Updated, &updated, false)
	}
	f, err := os.Open(file)
	if err != nil {
		return s, false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return s, false
	}
	switch p.Format {
	case "jsonl":
		line := func(b []byte) {
			if b = bytes.TrimSpace(b); len(b) == 0 || b[0] != '{' {
				return
			}
			var v map[string]json.RawMessage
			if json.Unmarshal(b, &v) == nil {
				take(v, true)
			}
		}
		eachLine(f, headMax, func(b []byte) bool { line(b); return true })
		if fi.Size() > headMax {
			tail(f, fi.Size(), tailMax, line)
		}
	default:
		if fi.Size() > lineMax {
			return s, false
		}
		b, err := os.ReadFile(file)
		if err != nil {
			return s, false
		}
		var v map[string]json.RawMessage
		if json.Unmarshal(b, &v) != nil {
			return s, false
		}
		take(v, true)
	}
	s.ID = scalar(id)
	if s.ID == "" && p.ID == "" {
		s.ID = strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	}
	s.Dir = scalar(folder)
	s.Title = oneLine(scalar(title))
	if prompt != nil {
		s.Prompt = promptOf(prompt)
	}
	s.Updated = timeOf(updated)
	return s, s.ID != ""
}

// pick is the value at a path of keys and indexes by dots in v, nil for
// none.
func pick(v map[string]json.RawMessage, field string) json.RawMessage {
	key, rest, more := strings.Cut(field, ".")
	raw := v[key]
	for more && raw != nil {
		key, rest, more = strings.Cut(rest, ".")
		switch {
		case len(raw) > 0 && raw[0] == '{':
			var m map[string]json.RawMessage
			if json.Unmarshal(raw, &m) != nil {
				return nil
			}
			raw = m[key]
		case len(raw) > 0 && raw[0] == '[':
			var a []json.RawMessage
			i, err := strconv.Atoi(key)
			if json.Unmarshal(raw, &a) != nil || err != nil || i < 0 || i >= len(a) {
				return nil
			}
			raw = a[i]
		default:
			return nil
		}
	}
	return raw
}

// scalar is a string or a number of JSON as text, "" for another value.
func scalar(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

// timeOf is a time of JSON: RFC 3339 text, or a number of seconds since
// 1970, or of milliseconds once it is that large; zero for none.
func timeOf(raw json.RawMessage) time.Time {
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		if n > 1e12 {
			return time.UnixMilli(int64(n))
		}
		return time.Unix(int64(n), 0)
	}
	if t, err := time.Parse(time.RFC3339Nano, scalar(raw)); err == nil {
		return t
	}
	return time.Time{}
}

// SessionOf is a session as a plugin's function gives it: its id a
// string or a number of JSON, its time as timeOf reads it, and its title
// and prompt each on one line.
func SessionOf(id json.RawMessage, title, dir string, updated json.RawMessage, prompt string) Session {
	return Session{ID: scalar(id), Title: oneLine(title), Dir: dir, Updated: timeOf(updated), Prompt: oneLine(prompt)}
}
