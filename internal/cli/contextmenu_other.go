//go:build !windows

package cli

import "errors"

// AddContextMenu fails outside Windows, which has no Explorer.
func AddContextMenu(string, string) error {
	return errors.New("Explorer's context menu is of Windows")
}

// RemoveContextMenu does nothing outside Windows.
func RemoveContextMenu() (bool, error) { return false, nil }

// RefreshContextMenu does nothing outside Windows.
func RefreshContextMenu(string) {}
