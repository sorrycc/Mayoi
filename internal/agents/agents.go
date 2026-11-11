// Package agents reads the sessions coding agents keep on this machine:
// Claude Code, in each of its config folders, Qoder, Codex and Grok. It
// reads their files only, and of a transcript only its start and its end,
// as one can be gigabytes: where it ran and what was asked first, and the
// title it was given last.
package agents

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Harness is an agent whose sessions are read.
type Harness string

const (
	Claude Harness = "claude"
	Qoder  Harness = "qoder"
	Codex  Harness = "codex"
	Grok   Harness = "grok"
)

// Agent is what is known of an agent: its id, its names, its mark, and
// how one of its sessions is resumed. How its sessions are listed is
// Scan's, for now.
type Agent struct {
	ID Harness
	// Name is its name, for people, and App the name it gives itself for
	// machines, in what it says it does (OSC 7501) and in what hc says
	// runs in a terminal.
	Name, App string
	// Icon is its mark, the shapes of an SVG of a 24 by 24 view drawn in
	// strokes of the color of the text, as the app's icons are; "" for
	// the mark of an agent of no mark of its own.
	Icon string
	// Tint is the color of its mark in a dark window and in a light one,
	// as hex; "" for the color of quiet text.
	Tint [2]string
	// resume is the shell command that resumes s, in its folder.
	resume func(e *Env, s Session) string
}

// Known are the agents read, in the order they are named.
var Known = []Agent{
	{
		ID: Claude, Name: "Claude Code", App: "claude-code",
		// A spark of eight rays, the long ones upright.
		Icon: `<path d="M12 3v18"/><path d="M3 12h18"/><path d="m6.7 6.7 10.6 10.6"/><path d="m17.3 6.7-10.6 10.6"/>`,
		Tint: [2]string{"#d97757", "#c15f3c"},
		resume: func(e *Env, s Session) string {
			cmd := "claude --resume " + quote(s.ID)
			if s.Root != "" && filepath.Clean(s.Root) != filepath.Join(e.Home, ".claude") {
				cmd = "CLAUDE_CONFIG_DIR=" + quote(s.Root) + " " + cmd
			}
			return cmd
		},
	},
	{
		ID: Qoder, Name: "Qoder", App: "qodercli",
		// A Q, its tail through its ring.
		Icon:   `<circle cx="11.5" cy="11" r="8"/><path d="m13.5 14.5 6 6.5"/>`,
		Tint:   [2]string{"#2fd189", "#13985a"},
		resume: func(_ *Env, s Session) string { return "qodercli -r " + quote(s.ID) },
	},
	{
		ID: Codex, Name: "Codex", App: "codex",
		// A prompt in a hexagon.
		Icon:   `<path d="M12 2.5 20.5 7.25v9.5L12 21.5 3.5 16.75v-9.5Z"/><path d="m10 8.5 3.5 3.5-3.5 3.5"/>`,
		resume: func(_ *Env, s Session) string { return "codex resume " + quote(s.ID) },
	},
	{
		ID: Grok, Name: "Grok", App: "grok",
		// A ring, open where a slash runs through it.
		Icon:   `<path d="M17.5 6.2A8 8 0 1 0 19.8 13"/><path d="M20.5 3.5 9 15"/>`,
		resume: func(_ *Env, s Session) string { return "grok --resume " + quote(s.ID) },
	},
}

// Harnesses are the agents read, in the order they are named.
var Harnesses = func() []Harness {
	out := make([]Harness, len(Known))
	for i, ag := range Known {
		out[i] = ag.ID
	}
	return out
}()

// Agent is what is known of the agent h; of one not known, its id as its
// names and no mark.
func (h Harness) Agent() Agent {
	for _, ag := range Known {
		if ag.ID == h {
			return ag
		}
	}
	return Agent{ID: h, Name: string(h), App: string(h)}
}

// Name is the agent's name, for people.
func (h Harness) Name() string { return h.Agent().Name }

// App is the name the agent gives itself for machines.
func (h Harness) App() string { return h.Agent().App }

// HarnessOfApp is the agent of an app's name, false for another program.
func HarnessOfApp(app string) (Harness, bool) {
	for _, ag := range Known {
		if ag.App == app || string(ag.ID) == app {
			return ag.ID, true
		}
	}
	return "", false
}

// Session is a conversation of an agent that can be resumed.
type Session struct {
	Harness Harness
	// Root is the config folder of Claude Code it is of, "" for the
	// other agents.
	Root string
	ID   string
	// Title is what it was named, by the user or the agent, "" for none.
	Title string
	// Prompt is the first thing asked in it, on one line, "" for none.
	Prompt string
	// Dir is the folder it ran in, "" when it does not say.
	Dir     string
	Updated time.Time
}

// Label is what the session is called: its title, else what was first
// asked, else its id.
func (s Session) Label() string {
	switch {
	case s.Title != "":
		return s.Title
	case s.Prompt != "":
		return s.Prompt
	}
	return s.ID
}

// Key tells the session apart from those of other agents and folders.
func (s Session) Key() string { return string(s.Harness) + "\x00" + s.Root + "\x00" + s.ID }

// Matches reports whether every word of query is in the session's title,
// first prompt, folder or id, ignoring case.
func (s Session) Matches(query string) bool {
	return HasWords(s.Text(), strings.Fields(strings.ToLower(query)))
}

// Text is what a query looks in, in lower case: the session's title,
// first prompt, folder, id and agent. A list filtered key by key makes it
// once for each session.
func (s Session) Text() string {
	return strings.ToLower(s.Title + "\n" + s.Prompt + "\n" + s.Dir + "\n" + s.ID + "\n" + s.Harness.Name())
}

// HasWords reports whether text, a session's Text, has every one of
// words, in lower case.
func HasWords(text string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

// In reports whether the session ran in dir or a folder in it.
func (s Session) In(dir string) bool {
	if s.Dir == "" || dir == "" {
		return false
	}
	d, sd := filepath.Clean(dir), filepath.Clean(s.Dir)
	return sd == d || strings.HasPrefix(sd, d+string(filepath.Separator))
}

// ResumeCommand is the shell command that resumes the session in its
// folder: Claude Code's of a config folder other than ~/.claude names it
// with CLAUDE_CONFIG_DIR.
func (e *Env) ResumeCommand(s Session) string {
	if ag := s.Harness.Agent(); ag.resume != nil {
		return ag.resume(e, s)
	}
	return ""
}

// quote quotes s for a POSIX shell, unless it needs no quotes.
func quote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Env is where the agents keep their sessions.
type Env struct {
	Home string
	// ConfigDir is $CLAUDE_CONFIG_DIR, "" when it is not set.
	ConfigDir string
	// ClaudeDirs are more config folders of Claude Code, as the settings
	// list them, which may start with ~.
	ClaudeDirs []string
}

// ClaudeRoots are the config folders of Claude Code: ~/.claude, each
// ~/.claude-* folder, $CLAUDE_CONFIG_DIR and those of the settings, each
// once, those of the home folder by name.
func (e *Env) ClaudeRoots() []string {
	roots := []string{filepath.Join(e.Home, ".claude")}
	if ents, err := os.ReadDir(e.Home); err == nil {
		for _, ent := range ents {
			if name, ok := strings.CutPrefix(ent.Name(), ".claude-"); ok && name != "" {
				roots = append(roots, filepath.Join(e.Home, ent.Name()))
			}
		}
	}
	for _, d := range append([]string{e.ConfigDir}, e.ClaudeDirs...) {
		if d = strings.TrimSpace(d); d == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(d, "~"); ok && (rest == "" || rest[0] == '/' || rest[0] == filepath.Separator) {
			d = e.Home + rest
		}
		roots = append(roots, filepath.Clean(d))
	}
	var out []string
	for _, r := range roots {
		if fi, err := os.Stat(r); err == nil && fi.IsDir() && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

// Scanner reads the sessions, and keeps what it read of each file until
// the file changes. It is safe for one goroutine at a time.
type Scanner struct {
	mu    sync.Mutex
	files map[string]*cached
	// titles are Codex's names of its sessions, by id, of the index as of
	// indexMod.
	titles   map[string]string
	indexMod time.Time
}

type cached struct {
	size int64
	mod  time.Time
	s    Session
	ok   bool
}

// NewScanner returns a scanner that has read nothing.
func NewScanner() *Scanner { return &Scanner{files: map[string]*cached{}} }

// scanWorkers is how many files are read at once.
const scanWorkers = 8

// Scan returns the sessions of every agent, the one used last first.
func (sc *Scanner) Scan(e *Env) []Session {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	type job struct {
		path string
		read func(string) (Session, bool)
		root string
	}
	var jobs []job
	for _, root := range e.ClaudeRoots() {
		for _, p := range transcripts(filepath.Join(root, "projects")) {
			jobs = append(jobs, job{p, func(p string) (Session, bool) { return readTranscript(p, Claude) }, root})
		}
	}
	for _, p := range transcripts(filepath.Join(e.Home, ".qoder", "projects")) {
		jobs = append(jobs, job{p, func(p string) (Session, bool) { return readTranscript(p, Qoder) }, ""})
	}
	for _, p := range codexRollouts(filepath.Join(e.Home, ".codex", "sessions")) {
		jobs = append(jobs, job{p, readCodex, ""})
	}
	for _, p := range grokSummaries(filepath.Join(e.Home, ".grok", "sessions")) {
		jobs = append(jobs, job{p, readGrok, ""})
	}
	sc.readCodexIndex(filepath.Join(e.Home, ".codex", "session_index.jsonl"))

	seen := make(map[string]bool, len(jobs))
	var todo []job
	var infos []os.FileInfo
	for _, j := range jobs {
		fi, err := os.Stat(j.path)
		if err != nil {
			continue
		}
		seen[j.path] = true
		if c := sc.files[j.path]; c != nil && c.size == fi.Size() && c.mod.Equal(fi.ModTime()) {
			continue
		}
		todo = append(todo, j)
		infos = append(infos, fi)
	}
	read := make([]cached, len(todo))
	var wg sync.WaitGroup
	next := make(chan int)
	for range min(scanWorkers, len(todo)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				j := todo[i]
				s, ok := j.read(j.path)
				s.Root = j.root
				if s.Updated.IsZero() {
					s.Updated = infos[i].ModTime()
				}
				read[i] = cached{size: infos[i].Size(), mod: infos[i].ModTime(), s: s, ok: ok}
			}
		}()
	}
	for i := range todo {
		next <- i
	}
	close(next)
	wg.Wait()
	for i, j := range todo {
		c := read[i]
		// A title past the end read of a transcript that grew stays.
		if old := sc.files[j.path]; old != nil && c.s.Title == "" {
			c.s.Title = old.s.Title
		}
		sc.files[j.path] = &c
	}
	for p := range sc.files {
		if !seen[p] {
			delete(sc.files, p)
		}
	}
	out := make([]Session, 0, len(sc.files))
	for _, c := range sc.files {
		if !c.ok {
			continue
		}
		s := c.s
		if s.Harness == Codex && sc.titles[s.ID] != "" {
			s.Title = sc.titles[s.ID]
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Updated.Equal(out[j].Updated) {
			return out[i].Updated.After(out[j].Updated)
		}
		return out[i].Key() < out[j].Key()
	})
	return out
}
