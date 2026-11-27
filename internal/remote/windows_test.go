package remote

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeWindowsSSH is an ssh to a Windows host, whose shell is cmd.exe: it
// runs powershell, which is pwsh here, and nothing else.
const fakeWindowsSSH = `#!/bin/sh
sock= master= last=
for a; do
  case $a in
    ControlPath=*) sock=${a#ControlPath=} ;;
    -N) master=1 ;;
  esac
  last=$a
done
if [ -n "$master" ]; then
  trap 'rm -f "$sock"; exit 0' INT TERM
  : > "$sock"
  while :; do sleep 0.05; done
fi
case $last in
  "powershell "*) exec sh -c "$last" ;;
esac
printf "'sh' is not recognized as an internal or external command,\r\noperable program or batch file.\r\n" >&2
exit 1
`

// windowsHost puts the fake ssh of a Windows host first on the PATH, with
// pwsh as its powershell.
func windowsHost(t *testing.T) {
	t.Helper()
	needsShell(t)
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("no pwsh to stand in for Windows PowerShell")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(fakeWindowsSSH), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(pwsh, filepath.Join(bin, "powershell")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func connect(t *testing.T, c *Conn) {
	t.Helper()
	changed := make(chan struct{}, 8)
	stop := c.Subscribe(func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	defer stop()
	c.Connect()
	for s, err := c.State(); s != Connected; s, err = c.State() {
		if s == Offline {
			t.Fatalf("could not connect: %v", err)
		}
		select {
		case <-changed:
		case <-time.After(60 * time.Second):
			t.Fatal("timed out connecting")
		}
	}
}

// TestFakeWindowsHost works with the files of a host that has no sh, by
// PowerShell's scripts.
func TestFakeWindowsHost(t *testing.T) {
	windowsHost(t)
	c := For("windows.invalid")
	defer Shutdown()
	connect(t, c)
	info := c.Info()
	if !info.Windows || !c.Windows() || info.Home == "" {
		t.Fatalf("info = %+v", info)
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name := dir + "/it's a ‘file’.txt"
	data := []byte("one\x00two\r\nthree 'quoted' $HOME\n")
	fi, err := c.WriteFile(name, data)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != int64(len(data)) || fi.ModTime().IsZero() || fi.Stamp() == "" {
		t.Fatalf("written = %+v", fi)
	}
	got, err := c.ReadFile(name, 0)
	if err != nil || string(got) != string(data) {
		t.Fatalf("read %q, %v", got, err)
	}
	if _, err := c.ReadFile(name, 4); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("limit: %v", err)
	}
	if _, err := c.ReadFile(dir+"/nope", 0); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file: %v", err)
	}
	if fi2, got, err := c.ReadFileInfo(name, 0); err != nil || string(got) != string(data) || fi2.Stamp() != fi.Stamp() {
		t.Fatalf("ReadFileInfo = %+v, %q, %v", fi2, got, err)
	}
	if _, _, err := c.ReadFileInfo(dir+"/nope", 0); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadFileInfo of a missing file: %v", err)
	}
	if _, err := c.Stat(dir + "/nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stat of a missing file: %v", err)
	}
	// A file changed since it was read is not written over.
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(name, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.WriteFileIf(name, []byte("mine"), fi.Stamp()); !errors.Is(err, ErrChanged) {
		t.Fatalf("WriteFileIf over a change: %v", err)
	}
	if fi, err = c.WriteFile(name, []byte("again")); err != nil || fi.Size() != 5 {
		t.Fatalf("written again = %+v, %v", fi, err)
	}
	if err := c.MkdirAll(dir + "/sub/deeper"); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(dir + "/sub/.hidden"); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(dir + "/sub/.hidden"); err == nil {
		t.Fatal("created a file that is there")
	}
	if err := os.Symlink("sub", dir+"/link"); err != nil {
		t.Fatal(err)
	}
	entries, err := c.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(entries, func(a, b DirEntry) int { return strings.Compare(a.Name, b.Name) })
	if want := []DirEntry{{"it's a ‘file’.txt", false, false}, {"link", true, true}, {"sub", true, false}}; !slices.Equal(entries, want) {
		t.Fatalf("entries = %+v", entries)
	}
	lists, err := c.ReadDirs([]string{dir + "/sub", dir + "/gone"})
	if err != nil {
		t.Fatal(err)
	}
	if got := lists[dir+"/sub"].Entries; len(got) != 2 {
		t.Fatalf("sub = %+v", got)
	}
	if !errors.Is(lists[dir+"/gone"].Err, fs.ErrNotExist) {
		t.Fatalf("gone = %+v", lists[dir+"/gone"])
	}
	infos, err := c.StatMany([]string{name, dir + "/sub", dir + "/gone"})
	if err != nil || len(infos) != 2 || !infos[dir+"/sub"].IsDir() {
		t.Fatalf("StatMany = %+v, %v", infos, err)
	}
	lists, files, err := c.Look([]string{dir, dir + "/gone"}, []string{name, dir + "/gone"})
	if err != nil || len(lists[dir].Entries) != 3 || !errors.Is(lists[dir+"/gone"].Err, fs.ErrNotExist) || len(files) != 1 || files[name] == nil {
		t.Fatalf("Look = %+v, %+v, %v", lists, files, err)
	}
	under, err := c.StatUnder([]string{name}, dir+"/sub")
	if err != nil || len(under) != 3 || under[dir+"/sub/deeper"] == nil {
		t.Fatalf("StatUnder = %+v, %v", under, err)
	}
	if err := c.Rename(name, dir+"/sub/.hidden"); err == nil {
		t.Fatal("renamed over a file")
	}
	if err := c.Rename(name, dir+"/moved.txt"); err != nil {
		t.Fatal(err)
	}
	read := map[string]string{}
	err = c.ReadFiles(dir, []string{"moved.txt", "sub/.hidden", "gone", "sub"}, 1<<20, func(rel string, data []byte) {
		read[rel] = string(data)
	})
	if err != nil || len(read) != 2 || read["moved.txt"] != "again" {
		t.Fatalf("ReadFiles = %q, %v", read, err)
	}

	// A program runs in the folder, with its arguments as they are and
	// the environment given, its input and output as they are.
	cmd, err := c.Exec(dir, []string{"MAYOI_X=it's"}, []string{"HOME"}, strings.NewReader("in\x00put"), "sh",
		"-c", `printf '%s|%s|%s|' "$1" "$MAYOI_X" "${HOME-unset}"; pwd; cat; exit 4`, "sh", `a "b" \c`)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 4 || out.String() != `a "b" \c|it's|unset|`+dir+"\nin\x00put" {
		t.Fatalf("Exec = %q, %v", out.String(), err)
	}

	// Files go up, are copied on the host, and come down.
	local := t.TempDir()
	os.MkdirAll(filepath.Join(local, "tree", "inner"), 0o755)
	os.WriteFile(filepath.Join(local, "tree", "inner", "f.bin"), bytes.Repeat([]byte{0, 1, 2}, 50000), 0o644)
	os.WriteFile(filepath.Join(local, "moved.txt"), []byte("up"), 0o644)
	p := NewProgress()
	placed, err := c.Upload([]string{filepath.Join(local, "tree"), filepath.Join(local, "moved.txt")}, dir, p)
	if err != nil || !slices.Equal(placed, []string{dir + "/tree", dir + "/moved copy.txt"}) {
		t.Fatalf("Upload = %q, %v", placed, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "tree", "inner", "f.bin")); len(b) != 150000 {
		t.Fatalf("uploaded %d bytes", len(b))
	}
	tmp, err := c.Upload([]string{filepath.Join(local, "moved.txt")}, "", nil)
	if err != nil || len(tmp) != 1 || !strings.HasSuffix(tmp[0], "/moved.txt") {
		t.Fatalf("Upload to a temporary folder = %q, %v", tmp, err)
	}
	c.Remove(filepath.Dir(tmp[0]))
	copied, err := c.CopyOnHost([]string{dir + "/tree"}, dir, nil)
	if err != nil || !slices.Equal(copied, []string{dir + "/tree copy"}) {
		t.Fatalf("CopyOnHost = %q, %v", copied, err)
	}
	down := filepath.Join(local, "down")
	if err := c.Download(dir+"/tree copy", down, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(down, "inner", "f.bin")); len(b) != 150000 {
		t.Fatalf("downloaded %d bytes", len(b))
	}
	if err := c.Remove(dir + "/tree"); err != nil {
		t.Fatal(err)
	}
	// Removing a link to a folder leaves what is in the folder.
	if err := c.Remove(dir + "/link"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", ".hidden")); err != nil {
		t.Fatalf("removed through a link: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "tree")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("tree is still there: %v", err)
	}
	os.MkdirAll(filepath.Join(dir, "node_modules", "x"), 0o755)
	os.WriteFile(filepath.Join(dir, "node_modules", "x", "skip.js"), nil, 0o644)
	walked, err := c.Walk(dir, []string{"node_modules"}, 100)
	slices.Sort(walked)
	if err != nil || !slices.Equal(walked, []string{"moved copy.txt", "moved.txt", "sub/.hidden", "tree copy/inner/f.bin"}) {
		t.Fatalf("Walk = %q, %v", walked, err)
	}
	find, err := c.FindProgram(nil, nil, "sh")
	if err != nil {
		t.Fatal(err)
	}
	if b, err := find.Output(); err != nil || strings.TrimSpace(string(b)) != "mayoi:found" {
		t.Fatalf("FindProgram = %q, %v", b, err)
	}
}

// TestFakeWindowsPool runs commands through workers of a Windows host,
// as a Windows client does: PowerShell answers frames.
func TestFakeWindowsPool(t *testing.T) {
	windowsHost(t)
	p := newPool(func() (workerPipe, error) { return startSSHPS("windows.invalid", nil) }, func(error) {})
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for i := range 2 {
		var out, errb bytes.Buffer
		code, err := p.Call(ctx, psScript(psProbe, nil), nil, &out, &errb)
		if err != nil || code != 0 || !strings.Contains(out.String(), "\nWindows\n") {
			t.Fatalf("probe %d = %d %q %q %v", i, code, out.String(), errb.String(), err)
		}
	}
	// Input and output larger than a pipe holds, both at once.
	big := bytes.Repeat([]byte("0123456789\x00"), 40000)
	var out, errb bytes.Buffer
	code, err := p.Call(ctx, psScript(`$MI.CopyTo($MO);Fail 'done' 5`, nil), bytes.NewReader(big), &out, &errb)
	if err != nil || code != 5 || !bytes.Equal(out.Bytes(), big) || errb.String() != "done\n" {
		t.Fatalf("echo = %d, %d bytes, %q, %v", code, out.Len(), errb.String(), err)
	}
}

func TestWindowsPaths(t *testing.T) {
	for in, want := range map[string]string{
		"/C:/Users/me": `C:\Users\me`,
		"/C:":          `C:\`,
		"/home/me":     "/home/me",
	} {
		if got := WindowsPath(in); got != want {
			t.Errorf("WindowsPath(%q) = %q", in, got)
		}
	}
	for in, want := range map[string]string{
		`C:\Users\me\repo\.git`: "/ssh:h/C:/Users/me/repo/.git",
		"c:/Users/me":           "/ssh:h/C:/Users/me",
		"/c:/Users/me":          "/ssh:h/C:/Users/me",
		"/home/me":              "/ssh:h/home/me",
		"relative":              "relative",
	} {
		if got := On("/ssh:h/x", in); got != want {
			t.Errorf("On(%q) = %q", in, got)
		}
	}
	if !HostAbs(`D:\x`) || !HostAbs("/x") || HostAbs("x/y") {
		t.Error("HostAbs")
	}
	for in, want := range map[string]string{
		"":             `""`,
		"plain":        "plain",
		"a b":          `"a b"`,
		`say "hi"`:     `"say \"hi\""`,
		`C:\dir with\`: `"C:\dir with\\"`,
	} {
		if got := winArg(in); got != want {
			t.Errorf("winArg(%q) = %s", in, got)
		}
	}
	args := psExecArgs("/C:/r", []string{"A=1"}, []string{"B"}, "git", []string{"-C", "/C:/r/sub", "a b"})
	if !slices.Equal(args, []string{"/C:/r", "git", `-C C:/r/sub "a b"`, "1", "B", "A=1"}) {
		t.Errorf("psExecArgs = %q", args)
	}
}

// TestNoShell: a host that signs in but runs neither sh nor PowerShell
// says so, and is not tried again by itself.
func TestNoShell(t *testing.T) {
	needsShell(t)
	bin := t.TempDir()
	noShell := strings.Replace(fakeWindowsSSH, `"powershell "*) exec sh -c "$last" ;;`, `nothing) ;;`, 1)
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(noShell), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	c := For("noshell.invalid")
	defer Shutdown()
	changed := make(chan struct{}, 8)
	defer c.Subscribe(func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})()
	c.Connect()
	for {
		s, err := c.State()
		if s == Offline {
			if !Refused(err) || !strings.Contains(err.Error(), "neither sh nor PowerShell") || !strings.Contains(err.Error(), "batch file") {
				t.Fatalf("error = %v, refused %v", err, Refused(err))
			}
			return
		}
		if s == Connected {
			t.Fatal("connected to a host without a shell")
		}
		select {
		case <-changed:
		case <-time.After(30 * time.Second):
			t.Fatal("timed out")
		}
	}
}
