package remote

import (
	"archive/tar"
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"
)

// FileInfo is what stat says of a file on a host.
type FileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
	// stamp is the size and time of the file as the host's stat
	// printed them, which WriteFileIf compares with what it prints then.
	stamp string
}

func (f *FileInfo) Name() string       { return f.name }
func (f *FileInfo) Size() int64        { return f.size }
func (f *FileInfo) Mode() fs.FileMode  { return f.mode }
func (f *FileInfo) ModTime() time.Time { return f.modTime }
func (f *FileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f *FileInfo) Sys() any           { return nil }

// Stamp is the size and time of the file as the host printed them, which
// tell whether it changed since, to WriteFileIf.
func (f *FileInfo) Stamp() string { return f.stamp }

// NewFileInfo makes the FileInfo of a file, as for tests.
func NewFileInfo(name string, size int64, mode fs.FileMode, modTime time.Time) *FileInfo {
	return &FileInfo{name: name, size: size, mode: mode, modTime: modTime}
}

// statCmd is the stat of the host that prints, for each file, a line of
// its size, the time it changed, its mode in hex and its name:
// "12 1700000000.123 81a4|name". follow follows links.
func (i Info) statCmd(follow bool) string {
	l := ""
	if follow {
		l = "-L "
	}
	switch {
	case i.gnu && i.nanos:
		return "stat " + l + `-c '%s %.9Y %f|%n'`
	case i.gnu:
		return "stat " + l + `-c '%s %Y %f|%n'`
	}
	return "stat " + l + `-f '%z %Fm %Xp|%N'`
}

// parseStat reads the lines of statCmd, by name.
func parseStat(out []byte) map[string]*FileInfo {
	infos := map[string]*FileInfo{}
	for line := range strings.SplitSeq(string(out), "\n") {
		head, name, ok := strings.Cut(line, "|")
		if !ok {
			continue
		}
		f := strings.Fields(head)
		if len(f) != 3 {
			continue
		}
		size, err1 := strconv.ParseInt(f[0], 10, 64)
		mode, err2 := strconv.ParseUint(f[2], 16, 32)
		if err1 != nil || err2 != nil {
			continue
		}
		infos[name] = &FileInfo{name: path.Base(name), size: size, mode: fileMode(uint32(mode)), modTime: parseTime(f[1]),
			stamp: f[0] + " " + f[1]}
	}
	return infos
}

// fileMode turns a mode of stat into Go's.
func fileMode(m uint32) fs.FileMode {
	mode := fs.FileMode(m & 0o777)
	switch m & 0o170000 {
	case 0o040000:
		mode |= fs.ModeDir
	case 0o120000:
		mode |= fs.ModeSymlink
	case 0o010000:
		mode |= fs.ModeNamedPipe
	case 0o140000:
		mode |= fs.ModeSocket
	case 0o020000:
		mode |= fs.ModeDevice | fs.ModeCharDevice
	case 0o060000:
		mode |= fs.ModeDevice
	}
	return mode
}

// parseTime reads seconds since 1970, with a fraction or not.
func parseTime(s string) time.Time {
	secs, frac, _ := strings.Cut(s, ".")
	sec, err := strconv.ParseInt(secs, 10, 64)
	if err != nil {
		return time.Time{}
	}
	var nsec int64
	if frac != "" {
		frac = (frac + "000000000")[:9]
		nsec, _ = strconv.ParseInt(frac, 10, 64)
	}
	return time.Unix(sec, nsec)
}

func notExist(op, p string) error {
	return &fs.PathError{Op: op, Path: p, Err: fs.ErrNotExist}
}

// StatMany stats files on the host, following links, in one command: the
// map leaves out those that are not there.
func (c *Conn) StatMany(paths []string) (map[string]*FileInfo, error) {
	if len(paths) == 0 {
		return map[string]*FileInfo{}, nil
	}
	script := c.Info().statCmd(true) + ` -- "$@" 2>/dev/null; exit 0`
	if c.windows() {
		script = psStatAll
	}
	out, err := c.Run(nil, script, paths...)
	if err != nil {
		return nil, err
	}
	return parseStat(out), nil
}

// Stat stats a file on the host, following a link.
func (c *Conn) Stat(p string) (*FileInfo, error) { return c.stat(p, true) }

// Lstat stats a file on the host, not following a link.
func (c *Conn) Lstat(p string) (*FileInfo, error) { return c.stat(p, false) }

func (c *Conn) stat(p string, follow bool) (*FileInfo, error) {
	script := c.Info().statCmd(follow) + ` -- "$1" 2>/dev/null; exit 0`
	if c.windows() {
		script = psStatAll
	}
	out, err := c.Run(nil, script, p)
	if err != nil {
		return nil, err
	}
	if f, ok := parseStat(out)[p]; ok {
		return f, nil
	}
	return nil, notExist("stat", p)
}

// DirEntry is a file in a folder of a host: its name, and whether it is a
// folder, or a link to one.
type DirEntry struct {
	Name string
	Dir  bool
	Link bool // a symbolic link, to a folder when Dir is set
}

// readDirScript lists the folders named after the first argument, each
// as its path, then a NUL, then its entries, each "d name" or "f name",
// "D name" or "F name" for a link, followed by a NUL, and a blank entry to end it; a folder that cannot
// be read is "!" then its error.
const readDirScript = "for dir; do\n" + readDirBody + "done\n"

// readDirBody lists the folder $dir, as readDirScript says.
const readDirBody = `  printf '%s\0' "$dir"
  if ! cd -- "$dir" 2>/dev/null; then
    if [ -e "$dir" ]; then printf '!permission denied\0'; else printf '!no such file or directory\0'; fi
    continue
  fi
  for f in * .*; do
    case $f in .|..) continue;; esac
    if [ -L "$f" ]; then
      if [ -d "$f" ]; then printf 'D %s\0' "$f"; else printf 'F %s\0' "$f"; fi
    elif [ -d "$f" ]; then printf 'd %s\0' "$f"
    elif [ -e "$f" ]; then printf 'f %s\0' "$f"
    fi
  done
  printf '\0'
`

// ReadDir lists a folder of the host.
func (c *Conn) ReadDir(dir string) ([]DirEntry, error) {
	dirs, err := c.ReadDirs([]string{dir})
	if err != nil {
		return nil, err
	}
	r := dirs[dir]
	return r.Entries, r.Err
}

// DirList is a folder listed by ReadDirs, or why it could not be.
type DirList struct {
	Entries []DirEntry
	Err     error
}

// ReadDirs lists folders of the host in one command.
func (c *Conn) ReadDirs(dirs []string) (map[string]DirList, error) {
	if len(dirs) == 0 {
		return map[string]DirList{}, nil
	}
	script := readDirScript
	if c.windows() {
		script = psReadDirs
	}
	out, err := c.Run(nil, script, dirs...)
	if err != nil {
		return nil, err
	}
	return parseDirs(out), nil
}

func parseDirs(out []byte) map[string]DirList {
	lists := map[string]DirList{}
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); {
		dir := fields[i]
		i++
		if dir == "" {
			continue
		}
		if i < len(fields) && strings.HasPrefix(fields[i], "!") {
			msg := fields[i][1:]
			var err error = errors.New(msg)
			if strings.HasPrefix(msg, "no such") {
				err = notExist("open", dir)
			}
			lists[dir] = DirList{Err: err}
			i++
			continue
		}
		entries := []DirEntry{}
		for ; i < len(fields) && fields[i] != ""; i++ {
			kind, name, ok := strings.Cut(fields[i], " ")
			if ok {
				entries = append(entries, DirEntry{Name: name, Dir: kind == "d" || kind == "D", Link: kind == "D" || kind == "F"})
			}
		}
		i++ // the blank entry that ends the folder
		lists[dir] = DirList{Entries: entries}
	}
	return lists
}

// ErrTooLarge is the error of a file larger than a read takes.
var ErrTooLarge = errors.New("file is too large to open")

// ReadFile reads a file of the host, at most limit bytes when limit is
// above 0: a larger file is ErrTooLarge.
func (c *Conn) ReadFile(p string, limit int64) ([]byte, error) {
	script := `if [ -d "$1" ]; then echo "$1: is a directory" >&2; exit 1; fi
exec cat -- "$1"`
	if limit > 0 {
		script = `if [ -d "$1" ]; then echo "$1: is a directory" >&2; exit 1; fi
exec head -c ` + strconv.FormatInt(limit+1, 10) + ` -- "$1"`
	}
	args := []string{p}
	if c.windows() {
		script, args = psReadFile, []string{p, psLimit(limit)}
	}
	out, err := c.Run(nil, script, args...)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, notExist("open", p)
		}
		return nil, err
	}
	if limit > 0 && int64(len(out)) > limit {
		return nil, ErrTooLarge
	}
	return out, nil
}

// ReadFileInfo reads a file of the host as ReadFile does, and stats it,
// at one round trip: what opening a file needs.
func (c *Conn) ReadFileInfo(p string, limit int64) (*FileInfo, []byte, error) {
	read := "cat"
	if limit > 0 {
		read = "head -c " + strconv.FormatInt(limit+1, 10)
	}
	script := `if [ -d "$1" ]; then echo "$1: is a directory" >&2; exit 1; fi
if [ ! -e "$1" ]; then echo "$1: No such file or directory" >&2; exit 1; fi
` + c.Info().statCmd(true) + ` -- "$1" || exit 1
exec ` + read + ` -- "$1"`
	args := []string{p}
	if c.windows() {
		script, args = psReadFileInfo, []string{p, psLimit(limit)}
	}
	out, err := c.Run(nil, script, args...)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, notExist("open", p)
		}
		return nil, nil, err
	}
	// The line of stat ends with the path, then the file.
	head, data, ok := bytes.Cut(out, []byte("|"+p+"\n"))
	if !ok {
		return nil, nil, errors.New("the host did not answer as a POSIX shell does")
	}
	info, ok := parseStat(append(head, "|"+p...))[p]
	if !ok {
		return nil, nil, fmt.Errorf("could not stat %s", p)
	}
	if limit > 0 && int64(len(data)) > limit {
		return nil, nil, ErrTooLarge
	}
	return info, data, nil
}

// WriteFile writes data to a file of the host, and returns what stat says
// of it then. data goes whole to a file beside the host's temporary ones
// first, and is copied into the file only once it is all there, so that
// a connection lost on the way leaves the file as it was. Copied rather
// than renamed in place, the file keeps its owner, mode, links and
// attributes, as the editors of a terminal keep them.
func (c *Conn) WriteFile(p string, data []byte) (*FileInfo, error) {
	return c.WriteFileIf(p, data, "")
}

// ErrChanged is the error of a write to a file that changed on the host
// since it was read.
var ErrChanged = errors.New("the file changed on the host since it was read")

// changedMark is what the script of WriteFileIf says of a file changed.
const changedMark = "mayoi: changed since read"

// WriteFileIf writes a file as WriteFile does, unless the file there no
// longer has the Stamp stamp, which fails with ErrChanged and leaves it
// as it is: the check and the write are one round trip. An empty stamp,
// or a file not there, writes as WriteFile does.
func (c *Conn) WriteFileIf(p string, data []byte, stamp string) (*FileInfo, error) {
	st := c.Info().statCmd(true)
	script := `if [ -n "$3" ] && [ -e "$1" ]; then
  cur=$(` + st + ` -- "$1" | sed -n '1s/^\([^ ]* [^ ]*\) .*/\1/p')
  if [ "$cur" != "$3" ]; then cat > /dev/null; echo "` + changedMark + `" >&2; exit 3; fi
fi
t=$(mktemp "${TMPDIR:-/tmp}/mayoi.XXXXXX") || exit 1
trap 'rm -f "$t"' EXIT
cat > "$t" || exit 1
n=$(wc -c < "$t")
if [ $n -ne $2 ]; then echo "the file did not arrive whole" >&2; exit 1; fi
cat "$t" > "$1" || exit 1
` + st + ` -- "$1"
`
	if c.windows() {
		script = psWriteFile
	}
	out, err := c.Run(bytes.NewReader(data), script, p, strconv.Itoa(len(data)), stamp)
	if err != nil {
		if re, ok := err.(*Error); ok && strings.Contains(re.Stderr, changedMark) {
			return nil, ErrChanged
		}
		return nil, err
	}
	if f, ok := parseStat(out)[p]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("could not stat %s once written", p)
}

// Rename moves a file of the host, unless the target is there.
func (c *Conn) Rename(from, to string) error {
	if c.windows() {
		_, err := c.Run(nil, psRename, from, to)
		return err
	}
	_, err := c.Run(nil, `if [ "$1" != "$2" ] && { [ -e "$2" ] || [ -L "$2" ]; }; then echo "$2: file exists" >&2; exit 1; fi
exec mv -- "$1" "$2"`, from, to)
	return err
}

// Remove deletes a file of the host, or a folder and all in it.
func (c *Conn) Remove(p string) error {
	script := `exec rm -rf -- "$1"`
	if c.windows() {
		script = psRemove
	}
	_, err := c.Run(nil, script, p)
	return err
}

// MkdirAll makes a folder of the host, and those above it.
func (c *Conn) MkdirAll(p string) error {
	script := `exec mkdir -p -- "$1"`
	if c.windows() {
		script = psMkdirAll
	}
	_, err := c.Run(nil, script, p)
	return err
}

// Create makes an empty file of the host, unless one is there.
func (c *Conn) Create(p string) error {
	if c.windows() {
		_, err := c.Run(nil, psCreate, p)
		return err
	}
	_, err := c.Run(nil, `if [ -e "$1" ] || [ -L "$1" ]; then echo "$1: file exists" >&2; exit 1; fi
mkdir -p -- "$(dirname -- "$1")" && : > "$1"`, p)
	return err
}

// ReadFiles reads the files rels of the folder dir of the host, relative
// to it with slashes, those smaller than max bytes, in one tar stream:
// fn gets each as it arrives. Files that are not there, or not regular,
// are left out.
func (c *Conn) ReadFiles(dir string, rels []string, max int64, fn func(rel string, data []byte)) error {
	if len(rels) == 0 {
		return nil
	}
	windows := c.windows()
	args := make([]string, 0, len(rels)+2)
	args = append(args, dir, strconv.FormatInt(max, 10))
	for _, r := range rels {
		if windows {
			args = append(args, r)
		} else {
			args = append(args, "./"+r)
		}
	}
	if windows {
		return c.readFilesPacked(args, rels, fn)
	}
	cmd, err := c.Command(nil, `cd -- "$1" || exit 1; max=$2; shift 2
find "$@" -prune -type f -size -"$max"c 2>/dev/null | {
  set --
  while IFS= read -r f; do set -- "$@" "$f"; done
  # The tar of macOS adds a file of attributes for each file, unless told.
  if [ $# -gt 0 ]; then COPYFILE_DISABLE=1 exec tar -cf - "$@" 2>/dev/null; fi
}
`, args...)
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		return err
	}
	asked := make(map[string]bool, len(rels))
	for _, r := range rels {
		asked[r] = true
	}
	tr := tar.NewReader(out)
	var readErr error
	for {
		h, err := tr.Next()
		if err != nil {
			if err != io.EOF {
				readErr = err
			}
			break
		}
		rel := strings.TrimPrefix(h.Name, "./")
		if h.Typeflag != tar.TypeReg || !asked[rel] {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			readErr = err
			break
		}
		fn(rel, data)
	}
	io.Copy(io.Discard, out)
	if err := cmd.Wait(); err != nil {
		return c.failure(err, errb.String())
	}
	return readErr
}

// lookMark parts what Look's command lists of the folders from what it
// says of the files.
const lookMark = "\x01mayoi-stat\x01\n"

// Look lists folders and stats files of the host, following links, in one
// command: the files that are not there are left out of the map.
func (c *Conn) Look(dirs, files []string) (map[string]DirList, map[string]*FileInfo, error) {
	if len(dirs) == 0 && len(files) == 0 {
		return map[string]DirList{}, map[string]*FileInfo{}, nil
	}
	args := append([]string{strconv.Itoa(len(dirs))}, dirs...)
	args = append(args, files...)
	if c.windows() {
		return c.look(psLook, args)
	}
	script := `n=$1; shift
k=0
for dir; do
  k=$((k+1))
  [ $k -le $n ] || break
` + readDirBody + `done
printf '\001mayoi-stat\001\n'
shift $n
if [ $# -gt 0 ]; then ` + c.Info().statCmd(true) + ` -- "$@" 2>/dev/null; fi
exit 0
`
	return c.look(script, args)
}

func (c *Conn) look(script string, args []string) (map[string]DirList, map[string]*FileInfo, error) {
	out, err := c.Run(nil, script, args...)
	if err != nil {
		return nil, nil, err
	}
	listed, statted, ok := strings.Cut(string(out), lookMark)
	if !ok {
		return nil, nil, errors.New("the host did not answer as a POSIX shell does")
	}
	return parseDirs([]byte(listed)), parseStat([]byte(statted)), nil
}

// StatUnder stats files, and every folder under the folder dir, following
// links, in one command, as for the refs of Git.
func (c *Conn) StatUnder(files []string, dir string) (map[string]*FileInfo, error) {
	st := c.Info().statCmd(true)
	script := `d=$1; shift
if [ $# -gt 0 ]; then ` + st + ` -- "$@" 2>/dev/null; fi
find "$d" -type d -exec ` + st + ` {} + 2>/dev/null
exit 0
`
	if c.windows() {
		script = psStatUnder
	}
	out, err := c.Run(nil, script, append([]string{dir}, files...)...)
	if err != nil {
		return nil, err
	}
	return parseStat(out), nil
}

// psLimit is the bytes psReadFile reads for a limit: one more, to tell a
// file larger than it, or all with none.
func psLimit(limit int64) string {
	if limit > 0 {
		return strconv.FormatInt(limit+1, 10)
	}
	return "-1"
}

// readFilesPacked is ReadFiles on a Windows host, whose files come as a
// pack.
func (c *Conn) readFilesPacked(args, rels []string, fn func(rel string, data []byte)) error {
	cmd, err := c.Command(nil, psReadFiles, args...)
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		return err
	}
	asked := make(map[string]bool, len(rels))
	for _, r := range rels {
		asked[r] = true
	}
	readErr := eachPacked(bufio.NewReader(out), func(dir bool, rel string, _ int64, r io.Reader) error {
		if dir || !asked[rel] {
			return nil
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		fn(rel, data)
		return nil
	})
	io.Copy(io.Discard, out)
	if err := cmd.Wait(); err != nil {
		return c.failure(err, errb.String())
	}
	return readErr
}
