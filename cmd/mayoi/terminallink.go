package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// terminalFile opens a printed path without consulting the search index,
// so files Git ignores can be opened too. Relative paths are the shell's,
// falling back to the folder the terminal started in when it reports none.
func (a *app) terminalFile(s *session, link string) bool {
	if strings.Contains(link, "://") || strings.HasPrefix(link, "mailto:") {
		return false
	}
	dir := s.dir
	if s.term != nil {
		if reported := s.term.Dir(); filepath.IsAbs(reported) {
			dir = reported
		}
	}
	resolve := func(name string) string {
		if strings.HasPrefix(name, "~/") || strings.HasPrefix(name, `~\`) {
			name = filepath.Join(homeDir(), name[2:])
		}
		if !filepath.IsAbs(name) {
			name = filepath.Join(dir, name)
		}
		return filepath.Clean(name)
	}
	path := resolve(link)
	info, err := os.Stat(path)
	line, col := 0, 0
	if err != nil {
		// Prefer a literal filename if it exists, even one ending in :12.
		name, l, c, ok := cutLineSuffix(link)
		if !ok {
			return false
		}
		line, col = l, c
		path = resolve(name)
		info, err = os.Stat(path)
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
