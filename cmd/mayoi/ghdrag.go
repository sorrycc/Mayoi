package main

import (
	"slices"
	"strings"

	"mayoi/internal/git"

	mtransfer "github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// linkDrag is what a row of the GitHub view drags: its issue or pull
// request, with the others chosen with it, of the repository of ctx, in
// the project from.
type linkDrag struct {
	from  *app
	ctx   *gitContext
	items func() []*git.GHItem
}

// text is what a terminal is typed of the items dragged: their links,
// between spaces, and one after them, as of paths dropped.
func (d *linkDrag) text() string {
	links := ghLinks(d.items())
	if len(links) == 0 {
		return ""
	}
	return strings.Join(links, " ") + " "
}

// dragGitHubItems makes a row of the GitHub view drag the items of
// items: to a terminal, which types their links, or to a column, which
// opens them. The drag also carries their links out to other apps, as
// text.
func (a *app) dragGitHubItems(row ui.Element, ctx *gitContext, items func() []*git.GHItem) {
	d := &linkDrag{from: a, ctx: ctx, items: items}
	row.Drag(dragItem{link: d})
	if a.host.nativeDrags() {
		row.DragDataFrom(func() mtransfer.Data { return mtransfer.TextData(strings.Join(ghLinks(items()), " ")) })
	}
	if row.Dragging() {
		a.dragNow = true
	}
}

// pasteLinks types the links of the items dragged into a terminal.
func (a *app) pasteLinks(s *session, d *linkDrag) {
	if text := d.text(); text != "" {
		s.term.Paste(text)
	}
}

// dropLinks opens the items dragged in tabs kept open at place at of a
// column, the first there and each other after it.
func (a *app) dropLinks(d *linkDrag, col *column, at int) {
	r := d.ctx.git.repo
	if r == nil {
		return
	}
	for _, it := range d.items() {
		a.openGitHubItem(d.ctx, it, true)
		var from *column
		var i int
		if it.PR {
			from, i = a.prTab(it.Number, r.Root)
		} else {
			from, i = a.issueTab(it.Number, r.Root)
		}
		if from == nil || slices.Index(a.cols, col) < 0 {
			continue
		}
		t := from.tabs[i]
		a.moveTab(t, col, at)
		if c, k := a.locate(t); c == col {
			at = k + 1
		}
	}
}

// dropLinksOnColumn types the links of the items dragged into the
// terminal a column shows, or opens them after the tab it shows.
func (a *app) dropLinksOnColumn(d *linkDrag, col *column) {
	if t := col.tab(); t != nil && t.term != nil {
		a.pasteLinks(t.term, d)
		return
	}
	a.dropLinks(d, col, col.active+1)
}

// dropLinksBeside opens the items dragged in a column of their own
// beside col.
func (a *app) dropLinksBeside(d *linkDrag, col *column, after bool) {
	nc := a.splitColumn(col, after)
	a.dropLinks(d, nc, 0)
	if len(nc.tabs) == 0 {
		a.removeColumn(nc)
	}
}
