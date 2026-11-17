package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

// SyncedFile is where the plugins folder keeps the records it last agreed
// on with settings sync: what changed here and what changed elsewhere are
// told apart against it.
const SyncedFile = ".synced.json"

// DeclinedFile is where the plugins folder keeps the records from settings
// sync that the person chose not to install here, so as not to ask again
// until one changes.
const DeclinedFile = ".declined.json"

func readSynced(dir string) (Records, bool) {
	return readRecordsFile(filepath.Join(dir, SyncedFile))
}

func writeSynced(dir string, recs Records) error {
	return writeRecordsFile(filepath.Join(dir, SyncedFile), recs)
}

func readRecordsFile(path string) (Records, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	recs := Records{}
	json.Unmarshal(data, &recs)
	return recs, true
}

func writeRecordsFile(path string, recs Records) error {
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Decline keeps recs, records settings sync brought, from being asked
// about again here until they change.
func Decline(dir string, recs Records) error {
	declined, _ := readRecordsFile(filepath.Join(dir, DeclinedFile))
	if declined == nil {
		declined = Records{}
	}
	maps.Copy(declined, recs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeRecordsFile(filepath.Join(dir, DeclinedFile), declined)
}

// SyncableURL reports whether settings sync may install from url: only
// over https or ssh, never a folder or file of the machine.
func SyncableURL(url string) bool {
	for _, p := range []string{"https://", "ssh://", "git@"} {
		if strings.HasPrefix(url, p) && len(url) > len(p) {
			return true
		}
	}
	return false
}

// syncableURL is SyncableURL, which the tests replace to sync from
// folders.
var syncableURL = SyncableURL

// Sync agrees the plugins installed here with remote, the records settings
// sync brought from the account, against what they last agreed on: a plugin
// installed, updated or removed elsewhere is so here, and one installed,
// updated or removed here stays so. A plugin from elsewhere is installed
// only when it is here already from the same URL or trusted has its record:
// the others are returned as asks, to ask the person about, but for those
// declined before. It returns the records both now have, for settings sync
// to send, which keep those not installed, and the names it installed and
// removed.
func Sync(ctx context.Context, dir string, remote, trusted Records) (agreed Records, installed, removed []string, asks Records, err error) {
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
	declined, _ := readRecordsFile(filepath.Join(dir, DeclinedFile))
	want := maps.Clone(local)
	held := Records{}
	var errs []error
	for name, r := range remote {
		if b, ok := base[name]; !ok || b != r {
			// Installed or updated elsewhere, unless changed here too.
			if l, ok := local[name]; !ok || l == base[name] {
				switch {
				case !syncableURL(r.URL):
					held[name] = r
					errs = append(errs, fmt.Errorf("%s: %s is not an https or ssh URL", name, r.URL))
				case ok && l.URL == r.URL, trusted[name] == r:
					want[name] = r
				default:
					held[name] = r
					if declined[name] != r {
						if asks == nil {
							asks = Records{}
						}
						asks[name] = r
					}
				}
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
		var ierr error
		installed, removed, ierr = SyncInstall(ctx, dir, want)
		errs = append(errs, ierr)
	}
	agreed = ReadRecords(dir)
	errs = append(errs, writeSynced(dir, agreed))
	// What is not installed here stays in the account for the other
	// machines, and is told apart again at the next sync.
	maps.Copy(agreed, held)
	return agreed, installed, removed, asks, errors.Join(errs...)
}
