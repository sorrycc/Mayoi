package settings

import (
	"encoding/json"
	"testing"
)

func TestSaveCodeActionPreferences(t *testing.T) {
	p := Default().Prefs
	if len(p.OrganizeImportsOnSave) > 0 || len(p.FixAllOnSave) > 0 {
		t.Fatal("save actions are on by default")
	}
	for _, key := range []string{"organizeImportsOnSave", "fixAllOnSave"} {
		if !p.IsDefaultKey(key) {
			t.Fatalf("%s is enabled by default", key)
		}
		field, ok := FieldOf(key)
		if !ok || !field.Folder || field.Kind != KindJSON {
			t.Fatalf("missing folder preference %s", key)
		}
		if err := p.Set(key, json.RawMessage(`{"go":true,"*":false}`)); err != nil {
			t.Fatal(err)
		}
		if p.IsDefaultKey(key) {
			t.Fatalf("%s was not set", key)
		}
		p.Reset(key)
		if !p.IsDefaultKey(key) {
			t.Fatalf("%s did not reset", key)
		}
	}
}
