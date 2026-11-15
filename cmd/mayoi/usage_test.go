package main

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mayoi/internal/settings"
	"mayoi/internal/usage"
)

func TestReportUsage(t *testing.T) {
	var got []usage.Payload
	fail := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/send" || !strings.HasPrefix(r.UserAgent(), "Mozilla/5.0 (") || !strings.Contains(r.UserAgent(), "Mayoi/") {
			t.Errorf("sent %s with %q", r.URL.Path, r.UserAgent())
		}
		if fail {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		var body struct {
			Type    string
			Payload usage.Payload
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Type != "event" {
			t.Errorf("type %q", body.Type)
		}
		got = append(got, body.Payload)
	}))
	defer ts.Close()
	c := usage.New(ts.URL, "site", "1.0.0")
	path := filepath.Join(t.TempDir(), "usage.json")
	day := time.Date(2026, 11, 15, 9, 0, 0, 0, time.Local)
	names := func() string {
		var s []string
		for _, p := range got {
			s = append(s, cmp.Or(p.Name, p.URL))
		}
		got = nil
		return strings.Join(s, " ")
	}
	report := func(version string, at time.Time) {
		t.Helper()
		if err := reportUsage(context.Background(), c, path, version, "en", at); err != nil && !fail {
			t.Fatal(err)
		}
	}

	// The first launch counts the day, and no update.
	report("1.0.0", day)
	if n := names(); n != "/1.0.0 active" {
		t.Fatalf("first launch sent %q", n)
	}
	// The same day sends nothing more.
	report("1.0.0", day.Add(3*time.Hour))
	if n := names(); n != "" {
		t.Fatalf("same day sent %q", n)
	}
	// An update the same day counts the update alone.
	report("1.1.0", day.Add(4*time.Hour))
	if len(got) != 1 || got[0].Name != "update" || got[0].Data["from"] != "1.0.0" || got[0].Data["to"] != "1.1.0" {
		t.Fatalf("update sent %+v", got)
	}
	got = nil
	// A failed send is sent at the next look.
	fail = true
	report("1.1.0", day.AddDate(0, 0, 1))
	fail = false
	report("1.1.0", day.AddDate(0, 0, 1).Add(time.Hour))
	if len(got) != 2 || got[0].URL != "/1.1.0" || got[1].Name != "active" || got[1].Data["version"] != "1.1.0" || got[1].Website != "site" || got[1].Language != "en" {
		t.Fatalf("next day sent %+v", got)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `"day":"2026-11-16"`) {
		t.Errorf("usage.json holds %s", b)
	}
}

// TestUsageOnWaits reads usageStats as mygo.RunOnMain runs it: on another
// thread, waited for, which a send to the reader would deadlock.
func TestUsageOnWaits(t *testing.T) {
	onMain := func(fn func()) {
		done := make(chan struct{})
		go func() {
			defer close(done)
			fn()
		}()
		<-done
	}
	for _, want := range []bool{true, false} {
		m := &mayoi{s: settings.Default()}
		m.s.UsageStats = want
		got := make(chan bool, 1)
		go func() { got <- m.usageOn(onMain) }()
		select {
		case on := <-got:
			if on != want {
				t.Errorf("usageStats %v read as %v", want, on)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("reading usageStats on the main thread deadlocks")
		}
	}
}

func TestSendsUsage(t *testing.T) {
	t.Setenv("MAYOI_UMAMI_URL", "http://localhost")
	t.Setenv("DO_NOT_TRACK", "")
	if !sendsUsage() {
		t.Error("MAYOI_UMAMI_URL doesn't send")
	}
	t.Setenv("DO_NOT_TRACK", "1")
	if sendsUsage() {
		t.Error("DO_NOT_TRACK sends")
	}
}

func TestUsageUserAgent(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "Macintosh", "windows": "Windows NT", "linux": "Linux"} {
		if ua := usage.UserAgent(goos, "arm64", "1.0.0"); !strings.Contains(ua, want) || !strings.HasSuffix(ua, "Mayoi/1.0.0") {
			t.Errorf("%s: %q", goos, ua)
		}
	}
}
