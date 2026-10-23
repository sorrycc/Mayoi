package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/remote"
	"mayoi/locales"
)

// The ports of a host forward to this machine, so that a server running
// there opens in a browser here: asked for by Forward a Port…, or as a
// link to localhost is clicked in a terminal of the host. The status bar
// shows the ports forwarded, with a menu to open, copy or stop each.

// askForwardPort asks for a port of the folder's host to forward.
func (a *app) askForwardPort() {
	if a.conn == nil {
		return
	}
	host := a.conn.Host
	a.prompt = &prompt{title: locales.PortForwardTitle(), hint: locales.PortOfHost(host), done: func(v string) string {
		port, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), ":")))
		if err != nil || port < 1 || port > 65535 {
			return locales.PortEnter()
		}
		if _, err := a.forwardPort(port); err != nil {
			return err.Error()
		}
		return ""
	}}
}

// forwardPort forwards a port of the folder's host, and says where it is
// here, with a button to open it, unless it was forwarded already.
func (a *app) forwardPort(port int) (*remote.Forward, error) {
	c := a.conn
	if c == nil {
		return nil, errors.New(locales.ErrNotRemoteFolder())
	}
	was := c.LocalPort(port)
	f, err := c.Forward(port)
	if err != nil {
		return nil, err
	}
	if was == 0 {
		msg := locales.PortIsAt(port, c.Host, f.Local)
		a.tell(note{message: msg, label: locales.OpenInBrowser(), action: func() { a.services.OpenURL(f.URL()) }})
	}
	return f, nil
}

// localLink is a link to a port of the host, as a server running there
// prints it, http://localhost:3000/, rewritten to the port of this
// machine that forwards to it, which it forwards first: false for a link
// to elsewhere.
func (a *app) localLink(link string) (string, bool) {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	switch strings.Trim(u.Hostname(), "[]") {
	case "localhost", "127.0.0.1", "0.0.0.0", "::1", "::":
	default:
		return "", false
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return "", false
	}
	f, err := a.forwardPort(port)
	if err != nil {
		a.fail("%s", locales.FailForwardPort(port, err.Error()))
		return "", false
	}
	u.Host = net.JoinHostPort("localhost", strconv.Itoa(f.Local))
	return u.String(), true
}

// terminalLink opens local file paths in the editor. A link to a port
// in a terminal of a host goes to the port forwarded to it here.
func (a *app) terminalLink(s *session, link string) bool {
	if !remote.Is(s.dir) && a.terminalFile(s, link) {
		return true
	}
	if sessionConn(s) == nil || sessionConn(s) != a.conn {
		return false
	}
	to, ok := a.localLink(link)
	if !ok {
		return false
	}
	a.services.OpenURL(to)
	return true
}

// portsStatus is the part of the status bar of the ports of the host
// forwarded: how many, and a menu of them.
func (a *app) portsStatus(c *ui.Context, text func(string) ui.Element) {
	if a.conn == nil {
		return
	}
	fs := a.conn.Forwards()
	if len(fs) == 0 {
		return
	}
	p := a.pal
	label := portsLabel(fs)
	tip := locales.PortsForwarded(len(fs), a.conn.Host)
	if len(fs) == 1 {
		tip = locales.PortIsAt(fs[0].Port, a.conn.Host, fs[0].Local)
	}
	b := ui.ButtonBase(c.Key("status:ports")).Height(statusBarH-6).Padding(0, 6).Gap(5).Radius(4).
		AlignItems(ui.Center).Label(tip).Tooltip(tip)
	if b.Hovered() {
		b.Background(p.hover)
	}
	b.Transition(hoverFade)
	b.Children(func() {
		ui.Icon(c, iconPlug).FontSize(12).TextColor(p.statusFg)
		text(label)
	})
	b.Menu(a.portsMenu)
}

// portsLabel is what the status bar says of the ports forwarded: the
// port, and where it is here when that differs, or how many there are.
func portsLabel(fs []*remote.Forward) string {
	switch {
	case len(fs) > 1:
		return fmt.Sprint(len(fs))
	case len(fs) == 1 && fs[0].Port != fs[0].Local:
		return fmt.Sprintf("%d → %d", fs[0].Port, fs[0].Local)
	case len(fs) == 1:
		return fmt.Sprint(fs[0].Port)
	}
	return ""
}

// portsMenu lists the ports forwarded, each with what to do with it, and
// forwards another.
func (a *app) portsMenu(m *ui.Menu) {
	if a.conn == nil {
		return
	}
	for _, f := range a.conn.Forwards() {
		m.Submenu(fmt.Sprintf("%d → localhost:%d", f.Port, f.Local), func(m *ui.Menu) {
			if m.Item(locales.OpenInBrowser()).Chosen() {
				a.services.OpenURL(f.URL())
			}
			if m.Item(locales.CopyAddress()).Chosen() {
				a.services.WriteClipboard(f.URL())
			}
			m.Separator()
			if m.Item(locales.StopForwarding()).Chosen() {
				f.Close()
			}
		})
	}
	if len(a.conn.Forwards()) > 0 {
		m.Separator()
	}
	if m.Item(locales.ForwardPortEllipsis()).Chosen() {
		a.post(a.askForwardPort)
	}
}
