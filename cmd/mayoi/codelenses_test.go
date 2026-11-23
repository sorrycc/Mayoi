package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"mayoi/internal/highlight"
	"mayoi/internal/plugin"
)

const lensProbe = `import {commands, editor, languages, workspace, window} from "mayoi";
let seen;
commands.register("lens.publish", async (arg) => {
 const ed = await editor.active(), snap = await ed.document.snapshot();
 return languages.setCodeLenses(ed.document.ref, [{id:"analyze", line:Number(arg ?? 1), title:"Run Analysis", command:"lens.click", arg:"original"}], {version:snap.version});
});
commands.register("lens.click", (arg, context) => { seen = {arg,context}; });
commands.register("lens.seen", () => seen);
commands.register("lens.crash", () => { while(true) {} });
commands.register("lens.output", async arg => { const ref=JSON.parse(arg); await window.openText('{"ok":true}', {title:"Analysis Result", language:"json", windowId:ref.windowId}); });
`

func lensFiles() map[string]map[string]string {
	return map[string]map[string]string{"lens": {
		"plugin.json": `{"name":"lens","main":"main.ts","contributes":{"commands":[{"id":"lens.publish","title":"Publish"},{"id":"lens.click","title":"Click"},{"id":"lens.seen","title":"Seen"},{"id":"lens.output","title":"Output"},{"id":"lens.crash","title":"Crash"}]}}`,
		"main.ts":     lensProbe,
	}}
}

func TestPluginCodeLensesVersionAndRuntime(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.newFile()
	d := a.activeEditor().document
	d.buf.Set("Document\nanalyze input\n")
	h := withPlugins(hs, lensFiles())
	if string(pluginRun(t, hs, h, "lens.publish", nil, nil)) != "true" {
		t.Fatal("publish failed")
	}
	hs.settle()
	if len(d.currentLenses()) != 1 {
		t.Fatal("missing actions")
	}
	old := d.lenses[0]
	a.runCodeLens(d, old, old.items[0])
	hs.until("lens click", func() bool { return string(pluginRun(t, hs, h, "lens.seen", nil, nil)) != "null" })
	seen := pluginRun(t, hs, h, "lens.seen", nil, nil)
	var result struct {
		Arg     string
		Context plugin.CommandContext
	}
	if err := json.Unmarshal(seen, &result); err != nil {
		t.Fatal(err)
	}
	if result.Arg != "original" || result.Context.Source != "codeLens" || *result.Context.Document != a.documentRef(d) || result.Context.Position.Line != 1 {
		t.Fatalf("wrong context: %s", seen)
	}
	d.buf.Set("changed\n")
	if len(d.currentLenses()) != 0 {
		t.Fatal("stale actions survived edit")
	}
	stale, _ := json.Marshal(map[string]any{"ref": a.documentRef(d), "version": old.version, "items": old.items})
	done := make(chan pluginReply, 1)
	go func() {
		value, err := h.Call(old.owner, old.generation, "languages.setCodeLenses", stale)
		done <- pluginReply{value, err}
	}()
	var reply pluginReply
	hs.until("stale publication", func() bool {
		select {
		case reply = <-done:
			return true
		default:
			return false
		}
	})
	if reply.err != nil || reply.value != false {
		t.Fatalf("stale version accepted: %+v", reply)
	}
	stale, _ = json.Marshal(map[string]any{"ref": a.documentRef(d), "version": d.buf.Version(), "items": old.items})
	h.reg.Reload("lens")
	if old.owner.Generation() == old.generation {
		t.Fatal("reload kept old instance identity")
	}
	go func() {
		value, err := h.Call(old.owner, old.generation, "languages.setCodeLenses", stale)
		done <- pluginReply{value, err}
	}()
	hs.until("old runtime publication", func() bool {
		select {
		case reply = <-done:
			return true
		default:
			return false
		}
	})
	if reply.err != nil || reply.value != false || len(d.currentLenses()) != 0 {
		t.Fatal("old runtime revived actions")
	}
}

func TestPluginOutputWindowAndLanguage(t *testing.T) {
	hs, other := newHarness(t, false), newHarness(t, true)
	a, b := hs.a, other.a
	a.newFile()
	b.newFile()
	ref := a.documentRef(a.activeEditor().document)
	h := withPlugins(hs, lensFiles())
	h.apps = func() []*app { return []*app{b, a} }
	raw, _ := json.Marshal(ref)
	pluginRun(t, hs, h, "lens.output", string(raw), nil)
	d := a.activeEditor().document
	if len(a.docs) != 2 || len(b.docs) != 1 || d.path != "" || languageOf(d) != "json" || highlight.Name(d.hl.Lexer()) != "JSON" {
		t.Fatalf("wrong output: %s/%s/%s", d.path, languageOf(d), highlight.Name(d.hl.Lexer()))
	}
	a.loadsClosed = true
	reply := pluginCall(t, hs, h, "window.openText", map[string]any{"windowId": ref.WindowID, "text": "late"})
	if reply.err == nil || len(b.docs) != 1 {
		t.Fatal("closed window output redirected")
	}
}

func TestPluginCodeLensGeometryAndClick(t *testing.T) {
	for _, wrap := range []bool{false, true} {
		t.Run(fmt.Sprint(wrap), func(t *testing.T) {
			hs := newHarness(t, false)
			a := hs.a
			a.settings.WordWrap = wrap
			a.newFile()
			d := a.activeEditor()
			d.buf.Set("Document\nanalyze input\nlast\n")
			h := withPlugins(hs, lensFiles())
			pluginRun(t, hs, h, "lens.publish", nil, nil)
			hs.settle()
			g := a.geometry(d, d.buf.Snapshot().LineCount())
			if math.Abs(float64(g.top(1)-g.top(0)-2*a.font.lineH)) > 0.1 {
				t.Fatalf("wrong gap geometry: %v %v", g.top(0), g.top(1))
			}
			r, ok := hs.tt.Find("Run Analysis")
			if !ok || r.H <= 0 || r.W <= 0 {
				t.Fatalf("missing button: %+v", r)
			}
			if err := hs.tt.Click("Run Analysis"); err != nil {
				t.Fatal(err)
			}
			hs.until("clicked action", func() bool { return string(pluginRun(t, hs, h, "lens.seen", nil, nil)) != "null" })
			if d.buf.String() != "Document\nanalyze input\nlast\n" {
				t.Fatal("button edited source")
			}
			hs.shot("code-lenses-" + fmt.Sprint(wrap))
		})
	}
}

func TestPluginCodeLensGeometryChanges(t *testing.T) {
	hs, d := foldHarness(t, "lenses.go", "package p\nfunc f() {\n x:=1\n}\n// "+strings.Repeat("wrap ", 120)+"\nvar after=1\n", false)
	a := hs.a
	h := withPlugins(hs, lensFiles())
	pluginRun(t, hs, h, "lens.publish", "5", nil)
	check := func(what string) {
		t.Helper()
		at, _ := d.buf.Snapshot().LineRange(5)
		hs.place(d, at, at)
		hs.settle()
		r, ok := hs.tt.Find("Run Analysis")
		anchor, visible := a.hoverAnchor(&hoverCard{ed: d, at: at})
		if !ok || !visible || math.Abs(float64(r.Y+r.H-anchor.Y)) > 1 {
			t.Fatalf("%s: button %+v, text %+v, visible %v", what, r, anchor, visible)
		}
	}
	check("plain")
	a.toggleFoldAt(d, 1)
	check("folded above")
	a.unfoldAllCmd(d)
	a.settings.WordWrap = true
	a.settings.WrapGen++
	check("wrapped")
	a.settings.FontSize = 20
	check("zoomed")
	a.settings.WordWrap = false
	a.settings.WrapGen++
	check("unwrapped after zoom")
	a.splitRight()
	hs.settle()
	if a.activeEditor().document != d.document {
		t.Fatal("split lost document")
	}
	if len(a.activeEditor().currentLenses()) != 1 {
		t.Fatal("split lost actions")
	}
}

func TestPluginCodeLensStopPaths(t *testing.T) {
	for _, action := range []string{"disable", "remove", "crash"} {
		t.Run(action, func(t *testing.T) {
			hs := newHarness(t, false)
			a := hs.a
			a.newFile()
			d := a.activeEditor().document
			d.buf.Set("Document\nanalyze input\n")
			h := withPlugins(hs, lensFiles())
			pluginRun(t, hs, h, "lens.publish", nil, nil)
			old := d.lenses[0]
			switch action {
			case "disable":
				w := &prefsWindow{m: &mayoi{s: a.settings}}
				w.setPluginSetting("lens", "enabled", false, true)
			case "remove":
				if err := os.RemoveAll(h.reg.Plugin("lens").Dir); err != nil {
					t.Fatal(err)
				}
				h.reg.Load()
			case "crash":
				limit := plugin.BusyLimit
				plugin.BusyLimit = 50 * time.Millisecond
				defer func() { plugin.BusyLimit = limit }()
				done := make(chan error, 1)
				go func() { _, err := h.reg.Run("lens.crash", nil); done <- err }()
				hs.until("crash", func() bool {
					select {
					case err := <-done:
						if err == nil {
							t.Fatal("crash succeeded")
						}
						return true
					default:
						return false
					}
				})
			}
			hs.until("actions cleared", func() bool { return len(d.currentLenses()) == 0 })
			if old.owner.Generation() == old.generation {
				t.Fatal("stop kept generation")
			}
		})
	}
}

func TestPluginCodeLensDuringActivation(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.newFile()
	d := a.activeEditor().document
	d.buf.Set("Document\nanalyze input\n")
	files := lensFiles()
	files["lens"]["main.ts"] += `export async function activate() {
const doc=(await editor.active()).document, snap=await doc.snapshot();
await languages.setCodeLenses(doc.ref,[{id:"early",line:1,title:"Early",command:"lens.click"}],{version:snap.version});
await new Promise(resolve=>setTimeout(resolve,400));
}`
	h := withPlugins(hs, files)
	done := make(chan pluginReply, 1)
	go func() { v, err := h.reg.Run("lens.seen", nil); done <- pluginReply{v, err} }()
	hs.until("actions during activation", func() bool { return len(d.currentLenses()) == 1 })
	if h.reg.Plugin("lens").Started() {
		t.Fatal("fixture did not pause activation")
	}
	batch := d.lenses[0]
	a.runCodeLens(d, batch, batch.items[0])
	hs.until("activation ended", func() bool {
		select {
		case result := <-done:
			if result.err != nil {
				t.Fatal(result.err)
			}
			return true
		default:
			return false
		}
	})
	hs.until("early click ran", func() bool { return string(pluginRun(t, hs, h, "lens.seen", nil, nil)) != "null" })
}

func TestPluginCodeLensPointerExpiresOnEdit(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.newFile()
	d := a.activeEditor().document
	d.buf.Set("old document\nanalyze old\n")
	h := withPlugins(hs, lensFiles())
	pluginRun(t, hs, h, "lens.publish", nil, nil)
	hs.settle()
	r, ok := hs.tt.Find("Run Analysis")
	if !ok {
		t.Fatal("no button")
	}
	x, y := r.X+r.W/2, r.Y+r.H/2
	hs.tt.Press(x, y)
	d.buf.Set("new document\nanalyze new\n")
	d.lenses[0].version = d.buf.Version()
	hs.settle()
	hs.tt.Release(x, y)
	hs.settle()
	for range 10 {
		if got := pluginRun(t, hs, h, "lens.seen", nil, nil); string(got) != "null" {
			t.Fatalf("old pointer press invoked a replaced action: %s", got)
		}
	}
}
