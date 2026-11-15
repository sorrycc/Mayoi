package cli

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// AddContextMenu adds label, as "Open with Mayoi", to Explorer's
// right-click menu of files, folders and a folder's background, opening
// them with Mayoi.exe at exe.
func AddContextMenu(exe, label string) error {
	defer notifyAssociations()
	for _, m := range contextMenus {
		if err := writeContextMenu(m, exe, label); err != nil {
			return err
		}
	}
	return nil
}

func writeContextMenu(m contextMenu, exe, label string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, m.key(), registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf(`open HKCU\%s: %w`, m.key(), err)
	}
	defer key.Close()
	if err := key.SetStringValue("", label); err != nil {
		return err
	}
	if err := key.SetStringValue("Icon", exe+",0"); err != nil {
		return err
	}
	cmd, _, err := registry.CreateKey(key, "command", registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf(`open HKCU\%s\command: %w`, m.key(), err)
	}
	defer cmd.Close()
	return cmd.SetStringValue("", m.command(exe))
}

// RemoveContextMenu takes the item off Explorer's menus, whether Mayoi or
// its installer added it, and reports whether there was one.
func RemoveContextMenu() (bool, error) {
	removed := false
	for _, m := range contextMenus {
		for _, k := range []string{m.key() + `\command`, m.key()} {
			err := registry.DeleteKey(registry.CURRENT_USER, k)
			switch {
			case err == nil:
				removed = true
			case !errors.Is(err, registry.ErrNotExist):
				return removed, fmt.Errorf(`delete HKCU\%s: %w`, k, err)
			}
		}
	}
	if removed {
		notifyAssociations()
	}
	return removed, nil
}

// RefreshContextMenu points the item at Mayoi.exe at exe when it opens
// another one, as one Mayoi installed elsewhere added, and keeps label.
// It adds no item where there is none.
func RefreshContextMenu(exe string) {
	changed := false
	for _, m := range contextMenus {
		cmd, err := registry.OpenKey(registry.CURRENT_USER, m.key()+`\command`, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		current, _, err := cmd.GetStringValue("")
		cmd.Close()
		if err != nil || current == m.command(exe) {
			continue
		}
		label := ""
		if key, err := registry.OpenKey(registry.CURRENT_USER, m.key(), registry.QUERY_VALUE); err == nil {
			label, _, _ = key.GetStringValue("")
			key.Close()
		}
		if writeContextMenu(m, exe, label) == nil {
			changed = true
		}
	}
	if changed {
		notifyAssociations()
	}
}

// notifyAssociations tells Explorer that the menus changed.
func notifyAssociations() {
	const shcneAssocChanged = 0x08000000
	windows.NewLazySystemDLL("shell32.dll").NewProc("SHChangeNotify").Call(shcneAssocChanged, 0, 0, 0)
}
