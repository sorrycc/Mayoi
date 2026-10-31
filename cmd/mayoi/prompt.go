package main

import (
	"cmp"
	"mayoi/locales"
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
	// optional lets the value be empty.
	optional bool
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
					ui.Text(c, pr.err).FontSize(12).TextColor(p.dangerFg)
				}
				if in.Changed() {
					pr.err = ""
				}
				if in.Submitted() {
					submit()
				}
				ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c, locales.ButtonCancel()).Clicked() {
						a.prompt = nil
					}
					if ui.PrimaryButton(c, strings.TrimSuffix(pr.title, "…")).Disabled(!pr.optional && strings.TrimSpace(pr.value) == "").Clicked() {
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
// run runs once the button named ok is chosen, runAlternate once the
// one named alternate is. okShort and alternateShort name them when the
// window is too narrow for three buttons.
type confirm struct {
	title, message, ok string
	danger             bool
	run                func()
	alternate          string
	runAlternate       func()
	okShort            string
	alternateShort     string
	// choices are the ways to go on that the dialog offers, choice the
	// one chosen, whose message it shows: a danger one makes the button
	// red.
	choices []confirmChoice
	choice  int
}

// confirmChoice is a way to go on that a confirm offers.
type confirmChoice struct {
	label, message string
	danger         bool
}

func (a *app) confirmView(c *ui.Context) {
	cf := a.confirm
	if cf == nil {
		return
	}
	p := a.pal
	width := float32(420)
	viewport, _ := c.Size()
	okLabel, alternate := cf.ok, cf.alternate
	compact := cf.alternate != "" && viewport < 560
	if compact {
		okLabel, alternate = cmp.Or(cf.okShort, okLabel), cmp.Or(cf.alternateShort, alternate)
	}
	if cf.alternate != "" {
		width = 560
	}
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "confirm:"+cf.title)
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(width).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Padding(14, 16).Gap(10).Radius(10), true).
				Role(ui.RoleDialog).Label(cf.title)
			box.Children(func() {
				ui.Text(c, cf.title).FontSize(13).FontWeight(600).TextColor(p.text)
				if cf.message != "" {
					// Clipped, so that a path too long to break cannot widen the box.
					ui.Text(c, cf.message).FontSize(12.5).TextColor(p.muted).ClipX()
				}
				if len(cf.choices) > 0 {
					labels := make([]string, len(cf.choices))
					for i, ch := range cf.choices {
						labels[i] = ch.label
					}
					ui.Segmented(c.Key("confirm-choice"), &cf.choice, labels...)
					ch := cf.choices[min(max(cf.choice, 0), len(cf.choices)-1)]
					col := p.muted
					if ch.danger {
						col = p.dangerFg
					}
					ui.Text(c, ch.message).FontSize(12.5).TextColor(col).ClipX()
				}
				buttons := ui.Row(c).Gap(8).Justify(ui.End)
				buttons.Children(func() {
					cancel := ui.Button(c, locales.ButtonCancel())
					if compact {
						cancel.FontSize(12).Padding(6, 8)
					}
					if cancel.Clicked() {
						a.confirm = nil
					}
					if alternate != "" {
						button := ui.Button(c, alternate)
						if compact {
							button.FontSize(12).Padding(6, 8)
						}
						if button.Clicked() {
							a.confirm = nil
							cf.runAlternate()
						}
					}
					ok := ui.PrimaryButton(c, okLabel).AutoFocus()
					if compact {
						ok.FontSize(12).Padding(6, 8)
					}
					if cf.danger || len(cf.choices) > 0 && cf.choices[min(max(cf.choice, 0), len(cf.choices)-1)].danger {
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
