package main

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/workspace"
	"mayoi/locales"
)

// pickItem is an item of a plugin's pick box.
type pickItem struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Icon        string `json:"icon,omitempty"`
}

// pluginPicker is the box a plugin's window.pick shows, found by label as
// quick open finds files.
type pluginPicker struct {
	open        bool
	query       string
	sel         int
	moved       bool
	items       []pickItem
	labels      []string
	placeholder string
	results     []workspace.Match
	shown       string
	stale       bool
	done        func(i int) // the item's index, -1 for none
}

// openPluginPicker shows a plugin's items; done gets the index of the one
// chosen, or -1. A box already open answers -1 first.
func (a *app) openPluginPicker(items []pickItem, placeholder string, done func(int)) {
	if pk := &a.pluginPick; pk.open && pk.done != nil {
		pk.done(-1)
	}
	labels := make([]string, len(items))
	for i, it := range items {
		labels[i] = it.Label
		if it.Description != "" {
			labels[i] += " " + it.Description
		}
	}
	a.pluginPick = pluginPicker{open: true, stale: true, items: items, labels: labels, placeholder: placeholder, done: done}
}

func (a *app) closePluginPicker(chosen int) {
	pk := &a.pluginPick
	if !pk.open {
		return
	}
	done := pk.done
	pk.open, pk.done = false, nil
	if a.focus != paneTerminal {
		a.focusColumn()
	}
	if done != nil {
		done(chosen)
	}
}

func (a *app) pluginPickerView(c *ui.Context) {
	pk := &a.pluginPick
	if !pk.open {
		return
	}
	p := a.pal
	if pk.stale || pk.shown != pk.query {
		q := strings.TrimSpace(pk.query)
		if q == "" {
			pk.results = pk.results[:0]
			for i := range pk.items {
				pk.results = append(pk.results, workspace.Match{Index: i})
			}
		} else {
			pk.results = workspace.Fuzzy(pk.labels, q, 200)
		}
		pk.shown, pk.stale, pk.sel = pk.query, false, 0
	}
	choose := func(i int) {
		if i >= 0 && i < len(pk.results) {
			a.closePluginPicker(pk.results[i].Index)
		}
	}
	ui.Overlay(c, func() {
		back := a.dialogBack(c)
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(560).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Radius(10), true).Clip().
				Role(ui.RoleDialog).Label(pk.placeholder)
			box.Children(func() {
				ui.Row(c).Padding(10, 14).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
					ui.Icon(c, iconPlug).FontSize(15).TextColor(p.muted)
					in := a.textKeys(ui.TextInputBase(c.Key("plugin-pick-input"), &pk.query)).Grow(1).FontSize(14).AutoFocus().
						Placeholder(pk.placeholder).Label(pk.placeholder)
					if in.Shortcut(0, ui.KeyDown) {
						pk.sel, pk.moved = min(pk.sel+1, len(pk.results)-1), true
					}
					if in.Shortcut(0, ui.KeyUp) {
						pk.sel, pk.moved = max(pk.sel-1, 0), true
					}
					if in.Submitted() {
						sel := pk.sel
						a.post(func() { choose(sel) })
					}
				})
				if len(pk.results) > 0 {
					ui.Scroll(c.Key("plugin-pick-results")).MaxHeight(380).Padding(6).Children(func() {
						for i, m := range pk.results {
							it := pk.items[m.Index]
							row := ui.ButtonBase(c.Key("plugin-pick:"+strconv.Itoa(m.Index))).Padding(5, 10).Gap(10).Radius(6).
								Justify(ui.Start).AlignItems(ui.Center).Label(it.Label)
							if i != pk.sel && row.Hovered() {
								row.Background(p.hover)
							}
							if i == pk.sel && pk.moved {
								row.ScrollIntoView()
							}
							if row.Clicked() {
								at := i
								a.post(func() { choose(at) })
							}
							row.Transition(hoverFade)
							row.Children(func() {
								a.pickMark(c, i == pk.sel)
								if it.Icon != "" {
									ui.Icon(c, namedIcon(it.Icon)).FontSize(14).TextColor(p.accent)
								}
								ui.Column(c).Gap(1).Shrink(1).Children(func() {
									ui.Row(c).Gap(8).AlignItems(ui.Baseline).Children(func() {
										ui.RichText(c, a.matchSpans(it.Label, 0, m.Positions, p.text)...).FontSize(13).SingleLine().Shrink(0)
										if it.Description != "" {
											ui.Text(c, it.Description).FontSize(12).TextColor(p.faint).SingleLine().Shrink(1)
										}
									})
									if it.Detail != "" {
										ui.Text(c, it.Detail).FontSize(11.5).TextColor(p.muted).SingleLine()
									}
								})
							})
						}
					})
				} else {
					ui.Text(c, locales.PluginPickNoMatches()).FontSize(13).TextColor(p.muted).Padding(12, 16)
				}
				pk.moved = false
			})
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closePluginPicker(-1)
			}
		})
	})
}

// namedIcon is the icon a plugin names, of Mayoi's own: a plug for a name
// it does not know.
func namedIcon(name string) *ui.SVG {
	switch strings.ToLower(name) {
	case "check", "done":
		return iconCheck
	case "x", "close":
		return iconX
	case "plus", "add":
		return iconPlus
	case "minus":
		return iconMinus
	case "file":
		return iconFile
	case "code", "file-code":
		return iconFileCode
	case "text", "file-text":
		return iconFileText
	case "folder":
		return iconFolder
	case "search":
		return iconSearch
	case "star":
		return iconStar
	case "info":
		return iconInfo
	case "warning", "alert":
		return iconTriangleAlert
	case "error":
		return iconOctagonAlert
	case "trash", "delete":
		return iconTrash
	case "refresh", "reload":
		return iconRefresh
	case "settings", "gear":
		return iconSettings
	case "terminal":
		return iconTerminal
	case "branch", "git":
		return iconBranch
	case "commit":
		return iconCommit
	case "tag":
		return iconTag
	case "clock", "time":
		return iconClock
	case "lightbulb", "idea":
		return iconLightbulb
	case "sparkles", "ai":
		return iconSparkles
	case "comment", "message":
		return iconComment
	case "copy":
		return iconCopy
	case "external", "link":
		return iconExternal
	case "pencil", "edit":
		return iconPencil
	case "list", "tree":
		return iconListTree
	case "function":
		return iconFunction
	case "hash":
		return iconHash
	case "lock":
		return iconLock
	case "server":
		return iconServer
	case "cloud":
		return iconCloud
	case "send":
		return iconSend
	case "keyboard":
		return iconKeyboard
	case "box", "package":
		return iconBox
	case "braces", "json":
		return iconBraces
	case "download":
		return iconDownload
	case "image":
		return iconImage
	}
	return iconPlug
}
