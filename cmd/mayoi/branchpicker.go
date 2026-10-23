package main

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/internal/workspace"
	"mayoi/locales"
)

// branchPicker is the box that switches branches: the branches, local and
// of the remotes, found by name, the one committed to last first, and
// the name typed, to create a branch of.
type branchPicker struct {
	repoRoot string
	open     bool
	query    string
	sel      int
	moved    bool
	loading  bool
	// branches are those read when the box opened, names their names, and
	// results those the query matches.
	branches []git.Branch
	names    branchNames
	results  []workspace.Match
	shown    string // the query the results are of
	stale    bool
}

// branchNames are the names of branches, local and of remotes.
type branchNames []string

// hasBranch reports whether name is a branch's, or a remote's branch's without
// the remote, as "feature" of "origin/feature", which switching to by
// name checks out.
func (b *branchPicker) hasBranch(name string) bool {
	for _, br := range b.branches {
		if br.Name == name {
			return true
		}
		if _, short, ok := strings.Cut(br.Name, "/"); br.Remote && ok && short == name {
			return true
		}
	}
	return false
}

// openBranchPicker shows the box of the branches, reading them in the
// background.
func (a *gitContext) openBranchPicker() {
	if !a.live() || a.git.busy != "" {
		return
	}
	r := a.git.repo
	if r == nil {
		return
	}
	b := &a.app.branches
	*b = branchPicker{open: true, loading: true, repoRoot: r.Root}
	bg.Go(func() {
		bs, err := r.BranchList()
		a.app.post(func() {
			if !b.open || b.repoRoot != r.Root || a.git.repo != r {
				return
			}
			b.loading = false
			if err != nil {
				a.gitFailed(err)
				return
			}
			// A remote's branch that a local one tracks by its name shows
			// as the local one.
			local := map[string]bool{}
			for _, br := range bs {
				if !br.Remote {
					local[br.Name] = true
				}
			}
			b.branches = slices.DeleteFunc(bs, func(br git.Branch) bool {
				_, name, _ := strings.Cut(br.Name, "/")
				return br.Remote && local[name]
			})
			// The branch checked out comes first.
			cur := a.branch()
			current := func(br git.Branch) int { return btoi(!br.Remote && br.Name == cur) }
			slices.SortStableFunc(b.branches, func(x, y git.Branch) int { return current(y) - current(x) })
			b.names = make(branchNames, len(b.branches))
			for i, br := range b.branches {
				b.names[i] = br.Name
			}
			b.stale = true
		})
	})
}

func (a *gitContext) closeBranchPicker() {
	a.app.branches.open = false
	if a.app.focus != paneTerminal {
		a.app.focusColumn()
	}
}

// branchChoice is a row of the box: a branch, or the name typed, to
// create.
type branchChoice struct {
	prompt bool   // ask for the name of a new branch
	create string // the name of the branch to create, "" for a branch
	at     int    // the result shown, for a branch
}

// branchChoices returns the rows of the box: the branches the query
// matches, preceded by creating a branch when the query is empty or
// names no existing branch, local or of a remote.
func (a *gitContext) branchChoices() []branchChoice {
	b := &a.app.branches
	var out []branchChoice
	if name := strings.TrimSpace(b.query); !b.loading {
		if name == "" {
			out = append(out, branchChoice{prompt: true})
		} else if validBranch(name) && !b.hasBranch(name) {
			out = append(out, branchChoice{create: name})
		}
	}
	for i := range b.results {
		out = append(out, branchChoice{at: i})
	}
	return out
}

// validBranch reports whether a name may be a branch's, as far as the
// box checks before Git does.
func validBranch(name string) bool {
	if name == "" || name == "HEAD" || strings.HasPrefix(name, "-") || strings.HasSuffix(name, ".") ||
		strings.ContainsAny(name, " ~^:?*[\\") || strings.Contains(name, "..") || strings.Contains(name, "@{") {
		return false
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return false
		}
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// chooseBranch does what a row of the box does: switches to its branch,
// or creates the branch typed from the one checked out.
func (a *gitContext) chooseBranch(ch branchChoice) {
	b := &a.app.branches
	if !a.live() || a.git.busy != "" || !b.open || b.repoRoot != a.git.repo.Root {
		return
	}
	if ch.prompt {
		a.closeBranchPicker()
		a.askNewBranch()
		return
	}
	if ch.create != "" {
		a.closeBranchPicker()
		a.switchBranch(ch.create, true)
		return
	}
	if ch.at < 0 || ch.at >= len(b.results) {
		return
	}
	br := b.branches[b.results[ch.at].Index]
	a.closeBranchPicker()
	name := br.Name
	if br.Remote {
		// Git makes the local branch that tracks it, of the same name.
		_, name, _ = strings.Cut(br.Name, "/")
	}
	if name != a.branch() {
		a.switchBranch(name, false)
	}
}

// branchPickerView shows the box of the branches while it is open.
func (a *gitContext) branchPickerView(c *ui.Context) {
	b := &a.app.branches
	if !b.open {
		return
	}
	p := a.app.pal
	changed := b.stale || b.shown != b.query
	if changed {
		b.results = workspace.Fuzzy(b.names, strings.TrimSpace(b.query), 200)
		if strings.TrimSpace(b.query) == "" {
			// Every branch, the one committed to last first.
			b.results = b.results[:0]
			for i := range b.names {
				b.results = append(b.results, workspace.Match{Index: i})
			}
		}
		b.shown, b.stale, b.sel = b.query, false, 0
	}
	choices := a.branchChoices()
	if changed && len(b.results) > 0 && len(choices) > len(b.results) {
		// Enter keeps switching to the first match; creating is explicit
		// until the query matches no branch.
		b.sel = 1
	}
	ui.Overlay(c, func() {
		back := a.app.dialogBack(c, "branches")
		back.Children(func() {
			box := a.app.floating(c, ui.Column(c).Width(560).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Radius(10), true).Clip().
				Role(ui.RoleDialog).Label(locales.BranchSwitchLabel())
			box.Children(func() {
				ui.Row(c).Padding(10, 14).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
					ui.Icon(c, iconBranch).FontSize(15).TextColor(p.muted)
					in := a.app.textKeys(ui.TextInputBase(c.Key("branch-input"), &b.query)).Grow(1).FontSize(14).AutoFocus().Disabled(a.git.busy != "").
						Placeholder(locales.BranchPlaceholder()).Label(locales.BranchSwitchLabel())
					if in.Shortcut(0, ui.KeyDown) {
						b.sel, b.moved = min(b.sel+1, len(choices)-1), true
					}
					if in.Shortcut(0, ui.KeyUp) {
						b.sel, b.moved = max(b.sel-1, 0), true
					}
					if in.Submitted() && b.sel >= 0 && b.sel < len(choices) {
						a.app.post(func() { a.chooseBranch(choices[b.sel]) })
					}
				})
				switch {
				case len(choices) > 0:
					ui.Scroll(c.Key("branch-results")).MaxHeight(380).Padding(6).Children(func() {
						for i, ch := range choices {
							a.branchRow(c, ch, i)
							if i == 0 && (ch.prompt || ch.create != "") && len(choices) > 1 {
								ui.Box(c).Height(1).Background(p.border).Margin(4, 6)
							}
						}
					})
				case b.loading:
					ui.Text(c, locales.BranchReading()).FontSize(13).TextColor(p.muted).Padding(12, 16)
				default:
					ui.Text(c, locales.BranchNoMatches()).FontSize(13).TextColor(p.muted).Padding(12, 16)
				}
				b.moved = false
				action := locales.QuickSwitchAction()
				if b.sel < len(choices) && (choices[b.sel].prompt || choices[b.sel].create != "") {
					action = locales.QuickCreateAction()
				}
				a.app.quickFooter(c, false, action, locales.BranchTypeToCreate(), false)
			})
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeBranchPicker()
			}
		})
	})
}

// branchRow is a row of the box of the branches: a branch, with the
// characters the query matched in the accent color, a check by the one
// checked out, and the subject of its last commit and how long ago it was
// made; or the branch to create.
func (a *gitContext) branchRow(c *ui.Context, ch branchChoice, i int) {
	b := &a.app.branches
	p := a.app.pal
	key, label := "create", locales.BranchCreateLabel(ch.create)
	var br git.Branch
	var positions []int
	if ch.prompt {
		label = locales.CreateBranchEllipsis()
	} else if ch.create == "" {
		m := b.results[ch.at]
		br, positions = b.branches[m.Index], m.Positions
		key, label = "branch:"+br.Name, br.Name
	}
	row := ui.ButtonBase(c.Key(key)).Padding(5, 10).Gap(10).Radius(6).Justify(ui.Start).AlignItems(ui.Center).Label(label).Disabled(a.git.busy != "")
	if i != b.sel && row.Hovered() {
		row.Background(p.hover)
	}
	if i == b.sel && b.moved {
		row.ScrollIntoView()
	}
	if row.Clicked() {
		a.app.post(func() { a.chooseBranch(ch) })
	}
	row.Transition(hoverFade)
	row.Children(func() {
		a.app.pickMark(c, i == b.sel, "branches")
		if ch.prompt || ch.create != "" {
			ui.Icon(c, iconPlus).FontSize(14).TextColor(p.accent)
			if ch.prompt {
				ui.Text(c, label).FontSize(13).TextColor(p.text).SingleLine().Shrink(1)
			} else {
				ui.RichText(c, ui.Span{Text: locales.BranchCreatePrefix()}, ui.Span{Text: ch.create, Weight: 650}).FontSize(13).
					TextColor(p.text).SingleLine().Shrink(1)
			}
			ui.Spacer(c)
			if from := a.branch(); from != "" {
				ui.Text(c, locales.BranchFrom(from)).FontSize(12).TextColor(p.faint).SingleLine().Shrink(0)
			}
			return
		}
		icon, col := iconBranch, p.muted
		if br.Remote {
			icon = iconFetch
		}
		current := !br.Remote && br.Name == a.branch()
		if current {
			icon, col = iconCheck, p.accent
		}
		ui.Icon(c, icon).FontSize(14).TextColor(col)
		var spans []ui.Span
		k := 0
		for at, r := range br.Name {
			for k < len(positions) && positions[k] < at {
				k++
			}
			on := k < len(positions) && positions[k] == at
			if n := len(spans); n > 0 && (spans[n-1].Weight == 650) == on {
				spans[n-1].Text += string(r)
				continue
			}
			sp := ui.Span{Text: string(r), Color: p.text}
			if on {
				sp.Color, sp.Weight = p.accent, 650
			}
			spans = append(spans, sp)
		}
		ui.RichText(c, spans...).FontSize(13).SingleLine().Shrink(0)
		ui.Text(c, br.Subject).FontSize(12).TextColor(p.faint).SingleLine().Shrink(1).Grow(1).MinWidth(0)
		if !br.Time.IsZero() {
			ui.Text(c, shortAgo(br.Time, time.Now())).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(0)
		}
	})
}
