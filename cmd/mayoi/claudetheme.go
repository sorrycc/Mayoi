package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/claudetheme"
	"mayoi/locales"
)

// claudeThemeMin is the first version of Claude Code known to read custom
// themes from themes/<slug>.json and choose one as "custom:<slug>".
var claudeThemeMin = [3]int{2, 1, 119}

const claudeThemeMinText = "2.1.119"

// claudeProbeFor is how long claude --version may take.
var claudeProbeFor = 5 * time.Second

// claudeThemeKey is the setting that has Claude Code take Mayoi's theme.
const claudeThemeKey = "terminalClaudeTheme"

// claudeThemeWant is what Claude Code's theme should be: Mayoi's, of a
// theme and an appearance, or as it was. gen is the plugins' generation,
// as a plugin's theme may change under its name, and epoch counts the
// times the setting was turned on, which takes Claude Code's theme back
// after a choice made in Claude Code.
type claudeThemeWant struct {
	on    bool
	theme string
	dark  bool
	gen   uint64
	epoch int
}

// claudeThemeStatus is what the last sync found, shown under the setting.
type claudeThemeStatus struct {
	kind claudeThemeKind
	// arg is the theme replaced, the version found, the path or the error.
	arg string
}

type claudeThemeKind int

const (
	claudeThemeOK claudeThemeKind = iota
	claudeThemeReplaced
	claudeThemeLost
	claudeThemeNotFound
	claudeThemeTooOld
	claudeThemeTaken
	claudeThemeMalformed
	claudeThemeFailed
)

// claudeThemeSync writes Claude Code's theme on a goroutine of its own,
// one change at a time, for every window: what is asked while it writes
// waits, and only the last of it is written.
type claudeThemeSync struct {
	mu      sync.Mutex
	want    claudeThemeWant
	done    claudeThemeWant
	hasDone bool
	busy    bool
	status  claudeThemeStatus
	// version is the version of Claude Code found for epoch, "" before.
	version      string
	versionEpoch int
	// store is the config folder of Claude Code and Mayoi's record of it,
	// nil until the app is ready, which keeps the sync off.
	store func() (claudetheme.Store, bool)
	// probe runs claude --version.
	probe func(ctx context.Context) (string, error)
	// changed tells of a new status, on any goroutine.
	changed func()
	// idle, when set, is closed as the sync has nothing more to do.
	idle chan struct{}
}

var claudeSync = &claudeThemeSync{probe: probeClaude}

// errClaudeNotFound is no claude command on the path.
var errClaudeNotFound = errors.New("claude not found")

// request asks for Claude Code's theme to be w.
func (s *claudeThemeSync) request(w claudeThemeWant) {
	s.mu.Lock()
	if s.store == nil {
		s.mu.Unlock()
		return
	}
	s.want = w
	if s.busy || s.hasDone && s.done == w {
		s.mu.Unlock()
		return
	}
	s.busy = true
	s.idle = make(chan struct{})
	s.mu.Unlock()
	go s.run()
}

func (s *claudeThemeSync) run() {
	for {
		s.mu.Lock()
		w, prev, had := s.want, s.done, s.hasDone
		if had && prev == w {
			s.busy = false
			close(s.idle)
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		st := s.sync(w, had && w.on && w.epoch != prev.epoch)
		s.mu.Lock()
		s.done, s.hasDone, s.status = w, true, st
		changed := s.changed
		s.mu.Unlock()
		if changed != nil {
			changed()
		}
	}
}

// wait waits until the sync has nothing more to do, for the tests.
func (s *claudeThemeSync) wait() {
	s.mu.Lock()
	idle := s.idle
	s.mu.Unlock()
	if idle != nil {
		<-idle
	}
}

// current is the status of the last sync.
func (s *claudeThemeSync) current() claudeThemeStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// sync makes Claude Code's theme w, and says how that went. again, as the
// setting was just turned on again, takes back a choice made in Claude
// Code first.
func (s *claudeThemeSync) sync(w claudeThemeWant, again bool) claudeThemeStatus {
	if !w.on {
		store, ok := s.store()
		if !ok {
			return claudeThemeStatus{}
		}
		return claudeThemeResult(claudetheme.Result{}, store.Restore())
	}
	// CLAUDE_CONFIG_DIR and the path are the shell's once read.
	select {
	case <-shellPathReady:
	case <-time.After(10 * time.Second):
	}
	if st, ok := s.checkVersion(w.epoch); !ok {
		return st
	}
	store, ok := s.store()
	if !ok {
		return claudeThemeStatus{}
	}
	if again {
		if err := store.Restore(); err != nil {
			return claudeThemeResult(claudetheme.Result{}, err)
		}
	}
	r, err := store.Apply(func(previous string) claudetheme.Theme {
		return claudeThemeOf(paletteFor(w.theme, w.dark), previous)
	})
	return claudeThemeResult(r, err)
}

// checkVersion finds the version of Claude Code once each time the
// setting is turned on, and whether it reads custom themes.
func (s *claudeThemeSync) checkVersion(epoch int) (claudeThemeStatus, bool) {
	s.mu.Lock()
	known := s.version != "" && s.versionEpoch == epoch
	s.mu.Unlock()
	if known {
		return claudeThemeStatus{}, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), claudeProbeFor)
	defer cancel()
	out, err := s.probe(ctx)
	if errors.Is(err, errClaudeNotFound) {
		return claudeThemeStatus{kind: claudeThemeNotFound}, false
	}
	if err != nil {
		return claudeThemeStatus{kind: claudeThemeFailed, arg: err.Error()}, false
	}
	v, ok := claudeVersion(out)
	if !ok {
		return claudeThemeStatus{kind: claudeThemeFailed, arg: strings.TrimSpace(out)}, false
	}
	if !versionAtLeast(v, claudeThemeMin) {
		return claudeThemeStatus{kind: claudeThemeTooOld, arg: fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])}, false
	}
	s.mu.Lock()
	s.version, s.versionEpoch = out, epoch
	s.mu.Unlock()
	return claudeThemeStatus{}, true
}

// claudeThemeResult is the status of what Apply or Restore did.
func claudeThemeResult(r claudetheme.Result, err error) claudeThemeStatus {
	var taken *claudetheme.TakenError
	var malformed *claudetheme.MalformedError
	switch {
	case errors.As(err, &taken):
		return claudeThemeStatus{kind: claudeThemeTaken, arg: taken.Path}
	case errors.As(err, &malformed):
		return claudeThemeStatus{kind: claudeThemeMalformed, arg: malformed.Path}
	case err != nil:
		return claudeThemeStatus{kind: claudeThemeFailed, arg: err.Error()}
	case r.Lost:
		return claudeThemeStatus{kind: claudeThemeLost}
	case claudeThemeNotable(r.Previous):
		return claudeThemeStatus{kind: claudeThemeReplaced, arg: r.Previous}
	}
	return claudeThemeStatus{}
}

// claudeThemeNotable is whether a theme Mayoi's replaced is one the person
// made or chose for how they see: a custom theme, a colorblind-friendly
// one or one of the terminal's own colors.
func claudeThemeNotable(theme string) bool {
	return strings.HasPrefix(theme, "custom:") || strings.HasSuffix(theme, "-daltonized") || strings.HasSuffix(theme, "-ansi")
}

// claudeThemeMessage is what the setting's row says of the status, "" for
// nothing, and whether it is a failure.
func claudeThemeMessage(st claudeThemeStatus) (string, bool) {
	switch st.kind {
	case claudeThemeReplaced:
		return locales.ClaudeThemeReplaced(st.arg), false
	case claudeThemeLost:
		return locales.ClaudeThemeLost(), false
	case claudeThemeNotFound:
		return locales.ClaudeThemeNotFound(), true
	case claudeThemeTooOld:
		return locales.ClaudeThemeTooOld(st.arg, claudeThemeMinText), true
	case claudeThemeTaken:
		return locales.ClaudeThemeTaken(st.arg), true
	case claudeThemeMalformed:
		return locales.ClaudeThemeMalformed(st.arg), true
	case claudeThemeFailed:
		return locales.ClaudeThemeFailed(st.arg), true
	}
	return "", false
}

// claudeThemeStore is the config folder of Claude Code of this machine,
// $CLAUDE_CONFIG_DIR or ~/.claude, and Mayoi's record of what it changed
// there, beside its settings.
func claudeThemeStore(settingsFile string) func() (claudetheme.Store, bool) {
	return func() (claudetheme.Store, bool) {
		home, err := os.UserHomeDir()
		if err != nil || settingsFile == "" {
			return claudetheme.Store{}, false
		}
		dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
		if rest, ok := strings.CutPrefix(dir, "~"); ok && (rest == "" || rest[0] == '/' || rest[0] == filepath.Separator) {
			dir = home + rest
		}
		if dir == "" {
			dir = filepath.Join(home, ".claude")
		}
		return claudetheme.Store{
			ConfigDir: filepath.Clean(dir),
			StatePath: filepath.Join(filepath.Dir(settingsFile), "claude-theme.json"),
		}, true
	}
}

// probeClaude runs claude --version, the claude of the path or of
// Claude Code's own installer.
func probeClaude(ctx context.Context) (string, error) {
	bin, err := exec.LookPath("claude")
	if err != nil {
		home, _ := os.UserHomeDir()
		for _, p := range []string{filepath.Join(home, ".local", "bin", "claude"), filepath.Join(home, ".claude", "local", "claude")} {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				bin = p
				break
			}
		}
	}
	if bin == "" {
		return "", errClaudeNotFound
	}
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return string(out), err
}

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// claudeVersion reads the version claude --version prints.
func claudeVersion(out string) ([3]int, bool) {
	m := versionRe.FindStringSubmatch(out)
	if m == nil {
		return [3]int{}, false
	}
	var v [3]int
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v, true
}

func versionAtLeast(v, least [3]int) bool {
	for i := range v {
		if v[i] != least[i] {
			return v[i] > least[i]
		}
	}
	return true
}

// syncClaudeTheme has Claude Code's theme follow the setting and the
// theme and appearance of the settings, the system's light or dark too.
func (m *mayoi) syncClaudeTheme() {
	if m.s == nil {
		return
	}
	p := m.s.Prefs
	if p.TerminalClaudeTheme && !m.claudeThemeOn && m.claudeThemeSeen {
		m.claudeThemeEpoch++
	}
	m.claudeThemeOn, m.claudeThemeSeen = p.TerminalClaudeTheme, true
	w := claudeThemeWant{on: p.TerminalClaudeTheme, epoch: m.claudeThemeEpoch}
	if w.on {
		w.theme, w.gen = p.Theme, pluginsGen()
		switch {
		case alwaysDark(p.Theme) || p.Appearance == "dark":
			w.dark = true
		case p.Appearance == "":
			w.dark = systemDark()
		}
	}
	claudeSync.request(w)
}

// systemDark is whether the system's appearance is dark.
var systemDark = func() bool { return mygo.Theme.IsDark() }

// pluginsGen is the plugins' generation, 0 without them.
func pluginsGen() uint64 {
	if plugins == nil {
		return 0
	}
	plugins.mu.Lock()
	defer plugins.mu.Unlock()
	return plugins.gen
}

// claudeThemeOf is a theme of Claude Code of a palette: the colors of its
// terminal, its accent, and its marks of what changed, each reading on the
// terminal's background. A colorblind-friendly theme it replaces keeps its
// base, and with it its colors of diffs, success, errors and warnings.
func claudeThemeOf(p *palette, previous string) claudetheme.Theme {
	tt := termTheme(p)
	bg := tt.Background
	base := "light"
	if p.dark {
		base = "dark"
	}
	daltonized := strings.HasSuffix(previous, "-daltonized")
	if daltonized {
		base += "-daltonized"
	}
	ansi := p.ansi
	text := legibleAt(tt.Foreground, p.dark, 7, bg)
	muted := legibleAt(p.muted, p.dark, 4.5, bg)
	faint := legibleAt(p.faint, p.dark, 3, bg)
	accent := legibleAt(p.accent, p.dark, 4.5, bg)
	fg := func(c ui.Color) ui.Color { return legibleAt(c, p.dark, 4.5, bg) }
	mark := func(c ui.Color) ui.Color { return legibleAt(c, p.dark, 3, bg) }
	shimmer := func(c ui.Color) ui.Color { return c.Mix(text, 0.35) }
	tint := func(c ui.Color, a float32) ui.Color { return c.Alpha(a).Over(bg) }
	o := map[string]ui.Color{
		"text":                       text,
		"inverseText":                bg,
		"background":                 bg,
		"inactive":                   muted,
		"inactiveShimmer":            shimmer(muted),
		"subtle":                     faint,
		"claude":                     accent,
		"claudeShimmer":              shimmer(accent),
		"suggestion":                 accent,
		"permission":                 accent,
		"permissionShimmer":          shimmer(accent),
		"promptBorder":               faint,
		"promptBorderShimmer":        muted,
		"planMode":                   fg(ansi[6]),
		"autoAccept":                 fg(ansi[5]),
		"autoAcceptShimmer":          shimmer(fg(ansi[5])),
		"bashBorder":                 mark(ansi[13]),
		"skill":                      fg(ansi[5]),
		"ide":                        fg(ansi[4]),
		"remember":                   fg(ansi[4]),
		"merged":                     fg(ansi[5]),
		"professionalBlue":           fg(ansi[4]),
		"selectionBg":                tt.Selection.Over(bg),
		"userMessageBackground":      tint(text, 0.08),
		"userMessageBackgroundHover": tint(text, 0.12),
		"bashMessageBackgroundColor": tint(ansi[13], 0.1),
		"memoryBackgroundColor":      tint(ansi[4], 0.1),
		"rate_limit_fill":            accent,
		"rate_limit_empty":           faint,
		"red_FOR_SUBAGENTS_ONLY":     fg(ansi[1]),
		"green_FOR_SUBAGENTS_ONLY":   fg(ansi[2]),
		"yellow_FOR_SUBAGENTS_ONLY":  fg(ansi[3]),
		"blue_FOR_SUBAGENTS_ONLY":    fg(ansi[4]),
		"purple_FOR_SUBAGENTS_ONLY":  fg(ansi[5]),
		"cyan_FOR_SUBAGENTS_ONLY":    fg(ansi[6]),
		"orange_FOR_SUBAGENTS_ONLY":  fg(ansi[1].Mix(ansi[3], 0.5)),
		"pink_FOR_SUBAGENTS_ONLY":    fg(ansi[13]),
	}
	if !daltonized {
		o["success"] = fg(ansi[2])
		o["error"] = fg(ansi[1])
		o["warning"] = fg(ansi[3])
		o["warningShimmer"] = shimmer(fg(ansi[3]))
		o["diffAdded"] = tint(p.added, 0.25)
		o["diffRemoved"] = tint(p.removed, 0.25)
		o["diffAddedDimmed"] = tint(p.added, 0.12)
		o["diffRemovedDimmed"] = tint(p.removed, 0.12)
		o["diffAddedWord"] = tint(p.added, 0.5)
		o["diffRemovedWord"] = tint(p.removed, 0.5)
	}
	out := make(map[string]string, len(o))
	for k, c := range o {
		if c.A < 255 {
			c = c.Over(bg)
		}
		out[k] = hexColor(c)
	}
	return claudetheme.Theme{Name: "Mayoi", Base: base, Overrides: out}
}

// legibleAt is legible for a contrast of ratio on back: col itself when
// it reads, or else col mixed toward black on a light background, or
// white on a dark one, no further than it must.
func legibleAt(col ui.Color, dark bool, ratio float64, back ui.Color) ui.Color {
	if col.A < 255 {
		col = col.Over(back)
	}
	to := black
	if dark {
		to = white
	}
	for i := 0; i <= 40; i++ {
		if c := col.Mix(to, float32(i)/40); contrast(c, back) >= ratio || i == 40 {
			return c
		}
	}
	return col
}
