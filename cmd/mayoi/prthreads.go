package main

import (
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/locales"
)

// The rows of a diff's cards of notes have the note's number, from 0; the
// cards of the review threads of a pull request's diff, below them, have
// threadRow of the thread's place among its threads.
func threadRow(k int) int { return -2 - k }

// rowThread is the place among the threads of a row's number, -1 for a
// note's.
func rowThread(n int) int {
	if n > -2 {
		return -1
	}
	return -2 - n
}

// The parts of a thread's card: the line of each comment's author, and
// the room under each comment.
const (
	threadAuthorH = 20
	threadGap     = 6
)

// threadAt is the review thread of the card of a diff's row number n, nil
// for a note's.
func (v *diffView) threadAt(n int) *git.GHThread {
	k := rowThread(n)
	if k < 0 || v.commit == nil || v.commit.pr == nil {
		return nil
	}
	ths := v.commit.pr.ghThreads
	if k >= len(ths) {
		return nil
	}
	return &ths[k]
}

// threadShows reports whether a review thread has a card in the diff:
// one on lines of the commits it shows, resolved only when asked for.
func (pr *prDiff) threadShows(th *git.GHThread) bool {
	return !th.Outdated && len(th.Comments) > 0 && (!th.Resolved || pr.showResolved || pr.replyTo == th.ID)
}

// threadRows adds the rows of the cards of the review threads of a pull
// request on file f of its diff laid out as l, number file, to the rows
// from start, which are of f: each under the last line it is on.
func (a *app) threadRows(v *diffView, l diffGeometry, f *diffFile, file, start int) {
	pr := v.commit.pr
	if f.prPath == "" || len(pr.ghThreads) == 0 {
		return
	}
	for k := range pr.ghThreads {
		th := &pr.ghThreads[k]
		if th.Path != f.prPath || !pr.threadShows(th) {
			continue
		}
		at, last := -1, th.Line-1
		for i := start; i < len(v.rows); i++ {
			row := v.rows[i]
			line := row.new
			if th.OldSide {
				line = row.old
			}
			if (row.kind == rowLine || row.kind == rowGap) && line >= 0 && line <= last {
				at = i
			}
		}
		if at < 0 {
			continue
		}
		for at+1 < len(v.rows) && v.rows[at+1].kind == rowComment {
			at++
		}
		_, w := noteGeometry(l, v.split, th.OldSide)
		h := a.threadLines(pr, th, noteTextW(w))
		v.rows = append(v.rows[:at+1], append([]diffRow{{kind: rowComment, file: file, old: -1, new: -1, hunk: -1, peer: -1, gap: -1, n: threadRow(k), h: h}}, v.rows[at+1:]...)...)
	}
}

// threadHeight is how tall the card of a review thread is, its text w
// wide: its header, each comment under its author, and while it is
// answered, the box to answer in and its buttons.
func (a *app) threadHeight(pr *prDiff, th *git.GHThread, w float32) float32 {
	h := float32(2*notePadY + noteHeadH)
	for _, cm := range th.Comments {
		h += threadAuthorH + a.noteTextH(strings.TrimSpace(cm.Body), w) + threadGap
	}
	if pr.replyTo == th.ID {
		h += a.noteFieldH(pr.reply, w) + noteFootH
	}
	return h
}

// threadLines is how many lines of a diff the card of a review thread
// takes, its text w wide.
func (a *app) threadLines(pr *prDiff, th *git.GHThread, w float32) int {
	h := a.threadHeight(pr, th, w) + 2*noteInset
	return max(int(math.Ceil(float64(h/a.font.lineH))), 2)
}

// threadsBuilt is what the cards of a pull request's threads were laid
// out for: their changes, and the height of the box answering one.
func (a *app) threadsBuilt(v *diffView, w float32) int {
	if v.commit == nil || v.commit.pr == nil {
		return 0
	}
	pr := v.commit.pr
	k := pr.threadGen * 4096
	if pr.replyTo != "" {
		k += int(a.noteFieldH(pr.reply, w)) + 1
	}
	return k
}

// threadCard shows the inside of the card of a review thread, its text w
// wide: where it is and how it stands, with buttons to answer it, resolve
// it or add it to the review items, then what its people said, and while
// it is answered, the box to answer in.
func (a *app) threadCard(c *ui.Context, v *diffView, th *git.GHThread, w float32) {
	p := a.pal
	pr := v.commit.pr
	now := c.Now()
	col := p.accent
	if th.Resolved {
		col = p.muted
	}
	replying := pr.replyTo == th.ID
	id := th.ID
	ui.Box(c).Absolute().Left(0).Top(0).Bottom(0).Width(noteStripe).Background(col)
	ui.Row(c).Height(noteHeadH).Gap(7).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Row(c).Size(20, 20).Radius(6).Justify(ui.Center).AlignItems(ui.Center).Background(col.Alpha(0.15)).Shrink(0).
			Children(func() { ui.Icon(c, iconGitHub).FontSize(11).TextColor(col) })
		where := filepath.Base(th.Path) + ":" + strconv.Itoa(th.Line)
		if th.StartLine > 0 && th.StartLine < th.Line {
			where = filepath.Base(th.Path) + ":" + strconv.Itoa(th.StartLine) + "–" + strconv.Itoa(th.Line)
		}
		ui.Text(c, where).FontSize(12).FontWeight(600).TextColor(p.text).SingleLine().Shrink(0)
		ui.Text(c, locales.GhComments(len(th.Comments))).FontSize(12).TextColor(p.muted).SingleLine().Shrink(1).MinWidth(0)
		if th.Resolved {
			notePill(c, locales.PrResolvedPill(), "", p.muted)
		}
		ui.Spacer(c)
		if replying {
			return
		}
		if a.smallButton(c, iconComment, locales.PrReply()) {
			a.post(func() { pr.replyTo, pr.reply = id, ""; pr.threadGen++; v.changed() })
		}
		if pr.resolving[id] {
			spinRing(c, "resolving", 12, p.muted)
		} else if th.Resolved {
			if a.smallButton(c, iconUndo, locales.PrUnresolve()) {
				a.post(func() { a.resolvePRThread(v, id, false) })
			}
		} else if a.smallButton(c, iconCircleCheck, locales.GhResolveThread()) {
			a.post(func() { a.resolvePRThread(v, id, true) })
		}
		if !th.Resolved && a.smallButton(c, iconClipboardList, locales.PrAddToReview()) {
			t := *th
			a.post(func() { a.addThreadToReview(v, t) })
		}
		if link := th.Comments[0].URL; link != "" && a.smallButton(c, iconExternal, locales.PrOpenOnGitHub()) {
			a.services.OpenURL(link)
		}
	})
	ui.Box(c).Height(2).Shrink(0)
	for i, cm := range th.Comments {
		ui.Row(c.Key("author:" + strconv.Itoa(i))).Height(threadAuthorH).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
			name := cm.Author
			if name == "" {
				name = locales.GhGhost()
			}
			a.ghAvatar(c, name, cm.Avatar, 16, now)
			ui.Text(c, name).FontSize(12).FontWeight(600).TextColor(p.text).SingleLine().Shrink(0)
			if !cm.Created.IsZero() {
				ui.Text(c, ago(cm.Created, now)).FontSize(11.5).TextColor(p.muted).SingleLine().Shrink(1).MinWidth(0).
					Tooltip(dateTime(cm.Created.Local()))
			}
		})
		ui.RichText(c.Key("body:"+strconv.Itoa(i)), a.noteSpan(strings.TrimSpace(cm.Body))).Width(w).Shrink(0).Selectable()
		ui.Box(c).Height(threadGap).Shrink(0)
	}
	if !replying {
		return
	}
	in := a.textKeys(ui.TextArea(c.Key("thread-reply"), &pr.reply)).AutoFocus().FontSize(noteFontSize).
		Width(w).Height(a.noteFieldH(pr.reply, w)).Shrink(0).Padding(noteFieldPad, noteFieldPad+2).
		Placeholder(locales.PrReplyPlaceholder()).Label(locales.PrReply())
	if pr.replying {
		in.ReadOnly(true)
	}
	if in.Changed() {
		v.changed()
	}
	if in.Focused() {
		a.review.replyTyped = a.frame
	}
	empty := strings.TrimSpace(pr.reply) == ""
	send := in.Shortcut(ui.Cmd, ui.KeyEnter)
	cancel := func() { pr.replyTo, pr.reply = "", ""; pr.threadGen++; v.changed(); v.pane.Focus() }
	if in.Shortcut(0, ui.KeyEscape) {
		a.post(cancel)
	}
	ui.Row(c).Height(noteFootH).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Text(c, locales.ReviewSaveHint(keyText("CmdOrCtrl+Enter"))).FontSize(11).TextColor(p.faint).SingleLine().Shrink(1).MinWidth(0)
		ui.Spacer(c)
		if a.noteButton(c, locales.ButtonCancel(), false, false) {
			a.post(cancel)
		}
		if a.noteButton(c, locales.PrReply(), true, empty || pr.replying) {
			send = true
		}
	})
	if send && !empty && !pr.replying {
		body := strings.TrimSpace(pr.reply)
		a.post(func() { a.replyPRThread(v, id, body) })
	}
}

// replyPRThread answers a review thread of a pull request's diff; the
// card shows the answer once GitHub has it.
func (a *app) replyPRThread(v *diffView, id, body string) {
	pr := v.commit.pr
	pr.replying = a.prAct(v, locales.PrReply(), locales.PrReplied(), func(r *git.Repo) error {
		return r.ReplyThread(id, body)
	}, func(err error) {
		pr.replying = false
		if err == nil && pr.replyTo == id {
			pr.replyTo, pr.reply = "", ""
			pr.threadGen++
			v.changed()
		}
	})
}

// resolvePRThread marks a review thread of a pull request resolved, or
// not: its card shows it at once, and goes back as GitHub refuses it.
func (a *app) resolvePRThread(v *diffView, id string, resolve bool) {
	pr := v.commit.pr
	set := func(on bool) {
		for i := range pr.ghThreads {
			if pr.ghThreads[i].ID == id {
				pr.ghThreads[i].Resolved = on
			}
		}
		pr.threadGen++
		v.changed()
	}
	done := locales.PrThreadResolved()
	if !resolve {
		done = locales.PrThreadReopened()
	}
	started := a.prAct(v, locales.GhResolveThread(), done, func(r *git.Repo) error {
		if resolve {
			return r.ResolveThread(id)
		}
		return r.UnresolveThread(id)
	}, func(err error) {
		delete(pr.resolving, id)
		if err != nil {
			set(!resolve)
		}
	})
	if !started {
		return
	}
	if pr.resolving == nil {
		pr.resolving = map[string]bool{}
	}
	pr.resolving[id] = true
	set(resolve)
}

// addThreadToReview adds a review thread of a pull request's diff to the
// review items, as a note on its lines, for an agent to address.
func (a *app) addThreadToReview(v *diffView, th git.GHThread) {
	cm := v.commit
	r := a.diffContext(v).git.repo
	if r == nil || cm.pending() {
		return
	}
	key, from, head, n := cm.key(), cm.from, cm.hash, cm.pr.number
	bg.Go(func() {
		defer recoverCrash()
		notes := threadNotes(r, key, from, head, []git.GHThread{th})
		a.post(func() { a.addThreadNotes(n, notes) })
	})
}
