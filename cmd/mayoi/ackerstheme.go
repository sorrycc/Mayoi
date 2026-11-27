package main

import (
	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
)

// ackersPalette brings Monokai Pro's dark editor colors into Mayoi's
// interface: warm charcoal with its pink, green, yellow, purple and cyan.
func ackersPalette() *palette {
	p := &palette{
		dark:   true,
		editor: ui.Hex("#2d2a2e"), chrome: ui.Hex("#2d2a2e"), panel: ui.Hex("#221f22"),
		border: ui.Hex("#403e41"), hover: ui.RGBA(252, 252, 250, 0.045),
		active: ui.RGBA(171, 157, 242, 0.18), text: ui.Hex("#fcfcfa"),
		muted: ui.Hex("#c1c0c0"), faint: ui.Hex("#939293"),
		accent: ui.Hex("#ab9df2"), accentDim: ui.RGBA(171, 157, 242, 0.55),
		line: ui.RGBA(252, 252, 250, 0.04), selection: ui.RGBA(171, 157, 242, 0.30),
		gutter: ui.Hex("#939293"), gutterNow: ui.Hex("#fcfcfa"),
		status: ui.Hex("#2d2a2e"), statusFg: ui.Hex("#fcfcfa"),
		modified: ui.Hex("#fc9867"), danger: ui.Hex("#ff6188"),
		match: ui.RGBA(255, 216, 102, 0.22), matchNow: ui.RGBA(255, 216, 102, 0.4),
		matchRing: ui.RGBA(255, 216, 102, 0.9),
		guide:     ui.RGBA(252, 252, 250, 0.07), guideNow: ui.RGBA(252, 252, 250, 0.22),
		bracket: ui.RGBA(252, 252, 250, 0.34), word: ui.RGBA(252, 252, 250, 0.08),
		shadow: ui.RGBA(0, 0, 0, 0.45), added: ui.Hex("#a9dc76"),
		changed: ui.Hex("#78dce8"), removed: ui.Hex("#ff6188"),
		fill: ui.Hex("#ab9df2"), fillText: ui.Hex("#221f22"),
		dangerFill: ui.Hex("#c63860"), dangerText: ui.Hex("#ffffff"),
		surface: ui.Hex("#373438"), surfaceHover: ui.Hex("#403e41"),
		surfacePressed: ui.Hex("#4b484c"), scrollbar: ui.RGBA(252, 252, 250, 0.16),
		inverse: ui.Hex("#fcfcfa"), inverseText: ui.Hex("#2d2a2e"),
		ansi: [16]ui.Color{
			ui.Hex("#221f22"), ui.Hex("#ff6188"), ui.Hex("#a9dc76"), ui.Hex("#ffd866"),
			ui.Hex("#ab9df2"), ui.Hex("#ab9df2"), ui.Hex("#78dce8"), ui.Hex("#fcfcfa"),
			ui.Hex("#727072"), ui.Hex("#ff8fb0"), ui.Hex("#c5f27f"), ui.Hex("#ffe08a"),
			ui.Hex("#c2b5ff"), ui.Hex("#c2b5ff"), ui.Hex("#9cecf5"), ui.Hex("#ffffff"),
		},
	}
	p.fillHover, p.fillPressed = p.fill.Mix(white, 0.12), p.fill.Mix(black, 0.15)
	s := &p.syntax
	s[highlight.Plain] = p.text
	s[highlight.Keyword] = ui.Hex("#ff6188")
	s[highlight.Type] = ui.Hex("#78dce8")
	s[highlight.Function] = ui.Hex("#a9dc76")
	// DEFAULT_STRING in Monokai_Pro.icls is the scheme's vivid pink;
	// the yellow is for numbers and constants instead.
	s[highlight.String] = ui.Hex("#ff66f0")
	s[highlight.Number] = ui.Hex("#ab9df2")
	s[highlight.Comment] = ui.Hex("#939293")
	s[highlight.Operator] = ui.Hex("#ff6188")
	s[highlight.Punctuation] = ui.Hex("#c1c0c0")
	s[highlight.Constant] = ui.Hex("#ab9df2")
	s[highlight.Builtin] = ui.Hex("#fc9867")
	s[highlight.Tag] = ui.Hex("#ff6188")
	s[highlight.Attribute] = ui.Hex("#78dce8")
	s[highlight.Variable] = ui.Hex("#fcfcfa")
	s[highlight.Heading] = ui.Hex("#ab9df2")
	s[highlight.Deleted] = p.removed
	s[highlight.Inserted] = p.added
	return p
}

var ackersPal = ackersPalette().derive()
