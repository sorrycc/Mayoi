//go:build linux

package main

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo/ui"
)

var (
	initGTKOnce sync.Once
	gtkReady    bool

	gtkCssProviderNew                      func() uintptr
	gtkCssProviderLoadFromData             func(provider uintptr, data *byte, length int, err unsafe.Pointer) bool
	gdkScreenGetDefault                    func() uintptr
	gtkStyleContextAddProviderForScreen    func(screen uintptr, provider uintptr, priority uint32)
	gtkStyleContextRemoveProviderForScreen func(screen uintptr, provider uintptr)
	gObjectUnref                           func(obj uintptr)

	installLinuxControlsOnce sync.Once

	themeProvider     uintptr
	themeProviderInit bool
	lastMuted         ui.Color
	lastText          ui.Color
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

func installLinuxControlsCSS() {
	installLinuxControlsOnce.Do(func() {
		initGTK()
		if !gtkReady {
			return
		}

		provider := gtkCssProviderNew()
		if provider == 0 {
			return
		}
		css := `
headerbar.mygo-window-controls {
    background: none;
    border: none;
    box-shadow: none;
    padding: 0;
    margin: 0;
}
headerbar.mygo-window-controls.mygo-sized {
    min-height: 0;
}
headerbar.mygo-window-controls label.title,
headerbar.mygo-window-controls label.subtitle,
headerbar.mygo-window-controls .title {
    font-size: 0px;
    min-width: 0px;
    min-height: 0px;
    margin: 0px;
    padding: 0px;
}
`
		cssBytes := append([]byte(css), 0)
		gtkCssProviderLoadFromData(provider, &cssBytes[0], len(cssBytes)-1, nil)
		screen := gdkScreenGetDefault()
		if screen != 0 {
			gtkStyleContextAddProviderForScreen(screen, provider, 800)
		}
		gObjectUnref(provider)
	})
}

// updateLinuxControlsTheme styles the window controls with the active
// palette, replacing the screen's CSS provider when the theme changes.
func updateLinuxControlsTheme(p *palette) {
	if p == nil {
		return
	}
	if themeProviderInit && p.muted == lastMuted && p.text == lastText {
		return
	}
	initGTK()
	if !gtkReady {
		return
	}
	screen := gdkScreenGetDefault()
	if screen == 0 {
		return
	}

	css := fmt.Sprintf(`
headerbar.mygo-window-controls button.titlebutton,
headerbar.mygo-window-controls:backdrop button.titlebutton,
headerbar.mygo-window-controls button.titlebutton:backdrop,
headerbar.mygo-window-controls:backdrop button.titlebutton:backdrop {
    color: %s;
    -gtk-icon-shadow: none;
    text-shadow: none;
}
headerbar.mygo-window-controls button.titlebutton:hover,
headerbar.mygo-window-controls button.titlebutton:active {
    color: %s;
}
`, cssColor(p.muted), cssColor(p.text))

	provider := gtkCssProviderNew()
	if provider == 0 {
		return
	}
	cssBytes := append([]byte(css), 0)
	gtkCssProviderLoadFromData(provider, &cssBytes[0], len(cssBytes)-1, nil)

	if themeProvider != 0 {
		gtkStyleContextRemoveProviderForScreen(screen, themeProvider)
	}
	gtkStyleContextAddProviderForScreen(screen, provider, 800)
	gObjectUnref(provider)

	themeProvider = provider
	themeProviderInit = true
	lastMuted = p.muted
	lastText = p.text
}

// cssColor formats a color as a CSS hex or rgba string.
func cssColor(c ui.Color) string {
	if c.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("rgba(%d, %d, %d, %f)", c.R, c.G, c.B, float64(c.A)/255.0)
}
