package main

import (
	"bytes"
	"errors"
	"io/fs"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/remote"
	"mayoi/internal/vfs"
	"mayoi/locales"
)

// remoteSupported is whether Mayoi reaches folders over SSH here: the
// paths of remote files are POSIX's, as on macOS and Linux.
var remoteSupported = runtime.GOOS != "windows"

// fileStamp is what tells that a file changed: its size and the time of
// its last change, and, for a remote file, both as its host printed
// them, which a save compares with the file's then.
type fileStamp struct {
	size int64
	mod  time.Time
	raw  string
}

func stampOf(info fs.FileInfo) *fileStamp {
	if info == nil {
		return nil
	}
	st := &fileStamp{size: info.Size(), mod: info.ModTime()}
	if r, ok := info.(interface{ Stamp() string }); ok {
		st.raw = r.Stamp()
	}
	return st
}

// attachRemote follows the connection to the host of a remote folder,
// and connects it; a folder of this machine lets go of the one followed.
func (a *app) attachRemote(dir string) {
	host := remote.HostOf(dir)
	if a.conn != nil && a.conn.Host == host {
		return
	}
	a.detachRemote()
	if host == "" {
		return
	}
	c := remote.For(host)
	c.Acquire()
	a.conn = c
	a.connUnsub = c.Subscribe(func() { a.post(a.connChanged) })
	a.connState, a.connErr = c.State()
	c.Connect()
}

// detachRemote stops following the connection, which ends when no other
// project uses it.
func (a *app) detachRemote() {
	c := a.conn
	if c == nil {
		return
	}
	a.connUnsub()
	a.stopRetry()
	a.retries = 0
	a.conn, a.connUnsub, a.connState, a.connErr = nil, nil, remote.Offline, nil
	// Ending it waits on ssh.
	go c.Release()
}

// online reports whether the folder's files can be reached: always for a
// folder of this machine.
func (a *app) online() bool { return a.conn == nil || a.connState == remote.Connected }

// connChanged follows the connection as it connects or goes: back online,
// what could not be read is read now, and what changed meanwhile again.
func (a *app) connChanged() {
	if a.conn == nil {
		return
	}
	was := a.connState
	a.connState, a.connErr = a.conn.State()
	switch {
	case a.connState == remote.Connected:
		a.stopRetry()
		a.retries = 0
		if was != remote.Connected {
			a.reconnected()
		}
	case a.connState == remote.Offline && a.connErr != nil && !remote.Refused(a.connErr):
		// Lost, or the host did not answer: tried again by itself, ever
		// less often, as the network may be back in a moment.
		if was == remote.Connecting && a.retry == nil {
			a.retries++
		}
		a.scheduleRetry()
	default:
		// Disconnected on purpose, or refused: until asked.
		a.stopRetry()
	}
}

// retryDelay is how long to wait before the reconnection after n failed
// in a row: 2s, then twice as long each time, up to half a minute.
func retryDelay(n int) time.Duration {
	return min(2*time.Second<<min(n, 5), 30*time.Second)
}

// scheduleRetry connects again once retryDelay passed, unless it is due.
func (a *app) scheduleRetry() {
	if a.retry != nil || a.conn == nil {
		return
	}
	d := retryDelay(a.retries)
	a.retryAt = time.Now().Add(d)
	a.retry = time.AfterFunc(d, func() {
		a.post(func() {
			if a.retry == nil {
				return
			}
			a.retry = nil
			if a.conn != nil {
				a.conn.Connect()
			}
		})
	})
}

func (a *app) stopRetry() {
	if a.retry != nil {
		a.retry.Stop()
		a.retry = nil
	}
}

// wake connects at once a folder whose host was lost, as the window comes
// back to the front: after sleep the network is often back by then.
func (a *app) wake() {
	if a.conn != nil && a.retry != nil {
		a.stopRetry()
		a.conn.Connect()
	}
}

// reconnected reads again what the folder showed, as the connection comes
// back or first connects.
func (a *app) reconnected() {
	if a.tree != nil {
		if !a.tree.Loaded(a.root) {
			a.tree.Load(a.root)
		}
		for _, dir := range a.window.Expanded {
			if d := filepath.Join(a.root, filepath.FromSlash(dir)); !a.tree.Loaded(d) {
				a.tree.Load(d)
			}
		}
		for dir := range a.files.Open.All() {
			if !a.tree.Loaded(dir) {
				a.tree.Load(dir)
			}
		}
		a.tree.Refresh()
	}
	for _, d := range a.docs {
		if !remote.Is(d.path) {
			continue
		}
		switch {
		case d.loadErr != nil:
			a.loadRemote(d)
		case !d.loading && !d.dirty():
			a.reloadDoc(d)
		}
	}
	for _, v := range a.imageViewers() {
		if remote.Is(v.path) && v.err != "" {
			a.loadImage(v)
		}
	}
	if a.git.repo == nil {
		a.git.looked = false
	}
	for _, ctx := range a.rootRepositoryList() {
		ctx.git.meta, ctx.git.refsMeta = time.Time{}, time.Time{}
	}
	a.repositories.dirty = true
	a.indexStale, a.ignoredStale = true, true
	a.gitChanged()
	a.restartTerminals()
}

// reconnect connects the folder's host again.
func (a *app) reconnect() {
	if a.conn != nil {
		a.stopRetry()
		a.retries = 0
		a.conn.Connect()
	}
}

// disconnect ends the connection to the folder's host, which keeps the
// folder and its tabs, read-only, until it connects again.
func (a *app) disconnect() {
	if c := a.conn; c != nil {
		go c.Disconnect()
	}
}

// markOffline makes the documents of a host that is not connected
// read-only. It runs once a frame.
func (a *app) markOffline() {
	for _, d := range a.docs {
		d.offline = remote.Is(d.path) && !remote.IsConnected(d.path)
	}
}

// loadRemote reads the file of a remote document in the background: the
// document is empty and read-only until it is read, or says why it could
// not be.
func (a *app) loadRemote(d *document) {
	d.loading, d.loadErr = true, nil
	path := d.path
	if !remote.IsConnected(path) {
		d.loading, d.loadErr = false, remote.ErrOffline
		return
	}
	go func() {
		// The stat and the text at one round trip.
		info, data, err := vfs.ReadFileInfo(path, maxOpenBytes)
		a.post(func() { a.remoteLoaded(d, path, info, data, err) })
	}()
}

// remoteLoaded takes the text of a remote document read, or why it could
// not be: a file that cannot be opened closes its tabs, as one of this
// machine never opens; one that could not be reached says so.
func (a *app) remoteLoaded(d *document, path string, info fs.FileInfo, data []byte, err error) {
	if !slices.Contains(a.docs, d) || d.path != path {
		return
	}
	d.loading = false
	if err == nil && isBinary(data) {
		err = errBinary
	} else if err == nil && hasLongLine(data) {
		err = errLongLine
	}
	if err != nil {
		if errors.Is(err, remote.ErrOffline) || !remote.IsConnected(path) {
			d.loadErr = remote.ErrOffline
			return
		}
		var re *remote.Error
		if errors.As(err, &re) && !errors.Is(err, fs.ErrNotExist) {
			// The host said something else went wrong: it may be tried again.
			d.loadErr = err
			return
		}
		switch {
		case errors.Is(err, errBinary):
			a.fail("%s", locales.FailBinary(filepath.Base(path)))
		case errors.Is(err, errLongLine):
			a.fail("%s", locales.FailLongLine(filepath.Base(path)))
		case !errors.Is(err, fs.ErrNotExist) || !d.restored:
			a.fail("%s", locales.FailOpen(filepath.Base(path), err.Error()))
		}
		a.dropDocTabs(d)
		return
	}
	d.loadErr = nil
	crlf := bytes.Contains(data, []byte("\r\n"))
	if crlf {
		data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	}
	d.buf.Set(string(data))
	d.hist = history{}
	d.markSaved()
	d.crlf, d.diskCRLF = crlf, crlf
	d.indent = detectIndent(d.buf.Snapshot(), path)
	d.measureWidest()
	d.stamp = stampOf(info)
	for _, ed := range d.views {
		if ts := ed.restoreTab; ts != nil {
			ed.restoreTab = nil
			a.restoreEditor(ed, *ts)
		}
	}
	a.find.stale = true
}

// dropDocTabs closes the tabs that show a document.
func (a *app) dropDocTabs(d *document) {
	for _, col := range slices.Clone(a.cols) {
		for _, t := range slices.Clone(col.tabs) {
			if t.ed != nil && t.ed.document == d {
				a.dropTab(t)
			}
		}
	}
	a.rememberTabs()
}

// reloadRemote makes a remote document's text the file's, as reloadDoc
// does, unless the file is as the document last read or wrote it: that
// costs a stat, not the file.
func (a *app) reloadRemote(d *document) {
	if d.loading || d.saving || !remote.IsConnected(d.path) {
		return
	}
	if d.loadErr != nil {
		a.loadRemote(d)
		return
	}
	path, old := d.path, d.stamp
	go func() {
		info, err := vfs.Stat(path)
		if err != nil || info.IsDir() {
			return
		}
		if now := stampOf(info); old != nil && *now == *old {
			return
		}
		info, data, err := vfs.ReadFileInfo(path, maxOpenBytes)
		if err != nil || isBinary(data) || hasLongLine(data) {
			return
		}
		a.post(func() {
			if !slices.Contains(a.docs, d) || d.path != path || d.loading || d.saving || d.dirty() {
				return
			}
			d.takeText(data)
			d.stamp = stampOf(info)
		})
	}()
}

// saveRemote writes a remote document in the background, then runs done,
// or failed with why it could not. A save asked for while one runs
// follows it.
func (a *app) saveRemote(d *document, done func(), failed func(error)) {
	if d.saving {
		d.resave, d.resaveDone, d.resaveFailed = true, done, failed
		return
	}
	if !remote.IsConnected(d.path) {
		failed(errors.New(locales.ErrNotConnectedTo(remote.HostOf(d.path))))
		return
	}
	d.saving = true
	snap, data := d.encode()
	crlf, path := d.crlf, d.path
	// The file as it was read, unless it changed on the host since.
	expect := ""
	if d.stamp != nil {
		expect = d.stamp.raw
	}
	go func() {
		var info fs.FileInfo
		c, rp, _ := remote.Lookup(path)
		ri, err := c.WriteFileIf(rp, data, expect)
		if err == nil {
			info = ri
		}
		a.post(func() {
			d.saving = false
			if errors.Is(err, remote.ErrChanged) && d.path == path {
				a.saveConflict(d, done, failed)
			} else if err != nil {
				failed(err)
			} else {
				if d.path == path {
					d.wrote(snap, crlf)
					d.stamp = stampOf(info)
				}
				if a.tree != nil {
					a.tree.Stamp(path, info)
				}
				if done != nil {
					done()
				}
			}
			if d.resave {
				next, nextFailed := d.resaveDone, d.resaveFailed
				d.resave, d.resaveDone, d.resaveFailed = false, nil, nil
				if d.dirty() || next != nil {
					a.saveRemote(d, next, nextFailed)
				}
			}
		})
	}()
}

// askSavePath asks where to save a document: in the native dialog, or,
// in a remote folder, for a path on its host.
func (a *app) askSavePath(name string, done func(path string)) {
	if a.conn == nil {
		a.host.saveAs(name, done)
		return
	}
	host, root, _ := remote.Split(a.root)
	a.prompt = &prompt{title: locales.SaveAsEllipsis(), hint: locales.OnHost(host), value: path.Join(root, name), done: func(p string) string {
		p = strings.TrimSpace(p)
		if !strings.HasPrefix(p, "/") {
			return locales.EnterPathFromRoot()
		}
		done(remote.Path(host, p))
		return ""
	}}
}

// hostPath is how a path is copied or shown as text: a remote one as its
// host has it.
func hostPath(p string) string {
	if _, rp, ok := remote.Split(p); ok {
		return rp
	}
	return p
}

// remoteBanner shows above the editors of a remote folder while its host
// is not connected: connecting, the reconnection due, or why it is not,
// and a button to connect now.
func (a *app) remoteBanner(c *ui.Context) {
	if a.conn == nil || a.connState == remote.Connected {
		return
	}
	p := a.pal
	host := a.conn.Host
	bold := func(t string) ui.Span { return ui.Span{Text: t, Color: p.text, Weight: 600} }
	plain := func(t string) ui.Span { return ui.Span{Text: t, Color: p.text} }
	faint := func(t string) ui.Span { return ui.Span{Text: t, Color: p.muted} }
	tint, icon, iconCol := p.hover, iconServer, p.muted
	// A message of the catalogs, with the host in it in bold.
	about := func(msg string) []ui.Span {
		before, after, ok := strings.Cut(msg, host)
		if !ok {
			return []ui.Span{plain(msg)}
		}
		return []ui.Span{plain(before), bold(host), plain(after)}
	}
	var spans []ui.Span
	button := locales.RemoteReconnect()
	switch {
	case a.connState == remote.Connecting && a.retries > 0:
		spans = append(about(locales.RemoteReconnectingTo(host)), faint(locales.RemoteAttempt(a.retries+1)))
	case a.connState == remote.Connecting:
		spans = about(locales.RemoteConnectingTo(host))
	case remote.Refused(a.connErr):
		tint, icon, iconCol = p.danger.Alpha(0.10), iconLock, p.danger
		spans = append(about(locales.RemoteRefused(host)), faint(a.connErr.Error()))
		button = locales.RemoteTryAgain()
	case a.connErr != nil:
		tint, icon, iconCol = p.danger.Alpha(0.08), iconWifiOff, p.danger
		spans = about(locales.RemoteLostTo(host))
		if !errors.Is(a.connErr, remote.ErrLost) {
			spans = append(spans, faint(a.connErr.Error()))
		}
		button = locales.RemoteReconnectNow()
	default:
		spans = append(about(locales.RemoteDisconnectedFrom(host)), faint(locales.RemoteReadOnly()))
	}
	full := ""
	for _, sp := range spans {
		full += sp.Text
	}
	ui.Row(c.Key("remote-banner")).Tooltip(full).Height(34).Padding(0, 6, 0, 12).Gap(10).AlignItems(ui.Center).Background(tint).
		BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
		if a.connState == remote.Connecting {
			ui.Spinner(c).Size(13, 13)
		} else {
			ui.Icon(c, icon).FontSize(14).TextColor(iconCol)
		}
		ui.RichText(c, spans...).FontSize(12.5).SingleLine().Shrink(1)
		ui.Spacer(c)
		if a.connState == remote.Connecting {
			return
		}
		if a.retry != nil {
			left := max(time.Until(a.retryAt).Round(time.Second), time.Second)
			ui.Text(c, locales.RemoteRetryingIn(int(left.Seconds()))).FontSize(12).TextColor(p.muted).SingleLine().Shrink(0)
			// Again as the count goes down.
			time.AfterFunc(max(time.Until(a.retryAt)%time.Second, 0)+20*time.Millisecond, a.invalidate)
		}
		if a.barButton(c, "remote-banner-connect", button).Clicked() {
			a.reconnect()
		}
	})
}

// remoteTreeWaits shows in the explorer of a remote folder not read yet:
// that its host is connecting, or is not, with a button to connect.
func (a *app) remoteTreeWaits(c *ui.Context) {
	p := a.pal
	host := a.conn.Host
	ui.Column(c).Padding(28, 20, 16, 20).Gap(8).AlignItems(ui.Center).Children(func() {
		popUp(ui.Box(c.Key("badge")).Size(44, 44).Radius(22).Background(p.text.Alpha(0.06)).Justify(ui.Center).AlignItems(ui.Center)).Children(func() {
			if a.connState == remote.Connecting {
				ui.Spinner(c).Size(18, 18)
			} else {
				ui.Icon(c, iconWifiOff).FontSize(20).TextColor(p.muted)
			}
		})
		title, msg := locales.RemoteConnecting(), locales.RemoteFilesShowOnce(host)
		if a.connState == remote.Offline {
			title, msg = locales.RemoteNotConnected(), locales.RemoteFilesOfShow(host)
		}
		ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(p.text).Margin(4, 0, 0, 0)
		ui.Text(c, msg).FontSize(12).TextColor(p.muted).TextAlign(ui.Center).LineHeight(1.45)
		if a.connState == remote.Offline {
			ui.Box(c).Height(4)
			if ui.Button(c.Key("tree-reconnect"), locales.RemoteReconnect()).FillWidth().Clicked() {
				a.reconnect()
			}
		}
	})
}

// barButton is a compact button of a bar, as the banner's.
func (a *app) barButton(c *ui.Context, key, label string) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c.Key(key)).Height(24).Padding(0, 10).Radius(5).AlignItems(ui.Center).Label(label).
		Background(p.surface).Border(1, p.border)
	switch {
	case b.Pressed():
		b.Background(p.surfacePressed)
	case b.Hovered():
		b.Background(p.surfaceHover)
	}
	b.Transition(hoverFade)
	pressPose(b)
	b.Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(p.text).SingleLine()
	})
	return b
}

// remoteStatus is the part of the status bar of a remote folder: its host
// and how it is connected, a menu of the connection.
func (a *app) remoteStatus(c *ui.Context, text func(string) ui.Element) {
	if a.conn == nil {
		return
	}
	p := a.pal
	host := a.conn.Host
	label, tip, tint := "SSH: "+host, locales.RemoteConnectedTo(host), p.accent
	switch {
	case a.connState == remote.Connected:
		if info := a.conn.Info(); info.OS != "" {
			tip += " (" + info.OS + ")"
		}
	case a.connState == remote.Connecting:
		tip = locales.RemoteConnectingTip(host)
	case a.retry != nil:
		tip, tint = locales.RemoteLostReconnecting(host), p.danger
	default:
		tip, tint = locales.RemoteDisconnectedTip(host), p.danger
	}
	b := ui.ButtonBase(c.Key("status:ssh")).Height(statusBarH-6).Padding(0, 7).Gap(5).Radius(4).
		AlignItems(ui.Center).Label(tip).Tooltip(tip)
	b.Background(tint.Alpha(0.16))
	if b.Hovered() {
		b.Background(tint.Alpha(0.26))
	}
	b.Transition(hoverFade)
	b.Children(func() {
		if a.connState == remote.Connecting {
			ui.Spinner(c).Size(11, 11)
		} else {
			icon, col := iconServer, p.statusFg
			if a.connState == remote.Offline {
				icon, col = iconWifiOff, p.danger
			}
			ui.Icon(c, icon).FontSize(12).TextColor(col)
		}
		text(label)
	})
	b.Menu(func(m *ui.Menu) {
		if m.Item(locales.RemoteReconnect()).Disabled(a.connState != remote.Offline).Chosen() {
			a.reconnect()
		}
		if m.Item(locales.RemoteDisconnect()).Disabled(a.connState == remote.Offline).Chosen() {
			a.disconnect()
		}
		m.Separator()
		m.Submenu(locales.RemotePorts(), a.portsMenu)
		m.Separator()
		if m.Item(locales.CmdRemoteOpenFolder()).Chosen() {
			a.post(a.openRemoteFolder)
		}
		if m.Item(locales.CmdRemoteConnect()).Chosen() {
			a.post(a.openRemotePicker)
		}
		m.Separator()
		if m.Item(locales.RemoteCloseFolder()).Chosen() {
			a.post(a.closeFolder)
		}
	})
}

// loadingView shows in place of the editor of a remote file not read yet:
// that it is being read, or why it could not be, with a button to try
// again.
func (a *app) loadingView(c *ui.Context, d *editor) {
	p := a.pal
	ui.Column(c).Fill().Center().Gap(10).Background(p.editor).Padding(24).Children(func() {
		if d.loading {
			ui.Spinner(c).Size(16, 16)
			ui.Text(c, locales.RemoteReading(d.name())).FontSize(13).TextColor(p.muted)
			return
		}
		ui.Icon(c, iconServer).FontSize(20).TextColor(p.muted)
		msg := locales.RemoteCouldNotRead(d.name(), d.loadErr.Error())
		if errors.Is(d.loadErr, remote.ErrOffline) {
			msg = locales.RemoteShowsOnce(d.name(), remote.HostOf(d.path))
		}
		ui.Text(c, msg).FontSize(13).TextColor(p.muted).MaxWidth(420)
		if !errors.Is(d.loadErr, remote.ErrOffline) && ui.Button(c.Key("retry:"+d.path), locales.RemoteTryAgain()).Clicked() {
			a.loadRemote(d.document)
		}
	})
}

// shutdown stops watching the folder and lets go of its connection, as
// the project closes for good.
func (a *app) shutdown() {
	a.docClosed(nil)
	if a.tree != nil {
		a.tree.Close()
	}
	a.detachRemote()
	a.dropFolderPrefs()
	a.closeRemotePicker()
}

// saveConflict asks what to do with a remote document whose file changed
// on the host since it was read: write it over the file, or take the
// file's text, as a step that undo takes back. An autosave asks once,
// until the document is saved by hand.
func (a *app) saveConflict(d *document, done func(), failed func(error)) {
	if d.conflict {
		return
	}
	d.conflict = true
	host := remote.HostOf(d.path)
	a.confirm = &confirm{
		title:   locales.RemoteChangedOn(d.name(), host),
		message: locales.RemoteChangedDetail(host),
		ok:      locales.RemoteOverwrite(), danger: true, okShort: locales.RemoteOverwrite(),
		run: func() {
			d.conflict, d.stamp = false, nil
			a.saveRemote(d, done, failed)
		},
		alternate: locales.RemoteRevertToFile(), alternateShort: locales.RemoteRevert(),
		runAlternate: func() {
			d.conflict = false
			a.revertRemote(d)
		},
	}
}

// revertRemote makes a remote document's text the file's, changes and
// all, as a step of undo.
func (a *app) revertRemote(d *document) {
	path := d.path
	go func() {
		info, data, err := vfs.ReadFileInfo(path, maxOpenBytes)
		a.post(func() {
			if !slices.Contains(a.docs, d) || d.path != path {
				return
			}
			if err != nil {
				a.fail("%s", locales.RemoteCouldNotRead(d.name(), err.Error()))
				return
			}
			if isBinary(data) {
				a.fail("%s", locales.FailBinaryNow(d.name()))
				return
			}
			if hasLongLine(data) {
				a.fail("%s", locales.FailLongLine(d.name()))
				return
			}
			d.takeText(data)
			d.stamp = stampOf(info)
		})
	}()
}
