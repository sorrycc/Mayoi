package main

import (
	"os"

	"mayoi/internal/remote"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// folderPrefs is what the window's folder chooses of the preferences, in
// its .mayoi/settings.json: its file, what stops watching it, and the
// preferences of the window as of gen, the person's with the folder's.
type folderPrefs struct {
	f    *settings.Folder
	stop func()
	eff  settings.Prefs
	gen  [2]uint64
	made bool
}

// prefs is the preferences of the window: the person's, with those its
// folder chooses. A change goes through setPref.
func (a *app) prefs() *settings.Prefs {
	fp := a.folderPrefs
	if fp == nil || len(fp.f.Keys()) == 0 {
		return &a.settings.Prefs
	}
	if gen := [2]uint64{a.settings.Gen(), fp.f.Gen()}; !fp.made || gen != fp.gen {
		fp.eff, fp.gen, fp.made = fp.f.Apply(a.settings.Prefs), gen, true
	}
	return &fp.eff
}

// setPref changes the preference of key as set does: in the folder's file
// when the folder chooses it, else the person's.
func (a *app) setPref(key string, set func(p *settings.Prefs)) {
	if fp := a.folderPrefs; fp != nil && fp.f.Has(key) {
		p := *a.prefs()
		set(&p)
		if err := fp.f.Set(key, p.Raw(key)); err != nil {
			a.fail("%s", locales.FailSave(fp.f.Path, err.Error()))
		}
		a.folderPrefsChanged()
		return
	}
	set(&a.settings.Prefs)
	a.settings.Changed()
}

// loadFolderPrefs reads the preferences the folder dir chooses, and
// watches them: a remote folder chooses none.
func (a *app) loadFolderPrefs(dir string) {
	a.dropFolderPrefs()
	if remote.Is(dir) {
		return
	}
	fp := &folderPrefs{f: settings.LoadFolder(dir)}
	if stop, err := fp.f.Watch(func() { a.post(a.folderPrefsChanged) }); err == nil {
		fp.stop = stop
	}
	a.folderPrefs = fp
}

// dropFolderPrefs stops watching the preferences of the folder, as it
// closes.
func (a *app) dropFolderPrefs() {
	if fp := a.folderPrefs; fp != nil && fp.stop != nil {
		fp.stop()
	}
	a.folderPrefs = nil
}

// folderPrefsChanged applies what the folder chooses anew, as its file
// changed: the files save themselves once it turns auto save on.
func (a *app) folderPrefsChanged() {
	if a.prefs().AutoSave {
		a.autoSaveAll()
	}
	a.invalidate()
	a.host.menuChanged()
}

// openFolderSettings opens the folder's own .mayoi/settings.json, made
// with no preferences when there is none.
func (a *app) openFolderSettings() {
	if a.root == "" || remote.Is(a.root) {
		a.tell(note{message: locales.PrefsNoFolder()})
		return
	}
	path := settings.FolderPath(a.root)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := settings.WriteFile(path, []byte("{\n}\n")); err != nil {
			a.fail("%s", locales.FailMake(path, err.Error()))
			return
		}
	}
	a.openFile(path, true)
}
