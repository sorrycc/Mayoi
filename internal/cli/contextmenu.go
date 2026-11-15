package cli

// contextMenu is a class whose right-click menu in Explorer has the
// "Open with Mayoi" item, and the argument it passes Mayoi: a file's or a
// folder's path, or the folder whose background was clicked.
type contextMenu struct{ class, arg string }

// contextMenus are the classes the installer adds the item to too, as
// mygo.json's windows.contextMenu has it.
var contextMenus = []contextMenu{
	{`*`, `%1`},
	{`Directory`, `%1`},
	{`Directory\Background`, `%V`},
}

// key is the item's key under HKCU, named the app's identifier as the
// installer names it, so that the two never make two items.
func (m contextMenu) key() string { return `Software\Classes\` + m.class + `\shell\` + bundleID }

// command is what the item runs: Mayoi.exe at exe with the path.
func (m contextMenu) command(exe string) string { return `"` + exe + `" "` + m.arg + `"` }
