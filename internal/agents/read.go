package agents

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// headMax is as much of a transcript as is read from its start, for
// where it ran and what was asked first, and tailMax as much from its
// end, for the title it was given last. A line is read whole, however
// long, up to lineMax.
const (
	headMax = 256 << 10
	tailMax = 256 << 10
	lineMax = 16 << 20
)

// promptMax is how many characters of the first prompt are kept.
const promptMax = 200

// transcripts are the transcripts of a projects folder of Claude Code or
// Qoder: a folder for each project, a file for each session in it. The
// transcripts of a session's helpers, deeper, are not sessions.
func transcripts(dir string) []string {
	projects, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(dir, p.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".jsonl") {
				out = append(out, filepath.Join(dir, p.Name(), f.Name()))
			}
		}
	}
	return out
}

// eachLine calls fn on each line of r, whole up to lineMax, those longer
// passed over, until fn returns false or more than max was read.
func eachLine(r io.Reader, max int, fn func([]byte) bool) {
	br := bufio.NewReaderSize(r, 64<<10)
	read := 0
	var long []byte
	skip := false
	for read < max {
		chunk, err := br.ReadSlice('\n')
		read += len(chunk)
		if err == bufio.ErrBufferFull {
			if skip = skip || len(long)+len(chunk) > lineMax; !skip {
				long = append(long, chunk...)
			}
			continue
		}
		line := chunk
		if long != nil {
			line = append(long, chunk...)
			long = nil
		}
		if skip {
			skip = false
		} else if len(bytes.TrimSpace(line)) > 0 && !fn(line) {
			return
		}
		if err != nil {
			return
		}
	}
}

// tail calls fn on each whole line of the last max bytes of f.
func tail(f *os.File, size int64, max int, fn func([]byte)) {
	from := size - int64(max)
	partial := from > 0
	if from < 0 {
		from = 0
	}
	buf := make([]byte, size-from)
	if _, err := f.ReadAt(buf, from); err != nil && err != io.EOF {
		return
	}
	if partial {
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
		} else {
			return
		}
	}
	for line := range bytes.SplitSeq(buf, []byte{'\n'}) {
		if len(line) > 0 {
			fn(line)
		}
	}
}

var (
	userMark  = []byte(`"user"`)
	titleMark = []byte(`itle"`)
)

// readTranscript reads a transcript of Claude Code or Qoder: its session
// is named after the file, and its lines say where it ran, what the user
// asked and what it is titled. A title the user gave wins over the
// agent's.
func readTranscript(path string, h Harness) (Session, bool) {
	s := Session{Harness: h, ID: strings.TrimSuffix(filepath.Base(path), ".jsonl")}
	f, err := os.Open(path)
	if err != nil {
		return s, false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return s, false
	}
	var custom, ai, summary, last string
	take := func(line []byte) {
		if !bytes.Contains(line, titleMark) && !bytes.Contains(line, []byte(`"summary"`)) && !bytes.Contains(line, []byte(`"lastPrompt"`)) {
			return
		}
		var v struct {
			Type, CustomTitle, AiTitle, Summary, LastPrompt string
		}
		if json.Unmarshal(line, &v) != nil {
			return
		}
		switch v.Type {
		case "custom-title":
			custom = cmpOr(v.CustomTitle, custom)
		case "ai-title":
			ai = cmpOr(v.AiTitle, ai)
		case "summary":
			summary = cmpOr(v.Summary, summary)
		case "last-prompt":
			last = cmpOr(v.LastPrompt, last)
		}
	}
	eachLine(f, headMax, func(line []byte) bool {
		take(line)
		if s.Dir == "" || s.Prompt == "" {
			readTranscriptLine(line, &s)
		}
		return true
	})
	if fi.Size() > headMax {
		tail(f, fi.Size(), tailMax, take)
	}
	s.Title = cmpOr(custom, cmpOr(ai, summary))
	if s.Prompt == "" {
		s.Prompt = oneLine(last)
	}
	// A transcript of nothing asked yet is no session to go back to.
	return s, s.Prompt != "" || s.Title != ""
}

// readTranscriptLine takes from a line of a transcript where it ran, and
// the first thing the user asked: not a line the agent made, a command's
// or a tool's result.
func readTranscriptLine(line []byte, s *Session) {
	if !bytes.Contains(line, []byte(`"cwd"`)) && !bytes.Contains(line, []byte(`"directories"`)) && !bytes.Contains(line, userMark) {
		return
	}
	var v struct {
		Type        string
		Cwd         string
		Directories []string
		IsMeta      bool
		IsSidechain bool
		Message     struct {
			Role    string
			Content json.RawMessage
		}
	}
	if json.Unmarshal(line, &v) != nil {
		return
	}
	if s.Dir == "" {
		if v.Cwd != "" {
			s.Dir = v.Cwd
		} else if v.Type == "workspace-directories" && len(v.Directories) > 0 {
			s.Dir = v.Directories[0]
		}
	}
	if s.Prompt == "" && v.Type == "user" && !v.IsMeta && !v.IsSidechain {
		s.Prompt = promptOf(v.Message.Content)
	}
}

// promptOf is what a user's message says, on one line, "" for one of
// tags, as a command's or the context an agent adds, or of no text.
func promptOf(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) != nil {
		var parts []struct{ Type, Text string }
		if json.Unmarshal(content, &parts) != nil {
			return ""
		}
		for _, p := range parts {
			if (p.Type == "text" || p.Type == "input_text") && strings.TrimSpace(p.Text) != "" && !tagged(p.Text) {
				text = p.Text
				break
			}
		}
	}
	if tagged(text) {
		return ""
	}
	return oneLine(text)
}

// tagged reports whether text starts with a tag, as what an agent adds
// to a message does.
func tagged(text string) bool { return strings.HasPrefix(strings.TrimSpace(text), "<") }

// oneLine is text on one line, cut to promptMax characters.
func oneLine(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > promptMax {
		text = string(r[:promptMax]) + "…"
	}
	return text
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// codexRollouts are Codex's transcripts, in folders of the year, month
// and day.
func codexRollouts(dir string) []string {
	var out []string
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasPrefix(d.Name(), "rollout-") && strings.HasSuffix(d.Name(), ".jsonl") {
			out = append(out, path)
		}
		return nil
	})
	return out
}

// readCodex reads a transcript of Codex: its first line says the session
// and where it ran, and its first message of the user what was asked.
// Sessions of its exec mode are not for resuming.
func readCodex(path string) (Session, bool) {
	s := Session{Harness: Codex}
	f, err := os.Open(path)
	if err != nil {
		return s, false
	}
	defer f.Close()
	exec := false
	eachLine(f, headMax, func(line []byte) bool {
		var v struct {
			Type    string
			Payload struct {
				Type       string
				ID         string
				Cwd        string
				Originator string
				Source     string
				Role       string
				Content    json.RawMessage
			}
		}
		if json.Unmarshal(line, &v) != nil {
			return true
		}
		switch {
		case v.Type == "session_meta":
			s.ID, s.Dir = v.Payload.ID, v.Payload.Cwd
			exec = v.Payload.Source == "exec" || v.Payload.Originator == "codex_exec"
		case v.Type == "response_item" && v.Payload.Type == "message" && v.Payload.Role == "user":
			s.Prompt = promptOf(v.Payload.Content)
		}
		return s.ID == "" || s.Prompt == ""
	})
	return s, s.ID != "" && !exec
}

// readCodexIndex reads Codex's names of its sessions, the last of each.
func readCodexIndex(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	titles := map[string]string{}
	eachLine(f, 64<<20, func(line []byte) bool {
		var v struct {
			ID         string
			ThreadName string `json:"thread_name"`
		}
		if json.Unmarshal(line, &v) == nil && v.ID != "" && v.ThreadName != "" {
			titles[v.ID] = v.ThreadName
		}
		return true
	})
	return titles
}

// piSessions are Pi's transcripts: a folder for each folder it ran in,
// a file for each session in it.
func piSessions(dir string) []string {
	var out []string
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".jsonl") {
			out = append(out, path)
		}
		return nil
	})
	return out
}

// readPi reads a transcript of Pi: its first line says the session and
// where it ran, its first message of the user what was asked, and its
// lines of session_info what it is named, the last of them winning.
func readPi(path string) (Session, bool) {
	s := Session{Harness: Pi}
	f, err := os.Open(path)
	if err != nil {
		return s, false
	}
	defer f.Close()
	eachLine(f, headMax, func(line []byte) bool {
		var v struct {
			Type    string
			ID      string
			Cwd     string
			Name    string
			Message struct {
				Role    string
				Content json.RawMessage
			}
		}
		if json.Unmarshal(line, &v) != nil {
			return true
		}
		switch v.Type {
		case "session":
			s.ID, s.Dir = v.ID, v.Cwd
		case "session_info":
			if v.Name != "" {
				s.Title = v.Name
			}
		case "message":
			if s.Prompt == "" && v.Message.Role == "user" {
				s.Prompt = promptOf(v.Message.Content)
			}
		}
		return s.ID == "" || s.Prompt == "" || s.Title == ""
	})
	return s, s.ID != "" && (s.Prompt != "" || s.Title != "")
}

// grokSummaries are Grok's summaries of its sessions: a folder for each
// folder it ran in, named by its path escaped, and one for each session
// in it.
func grokSummaries(dir string) []string {
	dirs, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		sessions, err := os.ReadDir(filepath.Join(dir, d.Name()))
		if err != nil {
			continue
		}
		for _, s := range sessions {
			if s.IsDir() {
				out = append(out, filepath.Join(dir, d.Name(), s.Name(), "summary.json"))
			}
		}
	}
	return out
}

// readGrok reads the summary of a session of Grok. Its sessions run
// headless, as scheduled, and those of its helpers are not for resuming.
// It is titled by the summary, as its first prompt is in tags of its own.
func readGrok(path string) (Session, bool) {
	s := Session{Harness: Grok}
	b, err := os.ReadFile(path)
	if err != nil {
		return s, false
	}
	var v struct {
		Info struct {
			ID, Cwd string
		}
		SessionSummary string    `json:"session_summary"`
		GeneratedTitle string    `json:"generated_title"`
		SessionKind    string    `json:"session_kind"`
		UpdatedAt      time.Time `json:"updated_at"`
		LastActiveAt   time.Time `json:"last_active_at"`
	}
	if json.Unmarshal(b, &v) != nil {
		return s, false
	}
	s.ID, s.Dir = v.Info.ID, v.Info.Cwd
	if s.Dir == "" {
		if d, err := url.PathUnescape(filepath.Base(filepath.Dir(filepath.Dir(path)))); err == nil {
			s.Dir = d
		}
	}
	s.Title = oneLine(cmpOr(v.GeneratedTitle, v.SessionSummary))
	s.Updated = v.LastActiveAt
	if s.Updated.IsZero() {
		s.Updated = v.UpdatedAt
	}
	// One it has not titled yet is of nothing asked.
	return s, s.ID != "" && s.Title != "" && v.SessionKind != "headless" && v.SessionKind != "subagent"
}

// ClaudeSessionOfPID is the session that the process pid of Claude Code
// runs, as it writes in sessions/<pid>.json of its config folder, and the
// folder: false when no folder of roots has it.
func ClaudeSessionOfPID(roots []string, pid int) (id, root string, ok bool) {
	if pid <= 0 {
		return "", "", false
	}
	for _, r := range roots {
		b, err := os.ReadFile(filepath.Join(r, "sessions", strconv.Itoa(pid)+".json"))
		if err != nil {
			continue
		}
		var v struct {
			PID       int
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(b, &v) == nil && v.SessionID != "" && (v.PID == 0 || v.PID == pid) {
			return v.SessionID, r, true
		}
	}
	return "", "", false
}
