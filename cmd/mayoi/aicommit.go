package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
	"mayoi/internal/mon"
	"mayoi/locales"
)

// aiModel is the model of Mon's AI gateway that writes commit messages,
// unless the aiModel setting names another.
const aiModel = "deepseek-flash"

// aiMaxTokens leaves room for the thinking the model does before it
// answers, as well as for the message.
const aiMaxTokens = 8192

// aiDiffMax is how much of the diff, in bytes, goes to the model: enough
// for most commits, and a big one costs no more of the day's tokens.
const aiDiffMax = 48 << 10

// aiRecent is how many of the last commits' summaries show the model the
// repository's style.
const aiRecent = 10

// aiTimeout is how long the AI has to write a message.
const aiTimeout = 2 * time.Minute

// aiSystem tells the model how to write.
const aiSystem = `You write Git commit messages. Reply with the message only: no preamble, no quotes, no Markdown, no code fences.

- Start with a summary line of at most 72 characters. Add a blank line and a short body, wrapped at 72 columns, only when the change needs explaining.
- Follow the style of the repository's recent commits: their tone, tense, capitalization, prefixes or lack of them, and length.
- Say what changed and why, as a reader of the history needs to know it. Don't list the files one by one.
- Write in the language of the recent commits, English when there are none.
- When the author gives notes, follow them.`

// aiWriting is the AI writing the commit message; cancel stops it.
type aiWriting struct {
	cancel context.CancelFunc
	// started is set once the first of the message came, which replaced
	// the box's text.
	started bool
}

// writeMessage has Mon's AI write the commit message from the changes
// that will be committed, into the box as it comes, or stops it writing.
// The box's text, if any, goes to it as the author's notes.
func (a *gitContext) writeMessage() {
	if !a.live() {
		return
	}
	g := a.git
	if w := g.writing; w != nil {
		w.cancel()
		g.writing = nil
		return
	}
	client := a.app.host.ai()
	if client == nil {
		a.app.host.signIn()
		return
	}
	if g.repo == nil {
		return
	}
	r, amend, notes, branch := g.repo, g.amend, strings.TrimSpace(g.message), a.branch()
	model := cmp.Or(strings.TrimSpace(a.app.prefs().AIModel), aiModel)
	ctx, cancel := context.WithTimeout(context.Background(), aiTimeout)
	w := &aiWriting{cancel: cancel}
	g.writing = w
	go func() {
		defer cancel()
		diff, staged, err := r.MessageDiff(amend, aiDiffMax)
		if err == nil && diff == "" {
			a.app.post(func() {
				if g.writing == w {
					g.writing = nil
					a.app.fail("%s", locales.NoChangesToCommit())
				}
			})
			return
		}
		var recent []string
		if err == nil {
			log, _ := r.Log(aiRecent, false)
			for _, c := range log {
				recent = append(recent, c.Subject)
			}
			_, err = client.StreamMessage(ctx, mon.MessageRequest{
				Model: model, MaxTokens: aiMaxTokens, System: aiSystem,
				Messages: []mon.Message{{Role: "user", Content: messagePrompt(diff, staged, amend, branch, notes, recent)}},
			}, func(s string) {
				a.app.post(func() {
					if g.writing != w {
						return
					}
					if !w.started {
						w.started, g.message = true, ""
					}
					g.message += s
				})
			})
		}
		a.app.post(func() {
			if g.writing != w {
				return
			}
			g.writing = nil
			if w.started {
				g.message = cleanMessage(g.message)
			}
			if err != nil {
				a.app.aiFailed(err)
			}
		})
	}()
}

// messagePrompt is what the model is asked: the changes and what tells of
// them.
func messagePrompt(diff string, staged, amend bool, branch, notes string, recent []string) string {
	var b strings.Builder
	if branch != "" {
		fmt.Fprintf(&b, "Branch: %s\n\n", branch)
	}
	if len(recent) > 0 {
		b.WriteString("Recent commits, newest first:\n")
		for _, s := range recent {
			b.WriteString("- " + s + "\n")
		}
		b.WriteString("\n")
	}
	if notes != "" {
		b.WriteString("Notes from the author:\n" + notes + "\n\n")
	}
	switch {
	case amend:
		b.WriteString("The commit replaces the last one, with these changes:\n")
	case staged:
		b.WriteString("The staged changes to commit:\n")
	default:
		b.WriteString("The changes to commit:\n")
	}
	b.WriteString(diff)
	return b.String()
}

// cleanMessage takes off what a model may put around a message despite
// being asked not to: a code fence and the space at its ends.
func cleanMessage(s string) string {
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(s, "```"); ok {
		if _, body, ok := strings.Cut(rest, "\n"); ok {
			s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(body), "```"))
		}
	}
	return s
}

// aiFailed says why the AI didn't write the message.
func (a *app) aiFailed(err error) {
	var limit *mon.RateLimitError
	switch {
	case errors.Is(err, context.Canceled):
	case errors.Is(err, mon.ErrUnauthorized):
		a.host.aiSignedOut()
	case errors.As(err, &limit) && limit.RetryAfter > 0 && limit.RetryAfter <= 2*time.Minute:
		a.fail("%s", locales.AiLimitSoon(int((limit.RetryAfter+time.Second-1)/time.Second)))
	case errors.As(err, &limit) && limit.RetryAfter > 0:
		a.fail("%s", locales.AiLimitLater(time.Now().Add(limit.RetryAfter).Format("15:04")))
	case errors.As(err, &limit):
		a.fail("%s", locales.AiLimitReached(limit.Message))
	case errors.Is(err, context.DeadlineExceeded):
		a.fail("%s", locales.AiTimedOut())
	default:
		var ge *git.Error
		if errors.As(err, &ge) {
			a.gitFailed(err)
			return
		}
		a.fail("%s", locales.AiFailed(syncError(err)))
	}
}

// writeButton is the button at the commit box's corner that has the AI
// write the message, or stop writing it, shown while signed in.
func (a *gitContext) writeButton(c *ui.Context) {
	g := a.git
	if a.app.host.ai() == nil && g.writing == nil {
		return
	}
	p := a.app.pal
	label := locales.ScmWriteMessage()
	if g.writing != nil {
		label = locales.ScmStopWriting()
	}
	b := ui.ButtonBase(c.Key("write-message")).Absolute().Right(4).Top(3).Size(22, 22).Radius(5).
		Justify(ui.Center).AlignItems(ui.Center).Label(label).Tooltip(label).Disabled(g.writing == nil && g.busy != "")
	if b.Hovered() {
		b.Background(p.hover)
	}
	b.Transition(hoverFade)
	b.Children(func() {
		switch {
		case g.writing != nil && b.Hovered():
			ui.Icon(c, iconStopSquare).FontSize(13).TextColor(p.accent)
		case g.writing != nil:
			ui.Spinner(c).Size(12, 12)
		default:
			ui.Icon(c, iconSparkles).FontSize(13).TextColor(p.faint)
		}
	})
	if b.Clicked() {
		a.writeMessage()
	}
}
