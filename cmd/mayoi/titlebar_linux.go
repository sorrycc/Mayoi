//go:build linux

package main

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var installLinuxControlsOnce sync.Once

func installLinuxControlsCSS() {
	installLinuxControlsOnce.Do(func() {
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

		var gtkCssProviderNew func() uintptr
		var gtkCssProviderLoadFromData func(provider uintptr, data *byte, length int, err unsafe.Pointer) bool
		var gdkScreenGetDefault func() uintptr
		var gtkStyleContextAddProviderForScreen func(screen uintptr, provider uintptr, priority uint32)
		var gObjectUnref func(obj uintptr)

		purego.RegisterLibFunc(&gtkCssProviderNew, libGTK, "gtk_css_provider_new")
		purego.RegisterLibFunc(&gtkCssProviderLoadFromData, libGTK, "gtk_css_provider_load_from_data")
		purego.RegisterLibFunc(&gdkScreenGetDefault, libGDK, "gdk_screen_get_default")
		purego.RegisterLibFunc(&gtkStyleContextAddProviderForScreen, libGTK, "gtk_style_context_add_provider_for_screen")
		purego.RegisterLibFunc(&gObjectUnref, libGObject, "g_object_unref")

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
