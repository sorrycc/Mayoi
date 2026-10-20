package main

import (
	"math"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
)

// palette is Mayoi's look: the colors of its chrome, of the code and of
// the terminal, for a light or a dark window, or for a theme of its own as
// The Matrix. Widgets take theirs from the ui.Theme that applyTheme
// derives from it.
type palette struct {
	dark bool
	// mono draws the whole window in the code's font, and the icons of
	// files in the palette's own colors rather than their languages'.
	mono bool

	editor    ui.Color // the editor and the terminal
	chrome    ui.Color // the sidebar, the tab strip and the panel's header
	panel     ui.Color // the bottom panel
	border    ui.Color
	hover     ui.Color
	active    ui.Color // a chosen row
	text      ui.Color
	muted     ui.Color
	faint     ui.Color
	accent    ui.Color
	accentDim ui.Color
	// fill is the accent as a button's background, under fillText, and
	// fillHover and fillPressed the button under the pointer and pressed.
	fill, fillHover, fillPressed, fillText ui.Color
	// dangerFill is danger as a button's background, under dangerText,
	// for a button that loses something, as Discard; dangerHover and
	// dangerPressed are derived from it.
	dangerFill, dangerHover, dangerPressed, dangerText ui.Color
	// The faces of controls, as the theme's Surface, SurfaceHover and
	// SurfacePressed.
	surface, surfaceHover, surfacePressed ui.Color
	scrollbar                             ui.Color
	// inverse fills tooltips and toasts, under inverseText: zero for the
	// theme's own.
	inverse, inverseText ui.Color
	line                 ui.Color // the caret's line
	selection            ui.Color
	// The selection as the editor paints it, opaque over its background:
	// while it has the keyboard, and fainter while it has not.
	selected, selectedDim ui.Color
	gutter                ui.Color // line numbers
	gutterNow             ui.Color // the caret's line number
	status                ui.Color
	statusFg              ui.Color
	modified              ui.Color
	danger                ui.Color
	match                 ui.Color // a match of find
	matchNow              ui.Color // the current match
	matchRing             ui.Color
	guide                 ui.Color // indent guides
	guideNow              ui.Color // the guide of the caret's block
	bracket               ui.Color // the brackets paired at the caret
	word                  ui.Color // the other occurrences of the word at the caret
	cursor                ui.Color // the block cursor of vim's normal mode
	shadow                ui.Color
	added                 ui.Color // lines changed since Git's last commit
	changed               ui.Color
	removed               ui.Color
	// modifiedFg, addedFg, removedFg and dangerFg are those colors as
	// text: the same hues, darker on a light window or lighter on a dark
	// one, as far as they need to read at 4.5:1 on the chrome and the
	// editor. The marks and tints keep the brighter ones.
	modifiedFg, addedFg, removedFg, dangerFg ui.Color

	syntax [highlight.Kinds]ui.Color
	// ansi is the terminal's 16 colors: black, red, green, yellow, blue,
	// magenta, cyan and white, then their bright kinds.
	ansi [16]ui.Color
}

func darkPalette() *palette {
	p := &palette{
		dark:      true,
		editor:    ui.Hex("#18181d"),
		chrome:    ui.Hex("#131317"),
		panel:     ui.Hex("#151519"),
		border:    ui.Hex("#26262e"),
		hover:     ui.RGBA(255, 255, 255, 0.045),
		active:    ui.RGBA(139, 124, 246, 0.16),
		text:      ui.Hex("#d6d6de"),
		muted:     ui.Hex("#8c8c99"),
		faint:     ui.Hex("#666672"),
		accent:    ui.Hex("#8b7cf6"),
		accentDim: ui.RGBA(139, 124, 246, 0.55),
		line:      ui.RGBA(255, 255, 255, 0.035),
		selection: ui.RGBA(139, 124, 246, 0.30),
		gutter:    ui.Hex("#62626e"),
		gutterNow: ui.Hex("#b9b9c6"),
		status:    ui.Hex("#131317"),
		statusFg:  ui.Hex("#8c8c99"),
		modified:  ui.Hex("#e8b86d"),
		danger:    ui.Hex("#f0718b"),
		match:     ui.RGBA(232, 184, 109, 0.20),
		matchNow:  ui.RGBA(232, 184, 109, 0.38),
		matchRing: ui.RGBA(232, 184, 109, 0.85),
		guide:     ui.RGBA(255, 255, 255, 0.065),
		guideNow:  ui.RGBA(255, 255, 255, 0.22),
		bracket:   ui.RGBA(255, 255, 255, 0.30),
		word:      ui.RGBA(255, 255, 255, 0.075),
		shadow:    ui.RGBA(0, 0, 0, 0.35),
		added:     ui.Hex("#4fa86d"),
		changed:   ui.Hex("#5a8ee0"),
		removed:   ui.Hex("#e0607e"),
		// The dark accent is light, for marks on a dark ground: white on
		// it would be hard to read, so buttons take a deeper purple, with
		// white on it at 4.6:1.
		fill:     ui.Hex("#7260e3"),
		fillText: ui.Hex("#ffffff"),
		// The danger of a dark window is light, as the accent is: white
		// on it would be hard to read.
		dangerFill:     ui.Hex("#bb3253"),
		dangerText:     ui.Hex("#ffffff"),
		surface:        ui.Hex("#202027"),
		surfaceHover:   ui.Hex("#272730"),
		surfacePressed: ui.Hex("#2e2e38"),
		scrollbar:      ui.RGBA(255, 255, 255, 0.16),
		inverse:        ui.Hex("#2b2b34"),
		inverseText:    ui.Hex("#e6e6ee"),
		ansi: [16]ui.Color{
			ui.Hex("#2a2a33"), ui.Hex("#f0718b"), ui.Hex("#a9d48f"), ui.Hex("#e8c482"),
			ui.Hex("#8fb4ff"), ui.Hex("#b99cf7"), ui.Hex("#7dcfe0"), ui.Hex("#d6d6de"),
			ui.Hex("#5a5a66"), ui.Hex("#ff8fa5"), ui.Hex("#bfe6a5"), ui.Hex("#f5d699"),
			ui.Hex("#a9c6ff"), ui.Hex("#cdb5fa"), ui.Hex("#9ee0ee"), ui.Hex("#ffffff"),
		},
	}
	p.fillHover, p.fillPressed = p.fill.Mix(white, 0.1), p.fill.Mix(black, 0.15)
	s := &p.syntax
	s[highlight.Plain] = p.text
	s[highlight.Keyword] = ui.Hex("#b99cf7")
	s[highlight.Type] = ui.Hex("#7dcfe0")
	s[highlight.Function] = ui.Hex("#8fb4ff")
	s[highlight.String] = ui.Hex("#a9d48f")
	s[highlight.Number] = ui.Hex("#f2a679")
	s[highlight.Comment] = ui.Hex("#747481")
	s[highlight.Operator] = ui.Hex("#a0a5b8")
	s[highlight.Punctuation] = ui.Hex("#8a8e9e")
	s[highlight.Constant] = ui.Hex("#f2a679")
	s[highlight.Builtin] = ui.Hex("#e8c482")
	s[highlight.Tag] = ui.Hex("#f590a8")
	s[highlight.Attribute] = ui.Hex("#e8c482")
	s[highlight.Variable] = ui.Hex("#e2e2ea")
	s[highlight.Heading] = ui.Hex("#8fb4ff")
	s[highlight.Deleted] = ui.Hex("#f0718b")
	s[highlight.Inserted] = ui.Hex("#a9d48f")
	return p
}

func lightPalette() *palette {
	p := &palette{
		editor:         ui.Hex("#fcfcfd"),
		chrome:         ui.Hex("#f4f4f7"),
		panel:          ui.Hex("#f8f8fa"),
		border:         ui.Hex("#e4e4ea"),
		hover:          ui.RGBA(0, 0, 0, 0.04),
		active:         ui.RGBA(109, 91, 208, 0.12),
		text:           ui.Hex("#2a2a33"),
		muted:          ui.Hex("#6c6c79"),
		faint:          ui.Hex("#8e8e9a"),
		accent:         ui.Hex("#6d5bd0"),
		accentDim:      ui.RGBA(109, 91, 208, 0.5),
		line:           ui.RGBA(0, 0, 0, 0.03),
		selection:      ui.RGBA(109, 91, 208, 0.20),
		gutter:         ui.Hex("#93939f"),
		gutterNow:      ui.Hex("#4a4a55"),
		status:         ui.Hex("#f4f4f7"),
		statusFg:       ui.Hex("#6c6c79"),
		modified:       ui.Hex("#c4862a"),
		danger:         ui.Hex("#d0365a"),
		match:          ui.RGBA(230, 160, 40, 0.22),
		matchNow:       ui.RGBA(230, 160, 40, 0.42),
		matchRing:      ui.RGBA(196, 120, 20, 0.9),
		guide:          ui.RGBA(0, 0, 0, 0.07),
		guideNow:       ui.RGBA(0, 0, 0, 0.24),
		bracket:        ui.RGBA(0, 0, 0, 0.28),
		word:           ui.RGBA(0, 0, 0, 0.065),
		shadow:         ui.RGBA(0, 0, 0, 0.12),
		added:          ui.Hex("#3fa363"),
		changed:        ui.Hex("#4a7fd4"),
		removed:        ui.Hex("#d6456a"),
		fill:           ui.Hex("#6d5bd0"),
		fillText:       ui.Hex("#ffffff"),
		dangerFill:     ui.Hex("#d0365a"),
		dangerText:     ui.Hex("#ffffff"),
		surface:        ui.Hex("#ffffff"),
		surfaceHover:   ui.Hex("#f2f2f6"),
		surfacePressed: ui.Hex("#eaeaf0"),
		scrollbar:      ui.RGBA(0, 0, 0, 0.18),
		ansi: [16]ui.Color{
			ui.Hex("#2a2a33"), ui.Hex("#c8264f"), ui.Hex("#3d8a28"), ui.Hex("#a86a0c"),
			ui.Hex("#2a5bd7"), ui.Hex("#7c3bd6"), ui.Hex("#0b7c99"), ui.Hex("#a3a3ae"),
			ui.Hex("#6c6c79"), ui.Hex("#e0405f"), ui.Hex("#4fa63a"), ui.Hex("#c4862a"),
			ui.Hex("#4a78e8"), ui.Hex("#9558e8"), ui.Hex("#1a9cbd"), ui.Hex("#d6d6de"),
		},
	}
	p.fillHover, p.fillPressed = p.fill.Mix(black, 0.1), p.fill.Mix(black, 0.22)
	s := &p.syntax
	s[highlight.Plain] = p.text
	s[highlight.Keyword] = ui.Hex("#7c3bd6")
	s[highlight.Type] = ui.Hex("#0b7c99")
	s[highlight.Function] = ui.Hex("#2a5bd7")
	s[highlight.String] = ui.Hex("#3d8a28")
	s[highlight.Number] = ui.Hex("#c2571a")
	s[highlight.Comment] = ui.Hex("#868692")
	s[highlight.Operator] = ui.Hex("#555a6b")
	s[highlight.Punctuation] = ui.Hex("#6c6f80")
	s[highlight.Constant] = ui.Hex("#c2571a")
	s[highlight.Builtin] = ui.Hex("#a86a0c")
	s[highlight.Tag] = ui.Hex("#c8264f")
	s[highlight.Attribute] = ui.Hex("#a86a0c")
	s[highlight.Variable] = ui.Hex("#2a2a33")
	s[highlight.Heading] = ui.Hex("#2a5bd7")
	s[highlight.Deleted] = ui.Hex("#c8264f")
	s[highlight.Inserted] = ui.Hex("#3d8a28")
	return p
}

// matrixPalette is The Matrix: phosphor green on black, the code in
// greens told apart by their brightness. What is removed or failed stays
// red, and what is changed or found amber, so diffs and errors still
// read.
func matrixPalette() *palette {
	p := &palette{
		dark:      true,
		mono:      true,
		editor:    ui.Hex("#050a06"),
		chrome:    ui.Hex("#020503"),
		panel:     ui.Hex("#040806"),
		border:    ui.Hex("#0f2a16"),
		hover:     ui.RGBA(0, 255, 65, 0.06),
		active:    ui.RGBA(0, 255, 65, 0.14),
		text:      ui.Hex("#4af07a"),
		muted:     ui.Hex("#2a9d4c"),
		faint:     ui.Hex("#1a5c2e"),
		accent:    ui.Hex("#00ff41"),
		accentDim: ui.RGBA(0, 255, 65, 0.5),
		line:      ui.RGBA(0, 255, 65, 0.04),
		selection: ui.RGBA(0, 255, 65, 0.22),
		gutter:    ui.Hex("#1f6b36"),
		gutterNow: ui.Hex("#6dff94"),
		status:    ui.Hex("#020503"),
		statusFg:  ui.Hex("#2a9d4c"),
		modified:  ui.Hex("#d9b44a"),
		danger:    ui.Hex("#ff4a5a"),
		match:     ui.RGBA(217, 180, 74, 0.22),
		matchNow:  ui.RGBA(217, 180, 74, 0.40),
		matchRing: ui.RGBA(217, 180, 74, 0.85),
		guide:     ui.RGBA(0, 255, 65, 0.07),
		guideNow:  ui.RGBA(0, 255, 65, 0.25),
		bracket:   ui.RGBA(0, 255, 65, 0.35),
		word:      ui.RGBA(0, 255, 65, 0.09),
		shadow:    ui.RGBA(0, 0, 0, 0.6),
		added:     ui.Hex("#00d43a"),
		changed:   ui.Hex("#3fd0b0"),
		removed:   ui.Hex("#e0505e"),
		// Black on the green, as a terminal's inverted text.
		fill:           ui.Hex("#00d43a"),
		fillText:       ui.Hex("#000000"),
		dangerFill:     ui.Hex("#ff4a5a"),
		dangerText:     ui.Hex("#000000"),
		surface:        ui.Hex("#08140b"),
		surfaceHover:   ui.Hex("#0c1d10"),
		surfacePressed: ui.Hex("#102615"),
		scrollbar:      ui.RGBA(0, 255, 65, 0.2),
		inverse:        ui.Hex("#0f2a16"),
		inverseText:    ui.Hex("#8dffad"),
		ansi: [16]ui.Color{
			ui.Hex("#0a1a0e"), ui.Hex("#e0505e"), ui.Hex("#00ff41"), ui.Hex("#d9b44a"),
			ui.Hex("#2fbf8f"), ui.Hex("#8fd94a"), ui.Hex("#00ff9c"), ui.Hex("#4af07a"),
			ui.Hex("#1f7a3a"), ui.Hex("#ff6b78"), ui.Hex("#6dff94"), ui.Hex("#f0d070"),
			ui.Hex("#5fe8c4"), ui.Hex("#b0f070"), ui.Hex("#7dffcf"), ui.Hex("#c8ffd6"),
		},
	}
	p.fillHover, p.fillPressed = p.fill.Mix(white, 0.15), p.fill.Mix(black, 0.15)
	s := &p.syntax
	s[highlight.Plain] = p.text
	s[highlight.Keyword] = ui.Hex("#b6ffc8")
	s[highlight.Type] = ui.Hex("#00ff9c")
	s[highlight.Function] = ui.Hex("#66ff8a")
	s[highlight.String] = ui.Hex("#8fd94a")
	s[highlight.Number] = ui.Hex("#c8ff5a")
	s[highlight.Comment] = ui.Hex("#1f7a3a")
	s[highlight.Operator] = ui.Hex("#3fbf63")
	s[highlight.Punctuation] = ui.Hex("#2e8f4a")
	s[highlight.Constant] = ui.Hex("#c8ff5a")
	s[highlight.Builtin] = ui.Hex("#7dffcf")
	s[highlight.Tag] = ui.Hex("#00ff9c")
	s[highlight.Attribute] = ui.Hex("#8fd94a")
	s[highlight.Variable] = ui.Hex("#5af58a")
	s[highlight.Heading] = ui.Hex("#b6ffc8")
	s[highlight.Deleted] = p.removed
	s[highlight.Inserted] = p.added
	return p
}

// claudeDarkPalette is Claude in a dark window: Anthropic's warm
// near-black and cloud, with Claude's orange for the accent.
func claudeDarkPalette() *palette {
	p := &palette{
		dark:      true,
		editor:    ui.Hex("#1f1e1d"),
		chrome:    ui.Hex("#191918"),
		panel:     ui.Hex("#1c1b1a"),
		border:    ui.Hex("#2e2c2a"),
		hover:     ui.RGBA(255, 245, 230, 0.045),
		active:    ui.RGBA(217, 119, 87, 0.16),
		text:      ui.Hex("#e8e6dc"),
		muted:     ui.Hex("#a39e93"),
		faint:     ui.Hex("#75716a"),
		accent:    ui.Hex("#d97757"),
		accentDim: ui.RGBA(217, 119, 87, 0.55),
		line:      ui.RGBA(255, 245, 230, 0.035),
		selection: ui.RGBA(217, 119, 87, 0.28),
		gutter:    ui.Hex("#6b675f"),
		gutterNow: ui.Hex("#c4bfb3"),
		status:    ui.Hex("#191918"),
		statusFg:  ui.Hex("#a39e93"),
		modified:  ui.Hex("#e3b25f"),
		danger:    ui.Hex("#e8705f"),
		match:     ui.RGBA(227, 178, 95, 0.20),
		matchNow:  ui.RGBA(227, 178, 95, 0.38),
		matchRing: ui.RGBA(227, 178, 95, 0.85),
		guide:     ui.RGBA(255, 245, 230, 0.065),
		guideNow:  ui.RGBA(255, 245, 230, 0.22),
		bracket:   ui.RGBA(255, 245, 230, 0.30),
		word:      ui.RGBA(255, 245, 230, 0.075),
		shadow:    ui.RGBA(0, 0, 0, 0.35),
		added:     ui.Hex("#7ba05b"),
		changed:   ui.Hex("#6a9bcc"),
		removed:   ui.Hex("#d9634f"),
		// White on Claude's orange reads at 3:1, so buttons take a
		// deeper one, with white on it at 4.6:1.
		fill:           ui.Hex("#b8573a"),
		fillText:       ui.Hex("#ffffff"),
		dangerFill:     ui.Hex("#b5402f"),
		dangerText:     ui.Hex("#ffffff"),
		surface:        ui.Hex("#262523"),
		surfaceHover:   ui.Hex("#2d2b29"),
		surfacePressed: ui.Hex("#34322f"),
		scrollbar:      ui.RGBA(255, 245, 230, 0.16),
		inverse:        ui.Hex("#302e2b"),
		inverseText:    ui.Hex("#f0eee6"),
		ansi: [16]ui.Color{
			ui.Hex("#2e2c2a"), ui.Hex("#e8705f"), ui.Hex("#9cbf7a"), ui.Hex("#e3b25f"),
			ui.Hex("#6a9bcc"), ui.Hex("#c68fb5"), ui.Hex("#7fbfb3"), ui.Hex("#e8e6dc"),
			ui.Hex("#6b675f"), ui.Hex("#f08a78"), ui.Hex("#b3d496"), ui.Hex("#f0c87e"),
			ui.Hex("#8fb6dd"), ui.Hex("#d9a8cb"), ui.Hex("#9fd6cb"), ui.Hex("#faf9f5"),
		},
	}
	p.fillHover, p.fillPressed = p.fill.Mix(white, 0.1), p.fill.Mix(black, 0.15)
	s := &p.syntax
	s[highlight.Plain] = p.text
	s[highlight.Keyword] = ui.Hex("#d97757")
	s[highlight.Type] = ui.Hex("#7fbfb3")
	s[highlight.Function] = ui.Hex("#8fb6dd")
	s[highlight.String] = ui.Hex("#b3c98a")
	s[highlight.Number] = ui.Hex("#d4a27f")
	s[highlight.Comment] = ui.Hex("#7d786f")
	s[highlight.Operator] = ui.Hex("#b0aa9e")
	s[highlight.Punctuation] = ui.Hex("#938d82")
	s[highlight.Constant] = ui.Hex("#d4a27f")
	s[highlight.Builtin] = ui.Hex("#e3b25f")
	s[highlight.Tag] = ui.Hex("#e8705f")
	s[highlight.Attribute] = ui.Hex("#e3b25f")
	s[highlight.Variable] = ui.Hex("#ebe9e0")
	s[highlight.Heading] = ui.Hex("#d97757")
	s[highlight.Deleted] = ui.Hex("#e8705f")
	s[highlight.Inserted] = ui.Hex("#9cbf7a")
	return p
}

// claudeLightPalette is Claude in a light window: Anthropic's ivory and
// near-black, with a deeper orange for the accent, for marks on a light
// ground.
func claudeLightPalette() *palette {
	p := &palette{
		editor:         ui.Hex("#faf9f5"),
		chrome:         ui.Hex("#f0eee6"),
		panel:          ui.Hex("#f5f4ee"),
		border:         ui.Hex("#e3dfd3"),
		hover:          ui.RGBA(20, 20, 19, 0.045),
		active:         ui.RGBA(201, 100, 66, 0.13),
		text:           ui.Hex("#141413"),
		muted:          ui.Hex("#6b675f"),
		faint:          ui.Hex("#938d82"),
		accent:         ui.Hex("#c96442"),
		accentDim:      ui.RGBA(201, 100, 66, 0.5),
		line:           ui.RGBA(20, 20, 19, 0.03),
		selection:      ui.RGBA(217, 119, 87, 0.22),
		gutter:         ui.Hex("#a39e93"),
		gutterNow:      ui.Hex("#3d3a35"),
		status:         ui.Hex("#f0eee6"),
		statusFg:       ui.Hex("#6b675f"),
		modified:       ui.Hex("#b5791f"),
		danger:         ui.Hex("#bf4d3a"),
		match:          ui.RGBA(212, 162, 64, 0.24),
		matchNow:       ui.RGBA(212, 162, 64, 0.44),
		matchRing:      ui.RGBA(181, 121, 31, 0.9),
		guide:          ui.RGBA(20, 20, 19, 0.07),
		guideNow:       ui.RGBA(20, 20, 19, 0.24),
		bracket:        ui.RGBA(20, 20, 19, 0.28),
		word:           ui.RGBA(20, 20, 19, 0.065),
		shadow:         ui.RGBA(40, 30, 20, 0.12),
		added:          ui.Hex("#5e8a3a"),
		changed:        ui.Hex("#4a7fb5"),
		removed:        ui.Hex("#c4513c"),
		fill:           ui.Hex("#b8573a"),
		fillText:       ui.Hex("#ffffff"),
		dangerFill:     ui.Hex("#bf4d3a"),
		dangerText:     ui.Hex("#ffffff"),
		surface:        ui.Hex("#ffffff"),
		surfaceHover:   ui.Hex("#f5f3ec"),
		surfacePressed: ui.Hex("#ece9df"),
		scrollbar:      ui.RGBA(20, 20, 19, 0.18),
		ansi: [16]ui.Color{
			ui.Hex("#141413"), ui.Hex("#bf4d3a"), ui.Hex("#5e8a3a"), ui.Hex("#a3680f"),
			ui.Hex("#3b6ea8"), ui.Hex("#9a4f86"), ui.Hex("#2f8378"), ui.Hex("#b0aa9e"),
			ui.Hex("#6b675f"), ui.Hex("#d9634f"), ui.Hex("#76a34f"), ui.Hex("#c4862a"),
			ui.Hex("#5a8cc4"), ui.Hex("#b56aa1"), ui.Hex("#45a093"), ui.Hex("#e3dfd3"),
		},
	}
	p.fillHover, p.fillPressed = p.fill.Mix(black, 0.1), p.fill.Mix(black, 0.22)
	s := &p.syntax
	s[highlight.Plain] = p.text
	s[highlight.Keyword] = ui.Hex("#b8573a")
	s[highlight.Type] = ui.Hex("#2f8378")
	s[highlight.Function] = ui.Hex("#3b6ea8")
	s[highlight.String] = ui.Hex("#5e8a3a")
	s[highlight.Number] = ui.Hex("#a85a1f")
	s[highlight.Comment] = ui.Hex("#938d82")
	s[highlight.Operator] = ui.Hex("#5c5850")
	s[highlight.Punctuation] = ui.Hex("#75716a")
	s[highlight.Constant] = ui.Hex("#a85a1f")
	s[highlight.Builtin] = ui.Hex("#a3680f")
	s[highlight.Tag] = ui.Hex("#bf4d3a")
	s[highlight.Attribute] = ui.Hex("#a3680f")
	s[highlight.Variable] = ui.Hex("#141413")
	s[highlight.Heading] = ui.Hex("#b8573a")
	s[highlight.Deleted] = ui.Hex("#bf4d3a")
	s[highlight.Inserted] = ui.Hex("#5e8a3a")
	return p
}

// qoderLightPalette is Qoder in a light window, its Forest Light: white,
// grey with a hint of green, and the forest green of its buttons for the
// accent. Forest has no colors of code, so they are drawn from its
// terminal's.
func qoderLightPalette() *palette {
	p := &palette{
		editor:    ui.Hex("#ffffff"),
		chrome:    ui.Hex("#f2f4f2"),
		panel:     ui.Hex("#f5f7f5"),
		border:    ui.Hex("#e3e6e3"),
		hover:     ui.RGBA(0, 0, 0, 0.04),
		active:    ui.RGBA(75, 111, 90, 0.12),
		text:      ui.Hex("#141414"),
		muted:     ui.Hex("#636261"),
		faint:     ui.Hex("#838280"),
		accent:    ui.Hex("#4b6f5a"),
		accentDim: ui.RGBA(75, 111, 90, 0.5),
		line:      ui.RGBA(0, 0, 0, 0.03),
		selection: ui.RGBA(75, 111, 90, 0.22),
		gutter:    ui.Hex("#949391"),
		gutterNow: ui.Hex("#474645"),
		status:    ui.Hex("#f2f4f2"),
		statusFg:  ui.Hex("#636261"),
		// Forest's warning and error are too light for marks on white, so
		// they take its terminal's deeper yellow and red.
		modified:  ui.Hex("#b77a0a"),
		danger:    ui.Hex("#c9363e"),
		match:     ui.RGBA(239, 176, 65, 0.24),
		matchNow:  ui.RGBA(239, 176, 65, 0.44),
		matchRing: ui.RGBA(183, 122, 10, 0.9),
		guide:     ui.RGBA(0, 0, 0, 0.07),
		guideNow:  ui.RGBA(0, 0, 0, 0.24),
		bracket:   ui.RGBA(0, 0, 0, 0.28),
		word:      ui.RGBA(0, 0, 0, 0.065),
		shadow:    ui.RGBA(0, 0, 0, 0.12),
		added:     ui.Hex("#579b6e"),
		changed:   ui.Hex("#3b81e9"),
		removed:   ui.Hex("#ea7485"),
		// Forest's primary button, as it is under the pointer and pressed.
		fill:           ui.Hex("#4b6f5a"),
		fillHover:      ui.Hex("#436651"),
		fillPressed:    ui.Hex("#3a5747"),
		fillText:       ui.Hex("#fdfdfd"),
		dangerFill:     ui.Hex("#c9363e"),
		dangerText:     ui.Hex("#ffffff"),
		surface:        ui.Hex("#ffffff"),
		surfaceHover:   ui.Hex("#f6f6f6"),
		surfacePressed: ui.Hex("#efefef"),
		scrollbar:      ui.RGBA(0, 0, 0, 0.18),
		// Forest's terminal, but for its whites, which would not show on
		// a light terminal.
		ansi: [16]ui.Color{
			ui.Hex("#24262b"), ui.Hex("#c9363e"), ui.Hex("#2e7d4f"), ui.Hex("#8a5a00"),
			ui.Hex("#0969a8"), ui.Hex("#8e4e8b"), ui.Hex("#0b7f78"), ui.Hex("#a7ada8"),
			ui.Hex("#727a74"), ui.Hex("#e04b53"), ui.Hex("#3f9a64"), ui.Hex("#b77a0a"),
			ui.Hex("#2b83c1"), ui.Hex("#aa69a7"), ui.Hex("#249d94"), ui.Hex("#d5dbd8"),
		},
	}
	s := &p.syntax
	s[highlight.Plain] = p.text
	s[highlight.Keyword] = ui.Hex("#2f7550")
	s[highlight.Type] = ui.Hex("#0b7f78")
	s[highlight.Function] = ui.Hex("#0969a8")
	s[highlight.String] = ui.Hex("#8a5a00")
	s[highlight.Number] = ui.Hex("#b85a1f")
	s[highlight.Comment] = ui.Hex("#848c86")
	s[highlight.Operator] = ui.Hex("#555b57")
	s[highlight.Punctuation] = ui.Hex("#6c726d")
	s[highlight.Constant] = ui.Hex("#b85a1f")
	s[highlight.Builtin] = ui.Hex("#8e4e8b")
	s[highlight.Tag] = ui.Hex("#b3434a")
	s[highlight.Attribute] = ui.Hex("#8e4e8b")
	s[highlight.Variable] = p.text
	s[highlight.Heading] = ui.Hex("#2f7550")
	s[highlight.Deleted] = ui.Hex("#c9363e")
	s[highlight.Inserted] = ui.Hex("#2e7d4f")
	return p
}

// qoderDarkPalette is Qoder in a dark window, its Forest Dark: greys
// with a little green, a deeper green for buttons, with dark labels, and
// a light one for links and marks.
func qoderDarkPalette() *palette {
	p := &palette{
		dark:      true,
		editor:    ui.Hex("#191b1a"),
		chrome:    ui.Hex("#242624"),
		panel:     ui.Hex("#1c1f1d"),
		border:    ui.Hex("#363c37"),
		hover:     ui.RGBA(220, 240, 225, 0.05),
		active:    ui.RGBA(77, 156, 97, 0.2),
		text:      ui.Hex("#d9dcd9"),
		muted:     ui.Hex("#949d96"),
		faint:     ui.Hex("#6b736d"),
		accent:    ui.Hex("#8ee5a1"),
		accentDim: ui.RGBA(142, 229, 161, 0.5),
		line:      ui.RGBA(220, 240, 225, 0.035),
		selection: ui.RGBA(77, 156, 97, 0.32),
		gutter:    ui.Hex("#646d66"),
		gutterNow: ui.Hex("#b3bab4"),
		status:    ui.Hex("#242624"),
		statusFg:  ui.Hex("#949d96"),
		modified:  ui.Hex("#faad14"),
		danger:    ui.Hex("#ff6b70"),
		match:     ui.RGBA(250, 173, 20, 0.20),
		matchNow:  ui.RGBA(250, 173, 20, 0.38),
		matchRing: ui.RGBA(250, 173, 20, 0.85),
		guide:     ui.RGBA(220, 240, 225, 0.065),
		guideNow:  ui.RGBA(220, 240, 225, 0.22),
		bracket:   ui.RGBA(220, 240, 225, 0.30),
		word:      ui.RGBA(220, 240, 225, 0.075),
		shadow:    ui.RGBA(0, 0, 0, 0.35),
		added:     ui.Hex("#73cd94"),
		changed:   ui.Hex("#5a9fe0"),
		removed:   ui.Hex("#e86a70"),
		// Forest Dark's primary button, as it is under the pointer and
		// pressed, with a near-black label, as Qoder's, at 5.9:1; and its
		// error, a button as bright, with the same.
		fill:           ui.Hex("#4d9c61"),
		fillHover:      ui.Hex("#62ad78"),
		fillPressed:    ui.Hex("#428955"),
		fillText:       ui.Hex("#080807"),
		dangerFill:     ui.Hex("#ff4d4f"),
		dangerText:     ui.Hex("#080807"),
		surface:        ui.Hex("#222523"),
		surfaceHover:   ui.Hex("#2d332e"),
		surfacePressed: ui.Hex("#363d37"),
		scrollbar:      ui.RGBA(220, 240, 225, 0.16),
		inverse:        ui.Hex("#303632"),
		inverseText:    ui.Hex("#eeeeeb"),
		// Forest's terminal, but for its black, which would not show on
		// a dark one.
		ansi: [16]ui.Color{
			ui.Hex("#2b302c"), ui.Hex("#ff6b70"), ui.Hex("#73cd94"), ui.Hex("#ffd666"),
			ui.Hex("#75beff"), ui.Hex("#d59bd1"), ui.Hex("#79e0d4"), ui.Hex("#f4f5f3"),
			ui.Hex("#8b918d"), ui.Hex("#ff8a8e"), ui.Hex("#9be6b3"), ui.Hex("#ffe58f"),
			ui.Hex("#a3d3ff"), ui.Hex("#e6b9e2"), ui.Hex("#a2ede5"), ui.Hex("#ffffff"),
		},
	}
	s := &p.syntax
	s[highlight.Plain] = p.text
	s[highlight.Keyword] = ui.Hex("#73cd94")
	s[highlight.Type] = ui.Hex("#82d2ce")
	s[highlight.Function] = ui.Hex("#87c3ff")
	s[highlight.String] = ui.Hex("#d8c07a")
	s[highlight.Number] = ui.Hex("#efb080")
	s[highlight.Comment] = ui.Hex("#758077")
	s[highlight.Operator] = ui.Hex("#a3aba5")
	s[highlight.Punctuation] = ui.Hex("#8a948c")
	s[highlight.Constant] = ui.Hex("#efb080")
	s[highlight.Builtin] = ui.Hex("#aaa0fa")
	s[highlight.Tag] = ui.Hex("#e394dc")
	s[highlight.Attribute] = ui.Hex("#aaa0fa")
	s[highlight.Variable] = ui.Hex("#e4e7e4")
	s[highlight.Heading] = ui.Hex("#8ee5a1")
	s[highlight.Deleted] = ui.Hex("#ff6b70")
	s[highlight.Inserted] = ui.Hex("#73cd94")
	return p
}

var (
	black, white = ui.Hex("#000000"), ui.Hex("#ffffff")

	lightPal  = lightPalette().derive()
	darkPal   = darkPalette().derive()
	matrixPal = matrixPalette().derive()

	claudeLightPal = claudeLightPalette().derive()
	claudeDarkPal  = claudeDarkPalette().derive()

	qoderLightPal = qoderLightPalette().derive()
	qoderDarkPal  = qoderDarkPalette().derive()
)

// The Themes of the settings beside Mayoi's own, "": The Matrix, which is
// always dark, and Claude and Qoder, light or dark as the Appearance is.
const (
	themeMatrix = "matrix"
	themeClaude = "claude"
	themeQoder  = "qoder"
)

// paletteFor returns the palette of a theme of the settings, in a window
// that is dark or not.
func paletteFor(theme string, dark bool) *palette {
	switch {
	case theme == themeMatrix:
		return matrixPal
	case theme == themeClaude && dark:
		return claudeDarkPal
	case theme == themeClaude:
		return claudeLightPal
	case theme == themeQoder && dark:
		return qoderDarkPal
	case theme == themeQoder:
		return qoderLightPal
	case dark:
		return darkPal
	}
	return lightPal
}

// themeSource is the light or dark appearance of the native window for a
// theme and an appearance of the settings: The Matrix is dark, and the
// other themes are as the appearance is, the system's for "".
func themeSource(theme, appearance string) mygo.ThemeSource {
	if theme == themeMatrix {
		return mygo.ThemeDark
	}
	return mygo.ThemeSource(appearance)
}

// derive fills in the colors made from the others.
func (p *palette) derive() *palette {
	if p.dark {
		p.dangerHover, p.dangerPressed = p.dangerFill.Mix(white, 0.1), p.dangerFill.Mix(black, 0.15)
	} else {
		p.dangerHover, p.dangerPressed = p.dangerFill.Mix(black, 0.1), p.dangerFill.Mix(black, 0.22)
	}
	p.modifiedFg = legible(p.modified, p.dark, p.chrome, p.editor, p.panel)
	p.addedFg = legible(p.added, p.dark, p.chrome, p.editor, p.panel)
	p.removedFg = legible(p.removed, p.dark, p.chrome, p.editor, p.panel)
	p.dangerFg = legible(p.danger, p.dark, p.chrome, p.editor, p.panel)
	p.selected = p.selection.Over(p.editor)
	p.selectedDim = p.selection.Alpha(0.5).Over(p.editor)
	p.cursor = p.accent.Alpha(0.5)
	return p
}

// legible returns col as text that reads at 4.5:1, WCAG's AA, on each of
// the backgrounds: col itself when it does, or else col mixed toward black
// on a light window, or white on a dark one, no further than it must.
func legible(col ui.Color, dark bool, backs ...ui.Color) ui.Color {
	to := black
	if dark {
		to = white
	}
	reads := func(c ui.Color) bool {
		for _, b := range backs {
			if contrast(c, b) < 4.5 {
				return false
			}
		}
		return true
	}
	for i := 0; i <= 40; i++ {
		if c := col.Mix(to, float32(i)/40); reads(c) || i == 40 {
			return c
		}
	}
	return col
}

// contrast is the WCAG contrast ratio of two opaque colors.
func contrast(x, y ui.Color) float64 {
	lum := func(c ui.Color) float64 {
		ch := func(v uint8) float64 {
			f := float64(v) / 255
			if f <= 0.03928 {
				return f / 12.92
			}
			return math.Pow((f+0.055)/1.055, 2.4)
		}
		return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
	}
	a, b := lum(x)+0.05, lum(y)+0.05
	return max(a, b) / min(a, b)
}

// applyTheme sets the window's theme from the palette of its theme,
// and returns that palette.
func (a *app) applyTheme(c *ui.Context) *palette {
	t := *c.Theme()
	dark := t.Dark
	switch a.settings.Appearance {
	case "light":
		dark = false
	case "dark":
		dark = true
	}
	p := paletteFor(a.settings.Theme, dark)
	t.Dark = p.dark
	t.Background = p.editor
	// Every state of the accent, or a hovered button would turn the
	// default theme's blue.
	t.Accent, t.AccentText = p.fill, p.fillText
	t.AccentHover, t.AccentPressed = p.fillHover, p.fillPressed
	t.Danger, t.Warning, t.Success = p.dangerFg, p.modifiedFg, p.addedFg
	t.Selection = p.selection
	t.Border = p.border
	t.Text, t.TextMuted = p.text, p.muted
	t.Focus = p.accentDim
	t.Radius = 6
	t.Spacing = 3.5
	t.FontSize = 13
	t.ScrollbarWidth = 8
	t.Surface, t.SurfaceHover, t.SurfacePressed = p.surface, p.surfaceHover, p.surfacePressed
	t.Scrollbar = p.scrollbar
	t.Inverse, t.InverseText = p.inverse, p.inverseText
	if p.mono {
		t.Font = a.settings.FontFamily
	}
	c.SetTheme(&t)
	return p
}

// popIn is how a box over an editor comes in, as find's: it fades in,
// from a little above where it settles, and goes back up as it fades out.
// Only its colors follow it after.
var popIn = ui.ElementTransition{Duration: 130 * time.Millisecond, Colors: true, Enter: &ui.Motion{Y: -6}, Exit: &ui.Motion{Y: -4}}

// hoverFade is how a button's background follows the pointer: it fades
// in and out rather than blinks.
var hoverFade = ui.ElementTransition{Colors: true, Duration: 110 * time.Millisecond}

// revealFade is hoverFade for a small button that shows only while the
// pointer is over its row, as the actions of a group of changes: it fades
// in and out too, rather than pops.
var revealFade = ui.ElementTransition{Colors: true, Duration: 110 * time.Millisecond, Enter: &ui.Motion{}, Exit: &ui.Motion{}}

// tabMotion is how a tab moves along its strip: it grows in as it opens,
// folds away as it closes, and the others slide to their places.
var tabMotion = ui.ElementTransition{
	Duration: 140 * time.Millisecond,
	Enter:    &ui.Motion{Collapse: true},
	Exit:     &ui.Motion{Collapse: true},
}

// dotPop is how a small mark comes and goes, as the dot of a tab's
// unsaved changes: it grows from nothing and shrinks away.
var dotPop = ui.ElementTransition{
	Animation: &ui.Snappy,
	Enter:     &ui.Motion{Scale: 0.2},
	Exit:      &ui.Motion{Scale: 0.2},
}

// pickMark is the identity of the mark under the row chosen in a list of
// a box, as quick open's, which slides from row to row as the choice
// moves.
type pickMark struct{ list string }

// pickMark marks the row chosen in a list, first in the row's children:
// it slides from the row chosen before, as the keys move the choice.
func (a *app) pickMark(c *ui.Context, on bool, list string) {
	if !on {
		return
	}
	ui.Box(c.Key("pick-mark")).Absolute().Left(0).Right(0).Top(0).Bottom(0).Radius(6).Background(a.pal.active).
		Transition(ui.ElementTransition{Duration: 90 * time.Millisecond, Match: pickMark{list}})
}

// riseIn brings a block of a page in as the page shows: it fades in from
// a little below, after delay, so that blocks delayed more follow.
func riseIn(e ui.Element, delay time.Duration) ui.Element {
	k := e.Keyframes("rise", []ui.Keyframe{{Value: 0}, {At: 240 * time.Millisecond, Value: 1, Ease: ui.EaseOut}},
		ui.KeyframeOptions{Delay: delay})
	return e.Opacity(k).Translate(0, 8*(1-k))
}

// chevron is the arrow of what folds: it points to the side while
// closed, and turns down as it opens.
func chevron(c *ui.Context, open bool) ui.Element {
	turn := float32(0)
	if open {
		turn = 90
	}
	return ui.Icon(c.Key("chevron"), iconChevronRight).AnimatePose(ui.Pose{Rotate: turn}, ui.Snappy)
}

// pressPose sinks a button a little as it is held down, as a key does.
func pressPose(b ui.Element) {
	pose := ui.Pose{}
	if b.Pressed() {
		pose.Scale = 0.92
	}
	b.AnimatePose(pose, ui.Snappy)
}

// shakeFor is how long a box shakes its head, as a password field of the
// Mac does at a wrong password.
const shakeFor = 360 * time.Millisecond

// shake moves a box side to side for a moment from at, as it finds
// nothing for the Return pressed in it, and settles where it was. A box
// shaken long ago, or never, stays put; with less motion, it does too.
func shake(c *ui.Context, e ui.Element, at time.Time) ui.Element {
	since := c.Now().Sub(at)
	if at.IsZero() || since < 0 || since >= shakeFor || c.Preferences().ReduceMotion {
		return e
	}
	c.AnimationFrame()
	t := float64(since) / float64(shakeFor)
	return e.Translate(float32(7*(1-t)*math.Sin(t*6*math.Pi)), 0)
}

// popUp brings the badge of an empty view in as the view shows: it grows
// from a little smaller, past its size and back, and fades in.
func popUp(e ui.Element) ui.Element {
	k := e.Keyframes("pop", []ui.Keyframe{{Value: 0}, {At: 320 * time.Millisecond, Value: 1, Ease: backOut}})
	o := min(1, 2*k)
	return e.Opacity(o).Scale(0.6+0.4*k, 0.6+0.4*k)
}

// backOut goes a little past its end and comes back to it, as CSS's
// easeOutBack does.
var backOut = ui.CubicBezier(0.34, 1.56, 0.64, 1)

// fieldFocus is how a field's border follows the keyboard: the accent
// fades in as the field takes it, and out as it leaves.
var fieldFocus = ui.ElementTransition{Colors: true, Duration: 120 * time.Millisecond}

// floating styles a box that floats over the window: a dialog, as quick
// open's, a picker's or a prompt's, or a smaller box over an editor, as
// find's. The shadow is lighter on a light window, where a dark one looks
// smudged.
func (a *app) floating(c *ui.Context, box ui.Element, dialog bool) ui.Element {
	p := a.pal
	box.Background(c.Theme().Surface).Border(1, p.border)
	switch {
	case dialog && p.dark:
		box.Shadow(0, 16, 40, 0, ui.RGBA(0, 0, 0, 0.40))
	case dialog:
		box.Shadow(0, 12, 32, 0, ui.RGBA(20, 20, 40, 0.16))
	case p.dark:
		box.Shadow(0, 6, 18, 0, ui.RGBA(0, 0, 0, 0.30))
	default:
		box.Shadow(0, 4, 14, 0, ui.RGBA(20, 20, 40, 0.12))
	}
	return box
}

// dialogBack is the layer under a dialog, over the whole window, that
// the dialog is in: label names the dialog. It fades in, from a little
// above, as the dialog shows. The layer is new with the dialog, so a
// Transition would not move it: its motion starts the first time it is
// built for the dialog.
func (a *app) dialogBack(c *ui.Context, label string) ui.Element {
	back := ui.Column(c).Absolute().Left(0).Right(0).Bottom(0).AlignItems(ui.Center).Modal()
	if a.dialog != label {
		a.dialog = label
		back.Animate("in", 0, 0)
	}
	a.dialogBuilt = true
	k := back.Animate("in", 1, 130*time.Millisecond)
	return back.Top(-6 * (1 - k)).Opacity(k)
}
