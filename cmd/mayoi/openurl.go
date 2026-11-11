package main

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"mayoi/internal/cli"
)

// openURLTarget reads the file, line and column of a mayoi:// URL:
//
//	mayoi://file/Users/me/app/src/App.tsx:12:5
//	mayoi://open?file=/Users/me/app/src/App.tsx&line=12&column=5
//
// The first is what LocatorJS's Custom link opens.
func openURLTarget(raw string) (cli.Target, bool) {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "mayoi") {
		return cli.Target{}, false
	}
	switch strings.ToLower(u.Host) {
	case "file":
		at, line, col, _ := cutLineSuffix(u.Path)
		return urlTarget(at, line, col)
	case "open":
		q := u.Query()
		line, col, ok := urlLineColumn(q.Get("line"), q.Get("column"))
		if !ok {
			return cli.Target{}, false
		}
		return urlTarget(q.Get("file"), line, col)
	}
	return cli.Target{}, false
}

// urlTarget is the file a URL names, absolute, at a line and column.
func urlTarget(path string, line, col int) (cli.Target, bool) {
	path = filepath.FromSlash(path)
	if runtime.GOOS == "windows" {
		// A URL path starts with the slash before C:/, which a Windows
		// path has not.
		path = strings.TrimPrefix(path, `\`)
	}
	if path == "" || !filepath.IsAbs(path) {
		return cli.Target{}, false
	}
	return cli.Target{Path: filepath.Clean(path), Line: line, Col: col}, true
}

// urlLineColumn reads a URL's line and column, 1-based, and 0 when not
// given.
func urlLineColumn(line, column string) (int, int, bool) {
	l, c := 0, 0
	if line != "" {
		n, err := strconv.Atoi(line)
		if err != nil || n < 1 {
			return 0, 0, false
		}
		l = n
	}
	if column != "" {
		n, err := strconv.Atoi(column)
		if err != nil || n < 0 {
			return 0, 0, false
		}
		c = n
	}
	return l, c, true
}
