package workspace

import (
	"bytes"
	"io/fs"
	"mayoi/internal/remotefs"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// MaxIndexed is the most files the index holds.
const MaxIndexed = 200_000

// Index lists the files under root, relative to it with slashes. In a Git
// work tree it lists what Git does, leaving out the files it ignores, as
// build output; elsewhere it walks the folder, leaving out the
// directories that Skipped names. It runs in the caller's goroutine; call
// it from another.
func Index(root string) []string {
	if files, ok := gitFiles(root); ok {
		return files
	}
	if remotefs.IsRemote(root) {
		return remoteWalk(root)
	}
	return walk(root)
}

// gitFiles lists the files Git tracks under root and those it does not
// ignore, less those deleted, false when root is not in a work tree or
// Git is missing.
func gitFiles(root string) ([]string, bool) {
	list := func(args ...string) ([]byte, bool) {
		cmd, cancel := remotefs.GitCommand(root, append([]string{"ls-files", "-z"}, args...)...)
		defer cancel()
		out, err := cmd.Output()
		return out, err == nil
	}
	out, ok := list("--cached", "--others", "--exclude-standard", "--deduplicate")
	if !ok {
		return nil, false
	}
	deleted := map[string]bool{}
	if del, ok := list("--deleted"); ok {
		for name := range bytes.SplitSeq(del, []byte{0}) {
			deleted[string(name)] = true
		}
	}
	var files []string
	for name := range bytes.SplitSeq(out, []byte{0}) {
		if len(name) == 0 || deleted[string(name)] {
			continue
		}
		f := string(name)
		if Hidden(f[strings.LastIndexByte(f, '/')+1:]) {
			continue
		}
		files = append(files, f)
		if len(files) >= MaxIndexed {
			break
		}
	}
	return files, true
}

// walk lists the files under root by reading its directories.
func walk(root string) []string {
	var files []string
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() && path != root {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path != root && Skipped(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if Hidden(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err == nil {
			files = append(files, filepath.ToSlash(rel))
		}
		if len(files) >= MaxIndexed {
			return fs.SkipAll
		}
		return nil
	})
	return files
}

// Match is a file that a query matches: its index in the list matched,
// its score, and the byte offsets of its matched characters.
type Match struct {
	Index     int
	Score     int
	Positions []int
}

// Fuzzy finds the files that query matches, its characters in order,
// best first: matches in the file's name, at the starts of words, and of
// consecutive characters, rank above those scattered in its directories.
// It returns at most limit.
//
// It scores the files without allocating, in parallel for long lists,
// keeps only the best limit of them, and finds the positions of their
// matched characters last.
func Fuzzy(files []string, query string, limit int) []Match {
	query = strings.ToLower(strings.ReplaceAll(query, " ", ""))
	if query == "" {
		n := min(limit, len(files))
		out := make([]Match, n)
		for i := range n {
			out[i] = Match{Index: i}
		}
		return out
	}
	if limit <= 0 {
		return nil
	}
	q := []rune(query)
	better := func(a, b Match) bool {
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if la, lb := len(files[a.Index]), len(files[b.Index]); la != lb {
			return la < lb
		}
		return a.Index < b.Index
	}
	// best scores files[from:to], keeping the top limit in order.
	best := func(from, to int) []Match {
		top := make([]Match, 0, limit)
		pos := make([]int, len(q))
		for i := from; i < to; i++ {
			sc, ok := score(files[i], q, pos)
			if !ok {
				continue
			}
			m := Match{Index: i, Score: sc}
			if len(top) == limit && !better(m, top[limit-1]) {
				continue
			}
			at, _ := slices.BinarySearchFunc(top, m, func(e, t Match) int {
				if better(e, t) {
					return -1
				}
				return 1
			})
			if len(top) == limit {
				top = top[:limit-1]
			}
			top = slices.Insert(top, at, m)
		}
		return top
	}
	var out []Match
	const chunk = 8192
	if n := len(files); n <= chunk {
		out = best(0, n)
	} else {
		workers := min(runtime.GOMAXPROCS(0), (n+chunk-1)/chunk)
		parts := make([][]Match, workers)
		var wg sync.WaitGroup
		for w := range workers {
			from, to := n*w/workers, n*(w+1)/workers
			wg.Go(func() { parts[w] = best(from, to) })
		}
		wg.Wait()
		for _, part := range parts {
			out = append(out, part...)
		}
		slices.SortFunc(out, func(a, b Match) int {
			if better(a, b) {
				return -1
			}
			return 1
		})
		out = out[:min(len(out), limit)]
	}
	for i := range out {
		out[i].Positions = make([]int, len(q))
		score(files[out[i].Index], q, out[i].Positions)
	}
	return out
}

// score matches q in s, preferring the characters of the base name, then
// those at word starts and following one another. It puts the byte
// offsets of the characters matched in pos, which is as long as q.
func score(s string, q []rune, pos []int) (int, bool) {
	// Quickly reject: every rune of q, in order.
	j := 0
	for i := 0; i < len(s) && j < len(q); {
		r := rune(s[i])
		size := 1
		if r >= utf8.RuneSelf {
			r, size = utf8.DecodeRuneInString(s[i:])
		}
		if lower(r) == q[j] {
			j++
		}
		i += size
	}
	if j < len(q) {
		return 0, false
	}

	base := strings.LastIndexByte(s, '/') + 1
	// Match greedily from the end, so that the base name takes what it can,
	// then score the positions.
	k := len(q) - 1
	for i := len(s); i > 0 && k >= 0; {
		r, size := rune(s[i-1]), 1
		if r >= utf8.RuneSelf {
			r, size = utf8.DecodeLastRuneInString(s[:i])
		}
		i -= size
		if lower(r) == q[k] {
			pos[k] = i
			k--
		}
	}
	if k >= 0 {
		return 0, false
	}
	// Pull matches right after an earlier match next to it, keeping runs
	// together: "mai" in "src/main.go" takes m-a-i of main.
	total := 0
	for n, p := range pos {
		total += 10
		if p >= base {
			total += 8
		}
		if p == 0 || isBoundary(s, p) {
			total += 12
		}
		if n > 0 && pos[n-1]+utf8.RuneLen(q[n-1]) == p {
			total += 15
		}
		if n == 0 && p == base {
			total += 10
		}
	}
	total -= (len(s) - base) / 4 // shorter names first
	total -= strings.Count(s, "/")
	return total, true
}

// lower is unicode.ToLower, quick for ASCII.
func lower(r rune) rune {
	if r < utf8.RuneSelf {
		if 'A' <= r && r <= 'Z' {
			r += 'a' - 'A'
		}
		return r
	}
	return unicode.ToLower(r)
}

func isBoundary(s string, i int) bool {
	prev, _ := utf8.DecodeLastRuneInString(s[:i])
	cur, _ := utf8.DecodeRuneInString(s[i:])
	switch prev {
	case '/', '_', '-', '.', ' ':
		return true
	}
	return unicode.IsLower(prev) && unicode.IsUpper(cur)
}

// Ignored returns the absolute paths of the files and directories under
// root that Git ignores, a directory standing for everything in it; nil
// outside a work tree. It runs in the caller's goroutine; call it from
// another.
func Ignored(root string) map[string]bool {
	cmd, cancel := remotefs.GitCommand(root, "ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory", "--no-empty-directory")
	defer cancel()
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	ignored := map[string]bool{}
	for name := range bytes.SplitSeq(out, []byte{0}) {
		if len(name) > 0 {
			ignored[filepath.Join(root, filepath.FromSlash(strings.TrimSuffix(string(name), "/")))] = true
		}
	}
	return ignored
}

// remoteWalk skips the same generated folders as the local index.
func remoteWalk(root string) []string {
	var files []string
	var visit func(string)
	visit = func(dir string) {
		entries, err := remotefs.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if len(files) >= MaxIndexed {
				return
			}
			p := filepath.Join(dir, e.Name())
			if e.IsDir() {
				if !Skipped(e.Name()) {
					visit(p)
				}
				continue
			}
			if Hidden(e.Name()) || e.Type()&fs.ModeSymlink != 0 {
				continue
			}
			rel, _ := filepath.Rel(root, p)
			files = append(files, filepath.ToSlash(rel))
		}
	}
	visit(root)
	return files
}
