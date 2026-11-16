package main

import (
	"path/filepath"
	"strconv"
	"strings"

	"mayoi/internal/remote"
	"mayoi/internal/vfs"
)

// shellDir is the folder a terminal's shell is in. A slash path the shell
// of a remote folder reports stays on that host or distribution.
func shellDir(started, reported string) string {
	if reported == "" {
		return started
	}
	if remote.Is(reported) {
		return remote.Clean(reported)
	}
	if drive, ok := windowsDrivePath(reported); ok {
		return drive
	}
	if remote.Is(started) && strings.HasPrefix(filepath.ToSlash(reported), "/") {
		return remote.On(started, filepath.ToSlash(reported))
	}
	if remote.IsAbs(reported) {
		return reported
	}
	return started
}

// windowsDrivePath is a file URL's path on Windows, /C:/dir, as the
// shell's directory. filepath does not treat that form as absolute.
func windowsDrivePath(p string) (string, bool) {
	if len(p) < 4 || p[0] != '/' || p[2] != ':' {
		return "", false
	}
	c := p[1]
	if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
		return "", false
	}
	return filepath.Clean(p[1:]), true
}

// terminalFile opens a printed path without consulting the search index,
// so files Git ignores can be opened too. Relative paths are the shell's,
// falling back to the folder the terminal started in when it reports none.
func (a *app) terminalFile(s *session, link string) bool {
	if strings.Contains(link, "://") || strings.HasPrefix(link, "mailto:") {
		return false
	}
	dir := s.dir
	if s.term != nil {
		dir = shellDir(s.dir, s.term.Dir())
	}
	resolve := func(name string) string {
		if strings.HasPrefix(name, "~/") || strings.HasPrefix(name, `~\`) {
			name = filepath.Join(homeDir(), name[2:])
		}
		if !remote.IsAbs(name) {
			name = remote.Join(dir, name)
		}
		return remote.Clean(name)
	}
	path := resolve(link)
	info, err := vfs.Stat(path)
	line, col := 0, 0
	if err != nil {
		// Prefer a literal filename if it exists, even one ending in :12.
		name, l, c, ok := cutLineSuffix(link)
		if !ok {
			return false
		}
		line, col = l, c
		path = resolve(name)
		info, err = vfs.Stat(path)
	}
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	a.openFile(path, true)
	if d := a.activeEditor(); line > 0 && d != nil && d.path == path {
		a.goToLine(line, col)
	}
	return true
}

// cutLineSuffix peels :line or :line:column, both 1-based, off the end of
// name. A column of 0, which some tools print, goes to the line.
func cutLineSuffix(name string) (rest string, line, col int, ok bool) {
	at := strings.LastIndexByte(name, ':')
	if at < 0 {
		return name, 0, 0, false
	}
	line, err := strconv.Atoi(name[at+1:])
	if err != nil || line < 0 {
		return name, 0, 0, false
	}
	rest = name[:at]
	if at = strings.LastIndexByte(rest, ':'); at >= 0 {
		if n, err := strconv.Atoi(rest[at+1:]); err == nil && n > 0 {
			line, col, rest = n, line, rest[:at]
		}
	}
	return rest, line, col, line >= 1
}
