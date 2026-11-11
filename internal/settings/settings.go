// Package settings keeps what Mayoi remembers between launches, in JSON
// files: the preferences in one, and the state Mayoi keeps itself in
// another beside it.
package settings

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Settings are what Mayoi remembers between launches: the preferences,
// in settings.json, and the state Mayoi keeps itself, in state.json
// beside it. A window's frame is its StateKey's.
type Settings struct {
	Prefs
	State
	// WrapGen counts the changes of WordWrap.
	WrapGen int

	path      string
	statePath string
	mu        sync.Mutex
	timer     *time.Timer
	// written is what the settings last wrote to their file, which a
	// change of the file that is not theirs differs from, and
	// stateWritten what they last wrote to the state's.
	written      []byte
	stateWritten []byte
	// prefsWritten is called, on a goroutine of its own, each time the
	// settings write a change of the preferences to their file.
	prefsWritten func()
	// seq counts the encodings of the settings, which wmu writes in turn:
	// wrote is the seq of the last written, before which none lands.
	seq   uint64
	wmu   sync.Mutex
	wrote uint64
	// gen counts the changes of the preferences, as Changed and SetPrefs
	// tell of them, prefsSeen is the preferences as gen last counted, and
	// prefsChanged is called with each change Changed tells of.
	gen          uint64
	prefsSeen    []byte
	prefsChanged func()
}

// State is what Mayoi keeps itself, not what a person chooses: the
// recent folders, the windows with the folder, the layout and the files
// open in each, and the sessions of the folders closed.
type State struct {
	Recent []string `json:"recent"`
	// Windows are the windows open, the one focused last first.
	Windows []*Window `json:"windows,omitempty"`
	// Rail is the order of the windows' IDs on the rail of project tabs,
	// as they were last.
	Rail []int `json:"rail,omitempty"`
	// Sessions are the tabs and the layout of the recent folders no
	// window has open, by folder, which the next window to open one gets
	// back.
	Sessions map[string]*Session `json:"sessions,omitempty"`
}

// oldLayout is the layout an older version kept for every window, in the
// state, which the windows and the sessions without one of their own get.
type oldLayout struct {
	SidebarOpen  *bool   `json:"sidebarOpen"`
	SidebarView  string  `json:"sidebarView"`
	SidebarWidth float32 `json:"sidebarWidth"`
	PanelHeight  float32 `json:"panelHeight"`
}

// legacy is what an older version kept in the settings' file besides the
// state: DiffSplit, which DiffLayout "split" is now, and the window as a
// version with one window kept it, Folder, Columns, Column, PanelOpen and
// TerminalOpen, with OpenFiles and ActiveFile its tabs as a version
// without columns kept them. Load makes a window of them.
type legacy struct {
	DiffSplit    bool     `json:"diffSplit"`
	Folder       string   `json:"folder"`
	Columns      []Column `json:"columns"`
	Column       int      `json:"column"`
	PanelOpen    bool     `json:"panelOpen"`
	TerminalOpen bool     `json:"terminalOpen"`
	OpenFiles    []string `json:"openFiles"`
	ActiveFile   string   `json:"activeFile"`
}

// PluginSource is where a plugin installed from Git came from.
type PluginSource struct {
	URL    string `json:"url"`
	Ref    string `json:"ref,omitempty"`
	Commit string `json:"commit,omitempty"`
}

// Prefs are the settings a person chooses, which an edit of the file
// applies as Mayoi runs: not the windows and the layout it keeps itself.
type Prefs struct {
	// ZoomLevel scales all window content by 1.2 to this power.
	ZoomLevel  float32 `json:"zoomLevel"`
	FontSize   float32 `json:"fontSize"`
	FontFamily string  `json:"fontFamily"`
	// FontWeight is how heavy the code's text is, from 100 to 900.
	FontWeight int `json:"fontWeight"`
	// FontFeatures turn OpenType features of the code's font on and off,
	// as "calt=0" turns off most coding ligatures and "ss01" a stylistic
	// set on.
	FontFeatures []string `json:"fontFeatures,omitempty"`
	// LineHeight is the height of a line of code, a multiple of FontSize.
	LineHeight float32 `json:"lineHeight"`
	// TerminalFontFamily and TerminalFontSize are the terminal's, or
	// those of the code when empty and zero.
	TerminalFontFamily string  `json:"terminalFontFamily,omitempty"`
	TerminalFontSize   float32 `json:"terminalFontSize,omitempty"`
	// TerminalCopyOnSelect copies what is selected in a terminal to the
	// clipboard as the selection ends.
	TerminalCopyOnSelect bool `json:"terminalCopyOnSelect"`
	// AutoSave saves changed files a moment after an edit, and as the
	// keyboard leaves them.
	AutoSave bool `json:"autoSave"`
	// WordWrap wraps long lines at the edge of the editor, rather than
	// scrolling sideways to them. A tab may choose otherwise, until this
	// changes: Settings.WrapGen counts its changes.
	WordWrap bool `json:"wordWrap,omitempty"`
	// EditorAgentContext offers selection actions in ordinary files to
	// collect code snippets and send them with review comments.
	EditorAgentContext bool `json:"editorAgentContext,omitempty"`
	// PreviewMermaid draws the blocks of Mermaid of a Markdown preview as
	// the diagrams they make, rather than their source.
	PreviewMermaid bool `json:"previewMermaid"`
	// PreviewMath draws the TeX math of a Markdown preview, between $ and
	// $, $$ and $$, or in a block of math, as the formulas it makes.
	PreviewMath bool `json:"previewMath"`
	// MarkdownPreview opens a Markdown file, .mdx included, rendered
	// rather than as text; ⇧⌘V still shows the text.
	MarkdownPreview bool `json:"markdownPreview,omitempty"`
	// DiffLayout is how diffs show: "split" side by side, "inline" in
	// one column, or "" side by side when they are wide enough to.
	DiffLayout string `json:"diffLayout,omitempty"`
	// DiffCrossFiles is how a diff's change navigation crosses into
	// another file at the first or last change of one: "" says where it
	// is and crosses on one more step that way, "always" crosses at once,
	// and "never" stays in the file.
	DiffCrossFiles string `json:"diffCrossFiles,omitempty"`
	// DiffHighlight is what a diff shows of a line that changed: "line"
	// no more than the line, "char" the characters that changed, or ""
	// the words that changed. What it shows is only so: a diff's block
	// actions are for its hunks of lines, whatever it highlights.
	DiffHighlight string `json:"diffHighlight,omitempty"`
	// DiffWhitespace controls the comparison: "eol" ignores line-end
	// whitespace, "all" ignores whitespace, and "blank" also treats
	// whitespace-only lines as empty. Empty is exact comparison.
	DiffWhitespace string `json:"diffWhitespace,omitempty"`
	// DiffContext is how many unchanged lines a diff shows around a
	// change, the rest folded away.
	DiffContext int `json:"diffContext"`
	// DiffSyncScroll has the sides of a side-by-side diff scroll
	// together. Without it, each side scrolls on its own, and scrolls
	// together again from where the right one was.
	DiffAlignedRows bool `json:"diffAlignedRows"`
	DiffSyncScroll  bool `json:"diffSyncScroll"`
	// MergeAutoApply puts the changes of a conflicted file that no
	// conflict holds in its result when the merge view opens. The
	// conflicts stand, for the hand to decide.
	MergeAutoApply bool `json:"mergeAutoApply"`
	// ScmTree lists the changes in source control in a tree of their
	// folders, rather than by path.
	ScmTree bool `json:"scmTree,omitempty"`
	// ScmAllBranches has the commits of source control show every branch
	// and tag, not only the history of the branch checked out.
	ScmAllBranches bool `json:"scmAllBranches,omitempty"`
	// ScmRepositoryOpen remembers whether the repository panel is expanded.
	ScmRepositoryOpen bool `json:"scmRepositoryOpen,omitempty"`
	// ScmRemotes selects Remotes instead of Branches in the repository panel.
	ScmRemotes bool `json:"scmRemotes,omitempty"`
	// Blame shows who last changed the caret's line, and when, after it.
	Blame bool `json:"blame"`
	// Vim edits with vim's keys: the editors start in normal mode.
	Vim bool `json:"vim,omitempty"`
	// VimCtrlKeys has vim's normal mode take ⌃F, ⌃B and ⌃R on Windows and
	// Linux, rather than Find, Toggle Sidebar and Open Recent.
	VimCtrlKeys bool `json:"vimCtrlKeys,omitempty"`
	// Appearance is "light", "dark", or "" to follow the system.
	Appearance string `json:"appearance,omitempty"`
	// Theme is "matrix" for The Matrix, "purplegold" for Purple Gold,
	// "pumpkin" for Jack-o'-Lantern, "witchsbrew" for Witch's Brew or
	// "bloodmoon" for Blood Moon, which are always dark, "claude" for Claude, "qoder" for Qoder,
	// "grokbot" for Grok Bot or "cursor" for Cursor, light or dark as
	// Appearance is, or "" for Mayoi's own.
	Theme string `json:"theme,omitempty"`
	// SidebarRight puts the sidebar right of the editors, rather than left.
	SidebarRight bool `json:"sidebarRight,omitempty"`
	// ExplorerCompactFolders shows a chain of single-child folders on one
	// row in the Explorer, with its names separated by slashes.
	ExplorerCompactFolders bool `json:"explorerCompactFolders,omitempty"`
	// PanelPosition is where the terminal panel was, before the views
	// moved between the panels: "" or "bottom" below the editors, "left"
	// or "right" at the window's side. The layout puts the terminal where
	// it says, once, and it goes.
	PanelPosition string `json:"panelPosition,omitempty"`
	// TerminalLocation is where a new terminal opens: "" in the panel, or
	// "editor" in a tab of the editor area.
	TerminalLocation string `json:"terminalLocation,omitempty"`
	// TerminalBackend is what runs a terminal's shell: "" Mayoi itself,
	// or "hc" a terminal of herdr-core, which outlives the window.
	TerminalBackend string `json:"terminalBackend,omitempty"`
	// TerminalBackendHcPath is the hc to run when TerminalBackend is
	// "hc": a path to the program, a folder that has it, or a name on
	// the path. Mayoi finds hc there first, and on the path or in
	// ~/.local/bin without it.
	TerminalBackendHcPath string `json:"terminalBackendHcPath,omitempty"`
	// TerminalBackendHcEnv is the environment of hc's commands when
	// TerminalBackend is "hc", as "KEY=value". A string without "=" or
	// with a NUL is ignored.
	TerminalBackendHcEnv []string `json:"terminalBackendHcEnv,omitempty"`
	// TerminalBackendHcShowInHerdr shows a terminal of herdr-core in the
	// Herdr app, in a space of the window's folder, when TerminalBackend
	// is "hc". Those of a remote folder stay out of it.
	TerminalBackendHcShowInHerdr bool `json:"terminalBackendHcShowInHerdr"`
	// ProjectTabs keeps the folders open in one window, a rail on its left
	// choosing which shows, rather than a window for each.
	ProjectTabs bool `json:"projectTabs,omitempty"`
	// ProjectTabStyle is a project's own label, color and icon on the rail
	// of project tabs, by its folder.
	ProjectTabStyle map[string]TabStyle `json:"projectTabStyle,omitempty"`
	// Locale is the language Mayoi shows, as "en", "zh-Hans" or "ja", or ""
	// for the one the system's languages prefer.
	Locale string `json:"locale,omitempty"`
	// AIModel is the model of Mon's AI gateway that writes commit
	// messages, "" for Mayoi's choice.
	AIModel string `json:"aiModel,omitempty"`
	// AISecretFiles are patterns of files whose lines a commit message's
	// AI never sees, after Mayoi's own: a name, or the end of a path with
	// "/", its start with "/" first, "**" for any folders, "/" last for a
	// folder, and "!" first to send a file that matched before. What looks
	// like a key is left out whatever it says. A folder can't set it, so
	// that a repository can't send what its cloner keeps out.
	AISecretFiles []string `json:"aiSecretFiles,omitempty"`
	// CrashReports is what Mayoi does with the report of a crash at the
	// next launch: "" asks whether to send it to Mon, "always" sends it
	// and "never" doesn't.
	CrashReports string `json:"crashReports,omitempty"`
	// SyncIgnored is what settings sync leaves on this machine: keys of
	// the preferences, "settings.json" for all of them, and
	// "keybindings.json" for the keyboard shortcuts.
	SyncIgnored []string `json:"syncIgnored,omitempty"`
	// Plugins are the plugins' own settings, by plugin name: each an
	// object of the settings its plugin.json declares, and "enabled":
	// false to turn the plugin off.
	Plugins map[string]json.RawMessage `json:"plugins,omitempty"`
	// FormatOnSave formats a document with a plugin's formatter as it
	// saves, by language, as "go", or "*" for every language.
	FormatOnSave map[string]bool `json:"formatOnSave,omitempty"`
	// Formatters chooses the formatter of a language where plugins offer
	// several: the formatter's id, or its plugin's name.
	Formatters map[string]string `json:"formatters,omitempty"`
	// PluginSources are the plugins installed from Git and where each came
	// from, as ~/.mayoi/plugins/plugins.json has them, which Mayoi keeps
	// here for settings sync to bring to the person's other machines.
	PluginSources map[string]PluginSource `json:"pluginSources,omitempty"`
}

// TabStyle is a project's own look on the rail: Label, up to two
// characters, in place of its initials or logo, Color, a name of the
// terminal's colors as "red" or "cyan", in place of its own, and Icon, the
// name of a picture in the project-icons folder beside the settings, in
// place of its logo. Each is "" for the project's own.
type TabStyle struct {
	Label string `json:"label,omitempty"`
	Color string `json:"color,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

// SetTabStyle keeps st as the look of the project of folder, forgetting
// it when st is the project's own.
func (s *Settings) SetTabStyle(folder string, st TabStyle) {
	if st == (TabStyle{}) {
		delete(s.ProjectTabStyle, folder)
		if len(s.ProjectTabStyle) == 0 {
			s.ProjectTabStyle = nil
		}
	} else {
		if s.ProjectTabStyle == nil {
			s.ProjectTabStyle = map[string]TabStyle{}
		}
		s.ProjectTabStyle[folder] = st
	}
	s.Changed()
}

// SyncIgnored's names of all the preferences, and of the keyboard
// shortcuts.
const (
	SyncSettings    = "settings.json"
	SyncKeybindings = "keybindings.json"
)

// MachineKept are the keys of the preferences settings sync leaves on
// this machine whatever SyncIgnored says, as a development build's looks,
// which would otherwise change the everyday Mayoi's.
var MachineKept []string

// SyncKept returns the keys of the preferences settings sync leaves on
// this machine, and whether it leaves the keyboard shortcuts.
func (p Prefs) SyncKept() (keys []string, keybindings bool) {
	all := slices.Contains(p.SyncIgnored, SyncSettings)
	for _, k := range PrefKeys {
		if all && !slices.Contains(unsynced, k) || slices.Contains(p.SyncIgnored, k) || slices.Contains(MachineKept, k) {
			keys = append(keys, k)
		}
	}
	return keys, slices.Contains(p.SyncIgnored, SyncKeybindings)
}

// SyncIgnoring returns SyncIgnored with name, a key or SyncSettings or
// SyncKeybindings, in it or out of it.
func (p Prefs) SyncIgnoring(name string, ignore bool) []string {
	out := slices.DeleteFunc(slices.Clone(p.SyncIgnored), func(k string) bool { return k == name })
	if ignore {
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Window is a window as the settings keep it: its folder, and its
// session of it.
type Window struct {
	// ID names the window's frame, which the next start gives it back.
	ID     int    `json:"id"`
	Folder string `json:"folder,omitempty"`
	Session
}

// Session is what a window shows of its folder: the columns of its editor
// area and what their tabs show, Column the one that had the keyboard,
// the panel, the folders open in the explorer, and its layout.
type Session struct {
	Layout
	Columns    []Column `json:"columns,omitempty"`
	Column     int      `json:"column,omitempty"`
	FileColumn int      `json:"fileColumn,omitempty"`
	// PanelTerms are the addresses in herdr-core of the panel's terminals
	// whose shells run there, which the next start attaches to again, and
	// PanelTerm the one of them the panel showed.
	PanelTerms []string `json:"panelTerms,omitempty"`
	PanelTerm  int      `json:"panelTerm,omitempty"`
	// PanelOpen, TerminalOpen and PanelHeight are a session of the version
	// before the views moved between the panels, which kept the bottom one
	// apart from them. Normalize makes its panel of them, and they go.
	PanelOpen    bool    `json:"panelOpen,omitempty"`
	TerminalOpen bool    `json:"terminalOpen,omitempty"`
	PanelHeight  float32 `json:"panelHeight,omitempty"`
	// Expanded are the explorer's folders showing their files, relative to
	// the window's folder.
	Expanded []string `json:"expanded,omitempty"`
	// Review is the review comments not sent yet.
	Review []ReviewNote `json:"review,omitempty"`
}

// ReviewNote is a review comment on lines of a file, as the settings keep
// it: Commit is the commit whose changes it is on, "" for the work tree,
// and Staged set for the changes staged. OldSide is set for lines removed,
// From and To the lines of the side, To after the last. Snippet is what
// the comment quotes, a diff when Diff is set, and Lines the lines as they
// were, which the comment finds again as the file changes.
type ReviewNote struct {
	// Context keeps a code snippet even when it has no comment.
	Context bool     `json:"context,omitempty"`
	Path    string   `json:"path"`
	Commit  string   `json:"commit,omitempty"`
	Staged  bool     `json:"staged,omitempty"`
	OldSide bool     `json:"oldSide,omitempty"`
	From    int      `json:"from"`
	To      int      `json:"to"`
	Diff    bool     `json:"diff,omitempty"`
	Snippet []string `json:"snippet,omitempty"`
	Lines   []string `json:"lines,omitempty"`
	Text    string   `json:"text"`
}

// The views a window shows, each in one of its panels.
const (
	ViewExplorer = "explorer"
	ViewSearch   = "search"
	ViewSCM      = "scm"
	ViewTerminal = "terminal"
)

// SidebarViews are the views that show on the layout's left, in the order
// of their tabs, as the terminal shows at the bottom.
var SidebarViews = []string{ViewExplorer, ViewSearch, ViewSCM}

// Layout is how a window lays its folder out: the panels beside and under
// its editors, by the side they show on.
type Layout struct {
	Panels Panels `json:"panels,omitempty"`
	// CommitFilesOpen shows the panel of the files of a commit's tab, hidden
	// at first, and CommitFilesWidth is how wide it is.
	CommitFilesOpen  bool    `json:"commitFilesOpen,omitempty"`
	CommitFilesWidth float32 `json:"commitFilesWidth,omitempty"`
	// ScmChangesRatio is the share of the multi-repository view for changes.
	// Zero fits the changes to their content, up to half the view.
	ScmChangesRatio float32 `json:"scmChangesRatio,omitempty"`
	// SidebarView, SidebarClosed, SidebarWidth, PanelHeight and PanelWidth
	// are a layout of the version before the views moved between the
	// panels, which kept the sidebar and the bottom panel apart from them.
	// Normalize makes panels of them, and they go.
	SidebarView   string  `json:"sidebarView,omitempty"`
	SidebarClosed bool    `json:"sidebarClosed,omitempty"`
	SidebarWidth  float32 `json:"sidebarWidth,omitempty"`
	// PanelHeight is a window's, and PanelWidth the panel at a side's;
	// a session keeps its own PanelHeight, which Normalize reads in its
	// place.
	PanelHeight float32 `json:"panelHeight,omitempty"`
	PanelWidth  float32 `json:"panelWidth,omitempty"`
}

// DefaultLayout is the layout of a first launch.
var DefaultLayout = Layout{Panels: DefaultPanels(), CommitFilesWidth: 240}

// DefaultPanels are the panels of a first launch: the file views on the
// layout's left, and the terminal under the editors, closed until it
// shows.
func DefaultPanels() Panels {
	return Panels{
		SideLeft: {Views: []string{ViewExplorer, ViewSearch, ViewSCM}, Active: ViewExplorer, Open: true,
			Size: DefaultPanelSize(SideLeft)},
		SideBottom: {Views: []string{ViewTerminal}, Active: ViewTerminal, Size: DefaultPanelSize(SideBottom)},
	}
}

// DefaultSide is the side a view shows on when nothing placed it: the
// terminal under the editors, the others on the left.
func DefaultSide(v string) Side {
	if v == ViewTerminal {
		return SideBottom
	}
	return SideLeft
}

// Side is a side of the editors a panel shows on: left of them, right of
// them, or under them.
type Side string

const (
	SideLeft   Side = "left"
	SideRight  Side = "right"
	SideBottom Side = "bottom"
)

// Sides are the sides a panel shows on, in the order a window lays them
// out.
var Sides = []Side{SideLeft, SideRight, SideBottom}

// Panel is a panel of a window: the views it holds, as tabs, the one it
// shows, whether it is open, and its size.
type Panel struct {
	// Views are the views the panel holds, in the order of their tabs.
	Views []string `json:"views,omitempty"`
	// Active is the view it shows, one of Views.
	Active string `json:"active,omitempty"`
	Open   bool   `json:"open,omitempty"`
	// Size is its width on a side of the editors, its height at the
	// bottom.
	Size float32 `json:"size,omitempty"`
}

// Shown is the view the panel shows: the one it last showed, or the first
// of its views.
func (p *Panel) Shown() string {
	if p == nil {
		return ""
	}
	if slices.Contains(p.Views, p.Active) {
		return p.Active
	}
	if len(p.Views) > 0 {
		return p.Views[0]
	}
	return ""
}

// Visible reports whether the panel shows: open, with a view to show.
func (p *Panel) Visible() bool { return p != nil && p.Open && len(p.Views) > 0 }

// Activate has the panel show one of its views, open.
func (p *Panel) Activate(v string) {
	if !slices.Contains(p.Views, v) {
		return
	}
	p.Active, p.Open = v, true
}

// Panels are a window's panels, by side. A panel is in the map from the
// first view placed on it, and stays after its last view leaves, keeping
// its size for the view that comes back; a panel with no view shows
// nothing.
type Panels map[Side]*Panel

// DefaultPanelSize is how big a panel starts: wide on a side of the
// editors, shorter at the bottom.
func DefaultPanelSize(s Side) float32 {
	switch s {
	case SideBottom:
		return 260
	case SideLeft:
		return 248
	}
	return 360
}

// Get is the panel on a side, nil when the window has none there.
func (ps Panels) Get(s Side) *Panel { return ps[s] }

// On is the panel on a side, made when the window has none there.
func (ps *Panels) On(s Side) *Panel {
	if p := (*ps)[s]; p != nil {
		return p
	}
	if *ps == nil {
		*ps = Panels{}
	}
	p := &Panel{Size: DefaultPanelSize(s)}
	(*ps)[s] = p
	return p
}

// SideOf is the side the panel that holds a view shows on, false when no
// panel holds it.
func (ps Panels) SideOf(v string) (Side, bool) {
	for _, s := range Sides {
		if p := ps[s]; p != nil && slices.Contains(p.Views, v) {
			return s, true
		}
	}
	return "", false
}

// Has reports whether a panel holds a view.
func (ps Panels) Has(v string) bool {
	_, ok := ps.SideOf(v)
	return ok
}

// Place moves a view to the panel on a side, which shows it, out of the
// panel it was in.
func (ps *Panels) Place(v string, s Side) *Panel {
	ps.Detach(v)
	p := ps.On(s)
	p.Views = append(slices.Clone(p.Views), v)
	p.Active, p.Open = v, true
	return p
}

// Detach takes a view out of the panel that holds it, which shows another
// of its views. A panel left with none keeps its size for the next view
// that comes, and shows nothing.
func (ps *Panels) Detach(v string) {
	for _, p := range *ps {
		if p == nil {
			continue
		}
		i := slices.Index(p.Views, v)
		if i < 0 {
			continue
		}
		p.Views = slices.Delete(slices.Clone(p.Views), i, i+1)
		if p.Active == v {
			p.Active = ""
			if len(p.Views) > 0 {
				p.Active = p.Views[0]
			}
		}
		if len(p.Views) == 0 {
			p.Open = false
		}
	}
}

// Copy returns the panels, and the views they hold, a copy.
func (ps Panels) Copy() Panels {
	if ps == nil {
		return nil
	}
	out := make(Panels, len(ps))
	for s, p := range ps {
		if p == nil {
			continue
		}
		c := *p
		c.Views = slices.Clone(p.Views)
		out[s] = &c
	}
	return out
}

// Normalize keeps each view in one panel, the view a panel shows one of
// its own, the panels on the sides there are and in their bounds, and a
// view no panel has on the side it starts on.
func (ps Panels) Normalize() {
	for s, p := range ps {
		if !slices.Contains(Sides, s) || p == nil {
			delete(ps, s)
		}
	}
	// A view shows in one panel: the first that has it.
	seen := make(map[string]bool, len(SidebarViews)+1)
	for _, s := range Sides {
		p := ps[s]
		if p == nil {
			continue
		}
		p.Views = slices.DeleteFunc(p.Views, func(v string) bool {
			if !isView(v) || seen[v] {
				return true
			}
			seen[v] = true
			return false
		})
	}
	// A view no panel has goes to the side it starts on, after the views
	// there.
	for _, v := range append(slices.Clone(SidebarViews), ViewTerminal) {
		if seen[v] {
			continue
		}
		p := ps.On(DefaultSide(v))
		p.Views = append(slices.Clone(p.Views), v)
		seen[v] = true
	}
	for _, s := range Sides {
		p := ps[s]
		if p == nil {
			continue
		}
		if len(p.Views) == 0 {
			// A panel with no view shows nothing, and keeps its size.
			p.Active, p.Open = "", false
		} else if !slices.Contains(p.Views, p.Active) {
			p.Active = p.Views[0]
		}
		if p.Size == 0 {
			p.Size = DefaultPanelSize(s)
		}
		if s == SideBottom {
			p.Size = clamp(p.Size, 80, 2000)
		} else {
			p.Size = clamp(p.Size, 160, 2000)
		}
	}
}

// isView reports whether v names a view a panel may hold: one of the
// window's, or a plugin's.
func isView(v string) bool {
	return v == ViewExplorer || v == ViewSearch || v == ViewSCM || v == ViewTerminal ||
		strings.HasPrefix(v, "plugin:")
}

// Normalize keeps the layout in its bounds, as base has it where it has
// none, and makes panels of the sidebar and the panel a version before the
// views moved between them kept apart.
func (l *Layout) Normalize(base Layout) {
	if l.CommitFilesWidth == 0 {
		l.CommitFilesWidth = base.CommitFilesWidth
	}
	l.CommitFilesWidth = clamp(l.CommitFilesWidth, 140, 800)
	// The sidebar's views, the one it showed and its width make the panel
	// on the left; the bottom panel's height and width, the panels on the
	// bottom and the side.
	if l.SidebarWidth != 0 || l.SidebarClosed || l.SidebarView != "" {
		p := l.Panels.On(SideLeft)
		p.Views = append(slices.Clone(p.Views), SidebarViews...)
		// The views are named by their ids: source control was "git", and
		// the explorer "".
		if l.SidebarView == "git" {
			l.SidebarView = ViewSCM
		}
		p.Active, p.Open = l.SidebarView, !l.SidebarClosed
		// A plugin's view was in the sidebar too, as a view of its own.
		if strings.HasPrefix(l.SidebarView, "plugin:") {
			p.Views = append(slices.Clone(p.Views), l.SidebarView)
		}
		if l.SidebarWidth != 0 {
			p.Size = clamp(l.SidebarWidth, 160, 600)
		}
	}
	if l.PanelHeight != 0 {
		l.Panels.On(SideBottom).Size = clamp(l.PanelHeight, 100, 2000)
	}
	if l.PanelWidth != 0 {
		l.Panels.On(SideRight).Size = clamp(l.PanelWidth, 160, 2000)
	}
	l.SidebarView, l.SidebarClosed, l.SidebarWidth = "", false, 0
	l.PanelHeight, l.PanelWidth = 0, 0
	if len(l.Panels) == 0 {
		l.Panels = base.Panels.Copy()
	}
	l.Panels.Normalize()
}

// Normalize keeps the session's layout in its bounds, and turns the bottom
// panel a version before the views moved between the panels kept apart
// into a panel of them.
func (s *Session) Normalize(base Layout) {
	open, terminal, height := s.PanelOpen, s.TerminalOpen, s.PanelHeight
	s.PanelOpen, s.TerminalOpen, s.PanelHeight = false, false, 0
	s.Layout.Normalize(base)
	if !terminal && !open && height == 0 {
		return
	}
	p := s.Panels.On(SideBottom)
	if terminal {
		p.Views = append(slices.Clone(p.Views), ViewTerminal)
		p.Active = ViewTerminal
	}
	if open {
		p.Open = true
	}
	if height != 0 {
		p.Size = height
	}
	s.Panels.Normalize()
}

// Column is a column of the editor area, as the settings keep it.
type Column struct {
	Tabs   []Tab   `json:"tabs"`
	Active int     `json:"active"`
	Weight float32 `json:"weight"`
	Locked bool    `json:"locked,omitempty"`
}

// Tab is a tab: the file it shows, or a terminal, which the next
// start opens anew, or attaches to again while its shell runs in
// herdr-core at HC.
type Tab struct {
	File     string `json:"file,omitempty"`
	Terminal bool   `json:"terminal,omitempty"`
	HC       string `json:"hc,omitempty"`
	// Preview is set for an SVG shown as a picture rather than as text.
	Preview bool `json:"preview,omitempty"`
	// Transient is set for the preview tab of its column, which the next
	// file previewed replaces.
	Transient bool `json:"transient,omitempty"`
	// Diff is set for the changes of File rather than the file: "head",
	// "index" or "staged", what they are against. With no File, it is
	// the changes of every file, against that. Layout is the diff's, as
	// the settings' DiffLayout; Split is what an older version kept for
	// "split". "commit:" and a hash is the changes of that commit.
	Diff     string `json:"diff,omitempty"`
	RepoRoot string `json:"repoRoot,omitempty"` // relative to the workspace; empty is its root repository
	Layout   string `json:"layout,omitempty"`
	Split    bool   `json:"split,omitempty"`
	// Full is set for a diff showing every unchanged line.
	Full bool `json:"full,omitempty"`
	// Sel is the selection of a file's editor, its caret at Sel[1], and
	// ScrollX and ScrollY where the tab was scrolled. Wrap is the tab's
	// own choice to wrap its lines, nil to follow WordWrap.
	Sel     [2]int  `json:"sel,omitzero"`
	ScrollX float32 `json:"scrollX,omitempty"`
	ScrollY float32 `json:"scrollY,omitempty"`
	Wrap    *bool   `json:"wrap,omitempty"`
	// Untitled is the file in the untitled folder beside the settings that
	// keeps the text of an untitled document as the app quit, and Number
	// the number of its name.
	Untitled string `json:"untitled,omitempty"`
	Number   int    `json:"number,omitempty"`
}

// MaxDiffContext is as many unchanged lines around a change as a diff
// shows, the rest folded away.
const MaxDiffContext = 100

// Default is the settings of a first launch.
func Default() *Settings {
	return &Settings{
		Prefs: Prefs{
			FontSize: 13, FontFamily: "monospace", FontWeight: 400, LineHeight: 1.55,
			AutoSave: true, Blame: true, TerminalCopyOnSelect: true, TerminalBackendHcShowInHerdr: true, PreviewMermaid: true, PreviewMath: true,
			DiffAlignedRows: true, DiffContext: 3, DiffSyncScroll: true, MergeAutoApply: true,
		},
	}
}

// Load reads the settings at path, and the state in state.json beside
// it, the defaults where they have none. The state an older version kept
// in the settings' file moves to the state's.
func Load(path string) *Settings {
	s := Default()
	s.path = path
	if path != "" {
		s.statePath = filepath.Join(filepath.Dir(path), "state.json")
	}
	var old legacy
	var layout oldLayout
	data, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(data, &s.Prefs)
		json.Unmarshal(data, &old)
		// The file as read is no edit of the person's to take again.
		s.written = data
	}
	moved := false
	if state, serr := os.ReadFile(s.statePath); serr == nil {
		json.Unmarshal(state, &s.State)
		json.Unmarshal(state, &layout)
	} else if err == nil {
		json.Unmarshal(data, &s.State)
		json.Unmarshal(data, &layout)
		moved = true
	}
	if old.DiffSplit && s.DiffLayout == "" {
		s.DiffLayout = "split"
	}
	s.normalize()
	if moved {
		s.migrate(old)
	}
	if s.layOut(layout) {
		moved = true
	}
	if moved {
		s.Flush()
	}
	return s
}

// layOut gives the windows and the sessions an older version kept the
// layout it kept for them all, and keeps each in its bounds.
func (s *Settings) layOut(old oldLayout) bool {
	base := DefaultLayout.Copy()
	left := base.Panels.Get(SideLeft)
	if old.SidebarWidth != 0 {
		left.Size = old.SidebarWidth
	}
	if old.PanelHeight != 0 {
		base.Panels.Get(SideBottom).Size = old.PanelHeight
	}
	// The views are named by their ids: source control was "git", and the
	// explorer "".
	if old.SidebarView == "git" {
		old.SidebarView = ViewSCM
	}
	if old.SidebarView == ViewSearch || old.SidebarView == ViewSCM || old.SidebarView == ViewExplorer {
		left.Active = old.SidebarView
	}
	if old.SidebarOpen != nil {
		left.Open = *old.SidebarOpen
	}
	for _, w := range s.Windows {
		w.Normalize(base)
		s.placePanel(w.Panels)
	}
	for _, ses := range s.Sessions {
		ses.Normalize(base)
		s.placePanel(ses.Panels)
	}
	// The layout takes the position from here: a pref that stays would
	// move the terminal back on every start.
	if s.PanelPosition != "" {
		s.PanelPosition = ""
		return true
	}
	return false
}

// placePanel puts the terminal where the version before the views moved
// between the panels put its panel, and forgets the choice.
func (s *Settings) placePanel(ps Panels) {
	switch Side(s.PanelPosition) {
	case SideLeft, SideRight:
		ps.Place(ViewTerminal, Side(s.PanelPosition))
	}
}

// Zoom levels keep the content within the UI's supported scale range.
const MinZoomLevel, MaxZoomLevel = -5, 8

// normalize keeps the preferences in their bounds, the defaults where the
// file leaves them out or makes no sense.
func (s *Settings) normalize() { s.Prefs.normalize() }

func (p *Prefs) normalize() {
	p.ZoomLevel = clamp(p.ZoomLevel, MinZoomLevel, MaxZoomLevel)
	p.FontSize = clamp(p.FontSize, 9, 28)
	if strings.TrimSpace(p.FontFamily) == "" {
		p.FontFamily = "monospace"
	}
	if p.FontWeight == 0 {
		p.FontWeight = 400
	}
	p.FontWeight = min(max(p.FontWeight, 100), 900)
	if p.LineHeight == 0 {
		p.LineHeight = 1.55
	}
	p.LineHeight = clamp(p.LineHeight, 1, 3)
	p.FontFeatures = slices.DeleteFunc(p.FontFeatures, func(f string) bool { return strings.TrimSpace(f) == "" })
	p.AISecretFiles = slices.DeleteFunc(p.AISecretFiles, func(f string) bool { return strings.TrimSpace(f) == "" })
	if p.TerminalFontSize != 0 {
		p.TerminalFontSize = clamp(p.TerminalFontSize, 9, 28)
	}
	switch p.DiffHighlight {
	case "line", "char":
	default:
		p.DiffHighlight = ""
	}
	switch p.DiffWhitespace {
	case "eol", "all", "blank":
	default:
		p.DiffWhitespace = ""
	}
	p.DiffContext = min(max(p.DiffContext, 0), MaxDiffContext)

	// "" and "bottom" are the same panel position.
	if p.PanelPosition != "left" && p.PanelPosition != "right" {
		p.PanelPosition = ""
	}

	if p.TerminalLocation != "editor" {
		p.TerminalLocation = ""
	}
	if p.TerminalBackend != "hc" {
		p.TerminalBackend = ""
	}
	// The themes were appearances once.
	if p.Appearance == "matrix" || p.Appearance == "claude" {
		p.Theme, p.Appearance = p.Appearance, ""
	}
}

// migrate makes a window of the one an older version kept.
func (s *Settings) migrate(old legacy) {
	if len(s.Windows) > 0 || old.Folder == "" && len(old.Columns) == 0 && len(old.OpenFiles) == 0 {
		return
	}
	w := &Window{Folder: old.Folder, Session: Session{
		Columns: old.Columns, Column: old.Column, PanelOpen: old.PanelOpen, TerminalOpen: old.TerminalOpen,
	}}
	if len(w.Columns) == 0 && len(old.OpenFiles) > 0 {
		// The files of one column.
		cs := Column{Active: -1, Weight: 1}
		for _, p := range old.OpenFiles {
			if p == old.ActiveFile {
				cs.Active = len(cs.Tabs)
			}
			cs.Tabs = append(cs.Tabs, Tab{File: p})
		}
		w.Columns, w.Column = []Column{cs}, 0
	}
	s.Windows = []*Window{w}
}

// Path is the file the settings are kept in, "" for none.
func (s *Settings) Path() string { return s.path }

// NewWindow keeps a new window, focused first, and names its frame by
// the first ID no other window has. It is laid out as the window focused
// last is.
func (s *Settings) NewWindow() *Window {
	id := 0
	for slices.ContainsFunc(s.Windows, func(w *Window) bool { return w.ID == id }) {
		id++
	}
	w := &Window{ID: id}
	w.Layout = DefaultLayout.Copy()
	if len(s.Windows) > 0 {
		w.Layout = s.Windows[0].Layout.Copy()
	}
	s.Windows = slices.Insert(s.Windows, 0, w)
	s.Changed()
	return w
}

// Copy returns the layout, and the views its panels hold, a copy, which a
// window moves without the window it was laid out as.
func (l Layout) Copy() Layout {
	l.Panels = l.Panels.Copy()
	return l
}

// InRail returns the windows in the order of the rail of project tabs:
// those it has in its order, then the others as the windows are.
func (s *Settings) InRail() []*Window {
	ws := slices.Clone(s.Windows)
	at := func(w *Window) int {
		if i := slices.Index(s.Rail, w.ID); i >= 0 {
			return i
		}
		return len(s.Rail)
	}
	slices.SortStableFunc(ws, func(a, b *Window) int { return at(a) - at(b) })
	return ws
}

// SetRail keeps the order of the windows on the rail of project tabs.
func (s *Settings) SetRail(ws []*Window) {
	ids := make([]int, len(ws))
	for i, w := range ws {
		ids[i] = w.ID
	}
	if !slices.Equal(ids, s.Rail) {
		s.Rail = ids
		s.Changed()
	}
}

// Focused puts w first, as the window Focused last.
func (s *Settings) Focused(w *Window) {
	i := slices.Index(s.Windows, w)
	if i <= 0 {
		return
	}
	s.Windows = slices.Insert(slices.Delete(s.Windows, i, i+1), 0, w)
	s.Changed()
}

// Forget lets go of a window that closed, keeping its session for the
// next window to open its folder.
func (s *Settings) Forget(w *Window) {
	s.Stash(w)
	s.Windows = slices.DeleteFunc(s.Windows, func(o *Window) bool { return o == w })
	s.Changed()
}

// Stash keeps the session of w's folder, which the folder's next window
// gets back, while the folder is a recent one. A session with no tabs
// and no folders open in the explorer lets the folder's go.
func (s *Settings) Stash(w *Window) {
	if w.Folder == "" {
		return
	}
	dir := filepath.Clean(w.Folder)
	if !slices.Contains(s.Recent, dir) {
		return
	}
	empty := len(w.Expanded) == 0 && len(w.Review) == 0
	for _, c := range w.Columns {
		empty = empty && len(c.Tabs) == 0
	}
	if empty {
		delete(s.Sessions, dir)
	} else {
		if s.Sessions == nil {
			s.Sessions = map[string]*Session{}
		}
		// The session keeps the layout, and the views its panels hold, a
		// copy: the window moves on without it.
		ses := w.Session
		ses.Layout = w.Layout.Copy()
		s.Sessions[dir] = &ses
	}
	s.Changed()
}

// TakeSession returns the session kept of dir, which is no longer kept
// once a window has it, or nil for none.
func (s *Settings) TakeSession(dir string) *Session {
	dir = filepath.Clean(dir)
	ses := s.Sessions[dir]
	if ses != nil {
		delete(s.Sessions, dir)
		s.Changed()
	}
	return ses
}

func clamp(v, lo, hi float32) float32 { return max(lo, min(v, hi)) }

// Changed saves the settings a moment after the last change.
func (s *Settings) Changed() {
	prefs, state := s.Encode()
	s.mu.Lock()
	var fn func()
	if !bytes.Equal(prefs, s.prefsSeen) {
		s.gen++
		s.prefsSeen = prefs
		fn = s.prefsChanged
	}
	if s.path != "" {
		if s.timer != nil {
			s.timer.Stop()
		}
		s.seq++
		seq := s.seq
		s.timer = time.AfterFunc(400*time.Millisecond, func() { s.write(seq, prefs, state) })
	}
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// Gen counts the changes of the preferences: what is made of them, as
// the preferences of a folder over them, is made again as it changes.
func (s *Settings) Gen() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen
}

// OnPrefsChanged calls fn each time Changed finds the preferences
// changed, as a menu changes one, on the goroutine of Changed.
func (s *Settings) OnPrefsChanged(fn func()) {
	s.mu.Lock()
	s.prefsChanged = fn
	s.mu.Unlock()
}

// Flush saves the settings now, as the app quits.
func (s *Settings) Flush() {
	if s.path == "" {
		return
	}
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
	}
	prefs, state := s.Encode()
	s.seq++
	seq := s.seq
	s.mu.Unlock()
	s.write(seq, prefs, state)
}

// Encode is the preferences as the settings' file keeps them, and the
// state as the state's does.
func (s *Settings) Encode() (prefs, state []byte) {
	prefs, _ = json.MarshalIndent(s.Prefs, "", "  ")
	state, _ = json.MarshalIndent(s.State, "", "  ")
	return prefs, state
}

// write writes the files that changed since the settings last wrote
// them, as of seq: most changes are of the state only. One of an older
// encoding than the last written, as a timer that fired as Flush ran,
// writes nothing.
func (s *Settings) write(seq uint64, prefs, state []byte) {
	s.wmu.Lock()
	if seq < s.wrote {
		s.wmu.Unlock()
		return
	}
	s.wrote = seq
	s.mu.Lock()
	samePrefs, sameState := bytes.Equal(prefs, s.written), bytes.Equal(state, s.stateWritten)
	s.written, s.stateWritten = prefs, state
	fn := s.prefsWritten
	s.mu.Unlock()
	if !samePrefs {
		WriteFile(s.path, prefs)
	}
	if !sameState && s.statePath != "" {
		WriteFile(s.statePath, state)
	}
	s.wmu.Unlock()
	// Outside wmu, as fn may wait for the main thread, which may wait to
	// write.
	if !samePrefs && fn != nil {
		fn()
	}
}

// WriteFile writes data to path through a file of its own beside it, so
// that a watcher never reads half of it, nor two writers' halves.
func WriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	os.MkdirAll(dir, 0o755)
	f, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0o644)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// AddRecent puts dir first of the recent folders, of which it keeps ten,
// and lets go of the sessions of those no longer recent.
func (s *Settings) AddRecent(dir string) {
	s.Recent = slices.DeleteFunc(s.Recent, func(d string) bool { return d == dir })
	s.Recent = slices.Insert(s.Recent, 0, dir)
	if len(s.Recent) > 10 {
		s.Recent = s.Recent[:10]
	}
	for d := range s.Sessions {
		if !slices.Contains(s.Recent, d) {
			delete(s.Sessions, d)
		}
	}
}

// RecentExcept returns the recent folders but dir, and those that are not
// on the disk now, which stay in the list for when they come back, as a
// drive plugged in again.
func (s *Settings) RecentExcept(dir string) []string {
	var out []string
	for _, d := range s.Recent {
		if d == dir {
			continue
		}
		// A remote folder is looked at as it opens.
		if strings.HasPrefix(d, "/ssh:") {
			out = append(out, d)
		} else if info, err := os.Stat(d); err == nil && info.IsDir() {
			out = append(out, d)
		}
	}
	return out
}

// ClearRecent forgets the recent folders, and the sessions kept of them.
func (s *Settings) ClearRecent() {
	s.Recent, s.Sessions = nil, nil
	s.Changed()
}

// SetPrefs makes p the preferences, as a change of the file has them,
// and returns those it replaces. A change of WordWrap counts in WrapGen.
func (s *Settings) SetPrefs(p Prefs) (old Prefs) {
	old = s.Prefs
	s.Prefs = p
	if p.WordWrap != old.WordWrap {
		s.WrapGen++
	}
	s.mu.Lock()
	s.gen++
	s.mu.Unlock()
	return old
}

// Watch calls changed, on a goroutine of its own, with the preferences
// of the file each time something other than the settings writes it, as
// a person saving it in an editor. A file that does not parse is let be
// until it does. stop stops watching.
func (s *Settings) Watch(changed func(Prefs)) (stop func(), err error) {
	if s.path == "" {
		return func() {}, nil
	}
	dir := filepath.Dir(s.path)
	os.MkdirAll(dir, 0o755)
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	// The folder rather than the file, which saves replace: the settings'
	// own, and most editors'.
	if err := w.Add(dir); err != nil {
		w.Close()
		return nil, err
	}
	go func() {
		var settle <-chan time.Time
		for {
			select {
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if filepath.Clean(ev.Name) == filepath.Clean(s.path) && ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
					// Once a save that writes the file in pieces is done.
					settle = time.After(80 * time.Millisecond)
				}
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			case <-settle:
				settle = nil
				if p, ok := s.edited(); ok {
					changed(p)
				}
			}
		}
	}()
	return func() { w.Close() }, nil
}

// edited reads the preferences of the file, unless the settings wrote it
// as it is or it does not parse.
func (s *Settings) edited() (Prefs, bool) {
	_, p, ok := s.read()
	return p, ok
}

// Edited returns the preferences of the file as a person saved it, which
// Watch may not have told of yet, and takes the file as read: Watch then
// tells of it only as it changes again. ok is false for a file the
// settings wrote as it is, or that does not parse.
func (s *Settings) Edited() (p Prefs, ok bool) {
	if s.path == "" {
		return Prefs{}, false
	}
	// Not as a write lands, whose file this would take for the person's.
	s.wmu.Lock()
	defer s.wmu.Unlock()
	data, p, ok := s.read()
	if ok {
		s.mu.Lock()
		s.written = data
		s.mu.Unlock()
	}
	return p, ok
}

func (s *Settings) read() ([]byte, Prefs, bool) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, Prefs{}, false
	}
	s.mu.Lock()
	ours := bytes.Equal(data, s.written)
	s.mu.Unlock()
	if ours {
		return nil, Prefs{}, false
	}
	f := Default()
	if json.Unmarshal(data, &f.Prefs) != nil {
		return nil, Prefs{}, false
	}
	f.normalize()
	return data, f.Prefs, true
}

// OnPrefsWritten calls fn, on a goroutine of its own, each time the
// settings write a change of the preferences to their file, as a menu
// changing one, though not as the person saves the file: Watch tells of
// that.
func (s *Settings) OnPrefsWritten(fn func()) {
	s.mu.Lock()
	s.prefsWritten = fn
	s.mu.Unlock()
}

// unsynced are the preferences that stay on each machine: what the
// windows remember of source control's panels rather than what a person
// chooses, what the person keeps on the machine, the looks of the
// projects on the rail, by folders of this machine, and the hc to run
// with its environment, which may hold secrets and runs as a terminal
// opens.
var unsynced = []string{"scmRepositoryOpen", "scmRemotes", "syncIgnored", "projectTabStyle", "terminalBackendHcPath", "terminalBackendHcEnv"}

// PrefKeys are the keys of the preferences in the settings' file.
var PrefKeys = func() []string {
	var keys []string
	t := reflect.TypeFor[Prefs]()
	for f := range t.Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name != "" && name != "-" {
			keys = append(keys, name)
		}
	}
	return keys
}()

// Synced returns the preferences that sync between machines, by their
// keys in the settings' file. Keys at their zero value with omitempty are
// left out, as the file leaves them out.
func (p Prefs) Synced() map[string]json.RawMessage {
	data, _ := json.Marshal(p)
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)
	for _, k := range unsynced {
		delete(m, k)
	}
	return m
}

// WithSynced returns the preferences of synced, by their keys in the
// settings' file, the defaults where it has none, and p's of those that
// do not sync or that it leaves on this machine.
func (p Prefs) WithSynced(synced map[string]json.RawMessage) (Prefs, error) {
	keep, _ := p.SyncKept()
	keep = append(keep, unsynced...)
	mine, _ := json.Marshal(p)
	var own map[string]json.RawMessage
	json.Unmarshal(mine, &own)
	m := make(map[string]json.RawMessage, len(synced)+len(keep))
	for k, v := range synced {
		if !slices.Contains(keep, k) {
			m[k] = v
		}
	}
	for _, k := range keep {
		if v, ok := own[k]; ok {
			m[k] = v
		}
	}
	f := Default()
	data, err := json.Marshal(m)
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(data, &f.Prefs); err != nil {
		return p, err
	}
	f.normalize()
	return f.Prefs, nil
}

// IsDefault reports whether the preferences that sync are those of a
// first launch.
func (p Prefs) IsDefault() bool {
	d := Default().Prefs.Synced()
	m := p.Synced()
	if len(d) != len(m) {
		return false
	}
	for k, v := range m {
		if !bytes.Equal(d[k], v) {
			return false
		}
	}
	return true
}
