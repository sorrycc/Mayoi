package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func outputProbe(name string) map[string]string {
	manifest := `{"name":"NAME","main":"main.ts","contributes":{"commands":[{"id":"NAME.write","title":"Write"}]}}`
	main := `import {commands,window} from "mayoi";commands.register("NAME.write",async arg=>{try{const a=JSON.parse(arg);await window.openText(a.text,{title:"Response",language:"plaintext",beside:a.ref,key:"response",windowId:a.windowId});return true;}catch(error){return {error:String(error)}}});`
	return map[string]string{"plugin.json": strings.ReplaceAll(manifest, "NAME", name), "main.ts": strings.ReplaceAll(main, "NAME", name)}
}

func TestPluginOutputBesideAndReuse(t *testing.T) {
	hs := newHarness(t, false)
	a := hs.a
	a.newFile()
	source := a.activeEditor().document
	h := withPlugins(hs, map[string]map[string]string{"output": outputProbe("output"), "other": outputProbe("other")})
	write := func(id, text string) string {
		raw, _ := json.Marshal(map[string]any{"ref": a.documentRef(source), "text": text})
		return string(pluginRun(t, hs, h, id, string(raw), nil))
	}
	if write("output.write", "first") != "true" {
		t.Fatal("first output failed")
	}
	if len(a.cols) != 2 || len(a.docs) != 2 || a.cols[0].tab().ed.document != source {
		t.Fatal("source not kept beside output")
	}
	output := a.cols[1].tab().ed.document
	if write("output.write", "second") != "true" || len(a.cols) != 2 || len(a.docs) != 2 || a.cols[1].tab().ed.document != output || output.buf.String() != "second" {
		t.Fatal("did not reuse output")
	}
	if write("other.write", "owned by other") != "true" || len(a.docs) != 3 || output.buf.String() != "second" {
		t.Fatal("output key crossed plugin owners")
	}
	output.buf.Set("user edits")
	if got := write("output.write", "overwrite"); !strings.Contains(got, "unsaved edits") || output.buf.String() != "user edits" {
		t.Fatalf("overwrote edits: %s", got)
	}
}

func TestPluginOutputBesideWindowAndClose(t *testing.T) {
	hs, other := newHarness(t, false), newHarness(t, true)
	a, b := hs.a, other.a
	a.newFile()
	b.newFile()
	source := a.activeTab()
	ref := a.documentRef(source.ed.document)
	h := withPlugins(hs, map[string]map[string]string{"output": outputProbe("output")})
	h.apps = func() []*app { return []*app{b, a} }
	raw, _ := json.Marshal(map[string]any{"ref": ref, "text": "source window"})
	if got := pluginRun(t, hs, h, "output.write", string(raw), nil); string(got) != "true" {
		t.Fatalf("output failed: %s", got)
	}
	if len(a.cols) != 2 || len(a.docs) != 2 || len(b.cols) != 1 || len(b.docs) != 1 {
		t.Fatal("output followed foreground window")
	}
	raw, _ = json.Marshal(map[string]any{"ref": ref, "text": "wrong window", "windowId": b.pluginWindow()})
	if got := pluginRun(t, hs, h, "output.write", string(raw), nil); !strings.Contains(string(got), "share a window") {
		t.Fatalf("accepted mismatched window: %s", got)
	}
	a.dropTab(source)
	raw, _ = json.Marshal(map[string]any{"ref": ref, "text": "late output"})
	if got := pluginRun(t, hs, h, "output.write", string(raw), nil); !strings.Contains(string(got), "closed") {
		t.Fatalf("reopened closed source: %s", got)
	}
	if len(b.docs) != 1 {
		t.Fatal("late output redirected")
	}
}

func TestPluginOutputBesideStackedSource(t *testing.T) {
	for _, below := range []bool{false, true} {
		t.Run(fmt.Sprint("below=", below), func(t *testing.T) {
			hs := newHarness(t, false)
			a := hs.a
			a.newFile()
			top := a.column()
			a.newEmptyColumn(true)
			a.newFile()
			bottom := a.column()
			sourceCol, otherCol := top, bottom
			if below {
				sourceCol, otherCol = bottom, top
			}
			a.activate(sourceCol, 0, true)
			source, other := sourceCol.tab().ed.document, otherCol.tab().ed.document
			h := withPlugins(hs, map[string]map[string]string{"output": outputProbe("output")})
			var output *document
			for _, text := range []string{"first", "second"} {
				raw, _ := json.Marshal(map[string]any{"ref": a.documentRef(source), "text": text})
				if got := pluginRun(t, hs, h, "output.write", string(raw), nil); string(got) != "true" {
					t.Fatalf("output failed: %s", got)
				}
				if len(a.cols) != 3 || len(a.docs) != 3 || otherCol.tab().ed.document != other {
					t.Fatal("output reused the pane stacked below its source")
				}
				parent := sourceCol.node.parent
				at := parent.at(sourceCol.node)
				if parent.down || at+1 >= len(parent.kids) || parent.kids[at+1].col == nil {
					t.Fatal("output is not immediately right of its source")
				}
				d := parent.kids[at+1].col.tab().ed.document
				if d.buf.String() != text || output != nil && output != d {
					t.Fatal("output was not reused in the same right-hand pane")
				}
				output = d
			}
			hs.settle()
			hs.shot("plugin-output-stacked-" + fmt.Sprint(below))
		})
	}
}
