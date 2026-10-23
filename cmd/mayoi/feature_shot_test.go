package main

// Screenshots of every feature, in a dark and a light window at 2×, for the
// README. Run scripts/screenshots/shoot.sh, or
// FSHOTS=<dir> go test -run TestFeat ./cmd/mayoi

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/remote"
	"mayoi/internal/settings"
)

const featW, featH = 1440, 900

func featDir(t *testing.T) string {
	dir := os.Getenv("FSHOTS")
	if dir == "" {
		t.Skip("FSHOTS is not set")
	}
	return dir
}

// featShell makes terminals start a zsh with a short, colorful prompt.
func featShell(t *testing.T) {
	if _, err := os.Stat("/bin/zsh"); err != nil {
		t.Skip("no zsh")
	}
	z := t.TempDir()
	os.WriteFile(filepath.Join(z, ".zshrc"), []byte(`
PROMPT='%F{cyan}%1~%f %F{magenta}main%f %F{green}❯%f '
RPROMPT=''
export CLICOLOR=1 LSCOLORS=ExGxFxdaCxDaDahbadacec
alias ls='ls -G'
`), 0o644)
	t.Setenv("ZDOTDIR", z)
	t.Setenv("SHELL", "/bin/zsh")
}

// featRepo clones Mayoi into a folder named mayoi.
func featRepo(t *testing.T) string {
	root := filepath.Join(t.TempDir(), "mayoi")
	featClone(t, root)
	return root
}

// featClone clones Mayoi into root, on main whichever branch the shots are
// taken from.
func featClone(t *testing.T, root string) {
	out, err := exec.Command("git", "clone", "-q", "--no-hardlinks", "--single-branch", "../..", root).CombinedOutput()
	if err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	branch, _ := exec.Command("git", "-C", root, "branch", "--show-current").Output()
	for _, args := range [][]string{
		{"checkout", "-q", "-B", "main"},
		{"update-ref", "refs/remotes/origin/main", "HEAD"},
		{"symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if b := strings.TrimSpace(string(branch)); b != "main" {
		exec.Command("git", "-C", root, "branch", "-q", "-D", b).Run()
		exec.Command("git", "-C", root, "update-ref", "-d", "refs/remotes/origin/"+b).Run()
	}
}

type featOpts struct {
	theme    string
	expanded []string
	w, h     int
	root     string
}

func featHarness(t *testing.T, dark bool, o featOpts) *harness {
	root := o.root
	if root == "" {
		root = featRepo(t)
	}
	s := settings.Default()
	w := s.NewWindow()
	w.Folder = root
	w.Expanded = o.expanded
	h := &fakeHost{folder: root}
	a := newApp(h, s, w)
	t.Cleanup(a.shutdown)
	if o.w == 0 {
		o.w, o.h = featW, featH
	}
	tt := ui.NewTester(a.view, o.w, o.h)
	tt.SetDark(dark)
	tt.SetScale(2)
	hs := &harness{t: t, a: a, h: h, tt: tt, root: root}
	t.Cleanup(a.closeTerminals)
	hs.settle()
	if o.theme != "" {
		a.setTheme(o.theme)
	}
	hs.settleGit()
	return hs
}

func (hs *harness) fshot(name string, dark bool) {
	dir := os.Getenv("FSHOTS")
	suffix := "-light"
	if dark {
		suffix = "-dark"
	}
	old := os.Getenv("SHOTS")
	os.Setenv("SHOTS", dir)
	hs.shot(name + suffix)
	os.Setenv("SHOTS", old)
}

// caretAt puts the caret, or a selection, at the first of what in d.
func (hs *harness) caretAt(d *editor, what string, off, n int) {
	s := d.buf.String()
	i := strings.Index(s, what)
	if i < 0 {
		hs.t.Fatalf("no %q", what)
	}
	at := len([]rune(s[:i])) + off
	hs.place(d, at, at+n)
}

// scrollTo puts line of at, less above, at the top of d's view.
func (hs *harness) scrollTo(d *editor, what string, above int) {
	s := d.buf.String()
	i := strings.Index(s, what)
	line := d.buf.Snapshot().LineAt(len([]rune(s[:i])))
	d.vscroll.Y = editorPadY + float32(max(line-above, 0))*hs.a.font.lineH
	hs.settle()
}

func (hs *harness) termRun(s *session, cmd, want string) {
	hs.tt.Type(cmd)
	hs.tt.Key(0, ui.KeyEnter)
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(s.term.Text(), want) && time.Now().Before(deadline) {
		hs.settle()
	}
	hs.settle()
}

func featBoth(t *testing.T, f func(t *testing.T, dark bool)) {
	featDir(t)
	for _, dark := range []bool{true, false} {
		f(t, dark)
	}
}

// 01: the hero, two columns and a terminal.
func TestFeatHero(t *testing.T) {
	featShell(t)
	featBoth(t, func(t *testing.T, dark bool) {
		hs := featHarness(t, dark, featOpts{expanded: []string{"cmd", "cmd/mayoi"}})
		a := hs.a
		a.openFile(hs.path("cmd/mayoi/editor.go"), true)
		hs.settle()
		d := a.activeEditor()
		hs.scrollTo(d, "func newCodeFont", 4)
		hs.caretAt(d, "func newCodeFont", 5, 11)
		a.run("view.splitRight")
		a.openFile(hs.path("cmd/mayoi/wrap.go"), true)
		hs.settle()
		a.run("view.terminal")
		hs.settle()
		hs.termRun(a.terms[0], "git log --oneline -6", "Open the folders")
		hs.fshot("01-hero", dark)
		a.closeTerminals()
	})
}

// gitIn runs git in root, as a test user.
func gitIn(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.email=yunqian@example.com", "-c", "user.name=yunqian"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func editFile(t *testing.T, path string, f func(string) string) {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte(f(string(b))), 0o644)
}

// featChanges changes the work tree in each way source control lists.
func featChanges(t *testing.T, root string) {
	p := func(rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }
	editFile(t, p("cmd/mayoi/wrap.go"), func(s string) string {
		s = strings.Replace(s, "// wraps reports whether an editor wraps its lines: as its tab chose, or\n// else as the settings say.\n",
			"// wraps reports whether an editor wraps its lines.\n//\n// A tab's own choice, made with ⌥Z, wins over the settings until\n// they change again.\n", 1)
		s = strings.Replace(s, "\ts.WordWrap = !s.WordWrap\n", "\ts.WordWrap = !s.WordWrap\n\ta.toast(\"Word wrap \" + onOff(s.WordWrap))\n", 1)
		return s
	})
	editFile(t, p("README.md"), func(s string) string {
		return strings.Replace(s, "a small, fast code editor written in Go", "a small, fast and friendly code editor written in Go", 1)
	})
	editFile(t, p("internal/linediff/linediff.go"), func(s string) string {
		return strings.Replace(s, "package linediff", "// Package linediff diffs texts line by line, as Git does.\npackage linediff", 1)
	})
	gitIn(t, root, "add", "internal/linediff/linediff.go")
	os.Remove(p("scripts/dev.sh"))
	os.WriteFile(p("docs/ROADMAP.md"), []byte("# Roadmap\n\n- [ ] Multiple cursors\n- [ ] Language servers\n"), 0o644)
	os.MkdirAll(p("build/darwin-arm64"), 0o755)
	os.WriteFile(p("build/darwin-arm64/Mayoi.dmg"), []byte("x"), 0o644)
}

// 02: the explorer, with Git's colors and ignored files faint.
func TestFeatExplorer(t *testing.T) {
	featBoth(t, func(t *testing.T, dark bool) {
		root := featRepo(t)
		featChanges(t, root)
		hs := featHarness(t, dark, featOpts{root: root, expanded: []string{"build", "build/darwin-arm64", "docs", "internal", "internal/linediff", "scripts"}})
		a := hs.a
		a.openFile(hs.path("docs/ROADMAP.md"), true)
		hs.settleGit()
		hs.tt.Move(120, 400)
		hs.settle()
		hs.fshot("02-explorer", dark)
	})
}

// 03: reading aids: indent guides, the bracket, the word at the caret.
func TestFeatReadingAids(t *testing.T) {
	featBoth(t, func(t *testing.T, dark bool) {
		hs := featHarness(t, dark, featOpts{})
		a := hs.a
		a.openFile(hs.path("cmd/mayoi/editor.go"), true)
		hs.settle()
		d := a.activeEditor()
		hs.scrollTo(d, "func newCodeFont", 6)
		hs.caretAt(d, "f.charW = w / 64", 2, 0)
		hs.fshot("03-reading-aids", dark)
		hs.caretAt(d, "w, _ := ui.ShapeText", 0, 120)
		hs.fshot("03-selection", dark)
	})
}

// 04: sticky scroll and breadcrumbs.
func TestFeatSticky(t *testing.T) {
	featBoth(t, func(t *testing.T, dark bool) {
		hs := featHarness(t, dark, featOpts{})
		a := hs.a
		a.openFile(hs.path("cmd/mayoi/editor.go"), true)
		hs.settle()
		d := a.activeEditor()
		hs.caretAt(d, "for _, w := range deco.words", 0, 0)
		hs.scrollTo(d, "for _, w := range deco.words", 10)
		hs.fshot("04-sticky-scroll", dark)
	})
}

// 05: word wrap.
func TestFeatWrap(t *testing.T) {
	featBoth(t, func(t *testing.T, dark bool) {
		hs := featHarness(t, dark, featOpts{expanded: []string{"docs"}})
		a := hs.a
		a.openFile(hs.path("docs/FEATURES.md"), true)
		hs.settle()
		a.run("view.wordWrap")
		hs.settle()
		d := a.activeEditor()
		hs.scrollTo(d, "## Source control", 0)
		hs.fshot("05-word-wrap", dark)
		a.run("view.wordWrap")
	})
}

// 06: vim mode, in visual line mode.
func TestFeatVim(t *testing.T) {
	featBoth(t, func(t *testing.T, dark bool) {
		hs := featHarness(t, dark, featOpts{})
		a := hs.a
		a.toggleVim()
		a.openFile(hs.path("cmd/mayoi/wrap.go"), true)
		hs.settle()
		d := a.activeEditor()
		hs.caretAt(d, "func (a *app) toggleWordWrap", 0, 0)
		a.focusColumn()
		hs.vim("Vjjjj")
		hs.settle()
		hs.fshot("06-vim-visual", dark)
		hs.vim("<Esc>")
		hs.caretAt(d, "s := a.settings", 0, 0)
		hs.vim("w")
		hs.settle()
		hs.fshot("06-vim-normal", dark)
		// A search as it is typed: its matches, and the command line.
		hs.vim("/")
		hs.tt.Type("settings")
		hs.settle()
		hs.fshot("06-vim-search", dark)
		hs.tt.Key(0, ui.KeyEnter)
		hs.settle()
		hs.vim(":")
		hs.tt.Type("%s/settings/s/g")
		hs.settle()
		hs.fshot("06-vim-command", dark)
		hs.tt.Key(0, ui.KeyEscape)
		hs.settle()
		// A register recording, in the status bar.
		hs.vim("qa")
		hs.settle()
		hs.fshot("06-vim-record", dark)
		hs.vim("q")
	})
}

// 07: the gutter's marks, a change peeked, and blame.
func TestFeatGutter(t *testing.T) {
	featBoth(t, func(t *testing.T, dark bool) {
		root := featRepo(t)
		featChanges(t, root)
		hs := featHarness(t, dark, featOpts{root: root})
		a := hs.a
		a.openFile(hs.path("cmd/mayoi/wrap.go"), true)
		hs.settleGit()
		d := a.activeEditor()
		hs.caretAt(d, "return a.settings.WordWrap", 0, 0)
		hs.until("blame", func() bool { step(hs.tt); return d.blame.done })
		hs.settle()
		hs.fshot("07-blame-gutter", dark)
		s := d.buf.String()
		line := d.buf.Snapshot().LineAt(len([]rune(s[:strings.Index(s, "A tab's own")])))
		d.peekHunk(line)
		hs.settle()
		hs.fshot("07-change-peek", dark)
	})
}

// 08: source control and the diffs.
func TestFeatSourceControl(t *testing.T) {
	featBoth(t, func(t *testing.T, dark bool) {
		root := featRepo(t)
		featChanges(t, root)
		hs := featHarness(t, dark, featOpts{root: root})
		a := hs.a
		a.window.SidebarWidth = 330
		a.run("view.scm")
		hs.settleGit()
		a.git.message = "Say what word wrap does as it toggles"
		a.showChanges(diffTarget{path: hs.path("cmd/mayoi/wrap.go"), base: diffIndex}, true)
		hs.settleGit()
		hs.settle()
		a.setDiffLayout(a.activeTab().diff, layoutInline)
		hs.settle()
		hs.fshot("08-scm-diff-inline", dark)
		if v := a.activeTab().diff; v != nil {
			for i, r := range v.rows {
				if r.changed() && v.files[r.file].path == hs.path("cmd/mayoi/wrap.go") {
					y := titleBarH + tabStripH + crumbsH + editorPadY + float32(v.tops[i])*a.font.lineH - v.scroll.Y + a.font.lineH/2
					hs.tt.Move(a.window.SidebarWidth+400, y)
					break
				}
			}
			hs.settle()
			hs.fshot("08-scm-hunk-actions", dark)
		}
		a.setDiffLayout(a.activeTab().diff, layoutSplit)
		hs.settle()
		hs.fshot("08-scm-diff-split", dark)
		a.settings.ScmTree = true
		hs.tt.Move(150, 300)
		hs.settle()
		hs.fshot("08-scm-tree", dark)
		a.settings.ScmTree = false
	})
}

// featHistory adds a merged branch, tags and unpushed commits.
func featHistory(t *testing.T, root string) {
	p := func(rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }
	gitIn(t, root, "tag", "v1.0.0")
	gitIn(t, root, "switch", "-q", "-c", "feature/minimap")
	editFile(t, p("cmd/mayoi/decorations.go"), func(s string) string {
		return s + "\n// minimap draws the file small, along the right edge.\nfunc minimap() {}\n"
	})
	gitIn(t, root, "commit", "-qam", "Draw a minimap along the right edge")
	editFile(t, p("cmd/mayoi/decorations.go"), func(s string) string {
		return s + "\n// minimapWidth is the minimap's width, in points.\nconst minimapWidth = 80\n"
	})
	gitIn(t, root, "commit", "-qam", "Make the minimap 80 points wide")
	gitIn(t, root, "switch", "-q", "main")
	editFile(t, p("README.md"), func(s string) string {
		return strings.Replace(s, "a small, fast code editor", "a small, fast and friendly code editor", 1)
	})
	gitIn(t, root, "commit", "-qam", "Call Mayoi friendly in the README")
	gitIn(t, root, "merge", "-q", "--no-ff", "-m", "Merge branch 'feature/minimap'", "feature/minimap")
	gitIn(t, root, "tag", "v1.1.0")
	for _, b := range []string{"fix/scroll-jump", "feature/lsp", "release/1.1"} {
		gitIn(t, root, "branch", b, "HEAD~2")
	}
	editFile(t, p("cmd/mayoi/wrap.go"), func(s string) string {
		return strings.Replace(s, "// else as the settings say.", "// else as the settings say, in every column.", 1)
	})
	gitIn(t, root, "commit", "-qam", "Wrap the lines of every column alike\n\nA tab's own choice still wins until the setting changes.")
}

// 09: the commits on their graph, a commit's changes, and the branches.
func TestFeatCommits(t *testing.T) {
	featBoth(t, func(t *testing.T, dark bool) {
		root := featRepo(t)
		featHistory(t, root)
		hs := featHarness(t, dark, featOpts{root: root})
		a := hs.a
		a.window.SidebarWidth = 330
		a.run("view.scm")
		hs.settleGit()
		a.showCommit(a.git.history[0], false)
		hs.settleGit()
		hs.settle()
		hs.fshot("09-commits", dark)
		a.settings.ScmAllBranches = true
		a.toggleAllBranches()
		a.toggleAllBranches()
		hs.settleGit()
		a.openBranchPicker()
		hs.settle()
		hs.fshot("09-branch-picker", dark)
	})
}

const featSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="320" height="200" viewBox="0 0 320 200">
  <defs>
    <linearGradient id="sky" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#7c5cff"/>
      <stop offset="1" stop-color="#ff6fb5"/>
    </linearGradient>
  </defs>
  <rect width="320" height="200" rx="28" fill="url(#sky)"/>
  <circle cx="240" cy="62" r="26" fill="#ffe08a"/>
  <path d="M0 160 L80 96 L140 140 L200 84 L320 168 L320 200 L0 200 Z" fill="#2a1f5c" opacity="0.85"/>
  <text x="28" y="56" font-family="Helvetica" font-size="30" font-weight="700" fill="#fff">迷い</text>
</svg>
`

// 10: the image viewer, the SVG preview and the Markdown preview.
func TestFeatViewers(t *testing.T) {
	featBoth(t, func(t *testing.T, dark bool) {
		hs := featHarness(t, dark, featOpts{expanded: []string{"resources", "docs"}})
		a := hs.a
		a.openFile(hs.path("resources/icon.png"), true)
		hs.settle()
		hs.until("image", func() bool { v := a.activeViewer(); return v != nil && v.src != nil })
		hs.settle()
		hs.fshot("10-image-viewer", dark)

		os.WriteFile(hs.path("resources/mountains.svg"), []byte(featSVG), 0o644)
		a.openFile(hs.path("resources/mountains.svg"), true)
		hs.settle()
		a.run("view.splitRight")
		hs.settle()
		a.run("view.togglePreview")
		d := a.activeEditor()
		hs.until("svg", func() bool { return d.preview != nil && d.preview.src != nil })
		hs.settle()
		hs.fshot("10-svg-preview", dark)
	})
}

func TestFeatMarkdown(t *testing.T) {
	featBoth(t, func(t *testing.T, dark bool) {
		hs := featHarness(t, dark, featOpts{})
		a := hs.a
		a.run("view.sidebar")
		os.WriteFile(hs.path("docs/GUIDE.md"), []byte(featMD), 0o644)
		a.openFile(hs.path("docs/GUIDE.md"), true)
		hs.settle()
		a.run("view.splitRight")
		hs.settle()
		a.run("view.togglePreview")
		hs.until("preview", func() bool { d := a.activeEditor(); return d.preview != nil && d.preview.loaded })
		hs.settle()
		time.Sleep(300 * time.Millisecond)
		hs.settle()
		hs.fshot("11-markdown-preview", dark)
	})
}

// 12: three columns.
func TestFeatColumns(t *testing.T) {
	featBoth(t, func(t *testing.T, dark bool) {
		hs := featHarness(t, dark, featOpts{w: 1680, h: 900})
		a := hs.a
		a.run("view.sidebar")
		for _, f := range []string{"cmd/mayoi/columns.go", "internal/linediff/linediff.go", "internal/settings/settings.go"} {
			if f != "cmd/mayoi/columns.go" {
				a.run("view.splitRight")
			}
			a.openFile(hs.path(f), true)
			hs.settle()
		}
		a.openFile(hs.path("go.mod"), true)
		hs.settle()
		hs.fshot("12-columns", dark)
	})
}

// 13: a terminal in a column, beside the code.
func TestFeatTerminal(t *testing.T) {
	featShell(t)
	featBoth(t, func(t *testing.T, dark bool) {
		hs := featHarness(t, dark, featOpts{})
		a := hs.a
		a.run("view.sidebar")
		a.openFile(hs.path("internal/linediff/linediff.go"), true)
		hs.settle()
		a.run("view.splitRight")
		a.run("view.terminalTab")
		hs.settle()
		s := a.terms[len(a.terms)-1]
		hs.termRun(s, "ls", "README")
		hs.termRun(s, "git log --graph --color=always --format='%C(yellow)%h%Creset %C(auto)%<(54,trunc)%s' -16", "Reconnect")
		hs.fshot("13-terminal-tab", dark)
		a.run("view.terminal")
		hs.settle()
		hs.termRun(a.panelTerm, "go version && echo 'Ghostty inside Mayoi \\e[1;35m♥\\e[0m'", "inside")
		hs.fshot("13-terminal-panel", dark)
		a.closeTerminals()
	})
}

// 14: quick open, symbols, the command palette and go to line.
func TestFeatQuickOpen(t *testing.T) {
	featBoth(t, func(t *testing.T, dark bool) {
		hs := featHarness(t, dark, featOpts{})
		a := hs.a
		a.openFile(hs.path("cmd/mayoi/editor.go"), true)
		hs.settle()
		a.openQuick("view")
		hs.settle()
		hs.fshot("14-quick-open", dark)
		a.closeQuick()
		a.openQuick("@")
		hs.settle()
		hs.fshot("14-go-to-symbol", dark)
		a.closeQuick()
		a.openQuick(">git")
		hs.settle()
		hs.fshot("14-command-palette", dark)
		a.closeQuick()
		a.openQuick(":120")
		hs.settle()
		hs.fshot("14-go-to-line", dark)
		a.closeQuick()
	})
}

// 15: find and replace in a file.
func TestFeatFind(t *testing.T) {
	featBoth(t, func(t *testing.T, dark bool) {
		hs := featHarness(t, dark, featOpts{})
		a := hs.a
		a.openFile(hs.path("cmd/mayoi/editor.go"), true)
		hs.settle()
		d := a.activeEditor()
		hs.scrollTo(d, "func newCodeFont", 4)
		a.openFind(true)
		a.find.query = "charW"
		a.find.with = "cellW"
		a.find.matchCase = true
		a.find.stale = true
		hs.settle()
		hs.fshot("15-find-replace", dark)
	})
}

// 16: search and replace in the folder's files.
func TestFeatSearch(t *testing.T) {
	featBoth(t, func(t *testing.T, dark bool) {
		hs := featHarness(t, dark, featOpts{})
		a := hs.a
		a.window.SidebarWidth = 360
		hs.searchFor("WordWrap")
		s := &a.inFiles
		if len(s.files) > 0 && len(s.files[0].matches) > 0 {
			a.openFile(s.files[0].path, true)
		}
		hs.settle()
		hs.fshot("16-search", dark)
		s.replacing, s.with = true, "LineWrap"
		s.details, s.include = true, "*.go"
		hs.settleSearch()
		hs.fshot("16-replace-in-files", dark)
	})
}

const featMD = "# A tour of Mayoi\n\n" +
	"Mayoi (**迷い**) is a *small*, **fast** code editor written in Go. It draws a native UI on the GPU with " +
	"[MyGo](https://github.com/egoist/mygo) — no webview, no Electron.\n\n" +
	"![Mayoi](screenshot-dark.png)\n\n" +
	"## What's inside\n\n" +
	"- An **editor** with tabs, sticky scroll and breadcrumbs\n" +
	"- **Vim mode**, with motions, operators and text objects\n" +
	"  - counts and `.` repeat\n" +
	"  - visual mode, by character or line\n" +
	"- A **source control** view, with a graph of the commits\n\n" +
	"## Roadmap\n\n" +
	"- [x] Remote SSH\n- [x] Markdown preview\n- [ ] Language servers\n\n" +
	"> Light enough to start in an instant, and to stay out of your way.\n\n" +
	"> [!TIP]\n> Press `⇧⌘V` to switch between the text and its preview.\n\n" +
	"| Shortcut | Action |\n|---|---|\n| ⌘P | Quick open |\n| ⇧⌘F | Find in files |\n| ⌘\\\\ | Split right |\n\n" +
	"```go\nfunc main() {\n\tfmt.Println(\"hello, mayoi\")\n}\n```\n"

// featProjects makes a few small folders beside a clone of Mayoi.
func featProjects(t *testing.T) []string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, "Projects")
	dirs := []string{filepath.Join(base, "mayoi")}
	if out, err := exec.Command("git", "clone", "-q", "--no-hardlinks", "../..", dirs[0]).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	for _, name := range []string{"mygo", "website", "api-server", "dotfiles"} {
		dir := filepath.Join(base, name)
		os.MkdirAll(filepath.Join(dir, "src"), 0o755)
		os.WriteFile(filepath.Join(dir, "README.md"), []byte("# "+name+"\n"), 0o644)
		os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
		dirs = append(dirs, dir)
	}
	return dirs
}

// 17: project tabs, the rail of the folders open in one window.
func TestFeatProjectTabs(t *testing.T) {
	featBoth(t, func(t *testing.T, dark bool) {
		dirs := featProjects(t)
		f, hs, _ := railFixture(t, dirs...)
		tt := ui.NewTester(f.view, featW, featH)
		tt.SetDark(dark)
		tt.SetScale(2)
		a := hs[0].a
		a.window.Expanded = []string{"cmd"}
		h := &harness{t: t, a: a, tt: tt, root: dirs[0]}
		h.settle()
		a.openFile(filepath.Join(dirs[0], "cmd/mayoi/projects.go"), true)
		h.settle()
		a.focusColumn()
		hs[2].a.alert = "Bell from zsh"
		hs[3].a.openFile(filepath.Join(dirs[3], "README.md"), true)
		r, _ := tt.Find("website")
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
		for range 40 {
			step(tt)
			time.Sleep(10 * time.Millisecond)
		}
		h.fshot("17-project-tabs", dark)
	})
}

// 18: the welcome page, recent folders and ⌃R.
func TestFeatWelcome(t *testing.T) {
	featBoth(t, func(t *testing.T, dark bool) {
		dirs := featProjects(t)
		hs := featHarness(t, dark, featOpts{root: dirs[0]})
		a := hs.a
		hs.fshot("18-welcome-folder", dark)
		a.settings.Recent = append([]string{}, dirs...)
		a.closeFolder()
		hs.settle()
		hs.fshot("18-welcome", dark)
		a.openFolder(dirs[0])
		hs.settle()
		a.openRecentPicker()
		hs.settle()
		hs.fshot("18-open-recent", dark)
	})
}

// featRemoteHome is a home folder with an ssh config and projects, that
// the fake ssh, which runs on this machine, takes for the host's.
func featRemoteHome(t *testing.T) (home, project string) {
	useFakeSSH(t)
	featShell(t)
	home = filepath.Join(t.TempDir(), "deploy")
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(`Host dev-box
  HostName 10.0.0.12
Host gpu-server gpu-backup
  User ml
Host staging
Host build-mac
`), 0o600)
	for _, d := range []string{"projects/api-server", "projects/website", "notes", "scratch"} {
		os.MkdirAll(filepath.Join(home, d), 0o755)
	}
	project = filepath.Join(home, "projects", "mayoi")
	featClone(t, project)
	z := t.TempDir()
	os.WriteFile(filepath.Join(z, ".zshrc"), []byte("PROMPT='%F{green}deploy@dev-box%f %F{cyan}%1~%f %F{magenta}❯%f '\nexport CLICOLOR=1\n"), 0o644)
	t.Setenv("ZDOTDIR", z)
	t.Setenv("HOME", home)
	return home, project
}

// 19: Remote SSH: the hosts, a host's folders, a folder of a host.
func TestFeatRemote(t *testing.T) {
	featRemoteHome(t)
	featBoth(t, func(t *testing.T, dark bool) {
		hs := featHarness(t, dark, featOpts{})
		a := hs.a
		a.settings = settings.Default()
		a.settings.AddRecent(remote.Path("prod.example.com", "/srv/www/shop"))
		a.settings.AddRecent(remote.Path("dev-box", "/home/deploy/projects/mayoi"))
		a.closeFolder()
		hs.settle()
		hs.fshot("19-remote-welcome", dark)
		a.openRemotePicker()
		hs.untilLong("the hosts", func() bool { return len(a.remotePick.resolved) >= 6 })
		hs.settle()
		hs.fshot("19-remote-hosts", dark)
		a.chooseHost("build-mac")
		hs.untilLong("the folders", func() bool { return a.remotePick.stage == pickFolder })
		hs.untilLong("the listing", func() bool { return a.remotePick.dir != "" && !a.remotePick.listing && hs.tt.HasText("scratch") })
		hs.settle()
		hs.fshot("19-remote-folders", dark)
		a.closeRemotePicker()
	})
}

func TestFeatRemoteWindow(t *testing.T) {
	_, project := featRemoteHome(t)
	featBoth(t, func(t *testing.T, dark bool) {
		s := settings.Default()
		w := s.NewWindow()
		w.Folder = remote.Path("dev-box", project)
		w.Expanded = []string{"cmd", "internal"}
		a := newApp(&fakeHost{}, s, w)
		t.Cleanup(a.shutdown)
		tt := ui.NewTester(a.view, featW, featH)
		tt.SetDark(dark)
		tt.SetScale(2)
		hs := &harness{t: t, a: a, h: &fakeHost{}, tt: tt, root: w.Folder}
		t.Cleanup(a.shutdown)
		hs.untilLong("the tree", func() bool { return tt.HasText("go.mod") })
		a.openFile(hs.path("internal/remote/conn.go"), true)
		hs.untilLong("the file", func() bool { d := a.activeEditor(); return d != nil && !d.loading })
		a.run("view.terminal")
		hs.settle()
		hs.termRun(a.panelTerm, "ls ~/projects", "website")
		if _, err := a.forwardPort(5173); err != nil {
			t.Log(err)
		}
		a.startTransfer("Uploading 3 items to dev-box…")
		hs.settleGit()
		for range 10 {
			hs.settle()
		}
		hs.fshot("20-remote-window", dark)
		a.disconnect()
		for range 20 {
			hs.settle()
		}
		hs.fshot("20-remote-disconnected", dark)
		a.closeTerminals()
	})
}

// 21: the themes.
func TestFeatThemes(t *testing.T) {
	featDir(t)
	featShell(t)
	for _, tc := range []struct {
		theme string
		dark  bool
		name  string
	}{
		{themeMatrix, true, "matrix"}, {themeClaude, false, "claude-light"}, {themeClaude, true, "claude-dark"},
		{themeQoder, false, "qoder-light"}, {themeQoder, true, "qoder-dark"},
		{themeGrokBot, false, "grokbot-light"}, {themeGrokBot, true, "grokbot-dark"},
	} {
		hs := featHarness(t, tc.dark, featOpts{theme: tc.theme, expanded: []string{"cmd", "cmd/mayoi"}})
		a := hs.a
		for range 20 {
			hs.settle()
		}
		hs.shotAs("21-theme-" + tc.name + "-welcome")
		a.openFile(hs.path("cmd/mayoi/theme.go"), true)
		hs.settle()
		d := a.activeEditor()
		hs.scrollTo(d, "func paletteFor", 3)
		hs.caretAt(d, "func paletteFor", 5, 10)
		a.run("view.terminal")
		hs.settle()
		hs.termRun(a.panelTerm, "git log --oneline --decorate -4 --color", "Open the folders")
		hs.shotAs("21-theme-" + tc.name + "-editor")
		a.closeTerminals()
	}
}

func (hs *harness) shotAs(name string) {
	old := os.Getenv("SHOTS")
	os.Setenv("SHOTS", os.Getenv("FSHOTS"))
	hs.shot(name)
	os.Setenv("SHOTS", old)
}

// 22: the sidebar on the right, and the settings and keybindings files.
func TestFeatSettings(t *testing.T) {
	featBoth(t, func(t *testing.T, dark bool) {
		hs := featHarness(t, dark, featOpts{})
		a := hs.a
		dir := "/tmp/Mayoi"
		os.RemoveAll(dir)
		os.MkdirAll(dir, 0o755)
		t.Cleanup(func() { os.RemoveAll(dir) })
		os.WriteFile(filepath.Join(dir, "keybindings.json"), []byte(featKeys), 0o644)
		a.settings = settings.Load(filepath.Join(dir, "settings.json"))
		a.settings.Theme = ""
		a.settings.SidebarRight = true
		a.settings.FontFamily = "JetBrains Mono"
		a.settings.Vim = true
		a.settings.WordWrap = true
		a.settings.FontSize = 14
		a.settings.Changed()
		a.openSettings()
		hs.settle()
		hs.fshot("22-settings-json", dark)
		a.openKeybindings()
		hs.settle()
		hs.fshot("22-keybindings-json", dark)
		a.settings.SidebarRight = false
	})
}

const featKeys = `// Keyboard shortcuts, in VS Code's format: "key" is a key or a chord as
// "ctrl+. s", "command" is a command of Mayoi's or of VS Code's, and
// "when" when it applies, as "editorTextFocus" or "terminalFocus".
[
  { "key": "cmd+k cmd+t", "command": "workbench.action.selectTheme" },
  { "key": "cmd+shift+e", "command": "workbench.view.explorer" },
  { "key": "ctrl+shift+g", "command": "workbench.view.scm" },
  { "key": "cmd+j", "command": "workbench.action.togglePanel" },
  { "key": "alt+cmd+left", "command": "view.prevColumn", "when": "editorTextFocus" },
  { "key": "ctrl+k", "command": "-editor.action.deleteLines", "when": "terminalFocus" },
  { "key": "cmd+shift+d", "command": "editor.action.copyLinesDownAction" }
]
`
