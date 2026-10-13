package main

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

// prompt asks for a name, as for a new file: done gets it, and returns
// why it cannot be taken, "" to close the prompt.
type prompt struct {
	title, hint string
	value       string
	err         string
	done        func(value string) string
	selected    bool
}

func (a *app) promptView(c *ui.Context) {
	pr := a.prompt
	if pr == nil {
		return
	}
	p := a.pal
	submit := func() {
		if msg := pr.done(pr.value); msg != "" {
			pr.err = msg
			return
		}
		a.prompt = nil
	}
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "prompt:"+pr.title)
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(420).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Padding(14, 16).Gap(10).Radius(10), true).
				Role(ui.RoleDialog).Label(pr.title)
			box.Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, pr.title).FontSize(13).FontWeight(600).TextColor(p.text)
					ui.Text(c, pr.hint).FontSize(12).TextColor(p.faint).SingleLine().Shrink(1)
				})
				in := a.textKeys(ui.TextInput(c.Key("prompt-"+pr.title), &pr.value)).AutoFocus().FontSize(13).Label(pr.title)
				if !pr.selected {
					// Select the name without its extension, as Finder does.
					pr.selected = true
					n := len([]rune(pr.value))
					if ext := filepath.Ext(pr.value); ext != "" && ext != pr.value {
						n -= len([]rune(ext))
					}
					in.SetTextSelection(0, n)
				}
				if pr.err != "" {
					in.Error(pr.err)
					ui.Text(c, pr.err).FontSize(12).TextColor(p.danger)
				}
				if in.Changed() {
					pr.err = ""
				}
				if in.Submitted() {
					submit()
				}
				ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c, "Cancel").Clicked() {
						a.prompt = nil
					}
					if ui.PrimaryButton(c, strings.TrimSuffix(pr.title, "…")).Disabled(strings.TrimSpace(pr.value) == "").Clicked() {
						submit()
					}
				})
			})
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.prompt = nil
			}
		})
	})
}

// confirm asks whether to go on with something, as discarding changes:
// run runs once the button named ok is chosen.
type confirm struct {
	title, message, ok string
	danger             bool
	run                func()
	alternate          string
	runAlternate       func()
}

func (a *app) confirmView(c *ui.Context) {
	cf := a.confirm
	if cf == nil {
		return
	}
	p := a.pal
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "confirm:"+cf.title)
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(420).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Padding(14, 16).Gap(10).Radius(10), true).
				Role(ui.RoleDialog).Label(cf.title)
			box.Children(func() {
				ui.Text(c, cf.title).FontSize(13).FontWeight(600).TextColor(p.text)
				if cf.message != "" {
					ui.Text(c, cf.message).FontSize(12.5).TextColor(p.muted)
				}
				buttons := ui.Row(c).Gap(8).Justify(ui.End)
				if cf.alternate != "" {
					buttons = ui.Column(c).Gap(8)
				}
				buttons.Children(func() {
					if ui.Button(c, "Cancel").Clicked() {
						a.confirm = nil
					}
					if cf.alternate != "" && ui.Button(c, cf.alternate).Clicked() {
						a.confirm = nil
						cf.runAlternate()
					}
					ok := ui.PrimaryButton(c, cf.ok).AutoFocus()
					if cf.danger {
						bg := p.dangerFill
						switch {
						case ok.Pressed():
							bg = p.dangerPressed
						case ok.Hovered():
							bg = p.dangerHover
						}
						ok.Background(bg).TextColor(p.dangerText)
					}
					if ok.Clicked() {
						a.confirm = nil
						cf.run()
					}
				})
			})
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.confirm = nil
			}
		})
	})
}
