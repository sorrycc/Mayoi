package main

import (
	"encoding/json"
	"strings"
	"testing"

	"mayoi/internal/plugin"
)

type pluginReply struct {
	value any
	err   error
}

func pluginCall(t *testing.T, hs *harness, h *pluginHost, method string, args any) pluginReply {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan pluginReply, 1)
	go func() {
		value, err := h.Call(nil, method, raw)
		done <- pluginReply{value, err}
	}()
	var reply pluginReply
	hs.until(method, func() bool {
		select {
		case reply = <-done:
			return true
		default:
			return false
		}
	})
	return reply
}

func pluginRun(t *testing.T, hs *harness, h *pluginHost, id string, arg any, origin *plugin.CommandContext) json.RawMessage {
	t.Helper()
	done := make(chan pluginReply, 1)
	go func() {
		value, err := h.reg.RunWithContext(id, arg, origin)
		done <- pluginReply{value, err}
	}()
	var reply pluginReply
	hs.until(id, func() bool {
		select {
		case reply = <-done:
			return true
		default:
			return false
		}
	})
	if reply.err != nil {
		t.Fatal(reply.err)
	}
	return reply.value.(json.RawMessage)
}

const contextProbe = `import { commands, editor, workspace } from "mayoi";
let saved;
commands.register("probe.capture", async () => {
  saved = await editor.active();
  return saved.document.ref;
});
commands.register("probe.read", async () => saved.document.snapshot());
commands.register("probe.insert", async () => saved.insert("!"));
commands.register("probe.context", (arg, context) => ({ arg, context }));
commands.register("probe.lookup", async (arg) => workspace.document(JSON.parse(arg)));
`

func probeFiles() map[string]map[string]string {
	return map[string]map[string]string{"probe": {
		"plugin.json": `{"name":"probe","main":"main.ts","contributes":{"commands":[
{"id":"probe.capture","title":"Capture"},{"id":"probe.read","title":"Read"},
{"id":"probe.insert","title":"Insert"},{"id":"probe.context","title":"Context"},
{"id":"probe.lookup","title":"Lookup"}]}}`,
		"main.ts": contextProbe,
	}}
}

func TestPluginDocumentWindowScope(t *testing.T) {
	hs, other := newHarness(t, false), newHarness(t, true)
	a, b := hs.a, other.a
	a.newFile()
	b.newFile()
	a.activeEditor().buf.Set("left")
	b.activeEditor().buf.Set("right")
	if a.activeEditor().id != b.activeEditor().id {
		t.Fatal("fixture must collide")
	}
	h := withPlugins(hs, probeFiles())
	apps := []*app{a, b}
	h.apps = func() []*app { return apps }
	pluginRun(t, hs, h, "probe.capture", nil, nil)
	apps = []*app{b, a}
	got := pluginRun(t, hs, h, "probe.read", nil, nil)
	var snap struct {
		Text    string
		Version uint64
		Ref     plugin.DocumentRef
	}
	if err := json.Unmarshal(got, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Text != "left" || snap.Version != a.activeEditor().buf.Version() || snap.Ref != a.documentRef(a.activeEditor().document) {
		t.Fatalf("wrong snapshot: %s", got)
	}
	pluginRun(t, hs, h, "probe.insert", nil, nil)
	if a.activeEditor().buf.String() != "!left" || b.activeEditor().buf.String() != "right" {
		t.Fatal("edit crossed windows")
	}
}

func TestPluginUntitledCloseIdentity(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.newFile()
	first := a.activeEditor().document
	a.newFile()
	second := a.activeEditor().document
	h := withPlugins(hs, probeFiles())
	ref := a.documentRef(first)
	a.closeTab(a.column().tabs[0], nil, nil)
	hs.settle()
	closed := pluginCall(t, hs, h, "document.snapshot", map[string]any{"ref": ref})
	if closed.err == nil || !strings.Contains(closed.err.Error(), "closed") {
		t.Fatalf("closed reference: %+v", closed)
	}
	live := pluginCall(t, hs, h, "workspace.document", map[string]any{"ref": a.documentRef(second)})
	if live.err != nil || live.value == nil {
		t.Fatalf("lost other untitled: %+v", live)
	}
	lookup := pluginCall(t, hs, h, "workspace.document", map[string]any{"ref": ref})
	if lookup.err != nil || lookup.value != nil {
		t.Fatalf("closed lookup: %+v", lookup)
	}
}

func TestPluginCommandContext(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.newFile()
	target := a.activeEditor().document
	a.newFile()
	h := withPlugins(hs, probeFiles())
	origin := a.pluginContext("tab/context", target)
	got := pluginRun(t, hs, h, "probe.context", "/original/path", origin)
	var result struct {
		Arg     string
		Context plugin.CommandContext
	}
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatal(err)
	}
	if result.Arg != "/original/path" || result.Context.Document == nil || *result.Context.Document != a.documentRef(target) || result.Context.Source != "tab/context" {
		t.Fatalf("context changed: %s", got)
	}
	legacy := pluginRun(t, hs, h, "probe.context", "legacy", nil)
	if !strings.Contains(string(legacy), `"arg":"legacy"`) {
		t.Fatalf("legacy argument: %s", legacy)
	}
}

func TestPluginPickerWindowCloses(t *testing.T) {
	hs := newHarness(t, false)
	h := withPlugins(hs, nil)
	window := hs.a.pluginWindow()
	done := make(chan pluginReply, 1)
	go func() {
		raw, _ := json.Marshal(map[string]any{"windowId": window, "items": []string{}})
		v, err := h.Call(nil, "window.pick", raw)
		done <- pluginReply{v, err}
	}()
	hs.until("picker", func() bool { return hs.a.pluginPick.open })
	hs.a.closePluginWindow()
	var reply pluginReply
	hs.until("closed picker", func() bool {
		select {
		case reply = <-done:
			return true
		default:
			return false
		}
	})
	if reply.err == nil {
		t.Fatal("window close left picker unresolved")
	}
}

func TestPluginLookupAfterWindowCloses(t *testing.T) {
	hs, other := newHarness(t, false), newHarness(t, true)
	a, b := hs.a, other.a
	a.newFile()
	b.newFile()
	h := withPlugins(hs, probeFiles())
	apps := []*app{a, b}
	h.apps = func() []*app { return apps }
	ref := b.documentRef(b.activeEditor().document)
	if live := pluginCall(t, hs, h, "workspace.document", map[string]any{"ref": ref}); live.err != nil || live.value == nil {
		t.Fatalf("background lookup: %+v", live)
	}
	b.shutdown()
	if gone := pluginCall(t, hs, h, "workspace.document", map[string]any{"ref": ref}); gone.err != nil || gone.value != nil {
		t.Fatalf("closed window lookup: %+v", gone)
	}
	if read := pluginCall(t, hs, h, "document.snapshot", map[string]any{"ref": ref}); read.err == nil {
		t.Fatal("closed window read fell through")
	}
}
