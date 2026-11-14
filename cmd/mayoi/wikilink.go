package main

import (
	"net/url"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"mayoi/internal/bg"
	"mayoi/internal/remote"
	"mayoi/internal/vfs"
	"mayoi/internal/workspace"
	"mayoi/locales"

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
// heading, as note > heading, and a heading under another as A > B.
func (l wikiLink) text() string {
	heading := strings.ReplaceAll(l.heading, "#", " > ")
	switch {
	case l.label != "":
		return l.label
	case l.heading == "":
		return l.target
	case l.target == "":
		return heading
	}
	return l.target + " > " + heading
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
	// start and stop are where the link and its brackets are in the
	// source, which Live Preview hides them by.
	start, stop int
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
	line, seg := block.PeekLine()
	l, n := wikiLinkAt(string(line))
	if n == 0 {
		return nil
	}
	block.Advance(n)
	return &wikiLinkNode{wikiLink: l, start: seg.Start, stop: seg.Start + n}
}

// wikiLinkFile is the file a wiki link's target names, as Obsidian finds
// it, whatever its case: at its path from the note at path, or from
// root, or else the file under root whose path ends in it, the shortest
// path first; "" for none. A target without an extension is a note, of
// .md. The files under root are those of its index, which calls arrived
// once a read of it in the background finds them changed. Without a
// root, only the note's folder is looked in.
func wikiLinkFile(root, path, target string, arrived func()) string {
	if target == "" {
		return ""
	}
	names := []string{target + ".md"}
	if filepath.Ext(target) != "" {
		names = []string{target, target + ".md"}
	}
	ix := wikiFiles(root, arrived)
	if ix == nil || len(ix.files) == 0 {
		for _, name := range names {
			if p := filepath.Join(filepath.Dir(path), filepath.FromSlash(name)); isFile(p) {
				return p
			}
		}
		return ""
	}
	dir, inRoot := "", false
	if rel, err := filepath.Rel(root, filepath.Dir(path)); err == nil && filepath.IsLocal(rel) {
		dir, inRoot = strings.ToLower(filepath.ToSlash(rel)), true
	}
	found := func(f string) string { return filepath.Join(root, filepath.FromSlash(f)) }
	for _, name := range names {
		want := strings.ToLower(pathpkg.Clean(filepath.ToSlash(name)))
		if inRoot {
			if f, ok := ix.byPath[pathpkg.Join(dir, want)]; ok {
				return found(f)
			}
		}
		if f, ok := ix.byPath[want]; ok {
			return found(f)
		}
		best := ""
		for _, f := range ix.byName[pathpkg.Base(want)] {
			if !strings.HasSuffix(strings.ToLower(f), "/"+want) {
				continue
			}
			if best == "" || len(f) < len(best) || len(f) == len(best) && f < best {
				best = f
			}
		}
		if best != "" {
			return found(best)
		}
	}
	return ""
}

// wikiLinkFile is the file a wiki link's target names, as the package
// function finds it, once for each folder and target in a parse.
func (c *markdownCache) wikiLinkFile(root, path, target string) string {
	if c == nil {
		return wikiLinkFile(root, path, target, nil)
	}
	k := [3]string{root, filepath.Dir(path), target}
	if f, ok := c.links[k]; ok {
		return f
	}
	f := wikiLinkFile(root, path, target, c.arrived)
	if c.links == nil {
		c.links = map[[3]string]string{}
	}
	c.links[k] = f
	return f
}

func isFile(p string) bool {
	info, err := vfs.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// wikiLinkDest is the destination of a link from the note at path to
// file at heading, as a link of Markdown has it: relative, or to #heading
// of the note itself without a file. A heading under another, A#B, or a
// block, ^id, of the note itself is a link to the note, which finds it.
func wikiLinkDest(path, file, heading string) string {
	if file == "" && (path == "" || !strings.ContainsAny(heading, "#^")) {
		return "#" + markdownSlug(heading)
	}
	if file == "" {
		file = path
	}
	rel, err := filepath.Rel(filepath.Dir(path), file)
	if err != nil {
		rel = file
	}
	u := url.URL{Path: filepath.ToSlash(rel), Fragment: heading}
	return u.String()
}

// wikiFileSet is the files under a folder, as paths from it with
// slashes, but those in hidden folders: by their names in lower case, by
// their paths in lower case, and in order.
type wikiFileSet struct {
	byName map[string][]string
	byPath map[string]string
	files  []string
}

// wikiIndex is the files under a folder as last read, when, whether a
// read is under way, and what to call should it find them changed. ready
// closes once the first read is done.
type wikiIndex struct {
	set     *wikiFileSet
	at      time.Time
	reading bool
	ready   chan struct{}
	arrived []func()
}

// wikiIndexes are the folders read for wiki links, by their paths. A
// folder is read again in the background once wikiIndexAge old, or
// remoteIndexEvery for one on a host, its last read serving meanwhile.
var wikiIndexes struct {
	sync.Mutex
	m map[string]*wikiIndex
}

const wikiIndexAge = 10 * time.Second

// wikiFiles is the files under root, nil without one. The first read
// waits for the files; later ones take those last read, and read them
// again in the background when old, calling arrived if they changed.
func wikiFiles(root string, arrived func()) *wikiFileSet {
	if root == "" {
		return nil
	}
	wikiIndexes.Lock()
	if wikiIndexes.m == nil {
		wikiIndexes.m = map[string]*wikiIndex{}
	}
	ix := wikiIndexes.m[root]
	if ix == nil {
		ix = &wikiIndex{ready: make(chan struct{}), reading: true}
		wikiIndexes.m[root] = ix
		wikiIndexes.Unlock()
		set := readWikiFiles(root)
		wikiIndexes.Lock()
		ix.set, ix.at, ix.reading = set, time.Now(), false
		close(ix.ready)
		wikiIndexes.Unlock()
		return set
	}
	wikiIndexes.Unlock()
	<-ix.ready
	wikiIndexes.Lock()
	defer wikiIndexes.Unlock()
	age := wikiIndexAge
	if remote.Is(root) {
		age = remoteIndexEvery
	}
	if !ix.reading && time.Since(ix.at) >= age {
		ix.reading = true
		bg.Go(func() { readWikiFilesAgain(root, ix) })
	}
	if ix.reading && arrived != nil {
		ix.arrived = append(ix.arrived, arrived)
	}
	return ix.set
}

// readWikiFilesAgain reads the files of index ix of root again, and
// calls those waiting if they changed. A host that lists none, as one
// that cannot be reached, keeps those read before.
func readWikiFilesAgain(root string, ix *wikiIndex) {
	defer recoverCrash()
	var set *wikiFileSet
	defer func() {
		wikiIndexes.Lock()
		changed := set != nil && !slices.Equal(set.files, ix.set.files) && (len(set.files) > 0 || !remote.Is(root))
		if changed {
			ix.set = set
		}
		ix.at, ix.reading = time.Now(), false
		calls := ix.arrived
		ix.arrived = nil
		wikiIndexes.Unlock()
		if changed {
			for _, f := range calls {
				f()
			}
		}
	}()
	set = readWikiFiles(root)
}

// readWikiFiles reads the files under root, leaving out hidden folders
// and those the index of the workspace leaves out.
func readWikiFiles(root string) *wikiFileSet {
	set := &wikiFileSet{byName: map[string][]string{}, byPath: map[string]string{}}
	for _, f := range workspace.Walk(root) {
		if wikiHidden(f) {
			continue
		}
		lf := strings.ToLower(f)
		set.byName[pathpkg.Base(lf)] = append(set.byName[pathpkg.Base(lf)], f)
		set.byPath[lf] = f
		set.files = append(set.files, f)
	}
	slices.Sort(set.files)
	return set
}

// wikiHidden reports whether the file at rel, a path with slashes, is in
// a hidden folder, as .obsidian or .trash, which links do not find.
func wikiHidden(rel string) bool {
	dir, _, ok := strings.Cut(rel, "/")
	for ok {
		if strings.HasPrefix(dir, ".") {
			return true
		}
		rel = rel[len(dir)+1:]
		dir, _, ok = strings.Cut(rel, "/")
	}
	return false
}

// forgetWikiFiles drops the files read under root, for the next link to
// read them anew.
func forgetWikiFiles(root string) {
	wikiIndexes.Lock()
	delete(wikiIndexes.m, root)
	wikiIndexes.Unlock()
}

// wikiFilesChanged marks old the folders read for wiki links that may
// have a file made or removed among changed and removed, as the watch of
// the workspace reports them: one they do not hold changed, or any
// removed, which may be a folder. It reports whether any did.
func wikiFilesChanged(changed, removed []string) bool {
	wikiIndexes.Lock()
	defer wikiIndexes.Unlock()
	any := false
	for root, ix := range wikiIndexes.m {
		if ix.set == nil {
			continue
		}
		old := false
		for i, p := range append(slices.Clone(changed), removed...) {
			rel, err := filepath.Rel(root, p)
			if err != nil || !filepath.IsLocal(rel) || wikiHidden(filepath.ToSlash(rel)) {
				continue
			}
			if _, ok := ix.set.byPath[strings.ToLower(filepath.ToSlash(rel))]; !ok || i >= len(changed) {
				old = true
				break
			}
		}
		if old {
			ix.at = time.Time{}
			any = true
		}
	}
	return any
}

// followWikiLink opens the note of the wiki link the caret is in, as a
// Cmd-click on one does in the text of a note.
func (a *app) followWikiLink(d *editor) {
	snap := d.buf.Snapshot()
	caret := min(d.sel[1], snap.Len())
	s, e := snap.LineRange(snap.LineAt(caret))
	if l, ok := wikiLinkIn(snap.Slice(s, e), caret-s); ok {
		a.openWikiLink(d.path, l)
	}
}

// openWikiLink opens what a wiki link of the note at path links to: a
// heading or block of its own, or another file, at its heading or block.
// As in Obsidian, a note that is not there yet is made, beside the note.
func (a *app) openWikiLink(path string, l wikiLink) {
	if l.target == "" {
		a.openMarkdownAt(path, l.heading)
		return
	}
	workspace := a.folder()
	// Finding it may read the folders, away from the UI thread.
	bg.Go(func() {
		defer recoverCrash()
		root := markdownRoot(path, workspace)
		file := wikiLinkFile(root, path, l.target, nil)
		made := false
		var err error
		if file == "" {
			if file = wikiLinkNew(path, l.target); file != "" {
				if err = vfs.Create(file); err == nil {
					made = true
					forgetWikiFiles(root)
				}
			}
		}
		a.post(func() {
			switch {
			case err != nil:
				a.fail("%s", locales.FailMake(filepath.Base(file), err.Error()))
			case file == "":
			case made:
				if a.tree != nil {
					a.tree.Refresh()
				}
				a.indexRead.Invalidate()
				a.indexed = time.Time{}
				a.openFile(file, true)
			case l.heading != "" && isMarkdownPath(file):
				a.openMarkdownAt(file, l.heading)
			default:
				a.openFile(file, true)
			}
		})
	})
}

// wikiLinkNew is the note a wiki link to one that is not there makes:
// target.md beside the note at path, "" for a target that names a picture
// or leaves the note's folder.
func wikiLinkNew(path, target string) string {
	if path == "" || target == "" || isImage(target) {
		return ""
	}
	if !isMarkdownPath(target) {
		target += ".md"
	}
	rel := filepath.FromSlash(target)
	if filepath.IsAbs(rel) || !filepath.IsLocal(rel) {
		return ""
	}
	return filepath.Join(filepath.Dir(path), rel)
}
