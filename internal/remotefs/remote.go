// Package remotefs routes workspace IO through OpenSSH/SFTP for remote paths.
// No remote agent, private key handling, or local workspace mirror is needed.
package remotefs

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
)

// Virtual paths remain filepath-compatible and keep remote and local files
// distinct in documents, settings and Git. They are never written locally.
const prefix = "/__mayoi_ssh__/"

type Target struct{ Host, Port string }

func Parse(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "ssh://") {
		return "", errors.New("Use ssh://[user@]host[:port]/absolute/path")
	}
	u, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if u.Scheme != "ssh" || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("Invalid SSH URL")
	}
	host := u.Hostname()
	if u.User != nil {
		if _, password := u.User.Password(); password {
			return "", errors.New("Use SSH Agent or ~/.ssh/config; do not put passwords in the URL")
		}
		host = u.User.Username() + "@" + host
	}
	if strings.HasPrefix(host, "-") || strings.ContainsAny(host, "\x00\r\n\t /\\") {
		return "", errors.New("Invalid SSH host")
	}
	port := u.Port()
	if port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return "", errors.New("Invalid SSH port")
		}
	}
	p := u.Path
	if p == "" {
		p = "/"
	}
	if !strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) {
		return "", errors.New("An absolute remote path is required")
	}
	t := Target{host, port}
	return t.Virtual(p), nil
}
func (t Target) Virtual(p string) string {
	key := base64.RawURLEncoding.EncodeToString([]byte(t.Host + "\n" + t.Port))
	return prefix + key + filepath.ToSlash(filepath.Clean("/"+p))
}
func Split(p string) (Target, string, bool) {
	rest, ok := strings.CutPrefix(filepath.ToSlash(p), prefix)
	if !ok {
		return Target{}, p, false
	}
	key, path, _ := strings.Cut(rest, "/")
	raw, err := base64.RawURLEncoding.DecodeString(key)
	if err != nil {
		return Target{}, p, false
	}
	host, port, ok := strings.Cut(string(raw), "\n")
	if !ok || host == "" {
		return Target{}, p, false
	}
	return Target{host, port}, "/" + path, true
}
func IsRemote(p string) bool { return strings.HasPrefix(filepath.ToSlash(p), prefix) }
func Display(p string) string {
	t, r, ok := Split(p)
	if !ok {
		return p
	}
	host := t.Host
	// Rebuild IPv6 authorities without confusing the username or port.
	user, h, hasUser := strings.Cut(host, "@")
	if !hasUser {
		h = host
		user = ""
	}
	if strings.Contains(h, ":") {
		h = "[" + h + "]"
	}
	u := &url.URL{Scheme: "ssh", Host: h, Path: r}
	if user != "" {
		u.User = url.User(user)
	}
	if t.Port != "" {
		u.Host += ":" + t.Port
	}
	return u.String()
}
func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

var controlOnce sync.Once
var controlDir string

func (t Target) controlArgs() []string {
	if runtime.GOOS == "windows" {
		return nil
	}
	controlOnce.Do(func() {
		// macOS's default temp path can exceed Unix socket path limits.
		base := ""
		if runtime.GOOS == "darwin" {
			base = "/tmp"
		}
		controlDir, _ = os.MkdirTemp(base, "mayoi-ssh-")
	})
	if controlDir == "" {
		return nil
	}
	key := sha256.Sum256([]byte(t.Host + "\n" + t.Port))
	return []string{"-oControlMaster=auto", "-oControlPersist=no", "-oControlPath=" + filepath.Join(controlDir, fmt.Sprintf("%x", key[:12]))}
}
func (t Target) args() []string {
	a := []string{"-oBatchMode=yes", "-oStrictHostKeyChecking=yes", "-oConnectTimeout=10", "-oServerAliveInterval=5", "-oServerAliveCountMax=3"}
	if t.Port != "" {
		a = append(a, "-p", t.Port)
	}
	return append(a, t.controlArgs()...)
}
func Terminal(p string) (string, []string) {
	t, r, ok := Split(p)
	if !ok {
		return "", nil
	}
	// Keep terminal authentication interactive; use the user's OpenSSH config.
	args := []string{"-t", "-oConnectTimeout=10", "-oServerAliveInterval=15", "-oServerAliveCountMax=3"}
	if t.Port != "" {
		args = append(args, "-p", t.Port)
	}
	args = append(args, t.controlArgs()...)
	return "ssh", append(args, "--", t.Host, "cd "+Quote(r)+" && exec \"${SHELL:-/bin/sh}\" -l")
}

type connection struct {
	client *sftp.Client
	cmd    *exec.Cmd
}

var connections = struct {
	sync.Mutex
	clients map[Target]*connection
}{clients: map[Target]*connection{}}

func clientFor(p string) (*sftp.Client, string, error) {
	t, r, ok := Split(p)
	if !ok {
		return nil, "", errors.New("Invalid remote path")
	}
	connections.Lock()
	defer connections.Unlock()
	if c := connections.clients[t]; c != nil {
		return c.client, r, nil
	}
	return nil, r, fmt.Errorf("SSH is disconnected. Use File → Reconnect SSH (%s)", t.Host)
}

// Connect is called off the UI thread. Existing connections are shared by windows.
func Connect(p string) error {
	t, _, ok := Split(p)
	if !ok {
		return errors.New("Invalid remote path")
	}
	connections.Lock()
	existing := connections.clients[t]
	connections.Unlock()
	if existing != nil {
		return nil
	}
	cmd := exec.Command("ssh", append(t.args(), "-T", "-s", "--", t.Host, "sftp")...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	timer := time.AfterFunc(20*time.Second, func() { _ = cmd.Process.Kill() })
	c, err := sftp.NewClientPipe(stdout, stdin)
	timer.Stop()
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("SSH: %s (%w). Verify this host with ssh in a terminal first; SFTP must be enabled", strings.TrimSpace(stderr.String()), err)
	}
	conn := &connection{c, cmd}
	connections.Lock()
	if connections.clients[t] != nil {
		connections.Unlock()
		_ = c.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil
	}
	connections.clients[t] = conn
	connections.Unlock()
	go func() {
		_ = c.Wait()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		connections.Lock()
		if connections.clients[t] == conn {
			delete(connections.clients, t)
		}
		connections.Unlock()
	}()
	return nil
}
func CloseAll() {
	connections.Lock()
	defer connections.Unlock()
	for t, c := range connections.clients {
		_ = c.cmd.Process.Kill()
		delete(connections.clients, t)
	}
}
func Connected(p string) bool {
	t, _, ok := Split(p)
	if !ok {
		return false
	}
	connections.Lock()
	defer connections.Unlock()
	return connections.clients[t] != nil
}
func Stat(p string) (os.FileInfo, error) {
	if !IsRemote(p) {
		return os.Stat(p)
	}
	c, r, e := clientFor(p)
	if e != nil {
		return nil, e
	}
	return c.Stat(r)
}
func Lstat(p string) (os.FileInfo, error) {
	if !IsRemote(p) {
		return os.Lstat(p)
	}
	c, r, e := clientFor(p)
	if e != nil {
		return nil, e
	}
	return c.Lstat(r)
}
func ReadFile(p string) ([]byte, error) {
	if !IsRemote(p) {
		return os.ReadFile(p)
	}
	c, r, e := clientFor(p)
	if e != nil {
		return nil, e
	}
	f, e := c.Open(r)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return io.ReadAll(f)
}
func ReadDir(p string) ([]fs.DirEntry, error) {
	if !IsRemote(p) {
		return os.ReadDir(p)
	}
	c, r, e := clientFor(p)
	if e != nil {
		return nil, e
	}
	infos, e := c.ReadDir(r)
	if e != nil {
		return nil, e
	}
	entries := make([]fs.DirEntry, 0, len(infos))
	for _, i := range infos {
		entries = append(entries, fs.FileInfoToDirEntry(i))
	}
	return entries, nil
}
func MkdirAll(p string, mode os.FileMode) error {
	if !IsRemote(p) {
		return os.MkdirAll(p, mode)
	}
	c, r, e := clientFor(p)
	if e != nil {
		return e
	}
	return c.MkdirAll(r)
}
func Rename(from, to string) error {
	if !IsRemote(from) && !IsRemote(to) {
		return os.Rename(from, to)
	}
	t, r, ok := Split(to)
	src, _, _ := Split(from)
	if !ok || src != t {
		return errors.New("Cannot move files between SSH hosts or local storage")
	}
	c, f, e := clientFor(from)
	if e != nil {
		return e
	}
	return c.Rename(f, r)
}

// WriteFile uses a sibling temporary file and atomic POSIX rename. If the
// server cannot replace atomically, fail without deleting the original.
func WriteFile(p string, data []byte, mode os.FileMode) error {
	if !IsRemote(p) {
		return os.WriteFile(p, data, mode)
	}
	c, r, e := clientFor(p)
	if e != nil {
		return e
	}
	if _, e = c.Lstat(r); e == nil {
		if r, e = c.RealPath(r); e != nil {
			return e
		}
	}
	tmp := r + ".mayoi-" + rand.Text()
	f, e := c.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if e != nil {
		return e
	}
	defer c.Remove(tmp)
	_, e = f.Write(data)
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if e = c.Chmod(tmp, mode); e != nil {
		return e
	}
	if _, ok := c.HasExtension("posix-rename@openssh.com"); ok {
		return c.PosixRename(tmp, r)
	}
	if _, e = c.Lstat(r); !errors.Is(e, os.ErrNotExist) {
		return errors.New("SFTP server does not support atomic overwrite; original file was preserved")
	}
	return c.Rename(tmp, r)
}

// GitCommand runs on the workspace's machine. Remote commands have a bounded
// lifetime and all arguments are individually shell-quoted.
func GitCommand(dir string, args ...string) (*exec.Cmd, func()) {
	t, r, remote := Split(dir)
	if !remote {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		return cmd, func() {}
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = Quote(a)
	}
	script := "cd " + Quote(r) + " && env GIT_TERMINAL_PROMPT=0 GIT_OPTIONAL_LOCKS=0 LC_ALL=C GIT_SSH_COMMAND=" + Quote("ssh -oBatchMode=yes") + " git " + strings.Join(quoted, " ")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	cmd := exec.CommandContext(ctx, "ssh", append(t.args(), "-T", "--", t.Host, script)...)
	return cmd, cancel
}

// MapAbsolute maps absolute paths emitted by remote Git back into its namespace.
func MapAbsolute(base, p string) string {
	t, _, ok := Split(base)
	if ok && filepath.IsAbs(p) {
		return t.Virtual(p)
	}
	return p
}

// Home returns the login directory reported by the SFTP server on this host.
func Home(p string) (string, error) {
	c, _, err := clientFor(p)
	if err != nil {
		return "", err
	}
	dir, err := c.Getwd()
	if err != nil {
		return "", err
	}
	target, _, _ := Split(p)
	return target.Virtual(dir), nil
}

// ReadFileLimit reads a regular remote file without allowing growth beyond limit.
func ReadFileLimit(p string, limit int64) ([]byte, error) {
	c, path, err := clientFor(p)
	if err != nil {
		return nil, err
	}
	info, err := c.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("File exceeds search limits")
	}
	f, err := c.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("File exceeds search limits")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("File exceeds search limits")
	}
	return data, err
}
