package main

import (
	"os"
	"os/exec"
	"strings"

	"mayoi/internal/bg"
	"mayoi/locales"
)

// Install hc… runs herdr's own installer, which puts hc in hcLocalBin,
// or updates the hc there, keeping its terminals running. hcPath finds
// it there, so new terminals run in herdr-core without a restart. With
// terminalBackend "hc" and no hc, the first terminal offers it.

// hcInstaller is herdr's installer of hc for macOS and Linux, which
// checks what it downloads against the checksum of the release.
const hcInstaller = "https://releases.herdr.dev/core/install.sh"

// hcInstallable reports whether herdr has builds of hc for this system:
// macOS and Linux, on the machines Mayoi is built for.
func hcInstallable() bool { return hostOS == "darwin" || hostOS == "linux" }

// hcInstall runs herdr's installer and returns what it printed. Tests
// replace it.
var hcInstall = func() ([]byte, error) {
	// Downloaded first, so that a failed download fails rather than run
	// sh on nothing.
	const script = `s=$(curl -fsSL "$1") || { echo "hc: cannot download $1" >&2; exit 1; }; printf '%s\n' "$s" | sh`
	cmd := exec.Command("/bin/sh", "-c", script, "sh", hcInstaller)
	cmd.Env = append(os.Environ(), "HC_INSTALL_DIR="+hcLocalBin())
	return cmd.CombinedOutput()
}

// hcInstallError is why the installer failed: the last line it printed,
// as "hc: needs curl", or err when it printed none.
func hcInstallError(out []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		return strings.TrimPrefix(last, "hc: ")
	}
	return err.Error()
}

// hcInstalling is set while hc installs; hcOffered once a terminal
// offered to install it, which it does once as Mayoi runs. The main
// thread's.
var hcInstalling, hcOffered bool

// installHC installs hc in the background, and says how it went.
func (a *app) installHC() {
	if hcInstalling || !hcInstallable() {
		return
	}
	hcInstalling = true
	a.tell(note{message: locales.HcInstalling()})
	bg.Go(func() {
		out, err := hcInstall()
		a.post(func() {
			hcInstalling = false
			switch {
			case err != nil:
				a.fail("%s", locales.FailInstallHC(hcInstallError(out, err)))
			case a.settings.TerminalBackend != "hc":
				a.tell(note{message: locales.HcInstalledSetBackend(shortenHome(hcLocalBin()))})
			default:
				a.tell(note{message: locales.HcInstalled(shortenHome(hcLocalBin()))})
			}
		})
	})
}

// offerHC asks, once as Mayoi runs, whether to install hc, which
// terminalBackend "hc" needs: the terminal runs in Mayoi meanwhile.
func (a *app) offerHC() {
	if a.settings.TerminalBackend != "hc" || hcOffered || hcInstalling || a.confirm != nil || !hcInstallable() || hcPath() != "" {
		return
	}
	hcOffered = true
	a.confirm = &confirm{
		title: locales.HcInstallTitle(), message: locales.HcInstallDetail(), ok: locales.HcInstall(),
		run: a.installHC,
	}
}
