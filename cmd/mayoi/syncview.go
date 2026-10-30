package main

import (
	"cmp"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/keymap"
	"mayoi/internal/settings"
	"mayoi/locales"
)

var (
	iconCloudCheck = lucide(`<path d="m17 15-5.5 5.5L9 18"/><path d="M5 17.743A7 7 0 1 1 15.71 10h1.79a4.5 4.5 0 0 1 1.5 8.742"/>`)
	iconCloudOff   = lucide(`<path d="m2 2 20 20"/><path d="M5.782 5.782A7 7 0 0 0 9 19h8.5a4.5 4.5 0 0 0 1.307-.193"/><path d="M21.532 16.5A4.5 4.5 0 0 0 17.5 10h-1.79A7.008 7.008 0 0 0 10 5.07"/>`)
	iconCloudAlert = lucide(`<path d="M12 12v4"/><path d="M12 20h.01"/><path d="M17 18h.5a1 1 0 0 0 0-9h-1.79A7 7 0 1 0 7 17.708"/>`)
	iconLogOut     = lucide(`<path d="m16 17 5-5-5-5"/><path d="M21 12H9"/><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/>`)
)

// syncStatusW is the width of settings sync's part of the status bar.
const syncStatusW = 2*6 + 12 + 2

// syncLook is how settings sync shows, as it is: its icon and color, a
// headline and a line under it.
type syncLook struct {
	icon            *ui.SVG
	color           ui.Color
	title, subtitle string
}

func (a *app) syncLook(si syncInfo, now time.Time) syncLook {
	p := a.pal
	synced := locales.SyncNever()
	if !si.syncedAt.IsZero() {
		synced = locales.SyncSyncedAgo(ago(si.syncedAt, now))
	}
	switch {
	case si.busy:
		return syncLook{iconCloud, p.accent, locales.SyncSyncing(), synced}
	case si.failure != "":
		sub := si.failure
		if !si.retryAt.IsZero() {
			sub = locales.SyncRetryIn(sub, countdown(si.retryAt.Sub(now)))
		}
		return syncLook{iconCloudOff, p.danger, locales.SyncCouldNot(), sub}
	case si.conflict:
		return syncLook{iconCloudAlert, p.modified, locales.SyncConflictTitle(), synced}
	case si.syncedAt.IsZero():
		return syncLook{iconCloud, p.statusFg, locales.SyncNotYet(), synced}
	}
	return syncLook{iconCloudCheck, p.added, locales.SyncUpToDateTitle(), synced}
}

// countdown is how long d is, as a clock counting down: "4:05".
func countdown(d time.Duration) string {
	s := max(int((d+time.Second-1)/time.Second), 0)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// countdownTick repaints the window at the next second, for a countdown it shows.
func (a *app) countdownTick() {
	now := time.Now()
	if a.tickAt.After(now) {
		return
	}
	a.tickAt = now.Truncate(time.Second).Add(time.Second)
	time.AfterFunc(time.Until(a.tickAt)+15*time.Millisecond, a.invalidate)
}

// syncStatus is settings sync's part of the status bar, while signed in:
// a cloud that tells how the last sync went, and opens the account's
// panel.
func (a *app) syncStatus(c *ui.Context) {
	si := a.host.syncInfo()
	if si.name == "" {
		a.syncOpen = false
		return
	}
	p := a.pal
	look := a.syncLook(si, time.Now())
	tip := look.title + " · " + look.subtitle
	b := ui.ButtonBase(c.Key("status:sync")).Height(statusBarH - 6).Width(syncStatusW - 2).Radius(4).
		AlignItems(ui.Center).Justify(ui.Center).Label(locales.SyncPanelLabel(si.name))
	if !a.syncOpen {
		b.Tooltip(tip)
	}
	if b.Hovered() || a.syncOpen {
		b.Background(p.hover)
	}
	b.Transition(hoverFade)
	b.Children(func() {
		if si.busy {
			ui.Spinner(c).Size(11, 11)
			return
		}
		color := look.color
		if color == p.added {
			// Synced is how it ought to be: it stays quiet.
			color = p.statusFg
		}
		ui.Icon(c, look.icon).FontSize(12.5).TextColor(color)
	})
	if b.Clicked() {
		a.syncOpen = !a.syncOpen
	}
	ui.PopoverBase(c, b, &a.syncOpen, func(panel ui.Element) { a.syncPanel(c, b, panel, si) })
}

// syncPanel is the account's panel, above its part of the status bar:
// who settings sync syncs for, how it went, what it syncs, and the
// account's commands.
func (a *app) syncPanel(c *ui.Context, anchor, panel ui.Element, si syncInfo) {
	p := a.pal
	now := time.Now()
	look := a.syncLook(si, now)
	if !si.retryAt.IsZero() && si.failure != "" && !si.busy {
		a.countdownTick()
	}
	panel.AttachTo(anchor, ui.AnchorTopRight, ui.AnchorBottomRight).Margin(0, 0, 6, 0)
	// It rises from the status bar, which it opens above.
	dropIn(a.floating(c, panel.Width(304).Radius(10).Gap(0), false), -4).Role(ui.RoleDialog).Label(locales.SyncPanelLabel(si.name))
	divider := func() { ui.Row(c).Height(1).Background(p.border) }
	close := func(fn func()) {
		a.syncOpen = false
		a.post(fn)
	}
	panel.Children(func() {
		// Who.
		ui.Row(c).Padding(14, 14, 12, 14).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Avatar(c, si.name, a.host.syncPicture()).FontSize(17).Shrink(0)
			ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Text(c, si.name).FontSize(13).FontWeight(600).TextColor(p.text).SingleLine()
				sub := cmp.Or(si.email, monHost())
				ui.Text(c, sub).FontSize(11.5).TextColor(p.muted).SingleLine()
			})
		})
		divider()
		// How it went.
		ui.Row(c).Padding(10, 10, 10, 14).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Width(28).Height(28).Radius(7).Shrink(0).AlignItems(ui.Center).Justify(ui.Center).
				Background(look.color.Alpha(0.14)).Children(func() {
				if si.busy {
					ui.Spinner(c).Size(13, 13)
				} else {
					ui.Icon(c, look.icon).FontSize(15).TextColor(look.color)
				}
			})
			ui.Column(c).Gap(2).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Text(c, look.title).FontSize(12.5).FontWeight(600).TextColor(p.text).SingleLine()
				ui.Text(c, look.subtitle).FontSize(11.5).TextColor(p.muted)
			})
			sync := ui.ButtonBase(c.Key("sync:now")).Width(28).Height(28).Radius(6).Shrink(0).
				AlignItems(ui.Center).Justify(ui.Center).Label(locales.CmdAccountSyncNow()).Tooltip(a.withKey(locales.CmdAccountSyncNow(), "account.syncNow")).
				Disabled(si.busy)
			if sync.Hovered() && !si.busy {
				sync.Background(p.hover)
			}
			sync.Transition(hoverFade)
			pressPose(sync)
			sync.Children(func() {
				col := p.muted
				if si.busy {
					col = p.faint
				}
				ui.Icon(c, iconRefresh).FontSize(14).TextColor(col)
			})
			if sync.Clicked() {
				a.host.syncNow()
			}
		})
		if si.conflict {
			ui.Column(c).Margin(0, 10, 10, 10).Padding(10, 12).Gap(8).Radius(8).
				Background(p.modified.Alpha(0.12)).BorderWidth(1, 1, 1, 1).BorderColor(p.modified.Alpha(0.30)).Children(func() {
				ui.Text(c, locales.SyncConflictMessage()).FontSize(11.5).TextColor(p.text)
				ui.Row(c).Gap(6).Justify(ui.End).Children(func() {
					if ui.Button(c, locales.SyncKeepMine()).FontSize(11.5).Padding(4, 10).Clicked() {
						close(a.keepMyKeybindings)
					}
					if ui.PrimaryButton(c, locales.SyncCompare()).FontSize(11.5).Padding(4, 10).Clicked() {
						close(a.compareKeybindings)
					}
				})
			})
		}
		divider()
		// What syncs.
		ui.Column(c).Padding(10, 14, 6, 14).Gap(2).Children(func() {
			ui.Text(c, strings.ToUpper(locales.SyncWhatSyncs())).FontSize(10.5).FontWeight(600).LetterSpacing(0.7).
				TextColor(p.faint).Padding(0, 0, 4, 0)
			keys, keybindings := a.settings.SyncKept()
			allKept := slices.Contains(a.settings.SyncIgnored, settings.SyncSettings)
			detail := "settings.json"
			if n := len(keys); n > 0 && !allKept {
				detail = locales.SyncKeptHere(n)
			}
			a.syncRow(c, "settings", iconSettings, locales.SyncSettings(), detail, !allKept, func(on bool) {
				a.settings.SyncIgnored = a.settings.SyncIgnoring(settings.SyncSettings, !on)
				a.settings.Changed()
			})
			a.syncRow(c, "keybindings", iconKeyboard, locales.SyncShortcuts(), "keybindings.json", !keybindings, func(on bool) {
				a.settings.SyncIgnored = a.settings.SyncIgnoring(settings.SyncKeybindings, !on)
				a.settings.Changed()
			})
			hint := ui.ButtonBase(c.Key("sync:hint")).Padding(4, 0).Radius(4).Label(locales.SyncKeepHint())
			hint.Children(func() {
				col := p.faint
				if hint.Hovered() {
					col = p.accent
				}
				ui.Text(c, locales.SyncKeepHint()).FontSize(11).TextColor(col)
			})
			if hint.Clicked() {
				close(func() { a.host.showSettings(settings.CatGeneral, "syncIgnored") })
			}
		})
		divider()
		// The account.
		ui.Row(c).Padding(6, 6).Gap(2).Children(func() {
			a.syncAction(c, "manage", iconOpenFile, locales.SyncManage(), p.muted, func() {
				a.services.OpenURL(monURL())
			})
			ui.Spacer(c)
			a.syncAction(c, "signout", iconLogOut, locales.SyncSignOut(), p.muted, a.confirmSignOut)
		})
	})
}

// syncRow is a row of what syncs in the account's panel, whose switch,
// or the row itself, calls toggle.
func (a *app) syncRow(c *ui.Context, key string, icon *ui.SVG, label, detail string, on bool, toggle func(bool)) {
	p := a.pal
	row := ui.ButtonBase(c.Key("sync:row:"+key)).Padding(5, 6).Margin(0, -6).Gap(10).Radius(6).AlignItems(ui.Center).
		Label(label)
	if row.Hovered() {
		row.Background(p.hover)
	}
	row.Transition(hoverFade)
	was := on
	row.Children(func() {
		ui.Icon(c, icon).FontSize(14).TextColor(p.muted).Shrink(0)
		ui.Column(c).Gap(1).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Text(c, label).FontSize(12.5).TextColor(p.text).SingleLine()
			ui.Text(c, detail).FontSize(11).TextColor(p.faint).SingleLine()
		})
		ui.Switch(c.Key("sync:switch:"+key), &on).Label(label)
	})
	if row.Clicked() && on == was {
		on = !on
	}
	if on != was {
		toggle(on)
	}
}

// syncAction is a button of the account's panel's foot.
func (a *app) syncAction(c *ui.Context, key string, icon *ui.SVG, label string, col ui.Color, fn func()) {
	p := a.pal
	b := ui.ButtonBase(c.Key("sync:"+key)).Height(28).Padding(0, 8).Gap(6).Radius(6).AlignItems(ui.Center).Label(label)
	if b.Hovered() {
		b.Background(p.hover)
		col = p.text
	}
	b.Transition(hoverFade)
	b.Children(func() {
		ui.Icon(c, icon).FontSize(13).TextColor(col)
		ui.Text(c, label).FontSize(12).TextColor(col).SingleLine()
	})
	if b.Clicked() {
		a.syncOpen = false
		a.post(fn)
	}
}

// showSync opens the account's panel, or signs in to sync.
func (a *app) showSync() {
	if a.host.account() == "" {
		a.host.signIn()
		return
	}
	a.syncOpen = true
}

// confirmSignOut asks before signing out of settings sync.
func (a *app) confirmSignOut() {
	if a.host.account() == "" {
		return
	}
	a.confirm = &confirm{
		title: locales.SyncSignOutTitle(a.host.account()), message: locales.SyncSignOutMessage(),
		ok: locales.SyncSignOut(), run: func() { a.host.signOut() },
	}
}

// compareKeybindings shows this machine's keybindings.json and the one
// settings sync kept beside it, side by side, to merge by hand.
func (a *app) compareKeybindings() {
	path := keymap.Path(a.settings.Path())
	if path == "" {
		return
	}
	other := filepath.Join(filepath.Dir(path), conflictFile)
	if _, err := os.Stat(other); err != nil {
		a.tell(note{message: locales.SyncNoConflict()})
		a.host.syncConflictResolved()
		return
	}
	a.openKeybindings()
	col := a.column()
	var next *column
	if i := slices.Index(a.cols, col); i+1 < len(a.cols) {
		next = a.cols[i+1]
	} else {
		next = a.splitColumn(col, true)
	}
	a.openFileIn(next, next.active+1, other, true)
}

// keepMyKeybindings drops the account's keybindings.json that settings
// sync kept, keeping this machine's.
func (a *app) keepMyKeybindings() {
	path := keymap.Path(a.settings.Path())
	if path == "" {
		return
	}
	other := filepath.Join(filepath.Dir(path), conflictFile)
	if err := os.Remove(other); err != nil && !os.IsNotExist(err) {
		a.fail("%s", err.Error())
	}
	a.host.syncConflictResolved()
}

// signin is the dialog of a sign-in to settings sync: the code to approve
// on Mon, "" until Mon gives it, where to approve it and until when, and
// what cancels the sign-in.
type signin struct {
	code, url string
	expires   time.Time
	cancel    func()
}

func (a *app) signinView(c *ui.Context) {
	si := a.signin
	if si == nil {
		return
	}
	p := a.pal
	cancel := func() {
		a.signin = nil
		si.cancel()
	}
	host := monHost()
	if u, err := url.Parse(si.url); err == nil && u.Host != "" {
		host = u.Host
	}
	if !si.expires.IsZero() {
		a.countdownTick()
	}
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "signin")
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(440).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Padding(20, 20, 16, 20).Gap(16).Radius(12), true).
				Role(ui.RoleDialog).Label(locales.SyncSignInTitle())
			box.Children(func() {
				// What it is.
				ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Width(40).Height(40).Radius(10).Shrink(0).AlignItems(ui.Center).Justify(ui.Center).
						Background(p.accent.Alpha(0.14)).Children(func() {
						ui.Icon(c, iconCloud).FontSize(20).TextColor(p.accent)
					})
					ui.Column(c).Gap(3).Grow(1).Shrink(1).MinWidth(0).Children(func() {
						ui.Text(c, locales.SyncSignInTitle()).FontSize(15).FontWeight(600).TextColor(p.text)
						ui.Text(c, locales.SyncSignInLead()).FontSize(12).TextColor(p.muted)
					})
				})
				// How.
				ui.Column(c).Padding(14, 16).Gap(12).Radius(10).Background(p.hover).BorderWidth(1, 1, 1, 1).BorderColor(p.border).Children(func() {
					a.signinStep(c, 1, locales.SyncStepSignIn(host))
					a.signinStep(c, 2, locales.SyncStepCheck())
					ui.Row(c.Key("signin:code")).Justify(ui.Center).Padding(2, 0, 4, 0).Gap(5).AlignItems(ui.Center).MinHeight(40).Children(func() {
						if si.code == "" {
							ui.Spinner(c).Size(14, 14)
							ui.Text(c, locales.SyncGettingCode()).FontSize(12).TextColor(p.muted)
							return
						}
						a.codeTiles(c, si.code)
					})
					a.signinStep(c, 3, locales.SyncStepApprove())
				})
				// How long the code lasts.
				if si.code != "" {
					ui.Row(c).Gap(8).Margin(-6, 0, 0, 2).AlignItems(ui.Center).Children(func() {
						ui.Spinner(c).Size(11, 11)
						wait := locales.SyncWaiting()
						if !si.expires.IsZero() {
							wait = locales.SyncWaitingExpires(countdown(time.Until(si.expires)))
						}
						ui.Text(c, wait).FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(1)
					})
				}
				ui.Row(c).Gap(8).Justify(ui.End).AlignItems(ui.Center).Children(func() {
					if ui.Button(c, locales.ButtonCancel()).Clicked() {
						cancel()
					}
					if ui.Button(c, locales.SyncCopyCode()).Disabled(si.code == "").Clicked() {
						a.services.WriteClipboard(si.code)
						a.tell(note{message: locales.SyncCodeCopied()})
					}
					if ui.PrimaryButton(c, locales.SyncOpenBrowser()).Disabled(si.url == "").AutoFocus().Clicked() {
						a.services.OpenURL(si.url)
					}
				})
			})
			if back.OverlayShortcut(0, ui.KeyEscape) {
				cancel()
			}
		})
	})
}

// signinStep is a numbered step of the sign-in.
func (a *app) signinStep(c *ui.Context, n int, text string) {
	p := a.pal
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Width(20).Height(20).Radius(10).Shrink(0).AlignItems(ui.Center).Justify(ui.Center).
			Background(p.accent.Alpha(0.16)).Children(func() {
			ui.Text(c, fmt.Sprint(n)).FontSize(11).FontWeight(600).TextColor(p.accent)
		})
		ui.Text(c, text).FontSize(12.5).TextColor(p.text).Grow(1).Shrink(1)
	})
}

// codeTiles shows a sign-in code a character to a tile, as "ABCD-EFGH",
// which one can select and copy whole.
func (a *app) codeTiles(c *ui.Context, code string) {
	p := a.pal
	for i, r := range code {
		if r == '-' {
			ui.Row(c.Key(i)).Width(10).Height(2).Radius(1).Background(p.faint).Margin(0, 3)
			continue
		}
		ui.Row(c.Key(i)).Width(30).Height(38).Radius(7).AlignItems(ui.Center).Justify(ui.Center).
			Background(c.Theme().Surface).BorderWidth(1, 1, 1, 1).BorderColor(p.border).Children(func() {
			ui.Text(c, string(r)).Font(a.font.family).FontSize(19).FontWeight(600).TextColor(p.text)
		})
	}
}
