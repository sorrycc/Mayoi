package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// Mayoi's motion, in one place. Every animation in the UI takes its
// length from one of these, so that what moves alike moves alike;
// TestMotionUsesTokens fails on a length written out elsewhere.
//
// What a person does a hundred times a day does not move: a key that
// opens a dialog shows it at once (keyedOpen), and the choice in a list
// moves with the keys. Motion is for what the pointer did, for where a
// thing came from, and for a state that changed on its own.
const (
	// motionTap is a mark that comes and goes with the pointer, as a
	// tab's cross.
	motionTap = 100 * time.Millisecond
	// motionHover is a color that follows the pointer or the keyboard:
	// a hover, a focus ring, a mark.
	motionHover = 120 * time.Millisecond
	// motionShow is a box or a row that comes in or goes, or a value
	// that glides: long enough to see where it came from, short enough
	// never to wait for.
	motionShow = 150 * time.Millisecond
	// motionLeave is a row that goes, as a session a reading dropped:
	// shorter than motionShow, which it came in over, as going out is.
	motionLeave = 110 * time.Millisecond
	// motionMove is a thing that moves across the screen to its new
	// place, as a scroll to a change or a chip's thumb, and the theme's
	// colors as they fade to another.
	motionMove = 200 * time.Millisecond
	// motionEnter is a page's blocks coming in once, as the welcome page
	// shows: the longest, and only for what shows rarely.
	motionEnter = 240 * time.Millisecond
)

// keyedWithin is how soon after a key ran a binding a dialog that opens
// counts as opened by it.
const keyedWithin = 100 * time.Millisecond

// keyedOpen reports whether what opens now was opened by a key: a
// binding ran in the last few frames. It shows at once then, as a key
// wants to type in it straight away; opened with the pointer, it fades in.
func (a *app) keyedOpen(c *ui.Context) bool {
	return !a.keyedAt.IsZero() && c.Now().Sub(a.keyedAt) <= keyedWithin
}

// showIn is the sequence of what comes in as it shows: from 0 to 1 over
// motionShow, easing out; or at 1 at once, as for a dialog a key opened.
func showIn(now bool) []ui.Keyframe {
	if now {
		return []ui.Keyframe{{Value: 1}, {At: time.Nanosecond, Value: 1}}
	}
	return []ui.Keyframe{{Value: 0}, {At: motionShow, Value: 1, Ease: ui.EaseOut}}
}

// popIn is how a box over an editor comes in, as find's: it fades in,
// from a little above where it settles, and goes back up as it fades out.
// Only its colors follow it after.
var popIn = ui.ElementTransition{Duration: motionShow, Colors: true, Enter: &ui.Motion{Y: -6}, Exit: &ui.Motion{Y: -4}}

// hoverFade is how a button's background follows the pointer: it fades
// in and out rather than blinks.
var hoverFade = ui.ElementTransition{Colors: true, Duration: motionHover}

// revealFade is hoverFade for a small button that shows only while the
// pointer is over its row, as the actions of a group of changes: it fades
// in and out too, rather than pops.
var revealFade = ui.ElementTransition{Colors: true, Duration: motionHover, Enter: &ui.Motion{}, Exit: &ui.Motion{}}

// tabMotion is how a tab moves along its strip: it grows in as it opens,
// folds away as it closes, and the others slide to their places.
var tabMotion = ui.ElementTransition{
	Duration: motionShow,
	Enter:    &ui.Motion{Collapse: true},
	Exit:     &ui.Motion{Collapse: true},
}

// wordIn is how a word that comes into a line takes its place, as the
// state of a session that starts to run before its agent: it opens its
// width as it fades in, and closes it as it fades out. slideAlong moves
// the words after it over in step.
var (
	wordIn     = ui.ElementTransition{Duration: motionShow, Enter: &ui.Motion{Collapse: true}, Exit: &ui.Motion{Collapse: true}}
	slideAlong = ui.ElementTransition{Duration: motionShow, Position: true}
)

// dotPop is how a small mark comes and goes, as the dot of a tab's
// unsaved changes: it grows from half its size and shrinks back.
var dotPop = ui.ElementTransition{
	Animation: &ui.Snappy,
	Enter:     &ui.Motion{Scale: 0.5},
	Exit:      &ui.Motion{Scale: 0.5},
}

// pickMark marks the row chosen in a list, first in the row's children.
// It moves at once with the keys that move the choice.
func (a *app) pickMark(c *ui.Context, on bool) {
	if !on {
		return
	}
	ui.Box(c.Key("pick-mark")).Absolute().Left(0).Right(0).Top(0).Bottom(0).Radius(6).Background(a.pal.active)
}

// riseIn brings a block of a page in as the page shows: it fades in from
// a little below, after delay, so that blocks delayed more follow.
func riseIn(e ui.Element, delay time.Duration) ui.Element {
	k := e.Keyframes("rise", []ui.Keyframe{{Value: 0}, {At: motionEnter, Value: 1, Ease: ui.EaseOut}},
		ui.KeyframeOptions{Delay: delay})
	return e.Opacity(k).Translate(0, 8*(1-k))
}

// fadePage brings a page in as it is chosen, as a section of the
// settings: it fades in, without moving.
func fadePage(e ui.Element) ui.Element {
	return e.Opacity(e.Keyframes("show", showIn(false)))
}

// fadeIn brings a pane in as it shows, as the sidebar or the panel does
// once toggled on: it fades in. Its motion starts the first time it is
// built, so it plays again each time the pane comes back.
func fadeIn(e ui.Element) ui.Element {
	return e.Opacity(e.Keyframes("show", showIn(false)))
}

// dropIn brings a box that floats from what opened it in, as a popover
// or a tip: it fades in, from dy above where it settles (below, for a
// negative dy), as popIn does for a box in a parent that was there.
func dropIn(e ui.Element, dy float32) ui.Element {
	k := e.Keyframes("show", showIn(false))
	return e.Opacity(k).Translate(0, -dy*(1-k))
}

// appearing follows the rows of a list by their keys, so that a row new
// to it comes in rather than pops, as those of a group just opened: the
// rows the list had when last noted, and when each that came since did.
type appearing struct {
	had map[string]bool
	at  map[string]time.Time
}

// note takes the rows of the list at now, keys giving each one's: one it
// did not have comes. A first note, one after none, or a fresh one, as
// for the list showing other rows, takes them as they are.
func (ap *appearing) note(n int, key func(int) string, now time.Time, fresh bool) {
	first := len(ap.had) == 0 || fresh
	if ap.had == nil {
		ap.had, ap.at = map[string]bool{}, map[string]time.Time{}
	}
	for k, at := range ap.at {
		if first || now.Sub(at) >= motionShow {
			delete(ap.at, k)
		}
	}
	if !first {
		for i := range n {
			if k := key(i); !ap.had[k] {
				ap.at[k] = now
			}
		}
	}
	clear(ap.had)
	for i := range n {
		ap.had[key(i)] = true
	}
}

// shown returns how far in the row of key is at now, from 0, as it
// comes, to 1.
func (ap *appearing) shown(key string, now time.Time) float32 {
	at, ok := ap.at[key]
	if !ok {
		return 1
	}
	return ui.EaseOut(min(float32(now.Sub(at))/float32(motionShow), 1))
}

// resume has the row of key, which comes again as it was going, come on
// from k, how far in it shows at now, rather than from nothing.
func (ap *appearing) resume(key string, k float32, now time.Time) {
	if _, ok := ap.at[key]; !ok || k <= 0 {
		return
	}
	if k >= 1 {
		delete(ap.at, key)
		return
	}
	// The inverse of EaseOut, 1-(1-t)³.
	t := 1 - math.Cbrt(float64(1-k))
	ap.at[key] = now.Add(-time.Duration(t * float64(motionShow)))
}

// chevron is the arrow of what folds: it points to the side while
// closed, and turns down as it opens.
func chevron(c *ui.Context, open bool) ui.Element {
	turn := float32(0)
	if open {
		turn = 90
	}
	return ui.Icon(c.Key("chevron"), iconChevronRight).AnimatePose(ui.Pose{Rotate: turn}, ui.Snappy)
}

// pressPose sinks a button a little as it is held down, as a key does.
func pressPose(b ui.Element) {
	pose := ui.Pose{}
	if b.Pressed() {
		pose.Scale = 0.96
	}
	b.AnimatePose(pose, ui.Snappy)
}

// shakeFor is how long a box shakes its head, as a password field of the
// Mac does at a wrong password.
const shakeFor = 360 * time.Millisecond

// shake moves a box side to side for a moment from at, as it finds
// nothing for the Return pressed in it, and settles where it was. A box
// shaken long ago, or never, stays put; with less motion, it does too.
func shake(c *ui.Context, e ui.Element, at time.Time) ui.Element {
	since := c.Now().Sub(at)
	if at.IsZero() || since < 0 || since >= shakeFor || c.Preferences().ReduceMotion {
		return e
	}
	c.AnimationFrame()
	t := float64(since) / float64(shakeFor)
	return e.Translate(float32(7*(1-t)*math.Sin(t*6*math.Pi)), 0)
}

// popUp brings the badge of an empty view in as the view shows: it grows
// from a little smaller to its size, and fades in.
func popUp(e ui.Element) ui.Element {
	k := e.Keyframes("pop", []ui.Keyframe{{Value: 0}, {At: motionEnter, Value: 1, Ease: ui.EaseOut}})
	return e.Opacity(k).Scale(0.92+0.08*k, 0.92+0.08*k)
}

// fieldFocus is how a field's border follows the keyboard: the accent
// fades in as the field takes it, and out as it leaves.
var fieldFocus = ui.ElementTransition{Colors: true, Duration: motionHover}
