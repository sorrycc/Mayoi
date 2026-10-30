package main

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

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
		ph.pt.Key(0, ui.KeyDown)
		ph.frames()
		ph.pt.Key(0, ui.KeyDown)
		ph.pt.Advance(60 * time.Millisecond)
		ph.pt.Frame()
		ph.shot("prefs-sections-moving" + suffix)
		ph.frames()
		ph.shot("prefs-sections" + suffix)
		ph.w.section = settings.CatFonts
		ph.w.set("fontSize", json.RawMessage(`15`))
		ph.frames()
		ph.shot("prefs-fonts" + suffix)
		ph.w.reset("fontSize", nil)
		ph.frames()
		ph.shot("prefs-undo" + suffix)
		ph.pt.Click("Dismiss")
		ph.frames()
		ph.w.section = settings.CatDiff
		ph.frames()
		ph.shot("prefs-diff" + suffix)
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
		ph.m.keys, _ = keymap.Parse([]byte(`[{"key": "f9", "command": "file.newWindow"}, {"key": "cmd+k cmd+o", "command": "file.openFolder"}, {"key": "cmd+n", "command": "-file.new"}, {"key": "cmd+b", "command": "git.fetch"}]`))
		ph.a.keys = ph.m.keys
		ph.frames()
		ph.shot("prefs-keys" + suffix)
		k := &ph.w.keys
		ph.pt.Click("Save")
		ph.frames()
		ph.pt.Key(0, ui.KeyDown)
		ph.frames()
		ph.shot("prefs-keys-focus" + suffix)
		ph.pt.Click("Conflicts")
		ph.frames()
		ph.shot("prefs-keys-conflicts" + suffix)
		k.filter = keysAll
		ph.w.section = settings.CatFonts
		ph.w.families = []string{"Courier New", "Menlo", "Monaco", "PT Mono"}
		ph.frames()
		ph.pt.Click("Show Fonts")
		ph.frames()
		ph.pt.Key(0, ui.KeyDown)
		ph.pt.Key(0, ui.KeyDown)
		ph.frames()
		ph.shot("prefs-fonts-picker" + suffix)
		ph.pt.Key(0, ui.KeyEscape)
		ph.w.section = prefsShortcuts
		ph.frames()
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
