package main

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"mayoi/internal/keymap"
	"mayoi/internal/settings"
)

// shot writes the settings window to the folder of $SHOTS.
func (ph *prefsHarness) shot(name string) {
	dir := os.Getenv("SHOTS")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		ph.t.Fatal(err)
	}
	png.Encode(f, ph.pt.Image())
	f.Close()
}

func TestShotPrefs(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip()
	}
	for _, dark := range []bool{true, false} {
		ph := newPrefsHarness(t)
		ph.pt.SetDark(dark)
		suffix := "-light"
		if dark {
			suffix = "-dark"
		}
		ph.w.set("sidebarRight", json.RawMessage(`true`))
		ph.w.set("panelPosition", json.RawMessage(`"right"`))
		ph.w.set("vim", json.RawMessage(`true`))
		ph.frames()
		ph.shot("prefs-appearance" + suffix)
		ph.w.section = settings.CatEditor
		ph.frames()
		ph.shot("prefs-editor" + suffix)
		ph.w.query = "font"
		ph.frames()
		ph.shot("prefs-search" + suffix)
		ph.w.query = "nothing like it"
		ph.frames()
		ph.shot("prefs-none" + suffix)
		ph.w.query = ""
		ph.w.section = prefsShortcuts
		ph.m.keys, _ = keymap.Parse([]byte(`[{"key": "f9", "command": "file.newWindow"}, {"key": "cmd+k cmd+o", "command": "file.openFolder"}, {"key": "cmd+n", "command": "-file.new"}]`))
		ph.a.keys = ph.m.keys
		ph.frames()
		ph.shot("prefs-keys" + suffix)
		k := &ph.w.keys
		rows := k.rows(ph.a)
		k.record(rows[0].cmd)
		k.strokes = rows[1].keys
		ph.frames()
		ph.shot("prefs-record" + suffix)
		k.recording = nil
		ph.w.section, ph.w.folder = settings.CatEditor, ph.root
		ph.frames()
		ph.shot("prefs-folder" + suffix)
	}
}
