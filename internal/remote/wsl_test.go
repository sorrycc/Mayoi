package remote

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWSLList(t *testing.T) {
	dir := t.TempDir()
	bin := "wsl"
	if runtime.GOOS == "windows" {
		bin = "wsl.exe"
	}
	src, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, bin)
	copyFile(t, src, dst)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MAYOI_FAKE_WSL", "1")
	t.Setenv("MAYOI_FAKE_WSL_NAMES", "Ubuntu\r\nDebian\r\nMy Distro\r\n")

	found, err := exec.LookPath("wsl")
	if err != nil || !strings.EqualFold(found, dst) {
		t.Fatalf("wsl resolved to %q, err %v", found, err)
	}

	got, err := Distros()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Ubuntu", "Debian", "My Distro"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("distros %q", got)
	}
	if CheckHost("My Distro") == nil {
		t.Fatal("CheckHost accepted a distribution name that contains a space")
	}
	args, err := distroArgs("My Distro")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 || args[0] != "-d" || args[1] != "My Distro" {
		t.Fatalf("args %q", args)
	}
	if _, err := distroArgs("Nope"); err == nil {
		t.Fatal("a name that is not installed was accepted")
	}
	if _, err := decodeWSLList([]byte{0xFF, 0xFE, 0x55}); err == nil {
		t.Fatal("an odd UTF-16 length was accepted")
	}

	t.Cleanup(func() {
		Shutdown()
		connsMu.Lock()
		delete(conns, connKey(KindSSH, "Ubuntu"))
		delete(conns, connKey(KindWSL, "Ubuntu"))
		connsMu.Unlock()
	})
	ssh := For("Ubuntu")
	again, _, ok := Lookup(Path("Ubuntu", "/home/me"))
	if !ok || again != ssh || ssh.Kind() != KindSSH {
		t.Fatalf("ssh lookup ok=%v kind=%v same=%v", ok, ssh.Kind(), again == ssh)
	}
	wsl, rpath, ok := Lookup(WSLPath("Ubuntu", "/home/me"))
	if !ok || rpath != "/home/me" || wsl == ssh || wsl.Kind() != KindWSL || wsl.Host != "Ubuntu" {
		t.Fatalf("wsl ok=%v path=%q kind=%v host=%q shared=%v", ok, rpath, wsl.Kind(), wsl.Host, wsl == ssh)
	}
	if againWSL, _, _ := Lookup(WSLPath("Ubuntu", "/tmp")); againWSL != wsl {
		t.Fatal("the same distribution returned another connection")
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
