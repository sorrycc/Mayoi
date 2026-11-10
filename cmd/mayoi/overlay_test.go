package main

import (
	"reflect"
	"testing"
	"unsafe"
)

// TestOverlaidKnowsEveryBox: every box of the window with an open flag of
// its own counts as shown over it, so a question asked in the background
// waits for it. A box added later fails here until overlaid names it.
func TestOverlaidKnowsEveryBox(t *testing.T) {
	// The parts with an open flag that are not boxes over the window.
	inline := map[string]bool{
		"find":   true, // the find widget, in its column
		"crumbs": true, // a popover at the breadcrumbs
	}
	a := &app{}
	if a.overlaid() {
		t.Fatal("an empty window is overlaid")
	}
	found := 0
	var visit func(v reflect.Value, name string, depth int)
	visit = func(v reflect.Value, name string, depth int) {
		if v.Kind() != reflect.Struct || inline[name] {
			return
		}
		if f := v.FieldByName("open"); depth > 0 && f.IsValid() && f.Kind() == reflect.Bool {
			open := (*bool)(unsafe.Pointer(f.UnsafeAddr()))
			*open = true
			if !a.overlaid() {
				t.Errorf("%s is open, and overlaid does not know", name)
			}
			*open = false
			found++
			return
		}
		if depth == 2 {
			return
		}
		for i := 0; i < v.NumField(); i++ {
			sub := v.Type().Field(i).Name
			if depth > 0 {
				sub = name + "." + sub
			}
			visit(v.Field(i), sub, depth+1)
		}
	}
	visit(reflect.ValueOf(a).Elem(), "", 0)
	if found < 11 {
		t.Errorf("found %d boxes with an open flag, of 11 or more", found)
	}
}
