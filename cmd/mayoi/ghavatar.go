package main

import (
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
)

// ghAvatarPx is how many pixels across the pictures of people on GitHub
// are kept at, as the overview asks for them: twice the card's, for a
// sharp one on a Retina display.
const ghAvatarPx = 64

// ghAvatarsAtOnce is how many pictures are fetched at once; the rest wait.
const ghAvatarsAtOnce = 4

// ghAvatarsKept is how many pictures are kept, about 16 KB each: past it,
// the one asked for least lately goes, to be fetched again if asked for.
const ghAvatarsKept = 256

// ghAvatarRetry is how long a picture that could not be fetched waits
// before it is asked for again.
const ghAvatarRetry = 5 * time.Minute

// avatarRead is the picture of a link, as it is fetched: nil until it
// came, at when it did, and failed when it could not be.
type avatarRead struct {
	bm      *ui.Bitmap
	at      time.Time
	loading bool
	failed  time.Time
	// used is when it was last asked for, by ghAvatars' tick.
	used uint64
	// waiting are the windows that drew it before it came, which draw
	// again as it does.
	waiting []*app
}

// ghAvatars are the pictures of the people of the GitHub view, by link,
// fetched once for every window, at most ghAvatarsAtOnce at a time.
var ghAvatars = struct {
	sync.Mutex
	m       map[string]*avatarRead
	running int
	queue   []string
	tick    uint64
}{m: map[string]*avatarRead{}}

// fetchGitHubAvatar fetches the picture at url, shrunk, nil if it cannot,
// off the main thread; the tests replace it.
var fetchGitHubAvatar = func(url string) *ui.Bitmap { return fetchAvatar(url, ghAvatarPx) }

// ghAvatarBitmap is the picture at url and when it came, nil while it is
// fetched, which it starts the first time it is asked for.
func (a *app) ghAvatarBitmap(url string) (*ui.Bitmap, time.Time) {
	if url == "" {
		return nil, time.Time{}
	}
	av := &ghAvatars
	av.Lock()
	defer av.Unlock()
	rd := av.m[url]
	av.tick++
	switch {
	case rd == nil:
		rd = &avatarRead{}
		av.m[url] = rd
	case rd.bm != nil:
		rd.used = av.tick
		return rd.bm, rd.at
	case !rd.failed.IsZero() && time.Since(rd.failed) < ghAvatarRetry:
		return nil, time.Time{}
	}
	rd.used = av.tick
	if !slices.Contains(rd.waiting, a) {
		rd.waiting = append(rd.waiting, a)
	}
	if !rd.loading {
		rd.loading, rd.failed = true, time.Time{}
		av.queue = append(av.queue, url)
		startGitHubAvatars()
		// Past the kept, others go: not this one, on its way.
		forgetGitHubAvatars()
	}
	return nil, time.Time{}
}

// forgetGitHubAvatars lets the pictures asked for least lately go, but
// those on their way, until ghAvatarsKept are kept. ghAvatars is locked.
func forgetGitHubAvatars() {
	av := &ghAvatars
	for len(av.m) > ghAvatarsKept {
		oldest, at := "", uint64(0)
		for url, rd := range av.m {
			if !rd.loading && (oldest == "" || rd.used < at) {
				oldest, at = url, rd.used
			}
		}
		if oldest == "" {
			return
		}
		delete(av.m, oldest)
	}
}

// startGitHubAvatars starts fetching the pictures waiting, as many as may
// run at once. ghAvatars is locked.
func startGitHubAvatars() {
	av := &ghAvatars
	for av.running < ghAvatarsAtOnce && len(av.queue) > 0 {
		url := av.queue[0]
		av.queue = av.queue[1:]
		rd := av.m[url]
		av.running++
		bg.Go(func() {
			defer recoverCrash()
			bm := fetchGitHubAvatar(url)
			av.Lock()
			av.running--
			rd.loading = false
			if bm != nil {
				rd.bm, rd.at = bm, time.Now()
			} else {
				rd.failed = time.Now()
			}
			waiting := rd.waiting
			rd.waiting = nil
			startGitHubAvatars()
			av.Unlock()
			for _, w := range waiting {
				w.invalidate()
			}
		})
	}
}

// ghAvatar is the picture of login, size across, round: their initials on
// a circle of their color while it comes, or if it cannot, which the
// picture fades in over as it comes.
func (a *app) ghAvatar(c *ui.Context, login, url string, size float32, now time.Time) ui.Element {
	p := a.pal
	bm, at := a.ghAvatarBitmap(url)
	k := float32(1)
	if since := now.Sub(at); bm != nil && since >= 0 && since < motionShow {
		k = ui.EaseOut(float32(since) / float32(motionShow))
		c.AnimationFrame()
	}
	box := ui.Box(c).Size(size, size).Shrink(0).Radius(size / 2).Label(login)
	if bm == nil || k < 1 {
		col := avatarColor(p, login)
		box.Draw(func(pt *ui.Painter, b ui.Rect) {
			pt.Fill(b, col.Alpha(0.22), b.W/2)
			paintInitials(pt, b, login, size*0.52, col)
		})
	}
	if bm != nil {
		// A hairline ring keeps a picture as light as what is behind it
		// round.
		box.Children(func() {
			img := ui.Image(c, bm).Fit(ui.Cover).Size(size, size).Radius(size / 2)
			ring := ui.Box(c).Absolute().Left(0).Top(0).Size(size, size).Radius(size/2).Border(1, p.text.Alpha(0.1))
			if k < 1 {
				img.Opacity(k)
				ring.Opacity(k)
			}
		})
	}
	return box
}
