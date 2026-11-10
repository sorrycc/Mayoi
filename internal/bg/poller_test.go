package bg

import (
	"testing"
	"time"
)

func TestPollerAsksOnceUntilTheAnswerIsOld(t *testing.T) {
	var p Poller[string]
	wake := func() {}
	if !p.Start("main", time.Hour) || !p.Running() {
		t.Fatal("a poller without an answer did not start")
	}
	if p.Start("main", time.Hour) || p.Start("other", time.Hour) {
		t.Fatal("started a second run")
	}
	if !p.Done("main") || p.Running() {
		t.Fatal("the run did not end")
	}
	p.Keep("main", time.Hour, wake)
	if p.Start("main", time.Hour) {
		t.Fatal("asked again while the answer is new")
	}
	if !p.Due("main", 0) {
		t.Fatal("an answer older than every is not due")
	}
	if !p.Start("other", time.Hour) {
		t.Fatal("did not ask for another key")
	}
	p.Done("other")
	p.Keep("other", time.Hour, wake)
	p.Expire()
	if !p.Start("other", time.Hour) {
		t.Fatal("did not ask again once expired")
	}
}

func TestPollerDropsRunOfAPollerMadeAnew(t *testing.T) {
	var p Poller[string]
	p.Start("before", time.Hour)
	p = Poller[string]{}
	if p.Done("before") {
		t.Fatal("a run from before the reset ended one")
	}
	p.Start("after", time.Hour)
	if p.Done("before") || !p.Running() {
		t.Fatal("a run from before the reset ended the new one")
	}
	if !p.Done("after") {
		t.Fatal("the new run was dropped")
	}
}

func TestPollerWakesWhenDue(t *testing.T) {
	var p Poller[int]
	woke := make(chan struct{})
	p.Keep(1, time.Millisecond, func() { close(woke) })
	select {
	case <-woke:
	case <-time.After(5 * time.Second):
		t.Fatal("not woken")
	}
}
