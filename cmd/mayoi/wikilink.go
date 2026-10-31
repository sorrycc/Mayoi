package main

import (
	"io/fs"
	"net/url"
	pathpkg "path"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"mayoi/internal/bg"
	"mayoi/internal/remote"
	"mayoi/internal/vfs"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// wikiLink is a link of Obsidian's to a note, [[target#heading|label]],
// or with embed, ![[target]], the note or picture shown in place.
type wikiLink struct {
	target, heading, label string
	embed                  bool
}

// text is what a wiki link shows: its label, or else its target and
// heading, as note > heading.
func (l wikiLink) text() string {
	switch {
	case l.label != "":
		return l.label
	case l.heading == "":
		return l.target
	case l.target == "":
		return l.heading
	}
	return l.target + " > " + l.heading
}

// wikiLinkAt reads the wiki link s opens with, and the bytes it takes, 0
// for none. It is on one line, without brackets between its own; in a
// table, the | before its label is escaped, as \|.
func wikiLinkAt(s string) (wikiLink, int) {
	var l wikiLink
	i := 0
	if strings.HasPrefix(s, "!") {
		l.embed, i = true, 1
	}
	if !strings.HasPrefix(s[i:], "[[") {
		return l, 0
	}
	end := strings.Index(s[i+2:], "]]")
	if end < 0 {
		return l, 0
	}
	body := s[i+2 : i+2+end]
	if strings.ContainsAny(body, "[]\r\n") {
		return l, 0
	}
	body = strings.ReplaceAll(body, `\|`, "|")
	target, label, _ := strings.Cut(body, "|")
	target, heading, _ := strings.Cut(target, "#")
	l.target, l.heading, l.label = strings.TrimSpace(target), strings.TrimSpace(heading), strings.TrimSpace(label)
	if l.target == "" && l.heading == "" {
		return l, 0
	}
	return l, i + 2 + end + 2
}

// wikiLinkIn is the wiki link of line that the rune at stands in or
// against, as the caret a click put there.
func wikiLinkIn(line string, at int) (wikiLink, bool) {
	r := 0
	for i := 0; i < len(line); {
		if line[i] == '[' || line[i] == '!' {
			if l, n := wikiLinkAt(line[i:]); n > 0 {
				end := r + utf8.RuneCountInString(line[i:i+n])
				if at >= r && at <= end {
					return l, true
				}
				i, r = i+n, end
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(line[i:])
		i += size
		r++
	}
	return wikiLink{}, false
}

// wikiLinkNode is a wiki link in the text of a note.
type wikiLinkNode struct {
	ast.BaseInline
	wikiLink
}

var kindWikiLink = ast.NewNodeKind("WikiLink")

func (n *wikiLinkNode) Kind() ast.NodeKind { return kindWikiLink }

func (n *wikiLinkNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Target": n.target, "Heading": n.heading, "Label": n.label}, nil)
}

// wikiLinkParser parses the wiki links of text, before goldmark's links
// take their brackets.
type wikiLinkParser struct{}

func (wikiLinkParser) Trigger() []byte { return []byte{'[', '!'} }

func (wikiLinkParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	l, n := wikiLinkAt(string(line))
	if n == 0 {
		return nil
	}
	block.Advance(n)
	return &wikiLinkNode{wikiLink: l}
}

// wikiLinkFile is the file a wiki link's target names, as Obsidian finds
// it: next to the note at path, at root, or else the shortest path under
// root that ends in it, whatever its case; "" for none. A target without
// an extension is a note, of .md.
func wikiLinkFile(root, path, target string) string {
	if target == "" {
		return ""
	}
	names := []string{target + ".md"}
	if filepath.Ext(target) != "" {
		names = []string{target, target + ".md"}
	}
	for _, name := range names {
		for _, dir := range []string{filepath.Dir(path), root} {
			if dir == "" {
				continue
			}
			if p := filepath.Join(dir, filepath.FromSlash(name)); isFile(p) {
				return p
			}
		}
	}
	if root == "" || remote.Is(root) {
		return ""
	}
	byName := wikiFiles(root)
	for _, name := range names {
		want := strings.ToLower(filepath.ToSlash(name))
		best := ""
		for _, f := range byName[pathpkg.Base(want)] {
			if lf := strings.ToLower(f); lf != want && !strings.HasSuffix(lf, "/"+want) {
				continue
			}
			if best == "" || len(f) < len(best) || len(f) == len(best) && f < best {
				best = f
			}
		}
		if p := filepath.Join(root, filepath.FromSlash(best)); best != "" && isFile(p) {
			return p
		}
	}
	return ""
}

func isFile(p string) bool {
	info, err := vfs.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// wikiLinkDest is the destination of a link from the note at path to
// file at heading, as a link of Markdown has it: relative, or to #heading
// of the note itself without a file.
func wikiLinkDest(path, file, heading string) string {
	if file == "" {
		return "#" + markdownSlug(heading)
	}
	rel, err := filepath.Rel(filepath.Dir(path), file)
	if err != nil {
		rel = file
	}
	u := url.URL{Path: filepath.ToSlash(rel), Fragment: heading}
	return u.String()
}

// wikiIndex is the files under a folder, by their names in lower case,
// as paths from it with slashes, and when it was read.
type wikiIndex struct {
	at     time.Time
	byName map[string][]string
}

// wikiIndexes are the folders read for wiki links, by their paths, read
// again once wikiIndexAge old. A folder holds up to wikiIndexMax files,
// the rest unread.
var wikiIndexes struct {
	sync.Mutex
	m map[string]wikiIndex
}

const (
	wikiIndexAge = 10 * time.Second
	wikiIndexMax = 200_000
)

// wikiFiles is the files under root by their names in lower case, read
// again when the last read is old. Hidden folders and node_modules are
// left out.
func wikiFiles(root string) map[string][]string {
	wikiIndexes.Lock()
	ix, ok := wikiIndexes.m[root]
	wikiIndexes.Unlock()
	if ok && time.Since(ix.at) < wikiIndexAge {
		return ix.byName
	}
	ix = wikiIndex{at: time.Now(), byName: map[string][]string{}}
	n := 0
	_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := e.Name()
		if e.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if n++; n > wikiIndexMax {
			return filepath.SkipAll
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		k := strings.ToLower(name)
		ix.byName[k] = append(ix.byName[k], filepath.ToSlash(rel))
		return nil
	})
	wikiIndexes.Lock()
	if wikiIndexes.m == nil {
		wikiIndexes.m = map[string]wikiIndex{}
	}
	wikiIndexes.m[root] = ix
	wikiIndexes.Unlock()
	return ix.byName
}

// followWikiLink opens the note of the wiki link the caret is in, as a
// Cmd-click on one does in the text of a note.
func (a *app) followWikiLink(d *editor) {
	snap := d.buf.Snapshot()
	caret := min(d.sel[1], snap.Len())
	s, e := snap.LineRange(snap.LineAt(caret))
	l, ok := wikiLinkIn(snap.Slice(s, e), caret-s)
	if !ok {
		return
	}
	path, workspace := d.path, a.folder()
	if l.target == "" {
		a.openMarkdownAt(path, l.heading)
		return
	}
	// Finding it may read the folders, away from the UI thread.
	bg.Go(func() {
		defer recoverCrash()
		file := wikiLinkFile(markdownRoot(path, workspace), path, l.target)
		a.post(func() {
			switch {
			case file == "":
			case l.heading != "" && isMarkdownPath(file):
				a.openMarkdownAt(file, l.heading)
			default:
				a.openFile(file, true)
			}
		})
	})
}
