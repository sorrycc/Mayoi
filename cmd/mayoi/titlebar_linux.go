//go:build linux

package main

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var (
	initGTKOnce sync.Once
	gtkReady    bool

	gtkCssProviderNew                      func() uintptr
	gtkCssProviderLoadFromData             func(provider uintptr, data string, length int, err uintptr) bool
	gdkScreenGetDefault                    func() uintptr
	gtkStyleContextAddProviderForScreen    func(screen uintptr, provider uintptr, priority uint32)
	gtkStyleContextRemoveProviderForScreen func(screen uintptr, provider uintptr)
	gObjectUnref                           func(obj uintptr)

	// controlsMuted and controlsText are the colors the window controls
	// were last styled with, on the view's goroutine; controlsProvider is
	// their style, on the main thread.
	controlsMuted, controlsText ui.Color
	controlsStyled              bool
	controlsProvider            uintptr
)

func initGTK() {
	initGTKOnce.Do(func() {
		libGTK, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		libGDK, err := purego.Dlopen("libgdk-3.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		libGObject, err := purego.Dlopen("libgobject-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&gtkCssProviderNew, libGTK, "gtk_css_provider_new")
		purego.RegisterLibFunc(&gtkCssProviderLoadFromData, libGTK, "gtk_css_provider_load_from_data")
		purego.RegisterLibFunc(&gdkScreenGetDefault, libGDK, "gdk_screen_get_default")
		purego.RegisterLibFunc(&gtkStyleContextAddProviderForScreen, libGTK, "gtk_style_context_add_provider_for_screen")
		purego.RegisterLibFunc(&gtkStyleContextRemoveProviderForScreen, libGTK, "gtk_style_context_remove_provider_for_screen")
		purego.RegisterLibFunc(&gObjectUnref, libGObject, "g_object_unref")
		gtkReady = true
	})
}

// controlsCSS styles the window controls in the palette's colors, flat as
// Mayoi's icons, with the bars' empty title labels collapsed so that they
// take no clicks from the title bar under them. mygo clears the bars.
const controlsCSS = `
headerbar.mygo-window-controls { padding: 0; margin: 0; }
headerbar.mygo-window-controls .title,
headerbar.mygo-window-controls label.subtitle {
	font-size: 0; min-width: 0; min-height: 0; margin: 0; padding: 0;
}
headerbar.mygo-window-controls button.titlebutton,
headerbar.mygo-window-controls button.titlebutton:backdrop {
	color: %s; -gtk-icon-shadow: none; text-shadow: none;
}
headerbar.mygo-window-controls button.titlebutton:hover,
headerbar.mygo-window-controls button.titlebutton:active {
	color: %s;
}
`

// updateLinuxControlsTheme styles the window controls with p, replacing the
// screen's style when the colors change.
func updateLinuxControlsTheme(p *palette) {
	if p == nil || controlsStyled && p.muted == controlsMuted && p.text == controlsText {
		return
	}
	controlsStyled, controlsMuted, controlsText = true, p.muted, p.text
	css := fmt.Sprintf(controlsCSS, cssColor(p.muted), cssColor(p.text))
	// The view runs in GTK's draw callback, where a new style would have
	// the widgets being drawn styled anew: replace it after, as setMenu
	// does the menu bar.
	go mygo.RunOnMain(func() {
		initGTK()
		if !gtkReady {
			return
		}
		screen := gdkScreenGetDefault()
		if screen == 0 {
			return
		}
		provider := gtkCssProviderNew()
		if provider == 0 {
			return
		}
		gtkCssProviderLoadFromData(provider, css, len(css), 0)
		if controlsProvider != 0 {
			gtkStyleContextRemoveProviderForScreen(screen, controlsProvider)
			gObjectUnref(controlsProvider)
		}
		gtkStyleContextAddProviderForScreen(screen, provider, 800) // GTK_STYLE_PROVIDER_PRIORITY_USER
		controlsProvider = provider
	})
}

// cssColor formats a color as a CSS hex or rgba string.
func cssColor(c ui.Color) string {
	if c.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("rgba(%d, %d, %d, %.3f)", c.R, c.G, c.B, float64(c.A)/255)
}
