package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/egoist/mygo/ui"
	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/locales"
)

// issueHandoffs are the prompts of issues Work on Issue made a worktree
// for, by the worktree's folder, which the project of that folder takes
// once it opens: it starts a terminal there that holds the prompt for the
// agent started in it.
var issueHandoffs = struct {
	sync.Mutex
	of map[string]issueHandoff
}{of: map[string]issueHandoff{}}

// issueHandoff is the prompt of an issue, and the issue, as "#12".
type issueHandoff struct {
	prompt, of string
}

// handOffIssue keeps the prompt of an issue for the project of dir.
func handOffIssue(dir string, h issueHandoff) {
	issueHandoffs.Lock()
	issueHandoffs.of[filepath.Clean(dir)] = h
	issueHandoffs.Unlock()
}

// takeIssueHandoff starts the terminal of the issue handed to the
// project's folder, if one was. It runs once a frame.
func (a *app) takeIssueHandoff() {
	if a.root == "" {
		return
	}
	issueHandoffs.Lock()
	h, ok := issueHandoffs.of[filepath.Clean(a.root)]
	if ok {
		delete(issueHandoffs.of, filepath.Clean(a.root))
	}
	issueHandoffs.Unlock()
	if !ok {
		return
	}
	s := a.startTerminal(a.root)
	if s == nil {
		a.services.WriteClipboard(h.prompt)
		a.tell(note{message: locales.GhPromptCopied(h.of)})
		return
	}
	s.prompt, s.promptOf = h.prompt, h.of
	a.placeTerminal(s)
}

// issueBranch is the branch of the worktree of issue n: its number and
// the words of its title, as 12-fix-the-crash, or issue-12 when its title
// has no word in the letters a branch reads well in.
func issueBranch(n int, title string) string {
	var words []string
	var w strings.Builder
	flush := func() {
		if w.Len() > 0 {
			words = append(words, w.String())
			w.Reset()
		}
	}
	for _, r := range strings.ToLower(title) {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			w.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	slug := ""
	for _, word := range words {
		if len(slug)+len(word)+1 > 40 {
			break
		}
		if slug != "" {
			slug += "-"
		}
		slug += word
	}
	if slug == "" {
		return "issue-" + strconv.Itoa(n)
	}
	return strconv.Itoa(n) + "-" + slug
}

// issuePrompt is the message to an agent of an issue: what it is, where
// it is, what it says and what its people said, and what to do with it;
// on branch when it has one of its own.
func issuePrompt(repo string, is *git.GHIssue, branch string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Work on GitHub issue #%d of %s: %s\n%s\n", is.Number, repo, is.Title, is.URL)
	if labels := ghSortedLabels(is.Labels); len(labels) > 0 {
		fmt.Fprintf(&b, "Labels: %s\n", strings.Join(labels, ", "))
	}
	if body := strings.TrimSpace(is.Body); body != "" {
		b.WriteString("\n" + body + "\n")
	}
	comments := is.Comments
	if len(comments) > 10 {
		comments = comments[len(comments)-10:]
	}
	if len(comments) > 0 {
		b.WriteString("\nComments:\n")
		for _, c := range comments {
			author := c.Author
			if author == "" {
				author = "ghost"
			}
			fmt.Fprintf(&b, "\n@%s:\n%s\n", author, strings.TrimSpace(c.Body))
		}
	}
	b.WriteString("\n")
	if branch != "" {
		fmt.Fprintf(&b, "Work in this worktree, on branch %s. ", branch)
	}
	fmt.Fprintf(&b, "When it is done, commit, push and open a pull request that says \"Closes #%d\".", is.Number)
	return b.String()
}

// workOnIssue makes a worktree for issue n, on a branch of its own from
// the tip of the default branch on GitHub, and opens it as a project of
// its own, whose terminal hands the issue to the agent started in it.
// A worktree made for it before opens again.
func (a *app) workOnIssue(ctx *gitContext, n int) {
	o := ctx.githubOverview()
	if o == nil || o.DefaultBranch == "" {
		return
	}
	var (
		is     *git.GHIssue
		path   string
		branch string
	)
	ctx.gitDo(locales.GhPreparingWorktree("#"+strconv.Itoa(n)), func(r *git.Repo) error {
		var err error
		if is, err = fetchIssue(r, n); err != nil {
			return err
		}
		branch = issueBranch(n, is.Title)
		if p, err := r.WorktreeForBranch(branch); err == nil && p != "" {
			path = p
			return nil
		}
		base, err := r.FetchDefaultBranch(o.URL, o.DefaultBranch)
		if err != nil {
			return err
		}
		path, err = r.WorktreeOfBranch(branch, base)
		var exists *git.WorktreeExists
		if errors.As(err, &exists) {
			path, err = exists.Path, nil
		}
		return err
	}, func() {
		handOffIssue(path, issueHandoff{prompt: issuePrompt(o.Repo, is, branch), of: "#" + strconv.Itoa(n)})
		a.tell(note{message: locales.GhWorktreeReady("#"+strconv.Itoa(n), shortenHome(path))})
		// A project of its own beside this one: a project tab, or a
		// window with a folder each.
		if !a.host.showFolder(filepath.Clean(path)) {
			a.host.newWindow(path)
		}
	})
}

// sendIssue pastes an issue into the terminal used last, as a message to
// the agent in it, without pressing Enter; with no terminal, it copies it.
func (a *app) sendIssue(ctx *gitContext, n int) {
	o := ctx.githubOverview()
	repo := ""
	if o != nil {
		repo = o.Repo
	}
	var is *git.GHIssue
	ctx.gitRead(locales.GhReadingIssue("#"+strconv.Itoa(n)), func(r *git.Repo) (err error) {
		is, err = fetchIssue(r, n)
		return err
	}, func() {
		text := issuePrompt(repo, is, "")
		if a.pasteToTerminal(text + "\n") {
			a.tell(note{message: locales.GhIssueSent("#" + strconv.Itoa(n))})
			return
		}
		a.services.WriteClipboard(text)
		a.tell(note{message: locales.GhPromptCopied("#" + strconv.Itoa(n))})
	})
}

// terminalPrompt hands a terminal's prompt to the agent once one runs in
// it; until then, a card over the terminal says it waits, with buttons
// that paste it now, copy it, or drop it.
func (a *app) terminalPrompt(c *ui.Context, s *session) {
	if s.prompt == "" {
		return
	}
	if agent, ok := termAgent(s); ok {
		s.term.Paste(s.prompt + "\n")
		a.tell(note{message: locales.GhPromptSent(s.promptOf, agent.Name())})
		s.prompt, s.promptOf = "", ""
		return
	}
	p := a.pal
	card := a.floating(c, ui.Row(c.Key("term-prompt")).Absolute().Top(8).Right(12).MaxWidth(520).Padding(8, 8, 8, 12).Gap(8).
		Radius(8).AlignItems(ui.Center), false).Transition(revealFade)
	card.Children(func() {
		ui.Icon(c, iconAgent).FontSize(14).TextColor(p.accent).Shrink(0)
		ui.Text(c, locales.GhPromptWaiting(s.promptOf)).FontSize(12).TextColor(p.text).Shrink(1).MinWidth(0).LineHeight(1.35)
		if ui.Button(c.Key("prompt-paste"), locales.GhPasteNow()).Shrink(0).Clicked() {
			text := s.prompt
			s.prompt, s.promptOf = "", ""
			a.post(func() { s.term.Paste(text + "\n"); s.view.Focus() })
		}
		if a.smallButton(c, iconCopy, locales.GhCopyPrompt()) {
			a.services.WriteClipboard(s.prompt)
			a.tell(note{message: locales.GhPromptCopied(s.promptOf)})
		}
		if a.smallButton(c, iconX, locales.GhDropPrompt()) {
			s.prompt, s.promptOf = "", ""
		}
	})
}

// openPRWithThreads opens the changes of pull request n with its review
// threads not resolved as notes on their lines, which Send Review Items
// to Terminal hands to an agent.
func (a *app) openPRWithThreads(ctx *gitContext, n int) { ctx.openPullRequestWith(n, true) }

// threadNotes are the notes of the review threads of a pull request not
// resolved: each on its lines of the side it is on, the head's or the
// base's, with what its people said. key is the diff's, of the commits
// from and head.
func threadNotes(r *git.Repo, key, from, head string, ths []git.GHThread) []*reviewNote {
	texts := map[string][]string{}
	lines := func(rev, path string) []string {
		k := rev + "\x00" + path
		if l, ok := texts[k]; ok {
			return l
		}
		text, _, err := r.Show(rev, path)
		var l []string
		if err == nil && text != "" {
			l = strings.Split(strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n"), "\n")
		}
		texts[k] = l
		return l
	}
	var out []*reviewNote
	for _, th := range ths {
		if th.Resolved || len(th.Comments) == 0 {
			continue
		}
		path := r.Abs(th.Path)
		rev := head
		if th.OldSide {
			rev = from
		}
		start := th.StartLine
		if start <= 0 || start > th.Line {
			start = th.Line
		}
		l := lines(rev, path)
		fromLine, to := start-1, th.Line
		if fromLine < 0 || to > len(l) {
			// The lines are not in the commit the diff shows.
			continue
		}
		snippet := slices.Clone(l[fromLine:to])
		var b strings.Builder
		for i, c := range th.Comments {
			if i > 0 {
				b.WriteString("\n\n")
			}
			author := c.Author
			if author == "" {
				author = "ghost"
			}
			b.WriteString("@" + author + ": " + strings.TrimSpace(c.Body))
		}
		out = append(out, &reviewNote{path: path, commit: key, oldSide: th.OldSide, from: fromLine, to: to,
			snippet: snippet, lines: slices.Clone(snippet), text: b.String(), outdated: th.Outdated, version: unfound, thread: th.ID})
	}
	return out
}

// addThreadNotes adds the notes of the review threads of pull request n,
// but those added already.
func (a *app) addThreadNotes(n int, notes []*reviewNote) {
	r := &a.review
	added := 0
	for _, nt := range notes {
		if slices.ContainsFunc(r.notes, func(o *reviewNote) bool { return o.thread == nt.thread || o.same(nt) && o.text == nt.text }) {
			continue
		}
		r.notes = append(r.notes, nt)
		added++
	}
	if added == 0 {
		a.tell(note{message: locales.GhNoThreads("#" + strconv.Itoa(n))})
		return
	}
	r.gen++
	a.reviewChanged()
	a.tell(note{message: locales.GhThreadsAdded(added, "#"+strconv.Itoa(n)), label: locales.GhSendToTerminal(), action: a.sendReview})
}

// resolveThread marks the review thread of a note resolved on GitHub, and
// drops the note once it is.
func (a *app) resolveThread(n *reviewNote) {
	if n.thread == "" {
		return
	}
	var r *git.Repo
	for _, ctx := range a.repositories.contexts {
		if repo := ctx.git.repo; repo != nil && repo.Contains(n.path) {
			r = repo
		}
	}
	if r == nil {
		r = a.repository().git.repo
	}
	if r == nil {
		return
	}
	if !a.online() {
		a.fail("%s", locales.NotConnectedTo(a.conn.Host))
		return
	}
	id := n.thread
	bg.Go(func() {
		defer recoverCrash()
		err := r.ResolveThread(id)
		a.post(func() {
			if err != nil {
				a.fail("%s", locales.GhResolveFailed(ghMessage(err)))
				return
			}
			rv := &a.review
			rv.notes = slices.DeleteFunc(rv.notes, func(o *reviewNote) bool { return o == n })
			if rv.draft == n {
				a.endDraft()
			}
			rv.gen++
			a.reviewChanged()
			a.tell(note{message: locales.GhResolved(n.ref(a.relPath))})
		})
	})
}
