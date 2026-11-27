package main

import (
	"bytes"
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"sync/atomic"

	"mayoi/internal/remote"
	"mayoi/internal/workspace"
)

// grepQuery is a query of search in files, as rg or grep take it, to find
// on a host the files that may match.
type grepQuery struct {
	query            string
	matchCase, regex bool
}

// remoteReadBatch is how many files of a remote folder search reads at
// once.
const remoteReadBatch = 200

// searchRemote searches the files rels of a remote folder, as runSearch
// does those of this machine. rg, or else grep, on the host first finds
// the files that may match, unless pre is nil; those are read over the
// connection in batches, and searched here, as the files of this machine
// are, so that both find the same.
func (a *app) searchRemote(gen int, root string, re *regexp.Regexp, fold foldQuery, rels []string, texts map[string]string,
	cancel *atomic.Bool, partial bool, pre *grepQuery) {
	c, rroot, _ := remote.Lookup(root)
	if pre != nil {
		if hits, ok := grepFiles(c, rroot, pre); ok {
			var want []string
			for _, rel := range rels {
				if _, open := texts[rel]; open || hits[rel] {
					want = append(want, rel)
				}
			}
			rels = want
		}
	}
	found := 0
	stopped := func() bool { return found >= maxSearchMatches }
	take := func(batch []*searchFile, done bool) {
		if len(batch) > 0 || done {
			st := stopped()
			a.post(func() { a.takeResults(gen, batch, done && !partial, st) })
		}
	}
	search := func(rel string, text map[string]string) *searchFile {
		limit := maxSearchMatches - found
		if limit <= 0 && !partial {
			return nil
		}
		f := searchFileText(root, rel, text, re, fold, max(limit, 1), partial)
		if f != nil {
			found += len(f.matches)
		}
		return f
	}
	// The open documents, as they are, then the rest as they are read.
	var batch []*searchFile
	var toRead []string
	for _, rel := range rels {
		if _, ok := texts[rel]; ok {
			if f := search(rel, texts); f != nil {
				batch = append(batch, f)
			}
		} else {
			toRead = append(toRead, rel)
		}
	}
	take(batch, false)
	for i := 0; i < len(toRead) && !cancel.Load() && !stopped(); i += remoteReadBatch {
		part := toRead[i:min(i+remoteReadBatch, len(toRead))]
		got := map[string]string{}
		err := c.ReadFiles(rroot, part, maxSearchBytes, func(rel string, data []byte) {
			if !isBinary(data) {
				got[rel] = string(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))
			}
		})
		if err != nil {
			break
		}
		batch = nil
		for _, rel := range part {
			if cancel.Load() {
				break
			}
			if f := search(rel, got); f != nil {
				batch = append(batch, f)
			}
		}
		take(batch, false)
	}
	take(nil, true)
}

// grepFiles finds the files of a remote folder that may match a query,
// relative to it with slashes, with rg on its host, or else grep: false
// when it cannot say, as for an expression grep does not take. It looks
// for more than the query finds, never fewer: whole words are left to the
// search after.
func grepFiles(c *remote.Conn, root string, q *grepQuery) (map[string]bool, bool) {
	// An expression of several lines matches across them, which grep,
	// a line at a time, cannot see.
	if q.regex && (strings.Contains(q.query, `\n`) || strings.Contains(q.query, `\s`) || strings.Contains(q.query, `(?s`)) {
		return nil, false
	}
	var flags []string
	if !q.matchCase {
		flags = append(flags, "-i")
	}
	if !q.regex {
		flags = append(flags, "-F")
	}
	if c.Windows() {
		return grepWindows(c, root, flags, q)
	}
	var script string
	if c.Info().Rg {
		script = `cd -- "$1" || exit 2; shift
rg -l --no-messages --hidden -0 "$@" .`
		flags = append(flags, "-e", q.query)
	} else {
		if q.regex {
			flags = append(flags, "-E")
		}
		args := []string{"-rlI"}
		for _, d := range workspace.SkippedDirs() {
			args = append(args, "--exclude-dir="+d)
		}
		flags = append(append(args, flags...), "-e", q.query)
		script = `cd -- "$1" || exit 2; shift
grep "$@" .`
	}
	out, err := c.Run(nil, script, append([]string{root}, flags...)...)
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return map[string]bool{}, true // nothing matches
		}
		return nil, false
	}
	hits := map[string]bool{}
	sep := "\n"
	if c.Info().Rg {
		sep = "\x00"
	}
	for name := range strings.SplitSeq(string(out), sep) {
		if name = strings.TrimPrefix(name, "./"); name != "" {
			hits[name] = true
		}
	}
	return hits, true
}

// grepWindows is grepFiles on a Windows host, with rg when it has one:
// without, it cannot say.
func grepWindows(c *remote.Conn, root string, flags []string, q *grepQuery) (map[string]bool, bool) {
	if !c.Info().Rg {
		return nil, false
	}
	args := append([]string{"-l", "--no-messages", "--hidden", "-0"}, flags...)
	cmd, err := c.Exec(root, nil, nil, nil, "rg", append(args, "-e", q.query, ".")...)
	if err != nil {
		return nil, false
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return map[string]bool{}, true // nothing matches
		}
		return nil, false
	}
	hits := map[string]bool{}
	for name := range strings.SplitSeq(out.String(), "\x00") {
		name = strings.ReplaceAll(name, `\`, "/")
		if name = strings.TrimPrefix(name, "./"); name != "" {
			hits[name] = true
		}
	}
	return hits, true
}
