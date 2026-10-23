package main

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/bg"
	"mayoi/internal/remote"
	"mayoi/locales"
)

// transfer is a copy to or from a host running in the background, which
// the status bar shows with how far it got.
type transfer struct {
	label    string
	progress *remote.Progress
}

// startTransfer shows a transfer in the status bar until endTransfer.
func (a *app) startTransfer(label string) *transfer {
	x := &transfer{label: label, progress: remote.NewProgress()}
	a.transfers = append(a.transfers, x)
	return x
}

func (a *app) endTransfer(x *transfer) {
	if i := slices.Index(a.transfers, x); i >= 0 {
		a.transfers = slices.Delete(a.transfers, i, i+1)
	}
}

// transferText is what the status bar says of the transfers: the first,
// with how much of it is done, and how many more there are.
func (a *app) transferText() string {
	if len(a.transfers) == 0 {
		return ""
	}
	x := a.transfers[0]
	s := x.label
	done, total := x.progress.Done(), x.progress.Total()
	switch {
	case total > 0:
		s += fmt.Sprintf(" %d%%", min(done*100/total, 100))
	case done > 0:
		s += " " + byteSize(done)
	}
	if n := len(a.transfers) - 1; n > 0 {
		s += fmt.Sprintf(" (+%d)", n)
	}
	return s
}

// transferStatus is the part of the status bar of the transfers running,
// drawn again a few times a second while they run: a click on it offers
// to cancel them.
func (a *app) transferStatus(c *ui.Context, text func(string) ui.Element) {
	label := a.transferText()
	if label == "" {
		return
	}
	p := a.pal
	b := ui.ButtonBase(c.Key("status:transfer")).Height(statusBarH-6).Padding(0, 6).Gap(5).Radius(4).
		AlignItems(ui.Center).Label(label).Tooltip(locales.TransferClickToCancel(label))
	if b.Hovered() {
		b.Background(p.hover)
	}
	b.Transition(hoverFade)
	b.Children(func() {
		ui.Spinner(c).Size(11, 11)
		text(label)
	})
	b.Menu(func(m *ui.Menu) {
		for _, x := range a.transfers {
			title := locales.TransferCancelLabel(strings.TrimSuffix(x.label, "…"))
			if len(a.transfers) == 1 {
				title = locales.TransferCancel()
			}
			if m.Item(title).Chosen() {
				x.progress.Cancel()
			}
		}
	})
	time.AfterFunc(250*time.Millisecond, a.invalidate)
}

// transferFailed says why a transfer failed, unless it was canceled.
func (a *app) transferFailed(what string, err error) {
	if errors.Is(err, remote.ErrCanceled) {
		a.tell(note{message: locales.TransferCanceled()})
		return
	}
	a.fail("%s", locales.FailWithError(what, err.Error()))
}

// byteSize is a count of bytes as people read it: 12 KB, 3.4 MB.
func byteSize(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d B", n)
	case n < 1000*1000:
		return fmt.Sprintf("%d KB", n/1000)
	case n < 1000*1000*1000:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	}
	return fmt.Sprintf("%.2f GB", float64(n)/1e9)
}

// A terminal of a host runs its programs there, which cannot read the
// files of this machine: a paste of files, of the Finder or of the
// explorer, or of an image alone, as a screenshot for Claude Code,
// uploads them to a temporary folder of the host and pastes their paths
// there. Files of the host itself paste their paths on it.

// sessionConn returns the connection of a terminal of a host, nil for
// one of this machine.
func sessionConn(s *session) *remote.Conn {
	if c, _, ok := remote.Lookup(s.dir); ok {
		return c
	}
	return nil
}

// terminalPaste takes a paste into a terminal, as OnPaste does, when the
// clipboard has files, as the Finder copies them, which any terminal gets
// as their paths, or, in a terminal of a host, an image alone.
func (a *app) terminalPaste(s *session, text string) bool {
	if srcs := a.clipboardSources(); len(srcs) > 0 {
		a.pastePaths(s, srcs)
		return true
	}
	if sessionConn(s) == nil || text != "" {
		return false
	}
	img := a.host.clipboardImage()
	if len(img) == 0 {
		return false
	}
	// A file of its own, named as a screenshot, which programs that take
	// images, as Claude Code, read when its path is pasted.
	dir, err := os.MkdirTemp("", "mayoi-image-")
	if err != nil {
		a.fail("%s", locales.FailPasteImage(err.Error()))
		return true
	}
	p := filepath.Join(dir, "image-"+time.Now().Format("20060102-150405")+".png")
	if err := os.WriteFile(p, img, 0o644); err != nil {
		os.RemoveAll(dir)
		a.fail("%s", locales.FailPasteImage(err.Error()))
		return true
	}
	a.uploadPaths(s, []string{p}, func() { os.RemoveAll(dir) })
	return true
}

// uploadPaths copies files to a temporary folder of the host of a
// terminal, in the background, and pastes their paths on the host into
// it, then runs done.
func (a *app) uploadPaths(s *session, srcs []string, done func()) {
	c := sessionConn(s)
	if !c.Connected() {
		a.fail("%s", locales.FailPasteNotConnected(c.Host))
		if done != nil {
			done()
		}
		return
	}
	label := locales.TransferUploadingTo(filepath.Base(srcs[0]), c.Host)
	if len(srcs) > 1 {
		label = locales.TransferUploadingTo(locales.ItemsCount(len(srcs)), c.Host)
	}
	x := a.startTransfer(label)
	go func() {
		got, err := toHost(c, srcs, "", x.progress)
		if done != nil {
			done()
		}
		a.post(func() {
			a.endTransfer(x)
			if err != nil {
				a.transferFailed("Could not paste", err)
				return
			}
			if slices.Contains(a.terms, s) {
				s.term.Paste(quotePaths(got))
			}
		})
	}()
}

// quotePaths are paths as a terminal takes them typed: quoted for the
// shell, with a space after each.
func quotePaths(paths []string) string {
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = shellQuote(p)
	}
	return strings.Join(quoted, " ") + " "
}

// askUpload asks for files of this machine to copy into dir, a folder of
// a host.
func (a *app) askUpload(dir string) {
	home, _ := os.UserHomeDir()
	a.host.choosePaths(locales.UploadTo(remote.Display(dir)), locales.UploadButton(), home, false, func(paths []string) {
		a.copyPathsInto(paths, dir)
	})
}

// askDownload asks for a folder of this machine to copy a file or folder
// of a host into, the Downloads folder first.
func (a *app) askDownload(src string) {
	home, _ := os.UserHomeDir()
	a.host.choosePaths(locales.DownloadTitle(path.Base(src)), locales.DownloadButton(), filepath.Join(home, "Downloads"), true, func(paths []string) {
		a.download(src, paths[0])
	})
}

// download copies a file or folder of a host into the folder dir of this
// machine in the background, and offers to show it once it is there.
func (a *app) download(src, dir string) {
	if !remote.IsConnected(src) {
		a.fail("%s", locales.FailDownloadNotConnected(remote.HostOf(src)))
		return
	}
	x := a.startTransfer(transferLabel([]string{src}, dir))
	bg.Go(func() {
		got, err := transferInto([]string{src}, dir, x.progress)
		a.post(func() {
			a.endTransfer(x)
			if err != nil {
				a.transferFailed(locales.FailDownload(path.Base(src)), err)
				return
			}
			to := got[0]
			a.tell(note{message: locales.Downloaded(filepath.Base(to)), label: revealLabel(), action: func() { a.host.reveal(to) }})
		})
	})
}
