package main

import (
	"errors"
	"testing"
	"time"

	"mayoi/internal/remote"
	"mayoi/internal/settings"
	"mayoi/locales"

	"github.com/egoist/mygo/ui"
)

// TestRemoteLinkShows: the banner and the status bar of a remote folder
// say how its host is connected, in each state of the link.
func TestRemoteLinkShows(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	const host = "link.invalid"
	a.conn = remote.For(host)
	t.Cleanup(func() { a.link.stopTimer(); a.conn, a.link = nil, link{} })
	cases := []struct {
		name  string
		set   func()
		shows []string
		hides []string
	}{
		{"connecting", func() { a.link = link{kind: linkConnecting} },
			[]string{locales.RemoteConnectingTo(host), locales.RemoteConnectingTip(host)},
			[]string{locales.RemoteReconnectingTo(host), locales.RemoteReconnect()}},
		{"reconnecting after a loss", func() { a.link = link{kind: linkReconnecting} },
			[]string{locales.RemoteReconnectingTo(host), locales.RemoteAttempt(1), locales.RemoteConnectingTip(host)},
			nil},
		{"reconnecting", func() { a.link = link{kind: linkReconnecting, n: 2} },
			[]string{locales.RemoteReconnectingTo(host), locales.RemoteAttempt(3), locales.RemoteConnectingTip(host)},
			nil},
		{"backoff", func() {
			a.link = link{kind: linkBackoff, why: remote.ErrLost}
			a.scheduleRetry()
			a.link.at = time.Now().Add(time.Hour)
		},
			[]string{locales.RemoteLostTo(host), locales.RemoteReconnectNow(), locales.RemoteLostReconnecting(host), "Retrying in"},
			[]string{remote.ErrLost.Error()}},
		{"backoff with a reason", func() {
			a.link.stopTimer()
			a.link = link{kind: linkBackoff, why: errors.New("no route to host")}
			a.scheduleRetry()
			a.link.at = time.Now().Add(time.Hour)
		},
			[]string{locales.RemoteLostTo(host), "no route to host", "Retrying in"},
			nil},
		{"stopped", func() { a.link.stopTimer(); a.link = link{} },
			[]string{locales.RemoteDisconnectedFrom(host), locales.RemoteReconnect(), locales.RemoteDisconnectedTip(host)},
			[]string{"Retrying in"}},
		{"online", func() { a.link = link{kind: linkOnline} },
			[]string{locales.RemoteConnectedTo(host)},
			[]string{locales.RemoteReconnect(), locales.RemoteDisconnectedFrom(host)}},
	}
	for _, tc := range cases {
		tc.set()
		hs.settle()
		for _, s := range tc.shows {
			if !hs.tt.HasText(s) {
				t.Errorf("%s: %q does not show: %q", tc.name, s, hs.tt.Texts())
			}
		}
		for _, s := range tc.hides {
			if hs.tt.HasText(s) {
				t.Errorf("%s: %q shows", tc.name, s)
			}
		}
	}
}

// TestRemoteRefusedShows: a host that refused is not tried again by
// itself, and the banner says why, with a button to try again.
func TestRemoteRefusedShows(t *testing.T) {
	useFakeSSH(t)
	s := settings.Default()
	w := s.NewWindow()
	w.Folder = remote.Path("refused.invalid", "/srv/app")
	a := newApp(&fakeHost{}, s, w)
	t.Cleanup(a.shutdown)
	hs := &harness{t: t, a: a, h: &fakeHost{}, tt: ui.NewTester(a.view, 1200, 760), root: w.Folder}
	hs.untilLong("the refusal", func() bool { return a.link.why != nil })
	hs.settle()
	for _, s := range []string{locales.RemoteRefused("refused.invalid"), locales.RemoteTryAgain(), locales.RemoteDisconnectedTip("refused.invalid")} {
		if !hs.tt.HasText(s) {
			t.Errorf("%q does not show: %q", s, hs.tt.Texts())
		}
	}
	if a.link.kind != linkStopped || a.link.timer != nil {
		t.Errorf("a refused connection is retried: %+v", a.link)
	}
}

// TestLinkNext: the link follows the connection, counting the
// reconnections that failed in a row.
func TestLinkNext(t *testing.T) {
	lost, noRoute := remote.ErrLost, errors.New("no route to host")
	steps := []struct {
		state remote.State
		err   error
		want  link
	}{
		// The first connection, which the host does not answer.
		{remote.Connecting, nil, link{kind: linkConnecting}},
		{remote.Offline, noRoute, link{kind: linkBackoff, n: 1, why: noRoute}},
		// Seen again while it waits.
		{remote.Offline, noRoute, link{kind: linkBackoff, n: 1, why: noRoute}},
		{remote.Connecting, nil, link{kind: linkReconnecting, n: 1}},
		{remote.Offline, lost, link{kind: linkBackoff, n: 2, why: lost}},
		{remote.Connecting, nil, link{kind: linkReconnecting, n: 2}},
		{remote.Connected, nil, link{kind: linkOnline}},
		// Lost: tried again from the start.
		{remote.Offline, lost, link{kind: linkBackoff, why: lost}},
		{remote.Connecting, nil, link{kind: linkReconnecting}},
		{remote.Connected, nil, link{kind: linkOnline}},
		// Disconnected on purpose.
		{remote.Offline, nil, link{}},
		{remote.Connecting, nil, link{kind: linkConnecting}},
	}
	var l link
	for i, st := range steps {
		l = l.next(st.state, st.err)
		if l != st.want {
			t.Fatalf("step %d, %v %v: %+v, want %+v", i, st.state, st.err, l, st.want)
		}
	}
}
