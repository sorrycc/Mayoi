package main

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// maxCrashes is how many crash logs the crashes folder keeps.
const maxCrashes = 10

// crashTime names a crash log by the time of the crash.
const crashTime = "2006-01-02-150405"

// crashDir is the data folder whose crashes folder keeps the crash logs,
// "" until keepCrashes is called, when no recovered panic is kept.
var crashDir string

// keepCrashes has the runtime write the panic or fatal error that ends
// the app, with every goroutine's stack, to crash.log in dir as well as
// to stderr, which nothing keeps when the app starts from the Finder or
// the Dock. crash.log starts with a line of the app's version and system,
// which the crash follows. A crash.log a crash left is moved first into
// the crashes folder, named by its time, with the home folder taken out,
// so that each launch starts an empty one, with the end of what the run
// wrote to stderr (keepStderr). The crashes the system reported since the
// last launch join it there.
func keepCrashes(dir string) {
	if dir == "" {
		return
	}
	crashDir = dir
	crashes := filepath.Join(dir, "crashes")
	path := filepath.Join(dir, "crash.log")
	stderr := filepath.Join(dir, crashStderrFile)
	if fi, err := os.Stat(path); err == nil {
		if b, err := os.ReadFile(path); err == nil && crashed(b) && os.MkdirAll(crashes, 0o755) == nil {
			name := filepath.Join(crashes, fi.ModTime().Format(crashTime)+".log")
			if os.WriteFile(name, []byte(redactHome(string(b)+crashedStderr(stderr, b))), 0o644) == nil {
				os.Chtimes(name, fi.ModTime(), fi.ModTime())
			}
		}
	}
	// Of the last run only: this one may not keep its stderr.
	os.Remove(stderr)
	keepSystemCrashes(crashes)
	pruneCrashes(crashes)
	os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	// The runtime writes from where the line ends.
	f.WriteString(crashHeader(time.Now()))
	debug.SetCrashOutput(f, debug.CrashOptions{})
	f.Close() // SetCrashOutput keeps its own copy
}

// crashHeader is the line a crash log starts with: what crashed, on what,
// and since when.
func crashHeader(started time.Time) string {
	return fmt.Sprintf("Mayoi %s %s/%s %s, started %s\n\n", appVersion(), runtime.GOOS, runtime.GOARCH,
		runtime.Version(), started.UTC().Format(time.RFC3339))
}

func appVersion() string { return cmp.Or(mygo.App.Version(), "dev") }

// crashed reports whether a crash.log holds a crash, more than its
// header.
func crashed(b []byte) bool {
	if bytes.HasPrefix(b, []byte("Mayoi ")) {
		_, b, _ = bytes.Cut(b, []byte("\n\n"))
	}
	return len(bytes.TrimSpace(b)) > 0
}

// crashStderrFile is the file in the data folder that keeps stderr
// (keepStderr).
const crashStderrFile = "stderr.log"

// crashStderr heads, in a crash log, what the run wrote to stderr.
const crashStderr = "\n\nstderr:\n"

// maxCrashStderr is how much of the end of stderr a crash log keeps.
const maxCrashStderr = 16 << 10

// crashedStderr is the section of a crash log with the end of the stderr
// of the run that crashed, kept at path: without the crash, which the
// runtime writes to stderr as well as to crash.log. "" when it wrote
// nothing else.
func crashedStderr(path string, crash []byte) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if bytes.HasPrefix(crash, []byte("Mayoi ")) {
		_, crash, _ = bytes.Cut(crash, []byte("\n\n"))
	}
	first, _, _ := bytes.Cut(bytes.TrimLeft(crash, "\x00\n"), []byte("\n"))
	if i := bytes.LastIndex(b, first); len(first) > 0 && i >= 0 {
		b = b[:i]
	}
	if len(b) > maxCrashStderr {
		b = b[len(b)-maxCrashStderr:]
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	b = bytes.TrimRight(b, "\n")
	if len(b) == 0 {
		return ""
	}
	return crashStderr + strings.ToValidUTF8(string(b), "")
}

// pruneCrashes removes all but the newest maxCrashes logs of dir.
func pruneCrashes(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	for len(names) > maxCrashes {
		os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

// homePath matches a home folder in a path, on macOS, Linux and Windows,
// also with JSON's escaped backslashes, as of a remote host's.
var homePath = regexp.MustCompile(`(?i)(?:/Users|/home|\b[a-z]:(?:\\\\|\\|/)Users)(?:\\\\|\\|/)[^/\\\s"':]+`)

// redactHome takes the home folders out of a crash log, as ~, so that a
// report says nothing of whose it is. /Users/Shared is no one's.
func redactHome(log string) string {
	if home, err := os.UserHomeDir(); err == nil && len(home) > 1 {
		log = strings.ReplaceAll(log, home, "~")
		log = strings.ReplaceAll(log, filepath.ToSlash(home), "~")
	}
	return homePath.ReplaceAllStringFunc(log, func(m string) string {
		if strings.HasSuffix(strings.ToLower(m), "shared") {
			return m
		}
		return "~"
	})
}

// recovered keeps the panics recovered from this run, so that one that
// happens again and again is kept once.
var recovered struct {
	sync.Mutex
	seen map[string]bool
}

// recoverCrash, deferred first in a goroutine that only reads, as a git
// status or a search, keeps a panic in it from ending Mayoi: the
// goroutine ends, and the panic's log is kept in the crashes folder, for
// the next launch to offer to send.
func recoverCrash() {
	if r := recover(); r != nil {
		keepRecovered(r, debug.Stack())
	}
}

func keepRecovered(r any, stack []byte) {
	msg := fmt.Sprint(r)
	if err, ok := r.(error); ok {
		msg = err.Error()
	}
	log := "panic: " + msg + " [recovered]\n\n" + panicStack(string(stack))
	fmt.Fprintln(os.Stderr, log)
	recovered.Lock()
	defer recovered.Unlock()
	if crashDir == "" || recovered.seen[msg] {
		return
	}
	if recovered.seen == nil {
		recovered.seen = map[string]bool{}
	}
	recovered.seen[msg] = true
	dir := filepath.Join(crashDir, "crashes")
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	now := time.Now()
	stem := now.Format(crashTime) + "-recovered"
	for i := 1; i <= maxCrashes; i++ {
		name := stem
		if i > 1 {
			name = fmt.Sprintf("%s-%d", stem, i)
		}
		f, err := os.OpenFile(filepath.Join(dir, name+".log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err == nil {
			f.WriteString(redactHome(crashHeader(startTime) + log))
			f.Close()
			pruneCrashes(dir)
		}
		return
	}
}

// startTime is when Mayoi started, for the logs of the panics it
// recovers from.
var startTime = time.Now()

// panicStack is a stack from debug.Stack in a deferred recover, from the
// function that panicked on: without debug.Stack, recoverCrash and the
// panic, which every one of them has.
func panicStack(stack string) string {
	lines := strings.Split(stack, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "panic(") && i+2 <= len(lines) {
			return lines[0] + "\n" + strings.Join(lines[i+2:], "\n")
		}
	}
	return stack
}
