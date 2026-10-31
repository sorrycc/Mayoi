package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"

	"mayoi/internal/mon"
	"mayoi/internal/settings"
)

func TestKeepCrashes(t *testing.T) {
	defer debug.SetCrashOutput(nil, debug.CrashOptions{})
	defer func(d string) { crashDir = d }(crashDir)
	home, _ := os.UserHomeDir()
	dir := t.TempDir()
	crashes := filepath.Join(dir, "crashes")
	os.MkdirAll(crashes, 0o755)
	for i := range maxCrashes {
		os.WriteFile(filepath.Join(crashes, fmt.Sprintf("2020-01-%02d-000000.log", i+1)), []byte("old"), 0o644)
	}
	path := filepath.Join(dir, "crash.log")
	os.WriteFile(path, []byte(crashHeader(time.Now())+"panic: boom\n\t"+home+"/x.go:1"), 0o644)
	when := time.Date(2026, 10, 14, 9, 30, 5, 0, time.Local)
	os.Chtimes(path, when, when)

	keepCrashes(dir)

	b, err := os.ReadFile(filepath.Join(crashes, "2026-10-14-093005.log"))
	if !strings.Contains(string(b), "panic: boom\n\t~/x.go:1") || strings.Contains(string(b), home) {
		t.Fatalf("moved crash = %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(crashes, "2020-01-01-000000.log")); !os.IsNotExist(err) {
		t.Errorf("oldest crash kept: %v", err)
	}
	if logs := listCrashes(crashes); len(logs) != maxCrashes {
		t.Errorf("%d crashes kept, want %d", len(logs), maxCrashes)
	}
	if b, err := os.ReadFile(path); err != nil || !strings.HasPrefix(string(b), "Mayoi ") || crashed(b) {
		t.Errorf("crash.log after launch: %q, %v", b, err)
	}

	// crash.log with only its header, from a launch that did not crash,
	// stays put.
	keepCrashes(dir)
	if logs := listCrashes(crashes); len(logs) != maxCrashes || logs[len(logs)-1].stem != "2026-10-14-093005" {
		t.Errorf("header-only crash.log moved: %+v", logs)
	}
}

func TestRecoverCrash(t *testing.T) {
	defer func(d string) { crashDir = d }(crashDir)
	crashDir = t.TempDir()
	panicked := func() {
		var wg sync.WaitGroup
		wg.Go(func() {
			defer recoverCrash()
			var m map[string]int
			//lint:ignore SA5000 the panic is what the test recovers
			m["x"] = 1
		})
		wg.Wait()
	}
	panicked()
	panicked() // the same panic again is kept once
	logs := listCrashes(filepath.Join(crashDir, "crashes"))
	if len(logs) != 1 || logs[0].kind != crashRecovered || logs[0].state != crashPending {
		t.Fatalf("logs = %+v", logs)
	}
	b, _ := os.ReadFile(logs[0].path)
	log := string(b)
	_, body, _ := strings.Cut(log, "\n\n")
	lines := strings.Split(body, "\n")
	if lines[0] != "panic: assignment to entry in nil map [recovered]" || !strings.HasPrefix(lines[2], "goroutine ") ||
		!strings.Contains(lines[3], ".TestRecoverCrash.func") {
		t.Errorf("log:\n%s", log)
	}
}

func TestCrashReport(t *testing.T) {
	at := time.Date(2026, 10, 14, 9, 30, 5, 0, time.Local)
	c := crashReport(crashLog{kind: crashFatal, at: at}, "Mayoi 0.11.4 linux/amd64 go1.27.0, started 2026-10-14T09:00:00Z\n\npanic: boom")
	if c.App != "mayoi" || c.Version != "0.11.4" || c.OS != "linux" || c.Arch != "amd64" || c.Runtime != "go1.27.0" || c.OccurredAt != at.Unix() {
		t.Errorf("fatal = %+v", c)
	}
	c = crashReport(crashLog{kind: crashSystem}, `{"app_name":"Mayoi","app_version":"0.11.3"}`+"\n{}")
	if c.Version != "0.11.3" || c.OS != runtime.GOOS {
		t.Errorf("ips = %+v", c)
	}
	// A log written before crash logs had the line is of an older version
	// than this one, not known which.
	c = crashReport(crashLog{kind: crashFatal}, "panic: boom\n\ngoroutine 1 [running]:")
	if c.Version != crashUnknownVersion || c.Runtime != "" || c.OS != runtime.GOOS {
		t.Errorf("headerless = %+v", c)
	}
	if c := crashReport(crashLog{kind: crashFatal}, strings.Repeat("é", maxCrashReport)); len(c.Log) > maxCrashReport {
		t.Errorf("log of %d bytes", len(c.Log))
	}
	// Goroutines too many to send make room for stderr.
	stderr := crashStderr + "*** Terminating app due to uncaught exception"
	c = crashReport(crashLog{kind: crashFatal}, "SIGABRT: abort\n"+strings.Repeat("goroutine\n", maxCrashReport/10)+stderr)
	if len(c.Log) > maxCrashReport || !strings.HasPrefix(c.Log, "SIGABRT: abort\n") || !strings.HasSuffix(c.Log, stderr) {
		t.Errorf("long log with stderr: %d bytes, %q…%q", len(c.Log), c.Log[:20], c.Log[len(c.Log)-60:])
	}
}

// TestCrashStderr adds the end of what a crashed run wrote to stderr to
// its log, without the crash, which the runtime wrote there too.
func TestCrashStderr(t *testing.T) {
	defer debug.SetCrashOutput(nil, debug.CrashOptions{})
	home, _ := os.UserHomeDir()
	dir := t.TempDir()
	crash := "SIGABRT: abort\nPC=0x183e9d5b0 m=0 sigcode=0\n"
	os.WriteFile(filepath.Join(dir, "crash.log"), []byte(crashHeader(time.Now())+crash), 0o644)
	why := "*** Terminating app due to uncaught exception 'NSRangeException', reason: " + home + "/x"
	os.WriteFile(filepath.Join(dir, crashStderrFile), []byte(strings.Repeat("noise\n", maxCrashStderr)+why+"\n"+crash), 0o644)

	keepCrashes(dir)

	logs := listCrashes(filepath.Join(dir, "crashes"))
	if len(logs) != 1 {
		t.Fatalf("%d crashes kept", len(logs))
	}
	b, _ := os.ReadFile(logs[0].path)
	log := string(b)
	i := strings.Index(log, crashStderr)
	if i < 0 || !strings.HasSuffix(log, "reason: ~/x") || strings.Count(log, "SIGABRT") != 1 {
		t.Fatalf("crash log = …%q", log[max(0, len(log)-200):])
	}
	if n := len(log) - i - len(crashStderr); n > maxCrashStderr {
		t.Errorf("%d bytes of stderr kept", n)
	}
	if _, err := os.Stat(filepath.Join(dir, crashStderrFile)); !os.IsNotExist(err) {
		t.Errorf("stderr.log of the last run kept: %v", err)
	}

	// A run that wrote nothing else to stderr adds no section.
	os.WriteFile(filepath.Join(dir, "crash.log"), []byte(crashHeader(time.Now())+crash), 0o644)
	os.WriteFile(filepath.Join(dir, crashStderrFile), []byte(crash), 0o644)
	if s := crashedStderr(filepath.Join(dir, crashStderrFile), []byte(crashHeader(time.Now())+crash)); s != "" {
		t.Errorf("stderr of only the crash = %q", s)
	}
}

func TestSendCrashes(t *testing.T) {
	var mu sync.Mutex
	var got []mon.Crash
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c mon.Crash
		json.NewDecoder(r.Body).Decode(&c)
		if r.Header.Get("Authorization") != "" || r.URL.Path != "/api/v1/crashes" {
			t.Errorf("request: %s %v", r.URL.Path, r.Header)
		}
		if c.Kind == crashSystem {
			http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
			return
		}
		mu.Lock()
		got = append(got, c)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	t.Setenv("MAYOI_MON_URL", ts.URL)
	defer func(d string) { crashDir = d }(crashDir)
	crashDir = t.TempDir()
	crashes := filepath.Join(crashDir, "crashes")
	os.MkdirAll(crashes, 0o755)
	for _, name := range []string{"2026-10-14-093005.log", "2026-10-14-093006-system.log", "2026-10-14-093007-recovered.sent.log"} {
		os.WriteFile(filepath.Join(crashes, name), []byte("panic: boom"), 0o644)
	}

	// Asked never: the new ones aren't sent.
	m := &mayoi{s: &settings.Settings{}}
	m.s.CrashReports = "never"
	m.offerCrashes(nil)
	states := func() string {
		var s []string
		for _, c := range listCrashes(crashes) {
			s = append(s, c.kind+"."+c.state)
		}
		return strings.Join(s, " ")
	}
	if s := states(); s != "fatal.unsent ips.unsent recovered.sent" {
		t.Fatalf("after never: %s", s)
	}

	// Always: they're sent, or marked unsent when Mon won't take one.
	for _, c := range listCrashes(crashes) {
		if c.state == crashUnsent {
			c.mark(crashPending)
		}
	}
	m.s.CrashReports = "always"
	m.offerCrashes(nil)
	for deadline := time.Now().Add(5 * time.Second); states() != "fatal.sent ips.unsent recovered.sent"; {
		if time.Now().After(deadline) {
			t.Fatalf("after always: %s", states())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(got) != 1 || got[0].Log != "panic: boom" || got[0].Kind != crashFatal {
		t.Errorf("sent %+v", got)
	}

	// Mon can't be reached: it stays marked to send.
	ts.Close()
	os.WriteFile(filepath.Join(crashes, "2026-10-14-093008.send.log"), []byte("panic: again"), 0o644)
	sendCrashes(listCrashes(crashes)[3:])
	if s := states(); !strings.HasSuffix(s, "fatal.send") {
		t.Errorf("offline: %s", s)
	}
}

func TestRedactHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	for in, want := range map[string]string{
		home + "/Projects/x.go":       "~/Projects/x.go",
		"/Users/ada/Projects/x.go:1":  "~/Projects/x.go:1",
		"/home/ada/x":                 "~/x",
		`C:\Users\ada\AppData`:        `~\AppData`,
		`"path":"C:\\Users\\ada\\x"`:  `"path":"~\\x"`,
		"/Users/Shared/x":             "/Users/Shared/x",
		"/usr/local/go/src/runtime/x": "/usr/local/go/src/runtime/x",
	} {
		if got := redactHome(in); got != want {
			t.Errorf("redactHome(%q) = %q, want %q", in, got, want)
		}
	}
}
