//go:build !windows

package cli

// AddToUserPath does nothing outside Windows.
func AddToUserPath(string) error { return nil }

// RemoveFromUserPath does nothing outside Windows.
func RemoveFromUserPath(string) error { return nil }
