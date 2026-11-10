package bg

// Refresher tracks a read run in the background again whenever what it
// read may have changed. Invalidate marks it stale; Start begins a run
// for a key, as the work tree read, unless it is fresh or a run for the
// same key is going; Done ends the run and reports whether the result is
// of the key last started, to be kept. A result read while stale is kept
// too: it is newer than the last, and the next Start reads again. The
// zero Refresher is fresh and idle. Its methods run on one goroutine.
type Refresher[K comparable] struct {
	of      K
	running bool
	stale   bool
}

// Invalidate has the next Start begin a run, after the one going, if any.
func (r *Refresher[K]) Invalidate() { r.stale = true }

// Stale reports whether a run is wanted that has not started.
func (r *Refresher[K]) Stale() bool { return r.stale }

// Running reports whether a run is going.
func (r *Refresher[K]) Running() bool { return r.running }

// Start begins a run for k when stale, and no run for k is going: a run
// for another key is left to Done to drop.
func (r *Refresher[K]) Start(k K) bool {
	if !r.stale || r.running && r.of == k {
		return false
	}
	r.of, r.running, r.stale = k, true, false
	return true
}

// Done ends the run for k, and reports whether it was the one going: a
// run that a run for another key replaced reports false.
func (r *Refresher[K]) Done(k K) bool {
	if !r.running || r.of != k {
		return false
	}
	r.running = false
	return true
}
