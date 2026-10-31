//go:build !unix

package main

// keepStderr keeps stderr on Unix only: Windows apps have none.
func keepStderr(dir string) {}
