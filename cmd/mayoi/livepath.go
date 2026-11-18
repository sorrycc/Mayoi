package main

import (
	"path/filepath"
	"strings"

	"mayoi/internal/bg"
	"mayoi/internal/remote"
)

// livePathsIn are the bare paths of files in src[s:e], as byte offsets:
// /abs, ~/x, ./x and ../x at a word's start, up to a space or a bracket,
// without the marks that end a sentence after them. Whether each is a
// file is for the caller to find.
func livePathsIn(src []byte, s, e int) [][2]int {
	var paths [][2]int
	for i := s; i < e; i++ {
		c := src[i]
		if c != '/' && c != '~' && c != '.' || i > 0 && !livePathAfter(src[i-1]) {
			continue
		}
		rest := src[i:e]
		var lead int
		switch {
		case len(rest) > 1 && c == '/' && rest[1] != '/':
			lead = 1
		case hasPrefixBytes(rest, "~/"), hasPrefixBytes(rest, "./"):
			lead = 2
		case hasPrefixBytes(rest, "../"):
			lead = 3
		default:
			continue
		}
		j := i + lead
		for j < e && !livePathStop(src[j]) {
			j++
		}
		// A sentence's marks after it, as in "see /x/y.md." or "(at ./a)".
		for j > i+lead && strings.IndexByte(".,;:!?'\"", src[j-1]) >= 0 {
			j--
		}
		if j > i+lead {
			paths = append(paths, [2]int{i, j})
		}
		i = j
	}
	return paths
}

func hasPrefixBytes(b []byte, p string) bool {
	return len(b) >= len(p) && string(b[:len(p)]) == p
}

// livePathAfter reports whether a path may start after c: at a word's
// start, not inside a word, a URL or another path.
func livePathAfter(c byte) bool {
	return strings.IndexByte(" \t\n\r(\"'*_>", c) >= 0 || c >= 0x80
}

// livePathStop reports whether c ends a bare path.
func livePathStop(c byte) bool {
	return c <= ' ' || strings.IndexByte("<>()[]{}\"'`|*", c) >= 0
}

// livePathIn is the bare path of a line of text at rune at, as a click
// or ⌘-click there follows it, whether or not it is a file.
func livePathIn(line string, at int) (string, bool) {
	src := []byte(line)
	for _, p := range livePathsIn(src, 0, len(src)) {
		s, e := len([]rune(line[:p[0]])), len([]rune(line[:p[1]]))
		if at >= s && at <= e {
			return line[p[0]:p[1]], true
		}
	}
	return "", false
}

// livePathName is the file a bare path, or a link's destination, of the
// note at path names: ~/ in the home folder, a whole path on the note's
// machine, and ./ or ../ from the note's folder. "" for one it cannot
// find without asking, as ~/ of a remote note.
func livePathName(path, p string) string {
	switch {
	case strings.HasPrefix(p, "~/"):
		if remote.Is(path) {
			return ""
		}
		return filepath.Join(homeDir(), filepath.FromSlash(p[2:]))
	case strings.HasPrefix(p, "/"):
		return remote.Clean(remote.On(path, p))
	case path == "":
		return ""
	}
	return remote.Clean(remote.Join(remote.Dir(path), p))
}

// livePathFile is the file a bare path of the note at path names, and the
// line and column of a :line:col after it, "" when it names none. It
// asks the disk, or the remote machine: never on the UI thread.
func livePathFile(path, p string) (file string, line, col int) {
	if f := livePathName(path, p); f != "" && isFile(f) {
		return f, 0, 0
	}
	if rest, l, c, ok := cutLineSuffix(p); ok {
		if f := livePathName(path, rest); f != "" && isFile(f) {
			return f, l, c
		}
	}
	return "", 0, 0
}

// livePathKnown is whether a bare path of the note at path names a file,
// once for each in a parse; a remote note's are kept from parse to parse,
// for the machine to be asked once.
func (c *markdownCache) livePathKnown(path, p string) bool {
	k := [2]string{livePathDir(path), p}
	if ok, seen := c.paths[k]; seen {
		return ok
	}
	f, _, _ := livePathFile(path, p)
	if c.paths == nil {
		c.paths = map[[2]string]bool{}
	}
	c.paths[k] = f != ""
	return f != ""
}

// livePathDir is the folder a bare path of the note at path is found
// from.
func livePathDir(path string) string {
	if path == "" {
		return ""
	}
	return remote.Dir(path)
}

// remotePaths are the bare paths of remote notes paths knows, for the
// next parse to keep.
func remotePaths(paths map[[2]string]bool) map[[2]string]bool {
	var kept map[[2]string]bool
	for k, ok := range paths {
		if remote.Is(k[0]) {
			if kept == nil {
				kept = map[[2]string]bool{}
			}
			kept[k] = ok
		}
	}
	return kept
}

// openLivePath opens the file a bare path of an editor's note names, at
// its :line:col, in a tab; nothing for one that names none.
func (a *app) openLivePath(d *editor, p string) {
	path := d.path
	bg.Go(func() {
		defer recoverCrash()
		file, line, col := livePathFile(path, p)
		if file == "" {
			return
		}
		a.post(func() { a.openPathAt(file, line, col) })
	})
}

// openPathAt opens file in a tab, at line and col when line is set.
func (a *app) openPathAt(file string, line, col int) {
	a.openFile(file, true)
	if d := a.activeEditor(); line > 0 && d != nil && d.path == file {
		a.goToLine(line, col)
	}
}
