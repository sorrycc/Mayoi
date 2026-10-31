package main

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
)

func TestGitPollingUsesFrameClock(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	want := hs.tt.Now().Add(statusEvery)
	hs.tt.Advance(statusEvery)
	hs.settle()
	if g := &hs.a.git; g.read.Before(want) || g.checked.Before(want) {
		t.Fatalf("git used wall time: status=%v metadata=%v want at least %v", g.read, g.checked, want)
	}
}

func TestSettleWaitsForBackgroundWork(t *testing.T) {
	for _, tc := range []struct {
		name       string
		after      time.Duration
		idleFrames int
		work       time.Duration
	}{
		{name: "slow job", work: 11 * time.Second},
		{name: "idle frame", idleFrames: 3, work: time.Second},
		{name: "last timed frame", after: 490 * time.Millisecond, work: time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a := &app{}
				start, last := time.Now(), time.Time{}
				frames := 0
				started, applied := false, false
				done := make(chan struct{})
				defer func() {
					if started {
						<-done
						synctest.Wait()
					}
				}()
				tt := ui.NewTester(func(c *ui.Context) {
					a.setServices(c.Services())
					a.drain()
					if c.Now() != last {
						last, frames = c.Now(), 0
					}
					frames++
					if !started && c.Now().Sub(start) >= tc.after && frames >= tc.idleFrames {
						started = true
						bg.Go(func() {
							defer close(done)
							time.Sleep(tc.work)
							a.post(func() { applied = true })
						})
					}
				}, 100, 100)
				hs := &harness{t: t, a: a, tt: tt}
				hs.settle()
				if !started || !applied || !hs.idle() {
					t.Fatalf("settle returned before the result: started=%v applied=%v idle=%v", started, applied, hs.idle())
				}
			})
		})
	}
}

// Staggered repository reads must settle even when they outlast the poll interval.
func TestSettleWaitsForSlowPolls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := &app{}
		polls := []struct {
			work    time.Duration
			read    time.Time
			running bool
			results int
		}{{work: 6 * time.Second}, {work: 11 * time.Second}}
		var jobs sync.WaitGroup
		defer func() { jobs.Wait(); synctest.Wait() }()
		tt := ui.NewTester(func(c *ui.Context) {
			a.setServices(c.Services())
			a.drain()
			for i := range polls {
				p := &polls[i]
				if p.running || a.gitNow().Sub(p.read) < statusEvery {
					continue
				}
				p.running = true
				jobs.Add(1)
				bg.Go(func() {
					defer jobs.Done()
					time.Sleep(p.work)
					a.post(func() {
						p.read, p.running = a.gitNow(), false
						p.results++
					})
				})
			}
		}, 100, 100)
		hs := &harness{t: t, a: a, tt: tt}
		hs.settle()
		for _, p := range polls {
			if p.running || p.results != 1 {
				t.Fatalf("poll restarted before settling: %+v", p)
			}
		}
	})
}
