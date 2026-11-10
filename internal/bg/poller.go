package bg

import "time"

// Poller tracks a question asked again every so often, as of a service,
// one run at a time. Start begins a run for a key, as the branch asked
// about, when none is going and the answer kept is of another key, or
// older than every; Done ends the run, and reports whether it is the one
// going; Keep keeps its answer as of now, and has wake called once it is
// due again. The zero Poller has no answer. Its methods run on one
// goroutine.
type Poller[K comparable] struct {
	of      K
	at      time.Time
	asked   K
	running bool
	wake    *time.Timer
}

// Running reports whether a run is going.
func (p *Poller[K]) Running() bool { return p.running }

// Due reports whether Start would begin a run for k.
func (p *Poller[K]) Due(k K, every time.Duration) bool {
	return !p.running && (p.at.IsZero() || p.of != k || time.Since(p.at) >= every)
}

// Start begins a run for k when one is due.
func (p *Poller[K]) Start(k K, every time.Duration) bool {
	if !p.Due(k, every) {
		return false
	}
	p.asked, p.running = k, true
	return true
}

// Done ends the run for k, and reports whether it was the one going: a
// run of a Poller made anew since, or run again for another key, reports
// false, and its answer is not for keeping.
func (p *Poller[K]) Done(k K) bool {
	if !p.running || p.asked != k {
		return false
	}
	p.running = false
	return true
}

// Keep has the answer be of k, as of now, and calls wake after every,
// when the next is due: wake runs on a goroutine of its own, and is not
// counted as running.
func (p *Poller[K]) Keep(k K, every time.Duration, wake func()) {
	p.of, p.at = k, time.Now()
	if p.wake != nil {
		p.wake.Stop()
	}
	p.wake = time.AfterFunc(every, wake)
}

// Expire has the next Start begin a run, whatever the answer's age.
func (p *Poller[K]) Expire() { p.at = time.Time{} }
