package workspace

import (
	"path"
	"strings"
)

// Globs is a list of patterns of paths, as typed in search's "files to
// include" and "files to exclude": separated by commas, with `*` and `?`
// within a name, `**` for any folders, and `{a,b}` for either. A pattern
// without a slash matches a name anywhere, as `*.go` or `node_modules`; one
// with a slash matches from the root, as `internal/**` or `web/*.css`. A
// pattern that matches a folder matches what is in it.
type Globs [][]string // each pattern, its braces expanded, split at slashes

// ParseGlobs reads a list of patterns, nil for none.
func ParseGlobs(list string) Globs {
	var g Globs
	for _, pat := range splitTop(list) {
		pat = strings.TrimSpace(pat)
		pat = strings.TrimPrefix(pat, "./")
		pat = strings.TrimSuffix(pat, "/")
		if pat == "" {
			continue
		}
		for _, p := range expandBraces(pat) {
			if anchored := strings.HasPrefix(p, "/"); anchored {
				p = strings.TrimLeft(p, "/")
			} else if !strings.Contains(p, "/") {
				p = "**/" + p
			}
			g = append(g, strings.Split(p, "/"))
		}
	}
	return g
}

// Match reports whether rel, a path relative to the root with slashes,
// or a folder above it, matches any of the patterns.
func (g Globs) Match(rel string) bool {
	if len(g) == 0 {
		return false
	}
	segs := strings.Split(rel, "/")
	for _, pat := range g {
		// The file, or a folder above it.
		for n := len(segs); n > 0; n-- {
			if matchSegs(pat, segs[:n]) {
				return true
			}
		}
	}
	return false
}

// MatchPath matches only the path itself, as file watcher patterns do.
func (g Globs) MatchPath(rel string) bool {
	segs := strings.Split(rel, "/")
	for _, pat := range g {
		if matchSegs(pat, segs) {
			return true
		}
	}
	return false
}

// matchSegs matches the segments of a pattern to those of a path, `**`
// taking any number of them.
func matchSegs(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for len(pat) > 1 && pat[1] == "**" {
				pat = pat[1:]
			}
			if len(pat) == 1 {
				return true
			}
			for i := range len(segs) + 1 {
				if matchSegs(pat[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, err := path.Match(pat[0], segs[0]); !ok || err != nil {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

// splitTop splits a list at the commas outside braces.
func splitTop(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth = max(0, depth-1)
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// expandBraces makes a pattern of each choice of its braces: `*.{ts,tsx}`
// gives `*.ts` and `*.tsx`.
func expandBraces(p string) []string {
	open := strings.IndexByte(p, '{')
	if open < 0 {
		return []string{p}
	}
	depth := 0
	for i := open; i < len(p); i++ {
		switch p[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth > 0 {
				continue
			}
			var out []string
			for _, choice := range splitTop(p[open+1 : i]) {
				out = append(out, expandBraces(p[:open]+choice+p[i+1:])...)
			}
			return out
		}
	}
	return []string{p} // unclosed: as typed
}
