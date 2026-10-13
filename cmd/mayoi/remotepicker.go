package main

import (
	"fmt"
	"mayoi/internal/remotefs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

// remotePicker browses a single SSH host. A successful listing owns dir;
// editable input never changes the workspace until Open Folder is chosen.
type remotePicker struct {
	target                     remotefs.Target
	dir, input, err            string
	folders                    []string
	loading, hidden, newWindow bool
	generation                 int
}

func (a *app) chooseRemoteFolder() {
	if dir := a.folder(); remotefs.IsRemote(dir) {
		a.browseRemote(dir, false, false)
	} else {
		a.chooseRemote()
	}
}

func (a *app) browseRemote(dir string, home, newWindow bool) {
	target, _, ok := remotefs.Split(dir)
	if !ok {
		return
	}
	r := &remotePicker{target: target, newWindow: newWindow}
	a.remotePicker = r
	a.readRemoteFolder(r, dir, home)
}

// All connection, home and directory IO runs off the frame thread. Closing or
// navigating again invalidates stale results, including late failures.
func (a *app) readRemoteFolder(r *remotePicker, dir string, home bool) {
	r.generation++
	gen := r.generation
	r.loading, r.err = true, ""
	_, input, _ := remotefs.Split(dir)
	r.input = input
	if home {
		r.input = "~"
	}
	go func() {
		err := remotefs.Connect(dir)
		if err == nil && home {
			dir, err = remotefs.Home(dir)
		}
		var folders []string
		if err == nil {
			var entries []os.DirEntry
			entries, err = remotefs.ReadDir(dir)
			for _, entry := range entries {
				isDir := entry.IsDir()
				if entry.Type()&os.ModeSymlink != 0 {
					info, e := remotefs.Stat(filepath.Join(dir, entry.Name()))
					isDir = e == nil && info.IsDir()
				}
				if isDir {
					folders = append(folders, entry.Name())
				}
			}
		}
		slices.SortFunc(folders, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
		a.post(func() {
			if a.remotePicker != r || r.generation != gen {
				return
			}
			r.loading = false
			if err != nil {
				r.err = err.Error()
				return
			}
			r.dir, r.folders = filepath.Clean(dir), folders
			_, r.input, _ = remotefs.Split(dir)
		})
	}()
}

func (a *app) remoteFolderGo(r *remotePicker) {
	input := strings.TrimSpace(r.input)
	switch {
	case input == "~":
		a.readRemoteFolder(r, r.target.Virtual("/"), true)
	case strings.HasPrefix(input, "/") && !strings.ContainsRune(input, 0):
		a.readRemoteFolder(r, r.target.Virtual(input), false)
	default:
		r.err = "Enter an absolute remote path, or ~ for your home folder."
	}
}

// openRemoteProject explicitly replaces the current project after the regular
// unsaved-document flow. New-window opens leave every current tab untouched.
func (a *app) openRemoteProject(dir string, newWindow bool) {
	if newWindow {
		a.host.newWindow(dir)
		return
	}
	if dir == a.root || a.host.showFolder(dir) {
		return
	}
	ses := a.session()
	a.closeAll(func() { a.stash(ses); a.closeTerminals(); a.openSession(dir) })
}

func (a *app) remotePickerView(c *ui.Context) {
	r := a.remotePicker
	if r == nil {
		return
	}
	p := a.pal
	_, height := c.Size()
	closePicker := func() {
		if a.remotePicker == r {
			a.remotePicker = nil
			a.focusColumn()
		}
	}
	open := func(newWindow bool) {
		dir := r.dir
		closePicker()
		a.post(func() { a.openRemoteProject(dir, newWindow) })
	}
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "remote-folder")
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(620).MaxWidthPercent(94).Margin(titleBarH+8, 0, 0, 0).Padding(16).Gap(10).Radius(10), true).Role(ui.RoleDialog).Label("Open Remote Folder")
			box.Children(func() {
				ui.Text(c, "Open Remote Folder").FontSize(16).FontWeight(600).TextColor(p.text)
				ui.Text(c, "SSH · "+r.target.Host).FontSize(12).TextColor(p.muted).SingleLine()
				ui.Text(c, "Browse to your project, then open the current folder.").FontSize(12).TextColor(p.muted)
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					_, current, _ := remotefs.Split(r.dir)
					if ui.Button(c, "Up").Disabled(r.loading || r.dir == "" || current == "/").Clicked() {
						a.readRemoteFolder(r, r.target.Virtual(filepath.Dir(current)), false)
					}
					if ui.Button(c, "Home").Disabled(r.loading).Clicked() {
						a.readRemoteFolder(r, r.target.Virtual("/"), true)
					}
					field := ui.TextInput(c.Key("remote-path"), &r.input).Grow(1).MinWidth(0).FontSize(13).AutoFocus().Label("Remote folder path")
					if field.Submitted() {
						a.remoteFolderGo(r)
					}
					if ui.Button(c, "Go").Disabled(r.loading || strings.TrimSpace(r.input) == "").Clicked() {
						a.remoteFolderGo(r)
					}
				})
				if r.err != "" {
					ui.Text(c, r.err).FontSize(12).TextColor(p.danger)
				}
				ui.Scroll(c.Key("remote-directories:" + r.dir)).Height(max(float32(70), min(float32(300), height-330))).Children(func() {
					ui.Column(c).Gap(2).Children(func() {
						if r.loading {
							ui.Text(c, "Connecting / loading folders…").FontSize(13).TextColor(p.muted).Padding(12)
							return
						}
						count := 0
						for _, name := range r.folders {
							if !r.hidden && strings.HasPrefix(name, ".") {
								continue
							}
							count++
							row := ui.ButtonBase(c.Key("remote-dir:"+name)).FillWidth().Padding(8, 10).Gap(8).Radius(5).AlignItems(ui.Center).Label("Browse " + name)
							if row.Hovered() {
								row.Background(p.hover)
							}
							row.Children(func() {
								ui.Icon(c, iconFolder).FontSize(15).TextColor(p.accent)
								ui.Text(c, name).FontSize(13).TextColor(p.text).SingleLine().Grow(1).Shrink(1)
								ui.Text(c, "›").FontSize(16).TextColor(p.faint)
							})
							if row.Clicked() {
								dir := filepath.Join(r.dir, name)
								a.post(func() {
									if a.remotePicker == r {
										a.readRemoteFolder(r, dir, false)
									}
								})
							}
						}
						if count == 0 && r.err == "" {
							ui.Text(c, "No subfolders. You can open this folder.").FontSize(13).TextColor(p.muted).Padding(12)
						}
					})
				})
				label := "Show hidden folders"
				if r.hidden {
					label = "Hide hidden folders"
				}
				if ui.Button(c, label).Clicked() {
					r.hidden = !r.hidden
				}
				_, current, _ := remotefs.Split(r.dir)
				ui.Text(c, fmt.Sprintf("Selected: %s", current)).FontSize(12).TextColor(p.muted).SingleLine()
				disabled := r.loading || r.dir == "" || r.err != "" || strings.TrimSpace(r.input) != current
				ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c, "Cancel").Clicked() {
						closePicker()
					}
					if !r.newWindow && ui.Button(c, "New Window").Disabled(disabled).Clicked() {
						open(true)
					}
					label := "Open Folder"
					if r.newWindow {
						label = "Open in New Window"
					}
					if ui.PrimaryButton(c, label).Disabled(disabled).Clicked() {
						open(r.newWindow)
					}
				})
			})
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				closePicker()
			}
		})
	})
}

// The virtual namespace key is an implementation detail, including at /.
func (a *app) workspaceName() string {
	if target, path, ok := remotefs.Split(a.root); ok {
		if path == "/" {
			return target.Host
		}
		return filepath.Base(path)
	}
	return filepath.Base(a.root)
}
