package main

import (
	"os"
	"path/filepath"
	"testing"

	"mayoi/internal/plugin"
)

func TestPluginSyncAsksBeforeInstalling(t *testing.T) {
	hs := newHarness(t, false)
	h := withPlugins(hs, nil)
	a := hs.a
	asks := plugin.Records{"demo": {URL: "https://example.com/demo"}}
	h.askInstall(a, asks)
	h.askInstall(a, asks)
	hs.settle()
	if a.confirm == nil || len(a.asks) != 0 || !hs.tt.HasText("Install a plugin from settings sync?") || !hs.tt.HasText("https://example.com/demo") {
		t.Fatalf("asked: %v, waiting %d", a.confirm != nil, len(a.asks))
	}
	// Installing trusts the records.
	cf := a.confirm
	a.confirm = nil
	cf.run()
	if h.trusted["demo"] != asks["demo"] || h.asking != nil {
		t.Fatalf("trusted = %v", h.trusted)
	}
	// Not now is remembered.
	other := plugin.Records{"other": {URL: "https://example.com/other"}}
	h.askInstall(a, other)
	hs.settle()
	a.dismiss()
	if _, err := os.Stat(filepath.Join(h.reg.Dirs.Plugins, plugin.DeclinedFile)); err != nil || h.trusted["other"] == other["other"] {
		t.Fatalf("declined: %v %v", err, h.trusted)
	}
}
