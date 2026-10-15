// Package vfs is the file system of the editor: the files of this
// machine, by the os package, and those of other machines, whose paths
// remote names, over SSH. Each function takes either path and does what
// the os package's of its name does.
//
// A remote file takes a round trip to its host: call these functions off
// the main thread for a path that may be remote.
package vfs

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"mayoi/internal/remote"
)

// IsRemote reports whether p is the path of a file of another machine.
func IsRemote(p string) bool { return remote.Is(p) }

// Stat stats a file, following a link.
func Stat(p string) (fs.FileInfo, error) {
	if c, rp, ok := remote.Lookup(p); ok {
		return wrap(c.Stat(rp))
	}
	return os.Stat(p)
}

// Lstat stats a file, not following a link.
func Lstat(p string) (fs.FileInfo, error) {
	if c, rp, ok := remote.Lookup(p); ok {
		return wrap(c.Lstat(rp))
	}
	return os.Lstat(p)
}

func wrap(f *remote.FileInfo, err error) (fs.FileInfo, error) {
	if err != nil {
		return nil, err
	}
	return f, nil
}

// StatMany stats files, following links, at one round trip for those of a
// host: the map has those that are there, by path. It fails when a host
// cannot be asked, rather than say its files are gone.
func StatMany(paths []string) (map[string]fs.FileInfo, error) {
	out := map[string]fs.FileInfo{}
	byConn := map[*remote.Conn][]string{}
	for _, p := range paths {
		if c, rp, ok := remote.Lookup(p); ok {
			byConn[c] = append(byConn[c], rp)
			continue
		}
		if info, err := os.Stat(p); err == nil {
			out[p] = info
		}
	}
	for c, rps := range byConn {
		infos, err := c.StatMany(rps)
		if err != nil {
			return out, err
		}
		for rp, info := range infos {
			out[remote.Path(c.Host, rp)] = info
		}
	}
	return out, nil
}

// ReadFile reads a file.
func ReadFile(p string) ([]byte, error) { return ReadFileLimit(p, 0) }

// ReadFileLimit reads a file of at most limit bytes when limit is above
// 0, failing with remote.ErrTooLarge for a larger one.
func ReadFileLimit(p string, limit int64) ([]byte, error) {
	if c, rp, ok := remote.Lookup(p); ok {
		return c.ReadFile(rp, limit)
	}
	if limit > 0 {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if info.Size() > limit {
			return nil, remote.ErrTooLarge
		}
	}
	return os.ReadFile(p)
}

// ReadFileInfo stats a file and reads it, as Stat and ReadFileLimit do,
// at one round trip for one of a host.
func ReadFileInfo(p string, limit int64) (fs.FileInfo, []byte, error) {
	if c, rp, ok := remote.Lookup(p); ok {
		info, data, err := c.ReadFileInfo(rp, limit)
		if err != nil {
			return nil, nil, err
		}
		return info, data, nil
	}
	info, err := os.Stat(p)
	if err != nil {
		return nil, nil, err
	}
	if info.IsDir() {
		return nil, nil, &fs.PathError{Op: "read", Path: p, Err: errors.New("is a directory")}
	}
	data, err := ReadFileLimit(p, limit)
	if err != nil {
		return nil, nil, err
	}
	return info, data, nil
}

// WriteFile writes data to a file, made with perm when it is new, and
// returns what stat says of it then. A file of this machine is written
// beside and renamed into place, so that a failure keeps the old one; one
// of a host as remote.Conn.WriteFile says.
func WriteFile(p string, data []byte, perm fs.FileMode) (fs.FileInfo, error) {
	if c, rp, ok := remote.Lookup(p); ok {
		return wrap(c.WriteFile(rp, data))
	}
	mode := perm
	if info, err := os.Stat(p); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".mayoi-*")
	if err != nil {
		if err := os.WriteFile(p, data, mode); err != nil {
			return nil, err
		}
		return os.Stat(p)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}
	os.Chmod(tmp.Name(), mode)
	if err := os.Rename(tmp.Name(), p); err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}
	return os.Stat(p)
}

// Create makes an empty file, and the folders above it, unless a file is
// there.
func Create(p string) error {
	if c, rp, ok := remote.Lookup(p); ok {
		return c.Create(rp)
	}
	if _, err := os.Lstat(p); err == nil {
		return &fs.PathError{Op: "create", Path: p, Err: fs.ErrExist}
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, nil, 0o644)
}

// MkdirAll makes a folder and those above it.
func MkdirAll(p string) error {
	if c, rp, ok := remote.Lookup(p); ok {
		return c.MkdirAll(rp)
	}
	return os.MkdirAll(p, 0o755)
}

// Rename moves a file, unless another is at to.
func Rename(from, to string) error {
	if c, rp, ok := remote.Lookup(from); ok {
		_, rto, ok := remote.Split(to)
		if !ok || remote.HostOf(to) != c.Host {
			return &fs.PathError{Op: "rename", Path: to, Err: fs.ErrInvalid}
		}
		return c.Rename(rp, rto)
	}
	if from != to {
		if _, err := os.Lstat(to); err == nil {
			return &fs.PathError{Op: "rename", Path: to, Err: fs.ErrExist}
		}
	}
	return os.Rename(from, to)
}

// RemoveAll deletes a file, or a folder and everything in it.
func RemoveAll(p string) error {
	if c, rp, ok := remote.Lookup(p); ok {
		return c.Remove(rp)
	}
	return os.RemoveAll(p)
}

// Entry is a file of a folder: its name, and whether it is a folder or a
// link to one.
type Entry struct {
	Name string
	Dir  bool
}

// ReadDir lists a folder.
func ReadDir(dir string) ([]Entry, error) {
	if c, rp, ok := remote.Lookup(dir); ok {
		es, err := c.ReadDir(rp)
		if err != nil {
			return nil, err
		}
		out := make([]Entry, len(es))
		for i, e := range es {
			out[i] = Entry{Name: e.Name, Dir: e.Dir}
		}
		return out, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(dir, e.Name())); err == nil && info.IsDir() {
				isDir = true
			}
		}
		out = append(out, Entry{Name: e.Name(), Dir: isDir})
	}
	return out, nil
}

// ReadDirs lists folders, at one round trip for those of a host: each
// folder's entries, or why it could not be read. It fails when a host
// cannot be asked.
func ReadDirs(dirs []string) (map[string][]Entry, map[string]error, error) {
	lists, errs := map[string][]Entry{}, map[string]error{}
	byConn := map[*remote.Conn][]string{}
	for _, d := range dirs {
		if c, rp, ok := remote.Lookup(d); ok {
			byConn[c] = append(byConn[c], rp)
			continue
		}
		if es, err := ReadDir(d); err != nil {
			errs[d] = err
		} else {
			lists[d] = es
		}
	}
	for c, rps := range byConn {
		got, err := c.ReadDirs(rps)
		if err != nil {
			return lists, errs, err
		}
		for rp, l := range got {
			p := remote.Path(c.Host, rp)
			if l.Err != nil {
				errs[p] = l.Err
				continue
			}
			es := make([]Entry, len(l.Entries))
			for i, e := range l.Entries {
				es[i] = Entry{Name: e.Name, Dir: e.Dir}
			}
			lists[p] = es
		}
	}
	return lists, errs, nil
}

// Look lists folders and stats files, as ReadDirs and StatMany do, at one
// round trip for those of one host.
func Look(dirs, files []string) (map[string][]Entry, map[string]error, map[string]fs.FileInfo, error) {
	var c *remote.Conn
	same := true
	for _, p := range append(slices.Clone(dirs), files...) {
		pc, _, ok := remote.Lookup(p)
		if !ok || c != nil && pc != c {
			same = false
			break
		}
		c = pc
	}
	if !same || c == nil {
		lists, errs, err := ReadDirs(dirs)
		if err != nil {
			return nil, nil, nil, err
		}
		infos, err := StatMany(files)
		return lists, errs, infos, err
	}
	rdirs, rfiles := make([]string, len(dirs)), make([]string, len(files))
	for i, d := range dirs {
		_, rdirs[i], _ = remote.Split(d)
	}
	for i, f := range files {
		_, rfiles[i], _ = remote.Split(f)
	}
	got, stats, err := c.Look(rdirs, rfiles)
	if err != nil {
		return nil, nil, nil, err
	}
	lists, errs, infos := map[string][]Entry{}, map[string]error{}, map[string]fs.FileInfo{}
	for rp, l := range got {
		p := remote.Path(c.Host, rp)
		if l.Err != nil {
			errs[p] = l.Err
			continue
		}
		es := make([]Entry, len(l.Entries))
		for i, e := range l.Entries {
			es[i] = Entry{Name: e.Name, Dir: e.Dir}
		}
		lists[p] = es
	}
	for rp, info := range stats {
		infos[remote.Path(c.Host, rp)] = info
	}
	return lists, errs, infos, nil
}

// Command returns the command that runs a program in dir, on the machine
// of dir, with env added to its environment and stdin, nil for none, as
// its input. The caller sets its output.
func Command(dir string, env []string, stdin io.Reader, name string, args ...string) (*exec.Cmd, error) {
	if c, rp, ok := remote.Lookup(dir); ok {
		cargs := append([]string{rp}, env...)
		cargs = append(cargs, name)
		cargs = append(cargs, args...)
		// The arguments: the folder, then the environment's, then the
		// program's.
		script := `cd -- "$1" || exit 1; shift
exec env "$@"`
		return c.Command(stdin, script, cargs...)
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	hideWindow(cmd)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	if stdin != nil {
		cmd.Stdin = stdin
	}
	return cmd, nil
}

// Output runs a program as Command has it and returns what it wrote.
func Output(dir string, name string, args ...string) ([]byte, error) {
	cmd, err := Command(dir, nil, nil, name, args...)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	return out.Bytes(), err
}
