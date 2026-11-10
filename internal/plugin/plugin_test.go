package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeHost struct {
	mu       sync.Mutex
	calls    []string
	settings map[string]json.RawMessage
	logs     []LogLine
}

func (h *fakeHost) Call(p *Plugin, method string, args json.RawMessage) (any, error) {
	h.mu.Lock()
	h.calls = append(h.calls, method+" "+string(args))
	h.mu.Unlock()
	switch method {
	case "editor.active":
		return map[string]any{"id": 1, "selections": []any{}, "document": map[string]any{"id": 7, "path": "/a.txt", "version": 3}}, nil
	case "document.text":
		return "hello world", nil
	}
	return nil, nil
}
func (h *fakeHost) Settings(name string) json.RawMessage { return h.settings[name] }
func (h *fakeHost) MayoiSetting(key string) any          { return nil }
func (h *fakeHost) Folder() string                       { return "/work/folder" }
func (h *fakeHost) Changed()                             {}
func (h *fakeHost) Logged(p *Plugin, line LogLine) {
	h.mu.Lock()
	h.logs = append(h.logs, line)
	h.mu.Unlock()
}

func (h *fakeHost) called(prefix string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.calls {
		if strings.HasPrefix(c, prefix) {
			return c
		}
	}
	return ""
}

func writePlugin(t *testing.T, root, name string, files map[string]string) {
	t.Helper()
	for f, body := range files {
		p := filepath.Join(root, name, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func newRegistry(t *testing.T) (*Registry, *fakeHost, Dirs) {
	t.Helper()
	root := t.TempDir()
	dirs := Dirs{Plugins: filepath.Join(root, "plugins"), Scripts: filepath.Join(root, "scripts"), Storage: filepath.Join(root, "storage")}
	os.MkdirAll(dirs.Plugins, 0o755)
	os.MkdirAll(dirs.Scripts, 0o755)
	h := &fakeHost{settings: map[string]json.RawMessage{}}
	r := New(dirs, h)
	r.Version = "0.20.0"
	t.Cleanup(r.Close)
	return r, h, dirs
}

func TestCommandRoundTrip(t *testing.T) {
	r, h, dirs := newRegistry(t)
	writePlugin(t, dirs.Plugins, "word-count", map[string]string{
		"plugin.json": `{
			// comments are fine
			"name": "word-count", "main": "main.ts",
			"contributes": {
				"commands": [{"id": "word-count.count", "title": "Count Words", "title:zh-Hans": "统计字数"}],
				"settings": {"factor": {"type": "number", "default": 2}},
			},
		}`,
		"main.ts": `import { commands, editor, settings, storage } from "mayoi";
import { words } from "./lib/count";
commands.register("word-count.count", async (arg?: string) => {
  const ed = await editor.active();
  const text: string = await ed!.document.text();
  storage.set("last", text.length);
  return { words: words(text) * settings.get("factor"), arg, path: ed!.document.path, stored: storage.get("last") };
});`,
		"lib/count.ts": `export function words(s: string): number { return s.split(/\s+/).filter(Boolean).length; }`,
	})
	r.Load()
	cmds := r.Commands("zh-Hans")
	if len(cmds) != 1 || cmds[0].Title != "统计字数" {
		t.Fatalf("commands = %+v", cmds)
	}
	res, err := r.Run("word-count.count", "x")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Words  int
		Arg    string
		Path   string
		Stored int
	}
	json.Unmarshal(res, &out)
	if out.Words != 4 || out.Arg != "x" || out.Path != "/a.txt" || out.Stored != 11 {
		t.Fatalf("result = %s", res)
	}
	if h.called("document.text") == "" {
		t.Fatal("document.text was not called")
	}
	// The store is on disk.
	data, _ := os.ReadFile(filepath.Join(dirs.Storage, "word-count", "global.json"))
	if !strings.Contains(string(data), `"last": 11`) {
		t.Fatalf("store = %s", data)
	}
	// Settings override the default.
	h.settings["word-count"] = json.RawMessage(`{"factor": 10}`)
	res, _ = r.Run("word-count.count", nil)
	json.Unmarshal(res, &out)
	if out.Words != 20 {
		t.Fatalf("with settings: %s", res)
	}
	// Disabled, it does not run.
	h.settings["word-count"] = json.RawMessage(`{"enabled": false}`)
	if _, err := r.Run("word-count.count", nil); err == nil {
		t.Fatal("a disabled plugin ran")
	}
}

// A plugin whose manifest says "enabled": false starts off, and its
// settings turn it on.
func TestManifestStartsOff(t *testing.T) {
	r, h, dirs := newRegistry(t)
	writePlugin(t, dirs.Plugins, "quiet", map[string]string{
		"plugin.json": `{"name": "quiet", "main": "main.js", "enabled": false, "contributes": {"commands": [{"id": "quiet.x", "title": "X"}]}}`,
		"main.js":     `import { commands } from "mayoi"; commands.register("quiet.x", () => 1);`,
	})
	r.Load()
	if _, err := r.Run("quiet.x", nil); err == nil {
		t.Fatal("a plugin off by default ran")
	}
	h.settings["quiet"] = json.RawMessage(`{"enabled": true}`)
	if _, err := r.Run("quiet.x", nil); err != nil {
		t.Fatalf("turned on: %v", err)
	}
	delete(h.settings, "quiet")
	if !r.Plugin("quiet").Stats().Disabled {
		t.Fatal("without its setting, the plugin is on")
	}
}

func TestImportsStayInFolder(t *testing.T) {
	r, _, dirs := newRegistry(t)
	writePlugin(t, dirs.Plugins, "bad", map[string]string{
		"plugin.json": `{"name": "bad", "main": "main.js", "contributes": {"commands": [{"id": "bad.x", "title": "X"}]}}`,
		"main.js":     `import fs from "node:fs";`,
	})
	writePlugin(t, dirs.Plugins, "escape", map[string]string{
		"plugin.json": `{"name": "escape", "main": "main.js", "contributes": {"commands": [{"id": "escape.x", "title": "X"}]}}`,
		"main.js":     `import x from "../bad/main.js";`,
	})
	writePlugin(t, dirs.Plugins, "evil", map[string]string{
		"plugin.json": `{"name": "evil", "main": "main.js", "contributes": {"commands": [{"id": "evil.x", "title": "X"}]}}`,
		"main.js":     `import { commands } from "mayoi"; commands.register("evil.x", () => eval("1+1"));`,
	})
	r.Load()
	if _, err := r.Run("bad.x", nil); err == nil || !strings.Contains(err.Error(), "node:fs") {
		t.Fatalf("node: import: %v", err)
	}
	if _, err := r.Run("escape.x", nil); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("escaping import: %v", err)
	}
	if _, err := r.Run("evil.x", nil); err == nil {
		t.Fatal("eval ran")
	}
}

func TestBusyPluginIsStopped(t *testing.T) {
	old := BusyLimit
	BusyLimit = 300 * time.Millisecond
	defer func() { BusyLimit = old }()
	r, _, dirs := newRegistry(t)
	writePlugin(t, dirs.Plugins, "spin", map[string]string{
		"plugin.json": `{"name": "spin", "main": "main.js", "contributes": {"commands": [{"id": "spin.go", "title": "Go"}, {"id": "spin.ok", "title": "OK"}]}}`,
		"main.js":     `import { commands } from "mayoi"; commands.register("spin.go", () => { while (true) {} }); commands.register("spin.ok", () => 1);`,
	})
	r.Load()
	began := time.Now()
	if _, err := r.Run("spin.go", nil); err == nil {
		t.Fatal("the loop returned")
	}
	if d := time.Since(began); d > 3*time.Second {
		t.Fatalf("stopped after %v", d)
	}
	p := r.Plugin("spin")
	if st := p.Stats(); st.Active || st.Crashes != 1 {
		t.Fatalf("stats = %+v", st)
	}
	// It starts again on the next call.
	if res, err := r.Run("spin.ok", nil); err != nil || string(res) != "1" {
		t.Fatalf("after a stop: %s %v", res, err)
	}
}

func TestUserScript(t *testing.T) {
	r, _, dirs := newRegistry(t)
	os.WriteFile(filepath.Join(dirs.Scripts, "text.ts"), []byte(`export function upperCase(s: string) { return s.toUpperCase(); }
export async function sortLines(s: string) { return s.split("\n").sort().join("\n"); }`), 0o644)
	r.Load()
	var ids []string
	for _, c := range r.Commands("") {
		ids = append(ids, c.ID+"="+c.Title)
	}
	if strings.Join(ids, ",") != "script.text.sortLines=Sort Lines,script.text.upperCase=Upper Case" {
		t.Fatalf("commands = %v", ids)
	}
	res, err := r.Run("script.text.upperCase", "abc")
	if err != nil || string(res) != `"ABC"` {
		t.Fatalf("upperCase = %s, %v", res, err)
	}
	res, err = r.Run("script.text.sortLines", "b\na")
	if err != nil || string(res) != `"a\nb"` {
		t.Fatalf("sortLines = %s, %v", res, err)
	}
}

func TestEventsAndConsole(t *testing.T) {
	r, h, dirs := newRegistry(t)
	writePlugin(t, dirs.Plugins, "listen", map[string]string{
		"plugin.json": `{"name": "listen", "main": "main.js", "activation": ["onLanguage:go"]}`,
		"main.js": `import { events, window } from "mayoi";
console.log("loaded", { a: 1 });
events.on("editor.didSave", (e) => window.notify("saved " + e.path));`,
	})
	r.Load()
	r.Activate("onLanguage:markdown")
	time.Sleep(50 * time.Millisecond)
	if r.Plugin("listen").Started() {
		t.Fatal("started for another language")
	}
	r.Activate("onLanguage:go")
	deadline := time.Now().Add(3 * time.Second)
	for !r.Plugin("listen").Subscribed("editor.didSave") {
		if time.Now().After(deadline) {
			t.Fatal("did not subscribe")
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.Emit("editor.didSave", map[string]string{"path": "/x.go"})
	for h.called("window.notify") == "" {
		if time.Now().After(deadline) {
			t.Fatal("no notification")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c := h.called("window.notify"); !strings.Contains(c, "saved /x.go") {
		t.Fatalf("notify = %s", c)
	}
	logs := r.Plugin("listen").Logs()
	if !slicesContainsMsg(logs, `loaded {"a":1}`) {
		t.Fatalf("logs = %+v", logs)
	}
}

func slicesContainsMsg(lines []LogLine, msg string) bool {
	for _, l := range lines {
		if l.Message == msg {
			return true
		}
	}
	return false
}

func TestManifestChecks(t *testing.T) {
	for _, bad := range []string{
		`{"name": "Bad"}`,
		`{"name": "git"}`,
		`{"name": "x", "contributes": {"commands": [{"id": "y.a", "title": "A"}]}}`,
		`{"name": "x", "contributes": {"commands": [{"id": "x.a"}]}}`,
		`{"name": "x", "main": "../y.js"}`,
		`{"name": "x", "mayoi": "soon"}`,
		`{"name": "x", "contributes": {"previews": [{"id": "x.p", "pattern": "*.d2", "output": "png"}]}}`,
	} {
		if _, err := ParseManifest([]byte(bad)); err == nil {
			t.Errorf("%s parsed", bad)
		}
	}
	if !Satisfies("0.20.1", ">=0.20.0 <1.0.0") || Satisfies("0.19.0", ">=0.20.0") || !Satisfies("dev", ">=9") {
		t.Fatal("Satisfies")
	}
	if !Satisfies("0.20.3", "^0.20.0") || Satisfies("0.21.0", "^0.20.0") {
		t.Fatal("caret")
	}
}

func TestReloadKeepsUnchanged(t *testing.T) {
	r, _, dirs := newRegistry(t)
	writePlugin(t, dirs.Plugins, "a", map[string]string{
		"plugin.json": `{"name": "a", "main": "main.js", "contributes": {"commands": [{"id": "a.x", "title": "X"}]}}`,
		"main.js":     `import { commands } from "mayoi"; let n = 0; commands.register("a.x", () => ++n);`,
	})
	r.Load()
	r.Run("a.x", nil)
	r.Load()
	if res, _ := r.Run("a.x", nil); string(res) != "2" {
		t.Fatalf("an unchanged plugin restarted: %s", res)
	}
	time.Sleep(10 * time.Millisecond)
	writePlugin(t, dirs.Plugins, "a", map[string]string{
		"main.js": `import { commands } from "mayoi"; let n = 10; commands.register("a.x", () => ++n);`,
	})
	r.Load()
	if res, _ := r.Run("a.x", nil); string(res) != "11" {
		t.Fatalf("a changed plugin kept running: %s", res)
	}
}

func TestPack(t *testing.T) {
	r, _, dirs := newRegistry(t)
	writePlugin(t, dirs.Plugins, "packed", map[string]string{
		"plugin.json":                         `{"name": "packed", "main": "main.ts", "contributes": {"commands": [{"id": "packed.x", "title": "X"}]}}`,
		"main.ts":                             `import { commands } from "mayoi"; import { shout } from "shouty"; import pad from "@acme/pad/left"; commands.register("packed.x", (s?: string) => pad(shout(s ?? "")));`,
		"node_modules/shouty/package.json":    `{"name": "shouty", "exports": {".": {"import": "./dist/index.mjs", "require": "./dist/index.cjs"}}}`,
		"node_modules/shouty/dist/index.mjs":  `import { bang } from "./bang.mjs"; export function shout(s) { return bang(s.toUpperCase()); }`,
		"node_modules/shouty/dist/bang.mjs":   `export const bang = (s) => s + "!";`,
		"node_modules/@acme/pad/package.json": `{"name": "@acme/pad", "type": "module"}`,
		"node_modules/@acme/pad/left.js":      `export default function pad(s) { return ">" + s; }`,
	})
	res, err := Pack(filepath.Join(dirs.Plugins, "packed"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rewrote) != 1 || len(res.Files) != 3 {
		t.Fatalf("pack = %+v", res)
	}
	// Without node_modules, it runs from vendor/.
	os.RemoveAll(filepath.Join(dirs.Plugins, "packed", "node_modules"))
	r.Load()
	out, err := r.Run("packed.x", "hi")
	if err != nil || string(out) != `">HI!"` {
		t.Fatalf("run = %s, %v", out, err)
	}
}

// Load notes where each plugin of the folder came from: Git, as
// plugins.json says, or a link, as mayoi plugin dev makes.
func TestOrigin(t *testing.T) {
	r, _, dirs := newRegistry(t)
	writePlugin(t, dirs.Plugins, "from-git", map[string]string{"plugin.json": `{"name": "from-git", "author": "Ann"}`})
	writePlugin(t, dirs.Plugins, "copied", map[string]string{"plugin.json": `{"name": "copied"}`})
	elsewhere := t.TempDir()
	writePlugin(t, elsewhere, "linked", map[string]string{"plugin.json": `{"name": "linked"}`})
	if err := os.Symlink(filepath.Join(elsewhere, "linked"), filepath.Join(dirs.Plugins, "linked")); err != nil {
		t.Skip(err)
	}
	WriteRecords(dirs.Plugins, Records{"from-git": {URL: "https://github.com/ann/from-git", Commit: "abc"}})
	r.Load()
	if o := r.Origin("from-git"); !o.FromGit || o.URL != "https://github.com/ann/from-git" || o.Linked {
		t.Errorf("from-git came from %+v", o)
	}
	if o := r.Origin("linked"); !o.Linked || o.FromGit {
		t.Errorf("linked came from %+v", o)
	}
	if o := r.Origin("copied"); o != (Origin{}) {
		t.Errorf("copied came from %+v", o)
	}
	if p := r.Plugin("from-git"); p == nil || p.Author != "Ann" {
		t.Errorf("the author of from-git is not read: %+v", p)
	}
}

// TestStateIsOneOfALife: a plugin's state is one word at a time, as its
// code starts, is turned off and on, and as it only declares.
func TestStateIsOneOfALife(t *testing.T) {
	r, h, dirs := newRegistry(t)
	writePlugin(t, dirs.Plugins, "life", map[string]string{
		"plugin.json": `{"name": "life", "main": "main.js", "contributes": {"commands": [{"id": "life.x", "title": "X"}]}}`,
		"main.js":     `import { commands } from "mayoi"; commands.register("life.x", () => 1);`,
	})
	writePlugin(t, dirs.Plugins, "look", map[string]string{
		"plugin.json": `{"name": "look", "contributes": {"commands": []}}`,
	})
	r.Load()
	state := func(name string) State { return r.Plugin(name).Stats().State }
	if got := state("life"); got != StateIdle {
		t.Fatalf("before its first call: %v", got)
	}
	if got := state("look"); got != StateStatic {
		t.Fatalf("without code: %v", got)
	}
	if _, err := r.Run("life.x", nil); err != nil {
		t.Fatal(err)
	}
	if got := state("life"); got != StateRunning {
		t.Fatalf("after its first call: %v", got)
	}
	h.settings["life"] = json.RawMessage(`{"enabled": false}`)
	r.Toggled("life")
	if got := state("life"); got != StateOff {
		t.Fatalf("turned off: %v", got)
	}
	h.settings["look"] = json.RawMessage(`{"enabled": false}`)
	if got := state("look"); got != StateOff {
		t.Fatalf("one that declares, turned off: %v", got)
	}
}
