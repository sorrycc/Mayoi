package main

import (
	"cmp"
	"html"
	"maps"
	"mayoi/internal/remotefs"
	"net/url"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"mayoi/internal/highlight"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/egoist/mygo/ui"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// markdownPreview keeps parsed blocks and the pane's place between edits.
type markdownPreview struct {
	blocks     []markdownBlock
	images     map[markdownImageKey]markdownInline
	root       string
	measure    float32
	measured   ui.Font
	list       ui.ListState
	scroll     ui.ScrollState
	observed   uint64
	path       string
	pending    bool
	running    bool
	generation atomic.Uint64
}

type markdownInline struct {
	text, link, image               string
	bold, italic, strike, code, raw bool
	bitmap                          ui.ImageSource
	w, h                            float32
	kind                            highlight.Kind
}

type markdownKind uint8

const (
	markdownParagraph markdownKind = iota
	markdownHeading
	markdownRule
	markdownCodeBlock
	markdownHTML
	markdownImageBlock
	markdownTable
	markdownTableHeader
)

type markdownImageKey struct {
	path  string
	mtime time.Time
	size  int64
}

// markdownBlock is a paragraph-sized row, including its enclosing lists and quotes.
type markdownBlock struct {
	kind                 markdownKind
	offset, markerWidth  float32
	tight                bool
	level, indent, quote int
	marker, language     string
	runs                 []markdownInline
	cells                [][]markdownInline
	aligns               []east.Alignment
}

func isMarkdown(d *document) bool {
	ext := filepath.Ext(d.path)
	return strings.EqualFold(ext, ".md") || strings.EqualFold(ext, ".markdown")
}

func canPreview(d *document) bool { return isSVG(d) || isMarkdown(d) }

// markdownPath resolves destinations on the document's filesystem, without fetching HTTP resources.
// A leading / means root, as it means the repository's on GitHub, or the
// filesystem's when there is no root.
func markdownPath(root, path, dest string) string {
	u, err := url.Parse(dest)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path == "" {
		return ""
	}
	if target, remotePath, ok := remotefs.Split(path); ok {
		if pathpkg.IsAbs(u.Path) {
			base := "/"
			if rootTarget, remoteRoot, valid := remotefs.Split(root); valid && rootTarget == target {
				base = remoteRoot
			}
			return target.Virtual(pathpkg.Join(base, u.Path))
		}
		return target.Virtual(pathpkg.Join(pathpkg.Dir(remotePath), u.Path))
	}
	if filepath.IsAbs(u.Path) {
		return filepath.Join(cmp.Or(root, "/"), filepath.FromSlash(u.Path))
	}
	return filepath.Join(filepath.Dir(path), filepath.FromSlash(u.Path))
}

// markdownRoot is the Git repository holding path, or else the workspace
// when it holds path, or "".
func markdownRoot(path, workspace string) string {
	boundary := ""
	if target, _, ok := remotefs.Split(path); ok {
		boundary = target.Virtual("/")
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if _, err := remotefs.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		if parent := filepath.Dir(dir); parent == dir || dir == boundary {
			break
		}
	}
	if rel, err := filepath.Rel(workspace, path); workspace != "" && err == nil && filepath.IsLocal(rel) {
		return workspace
	}
	return boundary
}

// markdownFrontMatter reports whether lines, those between front matter's
// delimiters, look like YAML, so a document opening with a rule keeps the
// text up to its next rule.
func markdownFrontMatter(lines []string) bool {
	keys := false
	for _, line := range lines {
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || line == "-" || strings.HasPrefix(line, "- "):
			if !keys {
				return false
			}
		default:
			key, rest, ok := strings.Cut(line, ":")
			if !ok || strings.TrimSpace(key) == "" || rest != "" && rest[0] != ' ' && rest[0] != '\t' {
				return false
			}
			keys = true
		}
	}
	return keys
}

// markdownParser parses GitHub's Markdown: goldmark's parser with the
// parts of its GFM extension that parse, without its HTML renderer, which
// the preview has no use for and would make Mayoi larger.
func markdownParser() parser.Parser {
	return parser.NewParser(
		parser.WithBlockParsers(parser.DefaultBlockParsers()...),
		parser.WithInlineParsers(append(parser.DefaultInlineParsers(),
			util.Prioritized(extension.NewStrikethroughParser(), 500),
			util.Prioritized(extension.NewTaskCheckBoxParser(), 0),
			util.Prioritized(extension.NewLinkifyParser(), 999))...),
		parser.WithParagraphTransformers(append(parser.DefaultParagraphTransformers(),
			util.Prioritized(extension.NewTableParagraphTransformer(), 200))...),
		parser.WithASTTransformers(util.Prioritized(extension.NewTableASTTransformer(), 0)),
	)
}

// parseMarkdown converts the AST once, away from the UI thread.
// A parse that current reports superseded keeps the images it skipped cached.
func parseMarkdown(source []byte, path, root string, current func() bool, caches ...map[markdownImageKey]markdownInline) []markdownBlock {
	cache := map[markdownImageKey]markdownInline{}
	if len(caches) > 0 && caches[0] != nil {
		cache = caches[0]
	}
	used := map[markdownImageKey]bool{}
	defer func() {
		if current != nil && !current() {
			return
		}
		for k := range cache {
			if !used[k] {
				delete(cache, k)
			}
		}
	}()
	// Front matter is recognized only at the start, with exact delimiter lines
	// around what looks like YAML.
	if lines := strings.SplitAfter(string(source), "\n"); strings.TrimSuffix(strings.TrimSuffix(lines[0], "\n"), "\r") == "---" {
		for i := 1; i < len(lines); i++ {
			line := strings.TrimSuffix(strings.TrimSuffix(lines[i], "\n"), "\r")
			if line == "---" || line == "..." {
				if markdownFrontMatter(lines[1:i]) {
					source = []byte(strings.Join(lines[i+1:], ""))
				}
				break
			}
		}
	}

	tree := markdownParser().Parse(text.NewReader(source))
	var blocks []markdownBlock
	var inline func(ast.Node, markdownInline) []markdownInline
	inline = func(n ast.Node, style markdownInline) []markdownInline {
		var out []markdownInline
		for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
			s := style
			switch x := ch.(type) {
			case *ast.Text:
				s.text = string(x.Segment.Value(source))
				if !x.IsRaw() {
					s.text = html.UnescapeString(string(util.UnescapePunctuations([]byte(s.text))))
				}
				if x.HardLineBreak() {
					s.text += "\n"
				} else if x.SoftLineBreak() {
					s.text += " "
				}
			case *ast.String:
				s.text = string(x.Value)
			case *ast.Emphasis:
				if x.Level == 2 {
					s.bold = true
				} else {
					s.italic = true
				}
				out = append(out, inline(ch, s)...)
				continue
			case *east.Strikethrough:
				s.strike = true
				out = append(out, inline(ch, s)...)
				continue
			case *ast.CodeSpan:
				s.code = true
				out = append(out, inline(ch, s)...)
				continue
			case *ast.Link:
				s.link = string(x.Destination)
				out = append(out, inline(ch, s)...)
				continue
			case *ast.AutoLink:
				s.text, s.link = string(x.Label(source)), string(x.URL(source))
			case *ast.Image:
				s.text = string(x.Text(source))
				s.image = string(x.Destination)
				if local := markdownPath(root, path, s.image); local != "" && (current == nil || current()) {
					if info, err := remotefs.Stat(local); err == nil && info.Mode().IsRegular() {
						key := markdownImageKey{local, info.ModTime(), info.Size()}
						used[key] = true
						image, ok := cache[key]
						if !ok {
							if strings.EqualFold(filepath.Ext(local), ".svg") {
								if data, err := readImage(local); err == nil {
									if svg, err := ui.ParseSVG(data); err == nil {
										image.bitmap = svg
										image.w, image.h = svg.Size()
										if image.w <= 0 || image.h <= 0 {
											image.bitmap = nil
										}
									}
								}
							} else {
								bm, w, h, _, _ := decodeImage(local)
								image.bitmap, image.w, image.h = bm, float32(w), float32(h)
								if bm == nil {
									image.bitmap = nil
								}
							}
							cache[key] = image
						}
						s.bitmap, s.w, s.h = image.bitmap, image.w, image.h
					}
				}
				if s.text == "" {
					s.text = filepath.Base(s.image)
					if s.text == "." || s.text == "" {
						s.text = "image"
					}
				}
				s.raw = s.bitmap == nil
			case *ast.RawHTML:
				s.raw = true
				s.text = markdownHTMLText(string(x.Segments.Value(source)))
				if tag := strings.ToLower(strings.TrimSpace(s.text)); tag == "<br>" || tag == "<br/>" || tag == "<br />" {
					s.text, s.raw = "\n", false
				}
			case *east.TaskCheckBox:
				s.text = "☐ "
				if x.IsChecked {
					s.text = "☑ "
				}
			default:
				out = append(out, inline(ch, s)...)
				continue
			}
			out = append(out, s)
		}
		return out
	}
	var walk func(ast.Node, int, int, string, bool, float32, float32)
	walk = func(n ast.Node, indent, quote int, marker string, tight bool, offset, markerWidth float32) {
		b := markdownBlock{indent: indent, quote: quote, marker: marker, tight: tight, offset: offset, markerWidth: markerWidth}
		switch x := n.(type) {
		case *ast.Heading:
			b.kind, b.level = markdownHeading, x.Level
			b.runs = inline(n, markdownInline{bold: true})
		case *ast.Paragraph, *ast.TextBlock:
			b.kind = markdownParagraph
			b.runs = inline(n, markdownInline{})
			if _, task := n.FirstChild().(*east.TaskCheckBox); task && marker != "" {
				b.marker = strings.TrimSpace(b.runs[0].text)
				b.runs = b.runs[1:]
			}

		case *ast.ThematicBreak:
			b.kind = markdownRule
		case *ast.FencedCodeBlock:
			b.kind, b.language = markdownCodeBlock, string(x.Language(source))
			b.runs = markdownCode(string(x.Lines().Value(source)), b.language)
		case *ast.CodeBlock:
			b.kind = markdownCodeBlock
			b.runs = markdownCode(string(x.Lines().Value(source)), "")
		case *ast.HTMLBlock:
			raw := string(x.Lines().Value(source))
			if x.HasClosure() {
				raw += string(x.ClosureLine.Value(source))
			}
			b.kind = markdownHTML
			raw = markdownHTMLText(raw)
			if strings.TrimSpace(raw) == "" {
				return
			}
			b.runs = []markdownInline{{text: raw, raw: true}}
		case *ast.Blockquote:
			for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
				walk(ch, indent, quote+1, marker, tight, offset, markerWidth)
				if marker != "" {
					offset += markerWidth + 0.5
					markerWidth = 0
				}
				marker = ""
			}
			return
		case *ast.List:
			i := x.Start
			width := float32(1.5)
			if x.IsOrdered() {
				width = max(width, float32(len(strconv.Itoa(x.Start+x.ChildCount()-1))+1)*0.65)
			}
			if marker != "" {
				offset += markerWidth + 0.5
			}
			for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
				m := "•"
				if x.IsOrdered() {
					m = strconv.Itoa(i) + "."
					i++
				}
				walk(ch, indent+1, quote, m, x.IsTight, offset, width)
			}
			return
		case *east.Table:
			for row := n.FirstChild(); row != nil; row = row.NextSibling() {
				rb := b
				rb.kind = markdownTable
				rb.aligns = x.Alignments
				_, head := row.(*east.TableHeader)
				if head {
					rb.kind = markdownTableHeader
				}
				for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
					rb.cells = append(rb.cells, inline(cell, markdownInline{bold: head}))
				}
				blocks = append(blocks, rb)
			}
			return
		default:
			for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
				walk(ch, indent, quote, marker, tight, offset, markerWidth)
				if marker != "" {
					offset += markerWidth + 0.5
					markerWidth = 0
				}
				marker = ""
			}
			return
		}
		// Decoded images get their own rows so that paragraphs retain native text wrapping.
		if b.kind == markdownParagraph || b.kind == markdownHeading {
			runs := b.runs
			b.runs = nil
			for _, r := range runs {
				if r.bitmap != nil {
					if markdownHasText(b.runs) {
						blocks = append(blocks, b)
						b.runs = nil
						if b.marker != "" {
							b.offset += b.markerWidth + 0.5
						}
						b.markerWidth = 0
						b.marker = ""
					}
					ib := b
					ib.kind = markdownImageBlock
					ib.runs = []markdownInline{r}
					blocks = append(blocks, ib)
					if b.marker != "" {
						b.offset += b.markerWidth + 0.5
					}
					b.markerWidth = 0
					b.marker = ""
				} else {
					b.runs = append(b.runs, r)
				}
			}
			if !markdownHasText(b.runs) {
				return
			}
		}
		blocks = append(blocks, b)
	}
	walk(tree, 0, 0, "", false, 0, 0)
	return blocks
}

// HTML comments may span lines; other HTML remains literal.
func markdownHTMLText(s string) string {
	for {
		start := strings.Index(s, "<!--")
		if start < 0 {
			return s
		}
		end := strings.Index(s[start+4:], "-->")
		if end < 0 {
			return s[:start]
		}
		s = s[:start] + s[start+4+end+3:]
	}
}

// Soft breaks between standalone images must not become empty paragraph rows.
func markdownHasText(runs []markdownInline) bool {
	for _, r := range runs {
		if strings.TrimSpace(r.text) != "" {
			return true
		}
	}
	return false
}

func markdownCode(code, language string) []markdownInline {
	var out []markdownInline
	var lines []highlight.Line
	if language != "" {
		lexer := lexers.Get(language)
		if lexer != nil {
			lexer = chroma.Coalesce(lexer)
		}
		lines = highlight.Lex(lexer, strings.TrimSuffix(code, "\n"))
	} else {
		return []markdownInline{{text: strings.TrimSuffix(code, "\n"), code: true}}
	}
	for i, line := range lines {
		rs := []rune(line.Text)
		at := 0
		for _, tok := range line.Tokens {
			if tok.Start > at {
				out = append(out, markdownInline{text: string(rs[at:tok.Start]), code: true})
			}
			out = append(out, markdownInline{text: string(rs[tok.Start:tok.End]), code: true, kind: tok.Kind})
			at = tok.End
		}
		tail := string(rs[at:])
		if i < len(lines)-1 {
			tail += "\n"
		}
		out = append(out, markdownInline{text: tail, code: true})
	}
	return out
}

// followMarkdown debounces edits and drops results superseded or closed while parsing.
func (a *app) followMarkdown(v *viewer) {
	d, m := v.doc, v.markdown
	ver := d.buf.Version()
	// Idle keeps the loaded version; debouncing keeps its timer; running keeps one
	// worker; running+dirty advances the generation and schedules after that worker.
	if (m.pending || v.loaded && v.version == ver) && m.observed == ver && m.path == d.path {
		return
	}
	first := m.generation.Load() == 0
	generation := m.generation.Add(1)
	m.observed, m.path = ver, d.path
	m.pending = true
	if v.wait != nil {
		v.wait.Stop()
		v.wait = nil
	}
	if m.running {
		return
	}
	loading := v.loading
	start := func() {
		if m.generation.Load() != generation || v.loading != loading || !slices.Contains(a.docs, d) {
			return
		}
		v.wait = nil
		m.running = true
		source, path, workspace := []byte(d.buf.String()), d.path, a.folder()
		// Give the worker its own cache snapshot; publish it on the UI thread.
		images := maps.Clone(m.images)
		if images == nil {
			images = map[markdownImageKey]markdownInline{}
		}
		go func() {
			root := markdownRoot(path, workspace)
			blocks := parseMarkdown(source, path, root, func() bool { return m.generation.Load() == generation }, images)
			a.post(func() {
				m.running, m.pending = false, false
				m.images = images
				m.root = root
				if v.loading != loading || !slices.Contains(a.docs, d) {
					return
				}
				if m.generation.Load() != generation || d.buf.Version() != ver || d.path != path {
					a.followMarkdown(v)
					return
				}
				m.blocks = blocks
				v.version = ver
				v.loaded = true
			})
		}()
	}
	if first {
		start()
	} else {
		v.wait = time.AfterFunc(svgDelay, func() { a.post(start) })
	}
}

// markdownSpan resolves every inline style before either a span or link is built.
func (a *app) markdownSpan(r markdownInline, color ui.Color, blockCode bool) ui.Span {
	p, settings := a.pal, a.settings
	span := ui.Span{Text: r.text, Color: color, Italic: r.italic, Strikethrough: r.strike}
	if p.mono || r.code || blockCode {
		span.Font, span.Weight, span.Features = settings.FontFamily, settings.FontWeight, strings.Join(settings.FontFeatures, ",")
	}
	if r.bold {
		span.Weight = 700
	}
	if r.raw || r.image != "" && r.bitmap == nil {
		span.Color = p.muted
	}
	if r.code {
		if blockCode {
			if col := p.syntax[r.kind]; col.A > 0 {
				span.Color = col
			}
		} else {
			span.Background = p.surfaceHover
			span.Text = "\u2009" + span.Text + "\u2009"
		}
	}
	if r.link != "" && r.image == "" {
		span.Color = p.accent
	}
	if r.strike {
		span.DecorationColor = span.Color
	}
	return span
}

func (a *app) markdownSize(b markdownBlock) float32 {
	size := a.settings.FontSize
	if b.kind == markdownHeading {
		size *= []float32{1.6, 1.35, 1.15, 1, 1, 1}[b.level-1]
	}
	return size
}

func (a *app) markdownSpacing(b markdownBlock) (top, bottom float32) {
	gap := a.settings.FontSize * a.settings.LineHeight * 0.75
	bottom = gap
	if b.tight || b.kind == markdownTable || b.kind == markdownTableHeader {
		bottom = 0
	}
	if b.kind == markdownHeading {
		top, bottom = gap, gap*0.5
	}
	return
}

func (a *app) markdownText(c *ui.Context, runs []markdownInline, size float32, muted bool, root, path string, blockCode bool) {
	p := a.pal
	color := p.text
	if muted {
		color = p.muted
	}
	text := ui.RichText(c).FontSize(size).LineHeight(a.settings.LineHeight).TextColor(color).DecorationColor(color).WidthPercent(100)
	if blockCode || p.mono {
		text.Font(a.settings.FontFamily).FontWeight(a.settings.FontWeight).FontFeatures(a.settings.FontFeatures...)
	}
	text.Children(func() {
		for _, r := range runs {
			span := a.markdownSpan(r, color, blockCode)
			if r.link != "" {
				if e := a.markdownLink(c, r.link, root, path, false); e.Valid() {
					e.Children(func() { ui.RichText(c, span) })
					continue
				}
			}
			ui.RichText(c, span)
		}
	})
}

func (a *app) markdownLink(c *ui.Context, dest, root, path string, image bool) ui.Element {
	u, err := url.Parse(dest)
	remote := err == nil && (u.Scheme == "http" || u.Scheme == "https")
	local := markdownPath(root, path, dest)
	if !remote && local == "" {
		return ui.Element{}
	}
	e := ui.Element{}
	if image {
		e = ui.Box(c).Cursor(ui.CursorPointer).Focusable()
	} else {
		e = ui.Link(c, "", "")
	}
	e = e.Label("Open link " + dest)
	if remote {
		return e.OnClick(func() { c.OpenURL(dest) })
	}
	return e.OnClick(func() { a.openFile(local, true) })
}

func (a *app) markdownImage(c *ui.Context, r markdownInline, width float32, root, path string) {
	draw := func() {
		w := min(r.w, max(1, width))
		ui.Image(c, r.bitmap).Width(w).Height(w * r.h / r.w).Fit(ui.Contain)
	}

	if e := a.markdownLink(c, r.link, root, path, true); e.Valid() {
		e.Children(draw)
	} else {
		draw()
	}
}

func (a *app) markdownView(c *ui.Context, v *viewer, name string) {
	a.followMarkdown(v)
	m, p := v.markdown, a.pal
	size := a.settings.FontSize
	// Measure the actual body font, including the Matrix UI font.
	font := ui.Font{Family: c.Theme().Font, Size: size}
	if p.mono {
		font.Weight, font.Features = a.settings.FontWeight, strings.Join(a.settings.FontFeatures, ",")
	}
	if m.measured != font || m.measure == 0 {
		m.measured = font
		m.measure, _ = ui.ShapeText(strings.Repeat("0", 80), font, 0).Size()
	}
	width := m.measure
	ui.List(c.Key("markdown"), &m.list, len(m.blocks)).Bind(&v.pane).TrackScroll(&m.scroll).Fill().Background(p.editor).Label("Preview "+name).Padding(editorPadY, textPadR).Rows(func(row ui.ListRow) {
		c := row.Context
		b := m.blocks[row.Index]
		top, bottom := a.markdownSpacing(b)
		if (b.tight && (row.Index+1 == len(m.blocks) || !m.blocks[row.Index+1].tight)) || (b.kind == markdownTable && (row.Index+1 == len(m.blocks) || m.blocks[row.Index+1].kind != markdownTable)) {
			bottom = size * a.settings.LineHeight * 0.75
		}
		outside := float32(0)
		if b.quote > 0 && (row.Index+1 == len(m.blocks) || m.blocks[row.Index+1].quote < b.quote) {
			outside, bottom = max(bottom, size*a.settings.LineHeight*0.75), 0
		}
		ui.Row(c).WidthPercent(100).Justify(ui.Center).Margin(0, 0, outside, 0).Children(func() {
			ui.Row(c).WidthPercent(100).MaxWidth(width).AlignItems(ui.Start).Padding(0, 0, 0, b.offset*size).Gap(size * 0.5).Children(func() {
				for range b.quote {
					ui.Box(c).Width(3).AlignSelf(ui.Stretch).Background(p.guideNow)
				}
				if b.marker != "" {
					if b.marker == "•" {
						ui.Box(c).Width(size * b.markerWidth).Height(size * a.settings.LineHeight).Children(func() {
							ui.Box(c).Width(size*0.35).Height(size*0.35).Radius(size*0.175).Background(p.text).Margin((size*a.settings.LineHeight-size*0.35)/2, 0)
						})
					} else {
						ui.Text(c, b.marker).Width(size * b.markerWidth).TextColor(p.text).FontSize(size).LineHeight(a.settings.LineHeight)
					}
				}

				ui.Column(c).Grow(1).MinWidth(0).Padding(top, 0, bottom, 0).Children(func() {
					switch b.kind {
					case markdownRule:
						ui.Box(c).Height(1).WidthPercent(100).Background(p.border).Margin(size*a.settings.LineHeight*0.375, 0)
					case markdownImageBlock:
						imageWidth := max(1, min(width, v.pane.Bounds(c).W-2*textPadR)-(b.offset+b.markerWidth)*size-float32(b.quote)*size)
						a.markdownImage(c, b.runs[0], imageWidth, m.root, v.doc.path)
					case markdownCodeBlock, markdownHTML:
						ui.Column(c).WidthPercent(100).Padding(size*0.5, size*0.75).Background(p.chrome).Radius(6).Children(func() {
							a.markdownText(c, b.runs, size, b.kind == markdownHTML, m.root, v.doc.path, true)
						})
					case markdownTable, markdownTableHeader:
						ui.Row(c).WidthPercent(100).Children(func() {
							for i, cell := range b.cells {
								cellBox := ui.Column(c).WidthPercent(100/float32(len(b.cells))).Padding(size*0.35, size*0.5).Border(1, p.border).BorderWidth(0, 1, 1, 0)
								if i == 0 {
									cellBox.BorderWidth(0, 1, 1, 1)
								}
								if b.kind == markdownTableHeader {
									if i == 0 {
										cellBox.BorderWidth(1)
									} else {
										cellBox.BorderWidth(1, 1, 1, 0)
									}
								}
								if b.kind == markdownTableHeader {
									cellBox.Background(p.chrome)
								}
								cellBox.Children(func() {
									align := ui.Start
									if i < len(b.aligns) {
										switch b.aligns[i] {
										case east.AlignCenter:
											align = ui.Center
										case east.AlignRight:
											align = ui.End
										}
									}
									ui.Column(c).TextAlign(align).WidthPercent(100).Children(func() {
										for len(cell) > 0 {
											n := 0
											for n < len(cell) && cell[n].bitmap == nil {
												n++
											}
											if n > 0 {
												a.markdownText(c, cell[:n], size, false, m.root, v.doc.path, false)
											}
											if n < len(cell) {
												a.markdownImage(c, cell[n], width/float32(len(b.cells))-size, m.root, v.doc.path)
												n++
											}
											cell = cell[n:]
										}
									})
								})
							}
						})
					default:
						a.markdownText(c, b.runs, a.markdownSize(b), b.kind == markdownHeading && b.level == 6, m.root, v.doc.path, false)
					}
				})
			})
		})
	})
}
