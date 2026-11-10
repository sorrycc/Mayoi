package bg

import "testing"

func TestRefresherRunsOnceWhenStale(t *testing.T) {
	var r Refresher[int]
	if r.Start(1) {
		t.Fatal("a fresh refresher started")
	}
	r.Invalidate()
	if !r.Start(1) || r.Stale() || !r.Running() {
		t.Fatalf("stale refresher did not start: %+v", r)
	}
	if r.Start(1) {
		t.Fatal("started twice for one key")
	}
	if !r.Done(1) || r.Running() {
		t.Fatalf("run did not end: %+v", r)
	}
	if r.Start(1) {
		t.Fatal("started again while fresh")
	}
}

func TestRefresherKeepsResultReadWhileStaleAndRunsAgain(t *testing.T) {
	var r Refresher[int]
	r.Invalidate()
	r.Start(1)
	r.Invalidate()
	if r.Start(1) {
		t.Fatal("started while the run for the key is going")
	}
	if !r.Done(1) {
		t.Fatal("result read while stale was dropped")
	}
	if !r.Start(1) {
		t.Fatal("did not run again after an Invalidate during the run")
	}
}

func TestRefresherDropsRunOfReplacedKey(t *testing.T) {
	var r Refresher[int]
	r.Invalidate()
	r.Start(1)
	r.Invalidate()
	if !r.Start(2) {
		t.Fatal("run for a new key waited for the old one")
	}
	if r.Done(1) || !r.Running() {
		t.Fatalf("old run ended the new one: %+v", r)
	}
	if !r.Done(2) {
		t.Fatal("new run was dropped")
	}
	if r.Done(2) {
		t.Fatal("ended twice")
	}
}
