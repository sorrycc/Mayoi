package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// AddToUserPath puts Mayoi's command folder on the user's PATH.
func AddToUserPath(home string) error { return updateUserPath(installDir(home), true) }

// RemoveFromUserPath takes Mayoi's command folder off the user's PATH.
func RemoveFromUserPath(home string) error { return updateUserPath(installDir(home), false) }

func updateUserPath(dir string, add bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf(`open HKCU\Environment: %w`, err)
	}
	defer key.Close()
	value, typ, err := key.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		value, typ, err = "", registry.SZ, nil
	}
	if err != nil {
		return fmt.Errorf("read the user Path: %w", err)
	}
	updated := editPath(value, dir, add)
	if updated != value {
		switch typ {
		case registry.SZ:
			err = key.SetStringValue("Path", updated)
		case registry.EXPAND_SZ:
			err = key.SetExpandStringValue("Path", updated)
		}
		if err != nil {
			return fmt.Errorf("write the user Path: %w", err)
		}
		defer broadcastEnvironmentChange()
	}
	if err := os.Setenv("PATH", editPath(os.Getenv("PATH"), dir, add)); err != nil {
		return fmt.Errorf("update Mayoi's process PATH: %w", err)
	}
	return nil
}

// editPath makes dir present or removes every copy, comparing Windows paths
// without case. Existing REG_EXPAND_SZ entries stay unexpanded.
func editPath(value, dir string, add bool) string {
	entries := filepath.SplitList(value)
	found := false
	kept := entries[:0]
	for _, entry := range entries {
		if strings.EqualFold(filepath.Clean(entry), filepath.Clean(dir)) {
			found = true
			continue
		}
		kept = append(kept, entry)
	}
	if add {
		if found {
			return value
		}
		if value == "" {
			return dir
		}
		if strings.HasSuffix(value, ";") {
			return value + dir
		}
		return value + ";" + dir
	}
	return strings.Join(kept, ";")
}

func broadcastEnvironmentChange() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001a
		smtoAbortIfHung = 0x0002
	)
	environment, _ := windows.UTF16PtrFromString("Environment")
	windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW").Call(
		hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(environment)), smtoAbortIfHung, 5000, 0)
}
