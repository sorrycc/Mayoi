package main

import (
	"fmt"
	"os"
	"testing"
)

// benchDoc opens a Go file of some 25,000 lines, for the cost of a frame
// of the editor: go test -short -run XXX -bench Frame
func benchDoc(b *testing.B) *harness {
	hs := newHarness(b, true)
	src, _ := os.ReadFile("editor.go")
	var big []byte
	for range 60 {
		big = append(big, src...)
	}
	os.WriteFile(hs.path("big.go"), big, 0o644)
	hs.a.openFile(hs.path("big.go"), true)
	hs.settle()
	return hs
}

func BenchmarkIdleFrame(b *testing.B) {
	hs := benchDoc(b)
	hs.a.activeEditor().vscroll.Y = 4000
	hs.settle()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		hs.tt.Frame()
	}
}

func BenchmarkIdleFrameTabs(b *testing.B) {
	for _, tabs := range []int{1, 10, 30} {
		b.Run(fmt.Sprint(tabs), func(b *testing.B) {
			hs := newHarness(b, true)
			src, _ := os.ReadFile("editor.go")
			for i := range tabs {
				name := fmt.Sprintf("bench-%02d.go", i)
				os.WriteFile(hs.path(name), src, 0o644)
				hs.a.openFile(hs.path(name), true)
			}
			hs.settle()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				hs.tt.Frame()
			}
		})
	}
}

func BenchmarkScrollFrame(b *testing.B) {
	hs := benchDoc(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		dy := float32(60)
		if i/200%2 == 1 {
			dy = -60
		}
		hs.tt.Scroll(700, 400, 0, dy)
		hs.tt.Frame()
	}
}

func BenchmarkTypeFrame(b *testing.B) {
	hs := benchDoc(b)
	hs.a.activeEditor().vscroll.Y = 4000
	d := hs.a.activeEditor()
	hs.place(d, 5000, 5000)
	hs.settle()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		hs.tt.Type("a")
		hs.tt.Frame()
	}
}

func BenchmarkWrapScrollFrame(b *testing.B) {
	hs := benchDoc(b)
	hs.a.run("view.wordWrap")
	hs.settle()
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		dy := float32(60)
		if i/200%2 == 1 {
			dy = -60
		}
		hs.tt.Scroll(700, 400, 0, dy)
		hs.tt.Frame()
	}
}

func BenchmarkWrapTypeFrame(b *testing.B) {
	hs := benchDoc(b)
	hs.a.run("view.wordWrap")
	hs.settle()
	d := hs.a.activeEditor()
	d.vscroll.Y = 4000
	hs.place(d, 5000, 5000)
	hs.settle()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		hs.tt.Type("a")
		hs.tt.Frame()
	}
}
