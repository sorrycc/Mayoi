package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"mayoi/internal/plugin"
)

// The initializationOptions of settings.json lay over a server's own as a
// merge patch: kept where not said, deeply, null taking a key away, an
// array taking the place of one, numbers and text as written.
func TestMergeOptions(t *testing.T) {
	for _, c := range []struct{ name, base, patch, want, err string }{
		{"nested", `{"a":{"b":1,"c":2},"d":"x"}`, `{"a":{"c":3,"e":{"f":true}}}`, `{"a":{"b":1,"c":3,"e":{"f":true}},"d":"x"}`, ""},
		{"null removes", `{"a":{"b":1,"c":2},"d":"x"}`, `{"a":{"b":null},"d":null,"z":null}`, `{"a":{"c":2}}`, ""},
		{"arrays replace", `{"list":[1,2,3]}`, `{"list":[4]}`, `{"list":[4]}`, ""},
		{"falsy kept", `{"on":true}`, `{"on":false,"n":0,"s":"","o":{},"l":[]}`, `{"l":[],"n":0,"o":{},"on":false,"s":""}`, ""},
		{"object over scalar", `{"a":"x"}`, `{"a":{"b":1}}`, `{"a":{"b":1}}`, ""},
		{"no base", ``, `{"a":1}`, `{"a":1}`, ""},
		{"base not object", `[1]`, `{"a":1}`, `{"a":1}`, ""},
		{"exact numbers", `{"keep":12345678901234567890}`, `{"big":9007199254740993,"f":1.10}`, `{"big":9007199254740993,"f":1.10,"keep":12345678901234567890}`, ""},
		{"literal text", `{}`, `{"t":"${settings.path}","h":"<a&b>"}`, `{"h":"<a&b>","t":"${settings.path}"}`, ""},
		{"null leaves none", `{"a":1}`, `null`, ``, ""},
		{"array is not an object", `{"a":1}`, `[1]`, ``, "must be an object or null"},
		{"text is not an object", `{"a":1}`, `"x"`, ``, "must be an object or null"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := mergeOptions(json.RawMessage(c.base), json.RawMessage(c.patch))
			if c.err != "" {
				if err == nil || err.Error() != c.err {
					t.Fatalf("err = %v, want %q", err, c.err)
				}
				return
			}
			if err != nil || string(got) != c.want {
				t.Fatalf("got %s, %v; want %s", got, err, c.want)
			}
		})
	}
}

// Only servers whose initializationOptions change are told of: not one
// whose entry is written otherwise, nor one with other keys only.
func TestChangedInitOptions(t *testing.T) {
	m := func(entries map[string]string) map[string]json.RawMessage {
		out := map[string]json.RawMessage{}
		for k, v := range entries {
			out[k] = json.RawMessage(v)
		}
		return out
	}
	old := m(map[string]string{
		"same.server":    `{"initializationOptions":{"a":1,"b":[1,2]}}`,
		"changed.server": `{"initializationOptions":{"a":1}}`,
		"removed.server": `{"initializationOptions":{"a":1}}`,
		"other.server":   `{"something":1}`,
		"nulled.server":  `{"initializationOptions":{"a":1}}`,
	})
	now := m(map[string]string{
		"same.server":    `{ "initializationOptions" : { "b" : [1, 2], "a" : 1 } }`,
		"changed.server": `{"initializationOptions":{"a":2}}`,
		"other.server":   `{"something":2}`,
		"nulled.server":  `{"initializationOptions":null}`,
		"added.server":   `{"initializationOptions":{}}`,
	})
	got := strings.Join(changedInitOptions(old, now), " ")
	if want := "added.server changed.server nulled.server removed.server"; got != want {
		t.Fatalf("changed = %q, want %q", got, want)
	}
	if got := changedInitOptions(nil, nil); len(got) != 0 {
		t.Fatalf("changed of none = %q", got)
	}
}

// initOptionsOf is the initializationOptions a running server was started
// with, as the fake server says it was greeted.
func initOptionsOf(t *testing.T, s *lspServer) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	raw, err := s.live().Call(ctx, "test/snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Initialize struct {
			InitializationOptions json.RawMessage `json:"initializationOptions"`
		} `json:"initialize"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	return string(snap.Initialize.InitializationOptions)
}

// A server starts with settings.json's initializationOptions over those
// of its plugin.json, its settings substituted first; a change restarts
// that server, in each folder, and no other; taken away, it starts with
// its plugin's again.
func TestLanguageServerInitOptions(t *testing.T) {
	t.Setenv("MAYOI_FAKE_LSP", "1")
	manifest, _ := json.Marshal(map[string]any{"name": "fake", "contributes": map[string]any{
		"settings": map[string]any{"path": map[string]any{"type": "string", "default": "/opt/one"}},
		"languages": []any{
			map[string]any{"id": "one", "extensions": []string{".one"}},
			map[string]any{"id": "two", "extensions": []string{".two"}},
		},
		"languageServers": []any{
			map[string]any{"id": "fake.one", "languages": []string{"one"}, "command": os.Args[0], "args": []string{"-test.run=^$"},
				"initializationOptions": json.RawMessage(`{"path":"${settings.path}","keep":{"a":1,"list":[1,2]},"drop":"x"}`)},
			map[string]any{"id": "fake.two", "languages": []string{"two"}, "command": os.Args[0], "args": []string{"-test.run=^$"}},
		},
	}})
	hs := newHarness(t, false)
	a := hs.a
	a.settings.Prefs.LanguageServers = map[string]json.RawMessage{
		"fake.one": json.RawMessage(`{"initializationOptions":{"keep":{"b":true,"list":[3]},"drop":null,"flag":false,` +
			`"big":9007199254740993,"literal":"${settings.path}"}}`),
		"fake.two":     json.RawMessage(`{"settings":{"x":1}}`),
		"fake.unknown": json.RawMessage(`{"initializationOptions":{"x":1}}`),
	}
	withPlugins(hs, map[string]map[string]string{"fake": {"plugin.json": string(manifest)}})
	t.Cleanup(stopServers)
	m := &mayoi{s: a.settings}
	m.wins = []*desktop{{m: m, a: a, f: &frame{m: m, win: &mygo.Window{}}}}
	for _, name := range []string{"a.one", "a.two"} {
		os.WriteFile(hs.path(name), []byte("x\n"), 0o644)
		a.openFile(hs.path(name), true)
	}
	oneKey, twoKey := serverKey(a.root, "fake.one"), serverKey(a.root, "fake.two")
	running := func(key string) func() bool {
		return func() bool { s := serverOf(key); return s != nil && s.live() != nil }
	}
	hs.until("fake.one", running(oneKey))
	hs.until("fake.two", running(twoKey))
	want := `{"big":9007199254740993,"flag":false,"keep":{"a":1,"b":true,"list":[3]},"literal":"${settings.path}","path":"/opt/one"}`
	if got := initOptionsOf(t, serverOf(oneKey)); got != want {
		t.Fatalf("fake.one started with %s, want %s", got, want)
	}
	if got := initOptionsOf(t, serverOf(twoKey)); got != "null" {
		t.Fatalf("fake.two started with %s", got)
	}

	// A server of the same id in another folder, which takes the change
	// too, as the settings are the person's.
	p, sc := serverByID("fake.one")
	other := newServer(p, *sc, t.TempDir(), 1)
	lspServers.Lock()
	lspServers.m[other.key] = other
	lspServers.Unlock()
	go other.start(a, nil)
	hs.until("the other folder's fake.one", func() bool { return other.live() != nil })

	// Written otherwise, the options restart nothing.
	one, two := serverOf(oneKey), serverOf(twoKey)
	p2 := a.settings.Prefs
	p2.LanguageServers = map[string]json.RawMessage{
		"fake.one": json.RawMessage(`{ "initializationOptions": { "literal": "${settings.path}", "big": 9007199254740993, "flag": false,
			"drop": null, "keep": { "list": [3], "b": true } } }`),
		"fake.two": json.RawMessage(`{"settings":{"x":2}}`),
	}
	m.reload(p2)
	hs.settle()
	if serverOf(oneKey) != one || serverOf(twoKey) != two || serverOf(other.key) != other {
		t.Fatal("options written otherwise restarted a server")
	}

	// Changed, fake.one restarts with them in both folders; fake.two runs on.
	p3 := a.settings.Prefs
	p3.LanguageServers = map[string]json.RawMessage{"fake.one": json.RawMessage(`{"initializationOptions":{"keep":{"a":2}}}`)}
	m.reload(p3)
	hs.until("fake.one restarted", func() bool { s := serverOf(oneKey); return s != one && s.asked && s.live() != nil })
	hs.until("the other fake.one restarted", func() bool { s := serverOf(other.key); return s != other && s.live() != nil })
	if serverOf(twoKey) != two || two.live() == nil {
		t.Fatal("fake.two was restarted")
	}
	select {
	case <-one.client.Done():
	default:
		t.Fatal("the old fake.one still runs")
	}
	want = `{"drop":"x","keep":{"a":2,"list":[1,2]},"path":"/opt/one"}`
	for _, key := range []string{oneKey, other.key} {
		if got := initOptionsOf(t, serverOf(key)); got != want {
			t.Fatalf("%q restarted with %s, want %s", key, got, want)
		}
	}

	// Taken away, it starts with its plugin's own.
	one = serverOf(oneKey)
	p4 := a.settings.Prefs
	p4.LanguageServers = nil
	m.reload(p4)
	hs.until("fake.one reset", func() bool { s := serverOf(oneKey); return s != one && s.live() != nil })
	if got, want := initOptionsOf(t, serverOf(oneKey)), `{"drop":"x","keep":{"a":1,"list":[1,2]},"path":"/opt/one"}`; got != want {
		t.Fatalf("reset fake.one started with %s, want %s", got, want)
	}
}

// A resolver's initializationOptions take settings.json's over them as a
// plugin.json's do; what is not an object is left out, and its plugin's
// log says where, never what.
func TestResolverInitOptions(t *testing.T) {
	t.Setenv("MAYOI_TEST_RESOLVER", "1")
	t.Setenv("MAYOI_TEST_RESOLVER_MODE", "options")
	hs := newHarness(t, false)
	hs.a.settings.Prefs.LanguageServers = map[string]json.RawMessage{
		"res.server": json.RawMessage(`{"initializationOptions":{"resolved":{"b":2},"token":null,"secret":"hunter2-literal"}}`),
		"bad.server": json.RawMessage(`{"initializationOptions":["not","an","object"]}`),
	}
	withPlugins(hs, map[string]map[string]string{"res": {"plugin.json": `{"name":"res"}`}})
	p := plugins.reg.Plugin("res")
	resolve := func(id string) json.RawMessage {
		s := &lspServer{root: hs.root, plugin: p, contrib: plugin.ServerContrib{ID: id, Resolve: &plugin.ServerResolver{Command: os.Args[0]}}}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		launch, err := s.resolve(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return launch.Options
	}
	if got, want := string(resolve("res.server")), `{"keep":true,"resolved":{"a":1,"b":2},"secret":"hunter2-literal"}`; got != want {
		t.Fatalf("resolved %s, want %s", got, want)
	}
	if got, want := string(resolve("bad.server")), `{"resolved":{"a":1},"token":"t","keep":true}`; got != want {
		t.Fatalf("an override not an object gave %s, want the resolver's %s", got, want)
	}
	logged := ""
	for _, l := range p.Logs() {
		logged += l.Message + "\n"
	}
	if !strings.Contains(logged, "languageServers.bad.server.initializationOptions must be an object or null") ||
		!strings.Contains(logged, "languageServers.res.server.initializationOptions") {
		t.Fatalf("log = %q", logged)
	}
	if strings.Contains(logged, "hunter2") || strings.Contains(logged, `"an"`) {
		t.Fatalf("the log has the options: %q", logged)
	}
}
