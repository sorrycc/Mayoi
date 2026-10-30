package settings

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FolderDir and FolderFile are where a folder keeps its own preferences:
// .mayoi/settings.json in it.
const (
	FolderDir  = ".mayoi"
	FolderFile = "settings.json"
)

// FolderPath is the file of the preferences of the folder dir.
func FolderPath(dir string) string { return filepath.Join(dir, FolderDir, FolderFile) }

// Folder is a folder's own preferences, which choose for its windows the
// preferences of Schema that a folder may: the others in its file are
// kept as they are, and left alone.
type Folder struct {
	Path string

	mu     sync.Mutex
	values map[string]json.RawMessage
	// keys are those of values a folder may choose, in Schema's order.
	keys []string
	// gen counts the changes of the values, written is what the folder
	// last wrote to its file, which a change of the file that is not its
	// own differs from.
	gen     uint64
	written []byte
}

// setValues makes values the folder's, as its lock is held.
func (f *Folder) setValues(values map[string]json.RawMessage) {
	f.values, f.keys = values, nil
	for _, k := range FolderKeys() {
		if _, ok := values[k]; ok {
			f.keys = append(f.keys, k)
		}
	}
	f.gen++
}

// LoadFolder reads the preferences of the folder dir: none when it has no
// file, or one that does not parse.
func LoadFolder(dir string) *Folder {
	f := &Folder{Path: FolderPath(dir)}
	f.Reload()
	return f
}

// Reload reads the file again, and reports whether its preferences
// changed. A file that does not parse is let be until it does.
// The file is read under the lock that Set writes it under, so a read
// never finds the file of before a change the values already have.
func (f *Folder) Reload() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := os.ReadFile(f.Path)
	var values map[string]json.RawMessage
	if err == nil {
		if json.Unmarshal(data, &values) != nil {
			return false
		}
	}
	if bytes.Equal(data, f.written) && data != nil {
		return false
	}
	if sameValues(values, f.values) {
		return false
	}
	f.setValues(values)
	return true
}

func sameValues(x, y map[string]json.RawMessage) bool {
	if len(x) != len(y) {
		return false
	}
	for k, v := range x {
		if w, ok := y[k]; !ok || !bytes.Equal(bytes.TrimSpace(v), bytes.TrimSpace(w)) {
			return false
		}
	}
	return true
}

// Gen counts the changes of the folder's preferences.
func (f *Folder) Gen() uint64 {
	if f == nil {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gen
}

// Keys are the keys of the preferences the folder chooses, of those a
// folder may, in Schema's order.
func (f *Folder) Keys() []string {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.keys
}

// Raw returns what the folder chooses for the preference of key, as JSON,
// nil for nothing.
func (f *Folder) Raw(key string) json.RawMessage {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Contains(f.keys, key) {
		return nil
	}
	return f.values[key]
}

// Has reports whether the folder chooses the preference of key.
func (f *Folder) Has(key string) bool { return slices.Contains(f.Keys(), key) }

// Apply returns p with the preferences the folder chooses.
func (f *Folder) Apply(p Prefs) Prefs {
	if f == nil {
		return p
	}
	f.mu.Lock()
	values, keys := f.values, f.keys
	f.mu.Unlock()
	for _, k := range keys {
		if raw, ok := values[k]; ok {
			// A value of the wrong type leaves the preference as it was.
			p.Set(k, raw)
		}
	}
	return p
}

// Set has the folder choose raw, as JSON, for the preference of key, or
// stop choosing it for nil, and writes the file.
func (f *Folder) Set(key string, raw json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	values := make(map[string]json.RawMessage, len(f.values)+1)
	for k, v := range f.values {
		values[k] = v
	}
	if raw == nil {
		delete(values, key)
	} else {
		values[key] = raw
	}
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f.setValues(values)
	f.written = data
	return WriteFile(f.Path, data)
}

// Watch calls changed, on a goroutine of its own, each time something
// other than the folder changes its file, as a person saving it, or a
// checkout. stop stops watching.
func (f *Folder) Watch(changed func()) (stop func(), err error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	// The folder of the file, once there is one, and the folder above it
	// to see it made.
	dir := filepath.Dir(f.Path)
	top := filepath.Dir(dir)
	if err := w.Add(top); err != nil {
		w.Close()
		return nil, err
	}
	w.Add(dir)
	go func() {
		var settle <-chan time.Time
		for {
			select {
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				switch filepath.Clean(ev.Name) {
				case dir:
					if ev.Op&fsnotify.Create != 0 {
						w.Add(dir)
					}
					settle = time.After(80 * time.Millisecond)
				case filepath.Clean(f.Path):
					settle = time.After(80 * time.Millisecond)
				}
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			case <-settle:
				settle = nil
				if f.Reload() {
					changed()
				}
			}
		}
	}()
	return func() { w.Close() }, nil
}
