package main

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Icons from Lucide (ISC license), drawn in the color of the text.
func lucide(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

var (
	iconCheckout      = lucide(`<path d="M9 5H7a4 4 0 0 0 0 8h13"/><path d="m16 9 4 4-4 4"/>`)
	iconCloud         = lucide(`<path d="M20 16.2A4.5 4.5 0 0 0 18 7.7a6 6 0 0 0-11.6 1.8A3.5 3.5 0 0 0 6 16.5h13"/>`)
	iconX             = lucide(`<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`)
	iconPlus          = lucide(`<path d="M5 12h14"/><path d="M12 5v14"/>`)
	iconChevronDown   = lucide(`<path d="m6 9 6 6 6-6"/>`)
	iconChevronUp     = lucide(`<path d="m18 15-6-6-6 6"/>`)
	iconChevronLeft   = lucide(`<path d="m15 18-6-6 6-6"/>`)
	iconFolder        = lucide(`<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>`)
	iconFolderOpen    = lucide(`<path d="m6 14 1.5-2.9A2 2 0 0 1 9.24 10H20a2 2 0 0 1 1.94 2.5l-1.54 6a2 2 0 0 1-1.95 1.5H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h3.9a2 2 0 0 1 1.69.9l.81 1.2a2 2 0 0 0 1.67.9H18a2 2 0 0 1 2 2v2"/>`)
	iconFile          = lucide(`<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/>`)
	iconFileCode      = lucide(`<path d="M10 12.5 8 15l2 2.5"/><path d="m14 12.5 2 2.5-2 2.5"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/>`)
	iconFileText      = lucide(`<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M10 9H8"/><path d="M16 13H8"/><path d="M16 17H8"/>`)
	iconFileJSON      = lucide(`<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M10 12a1 1 0 0 0-1 1v1a1 1 0 0 1-1 1 1 1 0 0 1 1 1v1a1 1 0 0 0 1 1"/><path d="M14 18a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1 1 1 0 0 1-1-1v-1a1 1 0 0 0-1-1"/>`)
	iconImage         = lucide(`<rect width="18" height="18" x="3" y="3" rx="2" ry="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21"/>`)
	iconSettings      = lucide(`<path d="M20 7h-9"/><path d="M14 17H5"/><circle cx="17" cy="17" r="3"/><circle cx="7" cy="7" r="3"/>`)
	iconLock          = lucide(`<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>`)
	iconServer        = lucide(`<rect width="20" height="8" x="2" y="2" rx="2" ry="2"/><rect width="20" height="8" x="2" y="14" rx="2" ry="2"/><line x1="6" x2="6.01" y1="6" y2="6"/><line x1="6" x2="6.01" y1="18" y2="18"/>`)
	iconPlug          = lucide(`<path d="M12 22v-5"/><path d="M9 8V2"/><path d="M15 8V2"/><path d="M18 8v5a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4V8Z"/>`)
	iconWifiOff       = lucide(`<path d="M12 20h.01"/><path d="M8.5 16.429a5 5 0 0 1 7 0"/><path d="M5 12.859a10 10 0 0 1 5.17-2.69"/><path d="M19 12.859a10 10 0 0 0-2.007-1.523"/><path d="M2 8.82a15 15 0 0 1 4.177-2.643"/><path d="M22 8.82a15 15 0 0 0-11.288-3.764"/><path d="m2 2 20 20"/>`)
	iconHome          = lucide(`<path d="M15 21v-8a1 1 0 0 0-1-1h-4a1 1 0 0 0-1 1v8"/><path d="M3 10a2 2 0 0 1 .709-1.528l7-5.999a2 2 0 0 1 2.582 0l7 5.999A2 2 0 0 1 21 10v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>`)
	iconParent        = lucide(`<path d="M14 9 9 4 4 9"/><path d="M20 20h-7a4 4 0 0 1-4-4V4"/>`)
	iconClock         = lucide(`<circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/>`)
	iconTerminal      = lucide(`<path d="m4 17 6-6-6-6"/><path d="M12 19h8"/>`)
	iconTrash         = lucide(`<path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/>`)
	iconSearch        = lucide(`<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>`)
	iconFileSearch    = lucide(`<path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M4.268 21a2 2 0 0 0 1.727 1H18a2 2 0 0 0 2-2V7l-5-5H6a2 2 0 0 0-2 2v3"/><path d="m9 18-1.5-1.5"/><circle cx="5" cy="14" r="3"/>`)
	iconArrowUp       = lucide(`<path d="m5 12 7-7 7 7"/><path d="M12 19V5"/>`)
	iconUpdate        = lucide(`<circle cx="12" cy="12" r="10"/><path d="m16 12-4-4-4 4"/><path d="M12 16V8"/>`)
	iconArrowDown     = lucide(`<path d="M12 5v14"/><path d="m19 12-7 7-7-7"/>`)
	iconArrowRight    = lucide(`<path d="M5 12h14"/><path d="m12 5 7 7-7 7"/>`)
	iconArrowLeft     = lucide(`<path d="M19 12H5"/><path d="m12 19-7-7 7-7"/>`)
	iconBranch        = lucide(`<line x1="6" x2="6" y1="3" y2="15"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M18 9a9 9 0 0 1-9 9"/>`)
	iconMerge         = lucide(`<circle cx="18" cy="18" r="3"/><circle cx="6" cy="6" r="3"/><path d="M6 21V9a9 9 0 0 0 9 9"/>`)
	iconRefresh       = lucide(`<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/>`)
	iconCollapse      = lucide(`<path d="m7 20 5-5 5 5"/><path d="m7 4 5 5 5-5"/>`)
	iconFilePlus      = lucide(`<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M9 15h6"/><path d="M12 18v-6"/>`)
	iconFolderPlus    = lucide(`<path d="M12 10v6"/><path d="M9 13h6"/><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>`)
	iconMaximize      = lucide(`<path d="M15 3h6v6"/><path d="M9 21H3v-6"/><path d="M21 3l-7 7"/><path d="M3 21l7-7"/>`)
	iconMinimize      = lucide(`<path d="m14 10 7-7"/><path d="M20 10h-6V4"/><path d="m3 21 7-7"/><path d="M4 14h6v6"/>`)
	iconCase          = lucide(`<path d="m3 15 4-8 4 8"/><path d="M4 13h6"/><circle cx="18" cy="12" r="3"/><path d="M21 9v6"/>`)
	iconWord          = lucide(`<circle cx="7" cy="12" r="3"/><path d="M10 9v6"/><circle cx="17" cy="12" r="3"/><path d="M14 7v8"/><path d="M22 17v1c0 .5-.5 1-1 1H3c-.5 0-1-.5-1-1v-1"/>`)
	iconRegex         = lucide(`<path d="M17 3v10"/><path d="m12.67 5.5 8.66 5"/><path d="m12.67 10.5 8.66-5"/><path d="M9 17a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v2a2 2 0 0 0 2 2h2a2 2 0 0 0 2-2v-2z"/>`)
	iconReplace       = lucide(`<path d="M14 4a2 2 0 0 1 2-2"/><path d="M16 10a2 2 0 0 1-2-2"/><path d="M20 2a2 2 0 0 1 2 2"/><path d="M22 8a2 2 0 0 1-2 2"/><path d="m3 7 3 3 3-3"/><path d="M6 10V5a3 3 0 0 1 3-3h1"/><rect x="2" y="14" width="8" height="8" rx="2"/>`)
	iconReplaceAll    = lucide(`<path d="M14 14a2 2 0 0 1 2-2"/><path d="M14 4a2 2 0 0 1 2-2"/><path d="M16 10a2 2 0 0 1-2-2"/><path d="M20 14a2 2 0 0 1 2 2"/><path d="M20 2a2 2 0 0 1 2 2"/><path d="M22 8a2 2 0 0 1-2 2"/><path d="m3 7 3 3 3-3"/><path d="M6 10V5a3 3 0 0 1 3-3h1"/><rect x="2" y="14" width="8" height="8" rx="2"/>`)
	iconChevronRight  = lucide(`<path d="m9 18 6-6-6-6"/>`)
	iconChevronsRight = lucide(`<path d="m6 17 5-5-5-5"/><path d="m13 17 5-5-5-5"/>`)
	iconChevronsLeft  = lucide(`<path d="m11 17-5-5 5-5"/><path d="m18 17-5-5 5-5"/>`)
	iconCommand       = lucide(`<path d="M15 6v12a3 3 0 1 0 3-3H6a3 3 0 1 0 3 3V6a3 3 0 1 0-3 3h12a3 3 0 1 0-3-3"/>`)
	iconMenu          = lucide(`<line x1="4" x2="20" y1="12" y2="12"/><line x1="4" x2="20" y1="6" y2="6"/><line x1="4" x2="20" y1="18" y2="18"/>`)
	iconSidebar       = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/>`)
	iconSidebarRight  = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M15 3v18"/>`)
	iconFunction      = lucide(`<rect width="18" height="18" x="3" y="3" rx="2" ry="2"/><path d="M9 17c2 0 2.8-1 2.8-2.8V10c0-2 1-3.3 3.2-3"/><path d="M9 11.2h5.7"/>`)
	iconBox           = lucide(`<path d="M21 8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16Z"/><path d="m3.3 7 8.7 5 8.7-5"/><path d="M12 22V12"/>`)
	iconBraces        = lucide(`<path d="M8 3H7a2 2 0 0 0-2 2v5a2 2 0 0 1-2 2 2 2 0 0 1 2 2v5c0 1.1.9 2 2 2h1"/><path d="M16 21h1a2 2 0 0 0 2-2v-5c0-1.1.9-2 2-2a2 2 0 0 1-2-2V5a2 2 0 0 0-2-2h-1"/>`)
	iconType          = lucide(`<polyline points="4 7 4 4 20 4 20 7"/><line x1="9" x2="15" y1="20" y2="20"/><line x1="12" x2="12" y1="4" y2="20"/>`)
	iconHash          = lucide(`<line x1="4" x2="20" y1="9" y2="9"/><line x1="4" x2="20" y1="15" y2="15"/><line x1="10" x2="8" y1="3" y2="21"/><line x1="16" x2="14" y1="3" y2="21"/>`)
	iconColumns       = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M12 3v18"/>`)
	iconToEditor      = lucide(`<path d="M7 7h10v10"/><path d="M7 17 17 7"/>`)
	iconFiles         = lucide(`<path d="M20 7h-3a2 2 0 0 1-2-2V2"/><path d="M9 18a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h7l4 4v10a2 2 0 0 1-2 2Z"/><path d="M3 7.6v12.8A1.6 1.6 0 0 0 4.6 22h9.8"/>`)
	iconTag           = lucide(`<path d="M12.586 2.586A2 2 0 0 0 11.172 2H4a2 2 0 0 0-2 2v7.172a2 2 0 0 0 .586 1.414l8.704 8.704a2.426 2.426 0 0 0 3.42 0l6.58-6.58a2.426 2.426 0 0 0 0-3.42z"/><circle cx="7.5" cy="7.5" r=".5" fill="currentColor"/>`)
	iconCommit        = lucide(`<circle cx="12" cy="12" r="3"/><line x1="3" x2="9" y1="12" y2="12"/><line x1="15" x2="21" y1="12" y2="12"/>`)
	iconMinus         = lucide(`<path d="M5 12h14"/>`)
	iconUndo          = lucide(`<path d="M9 14 4 9l5-5"/><path d="M4 9h10.5a5.5 5.5 0 0 1 5.5 5.5a5.5 5.5 0 0 1-5.5 5.5H11"/>`)
	iconCheck         = lucide(`<path d="M20 6 9 17l-5-5"/>`)
	iconKeyboard      = lucide(`<path d="M10 8h.01"/><path d="M12 12h.01"/><path d="M14 8h.01"/><path d="M16 12h.01"/><path d="M18 8h.01"/><path d="M6 8h.01"/><path d="M7 16h10"/><path d="M8 12h.01"/><rect width="20" height="16" x="2" y="4" rx="2"/>`)
	iconPush          = lucide(`<path d="m18 9-6-6-6 6"/><path d="M12 3v14"/><path d="M5 21h14"/>`)
	iconPull          = lucide(`<path d="M12 17V3"/><path d="m6 11 6 6 6-6"/><path d="M19 21H5"/>`)
	iconFetch         = lucide(`<path d="M12 13v8l-4-4"/><path d="m12 21 4-4"/><path d="M4.393 15.269A7 7 0 1 1 15.71 8h1.79a4.5 4.5 0 0 1 2.436 8.284"/>`)
	iconDiff          = lucide(`<circle cx="18" cy="18" r="3"/><circle cx="6" cy="6" r="3"/><path d="M13 6h3a2 2 0 0 1 2 2v7"/><path d="M11 18H8a2 2 0 0 1-2-2V9"/>`)
	iconRows          = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M3 12h18"/>`)
	iconMore          = lucide(`<circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/><circle cx="5" cy="12" r="1"/>`)
	iconWrap          = lucide(`<path d="m16 16-2 2 2 2"/><path d="M3 12h15a3 3 0 1 1 0 6h-4"/><path d="M3 18h7"/><path d="M3 6h18"/>`)
	iconHighlight     = lucide(`<path d="m9 11-6 6v3h9l3-3"/><path d="m22 12-4.6 4.6a2 2 0 0 1-2.8 0l-5.2-5.2a2 2 0 0 1 0-2.8L14 4l8 8Z"/>`)
	iconUnfold        = lucide(`<path d="M12 22v-6"/><path d="M12 8V2"/><path d="M4 12H2"/><path d="M10 12H8"/><path d="M16 12h-2"/><path d="M22 12h-2"/><path d="m15 19-3 3-3-3"/><path d="m15 5-3-3-3 3"/>`)
	iconComment       = lucide(`<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>`)
	iconSend          = lucide(`<path d="M14.536 21.686a.5.5 0 0 0 .937-.024l6.5-19a.496.496 0 0 0-.635-.635l-19 6.5a.5.5 0 0 0-.024.937l7.93 3.18a2 2 0 0 1 1.112 1.11z"/><path d="m21.854 2.147-10.94 10.939"/>`)
	iconCopy          = lucide(`<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>`)
	iconSparkles      = lucide(`<path d="M9.937 15.5A2 2 0 0 0 8.5 14.063l-6.135-1.582a.5.5 0 0 1 0-.962L8.5 9.936A2 2 0 0 0 9.937 8.5l1.582-6.135a.5.5 0 0 1 .963 0L14.063 8.5A2 2 0 0 0 15.5 9.937l6.135 1.581a.5.5 0 0 1 0 .964L15.5 14.063a2 2 0 0 0-1.437 1.437l-1.582 6.135a.5.5 0 0 1-.963 0z"/><path d="M20 3v4"/><path d="M22 5h-4"/><path d="M4 17v2"/><path d="M5 18H3"/>`)
	iconStopSquare    = lucide(`<rect width="12" height="12" x="6" y="6" rx="2"/>`)
	iconPencil        = lucide(`<path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"/>`)
	iconOpenFile      = lucide(`<path d="M15 3h6v6"/><path d="M10 14 21 3"/><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>`)
	// The icons of GitHub's alerts in a Markdown preview.
	iconInfo          = lucide(`<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>`)
	iconLightbulb     = lucide(`<path d="M15 14c.2-1 .7-1.7 1.5-2.5 1-.9 1.5-2.2 1.5-3.5A6 6 0 0 0 6 8c0 1 .2 2.2 1.5 3.5.7.7 1.3 1.5 1.5 2.5"/><path d="M9 18h6"/><path d="M10 22h4"/>`)
	iconMessageAlert  = lucide(`<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/><path d="M12 7v2"/><path d="M12 13h.01"/>`)
	iconTriangleAlert = lucide(`<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><path d="M12 9v4"/><path d="M12 17h.01"/>`)
	iconOctagonAlert  = lucide(`<path d="M12 16h.01"/><path d="M12 8v4"/><path d="M15.312 2a2 2 0 0 1 1.414.586l4.688 4.688A2 2 0 0 1 22 8.688v6.624a2 2 0 0 1-.586 1.414l-4.688 4.688a2 2 0 0 1-1.414.586H8.688a2 2 0 0 1-1.414-.586l-4.688-4.688A2 2 0 0 1 2 15.312V8.688a2 2 0 0 1 .586-1.414l4.688-4.688A2 2 0 0 1 8.688 2z"/>`)
)

// fileIcon returns the icon of a file and its color, by its name.
func fileIcon(p *palette, name string) (*ui.SVG, ui.Color) {
	lower := strings.ToLower(name)
	ext := strings.TrimPrefix(filepath.Ext(lower), ".")
	switch lower {
	case "go.mod", "go.sum", "go.work":
		return iconFileCode, langColor(p, "#00add8", "#00809f")
	case "package.json", "tsconfig.json", "bun.lock", "package-lock.json", "yarn.lock", "pnpm-lock.yaml":
		return iconFileJSON, langColor(p, "#cbcb41", "#9a8a00")
	case "makefile", "dockerfile", "justfile":
		return iconSettings, langColor(p, "#e37933", "#b5571a")
	case "license", "license.md", "license.txt":
		return iconFileText, langColor(p, "#d4af37", "#a07d10")
	}
	if strings.HasPrefix(lower, ".env") {
		return iconLock, langColor(p, "#e8c482", "#a86a0c")
	}
	if strings.HasPrefix(lower, ".") && ext == strings.TrimPrefix(lower, ".") {
		return iconSettings, p.muted // dotfiles: .gitignore, .editorconfig
	}
	switch ext {
	case "go":
		return iconFileCode, langColor(p, "#00add8", "#00809f")
	case "ts", "tsx", "mts", "cts":
		return iconFileCode, langColor(p, "#4d8fdf", "#2f6fc0")
	case "js", "jsx", "mjs", "cjs":
		return iconFileCode, langColor(p, "#e8d44d", "#a68f00")
	case "rs":
		return iconFileCode, langColor(p, "#dea584", "#b0643a")
	case "py":
		return iconFileCode, langColor(p, "#5a9fd4", "#3572a5")
	case "rb":
		return iconFileCode, langColor(p, "#e0525a", "#c0303a")
	case "swift":
		return iconFileCode, langColor(p, "#f0814b", "#d0602a")
	case "c", "h", "cpp", "cc", "hpp", "m", "mm":
		return iconFileCode, langColor(p, "#7aa6da", "#4a76b0")
	case "java", "kt", "kts", "scala":
		return iconFileCode, langColor(p, "#e76f51", "#c04a2b")
	case "html", "htm", "vue", "svelte", "astro":
		return iconFileCode, langColor(p, "#e8714a", "#c4492a")
	case "css", "scss", "sass", "less":
		return iconFileCode, langColor(p, "#a074c4", "#7a4fa8")
	case "sh", "bash", "zsh", "fish":
		return iconTerminal, langColor(p, "#89e051", "#4f9a1f")
	case "json", "jsonc", "json5":
		return iconFileJSON, langColor(p, "#cbcb41", "#9a8a00")
	case "yaml", "yml", "toml", "ini", "conf", "cfg", "xml", "plist":
		return iconSettings, langColor(p, "#c9a0dc", "#8a5aa8")
	case "md", "mdx", "markdown", "txt", "rst":
		return iconFileText, langColor(p, "#6ba4d8", "#3d78ad")
	case "png", "jpg", "jpeg", "gif", "webp", "svg", "ico", "icns", "bmp":
		return iconImage, langColor(p, "#a6c56a", "#5f8a26")
	case "lock", "sum":
		return iconLock, p.faint
	}
	return iconFile, p.muted
}

// langColor is the color of a language's icon, as it is in a dark
// window and in a light one. A mono palette has a shade of its own for
// it, as bright as the dark one.
func langColor(p *palette, dark, light string) ui.Color {
	if p.mono {
		c := ui.Hex(dark)
		luma := (0.299*float32(c.R) + 0.587*float32(c.G) + 0.114*float32(c.B)) / 255
		return p.muted.Mix(p.accent, luma)
	}
	if p.dark {
		return ui.Hex(dark)
	}
	return ui.Hex(light)
}
