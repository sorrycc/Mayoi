package remote

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPaths(t *testing.T) {
	p := Path("me@box:2222", "/home/me/proj/")
	if p != "/ssh:me@box:2222/home/me/proj" {
		t.Fatalf("Path = %q", p)
	}
	host, rpath, ok := Split(filepath.Join(p, "a", "..", "b.go"))
	if !ok || host != "me@box:2222" || rpath != "/home/me/proj/b.go" {
		t.Fatalf("Split = %q %q %v", host, rpath, ok)
	}
	if root := Path("box", "/"); root != "/ssh:box" || filepath.Base(root) != "ssh:box" {
		t.Fatalf("the root of a host is %q", root)
	}
	if _, rpath, _ := Split("/ssh:box"); rpath != "/" {
		t.Fatalf("the root splits to %q", rpath)
	}
	for _, local := range []string{"/Users/me/x", "/ssh:", "/ssh:/x", "ssh:box/x", ""} {
		if Is(local) {
			t.Errorf("%q is taken for remote", local)
		}
	}
	if got := filepath.ToSlash(filepath.Dir(p)); got != "/ssh:me@box:2222/home/me" {
		t.Errorf("Dir = %q", got)
	}
	if got := On(p, "/home/me/proj/.git"); got != "/ssh:me@box:2222/home/me/proj/.git" {
		t.Errorf("On = %q", got)
	}
	if got := On("/Users/me", "/x"); got != "/x" {
		t.Errorf("On of a local path = %q", got)
	}
	if got := Display(p); got != "me@box:2222:/home/me/proj" {
		t.Errorf("Display = %q", got)
	}
	if got := URL(p); got != "ssh://me@box:2222/home/me/proj" {
		t.Errorf("URL = %q", got)
	}
}

func TestParseURL(t *testing.T) {
	p, err := ParseURL("ssh://me@box:2222/srv/app")
	if err != nil || p != "/ssh:me@box:2222/srv/app" {
		t.Fatalf("ParseURL = %q, %v", p, err)
	}
	for _, bad := range []string{"http://box/x", "ssh:///x", "ssh://me:pw@box/x", "ssh://-oProxyCommand=x/y"} {
		if _, err := ParseURL(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestCheckHost(t *testing.T) {
	for _, ok := range []string{"box", "me@box", "box:22", "me@10.0.0.1:2200", "[::1]:22", "mayoi-test@orb"} {
		if err := CheckHost(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-oProxyCommand=sh", "a b", "box;rm", "box:0", "box:x", "a/b", "$(x)"} {
		if CheckHost(bad) == nil {
			t.Errorf("%q passed", bad)
		}
	}
	if got := hostArgs("me@box:2222"); !slices.Equal(got, []string{"-p", "2222", "me@box"}) {
		t.Errorf("hostArgs = %q", got)
	}
	if got := hostArgs("[::1]:22"); !slices.Equal(got, []string{"-p", "22", "::1"}) {
		t.Errorf("hostArgs of IPv6 = %q", got)
	}
	if target, port := SSHTarget("me@box"); target != "me@box" || port != "" {
		t.Errorf("SSHTarget = %q, %q", target, port)
	}
}

func TestQuote(t *testing.T) {
	if got := quote(`it's $HOME`); got != `'it'\''s $HOME'` {
		t.Fatalf("quote = %s", got)
	}
}

func TestConfigHosts(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "conf.d"), 0o755)
	os.WriteFile(filepath.Join(dir, "config"), []byte(`# mine
Include conf.d/*
Host web db # two
  HostName 10.0.0.2
Host *.internal !bad
Host = "quoted"
Match host x
`), 0o644)
	os.WriteFile(filepath.Join(dir, "conf.d", "a"), []byte("Host orb\n  User me\nHost web\n"), 0o644)
	var hosts []string
	seen := map[string]bool{}
	readConfig(filepath.Join(dir, "config"), dir, dir, 0, func(h string) {
		if !seen[h] {
			seen[h] = true
			hosts = append(hosts, h)
		}
	})
	if want := []string{"orb", "web", "db", "quoted"}; !slices.Equal(hosts, want) {
		t.Fatalf("hosts = %q, want %q", hosts, want)
	}
}

func TestParseStat(t *testing.T) {
	out := []byte("12 1700000000.250000000 81a4|/a/b c.txt\n40 1700000001 41ed|/a/d\nstat: cannot stat\n7 1700000002.5 a1ff|/a/l\n")
	infos := parseStat(out)
	f := infos["/a/b c.txt"]
	if f == nil || f.Size() != 12 || f.Name() != "b c.txt" || f.IsDir() || f.Mode().Perm() != 0o644 ||
		!f.ModTime().Equal(time.Unix(1700000000, 250000000)) {
		t.Fatalf("file = %+v", f)
	}
	if d := infos["/a/d"]; d == nil || !d.IsDir() || d.Mode().Perm() != 0o755 {
		t.Fatalf("dir = %+v", d)
	}
	if l := infos["/a/l"]; l == nil || l.Mode()&fs.ModeSymlink == 0 || l.ModTime().Nanosecond() != 500000000 {
		t.Fatalf("link = %+v", l)
	}
	if len(infos) != 3 {
		t.Fatalf("%d infos", len(infos))
	}
}

func TestParseDirs(t *testing.T) {
	out := []byte("/a\x00d sub\x00f x y.go\x00\x00/gone\x00!no such file or directory\x00/empty\x00\x00")
	lists := parseDirs(out)
	if got := lists["/a"].Entries; !slices.Equal(got, []DirEntry{{"sub", true}, {"x y.go", false}}) {
		t.Fatalf("/a = %+v", got)
	}
	if err := lists["/gone"].Err; !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("/gone = %v", err)
	}
	if l, ok := lists["/empty"]; !ok || l.Err != nil || len(l.Entries) != 0 {
		t.Fatalf("/empty = %+v", l)
	}
}

// TestHost runs the commands on a real host, which MAYOI_SSH_TEST_HOST
// names, as "user@host": one ssh signs in to without a password.
func TestHost(t *testing.T) {
	host := os.Getenv("MAYOI_SSH_TEST_HOST")
	if host == "" {
		t.Skip("MAYOI_SSH_TEST_HOST is not set")
	}
	exerciseHost(t, host)
}

// fakeSSH is an ssh that runs the command on this machine: as a master it
// makes its socket and waits.
const fakeSSH = `#!/bin/sh
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
exec sh -c "$last"
`

// needsShell skips a test whose fake ssh is a shell script where there is
// no shell to run it, on Windows.
func needsShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake ssh is a shell script")
	}
}

// TestFakeHost runs the commands with this machine's shell and tools, by
// an ssh that runs them here: BSD's stat on macOS, GNU's on Linux.
func TestFakeHost(t *testing.T) {
	needsShell(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(fakeSSH), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	exerciseHost(t, "fake.invalid")
}

func exerciseHost(t *testing.T, host string) {
	c := For(host)
	defer Shutdown()
	changed := make(chan struct{}, 8)
	stop := c.Subscribe(func() { changed <- struct{}{} })
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
	info := c.Info()
	if info.Home == "" || info.OS == "" {
		t.Fatalf("info = %+v", info)
	}
	out, err := c.Run(nil, `mktemp -d "${TMPDIR:-/tmp}/mayoi-test.XXXXXX"`)
	if err != nil {
		t.Fatal(err)
	}
	dir := strings.TrimSpace(string(out))
	defer c.Remove(dir)

	name := dir + "/it's a file.txt"
	data := []byte("one\x00two\nthree 'quoted' $HOME\n")
	fi, err := c.WriteFile(name, data)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != int64(len(data)) || fi.ModTime().IsZero() {
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
	// Read and stat at once.
	if fi, got, err := c.ReadFileInfo(name, 0); err != nil || string(got) != string(data) || fi.Size() != int64(len(data)) || fi.IsDir() {
		t.Fatalf("ReadFileInfo = %+v, %q, %v", fi, got, err)
	}
	if _, _, err := c.ReadFileInfo(name, 4); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("ReadFileInfo limit: %v", err)
	}
	if _, _, err := c.ReadFileInfo(dir+"/nope", 0); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadFileInfo of a missing file: %v", err)
	}
	if _, _, err := c.ReadFileInfo(dir, 0); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadFileInfo of a folder: %v", err)
	}
	if _, err := c.Stat(dir + "/nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stat of a missing file: %v", err)
	}
	// Writing again keeps the mode.
	c.Run(nil, `chmod 600 "$1"`, name)
	if fi, err = c.WriteFile(name, []byte("again")); err != nil || fi.Mode().Perm() != 0o600 || fi.Size() != 5 {
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
	c.Run(nil, `ln -s sub "$1/link"`, dir)
	entries, err := c.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(entries, func(a, b DirEntry) int { return strings.Compare(a.Name, b.Name) })
	if want := []DirEntry{{"it's a file.txt", false}, {"link", true}, {"sub", true}}; !slices.Equal(entries, want) {
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
	c.Disconnect()
	if _, err := c.Stat(dir); !errors.Is(err, ErrOffline) {
		t.Fatalf("offline stat: %v", err)
	}
	c.Connect()
	for s, _ := c.State(); s != Connected; s, _ = c.State() {
		if s == Offline {
			t.Fatal("could not connect again")
		}
		<-changed
	}
	if _, err := c.Stat(dir); err != nil {
		t.Fatal(err)
	}
}

// TestRefused: a host that refuses the keys, or whose key is not
// trusted, is not worth trying again; one that is not there is.
func TestRefused(t *testing.T) {
	for stderr, want := range map[string]bool{
		"me@box: Permission denied (publickey).":                     true,
		"Host key verification failed.":                              true,
		"ssh: connect to host box port 22: Operation timed out":      false,
		"ssh: Could not resolve hostname box: nodename nor servname": false,
		"": false,
	} {
		if got := Refused(sshError(stderr, ErrLost)); got != want {
			t.Errorf("Refused(%q) = %v", stderr, got)
		}
	}
}
