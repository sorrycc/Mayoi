package remote

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// Files go to and from a host as a tar stream through the connection:
// one command for any number of files, with nothing on the host but tar.
// They land in a folder of their own beside where they go, and move into
// place only once they all arrived, so that a connection lost on the way
// leaves nothing half copied.

// Progress counts the bytes of a transfer as they go, which another
// goroutine may read, and how many there are, when that is known. One of
// NewProgress can also stop the transfer.
type Progress struct {
	done, total atomic.Int64
	ctx         context.Context
	cancel      context.CancelFunc
}

// ErrCanceled is the error of a transfer stopped by Progress.Cancel.
var ErrCanceled = errors.New("canceled")

// NewProgress returns the Progress of a transfer that Cancel can stop.
func NewProgress() *Progress {
	p := &Progress{}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	return p
}

// Cancel stops the transfer: what arrived of it is taken away.
func (p *Progress) Cancel() {
	if p != nil && p.cancel != nil {
		p.cancel()
	}
}

// Canceled reports whether Cancel was called.
func (p *Progress) Canceled() bool {
	return p != nil && p.ctx != nil && p.ctx.Err() != nil
}

// within returns the context of the connection, which also ends as p is
// canceled, and a function to call once the transfer is done. A transfer
// canceled already does not start: the context would end only a moment
// later, after a small one may have arrived.
func (c *Conn) within(p *Progress) (context.Context, func(), error) {
	if p.Canceled() {
		return nil, nil, ErrCanceled
	}
	ctx, err := c.transferContext()
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	if p == nil || p.ctx == nil {
		return ctx, cancel, nil
	}
	stop := context.AfterFunc(p.ctx, cancel)
	return ctx, func() { stop(); cancel() }, nil
}

// transferContext is the context of the connection, or ErrOffline: with
// the pool, as on Windows and in WSL, there is no master's socket.
func (c *Conn) transferContext() (context.Context, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != Connected || (c.pool == nil && c.dir == "") {
		return nil, ErrOffline
	}
	if c.ctx == nil {
		return context.Background(), nil
	}
	return c.ctx, nil
}

// stream runs script on the host with stdin, nil for none, writing what
// it prints to stdout, through the pool or the master, whichever the
// connection has.
func (c *Conn) stream(ctx context.Context, stdin io.Reader, stdout io.Writer, script string, args ...string) error {
	var errb bytes.Buffer
	c.mu.Lock()
	pool := c.pool
	c.mu.Unlock()
	if pool != nil {
		code, err := pool.Call(ctx, c.full(script, args), stdin, stdout, &errb)
		if err != nil {
			return c.failure(err, errb.String())
		}
		if code != 0 {
			return c.failure(fmt.Errorf("exit status %d", code), errb.String())
		}
		return nil
	}
	sock, _, err := c.live()
	if err != nil {
		return err
	}
	cmd := c.command(ctx, sock, stdin, script, args...)
	cmd.Stdout, cmd.Stderr = stdout, &errb
	if err := cmd.Run(); err != nil {
		return c.failure(err, errb.String())
	}
	return nil
}

// stopped is err, or ErrCanceled once p was canceled.
func (p *Progress) stopped(err error) error {
	if err != nil && p.Canceled() {
		return ErrCanceled
	}
	return err
}

// Done returns the bytes sent or received so far.
func (p *Progress) Done() int64 {
	if p == nil {
		return 0
	}
	return p.done.Load()
}

// Total returns the bytes of the transfer, 0 when not known.
func (p *Progress) Total() int64 {
	if p == nil {
		return 0
	}
	return p.total.Load()
}

func (p *Progress) add(n int) {
	if p != nil {
		p.done.Add(int64(n))
	}
}

// counter is a writer or reader that counts what goes through it.
type counter struct {
	w io.Writer
	r io.Reader
	p *Progress
}

func (c *counter) Write(b []byte) (int, error) {
	n, err := c.w.Write(b)
	c.p.add(n)
	return n, err
}

func (c *counter) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.p.add(n)
	return n, err
}

// freeNameFn is the function of sh that prints a name for $1 in the
// folder $dir that no file has: $1 itself, or, as the Finder names
// copies, "name copy", then "name copy 2" and on, before the extension
// of a file. $2 is 1 for a folder, which keeps its name whole.
const freeNameFn = `free() {
  if [ ! -e "$dir/$1" ] && [ ! -L "$dir/$1" ]; then printf '%s' "$1"; return; fi
  stem=$1 ext=
  if [ "$2" != 1 ]; then
    case $1 in ?*.*) ext=.${1##*.}; stem=${1%.*} ;; esac
  fi
  k=1
  while :; do
    if [ $k = 1 ]; then n="$stem copy$ext"; else n="$stem copy $k$ext"; fi
    if [ ! -e "$dir/$n" ] && [ ! -L "$dir/$n" ]; then printf '%s' "$n"; return; fi
    k=$((k+1))
  done
}
`

// placeScript moves what the host unpacked into the staging folder $s,
// each argument at its place there, "$i/name", into $dir under a free
// name, and prints where each went, followed by a NUL.
const placeScript = freeNameFn + `i=0
for name; do
  d=0; [ -d "$s/$i/$name" ] && [ ! -L "$s/$i/$name" ] && d=1
  to=$(free "$name" $d) || exit 1
  mv -- "$s/$i/$name" "$dir/$to" || exit 1
  printf '%s/%s\0' "$dir" "$to"
  i=$((i+1))
done
`

// Upload copies files and folders of this machine into the folder dir of
// the host, each under a name no file there has, and returns where each
// went on the host. With dir "", they go to a new folder of the host's
// temporary ones, as for a program of the host to read them.
func (c *Conn) Upload(srcs []string, dir string, p *Progress) ([]string, error) {
	if len(srcs) == 0 {
		return nil, nil
	}
	names := make([]string, len(srcs))
	var total int64
	for i, src := range srcs {
		names[i] = filepath.Base(src)
		n, err := treeSize(src)
		if err != nil {
			return nil, err
		}
		total += n
	}
	if p != nil {
		p.total.Store(total)
	}
	ctx, done, err := c.within(p)
	if err != nil {
		return nil, err
	}
	defer done()
	windows := c.windows()
	pr, pw := io.Pipe()
	go func() {
		if windows {
			pw.CloseWithError(writePack(&counter{w: pw, p: p}, srcs))
		} else {
			pw.CloseWithError(writeTar(&counter{w: pw, p: p}, srcs))
		}
	}()
	script := `dir=$1; shift
if [ -z "$dir" ]; then dir=$(mktemp -d "${TMPDIR:-/tmp}/mayoi-paste.XXXXXX") || exit 1; fi
s=$(mktemp -d "$dir/.mayoi-paste.XXXXXX") || exit 1
trap 'rm -rf "$s"' EXIT
trap 'exit 1' HUP INT TERM PIPE
tar -x -o -f - -C "$s" || exit 1
` + placeScript
	if windows {
		script = psUpload
	}
	var b bytes.Buffer
	err = c.stream(ctx, pr, &b, script, append([]string{dir}, names...)...)
	pr.Close()
	out := b.Bytes()
	if err != nil {
		// Those it placed before it stopped, as a cancel can come after
		// the last: whole, each ended by its NUL.
		return splitNul(out[:bytes.LastIndexByte(out, 0)+1]), p.stopped(err)
	}
	return splitNul(out), nil
}

// CopyOnHost copies files and folders of the host into its folder dir,
// each under a name no file there has, and returns where each went.
func (c *Conn) CopyOnHost(srcs []string, dir string, p *Progress) ([]string, error) {
	for _, src := range srcs {
		if dir == src || strings.HasPrefix(dir, strings.TrimSuffix(src, "/")+"/") {
			return nil, errors.New("a folder cannot be pasted into itself")
		}
	}
	ctx, done, err := c.within(p)
	if err != nil {
		return nil, err
	}
	defer done()
	var b bytes.Buffer
	script := freeNameFn + `dir=$1; shift
for src; do
  d=0; [ -d "$src" ] && [ ! -L "$src" ] && d=1
  to=$(free "$(basename -- "$src")" $d) || exit 1
  cp -R -P -p -- "$src" "$dir/$to" || exit 1
  printf '%s/%s\0' "$dir" "$to"
done
`
	if c.windows() {
		script = psCopy
	}
	err = c.stream(ctx, nil, &b, script, append([]string{dir}, srcs...)...)
	out := b.Bytes()
	if err != nil {
		return splitNul(out), p.stopped(err)
	}
	return splitNul(out), nil
}

// Download copies a file or folder of the host into the folder dir of
// this machine, under name, which is where it lands.
func (c *Conn) Download(src, to string, p *Progress) error {
	ctx, done, err := c.within(p)
	if err != nil {
		return err
	}
	defer done()
	windows := c.windows()
	script := `cd -- "$(dirname -- "$1")" || exit 1
COPYFILE_DISABLE=1 exec tar -c -f - -- "$(basename -- "$1")"`
	if windows {
		script = psDownload
	}
	pr, pw := io.Pipe()
	ran := make(chan error, 1)
	go func() {
		err := c.stream(ctx, nil, pw, script, src)
		pw.CloseWithError(err)
		ran <- err
	}()
	var readErr error
	if windows {
		readErr = readPack(&counter{r: pr, p: p}, path.Base(src), to)
	} else {
		readErr = readTar(&counter{r: pr, p: p}, path.Base(src), to)
	}
	io.Copy(io.Discard, pr)
	if err := <-ran; err != nil {
		return p.stopped(err)
	}
	return p.stopped(readErr)
}

// splitNul splits what placeScript printed into the paths.
func splitNul(out []byte) []string {
	var paths []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			paths = append(paths, path.Clean(p))
		}
	}
	return paths
}

// treeSize returns the bytes of the files at src and under it.
func treeSize(src string) (int64, error) {
	var n int64
	err := filepath.WalkDir(src, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n, err
}

// writeTar writes the files and folders srcs to w as a tar stream, the
// one at i as "i/name", with their modes and links.
func writeTar(w io.Writer, srcs []string) error {
	tw := tar.NewWriter(w)
	for i, src := range srcs {
		top := fmt.Sprintf("%d", i)
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: top + "/", Mode: 0o700}); err != nil {
			return err
		}
		base := filepath.Dir(src)
		err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(base, p)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			h := &tar.Header{Name: top + "/" + filepath.ToSlash(rel), Mode: int64(info.Mode().Perm()),
				ModTime: info.ModTime().Truncate(1e9)}
			switch {
			case info.Mode()&fs.ModeSymlink != 0:
				target, err := os.Readlink(p)
				if err != nil {
					return err
				}
				h.Typeflag, h.Linkname = tar.TypeSymlink, target
			case info.IsDir():
				h.Typeflag = tar.TypeDir
				h.Name += "/"
			case info.Mode().IsRegular():
				h.Typeflag, h.Size = tar.TypeReg, info.Size()
			default:
				// Sockets, pipes and devices stay behind.
				return nil
			}
			if err := tw.WriteHeader(h); err != nil {
				return err
			}
			if h.Typeflag != tar.TypeReg {
				return nil
			}
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.CopyN(tw, f, h.Size)
			return err
		})
		if err != nil {
			return err
		}
	}
	return tw.Close()
}

// readTar unpacks a tar stream of the entry name, and what is under it,
// at to, which must not be there: the files, folders and links, with
// their modes. It takes nothing outside name.
func readTar(r io.Reader, name, to string) error {
	tr := tar.NewReader(r)
	got := false
	// links are the links unpacked, which nothing goes through.
	links := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		rel := path.Clean(strings.TrimPrefix(h.Name, "./"))
		var dst string
		switch {
		case rel == name:
			dst = to
		case strings.HasPrefix(rel, name+"/"):
			dst = filepath.Join(to, filepath.FromSlash(strings.TrimPrefix(rel, name+"/")))
		default:
			continue
		}
		if underLink(rel, links) {
			continue
		}
		mode := fs.FileMode(h.Mode).Perm()
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.Mkdir(dst, mode|0o700); err != nil && !errors.Is(err, fs.ErrExist) {
				return err
			}
		case tar.TypeSymlink:
			if err := os.Symlink(h.Linkname, dst); err != nil {
				return err
			}
			links[rel] = true
		case tar.TypeReg:
			f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		default:
			continue
		}
		got = true
	}
	if !got {
		return &fs.PathError{Op: "copy", Path: name, Err: fs.ErrNotExist}
	}
	return nil
}

// underLink reports whether a folder above rel is one of links.
func underLink(rel string, links map[string]bool) bool {
	for d := path.Dir(rel); d != "." && d != "/"; d = path.Dir(d) {
		if links[d] {
			return true
		}
	}
	return false
}
