//go:build !windows

package cli

import "errors"

// AddContextMenu fails outside Windows, which has no Explorer.
func AddContextMenu(string, string) error {
	return errors.New("the Explorer context menu is only on Windows")
}

// RemoveContextMenu does nothing outside Windows.
func RemoveContextMenu() (bool, error) { return false, nil }

// RefreshContextMenu does nothing outside Windows.
func RefreshContextMenu(string) {}
