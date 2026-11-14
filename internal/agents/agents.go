// Package agents reads the sessions coding agents keep on this machine:
// Claude Code, in each of its config folders, Qoder, Codex and Grok, and
// those of the agents plugins add. It reads their files only, and of a
// transcript only its start and its end, as one can be gigabytes: where it
// ran and what was asked first, and the title it was given last.
package agents

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Harness is an agent whose sessions are read.
type Harness string

const (
	Claude Harness = "claude"
	Qoder  Harness = "qoder"
	Codex  Harness = "codex"
	Grok   Harness = "grok"
	Pi     Harness = "pi"
)

// Agent is what is known of an agent: its id, its names, its mark, how
// its sessions are listed and how one of them is resumed. The agents Mayoi
// knows and those plugins add are of this one shape, and listed alike.
type Agent struct {
	ID Harness
	// Name is its name, for people, and App the name it gives itself for
	// machines, in what it says it does (OSC 7501) and in what hc says
	// runs in a terminal.
	Name, App string
	// Icon is its mark, the shapes of an SVG of a 24 by 24 view drawn in
	// strokes of the color of the text, as the app's icons are, a whole
	// SVG, or the name of one of the app's icons; "" for the mark of an
	// agent of no mark of its own.
	Icon string
	// Tint is the color of its mark in a dark window and in a light one,
	// as hex; "" for the color of quiet text.
	Tint [2]string
	// Resume is the shell command that resumes s, in its folder.
	Resume func(e *Env, s Session) string

	// Files are the files of its sessions, each read by Read, which
	// reports false for one that is no session to show. A file is read
	// again only once it changes.
	Files func(e *Env) []File
	Read  func(path string) (Session, bool)
	// Transcript, for an agent whose files read are not what was said,
	// is the file of what was, "" for none; nil for the file read.
	Transcript func(path string) string
	// Titles, for an agent that names its sessions in a file of its own,
	// are that file and how it reads, its names by session id, which win
	// over those of the sessions' files.
	Titles *TitleIndex
	// List, for an agent of no files to read, lists its sessions itself,
	// as a plugin's function does, before ctx is done: off the main thread,
	// and waited for no longer than ListWait.
	List func(ctx context.Context) ([]Session, error)
}

// File is a file of an agent's sessions, and the config folder it is of,
// "" for none.
type File struct{ Path, Root string }

// TitleIndex is a file of an agent's own, of the names of its sessions.
type TitleIndex struct {
	Path func(e *Env) string
	Read func(path string) map[string]string
}

// Known are the agents Mayoi reads of itself, in the order they are named.
var Known = []Agent{
	{
		ID: Claude, Name: "Claude Code", App: "claude-code",
		// A spark of eight rays, the long ones upright.
		Icon: `<path d="M12 3v18"/><path d="M3 12h18"/><path d="m6.7 6.7 10.6 10.6"/><path d="m17.3 6.7-10.6 10.6"/>`,
		Tint: [2]string{"#d97757", "#c15f3c"},
		Resume: func(e *Env, s Session) string {
			cmd := "claude --resume " + quote(s.ID)
			if s.Root != "" && filepath.Clean(s.Root) != filepath.Join(e.Home, ".claude") {
				cmd = "CLAUDE_CONFIG_DIR=" + quote(s.Root) + " " + cmd
			}
			return cmd
		},
		Files: func(e *Env) []File {
			var out []File
			for _, root := range e.ClaudeRoots() {
				for _, p := range transcripts(filepath.Join(root, "projects")) {
					out = append(out, File{p, root})
				}
			}
			return out
		},
		Read: func(p string) (Session, bool) { return readTranscript(p, Claude) },
	},
	{
		ID: Qoder, Name: "Qoder", App: "qodercli",
		// A Q, its tail through its ring.
		Icon:   `<circle cx="11.5" cy="11" r="8"/><path d="m13.5 14.5 6 6.5"/>`,
		Tint:   [2]string{"#2fd189", "#13985a"},
		Resume: func(_ *Env, s Session) string { return "qodercli -r " + quote(s.ID) },
		Files:  func(e *Env) []File { return files(transcripts(filepath.Join(e.Home, ".qoder", "projects"))) },
		Read:   func(p string) (Session, bool) { return readTranscript(p, Qoder) },
	},
	{
		ID: Codex, Name: "Codex", App: "codex",
		// A prompt in a hexagon.
		Icon:   `<path d="M12 2.5 20.5 7.25v9.5L12 21.5 3.5 16.75v-9.5Z"/><path d="m10 8.5 3.5 3.5-3.5 3.5"/>`,
		Resume: func(_ *Env, s Session) string { return "codex resume " + quote(s.ID) },
		Files:  func(e *Env) []File { return files(codexRollouts(filepath.Join(e.Home, ".codex", "sessions"))) },
		Read:   readCodex,
		Titles: &TitleIndex{
			Path: func(e *Env) string { return filepath.Join(e.Home, ".codex", "session_index.jsonl") },
			Read: readCodexIndex,
		},
	},
	{
		ID: Grok, Name: "Grok", App: "grok",
		// A ring, open where a slash runs through it.
		Icon:   `<path d="M17.5 6.2A8 8 0 1 0 19.8 13"/><path d="M20.5 3.5 9 15"/>`,
		Resume: func(_ *Env, s Session) string { return "grok --resume " + quote(s.ID) },
		Files:  func(e *Env) []File { return files(grokSummaries(filepath.Join(e.Home, ".grok", "sessions"))) },
		Read:   readGrok,
		// Its summary is beside the chat.
		Transcript: func(p string) string {
			chat := filepath.Join(filepath.Dir(p), "chat_history.jsonl")
			if _, err := os.Stat(chat); err != nil {
				return ""
			}
			return chat
		},
	},
	{
		ID: Pi, Name: "Pi", App: "pi",
		// A pi: a bar and two legs, the right one swept.
		Icon:   `<path d="M4.5 7h15"/><path d="M7.5 7v11"/><path d="M17.5 7c.5 5-1 11-5.5 11"/>`,
		Tint:   [2]string{"#8b7cf6", "#6d5ce0"},
		Resume: ResumeTemplate("pi --session {id}"),
		Files:  func(e *Env) []File { return files(piSessions(filepath.Join(e.Home, ".pi", "agent", "sessions"))) },
		Read:   readPi,
	},
}

// files are paths as files of no config folder.
func files(paths []string) []File {
	out := make([]File, len(paths))
	for i, p := range paths {
		out[i] = File{Path: p}
	}
	return out
}

// all are the agents Known and those Use added after them, made anew as
// those change.
var all atomic.Pointer[[]Agent]

func init() { all.Store(&Known) }

// Use has the agents be those Known and more, as plugins add them, after
// them: one of an id taken is left out.
func Use(more []Agent) {
	out := slices.Clone(Known)
	for _, ag := range more {
		if !slices.ContainsFunc(out, func(o Agent) bool { return o.ID == ag.ID }) {
			out = append(out, ag)
		}
	}
	all.Store(&out)
}

// All are the agents read: those Known, then those Use added.
func All() []Agent { return *all.Load() }

// Agent is what is known of the agent h; of one not known, its id as its
// names and no mark.
func (h Harness) Agent() Agent {
	for _, ag := range All() {
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
	for _, ag := range All() {
		if ag.App == app || string(ag.ID) == app {
			return ag.ID, true
		}
	}
	return "", false
}

// agentDomains are the domains of the emails the agents sign the commits
// they help with.
var agentDomains = map[string]Harness{
	"anthropic.com": Claude,
	"openai.com":    Codex,
	"x.ai":          Grok,
	"qoder.com":     Qoder,
}

// HarnessOfCoAuthor is the agent a commit's co-author is, by the email
// it signs with, or by its name's first word for one of no email or a
// noreply one; false for a person.
func HarnessOfCoAuthor(name, email string) (Harness, bool) {
	email = strings.ToLower(email)
	if _, domain, ok := strings.Cut(email, "@"); ok {
		if h, ok := agentDomains[domain]; ok {
			return h, true
		}
	}
	if email != "" && !strings.Contains(email, "noreply") {
		return "", false
	}
	first, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(name)), " ")
	for _, ag := range All() {
		if first != "" && (first == string(ag.ID) || first == strings.ToLower(ag.App)) {
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
	// Transcript is the file of what was said in it, "" when it has
	// none, as of a List.
	Transcript string
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
	if ag := s.Harness.Agent(); ag.Resume != nil {
		return ag.Resume(e, s)
	}
	return ""
}

// ResumeTemplate is a Resume of a command with {id}, {folder} and {title}
// in it, each the session's, quoted for a POSIX shell.
func ResumeTemplate(tmpl string) func(e *Env, s Session) string {
	return func(_ *Env, s Session) string {
		return strings.NewReplacer("{id}", quote(s.ID), "{folder}", quote(s.Dir), "{title}", quote(s.Title)).Replace(tmpl)
	}
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
// the file changes, and what each agent that lists its own sessions
// listed last. It is safe for one goroutine at a time.
type Scanner struct {
	mu    sync.Mutex
	files map[string]*cached
	// indexes are the names agents keep of their sessions, by agent.
	indexes map[Harness]*index

	// lmu guards lists, what the agents of a List listed, by agent, and
	// late, which a List that ends after Scan stopped waiting calls.
	lmu   sync.Mutex
	lists map[Harness]*listed
	late  func()
}

type cached struct {
	size int64
	mod  time.Time
	s    Session
	ok   bool
}

// index is a TitleIndex as of the time its file was last changed.
type index struct {
	path   string
	mod    time.Time
	titles map[string]string
}

// listed is what an agent's List listed last, and whether one runs.
type listed struct {
	ss   []Session
	busy bool
}

// NewScanner returns a scanner that has read nothing.
func NewScanner() *Scanner {
	return &Scanner{files: map[string]*cached{}, indexes: map[Harness]*index{}, lists: map[Harness]*listed{}}
}

// OnLate has fn called, on a goroutine of a List, as one ends after Scan
// stopped waiting for it, with sessions the next Scan has.
func (sc *Scanner) OnLate(fn func()) {
	sc.lmu.Lock()
	sc.late = fn
	sc.lmu.Unlock()
}

// scanWorkers is how many files are read at once.
const scanWorkers = 8

// ListWait is how long Scan waits for the agents of a List, and
// ListLimit how long one may run before it is given up. One that ends
// late is kept for the next Scan, and one that fails keeps what it listed
// before.
const (
	ListWait  = 1500 * time.Millisecond
	ListLimit = 10 * time.Second
)

// Scan returns the sessions of every agent, the one used last first.
func (sc *Scanner) Scan(e *Env) []Session {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	agents := All()
	waits := sc.startLists(agents)

	type job struct {
		File
		read       func(string) (Session, bool)
		transcript func(string) string
	}
	var jobs []job
	for _, ag := range agents {
		if ag.Files == nil || ag.Read == nil {
			continue
		}
		for _, f := range ag.Files(e) {
			jobs = append(jobs, job{f, ag.Read, ag.Transcript})
		}
	}
	sc.readIndexes(e, agents)

	seen := make(map[string]bool, len(jobs))
	var todo []job
	var infos []os.FileInfo
	for _, j := range jobs {
		fi, err := os.Stat(j.Path)
		if err != nil || seen[j.Path] {
			continue
		}
		seen[j.Path] = true
		if c := sc.files[j.Path]; c != nil && c.size == fi.Size() && c.mod.Equal(fi.ModTime()) {
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
				s, ok := j.read(j.Path)
				s.Root, s.Transcript = j.Root, j.Path
				if j.transcript != nil {
					s.Transcript = j.transcript(j.Path)
				}
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
		if old := sc.files[j.Path]; old != nil && c.s.Title == "" {
			c.s.Title = old.s.Title
		}
		sc.files[j.Path] = &c
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
		if ix := sc.indexes[s.Harness]; ix != nil && ix.titles[s.ID] != "" {
			s.Title = ix.titles[s.ID]
		}
		out = append(out, s)
	}
	out = append(out, sc.waitLists(agents, waits)...)
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Updated.Equal(out[j].Updated) {
			return out[i].Updated.After(out[j].Updated)
		}
		return out[i].Key() < out[j].Key()
	})
	return out
}

// readIndexes reads the TitleIndex of each agent of one, once its file
// changed, and forgets those of agents gone.
func (sc *Scanner) readIndexes(e *Env, agents []Agent) {
	keep := map[Harness]bool{}
	for _, ag := range agents {
		if ag.Titles == nil {
			continue
		}
		keep[ag.ID] = true
		path := ag.Titles.Path(e)
		fi, err := os.Stat(path)
		if err != nil {
			delete(sc.indexes, ag.ID)
			continue
		}
		if ix := sc.indexes[ag.ID]; ix != nil && ix.path == path && ix.mod.Equal(fi.ModTime()) {
			continue
		}
		sc.indexes[ag.ID] = &index{path: path, mod: fi.ModTime(), titles: ag.Titles.Read(path)}
	}
	for h := range sc.indexes {
		if !keep[h] {
			delete(sc.indexes, h)
		}
	}
}

// round is a Scan's wait for the Lists it started, until ListWait after
// they started: gaveUp, guarded by lmu, is set once it stopped waiting,
// and done closes as each List ends.
type round struct {
	until  time.Time
	gaveUp bool
	done   []chan struct{}
}

// startLists starts the List of each agent of one, unless one of it
// still runs, and forgets what agents gone listed.
func (sc *Scanner) startLists(agents []Agent) *round {
	sc.lmu.Lock()
	defer sc.lmu.Unlock()
	r := &round{until: time.Now().Add(ListWait)}
	keep := map[Harness]bool{}
	for _, ag := range agents {
		if ag.List == nil {
			continue
		}
		keep[ag.ID] = true
		l := sc.lists[ag.ID]
		if l == nil {
			l = &listed{}
			sc.lists[ag.ID] = l
		}
		if l.busy {
			continue
		}
		l.busy = true
		done := make(chan struct{})
		r.done = append(r.done, done)
		go sc.list(ag, l, r, done)
	}
	for h := range sc.lists {
		if !keep[h] {
			delete(sc.lists, h)
		}
	}
	return r
}

// list runs ag's List into l, keeping what it listed before when it
// fails, then closes done, and calls late when the Scan of r no longer
// waits.
func (sc *Scanner) list(ag Agent, l *listed, r *round, done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), ListLimit)
	ss, err := ag.List(ctx)
	cancel()
	for i := range ss {
		ss[i].Harness, ss[i].Root = ag.ID, ""
	}
	ss = slices.DeleteFunc(ss, func(s Session) bool { return s.ID == "" })
	sc.lmu.Lock()
	l.busy = false
	if err == nil {
		l.ss = ss
	}
	late := sc.late
	if !r.gaveUp || err != nil {
		late = nil
	}
	sc.lmu.Unlock()
	close(done)
	if late != nil {
		late()
	}
}

// waitLists waits for the Lists of r, as long as it may, and returns
// what every agent of one listed last.
func (sc *Scanner) waitLists(agents []Agent, r *round) []Session {
	if len(r.done) > 0 {
		timeout := time.NewTimer(time.Until(r.until))
		defer timeout.Stop()
	wait:
		for _, done := range r.done {
			select {
			case <-done:
			case <-timeout.C:
				break wait
			}
		}
	}
	sc.lmu.Lock()
	defer sc.lmu.Unlock()
	r.gaveUp = true
	var out []Session
	for _, ag := range agents {
		if l := sc.lists[ag.ID]; l != nil {
			out = append(out, l.ss...)
		}
	}
	return out
}
