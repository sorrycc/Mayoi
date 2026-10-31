// Package bg runs the app's one-off background work, reads and commands
// whose results are posted back to the frames, and the short pauses
// before it, and counts them, so that the tests can wait until none is
// running.
package bg

import (
	"sync/atomic"
	"time"
)

var running atomic.Int64

// Go runs fn on a goroutine of its own.
func Go(fn func()) {
	running.Add(1)
	go func() {
		defer running.Add(-1)
		fn()
	}()
}

// Timer is a pause AfterFunc started, which counts as running until it
// has called its function or is stopped.
type Timer struct{ t *time.Timer }

// AfterFunc calls fn on a goroutine of its own after d, as time.AfterFunc
// does.
func AfterFunc(d time.Duration, fn func()) *Timer {
	running.Add(1)
	return &Timer{time.AfterFunc(d, func() {
		defer running.Add(-1)
		fn()
	})}
}

// Stop keeps the timer from calling its function, and reports whether it
// did, as time.Timer's Stop does.
func (t *Timer) Stop() bool {
	if t.t.Stop() {
		running.Add(-1)
		return true
	}
	return false
}

// Running reports how many of the functions Go and AfterFunc started have
// not returned, the timers stopped aside.
func Running() int64 { return running.Load() }
