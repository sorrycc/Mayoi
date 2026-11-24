package main

import (
	"os"
	"runtime"
	"testing"
	"time"
)

// TestLogRehearsal is a rehearsal of opening a huge log, run by hand
// with the file to open:
//
//	MAYOI_LOG_REHEARSAL=huge.log go test ./cmd/mayoi -run TestLogRehearsal -v
//
// It tells how long the index of the file's lines takes, how long a
// window of it around a far line takes, and what memory holds after —
// the numbers the log mode's design is about.
func TestLogRehearsal(t *testing.T) {
	path := os.Getenv("MAYOI_LOG_REHEARSAL")
	if path == "" {
		t.Skip("MAYOI_LOG_REHEARSAL is not set")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	t0 := time.Now()
	l, err := openLogDoc(path)
	if err != nil {
		t.Fatal(err)
	}
	indexed := time.Since(t0)
	runtime.ReadMemStats(&m1)
	t.Logf("file %d bytes, %d lines", info.Size(), l.lineCount())
	t.Logf("index: %s, heap grew %d KB", indexed, (m1.HeapAlloc-m0.HeapAlloc)/1024)
	for _, line := range []int{0, l.lineCount() / 2, l.lineCount() - 1} {
		t0 = time.Now()
		w, err := l.materialize(line, 50)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("window at line %d: %s, %d bytes, %d lines", line, time.Since(t0), len(w.text), w.lines)
	}
	runtime.ReadMemStats(&m1)
	t.Logf("heap now: %d KB", m1.HeapAlloc/1024)
}
