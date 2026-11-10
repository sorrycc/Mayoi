package main

import (
	"testing"

	"github.com/egoist/mygo"
)

func TestMenuUpdateDeferredAndLatestWins(t *testing.T) {
	previous := mygo.App.Menu()
	t.Cleanup(func() { mygo.App.SetMenu(previous) })
	m := &mayoi{}
	first := mygo.NewMenu([]*mygo.MenuItem{{Label: "First"}})
	second := mygo.NewMenu([]*mygo.MenuItem{{Label: "Second"}})
	applyFirst := m.menuUpdate(first)
	applySecond := m.menuUpdate(second)
	if mygo.App.Menu() != previous {
		t.Fatal("preparing a menu replaced native widgets during drawing")
	}
	if m.menuUpdate(second) != nil {
		t.Fatal("unchanged menu scheduled another replacement")
	}
	applySecond()
	applyFirst() // workers can reach the UI thread out of order
	if mygo.App.Menu() != second {
		t.Fatal("an older deferred menu overwrote the latest menu")
	}
	// Returning to the first menu must not revive its original callback.
	applyFirstAgain := m.menuUpdate(first)
	applyFirst()
	if mygo.App.Menu() != second {
		t.Fatal("an obsolete callback applied after returning to its menu")
	}
	applyFirstAgain()
	if mygo.App.Menu() != first {
		t.Fatal("latest menu was not applied")
	}
	applySecond = m.menuUpdate(second)
	m.life = lifeClosing
	applySecond()
	if mygo.App.Menu() != first {
		t.Fatal("deferred menu applied while quitting")
	}
}
