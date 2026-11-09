package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/plugin"
	"mayoi/locales"
)

// viewNode is a node of the tree a plugin renders a view or a preview
// with, which Mayoi draws in its theme.
type viewNode struct {
	Type        string      `json:"type"`
	Children    []*viewNode `json:"children"`
	Gap         float32     `json:"gap"`
	Padding     float32     `json:"padding"`
	Text        string      `json:"text"`
	Muted       bool        `json:"muted"`
	Bold        bool        `json:"bold"`
	Size        float32     `json:"size"`
	Language    string      `json:"language"`
	SVG         string      `json:"svg"`
	Height      float32     `json:"height"`
	Items       []*viewItem `json:"items"`
	Empty       string      `json:"empty"`
	Value       string      `json:"value"`
	Placeholder string      `json:"placeholder"`
	Options     []string    `json:"options"`
	Label       string      `json:"label"`
	Checked     bool        `json:"checked"`
	Icon        string      `json:"icon"`
	Primary     bool        `json:"primary"`
	OnClick     *handlerRef `json:"onClick"`
	OnChange    *handlerRef `json:"onChange"`
	OnSubmit    *handlerRef `json:"onSubmit"`

	// What drawing it keeps: the picture an svg node parsed to, the text
	// an input holds, and the Markdown a markdown node parsed to.
	svg    *ui.SVG
	svgErr string
	input  *string
	blocks []markdownBlock
	parsed bool
}

// viewItem is a row of a list or a tree node.
type viewItem struct {
	Key         string       `json:"key"`
	Label       string       `json:"label"`
	Description string       `json:"description"`
	Icon        string       `json:"icon"`
	OnClick     *handlerRef  `json:"onClick"`
	Actions     []viewAction `json:"actions"`
	Children    []*viewItem  `json:"children"`
	Open        bool         `json:"open"`
}

type viewAction struct {
	Icon    string      `json:"icon"`
	Tooltip string      `json:"tooltip"`
	OnClick *handlerRef `json:"onClick"`
}

// pluginView is a view a plugin rendered: its tree, and the plugin to
// call back.
type pluginView struct {
	plugin *plugin.Plugin
	tree   *viewNode
	// open are the tree nodes opened or closed by hand, by key.
	open map[string]bool
}

var viewsMu sync.Mutex

// renderView keeps the tree a plugin rendered a view with.
func (h *pluginHost) renderView(p *plugin.Plugin, id string, raw json.RawMessage) error {
	known := false
	for _, v := range p.Contributes.Views {
		known = known || v.ID == id
	}
	if !known {
		return fmt.Errorf("view %s is not in plugin.json", id)
	}
	var n viewNode
	if err := json.Unmarshal(raw, &n); err != nil {
		return err
	}
	h.mu.Lock()
	old := h.views[id]
	h.views[id] = &n
	h.mu.Unlock()
	// Text fields keep what was typed in them, by place.
	if old != nil {
		keepInputs(old, &n)
	}
	viewsMu.Lock()
	pv := pluginViews[id]
	if pv == nil {
		pv = &pluginView{open: map[string]bool{}}
		pluginViews[id] = pv
	}
	pv.plugin, pv.tree = p, &n
	viewsMu.Unlock()
	h.redraw()
	return nil
}

// pluginViews are the views rendered, by id.
var pluginViews = map[string]*pluginView{}

func keepInputs(old, n *viewNode) {
	if old.Type == n.Type && old.input != nil && n.Type == "input" && old.Value == n.Value {
		n.input = old.input
	}
	for i := range min(len(old.Children), len(n.Children)) {
		keepInputs(old.Children[i], n.Children[i])
	}
}

// pluginSlot is a view or a webview the sidebar can show.
type pluginSlot struct {
	id, title, icon string
	plugin          *plugin.Plugin
	webview         *plugin.WebviewContrib
}

// pluginSlots are the views of the plugins that are on, and their
// webviews that are open.
func pluginSlots() []pluginSlot {
	if plugins == nil {
		return nil
	}
	var out []pluginSlot
	for _, p := range plugins.enabled() {
		for _, v := range p.Contributes.Views {
			out = append(out, pluginSlot{id: v.ID, title: v.Title, icon: v.Icon, plugin: p})
		}
		for i, w := range p.Contributes.Webviews {
			if plugins.webviewOpen(w.ID) {
				out = append(out, pluginSlot{id: w.ID, title: w.Title, icon: "external", plugin: p, webview: &p.Contributes.Webviews[i]})
			}
		}
	}
	return out
}

// pluginSidebarView is the id of the plugin view the sidebar shows, ""
// for one of Mayoi's.
func (a *app) pluginSidebarView() string {
	id, _ := strings.CutPrefix(a.window.SidebarView, "plugin:")
	if id == a.window.SidebarView {
		return ""
	}
	return id
}

// showPluginView shows a plugin's view in the sidebar, starting the
// plugin, which renders it.
func (a *app) showPluginView(id string) {
	var slot *pluginSlot
	for _, s := range pluginSlots() {
		if s.id == id {
			slot = &s
		}
	}
	if slot == nil {
		return
	}
	a.window.SidebarView = "plugin:" + id
	if a.window.SidebarClosed {
		a.toggleSidebar()
	}
	a.settings.Changed()
	if slot.webview == nil {
		p := slot.plugin
		plugins.reg.Activate("onView:" + id)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), plugin.BusyLimit)
			defer cancel()
			p.Request(ctx, "viewShow", map[string]any{"id": id})
		}()
	}
}

// pluginSwitches are the sidebar's buttons of the plugins' views.
func (a *app) pluginSwitches(c *ui.Context) {
	shown := a.pluginSidebarView()
	for _, s := range pluginSlots() {
		id := s.id
		a.sidebarSwitch(c, namedIcon(s.icon), s.title, shown == id, 0, func() { a.showPluginView(id) })
	}
}

// pluginSidebarTitle is the title of the plugin view shown.
func (a *app) pluginSidebarTitle() string {
	id := a.pluginSidebarView()
	for _, s := range pluginSlots() {
		if s.id == id {
			return s.title
		}
	}
	return ""
}

// pluginSidebar draws the plugin view the sidebar shows.
func (a *app) pluginSidebar(c *ui.Context) {
	id := a.pluginSidebarView()
	var slot *pluginSlot
	for _, s := range pluginSlots() {
		if s.id == id {
			slot = &s
		}
	}
	p := a.pal
	if slot == nil {
		ui.Text(c, locales.PluginViewGone()).FontSize(12).TextColor(p.muted).Padding(12, 16)
		return
	}
	if slot.webview != nil {
		a.webviewSlot(c, slot)
		return
	}
	viewsMu.Lock()
	pv := pluginViews[id]
	viewsMu.Unlock()
	if pv == nil || pv.tree == nil {
		ui.Row(c).Padding(12, 16).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Size(11, 11)
			ui.Text(c, locales.PluginViewLoading()).FontSize(12).TextColor(p.muted)
		})
		return
	}
	ui.Scroll(c.Key("plugin-view:"+id)).Grow(1).MinHeight(0).Padding(6, 8).Children(func() {
		a.drawViewNode(c, "pv:"+id, pv.tree, pv)
	})
}

// drawViewNode draws a node of a plugin's tree.
func (a *app) drawViewNode(c *ui.Context, key string, n *viewNode, pv *pluginView) {
	p := a.pal
	call := func(ref *handlerRef, v any) {
		if plugins != nil && pv != nil {
			plugins.callHandler(pv.plugin, ref, v)
		}
	}
	switch n.Type {
	case "stack", "row", "":
		var el ui.Element
		if n.Type == "row" {
			el = ui.Row(c.Key(key)).AlignItems(ui.Center)
		} else {
			el = ui.Column(c.Key(key))
		}
		gap := n.Gap
		if gap == 0 {
			gap = 6
		}
		el.Gap(gap).Padding(n.Padding, n.Padding).Children(func() {
			for i, ch := range n.Children {
				a.drawViewNode(c, fmt.Sprintf("%s/%d", key, i), ch, pv)
			}
		})
	case "text":
		size := n.Size
		if size == 0 {
			size = 12.5
		}
		col := p.text
		if n.Muted {
			col = p.muted
		}
		t := ui.Text(c.Key(key), n.Text).FontSize(size).TextColor(col).Selectable()
		if n.Bold {
			t.FontWeight(650)
		}
	case "markdown":
		a.viewMarkdown(c, key, n)
	case "code":
		ui.Text(c.Key(key), n.Text).Font(a.settings.FontFamily).FontSize(a.settings.FontSize*0.9).TextColor(p.text).
			Background(p.surfaceHover).Padding(6, 8).Radius(5).Selectable()
	case "svg":
		if n.svg == nil && n.svgErr == "" {
			s, err := ui.ParseSVG([]byte(n.SVG))
			if err != nil {
				n.svgErr = err.Error()
			} else {
				n.svg = s
			}
		}
		if n.svg == nil {
			ui.Text(c.Key(key), n.svgErr).FontSize(12).TextColor(p.dangerFg)
			return
		}
		w, h := n.svg.Size()
		height := n.Height
		if height == 0 {
			height = min(h, 400)
		}
		ui.Image(c.Key(key), n.svg).Height(height).Width(height * w / max(h, 1)).MaxWidthPercent(100)
	case "list":
		if len(n.Items) == 0 && n.Empty != "" {
			ui.Text(c.Key(key), n.Empty).FontSize(12).TextColor(p.muted).Padding(4, 6)
		}
		ui.Column(c.Key(key)).Gap(1).Children(func() {
			for i, it := range n.Items {
				a.viewItemRow(c, fmt.Sprintf("%s/%d", key, i), it, 0, pv)
			}
		})
	case "tree":
		ui.Column(c.Key(key)).Gap(1).Children(func() {
			for i, it := range n.Items {
				a.viewTreeRow(c, fmt.Sprintf("%s/%d", key, i), it, 0, pv)
			}
		})
	case "input":
		if n.input == nil {
			v := n.Value
			n.input = &v
		}
		in := a.textKeys(ui.TextInput(c.Key(key), n.input)).FontSize(12.5).Placeholder(n.Placeholder).Label(n.Placeholder)
		if in.Changed() {
			call(n.OnChange, *n.input)
		}
		if in.Submitted() {
			call(n.OnSubmit, *n.input)
		}
	case "select":
		label := n.Value
		if label == "" && len(n.Options) > 0 {
			label = n.Options[0]
		}
		b := ui.ButtonBase(c.Key(key)).Padding(4, 8).Radius(5).Gap(6).AlignItems(ui.Center).Background(p.surfaceHover).Label(label)
		b.Children(func() {
			ui.Text(c, label).FontSize(12.5).TextColor(p.text).Grow(1)
			ui.Icon(c, iconChevronDown).FontSize(12).TextColor(p.muted)
		})
		b.Menu(func(m *ui.Menu) {
			for _, o := range n.Options {
				if m.Item(o).Checked(o == n.Value).Chosen() {
					n.Value = o
					call(n.OnChange, o)
				}
			}
		})
	case "checkbox":
		checked := n.Checked
		if ui.Checkbox(c.Key(key), &checked, n.Label).Changed() {
			n.Checked = checked
			call(n.OnChange, checked)
		}
	case "button":
		var b ui.Element
		if n.Primary {
			b = ui.PrimaryButton(c.Key(key), n.Label)
		} else {
			b = ui.Button(c.Key(key), n.Label)
		}
		if b.Clicked() {
			call(n.OnClick, nil)
		}
	case "progress":
		if n.Value == "" {
			ui.Spinner(c.Key(key)).Size(12, 12)
		} else {
			var f float32
			fmt.Sscan(n.Value, &f)
			ui.Box(c.Key(key)).Height(4).Radius(2).Background(p.surfaceHover).Children(func() {
				ui.Box(c).Height(4).Radius(2).WidthPercent(min(max(f, 0), 1) * 100).Background(p.accent)
			})
		}
	case "divider":
		ui.Box(c.Key(key)).Height(1).Background(p.border).Margin(4, 0)
	case "empty":
		ui.Column(c.Key(key)).Padding(16, 8).Center().Children(func() {
			ui.Text(c, n.Text).FontSize(12).TextColor(p.muted).TextAlign(ui.Center)
		})
	default:
		ui.Text(c.Key(key), locales.PluginViewUnknown(n.Type)).FontSize(12).TextColor(p.muted)
	}
}

// viewMarkdown draws Markdown a plugin gave: its headings, paragraphs and
// code, as the preview has them.
func (a *app) viewMarkdown(c *ui.Context, key string, n *viewNode) {
	p := a.pal
	if !n.parsed {
		n.parsed = true
		n.blocks = parseMarkdown([]byte(n.Text), "", "", nil)
	}
	size := a.settings.FontSize
	ui.Column(c.Key(key)).Gap(6).Children(func() {
		for i, b := range n.blocks {
			k := fmt.Sprintf("%s/%d", key, i)
			switch b.kind {
			case markdownHeading:
				sz := size * (1.5 - 0.12*float32(min(b.level, 4)))
				var spans []ui.Span
				for _, r := range b.runs {
					sp := a.markdownSpan(r, p.text, sz, false)
					sp.Weight = 700
					spans = append(spans, sp)
				}
				ui.RichText(c.Key(k), spans...).FontSize(sz).Selectable()
			case markdownRule:
				ui.Box(c.Key(k)).Height(1).Background(p.border).Margin(4, 0)
			case markdownCodeBlock:
				var spans []ui.Span
				for _, r := range b.runs {
					spans = append(spans, a.markdownSpan(r, p.text, size*0.9, true))
				}
				ui.RichText(c.Key(k), spans...).FontSize(size*0.9).Background(p.surfaceHover).Padding(6, 8).Radius(5).Selectable()
			default:
				var spans []ui.Span
				for _, r := range b.runs {
					spans = append(spans, a.markdownSpan(r, p.text, size, false))
				}
				if len(spans) > 0 {
					t := ui.RichText(c.Key(k), spans...).FontSize(size).Selectable()
					if b.marker != "" {
						t.Padding(0, 0, 0, 14+float32(b.indent)*14)
					}
				}
			}
		}
	})
}

// viewItemRow draws a row of a list.
func (a *app) viewItemRow(c *ui.Context, key string, it *viewItem, depth int, pv *pluginView) {
	p := a.pal
	if it.Key != "" {
		key = "k:" + it.Key
	}
	row := ui.ButtonBase(c.Key(key)).Padding(3, 6, 3, 6+float32(depth)*14).Gap(7).Radius(5).Justify(ui.Start).AlignItems(ui.Center).Label(it.Label)
	if row.Hovered() {
		row.Background(p.hover)
	}
	row.Transition(hoverFade)
	if row.Clicked() && it.OnClick != nil {
		plugins.callHandler(pv.plugin, it.OnClick, nil)
	}
	hovered := row.Hovered()
	row.Children(func() {
		if it.Icon != "" {
			ui.Icon(c, namedIcon(it.Icon)).FontSize(13).TextColor(p.muted)
		}
		ui.Text(c, it.Label).FontSize(12.5).TextColor(p.text).SingleLine().Shrink(0)
		if it.Description != "" {
			ui.Text(c, it.Description).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(1)
		}
		ui.Spacer(c)
		if hovered {
			for _, ac := range it.Actions {
				ac := ac
				if a.iconButton(c, namedIcon(ac.Icon), ac.Tooltip, false).Clicked() {
					plugins.callHandler(pv.plugin, ac.OnClick, nil)
				}
			}
		}
	})
}

// viewTreeRow draws a node of a tree and, open, its children.
func (a *app) viewTreeRow(c *ui.Context, key string, it *viewItem, depth int, pv *pluginView) {
	p := a.pal
	if it.Key != "" {
		key = "k:" + it.Key
	}
	open := it.Open
	if o, ok := pv.open[key]; ok {
		open = o
	}
	ui.Column(c.Key(key)).Children(func() {
		row := ui.ButtonBase(c.Key("row")).Padding(3, 6, 3, 2+float32(depth)*14).Gap(4).Radius(5).Justify(ui.Start).AlignItems(ui.Center).Label(it.Label)
		if row.Hovered() {
			row.Background(p.hover)
		}
		row.Transition(hoverFade)
		if row.Clicked() {
			if len(it.Children) > 0 {
				pv.open[key] = !open
			}
			if it.OnClick != nil {
				plugins.callHandler(pv.plugin, it.OnClick, nil)
			}
		}
		row.Children(func() {
			chev := iconChevronRight
			if open {
				chev = iconChevronDown
			}
			if len(it.Children) > 0 {
				ui.Icon(c, chev).FontSize(12).TextColor(p.muted)
			} else {
				ui.Box(c).Width(12)
			}
			if it.Icon != "" {
				ui.Icon(c, namedIcon(it.Icon)).FontSize(13).TextColor(p.muted)
			}
			ui.Text(c, it.Label).FontSize(12.5).TextColor(p.text).SingleLine().Shrink(0)
			if it.Description != "" {
				ui.Text(c, it.Description).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(1)
			}
		})
		if open {
			for i, ch := range it.Children {
				a.viewTreeRow(c, fmt.Sprintf("%d", i), ch, depth+1, pv)
			}
		}
	})
}

// Previews

// previewOf is the preview a plugin declares for a document's file, nil
// for none.
func previewOf(d *document) (*plugin.Plugin, *plugin.PreviewContrib) {
	if plugins == nil || d.path == "" {
		return nil, nil
	}
	base := filepath.Base(d.path)
	for _, p := range plugins.enabled() {
		for i, pr := range p.Contributes.Previews {
			pat := pr.Pattern
			ok, _ := path.Match(pat, base)
			if !ok && strings.Contains(pat, "/") {
				ok, _ = path.Match(strings.TrimPrefix(pat, "**/"), base)
			}
			if !ok && strings.HasPrefix(pat, "**/") {
				ok, _ = path.Match(strings.TrimPrefix(pat, "**/"), base)
			}
			if ok {
				return p, &p.Contributes.Previews[i]
			}
		}
	}
	return nil, nil
}

// pluginPreview is what a preview of a plugin shows: a picture, or a
// tree.
type pluginPreview struct {
	plugin *plugin.Plugin
	id     string
	output string
	tree   *viewNode
	view   *pluginView
	gen    uint64 // refreshes asked for, which render again
}

// previewGen counts the refreshes of each preview plugins asked for.
var previewGen sync.Map // id -> uint64

func (h *pluginHost) refreshPreviews(id string) {
	n, _ := previewGen.Load(id)
	g, _ := n.(uint64)
	previewGen.Store(id, g+1)
	h.redraw()
}

// followPluginPreview renders a document's preview with its plugin once
// its text changed, a moment after the last edit, keeping the last that
// rendered.
func (a *app) followPluginPreview(v *viewer) {
	d := v.doc
	if v.plugin == nil {
		p, pr := previewOf(d)
		if p == nil {
			return
		}
		v.plugin = &pluginPreview{plugin: p, id: pr.ID, output: pr.Output}
		plugins.reg.Activate("onPreview:" + pr.ID)
	}
	pp := v.plugin
	n, _ := previewGen.Load(pp.id)
	gen, _ := n.(uint64)
	if v.loaded && d.buf.Version() == v.version && gen == pp.gen || v.wait != nil || v.drawing {
		return
	}
	render := func() {
		v.wait = nil
		v.drawing = true
		v.loading++
		load, ver, text, file := v.loading, d.buf.Version(), d.buf.String(), d.path
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), plugin.BusyLimit)
			defer cancel()
			res, err := pp.plugin.Request(ctx, "preview", map[string]any{"id": pp.id, "text": text, "path": file})
			a.post(func() {
				v.drawing = false
				if v.loading != load {
					return
				}
				v.version, v.loaded, pp.gen = ver, true, gen
				if err != nil {
					v.err = firstLine(err.Error())
					return
				}
				v.err = ""
				var s string
				if json.Unmarshal(res, &s) == nil {
					switch pp.output {
					case "svg":
						pic, err := ui.ParseSVG([]byte(s))
						if err != nil {
							v.err = err.Error()
							return
						}
						w, h := pic.Size()
						if v.src != nil && (v.w != w || v.h != h) {
							v.zoom, v.panX, v.panY = 0, 0, 0
						}
						v.src, v.w, v.h = pic, w, h
						pp.tree = nil
					default:
						pp.tree = &viewNode{Type: "markdown", Text: s}
					}
					return
				}
				var tree viewNode
				if err := json.Unmarshal(res, &tree); err != nil {
					v.err = err.Error()
					return
				}
				pp.tree = &tree
				if pp.view == nil {
					pp.view = &pluginView{plugin: pp.plugin, open: map[string]bool{}}
				}
				pp.view.tree = &tree
			})
		}()
	}
	if !v.loaded {
		render()
		return
	}
	v.wait = time.AfterFunc(svgDelay, func() { a.post(render) })
}

// pluginPreviewView draws a plugin preview that shows a tree or Markdown;
// it reports false for a picture, which the viewer draws.
func (a *app) pluginPreviewView(c *ui.Context, v *viewer, name string) bool {
	pp := v.plugin
	if pp == nil || pp.tree == nil {
		if v.err != "" && v.src == nil {
			p := a.pal
			ui.Column(c.Key("plugin-preview-err")).Fill().Center().Background(p.editor).Children(func() {
				ui.Icon(c, iconTriangleAlert).FontSize(24).TextColor(p.faint)
				ui.Text(c, v.err).FontSize(12.5).TextColor(p.muted).Margin(10, 0, 0, 0).MaxWidthPercent(80).TextAlign(ui.Center)
			})
			return true
		}
		return false
	}
	p := a.pal
	if pp.view == nil {
		pp.view = &pluginView{plugin: pp.plugin, open: map[string]bool{}, tree: pp.tree}
	}
	ui.Scroll(c.Key("plugin-preview")).Fill().Background(p.editor).Padding(editorPadY*2, textPadR).Label(locales.PreviewLabel(name)).Children(func() {
		a.drawViewNode(c, "pp", pp.tree, pp.view)
	})
	return true
}

// Webviews

// webviewState is a plugin's webview open in the windows: its page in
// each, made as the window first shows it.
type webviewState struct {
	plugin  *plugin.Plugin
	contrib plugin.WebviewContrib
	title   string
	pages   map[*mygo.Window]*mygo.WebView
}

var webviews = struct {
	sync.Mutex
	open map[string]*webviewState
}{open: map[string]*webviewState{}}

func (h *pluginHost) webviewOpen(id string) bool {
	webviews.Lock()
	defer webviews.Unlock()
	return webviews.open[id] != nil
}

// webviewBridge is the script every webview page has: mayoi.post sends a
// message to the plugin, mayoi.onMessage hears its messages.
const webviewBridge = `(function(){
  const fns = [];
  window.mayoi = {
    post(message) { location.href = "mayoi-message:" + encodeURIComponent(JSON.stringify(message ?? null)); },
    onMessage(fn) { fns.push(fn); },
    __receive(message) { for (const fn of fns) { try { fn(message); } catch (e) { console.error(e); } } },
  };
})();`

// webviewCall opens, messages and closes a plugin's webviews.
func (h *pluginHost) webviewCall(p *plugin.Plugin, method, id, title string, raw json.RawMessage) (any, error) {
	var contrib *plugin.WebviewContrib
	for i, w := range p.Contributes.Webviews {
		if w.ID == id {
			contrib = &p.Contributes.Webviews[i]
		}
	}
	if contrib == nil {
		return nil, fmt.Errorf("webview %s is not in plugin.json", id)
	}
	switch method {
	case "webview.open":
		webviews.Lock()
		st := webviews.open[id]
		if st == nil {
			st = &webviewState{plugin: p, contrib: *contrib, pages: map[*mygo.Window]*mygo.WebView{}}
			webviews.open[id] = st
		}
		if title != "" {
			st.title = title
		}
		webviews.Unlock()
		return h.main(func(a *app) (any, error) {
			a.showPluginView(id)
			return nil, nil
		})
	case "webview.post":
		var args struct {
			Message json.RawMessage `json:"message"`
		}
		json.Unmarshal(raw, &args)
		webviews.Lock()
		st := webviews.open[id]
		var pages []*mygo.WebView
		if st != nil {
			for _, wv := range st.pages {
				pages = append(pages, wv)
			}
		}
		webviews.Unlock()
		if st == nil {
			return nil, errors.New("the webview is not open")
		}
		msg := string(args.Message)
		if msg == "" {
			msg = "null"
		}
		for _, wv := range pages {
			wv.Page().Eval("window.mayoi && window.mayoi.__receive(" + msg + ")")
		}
		return nil, nil
	case "webview.close":
		webviews.Lock()
		st := webviews.open[id]
		delete(webviews.open, id)
		webviews.Unlock()
		if st != nil {
			for _, wv := range st.pages {
				wv.Destroy()
			}
		}
		h.main(func(a *app) (any, error) {
			for _, a := range h.apps() {
				if a.pluginSidebarView() == id {
					a.showExplorer()
				}
			}
			return nil, nil
		})
		return nil, nil
	}
	return nil, nil
}

// webviewSlot draws a webview in the sidebar, making its page in the
// window first.
func (a *app) webviewSlot(c *ui.Context, slot *pluginSlot) {
	p := a.pal
	h, ok := a.host.(*desktop)
	if !ok || h.f == nil || h.f.win == nil {
		return
	}
	webviews.Lock()
	st := webviews.open[slot.id]
	var wv *mygo.WebView
	if st != nil {
		wv = st.pages[h.f.win]
	}
	webviews.Unlock()
	if st == nil {
		return
	}
	if wv == nil {
		var err error
		wv, err = a.newPluginWebview(h.f.win, st)
		if err != nil {
			ui.Text(c, locales.PluginWebviewFailed(err.Error())).FontSize(12).TextColor(p.dangerFg).Padding(12, 16)
			return
		}
		webviews.Lock()
		st.pages[h.f.win] = wv
		webviews.Unlock()
	}
	ui.WebView(c.Key("webview:"+slot.id), wv).Grow(1)
}

// newPluginWebview makes the page of a plugin's webview in a window: its
// HTML, from the plugin's folder, with the bridge to the plugin.
func (a *app) newPluginWebview(win *mygo.Window, st *webviewState) (*mygo.WebView, error) {
	html, err := fs.ReadFile(st.plugin.FS(), path.Clean(st.contrib.Path))
	if err != nil {
		return nil, err
	}
	wv, err := win.NewWebView(mygo.WebViewOptions{Page: mygo.PageOptions{PreloadScript: webviewBridge}})
	if err != nil {
		return nil, err
	}
	pg := wv.Page()
	pl := st.plugin
	pg.OnWillNavigate(func(e *mygo.NavigateEvent) {
		data, ok := strings.CutPrefix(e.URL, "mayoi-message:")
		if !ok {
			return
		}
		e.PreventDefault()
		msg, err := url.PathUnescape(data)
		if err != nil {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), plugin.BusyLimit)
			defer cancel()
			pl.Request(ctx, "webview", map[string]any{"id": st.contrib.ID, "message": json.RawMessage(msg)})
		}()
	})
	base := ""
	if pl.Dir != "" {
		base = (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(pl.Dir, path.Dir(st.contrib.Path))) + "/"}).String()
	}
	pg.LoadHTML(string(html), base)
	return wv, nil
}
