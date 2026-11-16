package remote

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestDial(t *testing.T) {
	args := poolSSHArgs("me@box:2222")
	if containsControl(args) {
		t.Fatalf("windows ssh uses a control socket: %q", args)
	}
	text := strings.Join(args, "\n")
	if !strings.Contains(text, "-p\n2222\nme@box\n") && !strings.Contains(text, "-p\n2222\nme@box") {
		t.Fatalf("host args: %q", args)
	}
	if !strings.Contains(text, "BatchMode=yes") {
		t.Fatalf("batch mode: %q", args)
	}
	ssh := hiddenCommand("ssh", args...)
	wsl := hiddenCommand("wsl.exe", "-d", "Ubuntu", "-e", "/bin/true")
	if !consoleHidden(ssh) || !consoleHidden(wsl) {
		t.Fatal("ssh or wsl would open a console window")
	}
	if runtime.GOOS == "windows" && (len(ssh.Args) < 2 || ssh.Args[0] != "ssh") {
		t.Fatalf("command %q", ssh.Args)
	}
}

func TestControl(t *testing.T) {
	unix := masterCommandArgs("sock", "box")
	if !contains(unix, "ControlMaster=yes") || !contains(unix, "ControlPath=sock") {
		t.Fatalf("unix master: %q", unix)
	}
	win := poolSSHArgs("box")
	if containsControl(win) {
		t.Fatalf("windows ssh uses a control socket: %q", win)
	}
	if !contains(win, "box") {
		t.Fatalf("host: %q", win)
	}
}

func contains(args []string, want string) bool {
	for _, a := range args {
		if a == want || strings.Contains(a, want) {
			return true
		}
	}
	return false
}

func TestTerminalCommand(t *testing.T) {
	c := &Conn{Host: "me@box:2222"}
	t.Cleanup(func() {
		if c.dir != "" {
			os.RemoveAll(c.dir)
		}
	})
	args := c.TerminalCommand("/home/me/app")
	if len(args) == 0 || args[0] != "ssh" {
		t.Fatalf("command %q", args)
	}
	text := strings.Join(args, "\n")
	if !contains(args, "-t") || !strings.Contains(text, "/home/me/app") {
		t.Fatalf("terminal: %q", args)
	}
	if !strings.Contains(text, "-p\n2222\nme@box") {
		t.Fatalf("host args: %q", args)
	}
	if contains(args, "BatchMode=yes") {
		t.Fatalf("a terminal cannot ask for a password: %q", args)
	}
	if runtime.GOOS == "windows" {
		if containsControl(args) {
			t.Fatalf("windows terminal uses a control socket: %q", args)
		}
		return
	}
	if !contains(args, "ControlPath=") {
		t.Fatalf("unix terminal: %q", args)
	}
}

func TestSSHError(t *testing.T) {
	key := sshError("Warning: permanently added\nHost key verification failed.", errors.New("exit status 255"))
	if key.Error() != "Host key verification failed. Connect once with ssh in a terminal to trust the host's key." {
		t.Fatalf("host key: %v", key)
	}
	if !Refused(key) {
		t.Fatal("an untrusted host key would be tried again")
	}
	denied := sshError("Permission denied (publickey).", errors.New("exit"))
	if denied.Error() != "Permission denied (publickey). Mayoi signs in with keys and the SSH agent, not passwords." {
		t.Fatalf("permission: %v", denied)
	}
	missing := sshStartError(exec.ErrNotFound)
	if !strings.Contains(missing.Error(), "could not run ssh") {
		t.Fatalf("missing ssh: %v", missing)
	}
	_, err := parseInfo([]byte("not a shell"))
	if err == nil || err.Error() != "the host did not answer as a POSIX shell does" {
		t.Fatalf("non-posix: %v", err)
	}
}

func containsControl(args []string) bool {
	for _, a := range args {
		if strings.Contains(a, "ControlMaster") || strings.Contains(a, "ControlPath") {
			return true
		}
	}
	return false
}
