// Package claudetheme has Claude Code use a theme Mayoi makes: it writes
// the theme to themes/mayoi.json in a config folder of Claude Code and
// chooses it in that folder's settings.json, and takes that back.
//
// What it did is kept in a state file of Mayoi's own, written before
// Claude Code's files, so that a change cut short, by a crash or a quit,
// is finished or taken back the next time. It takes back only what is
// still its own: a theme chosen since in Claude Code, with /theme, stays,
// and so does a theme file Mayoi did not write.
package claudetheme

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Slug is the name of Mayoi's theme among Claude Code's, and Selection
// how settings.json chooses it.
const (
	Slug      = "mayoi"
	Selection = "custom:" + Slug
)

// Theme is a custom theme of Claude Code, as its themes/<slug>.json has
// it: a base theme and the colors that differ from it.
type Theme struct {
	Name      string            `json:"name"`
	Base      string            `json:"base"`
	Overrides map[string]string `json:"overrides"`
}

// Store is a config folder of Claude Code, and the file where Mayoi keeps
// what it changed there.
type Store struct {
	ConfigDir string
	StatePath string
}

// state is what Mayoi changed in a config folder of Claude Code.
type state struct {
	ConfigDir string `json:"configDir"`
	// Had is whether settings.json chose a theme before Mayoi's, and
	// Previous that theme.
	Had      bool            `json:"had,omitempty"`
	Previous json.RawMessage `json:"previous,omitempty"`
	// Selected is whether Mayoi chose its theme in settings.json.
	Selected bool `json:"selected,omitempty"`
	// Hash is the SHA-256 of the theme file Mayoi wrote last, and Next of
	// the one it is writing, until it has.
	Hash string `json:"hash,omitempty"`
	Next string `json:"next,omitempty"`
	// Lost is whether a theme was chosen in Claude Code since, which Mayoi
	// leaves as it is.
	Lost bool `json:"lost,omitempty"`
}

// Result is what Apply found.
type Result struct {
	// Previous is the theme settings.json chose before Mayoi's, "" for
	// none, which turning the sync off chooses again.
	Previous string
	// Lost is whether another theme was chosen in Claude Code since, which
	// Mayoi left as it is.
	Lost bool
}

// ErrMalformed is a settings.json that is not a JSON object, which Mayoi
// leaves as it is.
var ErrMalformed = errors.New("not a JSON object")

// TakenError is a theme file of Mayoi's name that Mayoi did not write.
type TakenError struct{ Path string }

func (e *TakenError) Error() string { return e.Path + " is not Mayoi's" }

// MalformedError is a settings.json Mayoi cannot read.
type MalformedError struct{ Path string }

func (e *MalformedError) Error() string { return e.Path + ": " + ErrMalformed.Error() }

func (e *MalformedError) Unwrap() error { return ErrMalformed }

func (s Store) settingsPath() string { return filepath.Join(s.ConfigDir, "settings.json") }
func (s Store) themePath() string    { return filepath.Join(s.ConfigDir, "themes", Slug+".json") }

// Apply writes the theme build makes and chooses it, unless another
// theme was chosen in Claude Code since Mayoi chose its own. build is
// given the theme chosen before Mayoi's, "" for none.
func (s Store) Apply(build func(previous string) Theme) (Result, error) {
	st, err := s.load()
	if err != nil {
		return Result{}, err
	}
	// Mayoi chose its theme in another folder, as CLAUDE_CONFIG_DIR
	// changed: that one is taken back first.
	if st != nil && filepath.Clean(st.ConfigDir) != filepath.Clean(s.ConfigDir) {
		if err := (Store{ConfigDir: st.ConfigDir, StatePath: s.StatePath}).Restore(); err != nil {
			return Result{}, err
		}
		st = nil
	}
	data, err := readSettings(s.settingsPath())
	if err != nil {
		return Result{}, err
	}
	cur, has, err := topKey(data, "theme")
	if err != nil {
		return Result{}, &MalformedError{s.settingsPath()}
	}
	ours := has && isSelection(cur)
	if st != nil && (st.Lost || st.Selected) && !ours {
		if !st.Lost {
			st.Lost = true
			if err := s.save(st); err != nil {
				return Result{}, err
			}
		}
		return Result{Previous: themeName(st.Previous), Lost: true}, nil
	}
	fresh := st == nil
	if fresh {
		st = &state{ConfigDir: s.ConfigDir}
		// A theme of Mayoi's chosen before, of which Mayoi kept nothing,
		// is none to go back to.
		if has && !ours {
			st.Had, st.Previous = true, cur
		}
	}
	st.Lost = false
	t := build(themeName(st.Previous))
	file, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return Result{}, err
	}
	file = append(file, '\n')
	sum := hash(file)
	old, err := os.ReadFile(s.themePath())
	switch {
	case errors.Is(err, fs.ErrNotExist):
		old = nil
	case err != nil:
		return Result{}, err
	case hash(old) != sum && (fresh || hash(old) != st.Hash && hash(old) != st.Next):
		return Result{}, &TakenError{s.themePath()}
	}
	st.Next = sum
	if err := s.save(st); err != nil {
		return Result{}, err
	}
	if !bytes.Equal(old, file) {
		if err := os.MkdirAll(filepath.Dir(s.themePath()), 0o755); err != nil {
			return Result{}, err
		}
		if err := writeAtomic(s.themePath(), file); err != nil {
			return Result{}, err
		}
	}
	if !ours {
		out, err := setTopKey(data, "theme", []byte(`"`+Selection+`"`))
		if err != nil {
			return Result{}, &MalformedError{s.settingsPath()}
		}
		if err := writeAtomic(s.settingsPath(), out); err != nil {
			return Result{}, err
		}
	}
	st.Selected, st.Hash, st.Next = true, sum, ""
	if err := s.save(st); err != nil {
		return Result{}, err
	}
	return Result{Previous: themeName(st.Previous)}, nil
}

// Restore chooses again the theme settings.json chose before Mayoi's, if
// Mayoi's is still chosen, and removes Mayoi's theme file if it is still
// as Mayoi wrote it. It does nothing when Mayoi changed nothing.
func (s Store) Restore() error {
	st, err := s.load()
	if err != nil || st == nil {
		return err
	}
	settings := filepath.Join(st.ConfigDir, "settings.json")
	data, err := readSettings(settings)
	if err != nil {
		return err
	}
	cur, has, err := topKey(data, "theme")
	if err != nil {
		return &MalformedError{settings}
	}
	if has && isSelection(cur) {
		var out []byte
		if st.Had {
			out, err = setTopKey(data, "theme", st.Previous)
		} else {
			out, err = setTopKey(data, "theme", nil)
		}
		if err != nil {
			return &MalformedError{settings}
		}
		if err := writeAtomic(settings, out); err != nil {
			return err
		}
	}
	theme := filepath.Join(st.ConfigDir, "themes", Slug+".json")
	if old, err := os.ReadFile(theme); err == nil {
		if h := hash(old); h == st.Hash || h == st.Next {
			if err := os.Remove(theme); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Remove(s.StatePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// load reads the state file, nil when there is none.
func (s Store) load() (*state, error) {
	data, err := os.ReadFile(s.StatePath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st state
	if err := json.Unmarshal(data, &st); err != nil || st.ConfigDir == "" {
		// A state file cut short holds nothing to take back.
		return nil, nil
	}
	return &st, nil
}

func (s Store) save(st *state) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.StatePath), 0o755); err != nil {
		return err
	}
	return writeAtomic(s.StatePath, append(data, '\n'))
}

// readSettings reads settings.json, "{}" when there is none.
func readSettings(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []byte("{}\n"), nil
	}
	return data, err
}

func isSelection(raw json.RawMessage) bool {
	var v string
	return json.Unmarshal(raw, &v) == nil && v == Selection
}

// themeName is a theme of settings.json as a string, "" for none.
func themeName(raw json.RawMessage) string {
	var v string
	json.Unmarshal(raw, &v)
	return v
}

func hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// writeAtomic writes data to a file beside path and renames it to path, so
// that path is never half written, with path's mode when it has one.
func writeAtomic(path string, data []byte) error {
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".mayoi-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(mode)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// member is a key of a JSON object at its top level: where the member
// starts, where its value starts and ends, and where the value before it
// ends, or the object's opening brace.
type member struct {
	key                           string
	prevEnd, start, valStart, end int
}

// members reads the members of the top level of a JSON object.
func members(data []byte) (open, closing int, ms []member, err error) {
	if !json.Valid(data) {
		return 0, 0, nil, ErrMalformed
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if d, ok := tok.(json.Delim); err != nil || !ok || d != '{' {
		return 0, 0, nil, ErrMalformed
	}
	open = int(dec.InputOffset())
	prev := open
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return 0, 0, nil, ErrMalformed
		}
		key, _ := tok.(string)
		keyEnd := int(dec.InputOffset())
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return 0, 0, nil, ErrMalformed
		}
		end := int(dec.InputOffset())
		start := skip(data, prev, " \t\r\n,")
		valStart := skip(data, keyEnd, " \t\r\n:")
		ms = append(ms, member{key: key, prevEnd: prev, start: start, valStart: valStart, end: end})
		prev = end
	}
	closing = bytes.LastIndexByte(data, '}')
	return open, closing, ms, nil
}

func skip(data []byte, i int, chars string) int {
	for i < len(data) && strings.IndexByte(chars, data[i]) >= 0 {
		i++
	}
	return i
}

// topKey returns the value of key at the top level of the JSON object of
// data, and whether it has one.
func topKey(data []byte, key string) (json.RawMessage, bool, error) {
	_, _, ms, err := members(data)
	if err != nil {
		return nil, false, err
	}
	for i := len(ms) - 1; i >= 0; i-- {
		if m := ms[i]; m.key == key {
			return json.RawMessage(data[m.valStart:m.end]), true, nil
		}
	}
	return nil, false, nil
}

// setTopKey returns data with the value of key at the top level of its
// JSON object val, or without key for nil, the rest of it, its other keys,
// their order and its spacing, as it was.
func setTopKey(data []byte, key string, val []byte) ([]byte, error) {
	open, closing, ms, err := members(data)
	if err != nil {
		return nil, err
	}
	at := -1
	for i, m := range ms {
		if m.key == key {
			at = i
		}
	}
	var b bytes.Buffer
	switch {
	case at >= 0 && val != nil:
		m := ms[at]
		b.Write(data[:m.valStart])
		b.Write(val)
		b.Write(data[m.end:])
	case at >= 0:
		m := ms[at]
		switch {
		case at > 0:
			b.Write(data[:m.prevEnd])
			b.Write(data[m.end:])
		case len(ms) > 1:
			b.Write(data[:m.start])
			b.Write(data[ms[1].start:])
		default:
			b.Write(data[:open])
			b.Write(data[closing:])
		}
	case val == nil:
		return data, nil
	case len(ms) == 0:
		b.Write(data[:open])
		fmt.Fprintf(&b, "\n  %q: %s\n", key, val)
		b.Write(data[closing:])
	default:
		// After the last member, indented as it is.
		last := ms[len(ms)-1]
		nl := bytes.LastIndexByte(data[:last.start], '\n')
		b.Write(data[:last.end])
		if nl < 0 {
			fmt.Fprintf(&b, ", %q: %s", key, val)
		} else {
			fmt.Fprintf(&b, ",\n%s%q: %s", data[nl+1:last.start], key, val)
		}
		b.Write(data[last.end:])
	}
	return b.Bytes(), nil
}
