package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"mayoi/internal/remote"
	"mayoi/internal/settings"

	"github.com/egoist/mygo/ui"
)

// TestRemoteFileOffline: a file of a host that is not connected opens in
// a tab at once, read-only, saying it shows once the host connects, and
// takes no edits.
func TestRemoteFileOffline(t *testing.T) {
	remoteOnly(t)
	hs := newHarness(t, true)
	a := hs.a
	path := remote.Path("offline.invalid", "/srv/app/main.go")
	a.openFile(path, true)
	hs.settle()
	d := a.activeEditor()
	if d == nil || d.path != path {
		t.Fatalf("no tab of %s", path)
	}
	if !errors.Is(d.loadErr, remote.ErrOffline) || !d.locked() {
		t.Fatalf("loadErr = %v, locked = %v", d.loadErr, d.locked())
	}
	if !hs.tt.HasText("main.go shows once offline.invalid is connected.") {
		t.Errorf("the tab does not say why it is empty: %q", hs.tt.Texts())
	}
	d.edit(d.sel, textEdit{0, 0, "typed"})
	if d.buf.String() != "" || d.dirty() {
		t.Fatalf("a locked document took an edit: %q", d.buf.String())
	}
	if d.undo() {
		t.Fatal("a locked document undid")
	}
	// Saving says why it cannot.
	a.saveDoc(d.document, nil)
	if !slices.ContainsFunc(a.errors, func(s string) bool { return strings.Contains(s, "not connected to offline.invalid") }) {
		t.Errorf("saving offline says %q", a.errors)
	}
}

func TestRemoteMarkdownPreviewOfflineRestore(t *testing.T) {
	remoteOnly(t)
	for _, preview := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "preview"}[preview], func(t *testing.T) {
			hs := newHarness(t, true)
			s := settings.Default()
			s.MarkdownPreview = !preview
			w := s.NewWindow()
			w.Folder = hs.root
			path := remote.Path("offline.invalid", "/srv/app/README.md")
			w.Columns = []settings.Column{{Weight: 1, Tabs: []settings.Tab{{File: path, Preview: preview, Sel: [2]int{12, 15}, ScrollY: 400}}}}
			for range 2 {
				a := newApp(&fakeHost{folder: hs.root}, s, w)
				t.Cleanup(a.shutdown)
				bh := &harness{t: t, a: a, root: hs.root, tt: ui.NewTester(a.view, 1200, 760)}
				bh.until("offline restored tab", func() bool {
					d := a.activeEditor()
					return d != nil && errors.Is(d.loadErr, remote.ErrOffline)
				})
				a.rememberTabs()
				saved := w.Columns[0].Tabs[0]
				if saved.Preview != preview || saved.Sel != [2]int{12, 15} || saved.ScrollY != 400 {
					t.Fatalf("offline restore lost saved state: %+v", saved)
				}
			}
		})
	}
}

// A Markdown file made anew on a host opens as its source, to edit, the
// markdownPreview preference notwithstanding; an ordinary remote open of
// an existing one still previews.
func TestRemoteNewMarkdownFileOpensAsSource(t *testing.T) {
	t.Setenv("MAYOI_SSH_TEST_HOST", "")
	hs, c, dir := remoteHarness(t)
	a, tt := hs.a, hs.tt
	a.settings.MarkdownPreview = true
	if _, err := c.Run(nil, `printf '# Readme\n\nRemote words.\n' > "$1/README.md"`, dir); err != nil {
		t.Fatal(err)
	}
	a.askNewEntry(a.root, false)
	hs.settle()
	tt.Type("notes.md")
	tt.Key(0, ui.KeyEnter)
	hs.untilLong("the new remote file", func() bool {
		d := a.activeEditor()
		return d != nil && d.path == hs.path("notes.md") && !d.loading && d.loadErr == nil
	})
	d := a.activeEditor()
	if d.preview != nil {
		t.Fatal("a new remote Markdown file opened previewed")
	}
	tt.Type("# hello")
	hs.settle()
	if got := d.buf.String(); got != "# hello" {
		t.Fatalf("the new remote file holds %q", got)
	}
	a.saveDoc(d.document, nil)
	hs.untilLong("the save", func() bool { return !d.saving && !d.dirty() })
	if got, err := c.ReadFile(dir+"/notes.md", 0); err != nil || string(got) != "# hello" {
		t.Fatalf("on the host: %q, %v", got, err)
	}
	a.openFile(hs.path("README.md"), true)
	hs.untilLong("the remote preview", func() bool {
		d := a.activeEditor()
		return d != nil && d.path == hs.path("README.md") && !d.loading && d.preview != nil && d.preview.loaded
	})
}

// TestRemotePathsShow: a remote path shows as host:/path, and copies as
// its host spells it.
func TestRemotePathsShow(t *testing.T) {
	remoteOnly(t)
	p := remote.Path("me@box", "/home/me/proj")
	if got := shortenHome(p); got != "me@box:/home/me/proj" {
		t.Errorf("shortenHome = %q", got)
	}
	if got := shortenHome(filepath.Dir(remote.Path("box", "/x"))); got != "box:/" {
		t.Errorf("the root of a host shows as %q", got)
	}
	if got := hostPath(p + "/a.go"); got != "/home/me/proj/a.go" {
		t.Errorf("hostPath = %q", got)
	}
	if got := hostPath("/Users/me/a.go"); got != "/Users/me/a.go" {
		t.Errorf("hostPath of a local path = %q", got)
	}
	s := settings.Default()
	s.AddRecent(p)
	if got := s.RecentExcept(""); !slices.Contains(got, p) {
		t.Errorf("a remote folder is left out of the recent ones: %q", got)
	}
}

func TestPickDir(t *testing.T) {
	for _, c := range []struct{ query, dir, prefix string }{
		{"/home/me/", "/home/me", ""},
		{"/home/me/pro", "/home/me", "pro"},
		{"/", "/", ""},
		{"", "/", ""},
		{"/srv", "/", "srv"},
	} {
		if dir, prefix := pickDir(c.query); dir != c.dir || prefix != c.prefix {
			t.Errorf("pickDir(%q) = %q, %q", c.query, dir, prefix)
		}
	}
}

// TestConnectBoxListsHosts: the box that connects lists the hosts of the
// recent folders, and takes a host typed that it does not list.
func TestConnectBoxListsHosts(t *testing.T) {
	remoteOnly(t)
	hs := newHarness(t, true)
	a := hs.a
	a.settings.AddRecent(remote.Path("recent-box.invalid", "/srv/app"))
	a.run("remote.connect")
	hs.settle()
	if !a.remotePick.open {
		t.Fatal("the box did not open")
	}
	if !hs.tt.HasText("recent-box.invalid") {
		t.Errorf("the box lacks the recent host: %q", hs.tt.Texts())
	}
	hs.tt.Type("someone@typed.invalid")
	hs.settle()
	if !hs.tt.HasText("Connect to someone@typed.invalid") {
		t.Errorf("the box does not offer the host typed: %q", hs.tt.Texts())
	}
	hs.shot("remote-01-connect")
	hs.tt.Key(0, ui.KeyEscape)
	hs.settle()
	if a.remotePick.open {
		t.Error("Escape left the box open")
	}
}

// fakeSSH is an ssh that runs the command on this machine: as a master it
// makes its socket and waits.
const fakeSSH = `#!/bin/sh
sock= master= last= resolve=
for a; do
  case $a in
    ControlPath=*) sock=${a#ControlPath=} ;;
    -N) master=1 ;;
    -G) resolve=1 ;;
  esac
  last=$a
done
if [ -n "$resolve" ]; then
  printf 'user deploy\nhostname %s.example.com\nport 22\n' "$last"
  exit 0
fi
# A host that refuses, and one that never answers.
case $last in
  *refused*) echo "deploy@$last: Permission denied (publickey)." >&2; exit 255 ;;
  *slow*) if [ -n "$master" ]; then exec sleep 600; fi ;;
esac
if [ -n "$master" ]; then
  trap 'rm -f "$sock"; exit 0' INT TERM
  : > "$sock"
  while :; do sleep 0.05; done
fi
exec sh -c "$last"
`

// remoteHarness opens a remote folder, made with a few files and a Git
// repository, and waits for its tree. The host is MAYOI_SSH_TEST_HOST,
// as "user@host", one ssh signs in to without a password; without it, an
// ssh that runs the commands on this machine stands for one.
func remoteHarness(t *testing.T) (*harness, *remote.Conn, string) {
	remoteOnly(t)
	host := os.Getenv("MAYOI_SSH_TEST_HOST")
	if host == "" {
		useFakeSSH(t)
		host = "fake.invalid"
	}
	c := remote.For(host)
	c.Acquire()
	t.Cleanup(c.Release)
	c.Connect()
	deadline := time.Now().Add(60 * time.Second)
	for !c.Connected() {
		if s, err := c.State(); s == remote.Offline && err != nil || time.Now().After(deadline) {
			t.Fatalf("could not connect to %s: %v", host, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	out, err := c.Run(nil, `d=$(mktemp -d "${TMPDIR:-/tmp}/mayoi-app.XXXXXX") && cd "$d" &&
mkdir -p src && printf 'package main\n\nfunc main() {}\n' > main.go && printf 'needle here\n' > src/a.txt &&
git init -q && git -c user.email=t@t -c user.name=t add -A && git -c user.email=t@t -c user.name=t commit -qm first && echo "$d"`)
	if err != nil {
		t.Fatal(err)
	}
	dir := strings.TrimSpace(string(out))
	t.Cleanup(func() { c.Remove(dir) })
	root := remote.Path(host, dir)

	s := settings.Default()
	w := s.NewWindow()
	w.Folder = root
	h := &fakeHost{}
	a := newApp(h, s, w)
	t.Cleanup(a.shutdown)
	tt := ui.NewTester(a.view, 1200, 760)
	hs := &harness{t: t, a: a, h: h, tt: tt, root: root}
	hs.untilLong("the remote tree", func() bool { return tt.HasText("main.go") && tt.HasText("src") })
	t.Cleanup(a.shutdown)
	return hs, c, dir
}

// remoteOnly skips a test of remote folders where Mayoi reaches none, as
// on Windows, whose paths are not POSIX's and which has no shell for the
// fake ssh.
func remoteOnly(t testing.TB) {
	t.Helper()
	if !remoteSupported {
		t.Skip("no remote folders on " + runtime.GOOS)
	}
}

// useFakeSSH puts fakeSSH first on the PATH.
func useFakeSSH(t *testing.T) {
	remoteOnly(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(fakeSSH), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// untilLong draws frames until ok, as until does, waiting longer, for a
// host over the network.
func (hs *harness) untilLong(what string, ok func() bool) {
	hs.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			hs.t.Fatalf("timed out waiting for %s: %q", what, hs.tt.Texts())
		}
		hs.settle()
	}
}

// TestRemoteFolder works in a folder of a real host: the tree, a file
// opened, edited and saved, a change made on the host, Git, search, and
// the connection ended and made again.
func TestRemoteFolder(t *testing.T) {
	hs, c, dir := remoteHarness(t)
	a := hs.a
	if !strings.Contains(a.windowTitle(), "[SSH: "+c.Host+"]") {
		t.Errorf("title = %q", a.windowTitle())
	}
	main := hs.path("main.go")
	a.openFile(main, true)
	hs.untilLong("main.go to be read", func() bool {
		d := a.activeEditor()
		return d != nil && !d.loading && strings.HasPrefix(d.buf.String(), "package main")
	})
	d := a.activeEditor()
	hs.shot("remote-02-connected")
	if d.dirty() || d.locked() {
		t.Fatalf("dirty = %v, locked = %v", d.dirty(), d.locked())
	}
	d.edit(d.sel, textEdit{0, 0, "// edited\n"})
	a.saveDoc(d.document, nil)
	hs.untilLong("the save", func() bool { return !d.saving && !d.dirty() })
	got, err := c.ReadFile(dir+"/main.go", 0)
	if err != nil || !strings.HasPrefix(string(got), "// edited\npackage main") {
		t.Fatalf("on the host: %q, %v", got, err)
	}
	// Git sees the change.
	hs.untilLong("the status", func() bool {
		e, ok := a.git.entries[main]
		return ok && e.Letter() == 'M'
	})
	// A change on the host shows in the editor, and a new file in the tree.
	if _, err := c.Run(nil, `sleep 1; printf 'changed on the host\n' > "$1/main.go"; : > "$1/new.txt"`, dir); err != nil {
		t.Fatal(err)
	}
	hs.untilLong("the change on the host", func() bool { return d.buf.String() == "changed on the host\n" })
	hs.untilLong("the new file", func() bool { return hs.tt.HasText("new.txt") })
	// Search finds the files on the host.
	hs.searchFor("needle")
	hs.untilLong("the search", func() bool {
		return slices.Equal(resultFiles(&a.inFiles), []string{"src/a.txt"})
	})
	// Disconnected, the file is read-only and the banner says so.
	a.disconnect()
	hs.untilLong("the disconnection", func() bool { return a.connState == remote.Offline })
	if !hs.tt.HasText("Reconnect") || !d.locked() {
		t.Fatalf("offline: banner %v, locked %v", hs.tt.HasText("Reconnect"), d.locked())
	}
	hs.shot("remote-03-offline")
	// The box of the folders of the host.
	a.reconnect()
	hs.untilLong("the connection", func() bool { return a.connState == remote.Connected })
	a.openRemoteFolder()
	hs.untilLong("the box of the folders", func() bool { return a.remotePick.stage == pickFolder })
	if a.remotePick.query != c.Info().Home+"/" {
		t.Errorf("the box starts at %q", a.remotePick.query)
	}
	// The folder of the folder open, which has it.
	a.remotePick.query = filepath.Dir(dir) + "/"
	hs.untilLong("the folders of the host", func() bool { return slices.Contains(a.pickRows(), filepath.Base(dir)) })
	hs.shot("remote-04-folders")
	a.closeRemotePicker()
	hs.untilLong("the connection again", func() bool { return a.connState == remote.Connected && !d.locked() })
	// New files and folders go to the host.
	a.newRemoteEntry(a.root, hs.path("made/by/mayoi.txt"), false)
	hs.untilLong("the new file to open", func() bool {
		ed := a.activeEditor()
		return ed != nil && ed.path == hs.path("made/by/mayoi.txt") && !ed.loading
	})
	if _, err := c.Stat(dir + "/made/by/mayoi.txt"); err != nil {
		t.Fatal(err)
	}
	// The tabs of the folder open again, each where it was once read.
	a.activate(a.column(), slices.IndexFunc(a.column().tabs, func(t *colTab) bool { return t.ed != nil && t.ed.path == main }), true)
	hs.settle()
	ed := a.activeEditor()
	ed.selectRange(3, 5, false)
	hs.settle()
	a.rememberTabs()
	w := *a.window
	a2 := newApp(&fakeHost{}, a.settings, &w)
	t.Cleanup(a2.shutdown)
	tt2 := ui.NewTester(a2.view, 1200, 760)
	t.Cleanup(a2.shutdown)
	hs2 := &harness{t: t, a: a2, h: &fakeHost{}, tt: tt2, root: hs.root}
	hs2.untilLong("the tab of the last session", func() bool {
		ed := a2.activeEditor()
		return ed != nil && ed.path == main && !ed.loading && ed.buf.Len() > 0
	})
	if got := a2.activeEditor().sel; got != [2]int{3, 5} {
		t.Errorf("the tab restored has %v selected", got)
	}
}

// A project of a host shows its logo as one of this machine does, found
// through the same files, with an ssh that runs the commands here.
func TestRemoteProjectLogo(t *testing.T) {
	remoteOnly(t)
	bin := t.TempDir()
	fake := "#!/bin/sh\nsock= master= last=\nfor a; do\n  case $a in\n    ControlPath=*) sock=${a#ControlPath=} ;;\n    -N) master=1 ;;\n  esac\n  last=$a\ndone\n" +
		"if [ -n \"$master\" ]; then\n  trap 'rm -f \"$sock\"; exit 0' INT TERM\n  : > \"$sock\"\n  while :; do sleep 0.05; done\nfi\nexec sh -c \"$last\"\n"
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	c := remote.For("fake.invalid")
	defer remote.Shutdown()
	c.Connect()
	for end := time.Now().Add(30 * time.Second); !c.Connected(); time.Sleep(20 * time.Millisecond) {
		if s, err := c.State(); s == remote.Offline || time.Now().After(end) {
			t.Fatalf("could not connect: %v", err)
		}
	}

	root := t.TempDir()
	dir := remote.Path("fake.invalid", root)
	if loadProjectLogo(dir) != nil {
		t.Fatal("a logo in an empty folder")
	}
	if err := os.MkdirAll(filepath.Join(root, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, filepath.Join(root, "public", "logo.png"), 16, 16)
	if loadProjectLogo(dir) == nil {
		t.Error("no logo from public/logo.png on a host")
	}
	// A declared icon is read from the host's source, then its file.
	declared := t.TempDir()
	if err := os.WriteFile(filepath.Join(declared, "index.html"), []byte(`<link rel="icon" href="/brand/mark.svg">`), 0o644); err != nil {
		t.Fatal(err)
	}
	remoteDeclared := remote.Path("fake.invalid", declared)
	if loadProjectLogo(remoteDeclared) != nil {
		t.Fatal("a logo without its file")
	}
	mark := filepath.Join(declared, "public", "brand", "mark.svg")
	if err := os.MkdirAll(filepath.Dir(mark), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mark, []byte(testSVG), 0o644); err != nil {
		t.Fatal(err)
	}
	if loadProjectLogo(remoteDeclared) == nil {
		t.Error("no logo from the icon a host's source declares")
	}
	// A mygo app's icon, and a macOS app's .icns, are found on a host too.
	mygoApp := t.TempDir()
	writeFile(t, filepath.Join(mygoApp, "mygo.json"), []byte(`{"name": "App"}`))
	writeFile(t, filepath.Join(mygoApp, "resources", "icon.png"), pngBytes(t, 16, 16))
	if loadProjectLogo(remote.Path("fake.invalid", mygoApp)) == nil {
		t.Error("no logo from a mygo app's icon on a host")
	}
	native := t.TempDir()
	writeFile(t, filepath.Join(native, "app", "Resources", "App.icns"), icnsBytes(t, map[string][]byte{"ic07": pngBytes(t, 128, 128)}))
	if loadProjectLogo(remote.Path("fake.invalid", native)) == nil {
		t.Error("no logo from a macOS app's .icns on a host")
	}
	// A host not connected has none, and is not scanned.
	off := remote.Path("offline.invalid", "/srv/app")
	if loadProjectLogo(off) != nil {
		t.Error("a logo from a host that is not connected")
	}
	h := &desktop{}
	if h.needsLogoScan(off, "") {
		t.Error("a host that is not connected is scanned")
	}
	// But its icon of its own, of this machine, shows.
	if !h.needsLogoScan(off, "/icons/a.png") {
		t.Error("the icon of a host's project waits for the host")
	}
	// And one it no longer has goes without waiting.
	h.logoDone, h.logoDir, h.logoIcon = true, off, "/icons/a.png"
	if !h.needsLogoScan(off, "") {
		t.Error("the icon reset waits for the host")
	}
}
