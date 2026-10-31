package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"mayoi/internal/bg"
	"mayoi/internal/git"
	"mayoi/locales"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

type scmPanelTab int

const (
	scmBranchesTab scmPanelTab = iota
	scmRemotesTab
)

// Each repository view registers one toolbar icon and a stable content key.
// Future views such as Stashes share the panel's header and switching behavior.
var scmPanelTabs = []struct {
	id   scmPanelTab
	key  string
	icon *ui.SVG
}{
	{scmBranchesTab, "branches", iconBranch},
	{scmRemotesTab, "remotes", iconCloud},
}

// panelTabLabel names a tab of the repository panel, by its key, in the
// language shown.
func panelTabLabel(key string) string {
	if key == "remotes" {
		return locales.PanelRemotes()
	}
	return locales.PanelBranches()
}

// Panels use the same virtual list as changes and history. Requests carry
// both a repository identity and generation so old results cannot escape
// a repository switch or ref refresh.
type scmPanels struct {
	tab             scmPanelTab
	repo            *git.Repo
	key             string
	request         *int
	gen             int
	loaded, loading bool
	// remotesLoaded is whether remotes were read since the repository was
	// opened: a read for Branches leaves those last read.
	remotesLoaded bool
	err           string
	branches      []git.Branch
	remotes       []git.Remote
	closed        map[string]bool
	logs          map[string]*scmPanelLog
}
type scmPanelLog struct {
	from, to        string
	n               int
	commits         []git.Commit
	loaded, loading bool
	err             string
	request         *int
}
type scmPanelRow struct {
	kind, label, detail string
	branch              *git.Branch
	remote              *git.Remote
	log                 string
}

func (a *gitContext) invalidateScmPanels() {
	// Changes views have separate file selection, but ref panels always belong
	// to the repository's canonical history state, never to its changes list.
	if a.git.repo != nil {
		a.app.repositoryForRoot(a.git.repo.Root).scm.panels.key = ""
	}
}

func (a *gitContext) refreshScmPanels() {
	g, p := a.git, &a.scm.panels
	if p.repo == nil && a.app.settings.ScmRemotes {
		p.tab = scmRemotesTab
	}
	if g.repo == nil || !a.app.online() || !a.app.settings.ScmRepositoryOpen {
		return
	}
	key := fmt.Sprintf("%p:%d:%d", g.repo, g.refsGen, p.tab)
	if p.repo == g.repo && p.key == key {
		return
	}
	gen, tab := p.gen+1, p.tab
	if p.repo != g.repo {
		*p = scmPanels{tab: tab}
	}
	// Keep the published snapshot while refreshing this repository. Clearing
	// it here makes disk notifications and metadata polling collapse the tree
	// into loading rows, then expand it again over several frames.
	logs := make(map[string]*scmPanelLog, len(p.logs))
	for key, log := range p.logs {
		log.loading = false
		logs[key] = log
	}
	p.repo, p.key, p.request, p.gen = g.repo, key, new(int), gen
	p.loading, p.err, p.logs = true, "", logs
	if p.closed == nil {
		p.closed = map[string]bool{}
	}
	repo, request := g.repo, p.request
	openRemotes := map[string]bool{}
	for key, closed := range p.closed {
		if strings.HasPrefix(key, "panel:remote:") && !closed {
			openRemotes[strings.TrimPrefix(key, "panel:remote:")] = true
		}
	}
	bg.Go(func() {
		defer recoverCrash()
		branches, err := repo.PanelBranches()
		var remotes []git.Remote
		if err == nil && tab == scmRemotesTab {
			var names []string
			var urls map[string]string
			names, err = repo.Remotes()
			if err == nil {
				urls, err = repo.ConfiguredRemoteURLs()
			}
			for _, name := range names {
				remote := git.Remote{Name: name}
				if openRemotes[name] {
					remote, err = repo.RemoteURLs(name)
					if err != nil {
						break
					}
				}
				remote.URL = urls[name]
				remotes = append(remotes, remote)
			}
		}
		a.app.post(func() {
			if a.git.repo != repo || p.repo != repo || p.key != key || p.request != request {
				return
			}
			p.loading = false
			if err != nil {
				p.err = err.Error()
			} else {
				p.branches, p.loaded = branches, true
				if tab == scmRemotesTab {
					p.remotes, p.remotesLoaded = remotes, true
				}
			}
			p.gen++
		})
	})
}

func panelRef(b git.Branch) string {
	if b.Remote {
		return "refs/remotes/" + b.Name
	}
	return "refs/heads/" + b.Name
}

func (a *gitContext) panelLog(key, from, to string) *scmPanelLog {
	p := &a.scm.panels
	l := p.logs[key]
	if l == nil {
		l = &scmPanelLog{from: from, to: to, n: historyPage}
		p.logs[key] = l
	}
	if p.loading || l.loading || (l.loaded || l.err != "") && l.request == p.request {
		return l
	}
	l.loading, l.request = true, p.request
	repo, token := p.repo, p.request
	n := l.n
	bg.Go(func() {
		defer recoverCrash()
		var commits []git.Commit
		var err error
		if from == "" {
			commits, err = repo.LogRef(to, n)
		} else {
			commits, err = repo.LogRange(from, to, n)
		}
		a.app.post(func() {
			if a.git.repo != repo || p.repo != repo || p.request != token || p.logs[key] != l {
				return
			}
			l.loading = false
			if l.n != n {
				// A further page was requested during this read. Keep the
				// published commits and let the next frame read the new limit.
				l.request = nil
				p.gen++
				return
			}
			if err != nil {
				l.err = err.Error()
			} else {
				l.commits = commits
				l.loaded = true
				l.err = ""
			}
			p.gen++
		})
	})
	return l
}

func panelRow(kind, key, label, detail string, depth int) scmRow {
	return scmRow{key: key, depth: depth, panel: &scmPanelRow{kind: kind, label: label, detail: detail}}
}
func (a *gitContext) scmPanelRows(rows []scmRow) []scmRow {
	p := &a.scm.panels
	if a.git.repo == nil {
		return rows
	}
	header := panelRow("tabs", "panel:tabs", strings.ToUpper(locales.PanelRepository()), "", 0)
	header.header = true
	rows = append(rows, header)
	if !a.app.settings.ScmRepositoryOpen {
		return rows
	}
	addStatus := func(key string, depth int) {
		label, kind := locales.Loading(), "status"
		if p.err != "" {
			label, kind = locales.PanelReadRefsFailed(), "retry"
		} else if p.loaded && (key != "remotes" || p.remotesLoaded) {
			label = locales.PanelNoBranches()
			if key == "remotes" {
				label = locales.PanelNoRemotes()
				kind = "addRemote"
			}
		}
		rows = append(rows, panelRow(kind, key+":status", label, p.err, depth))
	}
	addLog := func(key, from, to string, depth int) {
		l := a.panelLog(key, from, to)
		switch {
		case l.loading && !l.loaded:
			rows = append(rows, panelRow("status", key+":loading", locales.PanelLoadingCommits(), "", depth))
		case l.err != "":
			r := panelRow("retryLog", key+":error", locales.PanelReadCommitsFailed(), l.err, depth)
			r.panel.log = key
			rows = append(rows, r)
			if !l.loaded {
				return
			}
			fallthrough
		default:
			if len(l.commits) == 0 {
				rows = append(rows, panelRow("status", key+":empty", locales.PanelNoCommits(), "", depth))
			}
			for i := range l.commits {
				rows = append(rows, scmRow{key: key + ":" + l.commits[i].Hash, commit: &l.commits[i], at: -1, depth: depth})
			}
			if len(l.commits) >= l.n {
				r := panelRow("more", key+":more", locales.PanelLoadMore(), "", depth)
				r.panel.log = key
				rows = append(rows, r)
			}
		}
	}
	addBranch := func(b *git.Branch, depth int, remote *git.Remote) {
		key := "panel:branch:" + panelRef(*b)
		detail := b.Upstream
		if b.Gone {
			detail += locales.PanelGone()
		} else if b.Upstream != "" {
			// As the status bar counts them, leaving out a direction at 0.
			if b.Behind > 0 {
				detail += fmt.Sprintf(" ↓%d", b.Behind)
			}
			if b.Ahead > 0 {
				detail += fmt.Sprintf(" ↑%d", b.Ahead)
			}
		}
		if !b.Time.IsZero() {
			detail += " · " + shortAgo(b.Time, time.Now())
		}
		r := panelRow("branch", key, b.Name, strings.TrimPrefix(detail, " · "), depth)
		r.panel.branch = b
		r.panel.remote = remote
		rows = append(rows, r)
		if !p.closed[key] {
			if b.Upstream != "" && !b.Gone {
				for _, incoming := range []bool{true, false} {
					label, n, from, to := locales.PanelOutgoing(), b.Ahead, b.UpstreamRef, panelRef(*b)
					if incoming {
						label, n, from, to = locales.PanelIncoming(), b.Behind, panelRef(*b), b.UpstreamRef
					}
					if n == 0 {
						continue
					}
					k := key + ":" + strings.ToLower(label)
					q := panelRow("range", k, locales.PanelRangeCommits(label, n), "", depth+1)
					q.panel.log = k
					rows = append(rows, q)
					if !p.closed[k] {
						addLog(k, from, to, depth+2)
					}
				}
			}
			addLog(key+":history", "", panelRef(*b), depth+1)
		}
	}
	// Branches default closed; opening a branch is the first read of its log.
	section := scmPanelTabs[p.tab].key
	r := panelRow("section", "panel:"+section, strings.ToUpper(panelTabLabel(section)), "", 0)
	r.header = true
	if section == "branches" {
		for _, b := range p.branches {
			if !b.Remote {
				r.count++
			}
		}
	} else {
		r.count = len(p.remotes)
	}
	rows = append(rows, r)
	if !p.loaded || p.err != "" || section == "remotes" && !p.remotesLoaded {
		addStatus(section, 1)
		if !p.loaded || section == "remotes" && !p.remotesLoaded {
			return rows
		}
	}
	count := 0
	if section == "branches" {
		for i := range p.branches {
			b := &p.branches[i]
			if b.Remote {
				continue
			}
			count++
			key := "panel:branch:" + panelRef(*b)
			if _, ok := p.closed[key]; !ok {
				p.closed[key] = true
			}
			addBranch(b, 1, nil)
		}
	} else if section == "remotes" {
		for i := range p.remotes {
			remote := &p.remotes[i]
			count++
			key := "panel:remote:" + remote.Name
			if _, ok := p.closed[key]; !ok {
				p.closed[key] = true
			}
			r := panelRow("remote", key, remote.Name, "", 1)
			// Its URL rows say where it points once it opens.
			if p.closed[key] && remote.URL != "" {
				r.panel.detail = shortRemoteURL(remote.URL)
			}
			r.panel.remote = remote
			rows = append(rows, r)
			if p.closed[key] {
				continue
			}
			// A remote that pushes where it fetches, as most do, has one row
			// for its URL.
			fetchKind, same := "fetch", slices.Equal(remote.FetchURLs, remote.PushURLs)
			if same {
				fetchKind = "fetch, push"
			}
			for _, url := range remote.FetchURLs {
				q := panelRow("url", key+":fetch:"+url, url, fetchKind, 2)
				q.panel.remote = remote
				rows = append(rows, q)
			}
			for _, url := range remote.PushURLs {
				if same {
					break
				}
				q := panelRow("url", key+":push:"+url, url, "push", 2)
				q.panel.remote = remote
				rows = append(rows, q)
			}
			for j := range p.branches {
				b := &p.branches[j]
				if !b.Remote || !strings.HasPrefix(b.Name, remote.Name+"/") {
					continue
				}
				k := "panel:branch:" + panelRef(*b)
				if _, ok := p.closed[k]; !ok {
					p.closed[k] = true
				}
				addBranch(b, 2, remote)
			}
		}
	}
	if count == 0 && p.err == "" {
		addStatus(section, 1)
	}
	return rows
}

func (a *gitContext) activateScmPanelRow(r scmRow) {
	p := &a.scm.panels
	switch r.panel.kind {
	case "tabs":
		a.app.settings.ScmRepositoryOpen = !a.app.settings.ScmRepositoryOpen
		a.app.settings.Changed()
		p.gen++
	case "branch", "remote", "range":
		p.closed[r.key] = !p.closed[r.key]
		if r.panel.kind == "remote" {
			a.invalidateScmPanels()
		}
		p.gen++
	case "retry":
		a.invalidateScmPanels()
	case "retryLog":
		if l := p.logs[r.panel.log]; l != nil {
			l.request = nil
			l.err = ""
			p.gen++
		}
	case "more":
		if l := p.logs[r.panel.log]; l != nil {
			l.n += historyPage
			l.request = nil
			p.gen++
		}
	case "addRemote":
		a.askPanelRemote(nil, false)
	}
}

// shortRemoteURL is where a remote's URL points, as host and path:
// git@github.com:me/app.git and https://github.com/me/app both read
// github.com/me/app.
func shortRemoteURL(url string) string {
	s := url
	if _, rest, ok := strings.Cut(s, "://"); ok {
		s = rest
	} else if user, rest, ok := strings.Cut(s, "@"); ok && !strings.Contains(user, "/") {
		s = strings.Replace(rest, ":", "/", 1)
	}
	if i := strings.Index(s, "@"); i >= 0 && i < strings.Index(s+"/", "/") {
		s = s[i+1:]
	}
	return strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
}

func scmPanelIndent(depth int) float32 { return 4 + float32(depth)*10 }

// A child without a disclosure arrow starts in the tree's content column.
func scmPanelContentIndent(depth int) float32 { return scmPanelIndent(depth) + 17 }

func (a *gitContext) switchScmPanelTab(tab scmPanelTab) {
	p := &a.scm.panels
	if p.tab == tab && a.app.settings.ScmRepositoryOpen {
		return
	}
	p.tab = tab
	a.app.settings.ScmRepositoryOpen = true
	a.app.settings.ScmRemotes = tab == scmRemotesTab
	a.app.settings.Changed()
	p.gen++
	a.scm.row = -1
	a.scm.selection.Clear()
	a.scm.anchor = ""
}

func (a *gitContext) scmPanelTabsView(c *ui.Context, r scmRow, action *func()) {
	p := a.app.pal
	row := ui.Row(c).Grow(1).Height(26).Gap(4).Padding(0, 4, 0, 0).AlignItems(ui.Center).Background(p.chrome)
	clicked := row.Clicked()
	row.Children(func() {
		chevron(c, a.app.settings.ScmRepositoryOpen).FontSize(12).TextColor(p.muted)
		ui.Text(c, r.panel.label).FontSize(11).FontWeight(600).LetterSpacing(0.4).TextColor(p.muted).SingleLine().Shrink(1)
		ui.Spacer(c)
		for _, tab := range scmPanelTabs {
			active := a.scm.panels.tab == tab.id
			button := ui.ButtonBase(c.Key("repository-tab:"+tab.key)).Size(26, 24).Radius(3).AlignItems(ui.Center).Justify(ui.Center).Label(panelTabLabel(tab.key)).Tooltip(panelTabLabel(tab.key))
			color := p.muted
			if active {
				button.Background(p.active)
				color = p.accent
			} else if button.Hovered() {
				button.Background(p.hover)
				color = p.text
			}
			button.Transition(hoverFade)
			pressPose(button)
			button.Children(func() { ui.Icon(c, tab.icon).FontSize(16).TextColor(color) })
			if button.Clicked() {
				*action = func() { a.switchScmPanelTab(tab.id) }
			}
		}
	})
	if clicked && *action == nil {
		*action = func() { a.activateScmPanelRow(r) }
	}
}

func (a *gitContext) scmPanelRowView(c *ui.Context, r scmRow, action *func()) {
	if r.panel.kind == "tabs" {
		a.scmPanelTabsView(c, r, action)
		return
	}
	p := a.app.pal
	q := r.panel
	indent := scmPanelContentIndent(r.depth)
	switch q.kind {
	case "section":
		indent = 16 // Align beneath the Repository title, after its disclosure.
	case "branch", "remote":
		indent = scmPanelIndent(r.depth)
	}
	row := ui.Row(c).Grow(1).Gap(5).AlignItems(ui.Center).Height(22).Padding(0, 4, 0, indent)
	tip := q.label
	if q.branch != nil && q.branch.Current {
		tip += locales.PanelCurrentBranch()
	}
	if q.detail != "" {
		tip += "\n" + q.detail
	}
	if q.branch != nil {
		tip += "\n" + q.branch.Subject
		if !q.branch.Time.IsZero() {
			tip += "\n" + i18n.FormatDate(i18n.Locale(), q.branch.Time.Local(), i18n.DateOptions{Skeleton: "yMMMdHmsz"})
		}
	}
	row.Tooltip(tip)
	hovered := row.Hovered()
	clicked := row.Clicked()
	buttonHovered := false
	if hovered && !r.header {
		row.Background(p.hover)
	}
	if r.header {
		row.Background(p.chrome)
	}
	row.ContextMenu(func(m *ui.Menu) { a.scmPanelMenu(m, r, action) })
	row.Transition(hoverFade)
	row.Children(func() {
		switch q.kind {
		case "branch", "remote", "range":
			chevron(c, !a.scm.panels.closed[r.key]).FontSize(12).TextColor(p.muted)
		}
		switch q.kind {
		case "branch":
			ui.Icon(c, iconBranch).FontSize(13).TextColor(p.muted)
		case "remote":
			ui.Icon(c, iconCloud).FontSize(13).TextColor(p.muted)
		}
		col := p.text
		if q.branch != nil && q.branch.Current {
			col = p.accent
		}
		if q.kind == "url" {
			// Which way the URL goes leads, so a long URL cannot cut it off.
			ui.Text(c, q.detail).FontSize(11).TextColor(p.muted).SingleLine().Shrink(0)
		}
		text := ui.Text(c, q.label).FontSize(12.5).TextColor(col).SingleLine().Shrink(1)
		if r.header {
			text.FontSize(10).FontWeight(500).LetterSpacing(0.2).TextColor(p.muted)
			ui.Text(c, fmt.Sprint(r.count)).FontSize(10).TextColor(p.faint)
		}
		if q.detail != "" && q.kind != "url" {
			ui.Text(c, q.detail).FontSize(11).TextColor(p.muted).SingleLine().Shrink(2)
		}
		ui.Spacer(c)
		if q.branch != nil {
			b := *q.branch
			visible := hovered || row.FocusWithin()
			moving := a.git.busy != "" || a.git.status != nil && a.git.status.Op != ""
			button := func(icon *ui.SVG, label string, disabled bool, run func()) {
				btn := ui.ButtonBase(c.Key(label)).Size(20, 20).Radius(4).Justify(ui.Center).AlignItems(ui.Center).
					Label(label + " " + b.Name).Tooltip(label).Disabled(disabled)
				if !visible {
					btn.Opacity(0)
				}
				col := p.muted
				if disabled {
					col = p.faint
				}
				if btn.Hovered() {
					buttonHovered = true
					btn.Background(p.active)
				}
				btn.Transition(hoverFade)
				pressPose(btn)
				btn.Children(func() { ui.Icon(c, icon).FontSize(13).TextColor(col) })
				if btn.Clicked() {
					*action = run
				}
			}
			button(iconCheckout, locales.PanelCheckOutBranch(), moving || b.Current, func() { a.checkoutPanelBranch(b) })
			if b.Remote {
				button(iconFetch, locales.PanelFetchRemote(), a.git.busy != "" || q.remote == nil, func() {
					if q.remote != nil {
						name := q.remote.Name
						a.gitDo(locales.BusyFetchingRemote(name), func(repo *git.Repo) error { return repo.FetchRemote(name, false) }, nil)
					}
				})
			} else {
				button(iconPull, locales.PanelPullBranch(), moving || b.UpstreamRef == "" || b.Gone, func() { a.pullPanelBranch(b) })
			}
		}
		if r.header {
			if r.key == "panel:branches" && a.app.smallButton(c, iconPlus, locales.CreateBranchEllipsis()) {
				*action = a.askPanelNewBranch
			}
			if r.key == "panel:remotes" && a.app.smallButton(c, iconPlus, locales.PanelAddRemote()) {
				*action = func() { a.askPanelRemote(nil, false) }
			}
			if a.app.smallButton(c, iconRefresh, locales.Refresh()) {
				*action = a.invalidateScmPanels
			}
		}
	})
	if clicked && *action == nil && !buttonHovered {
		*action = func() { a.activateScmPanelRow(r) }
	}
}

func (a *gitContext) scmPanelMenu(m *ui.Menu, r scmRow, action *func()) {
	repo := a.git.repo
	defer func() {
		if *action == nil {
			return
		}
		run := *action
		*action = func() {
			if a.git.repo != repo {
				return
			}
			run()
			a.bindPanelDialogs(repo)
		}
	}()
	q := r.panel
	busy := a.git.busy != ""
	moving := busy || a.git.status != nil && a.git.status.Op != ""
	if q.branch != nil {
		b := *q.branch
		if m.Item(locales.PanelCheckOutBranch()).Disabled(moving || b.Current).Chosen() {
			*action = func() { a.checkoutPanelBranch(b) }
		}
		if m.Item(locales.ScmCreateBranchHere()).Disabled(busy).Chosen() {
			*action = func() { a.askRefAt(git.Commit{Hash: b.Hash, Subject: b.Subject}, false) }
		}
		if !b.Remote {
			if m.Item(locales.PanelRenameBranch()).Disabled(busy).Chosen() {
				*action = func() { a.askRenamePanelBranch(b) }
			}
			if m.Item(locales.PanelDeleteBranch()).Disabled(moving || b.Current).Chosen() {
				*action = func() {
					a.app.confirm = &confirm{title: locales.PanelDeleteBranchQuestion(b.Name), ok: locales.ButtonDelete(), danger: true, message: locales.PanelDeleteBranchDetail(), run: func() {
						a.gitDo(locales.BusyDeletingBranch(), func(repo *git.Repo) error { return repo.DeleteBranch(b.Name, false) }, nil)
					}}
				}
			}
		}
		m.Separator()
		if m.Item(locales.PanelMergeIntoCurrent()).Disabled(moving || b.Current).Chosen() {
			*action = func() { a.mergeCommit(git.Commit{Hash: b.Hash, Subject: b.Name}) }
		}
		if m.Item(locales.PanelRebaseCurrentOnto()).Disabled(moving || b.Current).Chosen() {
			*action = func() { a.rebaseOnto(git.Commit{Hash: b.Hash, Subject: b.Name}) }
		}
		if m.Item(locales.PanelCopyBranchName()).Chosen() {
			a.app.services.WriteClipboard(b.Name)
		}
		return
	}
	if q.remote != nil {
		remote := *q.remote
		if m.Item(locales.CmdGitFetch()).Disabled(busy).Chosen() {
			*action = func() {
				a.gitDo(locales.BusyFetching(), func(repo *git.Repo) error { return repo.FetchRemote(remote.Name, false) }, nil)
			}
		}
		if m.Item(locales.PanelFetchAndPrune()).Disabled(busy).Chosen() {
			*action = func() {
				a.askPrunePanelRemote(remote.Name, true)
			}
		}
		if m.Item(locales.PanelPruneRemote()).Disabled(busy).Chosen() {
			*action = func() {
				a.app.confirm = &confirm{title: locales.PanelPruneQuestion(remote.Name), ok: locales.PanelPrune(), message: locales.PanelPruneDetail(), run: func() {
					a.gitDo(locales.BusyPruning(), func(repo *git.Repo) error { return repo.PruneRemote(remote.Name) }, nil)
				}}
			}
		}
		m.Separator()
		if m.Item(locales.PanelEditFetchURL()).Disabled(busy).Chosen() {
			*action = func() { a.editPanelRemote(remote.Name, false) }
		}
		if m.Item(locales.PanelEditPushURL()).Disabled(busy).Chosen() {
			*action = func() { a.editPanelRemote(remote.Name, true) }
		}
		if m.Item(locales.PanelRemoveRemote()).Disabled(busy).Chosen() {
			*action = func() {
				a.app.confirm = &confirm{title: locales.PanelRemoveRemoteQuestion(remote.Name), ok: locales.PanelRemove(), danger: true, message: locales.PanelRemoveRemoteDetail(), run: func() {
					a.gitDo(locales.BusyRemovingRemote(), func(repo *git.Repo) error { return repo.RemoveRemote(remote.Name) }, nil)
				}}
			}
		}
		if q.kind == "url" {
			if m.Item(locales.PanelCopyURL()).Chosen() {
				a.app.services.WriteClipboard(q.label)
			}
		} else {
			for _, url := range remote.FetchURLs {
				if m.Item(locales.CopiedTextItem(url)).Chosen() {
					a.app.services.WriteClipboard(url)
				}
			}
		}
	}
	if q.kind == "section" && r.key == "panel:branches" {
		if m.Item(locales.CreateBranchEllipsis()).Disabled(busy).Chosen() {
			*action = a.askPanelNewBranch
		}
	} else if q.kind == "remote" || q.kind == "url" || q.kind == "addRemote" || q.kind == "section" && r.key == "panel:remotes" {
		if m.Item(locales.PanelAddRemote()).Disabled(busy).Chosen() {
			*action = func() { a.askPanelRemote(nil, false) }
		}
	}
	if m.Item(locales.Refresh()).Chosen() {
		*action = a.invalidateScmPanels
	}
}

func (a *gitContext) askRenamePanelBranch(b git.Branch) {
	defer a.bindPanelDialogs(a.git.repo)
	a.app.prompt = &prompt{title: locales.PanelRenameBranch(), value: b.Name, done: func(name string) string {
		name = strings.TrimSpace(name)
		if name == "" {
			return locales.PanelEnterBranchName()
		}
		a.gitDo(locales.BusyRenamingBranch(), func(repo *git.Repo) error { return repo.RenameBranch(b.Name, name) }, nil)
		return ""
	}}
}

// Resolve URLs when editing even if the remote's tree is still collapsed.
func (a *gitContext) editPanelRemote(name string, push bool) {
	repo := a.git.repo
	if repo == nil {
		return
	}
	bg.Go(func() {
		remote, err := repo.RemoteURLs(name)
		a.app.post(func() {
			if a.git.repo != repo {
				return
			}
			if err != nil {
				a.gitFailed(err)
				return
			}
			a.askPanelRemote(&remote, push)
		})
	})
}

func (a *gitContext) askPanelRemote(remote *git.Remote, push bool) {
	repo := a.git.repo
	defer a.bindPanelDialogs(repo)
	if remote == nil {
		a.app.prompt = &prompt{title: locales.PanelAddRemote(), hint: locales.PanelRemoteNameHint(), done: func(name string) string {
			name = strings.TrimSpace(name)
			if name == "" {
				return locales.PanelEnterRemoteName()
			}
			// Defer the next prompt because the current prompt closes after done.
			a.app.post(func() {
				if a.git.repo == repo {
					a.askPanelRemote(&git.Remote{Name: name}, false)
				}
			})
			return ""
		}}
		return
	}
	name := remote.Name
	title := locales.PanelAddRemoteURL()
	value := ""
	existing := len(remote.FetchURLs) > 0 || len(remote.PushURLs) > 0
	if existing {
		title = locales.PanelEditFetchURL()
		if len(remote.FetchURLs) > 0 {
			value = remote.FetchURLs[0]
		}
	}
	if push {
		title = locales.PanelEditPushURL()
		if len(remote.PushURLs) > 0 {
			value = remote.PushURLs[0]
		}
	}
	a.app.prompt = &prompt{title: title, hint: locales.PanelURLFor(name), value: value, done: func(url string) string {
		url = strings.TrimSpace(url)
		if url == "" {
			return locales.PanelEnterRemoteURL()
		}
		a.gitDo(locales.BusyUpdatingRemote(), func(repo *git.Repo) error {
			if !existing {
				return repo.AddRemote(name, url)
			}
			return repo.SetRemoteURL(name, url, push)
		}, nil)
		return ""
	}}
}

// Dialogs remain tied to the repository where their command was chosen.
// A folder switch may leave a captured callback alive until the next frame.
func (a *gitContext) bindPanelDialogs(repo *git.Repo) {
	if pr := a.app.prompt; pr != nil {
		done := pr.done
		pr.done = func(value string) string {
			if a.git.repo != repo {
				return locales.PanelRepoChanged()
			}
			return done(value)
		}
	}
	if cf := a.app.confirm; cf != nil {
		run := cf.run
		cf.run = func() {
			if a.git.repo == repo && run != nil {
				run()
			}
		}
	}
}
func (a *gitContext) askPanelNewBranch() {
	a.askNewBranch()
}
func (a *gitContext) askPrunePanelRemote(name string, fetch bool) {
	defer a.bindPanelDialogs(a.git.repo)
	title, ok := locales.PanelPruneQuestion(name), locales.PanelPrune()
	if fetch {
		title, ok = locales.PanelFetchPruneQuestion(name), locales.PanelFetchAndPruneButton()
	}
	a.app.confirm = &confirm{title: title, ok: ok, message: locales.PanelPruneDetail(), run: func() {
		a.gitDo(locales.BusyPruning(), func(repo *git.Repo) error {
			if fetch {
				return repo.FetchRemote(name, true)
			}
			return repo.PruneRemote(name)
		}, nil)
	}}
}

func (a *gitContext) checkoutPanelBranch(b git.Branch) {
	if a.git.busy != "" || b.Current || a.git.status != nil && a.git.status.Op != "" {
		return
	}
	if !b.Remote {
		a.switchBranch(b.Name, false)
		return
	}
	if !a.docsSaved(locales.BeforeSwitching()) {
		return
	}
	_, name, ok := strings.Cut(b.Name, "/")
	if !ok {
		a.app.fail("%s", locales.FailInvalidRemoteBranch(b.Name))
		return
	}
	a.gitDo(locales.BusyCheckingOutPlain(), func(repo *git.Repo) error { return repo.CheckoutTracking(b.Name, name) }, nil)
}

func (a *gitContext) pullPanelBranch(b git.Branch) {
	if a.git.busy != "" || b.Remote || b.UpstreamRef == "" || b.Gone || a.git.status != nil && a.git.status.Op != "" {
		return
	}
	if b.Current && !a.docsSaved(locales.BeforePullingBranch()) {
		return
	}
	a.gitDo(locales.BusyPullingBranch(b.Name), func(repo *git.Repo) error { return repo.PullBranch(b.Name) }, nil)
}
