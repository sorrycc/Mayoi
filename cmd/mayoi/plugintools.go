package main

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/plugin"
	"mayoi/locales"
)

// Two tools of the plugins, each in a tab of its own: Running Plugins, a
// table of how each plugin is doing, read again every second while it
// shows, and the Plugin Log, what every plugin logged, as it comes.

// toolKind is which tool a tool tab shows.
type toolKind int

const (
	toolRunning toolKind = iota
	toolLog
)

// toolView is a tab of a tool of the plugins.
type toolView struct {
	kind toolKind
	pane ui.Handle
	run  runningView
	log  logView
}

// newToolView makes the tab of a tool.
func newToolView(kind toolKind) *toolView {
	v := &toolView{kind: kind}
	v.run.hist = map[string]*memHistory{}
	l := &v.log
	l.levels = logAllLevels
	l.list.FollowEnd = true
	l.list.FollowAnimation = &logFollow
	l.list.Key = func(i int) any { return l.shown[i] }
	return v
}

func (v *toolView) name() string {
	if v.kind == toolLog {
		return locales.PluginLogTitle()
	}
	return locales.PluginRunningTitle()
}

func (v *toolView) icon() *ui.SVG {
	if v.kind == toolLog {
		return iconScrollText
	}
	return iconActivity
}

// clone is the tool again, for another column: as filtered and sorted.
func (v *toolView) clone() *toolView {
	n := newToolView(v.kind)
	n.run.sort, n.run.desc = v.run.sort, v.run.desc
	n.log.plugin, n.log.levels, n.log.query = v.log.plugin, v.log.levels, v.log.query
	return n
}

// showTool shows the tab of a tool, opening it in the file column unless
// a column has it already.
func (a *app) showTool(kind toolKind) *toolView {
	for _, col := range a.cols {
		for i, t := range col.tabs {
			if t.tool != nil && t.tool.kind == kind {
				a.activate(col, i, true)
				return t.tool
			}
		}
	}
	col := a.fileColumn()
	v := newToolView(kind)
	at := min(col.active+1, len(col.tabs))
	a.addTab(col, at, &colTab{tool: v})
	a.activate(col, at, true)
	return v
}

// showRunningPlugins shows the table of the plugins.
func (a *app) showRunningPlugins() { a.showTool(toolRunning) }

// showPluginLog shows the log of every plugin.
func (a *app) showPluginLog() { a.showPluginLogOf(nil) }

// showPluginLogOf shows the log of the plugin p, of every plugin for nil.
func (a *app) showPluginLogOf(p *plugin.Plugin) {
	v := a.showTool(toolLog)
	name := ""
	if p != nil {
		name = p.Name
	}
	v.log.plugin = name
	v.log.list.ScrollToEnd()
}

// logShown draws the window anew as lines come, while it shows the log.
func (a *app) logShown() {
	for _, col := range a.cols {
		if t := col.tab(); t != nil && t.tool != nil && t.tool.kind == toolLog {
			a.invalidate()
			return
		}
	}
}

// toolViewUI draws the tab of a tool.
func (a *app) toolViewUI(c *ui.Context, v *toolView) {
	p := a.pal
	pane := ui.Column(c.Key("tool")).Bind(&v.pane).Fill().Background(p.editor).Focusable().FocusRing(false)
	pane.Children(func() {
		if plugins == nil {
			a.toolEmpty(c, "tool-none", iconPlug, locales.PluginsNotStarted(), "", nil)
			return
		}
		if v.kind == toolLog {
			a.logViewUI(c, v)
		} else {
			a.runningViewUI(c, v)
		}
	})
}

// toolBar is the bar along the top of a tool, over its content.
func (a *app) toolBar(c *ui.Context, body func()) {
	p := a.pal
	ui.Row(c.Key("tool-bar")).Height(40).Padding(0, 10, 0, 14).Gap(8).AlignItems(ui.Center).Shrink(0).
		BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(body)
}

// toolButton is a button of a tool's bar: an icon and its label.
func (a *app) toolButton(c *ui.Context, key string, icon *ui.SVG, label string) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c.Key(key)).Height(26).Padding(0, 9, 0, 7).Gap(6).Radius(6).AlignItems(ui.Center).Shrink(0).
		Label(label).Tooltip(label)
	col := p.muted
	if b.Hovered() || b.Pressed() {
		b.Background(p.hover)
		col = p.text
	}
	b.Transition(hoverFade)
	pressPose(b)
	b.Children(func() {
		ui.Icon(c.Key("icon"), icon).FontSize(13).TextColor(col).ContentTransition(ui.ContentTransition{})
		ui.Text(c.Key("label"), label).FontSize(12).TextColor(col).SingleLine().Transition(hoverFade)
	})
	return b
}

// toolEmpty is what a tool shows of nothing: a badge of icon over the
// text, and a button of action, when it has one.
func (a *app) toolEmpty(c *ui.Context, key string, icon *ui.SVG, text, action string, do func()) {
	p := a.pal
	ui.Column(c.Key(key)).Grow(1).Padding(24).Gap(12).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
		popUp(ui.Box(c.Key("badge")).Size(44, 44).Radius(22).Background(p.text.Alpha(0.06)).Justify(ui.Center).AlignItems(ui.Center)).
			Children(func() {
				ui.Icon(c, icon).FontSize(19).TextColor(p.muted)
			})
		ui.Text(c, text).FontSize(12.5).LineHeight(1.45).TextColor(p.muted).TextAlign(ui.Center).MaxWidth(360)
		if action != "" {
			b := ui.Button(c.Key("action"), action).FontSize(12).Padding(3, 12)
			pressPose(b)
			if b.Clicked() {
				do()
			}
		}
	})
}

// Running Plugins

// runningEvery is how often Running Plugins reads how the plugins are
// doing, while it shows.
const runningEvery = time.Second

// memSamples is how many readings of its memory a plugin's spark line
// shows: the last half minute.
const memSamples = 30

// runCol is a column of Running Plugins.
type runCol int

const (
	runName runCol = iota
	runVersion
	runState
	runCPU
	runMemory
	runStarted
	runCrashes
	runError
)

// runColumn is how a column of Running Plugins shows: its width, 0 for
// a share of the room the others leave, whether its values are numbers,
// aligned at the end, and the least width of the table that shows it.
type runColumn struct {
	col      runCol
	width    float32
	end      bool
	least    float32
	sortable bool
}

var runColumns = [...]runColumn{
	{runName, 0, false, 0, true},
	{runVersion, 78, false, 700, true},
	{runState, 96, false, 0, true},
	{runCPU, 78, true, 0, true},
	{runMemory, 136, true, 480, true},
	{runStarted, 82, true, 800, true},
	{runCrashes, 74, true, 600, true},
	{runError, 0, false, 940, false},
}

// runActionsW is the width of a row's buttons, at its end.
const runActionsW = 4*24 + 3*2 + 8

// runningView is what Running Plugins keeps: the plugins as last read,
// at, sorted by sort, and the memory each held of late.
type runningView struct {
	rows    []runRow
	at      time.Time
	sort    runCol
	desc    bool
	hist    map[string]*memHistory
	active  int
	total   int64
	palette *palette
}

// runRow is a plugin as Running Plugins read it, its values as they show.
type runRow struct {
	pl                                 *plugin.Plugin
	st                                 plugin.Stats
	rank                               int
	state                              string
	cpu, mem, started, crashes, errMsg string
}

// memHistory is the memory a plugin held at its last readings, in a ring.
type memHistory struct {
	vals [memSamples]float32
	n    int
	pts  [memSamples][2]float32
}

func (h *memHistory) add(v float32) {
	if h.n == memSamples {
		copy(h.vals[:], h.vals[1:])
		h.vals[memSamples-1] = v
		return
	}
	h.vals[h.n] = v
	h.n++
}

// refresh reads how the plugins are doing.
func (r *runningView) refresh() {
	r.rows = r.rows[:0]
	r.active, r.total = 0, 0
	all := plugins.reg.Plugins()
	for name := range r.hist {
		if !slices.ContainsFunc(all, func(p *plugin.Plugin) bool { return p.Name == name }) {
			delete(r.hist, name)
		}
	}
	for _, pl := range all {
		st := pl.Stats()
		row := runRow{pl: pl, st: st, cpu: "—", mem: "—", started: "—", crashes: strconv.Itoa(st.Crashes), errMsg: firstLine(st.Err)}
		row.rank, row.state = stateOf(st)
		if st.Active {
			r.active++
			r.total += st.Memory
			row.cpu = shortDuration(st.CPU)
			row.mem = byteSize(st.Memory)
			if !st.StartedAt.IsZero() {
				row.started = st.StartedAt.Format("15:04:05")
			}
		}
		h := r.hist[pl.Name]
		if h == nil {
			h = &memHistory{}
			r.hist[pl.Name] = h
		}
		if st.Active {
			h.add(float32(st.Memory))
		} else {
			h.add(0)
		}
		r.rows = append(r.rows, row)
	}
	r.sortRows()
}

// stateOf is the state of a plugin in a word, and its place as the rows
// are sorted by it: the plugins running first, those in trouble next.
func stateOf(st plugin.Stats) (int, string) {
	switch st.State {
	case plugin.StateRunning:
		return 0, locales.RunningStateRunning()
	case plugin.StateCrashed:
		return 1, locales.RunningStateCrashed()
	case plugin.StateOff:
		return 5, locales.PluginsOff()
	case plugin.StateStatic:
		return 4, locales.RunningStateStatic()
	}
	// Starting, or waiting for its first call: in trouble when it has
	// failed, or cannot be read.
	if st.Err != "" {
		return 2, locales.PluginsIdle()
	}
	return 3, locales.PluginsIdle()
}

// stateColor is the color of a row's dot and its state.
func (r *runRow) stateColor(p *palette) ui.Color {
	switch r.rank {
	case 0:
		return p.addedFg
	case 1, 2:
		return p.dangerFg
	case 4:
		return p.accent
	}
	return p.faint
}

// sortRows sorts the rows by the column chosen, then by their names.
func (r *runningView) sortRows() {
	slices.SortStableFunc(r.rows, func(x, y runRow) int {
		n := 0
		switch r.sort {
		case runVersion:
			n = strings.Compare(x.pl.Version, y.pl.Version)
		case runState:
			n = x.rank - y.rank
		case runCPU:
			n = cmpOf(x.st.CPU, y.st.CPU)
		case runMemory:
			n = cmpOf(x.st.Memory, y.st.Memory)
		case runStarted:
			n = x.st.StartedAt.Compare(y.st.StartedAt)
		case runCrashes:
			n = x.st.Crashes - y.st.Crashes
		}
		if r.desc {
			n = -n
		}
		if n == 0 {
			n = strings.Compare(strings.ToLower(x.pl.Title()), strings.ToLower(y.pl.Title()))
			if r.desc && r.sort == runName {
				n = -n
			}
		}
		return n
	})
}

func cmpOf[T int64 | time.Duration](x, y T) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

// colTitle is the head of a column.
func (col runCol) title() string {
	switch col {
	case runName:
		return locales.RunningColPlugin()
	case runVersion:
		return locales.RunningColVersion()
	case runState:
		return locales.RunningColState()
	case runCPU:
		return locales.RunningColCPU()
	case runMemory:
		return locales.RunningColMemory()
	case runStarted:
		return locales.RunningColStarted()
	case runCrashes:
		return locales.RunningColCrashes()
	}
	return locales.RunningColError()
}

// runningViewUI is Running Plugins: its bar, then the table.
func (a *app) runningViewUI(c *ui.Context, v *toolView) {
	r := &v.run
	// How they are doing is read every runningEvery while it shows, and
	// not once it does not: the next frame asks for no more.
	if now := c.Now(); now.Sub(r.at) >= runningEvery || r.palette != a.pal {
		r.refresh()
		r.at, r.palette = now, a.pal
		c.After(runningEvery)
	} else {
		c.After(runningEvery - now.Sub(r.at))
	}
	p := a.pal
	a.toolBar(c, func() {
		ui.Icon(c, iconActivity).FontSize(14).TextColor(p.accent).Shrink(0)
		ui.Text(c, locales.RunningCount(len(r.rows))).FontSize(12.5).FontWeight(600).TextColor(p.text).SingleLine().Shrink(0)
		dot := func() { ui.Text(c, "·").FontSize(12).TextColor(p.faint).Shrink(0) }
		dot()
		ui.Row(c.Key("active")).Gap(5).AlignItems(ui.Center).Shrink(0).Children(func() {
			col := p.faint
			if r.active > 0 {
				col = p.addedFg
			}
			ui.Box(c.Key("dot")).Size(6, 6).Radius(3).Background(col).Transition(hoverFade)
			ui.Text(c.Key("n"), locales.RunningActive(strconv.Itoa(r.active))).FontSize(12).FontFeatures("tnum").TextColor(p.muted).SingleLine().
				ContentTransition(ui.ContentTransition{Numeric: true})
		})
		if r.total > 0 {
			dot()
			ui.Text(c.Key("total"), locales.RunningMemoryTotal(byteSize(r.total))).FontSize(12).
				FontFeatures("tnum").TextColor(p.muted).SingleLine().Shrink(1).MinWidth(0).ContentTransition(ui.ContentTransition{Numeric: true})
		}
		ui.Spacer(c)
		if a.toolButton(c, "reload", iconRefresh, locales.CmdPluginsReload()).Clicked() {
			go plugins.reloadAll()
		}
		if a.toolButton(c, "manage", iconSettings, strings.TrimSuffix(locales.CmdPluginsManage(), "…")).Clicked() {
			a.post(func() { a.host.showSettings(prefsPlugins, "") })
		}
	})
	if len(r.rows) == 0 {
		a.toolEmpty(c, "running-none", iconPlug, locales.RunningNone(), locales.CmdPluginsManage(), func() { a.host.showSettings(prefsPlugins, "") })
		return
	}
	table := ui.Column(c.Key("running-table")).Grow(1).MinHeight(0)
	width := table.Bounds().W
	table.Children(func() {
		a.runningHead(c, r, width)
		ui.Scroll(c.Key("running-rows")).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c.Key("rows")).Padding(4, 8, 12, 8).Gap(1).Children(func() {
				for i := range r.rows {
					a.runningRow(c, r, &r.rows[i], width)
				}
			})
		})
	})
}

// runShows reports whether a table width wide shows the column, all
// before the first layout gives it its width.
func runShows(col *runColumn, width float32) bool { return width == 0 || width >= col.least }

// runCell is a cell of the table, in the column's place.
func runCell(c *ui.Context, col *runColumn) ui.Element {
	cell := ui.Row(c.Key(col.col)).Padding(0, 8).Gap(6).AlignItems(ui.Center).MinWidth(0)
	if col.width > 0 {
		cell.Width(col.width).Shrink(0)
	} else {
		cell.Grow(1).Basis(0).Shrink(1)
	}
	if col.col == runError {
		cell.Grow(1.3)
	}
	if col.end {
		cell.Justify(ui.End)
	}
	return cell
}

// runningHead is the head of the table: a click on a column sorts by
// it, and again the other way.
func (a *app) runningHead(c *ui.Context, r *runningView, width float32) {
	p := a.pal
	ui.Row(c.Key("running-head")).Height(30).Padding(0, 20, 0, 20).AlignItems(ui.Stretch).Shrink(0).
		BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
		for i := range runColumns {
			col := &runColumns[i]
			if !runShows(col, width) {
				continue
			}
			on := r.sort == col.col
			if !col.sortable {
				runCell(c, col).Children(func() {
					ui.Text(c, col.col.title()).FontSize(11).FontWeight(600).TextColor(p.faint).SingleLine()
				})
				continue
			}
			b := ui.ButtonBase(c.Key(col.col)).Padding(0, 8).Gap(4).AlignItems(ui.Center).MinWidth(0).Label(col.col.title())
			if col.width > 0 {
				b.Width(col.width).Shrink(0)
			} else {
				b.Grow(1).Basis(0).Shrink(1)
			}
			if col.end {
				b.Justify(ui.End)
			} else {
				b.Justify(ui.Start)
			}
			tint := p.faint
			if on || b.Hovered() {
				tint = p.text
			}
			b.Children(func() {
				ui.Text(c.Key("title"), col.col.title()).FontSize(11).FontWeight(600).TextColor(tint).SingleLine().Shrink(1).MinWidth(0).
					Transition(hoverFade)
				if on {
					turn := float32(0)
					if r.desc {
						turn = 180
					}
					ui.Icon(c.Key("arrow"), iconArrowUp).FontSize(11).TextColor(p.accent).Shrink(0).
						AnimatePose(ui.Pose{Rotate: turn}, ui.Snappy).Transition(revealFade)
				}
			})
			if b.Clicked() {
				if on {
					r.desc = !r.desc
				} else {
					// Numbers read best the largest first.
					r.sort, r.desc = col.col, col.end
				}
				r.sortRows()
			}
		}
		ui.Box(c.Key("actions")).Width(runActionsW).Shrink(0)
	})
}

// runRowMotion is how a row of the table follows the pointer, and moves
// to its place as the rows are sorted again.
var runRowMotion = ui.ElementTransition{Duration: motionShow, Position: true, Colors: true}

// runningRow is a plugin of the table, with its buttons at the end while
// the pointer is over it.
func (a *app) runningRow(c *ui.Context, r *runningView, row *runRow, width float32) {
	p := a.pal
	pl := row.pl
	el := ui.Row(c.Key(pl.Name)).Height(38).Padding(0, 12).Radius(6).AlignItems(ui.Center)
	hovered := el.Hovered()
	if hovered {
		el.Background(p.hover)
	}
	el.Transition(runRowMotion)
	el.ContextMenu(func(m *ui.Menu) { a.runningMenu(m, row) })
	mono := a.settings.FontFamily
	tint := row.stateColor(p)
	el.Children(func() {
		for i := range runColumns {
			col := &runColumns[i]
			if !runShows(col, width) {
				continue
			}
			runCell(c, col).Children(func() {
				num := func(key, s string, color ui.Color) {
					ui.Text(c.Key(key), s).FontSize(12).FontFeatures("tnum").TextColor(color).SingleLine().Shrink(0).
						ContentTransition(ui.ContentTransition{Numeric: true})
				}
				switch col.col {
				case runName:
					ui.Box(c.Key("dot")).Size(7, 7).Radius(3.5).Shrink(0).Background(tint).Transition(hoverFade)
					ui.Text(c.Key("title"), pl.Title()).FontSize(13).FontWeight(500).TextColor(p.text).SingleLine().Shrink(1).MinWidth(24)
					if pl.Title() != pl.Name {
						ui.Text(c.Key("name"), pl.Name).Font(mono).FontSize(11).TextColor(p.faint).SingleLine().Shrink(1000).MinWidth(0)
					}
				case runVersion:
					ui.Text(c, pl.Version).Font(mono).FontSize(11.5).TextColor(p.muted).SingleLine().Shrink(1).MinWidth(0)
				case runState:
					ui.Text(c.Key("state"), row.state).FontSize(12).TextColor(tint).SingleLine().Shrink(1).MinWidth(0).
						ContentTransition(ui.ContentTransition{})
				case runCPU:
					num("cpu", row.cpu, p.text)
				case runMemory:
					a.sparkline(c, r.hist[pl.Name], tint)
					num("mem", row.mem, p.text)
				case runStarted:
					num("started", row.started, p.muted)
				case runCrashes:
					color := p.faint
					if row.st.Crashes > 0 {
						color = p.dangerFg
					}
					num("crashes", row.crashes, color)
				case runError:
					if row.errMsg != "" {
						ui.Icon(c, iconOctagonAlert).FontSize(12).TextColor(p.dangerFg).Shrink(0)
						ui.Text(c, row.errMsg).FontSize(12).TextColor(p.dangerFg).SingleLine().Shrink(1).MinWidth(0).Tooltip(row.st.Err)
					}
				}
			})
		}
		ui.Row(c.Key("actions")).Width(runActionsW).Shrink(0).Gap(2).Justify(ui.End).AlignItems(ui.Center).Children(func() {
			if !hovered {
				return
			}
			acts := pl.HasCode() && !pl.Script
			off := row.st.Disabled
			if acts && !off && a.rowAction(c, "restart", iconRefresh, locales.RunningRestart()).Clicked() {
				go plugins.restart(pl.Name)
			}
			if !pl.Script {
				label := locales.RunningTurnOff()
				if off {
					label = locales.RunningTurnOn()
				}
				if a.rowAction(c, "toggle", iconPower, label).Clicked() {
					a.post(func() { plugins.setEnabled(pl.Name, off) })
				}
			}
			if a.rowAction(c, "details", iconInfo, locales.RunningDetails()).Clicked() {
				a.post(func() { a.host.showSettings(prefsPlugins, pluginDetailKey+pl.Name) })
			}
			if a.rowAction(c, "log", iconScrollText, locales.RunningShowLog()).Clicked() {
				a.post(func() { a.showPluginLogOf(pl) })
			}
		})
	})
}

// runningMenu is the menu of a row of the table: what its buttons do.
func (a *app) runningMenu(m *ui.Menu, row *runRow) {
	pl, off := row.pl, row.st.Disabled
	if m.Item(locales.RunningRestart()).Disabled(!pl.HasCode() || pl.Script || off).Chosen() {
		go plugins.restart(pl.Name)
	}
	if !pl.Script {
		label := locales.RunningTurnOff()
		if off {
			label = locales.RunningTurnOn()
		}
		if m.Item(label).Chosen() {
			a.post(func() { plugins.setEnabled(pl.Name, off) })
		}
	}
	m.Separator()
	if m.Item(locales.RunningDetails()).Chosen() {
		a.post(func() { a.host.showSettings(prefsPlugins, pluginDetailKey+pl.Name) })
	}
	if m.Item(locales.RunningShowLog()).Chosen() {
		a.post(func() { a.showPluginLogOf(pl) })
	}
}

// pluginDetailKey starts the key of showSettings that opens the details
// of a plugin, by its name after it.
const pluginDetailKey = "plugin:"

// rowAction is a small button of a row, showing an icon, its tooltip
// the label.
func (a *app) rowAction(c *ui.Context, key string, icon *ui.SVG, label string) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c.Key(key)).Size(24, 24).Radius(5).Justify(ui.Center).AlignItems(ui.Center).Shrink(0).
		Label(label).Tooltip(label).Transition(revealFade)
	col := p.muted
	if b.Hovered() {
		b.Background(p.hover)
		col = p.text
	}
	pressPose(b)
	b.Children(func() { ui.Icon(c, icon).FontSize(13).TextColor(col) })
	return b
}

// sparkline draws the memory a plugin held at its last readings, scaled
// to the most it held, as a line over a faint fill.
func (a *app) sparkline(c *ui.Context, h *memHistory, col ui.Color) {
	box := ui.Box(c.Key("spark")).Size(40, 14).Shrink(0)
	if h == nil || h.n < 2 {
		return
	}
	box.Draw(func(pt *ui.Painter, r ui.Rect) {
		var top float32
		for _, v := range h.vals[:h.n] {
			top = max(top, v)
		}
		if top == 0 {
			pt.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 1, W: r.W, H: 1}, col.Alpha(0.25), 0)
			return
		}
		step := r.W / float32(memSamples-1)
		x0 := r.X + r.W - step*float32(h.n-1)
		for i, v := range h.vals[:h.n] {
			x := x0 + step*float32(i)
			y := r.Y + r.H - 1 - (r.H-2)*v/top
			pt.Fill(ui.Rect{X: x - step/2, Y: y, W: step, H: r.Y + r.H - y}, col.Alpha(0.12), 0)
			h.pts[i] = [2]float32{x, y}
		}
		pt.StrokePolyline(h.pts[:h.n], 1.25, col.Alpha(0.85))
	})
}

// Plugin Log

// logLevel is a level of the lines of the log, as a bit of the levels
// shown.
type logLevel uint8

const (
	logDebug logLevel = 1 << iota
	logInfo
	logWarn
	logError
	logAllLevels = logDebug | logInfo | logWarn | logError
)

// logLevels are the levels in the order the bar shows them.
var logLevels = [...]logLevel{logError, logWarn, logInfo, logDebug}

func levelOf(s string) logLevel {
	switch s {
	case "error":
		return logError
	case "warn":
		return logWarn
	case "debug":
		return logDebug
	}
	return logInfo
}

func (l logLevel) title() string {
	switch l {
	case logError:
		return locales.LogLevelError()
	case logWarn:
		return locales.LogLevelWarn()
	case logDebug:
		return locales.LogLevelDebug()
	}
	return locales.LogLevelInfo()
}

func (l logLevel) color(p *palette) ui.Color {
	switch l {
	case logError:
		return p.dangerFg
	case logWarn:
		return p.modifiedFg
	case logDebug:
		return p.faint
	}
	return p.accent
}

// logRowH is the height of a line of the log.
const logRowH = 22

// logFlash is how long a line new to the log stands out as it comes.
const logFlash = 900 * time.Millisecond

// logFollow is how the log glides to a line that comes while its end
// shows.
var logFollow = ui.Animation{Duration: motionHover, Ease: ui.EaseOut}

// logView is what the Plugin Log keeps: the filters, and the numbers of
// the lines they let through, oldest first, of the lines up to upTo.
type logView struct {
	plugin string
	levels logLevel
	query  string
	shown  []uint64
	upTo   uint64
	// made is what shown was made for: the filters and the log's floor,
	// which clearing it moves.
	made logFilter
	list ui.ListState
	// came are the lines that came of late, as they stand out: each batch
	// from the line it starts at, until the next. seen is how many lines
	// were shown as the end last showed, and copied when Copy last copied.
	came   []logBatch
	seen   int
	copied time.Time
}

// logBatch is lines that came in the same frame, from the line of number
// from, at.
type logBatch struct {
	from uint64
	at   time.Time
}

// logFilter is what the lines of the log shown follow.
type logFilter struct {
	plugin string
	levels logLevel
	query  string
	floor  uint64
}

// sync takes the lines that came since it last did, those the filters
// let through, and starts again as the filters change: under the lock of
// the log, which it holds as briefly as the new lines take. now is the
// frame's time, which the lines that came stand out from.
func (v *logView) sync(h *pluginHost, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := &h.log
	f := logFilter{v.plugin, v.levels, strings.TrimSpace(v.query), r.floor}
	fresh := v.made != (logFilter{}) || v.upTo > 0
	if f != v.made {
		// Made again: none of the lines are new to it.
		v.made, v.shown, v.upTo, v.seen, v.came = f, v.shown[:0], r.first(), math.MaxInt, v.came[:0]
		fresh = false
	}
	for len(v.came) > 0 && now.Sub(v.came[0].at) >= logFlash {
		v.came = v.came[1:]
	}
	first := r.first()
	// The lines the ring let go of, at the start.
	drop := 0
	for drop < len(v.shown) && v.shown[drop] < first {
		drop++
	}
	v.shown = v.shown[drop:]
	v.seen = max(0, v.seen-drop)
	had := len(v.shown)
	for n := max(v.upTo, first); n < r.next; n++ {
		if v.matches(r.at(n), &f) {
			v.shown = append(v.shown, n)
		}
	}
	v.upTo = r.next
	if fresh && len(v.shown) > had {
		v.came = append(v.came, logBatch{v.shown[had], now})
	}
}

// cameAt is when the line of number n came, false for one that came
// before the lines that stand out.
func (v *logView) cameAt(n uint64) (time.Time, bool) {
	for i := len(v.came) - 1; i >= 0; i-- {
		if n >= v.came[i].from {
			return v.came[i].at, true
		}
	}
	return time.Time{}, false
}

// matches reports whether the filters let a line through.
func (v *logView) matches(l *plugin.LogLine, f *logFilter) bool {
	if f.plugin != "" && l.Plugin != f.plugin {
		return false
	}
	if f.levels&levelOf(l.Level) == 0 {
		return false
	}
	return f.query == "" || containsFold(l.Message, f.query) || containsFold(l.Plugin, f.query)
}

// containsFold reports whether sub is in s, whatever their case, without
// making a copy of either.
func containsFold(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); {
		if strings.EqualFold(s[i:i+len(sub)], sub) {
			return true
		}
		_, w := utf8.DecodeRuneInString(s[i:])
		i += w
	}
	return false
}

// line is a copy of the line of number n, false once the log let go of
// it.
func (v *logView) line(h *pluginHost, n uint64) (plugin.LogLine, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if n < h.log.first() || n >= h.log.next {
		return plugin.LogLine{}, false
	}
	return *h.log.at(n), true
}

// text is the lines shown, as the clipboard takes them.
func (v *logView) text(h *pluginHost) string {
	var b strings.Builder
	h.mu.Lock()
	defer h.mu.Unlock()
	first := h.log.first()
	for _, n := range v.shown {
		if n >= first {
			writeLogLine(&b, h.log.at(n))
		}
	}
	return b.String()
}

func writeLogLine(b *strings.Builder, l *plugin.LogLine) {
	fmt.Fprintf(b, "%s [%s] %s: %s\n", l.Time.Format("15:04:05.000"), l.Level, l.Plugin, l.Message)
}

// logViewUI is the Plugin Log: its bar of filters, then its lines, of
// which only those in view are drawn.
func (a *app) logViewUI(c *ui.Context, v *toolView) {
	p := a.pal
	l := &v.log
	l.sync(plugins, c.Now())
	a.logBar(c, l)
	h := plugins
	h.mu.Lock()
	empty := h.log.next == h.log.first()
	h.mu.Unlock()
	switch {
	case empty:
		a.toolEmpty(c, "log-none", iconScrollText, locales.PluginLogEmpty(), "", nil)
		return
	case len(l.shown) == 0:
		a.toolEmpty(c, "log-no-match", iconSearch, locales.LogNoMatch(), locales.LogClearFilters(), func() {
			l.plugin, l.levels, l.query = "", logAllLevels, ""
		})
		return
	}
	now := c.Now()
	calm := c.Preferences().ReduceMotion
	flashing := false
	mono := a.settings.FontFamily
	// The list's own marks of the row under the pointer give way to the
	// rows', which blend with how they stand out.
	base := c.Theme()
	quiet := *base
	quiet.Accent, quiet.AccentText, quiet.SurfaceHover = ui.Color{}, p.text, ui.Color{}
	ui.Box(c.Key("log-body")).Grow(1).MinHeight(0).Children(func() {
		c.SetTheme(&quiet)
		ui.List(c.Key("log-lines"), &l.list, len(l.shown), func(i int) {
			n := l.shown[i]
			line, ok := l.line(h, n)
			if !ok {
				ui.Box(c.Key("gone")).Height(logRowH)
				return
			}
			lvl := levelOf(line.Level)
			row := ui.Row(c.Key("line")).Height(logRowH).Padding(0, 14).Gap(10).AlignItems(ui.Center)
			bg := p.editor
			if lvl == logError {
				bg = p.dangerFg.Alpha(0.07).Over(bg)
			}
			// A line that came while the tab showed stands out for a moment,
			// and fades.
			if at, ok := l.cameAt(n); ok && !calm {
				since := now.Sub(at)
				k := 1 - ui.EaseOut(float32(since)/float32(logFlash))
				bg = p.accent.Alpha(0.18 * k).Over(bg)
				flashing = true
			}
			if row.Hovered() {
				bg = p.hover.Over(bg)
			}
			row.Background(bg)
			row.ContextMenu(func(m *ui.Menu) {
				if m.Item(locales.LogCopyLine()).Chosen() {
					var b strings.Builder
					writeLogLine(&b, &line)
					a.services.WriteClipboard(strings.TrimSuffix(b.String(), "\n"))
				}
				if m.Item(locales.LogOnlyPlugin(line.Plugin)).Disabled(l.plugin == line.Plugin).Chosen() {
					l.plugin = line.Plugin
				}
			})
			msg := line.Message
			multi := strings.IndexByte(msg, '\n')
			row.Children(func() {
				ui.Text(c.Key("time"), line.Time.Format("15:04:05.000")).Font(mono).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(0)
				ui.Row(c.Key("level")).Width(64).Shrink(0).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Box(c.Key("dot")).Size(6, 6).Radius(3).Shrink(0).Background(lvl.color(p))
					ui.Text(c, line.Level).Font(mono).FontSize(11.5).TextColor(lvl.color(p)).SingleLine()
				})
				ui.Text(c.Key("plugin"), line.Plugin).FontSize(12).TextColor(p.muted).SingleLine().Width(120).Shrink(0)
				if multi >= 0 {
					msg = msg[:multi] + " …"
				}
				t := ui.Text(c.Key("msg"), msg).Font(mono).FontSize(12).TextColor(p.text).SingleLine().Grow(1).Shrink(1).MinWidth(0)
				if multi >= 0 || len(msg) > 120 {
					t.Tooltip(line.Message)
				}
			})
		}).Fill().Padding(4, 0, 8, 0).Label(locales.PluginLogTitle())
		c.SetTheme(base)
		atEnd := l.list.AtEnd()
		if atEnd {
			l.seen = len(l.shown)
		}
		if !atEnd {
			a.logJump(c, l, len(l.shown)-l.seen)
		}
	})
	if flashing {
		c.AnimationFrame()
	}
}

// logBar is the bar of the log: the plugin and the levels it shows, what
// to look for, and Copy and Clear.
func (a *app) logBar(c *ui.Context, l *logView) {
	p := a.pal
	a.toolBar(c, func() {
		// The plugin the log is of, all for "".
		label := locales.LogAllPlugins()
		if l.plugin != "" {
			label = l.plugin
			if pl := plugins.reg.Plugin(l.plugin); pl != nil {
				label = pl.Title()
			}
		}
		pick := ui.ButtonBase(c.Key("plugin")).Height(26).Padding(0, 8, 0, 9).Gap(6).Radius(6).AlignItems(ui.Center).Shrink(0).
			Label(label).Background(p.surface).Border(1, p.border)
		if pick.Hovered() {
			pick.Background(p.surfaceHover)
		}
		pick.Transition(hoverFade)
		pick.Children(func() {
			ui.Icon(c, iconPlug).FontSize(12).TextColor(p.muted)
			ui.Text(c.Key("label"), label).FontSize(12).TextColor(p.text).SingleLine().MaxWidth(160)
			ui.Icon(c, iconChevronDown).FontSize(11).TextColor(p.muted)
		})
		pick.Menu(func(m *ui.Menu) {
			if m.Item(locales.LogAllPlugins()).Checked(l.plugin == "").Chosen() {
				l.plugin = ""
			}
			m.Separator()
			for _, pl := range plugins.reg.Plugins() {
				if m.Item(pl.Title()).Checked(l.plugin == pl.Name).Chosen() {
					l.plugin = pl.Name
				}
			}
		})
		// The levels, each on or off.
		ui.Row(c.Key("levels")).Height(26).Padding(2).Gap(2).Radius(7).Background(p.text.Alpha(0.05)).Shrink(0).AlignItems(ui.Center).Children(func() {
			for _, lvl := range logLevels {
				on := l.levels&lvl != 0
				b := ui.ButtonBase(c.Key(lvl)).Height(22).Padding(0, 8).Gap(5).Radius(5).AlignItems(ui.Center).
					Label(lvl.title()).Checked(on)
				switch {
				case on:
					b.Background(p.editor).Shadow(0, 1, 2, 0, p.shadow.Alpha(0.5))
				case b.Hovered():
					b.Background(p.hover)
				}
				b.Transition(hoverFade)
				pressPose(b)
				col, text := lvl.color(p), p.text
				if !on {
					col, text = p.faint.Alpha(0.6), p.faint
				}
				b.Children(func() {
					ui.Box(c.Key("dot")).Size(6, 6).Radius(3).Background(col).Transition(hoverFade)
					ui.Text(c.Key("t"), lvl.title()).FontSize(11.5).TextColor(text).SingleLine().Transition(hoverFade)
				})
				if b.Clicked() {
					l.levels ^= lvl
				}
			}
		})
		search := ui.SearchField(c.Key("query"), &l.query).Grow(1).Shrink(1).MinWidth(80).MaxWidth(320).FontSize(12).
			Label(locales.LogFilter())
		a.textKeys(search)
		ui.Spacer(c)
		ui.Text(c.Key("count"), locales.LogLines(len(l.shown))).FontSize(11.5).FontFeatures("tnum").TextColor(p.faint).SingleLine().Shrink(0).
			ContentTransition(ui.ContentTransition{Numeric: true})
		copied := !l.copied.IsZero() && c.Now().Sub(l.copied) < 1500*time.Millisecond
		icon, label := iconCopy, locales.LogCopy()
		if copied {
			icon, label = iconCheck, locales.LogCopied()
			c.After(1500*time.Millisecond - c.Now().Sub(l.copied))
		}
		if a.toolButton(c, "copy", icon, label).Disabled(len(l.shown) == 0).Clicked() {
			a.services.WriteClipboard(l.text(plugins))
			l.copied = c.Now()
		}
		if a.toolButton(c, "clear", iconTrash, locales.LogClear()).Clicked() {
			plugins.mu.Lock()
			plugins.log.clear()
			plugins.mu.Unlock()
			l.seen = 0
			for _, other := range plugins.apps() {
				other.invalidate()
			}
		}
	})
}

// logJump is the pill over the end of the log while it is scrolled up
// from it: a click goes back to the end, which it follows again. It
// tells how many lines came since.
func (a *app) logJump(c *ui.Context, l *logView, fresh int) {
	p := a.pal
	label := locales.LogJump()
	if fresh > 0 {
		label = locales.LogNewLines(fresh)
	}
	ui.Row(c.Key("log-jump")).Absolute().Left(0).Right(0).Bottom(14).Justify(ui.Center).PassThrough().Transition(popUpPill).Children(func() {
		b := ui.ButtonBase(c.Key("pill")).Height(28).Padding(0, 12, 0, 10).Gap(6).Radius(14).AlignItems(ui.Center).
			Label(locales.LogJump()).Tooltip(locales.LogJump()).Background(p.fill).Shadow(0, 4, 14, 0, p.shadow)
		switch {
		case b.Pressed():
			b.Background(p.fillPressed)
		case b.Hovered():
			b.Background(p.fillHover)
		}
		b.Transition(hoverFade)
		pressPose(b)
		b.Children(func() {
			ui.Icon(c, iconArrowDown).FontSize(13).TextColor(p.fillText)
			ui.Text(c.Key("label"), label).FontSize(12).FontWeight(500).FontFeatures("tnum").TextColor(p.fillText).SingleLine().
				ContentTransition(ui.ContentTransition{Numeric: true})
		})
		if b.Clicked() {
			l.list.ScrollToEnd()
		}
	})
}

// popUpPill is how the pill over the end of the log comes and goes: up a
// little as it fades in, and down as it fades out.
var popUpPill = ui.ElementTransition{Duration: motionShow, Colors: true, Enter: &ui.Motion{Y: 8}, Exit: &ui.Motion{Y: 8}}
