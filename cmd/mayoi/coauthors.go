package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/agents"
	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/locales"
)

// coAuthorAgents are the agents co-authors are, by name and email, matched
// once: the rows of the history ask each frame. Only the main thread asks.
var coAuthorAgents = map[git.Person]agents.Harness{}

// coAuthorAgent is the first of a commit's co-authors that is an agent,
// false for none.
func coAuthorAgent(cs []git.Person) (agents.Harness, bool) {
	for _, p := range cs {
		h, ok := coAuthorAgents[p]
		if !ok {
			h, _ = agents.HarnessOfCoAuthor(p.Name, p.Email)
			coAuthorAgents[p] = h
		}
		if h != "" {
			return h, true
		}
	}
	return "", false
}

// coAuthorNames are the names of a commit's co-authors, for people.
func coAuthorNames(cs []git.Person) string {
	names := make([]string, len(cs))
	for i, p := range cs {
		names[i] = p.Name
	}
	return strings.Join(names, ", ")
}

// commitAvatar is the circle of the initials of a commit's author, size
// wide, with the mark of the agent that helped make it, if one did, on
// its lower right, cut out of the avatar by a ring of the color behind
// it, ring.
func (a *app) commitAvatar(c *ui.Context, author, email string, cos []git.Person, size float32, ring ui.Color) {
	p := a.pal
	col := avatarColor(p, email+author)
	box := ui.Box(c).Size(size, size).Shrink(0).Draw(func(pt *ui.Painter, b ui.Rect) {
		pt.Fill(b, col.Alpha(0.22), b.W/2)
		paintInitials(pt, b, author, size*0.47, col)
	})
	h, ok := coAuthorAgent(cos)
	if !ok {
		return
	}
	icon, tint := agentIcon(p, h)
	// A disc of the agent's color, its mark cut out of it in the color
	// behind, as large as the avatar leaves legible.
	d := float32(int(size*0.64 + 0.5))
	box.Children(func() {
		ui.Box(c).Absolute().Right(-d/3-1).Bottom(-d/3-1).Size(d+2, d+2).Radius((d + 2) / 2).
			Background(ring).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(d, d).Radius(d / 2).Background(tint).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icon).FontSize(d * 0.78).TextColor(ring)
			})
		})
	})
}

// withoutCoAuthorLines is a commit's message without its Co-authored-by
// lines, which its card says by its author.
func withoutCoAuthorLines(body string) string {
	lines := strings.Split(body, "\n")
	out := lines[:0]
	for _, l := range lines {
		k, _, ok := strings.Cut(strings.TrimSpace(l), ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), "Co-authored-by") {
			continue
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// commitStatRead is how much a commit changed, as it is read.
type commitStatRead struct {
	stat    git.CommitStat
	loading bool
	failed  bool
}

// wantCommitStat has how much a commit changed read, in the background,
// once, and returns it as it is so far.
func (a *gitContext) wantCommitStat(cm git.Commit) *commitStatRead {
	s := a.scm
	if read := s.commitStats[cm.Hash]; read != nil {
		return read
	}
	read := &commitStatRead{loading: true}
	r := a.git.repo
	if r == nil {
		read.loading, read.failed = false, true
		return read
	}
	if s.commitStats == nil {
		s.commitStats = map[string]*commitStatRead{}
	}
	s.commitStats[cm.Hash] = read
	bg.Go(func() {
		defer recoverCrash()
		st, err := r.CommitStat(cm)
		a.app.post(func() {
			read.loading, read.stat, read.failed = false, st, err != nil
		})
	})
	return read
}

// commitCardW is how wide the card of a commit of the history is, the
// same for every commit, so it does not change size as its numbers come.
const commitCardW = 288

// commitHoverCard is the card that shows as the pointer rests on a
// commit's row: who made it, and with whom, then its hash, how much it
// changed, when, its branches and tags, and what else its row's tip said,
// extra.
func (a *gitContext) commitHoverCard(c *ui.Context, row ui.Element, cm *git.Commit, extra []string) {
	app := a.app
	p := app.pal
	ui.TooltipBase(c, row, func(tip ui.Element) {
		tip.AttachTo(row, ui.AnchorRight, ui.AnchorLeft).Margin(0, 0, 0, 8)
		fadeIn(app.floating(c, tip, false)).Radius(10).Width(commitCardW)
		surface := c.Theme().Surface
		stat := a.wantCommitStat(*cm)
		tip.Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(12, 14, 10, 14).Children(func() {
				app.commitAvatar(c, cm.Author, cm.Email, cm.CoAuthors, 28, surface)
				ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, cm.Author).FontSize(13).FontWeight(600).TextColor(p.text).SingleLine()
					if len(cm.CoAuthors) > 0 {
						ui.Text(c, locales.ScmCoAuthoredWith(coAuthorNames(cm.CoAuthors))).FontSize(11.5).TextColor(p.muted).MaxLines(2)
					}
				})
			})
			ui.Text(c, cm.Subject).FontSize(12).TextColor(p.text).MaxLines(3).Padding(0, 14, 10, 14)
			ui.Box(c).Height(1).Background(p.border).Shrink(0)
			ui.Column(c).Gap(6).Padding(10, 14, 12, 14).Children(func() {
				field := func(name string, value func()) {
					ui.Row(c).Gap(12).AlignItems(ui.Center).Height(18).Children(func() {
						ui.Text(c, name).FontSize(11.5).TextColor(p.muted).Shrink(0)
						ui.Spacer(c)
						ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(1).MinWidth(0).Children(value)
					})
				}
				num := func(s string, col ui.Color) {
					ui.Text(c, s).FontSize(11.5).TextColor(col).FontFeatures("tnum").SingleLine().Shrink(0)
				}
				field(locales.ScmCardCommit(), func() {
					ui.Text(c, cm.Short()).Font(app.font.family).FontSize(11.5).TextColor(p.text).SingleLine()
				})
				field(locales.ScmCardChanged(), func() {
					switch {
					case stat.loading:
						ui.Box(c).Size(72, 8).Radius(4).Background(p.hover)
					case stat.failed:
						num("—", p.faint)
					default:
						num(locales.ScmCardFiles(stat.stat.Files), p.text)
						num("+"+strconv.Itoa(stat.stat.Added), p.addedFg)
						num("−"+strconv.Itoa(stat.stat.Deleted), p.removedFg)
					}
				})
				field(locales.ScmCardCommitted(), func() {
					ui.Text(c, ago(cm.Time, time.Now())).FontSize(11.5).TextColor(p.text).SingleLine()
				})
				if badges := refBadges(cm.Refs); len(badges) > 0 {
					names := make([]string, len(badges))
					for i, b := range badges {
						names[i] = b.label()
					}
					field(locales.ScmCardBranch(), func() {
						ui.Text(c, strings.Join(names, ", ")).FontSize(11.5).TextColor(p.text).SingleLine().Shrink(1).MinWidth(0)
					})
				}
				for _, s := range extra {
					ui.Text(c, s).FontSize(11.5).TextColor(p.muted).SingleLine()
				}
			})
		})
	})
}
