package main

import "os"

// commandLine reports whether Mayoi was started as the mayoi command,
// which on Linux is Mayoi itself: with arguments, unless detach or a
// snapshot started it.
func commandLine() bool {
	return os.Getenv(cliEnv) != "" || len(os.Args) > 1 && os.Getenv(detachedEnv) == "" && os.Getenv("MAYOI_SNAPSHOT") == ""
}

// startApp starts Mayoi apart from the command, opening paths.
func startApp(paths []string) error { return startDetached(paths, detachedEnv+"=1") }

// refreshCLI does nothing: the command is Mayoi itself.
func refreshCLI() {}
