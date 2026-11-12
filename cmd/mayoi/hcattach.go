package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"mayoi/internal/bg"
	"mayoi/internal/remote"
	"mayoi/locales"
)

// View → Attach to Terminal in herdr-core… shows the terminals of
// herdr-core of the window's machine, and shows the one chosen in a
// terminal of this window. That terminal runs hc attach here, not a shell of
// herdr-core: closing it, or Ctrl+\, ends the attach alone, and the program
// runs on, as it does when the Herdr app closes.

// hcLiveTerm is what hc ls says of a terminal of herdr-core that runs.
type hcLiveTerm struct {
	Address string
	Agent   string
	Cwd     string
	Where   string
	Status  string
}

// hcWindowTerms returns the terminals of herdr-core that run on the machine
// of the window: those of this machine, or those of the host of a remote
// folder. A host hc doesn't know has none.
func hcWindowTerms(root string) ([]hcLiveTerm, error) {
	machine, known := "", true
	if c, _, isRemote := remote.Lookup(root); isRemote {
		machine, known = hcMachine(c.Host)
	}
	if !known {
		return nil, nil
	}
	out, err := hcRun("ls", "--json")
	if err != nil {
		return nil, err
	}
	var l struct {
		Terminals []struct {
			Address, Agent, Cwd, State, Status, Where string
		}
	}
	if err := json.Unmarshal(out, &l); err != nil {
		return nil, err
	}
	var terms []hcLiveTerm
	for _, t := range l.Terminals {
		if t.State != "live" || !hcWindowAddr(t.Address, machine) {
			continue
		}
		terms = append(terms, hcLiveTerm{t.Address, t.Agent, t.Cwd, t.Where, t.Status})
	}
	slices.SortFunc(terms, func(x, y hcLiveTerm) int {
		return cmp.Or(
			cmp.Compare(hcTermFolder(x), hcTermFolder(y)),
			cmp.Compare(x.Address, y.Address),
		)
	})
	return terms, nil
}

// hcAddrArg is the address of a terminal of herdr-core from what a command
// gives: one of the window's machine, which a remote folder's names.
func hcAddrArg(root, arg string) (string, error) {
	addr := strings.TrimSpace(arg)
	switch {
	case addr == "":
		return "", errors.New("no address")
	case strings.Contains(addr, "/"):
		return addr, nil
	}
	if c, _, isRemote := remote.Lookup(root); isRemote {
		machine, known := hcMachine(c.Host)
		if !known {
			return "", fmt.Errorf("hc doesn't know %s", c.Host)
		}
		return machine + "/" + addr, nil
	}
	return addr, nil
}

// hcWindowAddr reports whether addr is the address of a terminal of
// machine: of this machine, which hc names in no address, or of the machine
// by its name.
func hcWindowAddr(addr, machine string) bool {
	if machine == "" {
		return !strings.Contains(addr, "/")
	}
	return strings.HasPrefix(addr, machine+"/")
}

// hcTermFolder is what tells a terminal of herdr-core from another: the
// space of the Herdr app it is in, or else the folder it runs in.
func hcTermFolder(t hcLiveTerm) string {
	if space, _, ok := strings.Cut(t.Where, " / "); ok && space != "" {
		return space
	}
	if t.Cwd != "" {
		return filepath.Base(t.Cwd)
	}
	return t.Address
}

// hcTermAbout is what a terminal of herdr-core is, in a few words: its
// agent, or its shell, and what it says it does.
func hcTermAbout(t hcLiveTerm) string {
	about := t.Agent
	if shells[t.Agent] {
		about = locales.HcTermShell()
	}
	if about == "" {
		about = locales.HcTermShell()
	}
	if t.Status != "" {
		about += " · " + t.Status
	}
	return about
}

// shells are the programs of a shell of hc's terminals, which a box names
// as one.
var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "fish": true, "nu": true, "dash": true}

// pickHCAttach shows the terminals of herdr-core of the window's machine in
// a box, and shows the one chosen in a terminal here.
func (a *app) pickHCAttach() {
	root := a.root
	bg.Go(func() {
		terms, err := hcWindowTerms(root)
		a.post(func() {
			switch {
			case err != nil:
				a.fail("%s", locales.FailListHC(err.Error()))
			case len(terms) == 0:
				a.tell(note{message: locales.HcTermsNone()})
			default:
				items := make([]pickItem, len(terms))
				for i, t := range terms {
					items[i] = pickItem{Label: hcTermFolder(t), Description: hcTermAbout(t), Detail: t.Address, Icon: "terminal"}
				}
				a.openPluginPicker(items, locales.HcTermsPick(), func(i int) {
					if i < 0 {
						return
					}
					a.attachHCTerminal(terms[i].Address, hcTermFolder(terms[i]))
				})
			}
		})
	})
}

// attachHCTerminal shows the terminal of herdr-core at addr in a tab of the
// column with the keyboard, called title: one there already showing it, which
// it shows rather than attaching a second time, or a terminal that runs hc
// attach on it. A plugin, or the mayoi command, asks for one by its address.
func (a *app) attachHCTerminal(addr, title string) {
	for _, s := range a.terms {
		if s.attach == addr {
			// One attach of a terminal of herdr-core, not two: the one here
			// shows again, and says so when it is already in front.
			inFront := a.termInFront(s)
			a.terminalInColumn(s, false)
			if inFront {
				a.tell(note{message: locales.HcAttached(addr)})
			}
			return
		}
	}
	s := &session{dir: a.workDir(), title: title, attach: addr}
	if a.addTerminal(s) == nil {
		return
	}
	a.terminalInColumn(s, true)
}

// termInFront reports whether s is the terminal in front of its window, which
// showing it again would not change.
func (a *app) termInFront(s *session) bool {
	if s.tab != nil {
		col, i := a.locate(s.tab)
		return col != nil && col == a.column() && i == col.active && a.focus == paneEditor
	}
	return a.panelTerm == s && a.window.PanelOpen && a.window.TerminalOpen && a.focus == paneTerminal
}
