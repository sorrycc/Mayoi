package main

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/remote"
	"mayoi/internal/workspace"
	"mayoi/locales"
)

// remotePicker is the box that connects to a host and chooses a folder
// on it: first the hosts, of ~/.ssh/config and those connected to before,
// then, once connected, the folders of the host.
type remotePicker struct {
	open  bool
	stage pickStage
	query string
	sel   int
	moved bool
	err   string
	// toEnd puts the caret at the end of the query, which the box changed.
	toEnd bool

	// hosts are the hosts to choose from, names their rows, recent those
	// of the recent folders, with the folder opened last on each, resolved
	// who and where each signs in as, as ssh's config says, and results
	// those the query matches.
	hosts    []string
	recent   map[string]string
	resolved map[string]string
	results  []workspace.Match
	shown    string
	stale    bool

	// conn is the connection to the host chosen, held while the box is
	// open, and unsub stops following it; since is when it started to
	// connect.
	conn  *remote.Conn
	unsub func()
	since time.Time
	// dir is the folder listed on the host, dirs its folders, listing set
	// while it is read and listErr why it could not be; checking is set
	// while the folder to open is looked at.
	dir      string
	dirs     []string
	listErr  error
	listing  bool
	listed   string
	checking bool
}

type pickStage int

const (
	pickHost pickStage = iota
	pickConnecting
	pickFolder
)

// The rows of the folders that are not folders in the one listed: the
// folder typed, to open it, and the one above.
const (
	rowOpen   = ""
	rowParent = ".."
)

// recentHosts are the hosts of the recent folders, the latest first, with
// the folder opened last on each.
func (a *app) recentHosts() ([]string, map[string]string) {
	var hosts []string
	last := map[string]string{}
	for _, d := range a.settings.Recent {
		if h, rp, ok := remote.Split(d); ok && !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
			last[h] = rp
		}
	}
	return hosts, last
}

// openRemotePicker shows the box of the hosts.
func (a *app) openRemotePicker() {
	if !remoteSupported {
		return
	}
	a.closeRemotePicker()
	r := &a.remotePick
	*r = remotePicker{open: true, stale: true, resolved: map[string]string{}}
	r.hosts, r.recent = a.recentHosts()
	for _, h := range remote.ConfigHosts() {
		if !slices.Contains(r.hosts, h) {
			r.hosts = append(r.hosts, h)
		}
	}
	// Who and where each signs in as, which ssh -G tells: known at once
	// for those asked before, the others a few at a time.
	var ask []string
	for _, h := range r.hosts {
		if s, ok := remote.Resolved(h); ok {
			r.resolved[h] = s
		} else {
			ask = append(ask, h)
		}
	}
	go func() {
		var wg sync.WaitGroup
		gate := make(chan struct{}, 4)
		for _, h := range ask {
			wg.Add(1)
			gate <- struct{}{}
			go func() {
				defer func() { <-gate; wg.Done() }()
				if s := remote.Resolve(h); s != "" {
					a.post(func() {
						if a.remotePick.resolved != nil {
							a.remotePick.resolved[h] = s
						}
					})
				}
			}()
		}
		wg.Wait()
	}()
}

// openRemoteFolder shows the box of the folders of the folder's host, or
// of the hosts for a folder of this machine.
func (a *app) openRemoteFolder() {
	if a.conn == nil || a.connState != remote.Connected {
		a.openRemotePicker()
		return
	}
	a.openRemotePicker()
	a.chooseHost(a.conn.Host)
}

// closeRemotePicker hides the box, letting go of the connection it held,
// which ends unless a folder uses it.
func (a *app) closeRemotePicker() {
	r := &a.remotePick
	if !r.open {
		return
	}
	r.open = false
	a.releasePick()
	if a.focus != paneTerminal {
		a.focusColumn()
	}
}

func (a *app) releasePick() {
	r := &a.remotePick
	if r.conn != nil {
		r.unsub()
		c := r.conn
		r.conn, r.unsub = nil, nil
		go c.Release()
	}
}

// chooseHost connects to host, then lists its folders.
func (a *app) chooseHost(host string) {
	r := &a.remotePick
	host = strings.TrimSpace(host)
	if err := remote.CheckHost(host); err != nil {
		r.err = err.Error()
		return
	}
	a.releasePick()
	c := remote.For(host)
	c.Acquire()
	r.conn, r.err, r.since = c, "", time.Now()
	r.unsub = c.Subscribe(func() { a.post(a.pickConnChanged) })
	r.stage, r.query = pickConnecting, ""
	c.Connect()
	a.pickConnChanged()
}

// cancelConnect goes back from connecting to the hosts, with the host
// chosen typed.
func (a *app) cancelConnect() {
	r := &a.remotePick
	if r.conn == nil {
		return
	}
	host := r.conn.Host
	a.releasePick()
	r.stage, r.query, r.err, r.sel, r.toEnd = pickHost, host, "", 0, true
}

// pickConnChanged follows the connection of the box: once connected, it
// lists the home folder, or the folder of the host opened last.
func (a *app) pickConnChanged() {
	r := &a.remotePick
	if !r.open || r.conn == nil || r.stage != pickConnecting {
		return
	}
	switch s, err := r.conn.State(); s {
	case remote.Connected:
		start := r.conn.Info().Home
		for _, d := range a.settings.Recent {
			if host, rp, ok := remote.Split(d); ok && host == r.conn.Host && d != a.root {
				start = rp
				break
			}
		}
		r.stage, r.sel, r.toEnd = pickFolder, 0, true
		r.query = strings.TrimSuffix(start, "/") + "/"
		r.listed = ""
	case remote.Offline:
		r.stage, r.toEnd = pickHost, true
		r.query = r.conn.Host
		if err != nil {
			r.err = err.Error()
		} else {
			r.err = locales.PickCouldNotConnect(r.conn.Host)
		}
		a.releasePick()
	}
}

// pickDir is the folder the query of the box lists, and the start of the
// name of the folder in it typed so far.
func pickDir(query string) (dir, prefix string) {
	q := strings.TrimSpace(query)
	if q == "" {
		return "/", ""
	}
	if strings.HasSuffix(q, "/") {
		return path.Clean(q), ""
	}
	return path.Dir(path.Clean(q)), path.Base(q)
}

// expandHome makes a query that starts with ~ start with the home folder
// of the host instead, as a shell would.
func expandHome(query, home string) string {
	if home == "" {
		return query
	}
	switch {
	case query == "~":
		return strings.TrimSuffix(home, "/") + "/"
	case strings.HasPrefix(query, "~/"):
		return strings.TrimSuffix(home, "/") + query[1:]
	}
	return query
}

// listPick reads the folders of the folder the query names, in the
// background, when it changed.
func (a *app) listPick() {
	r := &a.remotePick
	dir, _ := pickDir(r.query)
	if r.conn == nil || r.listing || dir == r.listed || !strings.HasPrefix(dir, "/") {
		return
	}
	r.listing, r.listed = true, dir
	c := r.conn
	go func() {
		entries, err := c.ReadDir(dir)
		a.post(func() {
			r.listing = false
			if r.conn != c || r.listed != dir {
				return
			}
			r.dir, r.dirs, r.listErr = dir, nil, err
			for _, e := range entries {
				if e.Dir {
					r.dirs = append(r.dirs, e.Name)
				}
			}
			slices.SortFunc(r.dirs, func(x, y string) int {
				// Hidden folders last.
				if hx, hy := strings.HasPrefix(x, "."), strings.HasPrefix(y, "."); hx != hy {
					if hx {
						return 1
					}
					return -1
				}
				return strings.Compare(strings.ToLower(x), strings.ToLower(y))
			})
		})
	}()
}

// pickRows are the rows of the folders of the box: the folder typed, to
// open it, the one above it, then the folders in it whose names start as
// the query's last part does.
func (a *app) pickRows() []string {
	r := &a.remotePick
	dir, prefix := pickDir(r.query)
	rows := []string{rowOpen}
	if prefix == "" && dir != "/" {
		rows = append(rows, rowParent)
	}
	if r.dir != dir {
		return rows
	}
	for _, name := range r.dirs {
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) &&
			(prefix != "" || !strings.HasPrefix(name, ".")) {
			rows = append(rows, name)
		}
	}
	return rows
}

// pickFolderRow goes into the folder of a row, or up for the one above,
// or opens the folder typed for the first, as it is there.
func (a *app) pickFolderRow(i int) {
	r := &a.remotePick
	rows := a.pickRows()
	if i < 0 || i >= len(rows) {
		return
	}
	dir, _ := pickDir(r.query)
	switch rows[i] {
	case rowParent:
		r.query = strings.TrimSuffix(path.Dir(dir), "/") + "/"
		r.sel, r.err, r.moved, r.toEnd = 0, "", true, true
		return
	case rowOpen:
	default:
		r.query = path.Join(dir, rows[i]) + "/"
		r.sel, r.err, r.moved, r.toEnd = 0, "", true, true
		return
	}
	target := path.Clean(strings.TrimSpace(r.query))
	if !strings.HasPrefix(target, "/") {
		r.err = locales.EnterPathFromRoot()
		return
	}
	if r.checking || r.conn == nil {
		return
	}
	r.checking, r.err = true, ""
	c := r.conn
	go func() {
		info, err := c.Stat(target)
		a.post(func() {
			r.checking = false
			if r.conn != c || !r.open {
				return
			}
			switch {
			case errors.Is(err, fs.ErrNotExist):
				r.err = locales.PickNoFolder(target)
			case err != nil:
				r.err = err.Error()
			case !info.IsDir():
				r.err = locales.PickNotFolder(target)
			default:
				a.openRemote(remote.Path(c.Host, target))
			}
		})
	}()
}

// openRemote opens a remote folder as Open Folder opens one: in the
// window when it shows nothing, else in a window, or a project tab, of
// its own.
func (a *app) openRemote(dir string) {
	// The folder holds the connection before the box lets go of it.
	if a.blank() {
		a.switchFolder(dir)
	} else {
		a.host.newWindow(dir)
	}
	a.closeRemotePicker()
}

// pickRowH is the height of a row of the box.
const pickRowH = 30

// remotePickerView shows the box of the hosts, or of the folders of the
// host chosen, while it is open.
func (a *app) remotePickerView(c *ui.Context) {
	r := &a.remotePick
	if !r.open {
		return
	}
	p := a.pal
	typed := strings.TrimSpace(r.query)
	if r.stage == pickHost && (r.stale || r.shown != r.query) {
		r.results = workspace.Fuzzy(r.hosts, typed, 60)
		if typed == "" {
			r.results = r.results[:0]
			for i := range r.hosts {
				r.results = append(r.results, workspace.Match{Index: i})
			}
		}
		r.shown, r.stale, r.sel = r.query, false, 0
	}
	if r.stage == pickFolder {
		a.listPick()
	}
	// The host typed, when it is none of those listed, is the first row.
	extra := r.stage == pickHost && typed != "" && !slices.Contains(r.hosts, typed)
	var rows []string
	count := 0
	switch {
	case r.stage == pickFolder:
		rows = a.pickRows()
		count = len(rows)
	case r.stage == pickHost:
		count = len(r.results)
		if extra {
			count++
		}
	}
	r.sel = min(r.sel, max(count-1, 0))
	choose := func(i int) {
		switch r.stage {
		case pickHost:
			if extra {
				if i == 0 {
					a.chooseHost(typed)
					return
				}
				i--
			}
			if i >= 0 && i < len(r.results) {
				a.chooseHost(r.hosts[r.results[i].Index])
			} else if typed != "" {
				a.chooseHost(typed)
			}
		case pickFolder:
			a.pickFolderRow(i)
		}
	}
	title, placeholder := locales.PickConnectTitle(), locales.PickConnectPlaceholder()
	if r.stage != pickHost && r.conn != nil {
		title, placeholder = locales.PickFolderTitle(r.conn.Host), locales.PickFolderPlaceholder()
	}
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "remote")
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(600).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Radius(10), true).Clip().
				Role(ui.RoleDialog).Label(title)
			box.Children(func() {
				a.pickHeader(c, title, placeholder, rows, count, choose)
				if r.err != "" {
					ui.Row(c).Margin(8, 8, 0).Padding(8, 10).Gap(8).Radius(6).AlignItems(ui.Start).Background(p.danger.Alpha(0.10)).Children(func() {
						ui.Icon(c, iconX).FontSize(13).TextColor(p.danger).Margin(1, 0, 0)
						ui.Text(c, r.err).FontSize(12.5).TextColor(p.text).Shrink(1).Grow(1)
					})
				}
				switch r.stage {
				case pickHost:
					a.pickHostRows(c, extra, typed, choose)
				case pickConnecting:
					a.pickConnecting(c)
				case pickFolder:
					a.pickFolderRows(c, rows, choose)
				}
				r.moved = false
				switch r.stage {
				case pickFolder:
					a.quickFooter(c, false, locales.QuickOpenOrGoIn(), locales.PickTabGoesIn(), false, false)
				case pickConnecting:
					a.connectingFooter(c)
				case pickHost:
					a.quickFooter(c, false, locales.QuickConnectAction(), locales.PickSignsIn(), false, false)
				}
			})
			if box.PressedOutside() {
				a.closeRemotePicker()
			}
			if back.OverlayShortcut(0, ui.KeyEscape) {
				if r.stage == pickConnecting {
					a.cancelConnect()
				} else {
					a.closeRemotePicker()
				}
			}
		})
	})
}

// pickHeader is the top of the box: what it lists, the input, and, for
// the folders, the host they are on.
func (a *app) pickHeader(c *ui.Context, title, placeholder string, rows []string, count int, choose func(int)) {
	r := &a.remotePick
	p := a.pal
	ui.Row(c).Height(46).Padding(0, 14).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
		icon := iconServer
		if r.stage == pickFolder {
			icon = iconFolderOpen
		}
		ui.Icon(c, icon).FontSize(16).TextColor(p.muted)
		if r.stage == pickConnecting {
			ui.Text(c, locales.RemoteConnectingTo(r.conn.Host)).FontSize(14).TextColor(p.text).Grow(1).SingleLine()
			ui.Spinner(c).Size(13, 13)
			return
		}
		in := a.textKeys(ui.TextInputBase(c.Key(fmt.Sprintf("remote-input-%d", r.stage)), &r.query)).Grow(1).FontSize(14).AutoFocus().
			Placeholder(placeholder).Label(title)
		// The input takes a query the box changed in the next frame.
		if r.toEnd {
			n := utf8.RuneCountInString(r.query)
			in.SetTextSelection(n, n)
			r.toEnd = false
		}
		if in.Changed() {
			r.err = ""
			if r.stage == pickFolder && r.conn != nil {
				if q := expandHome(r.query, r.conn.Info().Home); q != r.query {
					r.query, r.toEnd = q, true
					a.invalidate()
				}
			}
		}
		if in.Shortcut(0, ui.KeyDown) {
			r.sel, r.moved = min(r.sel+1, count-1), true
		}
		if in.Shortcut(0, ui.KeyUp) {
			r.sel, r.moved = max(r.sel-1, 0), true
		}
		sel := r.sel
		if r.stage == pickFolder && in.Shortcut(0, ui.KeyTab) {
			// Into the folder chosen, or the only one that matches.
			if sel == 0 {
				var dirs []int
				for i, name := range rows {
					if name != rowOpen && name != rowParent {
						dirs = append(dirs, i)
					}
				}
				if len(dirs) == 1 {
					sel = dirs[0]
				}
			}
			if sel > 0 {
				a.post(func() { a.pickFolderRow(sel) })
			}
		}
		if in.Submitted() {
			a.post(func() { choose(sel) })
		}
		if r.checking || r.listing {
			ui.Spinner(c).Size(12, 12)
		}
		if r.stage == pickFolder && r.conn != nil {
			// The host the folders are on.
			ui.Row(c).Height(22).Padding(0, 8).Gap(5).Radius(11).AlignItems(ui.Center).Shrink(0).
				Background(p.accent.Alpha(0.12)).Children(func() {
				ui.Icon(c, iconServer).FontSize(11).TextColor(p.accent)
				ui.Text(c, r.conn.Host).FontSize(11.5).FontWeight(500).TextColor(p.accent).SingleLine()
			})
		}
	})
}

// pickSection is the title of a group of rows of the box.
func (a *app) pickSection(c *ui.Context, title string) {
	ui.Text(c, strings.ToUpper(title)).FontSize(10.5).FontWeight(600).LetterSpacing(0.7).TextColor(a.pal.faint).
		Padding(10, 10, 4)
}

// pickHostRows are the rows of the hosts of the box: the host typed, then
// those the query matches, with no query the recent ones, then those of
// ssh's config.
func (a *app) pickHostRows(c *ui.Context, extra bool, typed string, choose func(int)) {
	r := &a.remotePick
	p := a.pal
	if !extra && len(r.results) == 0 {
		ui.Column(c).Padding(22, 16).Gap(6).AlignItems(ui.Center).Children(func() {
			msg, tip := locales.PickNoHosts(), locales.PickNoHostsTip()
			if typed != "" {
				msg, tip = locales.PickNoHostsMatch(typed), locales.PickConnectAnyway()
			}
			ui.Icon(c, iconServer).FontSize(22).TextColor(p.faint)
			ui.Text(c, msg).FontSize(13).FontWeight(500).TextColor(p.text)
			ui.Text(c, tip).FontSize(12).TextColor(p.muted).TextAlign(ui.Center)
		})
		return
	}
	ui.Scroll(c.Key("remote-hosts")).MaxHeight(400).Padding(4, 6, 6).Children(func() {
		i := 0
		if extra {
			a.pickRow(c, 0, "remote-typed", iconPlus, []ui.Span{{Text: locales.PickConnectToPrefix(), Color: p.muted}, {Text: typed, Color: p.text, Weight: 600}}, "", "", choose)
			i++
		}
		grouped := typed == ""
		section := ""
		for _, m := range r.results {
			h := r.hosts[m.Index]
			last, recent := r.recent[h]
			if grouped {
				s := locales.PickSshConfig()
				if recent {
					s = locales.PickRecent()
				}
				if s != section {
					section = s
					a.pickSection(c, s)
				}
			}
			icon := iconServer
			if recent {
				icon = iconClock
			}
			trail := ""
			if recent {
				trail = a.homeShort(h, last)
			}
			a.pickRow(c, i, "remote-host:"+h, icon, a.matchSpans(h, 0, m.Positions, p.text), r.resolved[h], trail, choose)
			i++
		}
	})
}

// homeShort is a path of a host with its home folder as ~, when the
// connection knows it.
func (a *app) homeShort(host, p string) string {
	if host == "" {
		return p
	}
	home := remote.For(host).Info().Home
	if home != "" && home != "/" && (p == home || strings.HasPrefix(p, home+"/")) {
		return "~" + p[len(home):]
	}
	return p
}

// pickConnecting shows how the connection of the box goes while it
// connects: who it signs in as, and, once it takes a while, for how long.
func (a *app) pickConnecting(c *ui.Context) {
	r := &a.remotePick
	p := a.pal
	if r.conn == nil {
		return
	}
	host := r.conn.Host
	ui.Column(c).Padding(26, 20, 24).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.PickSigningIn(host)).FontSize(13.5).FontWeight(500).TextColor(p.text)
		detail := r.resolved[host]
		if detail == "" || detail == host {
			detail = locales.PickWithKeys()
		} else {
			detail = locales.PickAs(detail)
		}
		ui.Text(c, detail).FontSize(12).TextColor(p.muted).SingleLine()
		if took := time.Since(r.since); took >= 3*time.Second {
			ui.Text(c, locales.PickWaiting(int(took.Seconds()))).FontSize(12).TextColor(p.faint)
		}
		time.AfterFunc(time.Second-time.Since(r.since)%time.Second, a.invalidate)
	})
}

// pickFolderRows are the rows of the folders of the box, or why it has
// none.
func (a *app) pickFolderRows(c *ui.Context, rows []string, choose func(int)) {
	r := &a.remotePick
	p := a.pal
	dir, prefix := pickDir(r.query)
	target := path.Clean(strings.TrimSpace(r.query))
	host := ""
	if r.conn != nil {
		host = r.conn.Host
	}
	ui.Scroll(c.Key("remote-folders")).MaxHeight(400).Padding(6).Children(func() {
		dirs := 0
		for i, name := range rows {
			switch name {
			case rowOpen:
				label := []ui.Span{{Text: locales.PickOpenPrefix(), Color: p.muted}, {Text: path.Base(target), Color: p.text, Weight: 600}}
				a.pickRow(c, i, "remote-open", iconFolderOpen, label, a.homeShort(host, target), "", choose)
			case rowParent:
				a.pickRow(c, i, "remote-parent", iconParent, []ui.Span{{Text: "..", Color: p.text}}, a.homeShort(host, path.Dir(dir)), "", choose)
			default:
				if dirs == 0 {
					a.pickSection(c, locales.PickFoldersIn(path.Base(dir)))
				}
				dirs++
				spans := []ui.Span{{Text: name, Color: p.text}}
				if n := len(prefix); n > 0 && n <= len(name) {
					spans = []ui.Span{{Text: name[:n], Color: p.accent, Weight: 650}, {Text: name[n:], Color: p.text}}
				}
				a.pickRow(c, i, "remote-dir:"+name, iconFolder, spans, "", "", choose)
			}
		}
		if dirs > 0 {
			return
		}
		msg := ""
		switch {
		case r.dir != dir:
			msg = locales.PickReading(dir)
		case r.listErr != nil:
			msg = locales.PickCouldNotRead(dir, r.listErr.Error())
		case prefix != "":
			msg = locales.PickNoFoldersStart(path.Base(dir), prefix)
		default:
			msg = locales.PickNoFolders(path.Base(dir))
		}
		ui.Text(c, msg).FontSize(12.5).TextColor(p.muted).Padding(10, 10, 6)
	})
}

// pickRow is a row of the box, chosen by click or Enter: an icon, its
// label, a detail after it, and trail at its end.
func (a *app) pickRow(c *ui.Context, i int, key string, icon *ui.SVG, label []ui.Span, detail, trail string, choose func(int)) {
	r := &a.remotePick
	p := a.pal
	text := ""
	for _, s := range label {
		text += s.Text
	}
	row := ui.ButtonBase(c.Key(key)).Height(pickRowH).Padding(0, 10).Gap(10).Radius(6).Justify(ui.Start).AlignItems(ui.Center).Label(text)
	sel := i == r.sel
	if !sel && row.Hovered() {
		row.Background(p.hover)
	}
	if sel && r.moved {
		row.ScrollIntoView()
	}
	if row.Clicked() {
		a.post(func() { choose(i) })
	}
	row.Transition(hoverFade)
	row.Children(func() {
		a.pickMark(c, sel, "remote")
		col := p.muted
		if sel {
			col = p.accent
		}
		ui.Icon(c, icon).FontSize(15).TextColor(col)
		ui.RichText(c, label...).FontSize(13).SingleLine().Shrink(0)
		if detail != "" {
			ui.Text(c, detail).FontSize(12).TextColor(p.faint).SingleLine().Shrink(1)
		}
		if trail != "" {
			ui.Spacer(c)
			ui.Text(c, trail).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(1)
		}
	})
}

// connectingFooter is the footer of the box while it connects: Escape
// goes back to the hosts.
func (a *app) connectingFooter(c *ui.Context) {
	p := a.pal
	ui.Row(c).Height(30).Padding(0, 14).Gap(6).AlignItems(ui.Center).Background(p.hover).
		BorderWidth(1, 0, 0, 0).BorderColor(p.border).Children(func() {
		a.keycaps(c, "esc")
		ui.Text(c, locales.QuickCancel()).FontSize(11.5).TextColor(p.muted).SingleLine()
	})
}
