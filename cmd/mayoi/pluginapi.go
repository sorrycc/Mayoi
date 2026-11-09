package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/cli"
	"mayoi/internal/mon"
	"mayoi/internal/plugin"
	"mayoi/internal/remote"
	"mayoi/internal/vfs"
	"mayoi/internal/workspace"
	"mayoi/locales"
)

// The calls plugins make to the app: plugin.Host's Call. Each runs on a
// goroutine of its own and reaches the app on its main thread.

// position is a place in a document as plugins see it: a line and a
// column in UTF-16 code units, both from 0, as LSP has them.
type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type textRange struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

type selectionJSON struct {
	Anchor position `json:"anchor"`
	Active position `json:"active"`
}

// runeAt is the rune offset of a position in snap, clamped to the text.
func posRune(snap ui.TextSnapshot, p position) int {
	n := snap.LineCount()
	if p.Line < 0 {
		return 0
	}
	if p.Line >= n {
		return snap.Len()
	}
	start, end := snap.LineRange(p.Line)
	line := snap.Line(p.Line)
	units, runes := 0, 0
	for _, r := range line {
		if units >= p.Character {
			break
		}
		units += utf16.RuneLen(r)
		if units < 0 { // a rune utf16 cannot take
			units += 2
		}
		runes++
	}
	return min(start+runes, end)
}

// positionOf is the position of a rune offset of snap.
func positionOf(snap ui.TextSnapshot, at int) position {
	at = min(max(at, 0), snap.Len())
	line := snap.LineAt(at)
	start, _ := snap.LineRange(line)
	text := snap.Slice(start, at)
	units := 0
	for _, r := range text {
		if n := utf16.RuneLen(r); n > 0 {
			units += n
		} else {
			units += 2
		}
	}
	return position{line, units}
}

// docJSON describes a document to a plugin.
func docJSON(d *document) map[string]any {
	snap := d.buf.Snapshot()
	return map[string]any{
		"id": d.id, "path": d.path, "name": d.name(), "language": languageOf(d),
		"version": snap.Version(), "dirty": d.dirty(), "untitled": d.path == "", "lineCount": snap.LineCount(),
	}
}

// editorJSON describes an editor, numbering it for the calls that name it.
func (h *pluginHost) editorJSON(d *editor) map[string]any {
	if d == nil {
		return nil
	}
	id, ok := h.edIDs[d]
	if !ok {
		h.edNext++
		id = h.edNext
		h.edIDs[d] = id
	}
	snap := d.buf.Snapshot()
	sel := d.sel
	if d.pending != nil {
		sel = *d.pending
	}
	return map[string]any{
		"id":         id,
		"document":   docJSON(d.document),
		"selections": []selectionJSON{{Anchor: positionOf(snap, sel[0]), Active: positionOf(snap, sel[1])}},
	}
}

// editorByID finds an editor a plugin was told of, still open in a.
func (h *pluginHost) editorByID(a *app, id int) (*editor, error) {
	for _, col := range a.cols {
		for _, t := range col.tabs {
			if t.ed != nil && h.edIDs[t.ed] == id {
				return t.ed, nil
			}
		}
	}
	// Forget the editors that closed.
	for d := range h.edIDs {
		if !slices.ContainsFunc(a.cols, func(col *column) bool {
			return slices.ContainsFunc(col.tabs, func(t *colTab) bool { return t.ed == d })
		}) {
			delete(h.edIDs, d)
		}
	}
	return nil, errors.New("the editor is closed")
}

// docByID finds an open document of a.
func docByID(a *app, id int) (*document, error) {
	for _, d := range a.docs {
		if d.id == id {
			return d, nil
		}
	}
	return nil, errors.New("the document is closed")
}

// pluginPath makes a path a plugin gives absolute, from the folder in front.
func pluginPath(a *app, p string) (string, error) {
	if p == "" {
		return "", errors.New("no path")
	}
	if remote.Is(p) || filepath.IsAbs(p) {
		return p, nil
	}
	if a.root == "" {
		return "", errors.New("no folder is open for a relative path")
	}
	if host, root, ok := remote.Split(a.root); ok {
		return remote.Path(host, path.Join(root, filepath.ToSlash(p))), nil
	}
	return filepath.Join(a.root, p), nil
}

// Call implements plugin.Host.
func (h *pluginHost) Call(p *plugin.Plugin, method string, raw json.RawMessage) (any, error) {
	var args struct {
		ID     string     `json:"id"`
		Arg    string     `json:"arg"`
		Doc    int        `json:"doc"`
		Editor int        `json:"editor"`
		Line   int        `json:"line"`
		Column int        `json:"column"`
		Range  *textRange `json:"range"`
		Edits  []struct {
			Range textRange `json:"range"`
			Text  string    `json:"text"`
		} `json:"edits"`
		Version     uint64          `json:"version"`
		Select      []selectionJSON `json:"select"`
		Selections  []selectionJSON `json:"selections"`
		Text        string          `json:"text"`
		Title       string          `json:"title"`
		Language    string          `json:"language"`
		Path        string          `json:"path"`
		Glob        string          `json:"glob"`
		Limit       int             `json:"limit"`
		Message     string          `json:"message"`
		Kind        string          `json:"kind"`
		Action      string          `json:"action"`
		Value       string          `json:"value"`
		Placeholder string          `json:"placeholder"`
		Done        bool            `json:"done"`
		Command     string          `json:"command"`
		Args        []string        `json:"args"`
		Cwd         string          `json:"cwd"`
		Input       *string         `json:"input"`
		Timeout     int             `json:"timeout"`
		Prompt      string          `json:"prompt"`
		System      string          `json:"system"`
		Items       json.RawMessage `json:"items"`
		Source      string          `json:"source"`
		Clear       bool            `json:"clear"`
		Item        json.RawMessage `json:"item"`
		Tree        json.RawMessage `json:"tree"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, fmt.Errorf("%s: %v", method, err)
		}
	}
	switch method {
	case "commands.run":
		if h.reg.CommandPlugin(args.ID) != nil {
			var arg any
			if args.Arg != "" {
				arg = args.Arg
			}
			res, err := h.reg.Run(args.ID, arg)
			if err != nil {
				return nil, err
			}
			return res, nil
		}
		return h.main(func(a *app) (any, error) {
			runs, err := a.cliCommands([]cli.Command{{ID: args.ID, Arg: args.Arg}}, a.root)
			if err != nil {
				return nil, err
			}
			for _, run := range runs {
				run()
			}
			return nil, nil
		})
	case "commands.list":
		return h.main(func(a *app) (any, error) {
			var out []map[string]string
			for _, c := range a.commands() {
				out = append(out, map[string]string{"id": c.id, "title": c.label})
			}
			return out, nil
		})
	case "editor.active":
		return h.main(func(a *app) (any, error) { return h.editorJSON(a.activeEditor()), nil })
	case "editor.visible":
		return h.main(func(a *app) (any, error) {
			out := []any{}
			for _, col := range a.cols {
				if t := col.tab(); t != nil && t.ed != nil {
					out = append(out, h.editorJSON(t.ed))
				}
			}
			return out, nil
		})
	case "document.text":
		return h.main(func(a *app) (any, error) {
			d, err := docByID(a, args.Doc)
			if err != nil {
				return nil, err
			}
			snap := d.buf.Snapshot()
			if args.Range == nil {
				return snap.String(), nil
			}
			s, e := posRune(snap, args.Range.Start), posRune(snap, args.Range.End)
			return snap.Slice(min(s, e), max(s, e)), nil
		})
	case "document.line":
		return h.main(func(a *app) (any, error) {
			d, err := docByID(a, args.Doc)
			if err != nil {
				return nil, err
			}
			snap := d.buf.Snapshot()
			if args.Line < 0 || args.Line >= snap.LineCount() {
				return nil, fmt.Errorf("no line %d", args.Line)
			}
			return snap.Line(args.Line), nil
		})
	case "document.save":
		return h.wait(func(a *app, done func(any, error)) {
			d, err := docByID(a, args.Doc)
			if err != nil {
				done(nil, err)
				return
			}
			a.saveDoc(d, func() { done(nil, nil) })
		})
	case "editor.edit", "editor.insert":
		return h.main(func(a *app) (any, error) {
			d, err := h.editorByID(a, args.Editor)
			if err != nil {
				return nil, err
			}
			if d.locked() {
				return false, nil
			}
			snap := d.buf.Snapshot()
			var edits []textEdit
			sel := d.sel
			if d.pending != nil {
				sel = *d.pending
			}
			if method == "editor.insert" {
				s, e := min(sel[0], sel[1]), max(sel[0], sel[1])
				edits = []textEdit{{s, e, args.Text}}
				at := s + utf8.RuneCountInString(args.Text)
				sel = [2]int{at, at}
			} else {
				if args.Version != 0 && args.Version != snap.Version() {
					return false, nil
				}
				for _, e := range args.Edits {
					s, en := posRune(snap, e.Range.Start), posRune(snap, e.Range.End)
					edits = append(edits, textEdit{min(s, en), max(s, en), e.Text})
				}
				if editsOverlap(edits) {
					return nil, errors.New("the edits overlap")
				}
				// The selection moves with the edits, unless they say where.
				moved := func(x int) int {
					for _, e := range sortedEdits(edits) {
						x = movedBy(x, e.start, e.end, utf8.RuneCountInString(e.text))
					}
					return x
				}
				sel = [2]int{moved(sel[0]), moved(sel[1])}
			}
			d.edit(sel, edits...)
			if len(args.Select) > 0 {
				after := d.buf.Snapshot()
				d.selectRange(posRune(after, args.Select[0].Anchor), posRune(after, args.Select[0].Active), false)
			}
			return true, nil
		})
	case "editor.select":
		return h.main(func(a *app) (any, error) {
			d, err := h.editorByID(a, args.Editor)
			if err != nil || len(args.Selections) == 0 {
				return nil, err
			}
			snap := d.buf.Snapshot()
			d.selectRange(posRune(snap, args.Selections[0].Anchor), posRune(snap, args.Selections[0].Active), false)
			return nil, nil
		})
	case "editor.reveal":
		return h.main(func(a *app) (any, error) {
			d, err := h.editorByID(a, args.Editor)
			if err != nil {
				return nil, err
			}
			snap := d.buf.Snapshot()
			s, _ := snap.LineRange(min(max(args.Line, 0), snap.LineCount()-1))
			d.selectRange(s, s, true)
			return nil, nil
		})
	case "workspace.root":
		return h.main(func(a *app) (any, error) { return a.root, nil })
	case "workspace.isRemote":
		return h.main(func(a *app) (any, error) { return remote.Is(a.root), nil })
	case "workspace.open":
		return h.main(func(a *app) (any, error) {
			p, err := pluginPath(a, args.Path)
			if err != nil {
				return nil, err
			}
			if args.Line > 0 {
				a.openFileAt(p, args.Line-1, true)
				if args.Column > 0 {
					a.goToLine(args.Line, args.Column)
				}
			} else {
				a.openPath(p, true)
			}
			return nil, nil
		})
	case "workspace.readFile", "workspace.writeFile", "workspace.stat":
		v, err := h.main(func(a *app) (any, error) { return pluginPath(a, args.Path) })
		if err != nil {
			return nil, err
		}
		p := v.(string)
		switch method {
		case "workspace.readFile":
			// An open document's text, unsaved edits and all.
			if text, ok := h.openText(p); ok {
				return text, nil
			}
			data, err := vfs.ReadFileLimit(p, 64<<20)
			if err != nil {
				return nil, err
			}
			return string(data), nil
		case "workspace.writeFile":
			_, err := vfs.WriteFile(p, []byte(args.Text), 0o644)
			return nil, err
		default:
			info, err := vfs.Stat(p)
			if err != nil {
				return nil, nil
			}
			return map[string]any{"isDir": info.IsDir(), "size": info.Size(), "mtime": info.ModTime().UnixMilli()}, nil
		}
	case "workspace.findFiles":
		// The files of quick open's index, listed first when they are not.
		for deadline := time.Now().Add(30 * time.Second); ; {
			ready, err := h.main(func(a *app) (any, error) {
				if a.root == "" {
					return true, nil
				}
				a.refreshIndex()
				return a.index != nil && !a.indexing, nil
			})
			if err != nil {
				return nil, err
			}
			if ready.(bool) || time.Now().After(deadline) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		return h.main(func(a *app) (any, error) {
			limit := args.Limit
			if limit <= 0 {
				limit = 1000
			}
			out := []string{}
			globs := workspace.ParseGlobs(args.Glob)
			for _, f := range a.index {
				if args.Glob == "" || globs.Match(f) {
					out = append(out, f)
					if len(out) >= limit {
						break
					}
				}
			}
			return out, nil
		})
	case "workspace.documents":
		return h.main(func(a *app) (any, error) {
			out := []any{}
			for _, d := range a.docs {
				out = append(out, docJSON(d))
			}
			return out, nil
		})
	case "window.notify":
		return h.wait(func(a *app, done func(any, error)) {
			msg := args.Message
			if args.Kind == "error" {
				a.fail("%s: %s", p.Title(), msg)
				done(nil, nil)
				return
			}
			if args.Action == "" {
				a.tell(note{message: msg})
				done(false, nil)
				return
			}
			a.tell(note{message: msg, label: args.Action, action: func() { done(true, nil) }})
			// A toast that goes unanswered.
			time.AfterFunc(20*time.Second, func() { done(false, nil) })
		})
	case "window.pick":
		var items []pickItem
		json.Unmarshal(args.Items, &items)
		return h.wait(func(a *app, done func(any, error)) {
			a.openPluginPicker(items, args.Placeholder, func(i int) {
				if i < 0 {
					done(nil, nil)
				} else {
					done(i, nil)
				}
			})
		})
	case "window.prompt":
		return h.wait(func(a *app, done func(any, error)) {
			title := args.Title
			if title == "" {
				title = p.Title()
			}
			a.prompt = &prompt{title: title, hint: args.Placeholder, value: args.Value, optional: true, selected: args.Value == "",
				done: func(v string) string {
					done(v, nil)
					return ""
				},
				cancel: func() { done(nil, nil) },
			}
		})
	case "window.progress":
		h.mu.Lock()
		if args.Done {
			if i := slices.Index(h.progress, args.Title); i >= 0 {
				h.progress = slices.Delete(slices.Clone(h.progress), i, i+1)
			}
		} else {
			h.progress = append(slices.Clip(h.progress), args.Title)
		}
		h.mu.Unlock()
		h.redraw()
		return nil, nil
	case "window.openText":
		return h.main(func(a *app) (any, error) {
			a.openGenerated(args.Title, args.Text)
			if args.Title == "" {
				if t := a.activeTab(); t != nil && t.ed != nil {
					t.ed.title = locales.UntitledName(t.ed.number)
				}
			}
			return nil, nil
		})
	case "process.run":
		v, err := h.main(func(a *app) (any, error) {
			if args.Cwd == "" {
				return a.root, nil
			}
			return pluginPath(a, args.Cwd)
		})
		if err != nil {
			return nil, err
		}
		return runProcess(v.(string), args.Command, args.Args, args.Input, args.Timeout)
	case "languages.diagnostics":
		h.setDiagnostics(p.Name, args.Path, args.Source, args.Clear, args.Items)
		return nil, nil
	case "status.set":
		h.setStatus(p, args.ID, args.Item)
		return nil, nil
	case "views.render":
		return nil, h.renderView(p, args.ID, args.Tree)
	case "views.reveal":
		return h.main(func(a *app) (any, error) {
			a.showPluginView(args.ID)
			return nil, nil
		})
	case "previews.refresh":
		h.refreshPreviews(args.ID)
		return nil, nil
	case "webview.open", "webview.post", "webview.close":
		return h.webviewCall(p, method, args.ID, args.Title, raw)
	case "ai.complete":
		v, err := h.main(func(a *app) (any, error) {
			client := a.host.ai()
			if client == nil {
				return nil, errors.New("sign in to Mayoi to use AI")
			}
			return client, nil
		})
		if err != nil {
			return nil, err
		}
		client := v.(*mon.Client)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		model := aiModel
		if p := h.prefsSnap.Load(); p != nil && strings.TrimSpace(p.AIModel) != "" {
			model = strings.TrimSpace(p.AIModel)
		}
		return client.StreamMessage(ctx, mon.MessageRequest{Model: model, MaxTokens: 4096, System: args.System,
			Messages: []mon.Message{{Role: "user", Content: args.Prompt}}}, func(string) {})
	}
	return nil, fmt.Errorf("unknown call %s", method)
}

// openText is the text of an open document of a path, unsaved edits and
// all, from any window.
func (h *pluginHost) openText(p string) (string, bool) {
	v, err := h.main(func(*app) (any, error) {
		for _, a := range h.apps() {
			for _, d := range a.docs {
				if d.path == p {
					return d.buf.Snapshot().String(), nil
				}
			}
		}
		return nil, nil
	})
	s, ok := v.(string)
	return s, err == nil && ok
}

// redraw draws every window anew, as what the plugins show changed.
func (h *pluginHost) redraw() {
	for _, a := range h.apps() {
		a.invalidate()
	}
}

func editsOverlap(edits []textEdit) bool {
	s := slices.Clone(edits)
	slices.SortFunc(s, func(a, b textEdit) int { return a.start - b.start })
	for i := 1; i < len(s); i++ {
		if s[i].start < s[i-1].end {
			return true
		}
	}
	return false
}

func sortedEdits(edits []textEdit) []textEdit {
	s := slices.Clone(edits)
	sortEdits(s)
	return s
}

// runProcess runs a program a plugin asks for in dir, on dir's machine.
func runProcess(dir, name string, args []string, input *string, timeoutMS int) (any, error) {
	if name == "" {
		return nil, errors.New("no command")
	}
	var stdin *strings.Reader
	if input != nil {
		stdin = strings.NewReader(*input)
	}
	var cmd *exec.Cmd
	var err error
	if stdin != nil {
		cmd, err = vfs.Command(dir, nil, stdin, name, args...)
	} else {
		cmd, err = vfs.Command(dir, nil, nil, name, args...)
	}
	if err != nil {
		return nil, err
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	timeout := time.Duration(timeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	select {
	case err = <-finished:
	case <-time.After(timeout):
		cmd.Process.Kill()
		<-finished
		return nil, fmt.Errorf("%s ran longer than %v", name, timeout)
	}
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return nil, err
		}
		code = ee.ExitCode()
	}
	return map[string]any{"code": code, "stdout": out.String(), "stderr": errb.String()}, nil
}
