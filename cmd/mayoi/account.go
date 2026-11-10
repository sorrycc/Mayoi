package main

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"golang.org/x/image/draw"

	"mayoi/internal/cloudsync"
	"mayoi/internal/keymap"
	"mayoi/internal/mon"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// monURL is Mon's, the account behind settings sync, or MAYOI_MON_URL's,
// as a Mon on localhost to try it with.
func monURL() string {
	return cmp.Or(os.Getenv("MAYOI_MON_URL"), "https://mon.sorrycc.dev")
}

// tokenKey is the key of Mon's access token in the system's credential
// store, and the name of the file that keeps it in development.
const tokenKey = "mon-token"

// conflictFile is where the account's keybindings.json goes when it
// changed here too, beside keybindings.json.
const conflictFile = "keybindings.sync-conflict.json"

// pushDelay is how long settings sync waits after a change before saving
// it to the account, so that changes made together go together.
const pushDelay = 2 * time.Second

// pullEvery is how often, at most, settings sync asks the account for
// changes as the app comes to the front.
const pullEvery = 30 * time.Second

// busyShow is how long a sync that no one asked for runs before the
// status bar shows it, so that the quick ones do not flicker there.
const busyShow = 400 * time.Millisecond

// quitPush is how long, at most, quitting waits to save a change made
// just before to the account.
const quitPush = 3 * time.Second

// syncRetryDelay is how long settings sync waits to try again after its
// n-th failure in a row, as the connection comes back.
func syncRetryDelay(n int) time.Duration {
	delays := []time.Duration{10 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute}
	if n < len(delays) {
		return delays[n]
	}
	return 5 * time.Minute
}

// errQuitting is what applying the account's settings fails with as the
// app quits: they apply at the next launch.
var errQuitting = errors.New("mayoi is quitting")

// cloud syncs the settings and the keyboard shortcuts with the person's
// Mon account. Its fields are the main thread's, but for the engine.
type cloud struct {
	m      *mayoi
	client *mon.Client
	engine *cloudsync.Engine
	// cancelSignIn stops a sign-in waiting for its approval.
	cancelSignIn context.CancelFunc
	// asked is set once a first sync asked whose settings to keep, which
	// a sync of its own asks again only as the person syncs.
	asked    bool
	push     *time.Timer
	lastPull time.Time
	// busy counts the syncs running, the first since busyAt, and loud is
	// set while one the person asked for runs, which shows at once.
	busy   int
	busyAt time.Time
	loud   bool
	// failure says why the last sync failed, "" when it did not; retry
	// tries again at retryAt, after failures in a row.
	failure  string
	retry    *time.Timer
	retryAt  time.Time
	failures int
	// conflict is set while keybindings.sync-conflict.json waits to be
	// merged by hand.
	conflict bool
	// avatar is the person's picture, of avatarURL, once fetched.
	avatar    *ui.Bitmap
	avatarURL string
	// frozen is what a push as the app quits reads, as the main thread
	// waits for it; quitting fails an Apply then.
	frozen   atomic.Pointer[frozenLocal]
	quitting atomic.Bool
	// onMain runs fn on the main thread and waits for it: mygo.RunOnMain,
	// but in tests.
	onMain func(fn func())
	// tokenFile keeps the token in development, "" for the credential
	// store.
	tokenFile string
}

// frozenLocal is this machine's settings as the app quits: its snapshot,
// and what stays on it.
type frozenLocal struct {
	snap        cloudsync.Snapshot
	keys        []string
	keybindings bool
}

// syncInfo is settings sync as the status bar and the account's panel
// show it.
type syncInfo struct {
	// name and email are the person's, name "" while signed out.
	name, email string
	// busy is set while a sync runs, long enough to show.
	busy     bool
	syncedAt time.Time
	// failure says why the last sync failed, and retryAt when it is tried
	// again.
	failure string
	retryAt time.Time
	// conflict is set while keybindings.sync-conflict.json waits.
	conflict bool
}

// startCloud loads the account settings sync last had, and syncs with it.
func (m *mayoi) startCloud() {
	if m.s.Path() == "" {
		return
	}
	dir := filepath.Dir(m.s.Path())
	c := &cloud{m: m, client: mon.New(monURL()), onMain: mygo.RunOnMain}
	if mygo.IsDev() {
		// On macOS each development build signs anew, ad hoc, and the
		// Keychain asks again whether it may read what the last one kept.
		c.tokenFile = filepath.Join(dir, tokenKey)
	}
	c.engine = cloudsync.New(c.client, cloudLocal{c}, filepath.Join(dir, "sync.json"))
	m.cloud = c
	c.checkConflict()
	m.s.OnPrefsWritten(func() { mygo.RunOnMain(c.changed) })
	mygo.App.OnDidBecomeActive(func() {
		c.checkConflict()
		if time.Since(c.lastPull) > pullEvery {
			c.sync(cloudsync.Ask, false)
		}
	})
	if c.engine.State().UserID == "" {
		return
	}
	go func() {
		token, err := c.loadToken()
		mygo.RunOnMain(func() {
			if errors.Is(err, mygo.ErrSecretNotFound) {
				// The token is gone: the next sign-in starts anew.
				c.engine.Forget()
				m.refreshMenu()
				return
			}
			if err != nil {
				// The keychain is locked or out of reach: sync waits for
				// the next launch.
				return
			}
			c.client.Token = string(token)
			c.sync(cloudsync.Ask, false)
			c.refreshProfile()
		})
	}()
}

// refreshProfile takes the person's name and picture as the account has
// them now, which they may have changed since signing in.
func (c *cloud) refreshProfile() {
	client := *c.client
	go func() {
		defer recoverCrash()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		u, err := client.Me(ctx)
		if err != nil {
			return
		}
		if c.engine.Refreshed(u) == nil {
			mygo.RunOnMain(func() {
				c.notify()
				c.m.refreshMenu()
			})
		}
	}()
}

// account is the name of the person settings sync syncs for, "" for no
// one.
func (c *cloud) account() string {
	if c == nil {
		return ""
	}
	st := c.engine.State()
	if st.UserID == "" {
		return ""
	}
	return cmp.Or(st.Name, st.Email, st.UserID)
}

// info is settings sync as it is now.
func (c *cloud) info() syncInfo {
	name := c.account()
	if name == "" {
		return syncInfo{}
	}
	st := c.engine.State()
	email := st.Email
	if email == name {
		email = ""
	}
	return syncInfo{
		name: name, email: email,
		busy:     c.busy > 0 && (c.loud || time.Since(c.busyAt) >= busyShow),
		syncedAt: st.SyncedAt,
		failure:  c.failure, retryAt: c.retryAt,
		conflict: c.conflict,
	}
}

// notify repaints the windows, which show how settings sync is.
func (c *cloud) notify() {
	for _, h := range c.m.wins {
		h.f.win.Invalidate()
	}
}

// checkConflict notes whether keybindings.sync-conflict.json waits, as
// the person may have merged and removed it since.
func (c *cloud) checkConflict() {
	_, err := os.Stat(c.conflictPath())
	if was := c.conflict; was != (err == nil) {
		c.conflict = err == nil
		c.notify()
	}
}

func (c *cloud) conflictPath() string {
	return filepath.Join(filepath.Dir(c.m.s.Path()), conflictFile)
}

// changed saves this machine's settings to the account a moment after
// they change: the preferences or the shortcuts.
func (c *cloud) changed() {
	if c == nil || c.account() == "" || c.client.Token == "" {
		return
	}
	c.checkConflict()
	if c.push != nil {
		c.push.Stop()
	}
	c.push = time.AfterFunc(pushDelay, func() {
		mygo.RunOnMain(func() { c.run(false, c.engine.Push) })
	})
}

// sync merges the account's settings with this machine's, the first time
// as choice says. manual tells of the outcome, as Sync Now does.
func (c *cloud) sync(choice cloudsync.Choice, manual bool) {
	if c.account() == "" || c.client.Token == "" {
		return
	}
	c.lastPull = time.Now()
	c.run(manual, func(ctx context.Context) (cloudsync.Result, error) { return c.engine.Sync(ctx, choice) })
}

// run runs a sync off the main thread, showing that it does.
func (c *cloud) run(manual bool, job func(context.Context) (cloudsync.Result, error)) {
	if c.busy == 0 {
		c.busyAt = time.Now()
	}
	c.busy++
	c.loud = c.loud || manual
	if c.retry != nil {
		c.retry.Stop()
		c.retry = nil
	}
	if manual {
		c.notify()
	} else {
		time.AfterFunc(busyShow, func() { mygo.RunOnMain(c.notify) })
	}
	go func() {
		res, err := job(context.Background())
		mygo.RunOnMain(func() {
			c.busy--
			if c.busy == 0 {
				c.loud = false
			}
			c.done(res, err, manual)
			c.notify()
		})
	}()
}

// done tells of a sync's outcome, and asks whose settings to keep when a
// first sync needs to know.
func (c *cloud) done(res cloudsync.Result, err error, manual bool) {
	m := c.m
	if err == nil || errors.Is(err, cloudsync.ErrNeedsChoice) || errors.Is(err, cloudsync.ErrStale) {
		c.failure, c.failures, c.retryAt = "", 0, time.Time{}
	}
	switch {
	case errors.Is(err, errQuitting):
	case errors.Is(err, cloudsync.ErrStale):
		// The settings kept changing as it synced: the push of the last
		// change syncs them.
	case errors.Is(err, cloudsync.ErrNeedsChoice):
		if c.asked && !manual {
			return
		}
		c.asked = true
		m.inFront(func(a *app) {
			a.ask(&confirm{
				title: locales.SyncChooseTitle(), message: locales.SyncChooseMessage(),
				ok: locales.SyncUseAccount(), run: func() { c.sync(cloudsync.UseAccount, true) },
				alternate: locales.SyncUseLocal(), runAlternate: func() { c.sync(cloudsync.UseLocal, true) },
				okShort: locales.SyncUseAccountShort(), alternateShort: locales.SyncUseLocalShort(),
			})
		})
	case errors.Is(err, mon.ErrUnauthorized):
		// Signed out elsewhere, as from the account page.
		c.forget()
		m.toast(locales.SyncSessionEnded())
	case err != nil:
		// Offline, most likely: nothing is lost, and the next sync merges
		// what this one did not.
		c.failure = syncError(err)
		c.retryAt = time.Now().Add(syncRetryDelay(c.failures))
		c.retry = time.AfterFunc(time.Until(c.retryAt), func() {
			mygo.RunOnMain(func() {
				c.retry = nil
				c.sync(cloudsync.Ask, false)
			})
		})
		c.failures++
		if manual {
			m.toast(locales.SyncFailed(c.failure))
		}
	case res.KeybindingsConflict != "":
		c.conflict = true
		m.inFront(func(a *app) {
			a.tell(note{message: locales.SyncKeybindingsConflict(), label: locales.SyncCompare(), action: a.compareKeybindings})
		})
	case manual && !res.Applied && !res.Pushed:
		m.toast(locales.SyncUpToDate())
	case manual:
		m.toast(locales.SyncDone())
	}
}

// syncError says why a sync failed, in words for the person rather than
// the network's.
func syncError(err error) string {
	host := monHost()
	var me *mon.Error
	var ne net.Error
	switch {
	case errors.As(err, &me) && me.Status >= 500:
		return locales.SyncServerTrouble(host)
	case errors.As(err, &ne), errors.Is(err, context.DeadlineExceeded), errors.Is(err, io.ErrUnexpectedEOF):
		return locales.SyncOffline(host)
	}
	return err.Error()
}

// monHost is the host name of Mon, as the person sees it in the browser.
func monHost() string {
	if u, err := url.Parse(monURL()); err == nil && u.Host != "" {
		return u.Host
	}
	return monURL()
}

// syncNow syncs at once, and asks again whose settings to keep if a first
// sync waits for it.
func (c *cloud) syncNow() {
	c.sync(cloudsync.Ask, true)
}

// flush saves a change waiting to go to the account, as the app quits,
// waiting for it a moment at most.
func (c *cloud) flush() {
	if c == nil || c.push == nil || !c.push.Stop() || c.client.Token == "" {
		return
	}
	snap, err := cloudLocal{c}.Read()
	if err != nil {
		return
	}
	keys, keybindings := c.m.s.Prefs.SyncKept()
	c.frozen.Store(&frozenLocal{snap, keys, keybindings})
	c.quitting.Store(true)
	done := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), quitPush)
		defer cancel()
		c.engine.Push(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(quitPush + 250*time.Millisecond):
	}
}

// deviceName is how Mon lists Mayoi signed in on this computer: a Mac's name,
// as in Sharing settings, or the host name. A development build signs in
// apart from the release, and says so.
func deviceName() string {
	name := ""
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("/usr/sbin/scutil", "--get", "ComputerName").Output(); err == nil {
			name = strings.TrimSpace(string(out))
		}
	}
	if name == "" {
		host, _ := os.Hostname()
		name = cmp.Or(strings.TrimSuffix(host, ".local"), "Computer")
	}
	if mygo.IsDev() {
		name += " (dev)"
	}
	return name
}

// signIn signs in to Mon, showing the code to approve in a's window, and
// syncs once it is approved.
func (c *cloud) signIn(a *app) {
	if c.cancelSignIn != nil {
		c.cancelSignIn()
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancelSignIn = cancel
	// The dialog shows at once, as the code comes.
	si := &signin{cancel: cancel}
	a.signin = si
	a.invalidate()
	go func() {
		// Where there is no credential store, the token has nowhere to go.
		if _, err := c.loadToken(); errors.Is(err, errors.ErrUnsupported) {
			mygo.RunOnMain(func() {
				a.dropSignin(si)
				c.m.toast(locales.SyncNoKeyring())
			})
			return
		}
		// A token still kept is signed out first, so Mon doesn't list it on.
		if old, err := c.loadToken(); err == nil && len(old) > 0 {
			stale := *c.client
			stale.Token = string(old)
			lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			stale.Logout(lctx)
			cancel()
		}
		dc, err := c.client.StartDeviceLogin(ctx, cloudsync.App, deviceName())
		if err != nil {
			mygo.RunOnMain(func() {
				a.dropSignin(si)
				c.signInFailed(ctx, err)
			})
			return
		}
		mygo.RunOnMain(func() {
			if ctx.Err() != nil || a.signin != si {
				return
			}
			si.code, si.url = dc.UserCode, dc.VerificationURIComplete
			if dc.ExpiresIn > 0 {
				si.expires = time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
			}
			a.services.OpenURL(dc.VerificationURIComplete)
			a.invalidate()
		})
		token, err := c.client.WaitForDeviceToken(ctx, dc)
		var u *mon.User
		if err == nil {
			err = c.saveToken([]byte(token))
		}
		if err == nil {
			signedIn := *c.client
			signedIn.Token = token
			u, err = signedIn.Me(ctx)
		}
		mygo.RunOnMain(func() {
			a.dropSignin(si)
			if err != nil {
				c.signInFailed(ctx, err)
				return
			}
			c.client.Token = token
			c.asked = false
			if err := c.engine.SignedIn(u); err != nil {
				c.signInFailed(ctx, err)
				return
			}
			c.m.toast(locales.SyncSignedIn(c.account()))
			c.m.refreshMenu()
			c.notify()
			c.sync(cloudsync.Ask, false)
		})
	}()
}

// dropSignin closes the sign-in dialog, if it still shows si.
func (a *app) dropSignin(si *signin) {
	if a.signin == si {
		a.signin = nil
		a.invalidate()
	}
}

// signInFailed says why a sign-in did not happen, unless it was
// cancelled.
func (c *cloud) signInFailed(ctx context.Context, err error) {
	switch {
	case ctx.Err() != nil:
	case errors.Is(err, mon.ErrAccessDenied):
		c.m.toast(locales.SyncDeclined())
	case errors.Is(err, mon.ErrExpired):
		c.m.toast(locales.SyncExpired())
	case errors.Is(err, errors.ErrUnsupported):
		c.m.toast(locales.SyncNoKeyring())
	default:
		c.m.toast(locales.SyncSignInFailed(syncError(err)))
	}
}

// signOut stops syncing, and revokes the token on Mon.
func (c *cloud) signOut() {
	client := *c.client
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		client.Logout(ctx)
	}()
	c.forget()
	c.m.toast(locales.SyncSignedOut())
}

// loadToken returns the token kept at sign-in, or mygo.ErrSecretNotFound.
func (c *cloud) loadToken() ([]byte, error) {
	if c.tokenFile == "" {
		return mygo.Secrets.Get(tokenKey)
	}
	b, err := os.ReadFile(c.tokenFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, mygo.ErrSecretNotFound
	}
	return b, err
}

// saveToken keeps the token for the next launches.
func (c *cloud) saveToken(token []byte) error {
	if c.tokenFile == "" {
		return mygo.Secrets.Set(tokenKey, token)
	}
	return os.WriteFile(c.tokenFile, token, 0o600)
}

// dropToken removes the token kept, if any.
func (c *cloud) dropToken() error {
	if c.tokenFile == "" {
		return mygo.Secrets.Delete(tokenKey)
	}
	if err := os.Remove(c.tokenFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// forget forgets the account and its token, as signed out.
func (c *cloud) forget() {
	if c.push != nil {
		c.push.Stop()
	}
	if c.retry != nil {
		c.retry.Stop()
		c.retry = nil
	}
	c.failure, c.failures, c.retryAt = "", 0, time.Time{}
	c.avatar, c.avatarURL = nil, ""
	c.client.Token = ""
	c.engine.Forget()
	go c.dropToken()
	c.m.refreshMenu()
	c.notify()
}

// picture returns the person's picture, nil until it is fetched, which
// it starts the first time it is asked for.
func (c *cloud) picture() *ui.Bitmap {
	url := c.engine.State().Avatar
	if url == "" || url == c.avatarURL {
		return c.avatar
	}
	c.avatarURL, c.avatar = url, nil
	go func() {
		defer recoverCrash()
		bm := fetchAvatar(url)
		if bm == nil {
			return
		}
		mygo.RunOnMain(func() {
			if c.avatarURL == url {
				c.avatar = bm
				c.notify()
			}
		})
	}()
	return nil
}

// avatarSize is the size the person's picture is kept at, in pixels:
// twice the panel's, for a sharp one on a Retina display.
const avatarSize = 96

// fetchAvatar fetches and shrinks the picture at url, nil if it cannot.
func fetchAvatar(url string) *ui.Bitmap {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, avatarSize, avatarSize))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return ui.NewBitmap(dst)
}

// cloudLocal is this machine's settings and shortcuts, as settings sync
// reads and writes them, from the engine's goroutine.
type cloudLocal struct{ c *cloud }

func (l cloudLocal) keysPath() string { return keymap.Path(l.c.m.s.Path()) }

func (l cloudLocal) Read() (cloudsync.Snapshot, error) {
	if f := l.c.frozen.Load(); f != nil {
		return f.snap, nil
	}
	var s cloudsync.Snapshot
	l.c.onMain(func() {
		m := l.c.m
		// A save of settings.json the watcher has yet to tell of.
		if p, ok := m.s.Edited(); ok {
			m.reload(p)
		}
		s.Settings = m.s.Prefs.Synced()
	})
	data, err := os.ReadFile(l.keysPath())
	if err != nil && !os.IsNotExist(err) {
		return s, err
	}
	s.Keybindings = string(data)
	return s, nil
}

// Apply checks and applies on the main thread, where the person changes
// the settings, so that none of their changes falls between the two.
func (l cloudLocal) Apply(read, s cloudsync.Snapshot, conflict string) error {
	if l.c.quitting.Load() {
		return errQuitting
	}
	var err error
	l.c.onMain(func() {
		m := l.c.m
		p := m.s.Prefs
		_, keepKeys := p.SyncKept()
		path := l.keysPath()
		old, rerr := os.ReadFile(path)
		if rerr != nil && !os.IsNotExist(rerr) {
			err = rerr
			return
		}
		if q, ok := m.s.Edited(); ok {
			// Saved since it was read: the next read has it.
			m.reload(q)
			err = cloudsync.ErrStale
			return
		}
		if !(cloudsync.Snapshot{Settings: p.Synced(), Keybindings: read.Keybindings}).Equal(read) ||
			!keepKeys && string(old) != read.Keybindings {
			err = cloudsync.ErrStale
			return
		}
		if p, err = p.WithSynced(s.Settings); err != nil {
			return
		}
		m.reload(p)
		m.s.Flush()
		if keepKeys {
			return
		}
		if conflict != "" {
			if err = settings.WriteFile(filepath.Join(filepath.Dir(path), conflictFile), []byte(conflict)); err != nil {
				return
			}
		}
		switch {
		case rerr == nil && string(old) == s.Keybindings:
		case s.Keybindings == "":
			if rerr == nil {
				err = os.Remove(path)
			}
		default:
			err = settings.WriteFile(path, []byte(s.Keybindings))
		}
	})
	return err
}

func (l cloudLocal) IsDefault(s cloudsync.Snapshot) bool {
	var p bool
	l.c.onMain(func() {
		q, err := l.c.m.s.Prefs.WithSynced(s.Settings)
		p = err == nil && q.IsDefault()
	})
	return p && (s.Keybindings == "" || s.Keybindings == keymap.Template)
}

func (l cloudLocal) Known(key string) bool { return slices.Contains(settings.PrefKeys, key) }

func (l cloudLocal) Kept() (keys []string, keybindings bool) {
	if f := l.c.frozen.Load(); f != nil {
		return f.keys, f.keybindings
	}
	l.c.onMain(func() { keys, keybindings = l.c.m.s.Prefs.SyncKept() })
	return keys, keybindings
}

// inFront runs fn on the app of the window focused last, before its next
// frame.
func (m *mayoi) inFront(fn func(a *app)) {
	if len(m.wins) == 0 {
		return
	}
	h := m.wins[0]
	h.do(func() { fn(h.a) })
}

// toast tells of msg in the window focused last.
func (m *mayoi) toast(msg string) {
	m.inFront(func(a *app) { a.tell(note{message: msg}) })
}

// The host's side of settings sync, which the app's commands run.

func (h *desktop) account() string { return h.m.cloud.account() }

func (h *desktop) syncInfo() syncInfo {
	if h.m.cloud == nil {
		return syncInfo{}
	}
	return h.m.cloud.info()
}

func (h *desktop) syncPicture() *ui.Bitmap {
	if h.m.cloud == nil {
		return nil
	}
	return h.m.cloud.picture()
}

func (h *desktop) signIn() {
	if h.m.cloud != nil {
		h.m.cloud.signIn(h.a)
	}
}

func (h *desktop) signOut() {
	if h.m.cloud != nil {
		h.m.cloud.signOut()
	}
}

func (h *desktop) syncNow() {
	if h.m.cloud != nil {
		h.m.cloud.syncNow()
	}
}

func (h *desktop) ai() *mon.Client {
	c := h.m.cloud
	if c.account() == "" || c.client.Token == "" {
		return nil
	}
	client := *c.client
	return &client
}

func (h *desktop) aiSignedOut() {
	if c := h.m.cloud; c.account() != "" {
		c.forget()
		h.m.toast(locales.SyncSessionEnded())
	}
}

func (h *desktop) syncConflictResolved() {
	if h.m.cloud != nil {
		h.m.cloud.checkConflict()
	}
}
