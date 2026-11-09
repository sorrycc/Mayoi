package plugin

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
)

// SyncedFile is where the plugins folder keeps the records it last agreed
// on with settings sync: what changed here and what changed elsewhere are
// told apart against it.
const SyncedFile = ".synced.json"

func readSynced(dir string) (Records, bool) {
	data, err := os.ReadFile(filepath.Join(dir, SyncedFile))
	if err != nil {
		return nil, false
	}
	recs := Records{}
	json.Unmarshal(data, &recs)
	return recs, true
}

func writeSynced(dir string, recs Records) error {
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, SyncedFile), data, 0o644)
}

// Sync agrees the plugins installed here with remote, the records settings
// sync brought from the account, against what they last agreed on: a plugin
// installed, updated or removed elsewhere is so here, and one installed,
// updated or removed here stays so. It returns the records both now have,
// for settings sync to send, and the names it installed and removed.
func Sync(ctx context.Context, dir string, remote Records) (agreed Records, installed, removed []string, err error) {
	local := ReadRecords(dir)
	base, ok := readSynced(dir)
	if !ok {
		// The first sync of this folder: what is here stays, and what the
		// account has is added.
		base = Records{}
		for name, r := range remote {
			if l, ok := local[name]; ok && l == r {
				base[name] = r
			}
		}
	}
	want := maps.Clone(local)
	for name, r := range remote {
		if b, ok := base[name]; !ok || b != r {
			// Installed or updated elsewhere, unless changed here too.
			if l, ok := local[name]; !ok || l == base[name] {
				want[name] = r
			}
		}
	}
	for name, b := range base {
		if _, ok := remote[name]; !ok {
			// Removed elsewhere, unless changed here since.
			if l, ok := local[name]; ok && l == b {
				delete(want, name)
			}
		}
	}
	if !maps.Equal(want, local) {
		installed, removed, err = SyncInstall(ctx, dir, want)
	}
	agreed = ReadRecords(dir)
	if werr := writeSynced(dir, agreed); err == nil {
		err = werr
	}
	return agreed, installed, removed, err
}
