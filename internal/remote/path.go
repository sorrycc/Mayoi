// Package remote reaches folders on other machines over SSH, with the
// system's ssh command and nothing installed on the other side: one
// connection to each host, which every command shares, and the files read
// and written by small shell scripts run through it.
//
// The editor knows a remote file by a path of its own, /ssh:host/path,
// which the path functions of Go take as any other: the folder of a file,
// its name and paths relative to a folder come out as they should.
package remote

import (
	"errors"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// prefix starts the paths of remote files.
const prefix = "/ssh:"

// Path returns the path the editor knows the file at p on host by.
func Path(host, p string) string {
	p = path.Clean("/" + p)
	if p == "/" {
		return prefix + host
	}
	return prefix + host + p
}

// Split returns the host of a remote path and the path on the host,
// false for a path of this machine.
func Split(p string) (host, rpath string, ok bool) {
	rest, ok := strings.CutPrefix(filepath.ToSlash(p), prefix)
	if !ok || rest == "" {
		return "", "", false
	}
	host, rpath, _ = strings.Cut(rest, "/")
	if host == "" {
		return "", "", false
	}
	return host, path.Clean("/" + rpath), true
}

// Is reports whether p is the path of a remote file.
func Is(p string) bool {
	_, _, ok := Split(p)
	return ok
}

// HostOf returns the host of a remote path, "" for a path of this
// machine.
func HostOf(p string) string {
	host, _, _ := Split(p)
	return host
}

// On returns p, a path a program on the machine of ref wrote, as the
// editor knows it: as ref's host has it when ref is remote.
func On(ref, p string) string {
	if host, _, ok := Split(ref); ok && strings.HasPrefix(p, "/") && !Is(p) {
		return Path(host, p)
	}
	return p
}

// Display is how a remote path shows, as ssh's scp-like syntax spells it:
// host:/path. A path of this machine shows as it is.
func Display(p string) string {
	host, rpath, ok := Split(p)
	if !ok {
		return p
	}
	return host + ":" + rpath
}

// URL returns the ssh:// URL of a remote path, "" for a path of this
// machine.
func URL(p string) string {
	host, rpath, ok := Split(p)
	if !ok {
		return ""
	}
	return "ssh://" + host + rpath
}

// ParseURL returns the path of the folder an ssh://[user@]host[:port]/path
// URL names.
func ParseURL(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "ssh" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("use ssh://[user@]host[:port]/path")
	}
	if _, ok := u.User.Password(); ok {
		return "", errors.New("passwords go in the SSH agent or ~/.ssh/config, not in the URL")
	}
	host := u.Host
	if u.User != nil {
		host = u.User.Username() + "@" + host
	}
	if err := CheckHost(host); err != nil {
		return "", err
	}
	return Path(host, u.Path), nil
}

// CheckHost returns why host cannot name a machine to connect to: a name
// of ~/.ssh/config, or [user@]host[:port].
func CheckHost(host string) error {
	switch {
	case host == "":
		return errors.New("enter a host")
	case strings.HasPrefix(host, "-"):
		return errors.New("a host cannot start with -")
	case strings.ContainsAny(host, "/\\\x00\r\n\t '\"`$;&|<>(){}*?!#"):
		return errors.New("a host has no spaces, slashes or quotes")
	}
	if _, port := splitPort(host); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return errors.New("the port is a number from 1 to 65535")
		}
	}
	return nil
}

// splitPort parts [user@]host[:port] at its port, "" for none. A host of
// IPv6 is in brackets, as [::1]:22.
func splitPort(host string) (string, string) {
	if i := strings.LastIndexByte(host, ']'); i >= 0 {
		if rest, ok := strings.CutPrefix(host[i+1:], ":"); ok {
			return host[:i+1], rest
		}
		return host, ""
	}
	if strings.Count(host, ":") == 1 {
		h, port, _ := strings.Cut(host, ":")
		return h, port
	}
	return host, ""
}

// hostArgs are the arguments of ssh that name host.
func hostArgs(host string) []string {
	h, port := SSHTarget(host)
	if port != "" {
		return []string{"-p", port, h}
	}
	return []string{h}
}

// SSHTarget is host as ssh takes it, [user@]host with no brackets, and
// its port, "" for none.
func SSHTarget(host string) (target, port string) {
	h, port := splitPort(host)
	return strings.NewReplacer("[", "", "]", "").Replace(h), port
}

// quote quotes s for the POSIX shell.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
