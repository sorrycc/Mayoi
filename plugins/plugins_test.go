package plugins

import (
	"io/fs"
	"testing"

	"mayoi/internal/plugin"
)

// TestBundled: each bundled plugin reads, and those that bring a language
// server start off, until the settings turn them on.
func TestBundled(t *testing.T) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	servers := 0
	for _, e := range entries {
		data, err := fs.ReadFile(FS, e.Name()+"/"+plugin.ManifestFile)
		if err != nil {
			t.Fatal(err)
		}
		m, err := plugin.ParseManifest(data)
		if err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		if m.Name != e.Name() {
			t.Errorf("%s is named %q", e.Name(), m.Name)
		}
		if len(m.Contributes.LanguageServers) == 0 {
			continue
		}
		servers++
		if m.DefaultOn() {
			t.Errorf("%s starts on", m.Name)
		}
		if _, ok := m.Contributes.Settings["path"]; !ok {
			t.Errorf("%s has no path setting", m.Name)
		}
	}
	if servers != 5 {
		t.Fatalf("%d bundled language servers", servers)
	}
}
