//go:build !darwin && !linux

package main

import "errors"

// commandLine reports whether Mayoi was started as the mayoi command,
// which there is none of here.
func commandLine() bool { return false }

func startApp([]string) error { return errors.New("the mayoi command is not supported here") }

func refreshCLI() {}
