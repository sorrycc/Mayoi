// Package remote reaches folders on other machines over SSH, with the
// system's ssh command and nothing installed on the other side: one
// connection to each host, which every command shares, and the files read
// and written by small shell scripts run through it.
//
// The editor knows a remote file by a path of its own, /ssh:host/path or
// /wsl:distro/path. On Windows those are not absolute to filepath, so
// code that may see one uses IsAbs, Join, Dir, Clean, Rel and Contains.
package remote

import (
	"errors"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// sshPrefix and wslPrefix start the paths of remote files. The name is
// the host, or the WSL distribution, up to the next slash.
const (
	sshPrefix = "/ssh:"
	wslPrefix = "/wsl:"
)

// prefix is the SSH form. Callers that build a path use Path.
const prefix = sshPrefix

// Kind is which machine a remote path names.
type Kind int

const (
	// KindSSH is a folder reached with the system's ssh.
	KindSSH Kind = iota
	// KindWSL is a folder in a WSL distribution on this Windows machine.
	KindWSL
)

// Path returns the path the editor knows the file at p on host by.
func Path(host, p string) string { return pathOf(sshPrefix, host, p) }

// WSLPath returns the path of p inside a WSL distribution.
func WSLPath(distro, p string) string { return pathOf(wslPrefix, distro, p) }

func pathOf(prefix, name, p string) string {
	p = path.Clean("/" + filepath.ToSlash(p))
	if p == "/" {
		return prefix + name
	}
	return prefix + name + p
}

// Split returns the host or distribution of a remote path and the path
// on that machine, false for a path of this machine.
func Split(p string) (name, rpath string, ok bool) {
	_, name, rpath, ok = splitKind(p)
	return name, rpath, ok
}

// KindOf reports whether p is an SSH host or a WSL distribution.
func KindOf(p string) (Kind, bool) {
	kind, _, _, ok := splitKind(p)
	return kind, ok
}

func splitKind(p string) (kind Kind, name, rpath string, ok bool) {
	slash := filepath.ToSlash(p)
	var rest string
	switch {
	case strings.HasPrefix(slash, sshPrefix):
		kind, rest = KindSSH, slash[len(sshPrefix):]
	case strings.HasPrefix(slash, wslPrefix):
		kind, rest = KindWSL, slash[len(wslPrefix):]
	default:
		return 0, "", "", false
	}
	if rest == "" {
		return 0, "", "", false
	}
	name, rpath, _ = strings.Cut(rest, "/")
	if name == "" {
		return 0, "", "", false
	}
	return kind, name, path.Clean("/" + rpath), true
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
// editor knows it: as ref's host or distribution has it when ref is remote.
func On(ref, p string) string {
	kind, name, _, ok := splitKind(ref)
	if ok && strings.HasPrefix(p, "/") && !Is(p) {
		if kind == KindWSL {
			return WSLPath(name, p)
		}
		return Path(name, p)
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

// IsAbs reports whether p is a remote path or an absolute path of this
// machine. A remote path is absolute even where filepath.IsAbs is not,
// which is Windows.
func IsAbs(p string) bool {
	return Is(p) || filepath.IsAbs(p)
}

// Join joins elements. A remote first element stays a slash path of that
// host or distribution. Anything else is joined as a path of this machine.
func Join(elem ...string) string {
	if len(elem) == 0 {
		return ""
	}
	if kind, name, rpath, ok := splitKind(elem[0]); ok {
		parts := make([]string, 1, len(elem))
		parts[0] = rpath
		for _, e := range elem[1:] {
			parts = append(parts, filepath.ToSlash(e))
		}
		joined := path.Join(parts...)
		if kind == KindWSL {
			return pathOf(wslPrefix, name, joined)
		}
		return pathOf(sshPrefix, name, joined)
	}
	return filepath.Join(elem...)
}

// Dir returns the folder of p. The root of a host or distribution stays
// that root.
func Dir(p string) string {
	kind, name, rpath, ok := splitKind(p)
	if !ok {
		return filepath.Dir(p)
	}
	d := path.Dir(rpath)
	if kind == KindWSL {
		return pathOf(wslPrefix, name, d)
	}
	return pathOf(sshPrefix, name, d)
}

// Clean returns the shortest equivalent of p. A remote path stays remote.
func Clean(p string) string {
	kind, name, rpath, ok := splitKind(p)
	if !ok {
		return filepath.Clean(p)
	}
	if kind == KindWSL {
		return pathOf(wslPrefix, name, rpath)
	}
	return pathOf(sshPrefix, name, rpath)
}

// Rel returns the path of target relative to base, with slashes when
// either is remote. The two must name the same host or distribution.
func Rel(base, target string) (string, error) {
	bk, bn, br, bok := splitKind(base)
	tk, tn, tr, tok := splitKind(target)
	if bok || tok {
		if !bok || !tok || bk != tk || bn != tn {
			return "", errors.New("paths are on different machines")
		}
		return slashRel(br, tr)
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// slashRel is path.Rel for slash paths. The path package has no Rel.
func slashRel(base, target string) (string, error) {
	base, target = path.Clean(base), path.Clean(target)
	if base == target {
		return ".", nil
	}
	bparts, tparts := splitSlash(base), splitSlash(target)
	i := 0
	for i < len(bparts) && i < len(tparts) && bparts[i] == tparts[i] {
		i++
	}
	out := make([]string, 0, len(bparts)-i+len(tparts)-i)
	for n := i; n < len(bparts); n++ {
		out = append(out, "..")
	}
	out = append(out, tparts[i:]...)
	if len(out) == 0 {
		return ".", nil
	}
	return path.Join(out...), nil
}

func splitSlash(p string) []string {
	if p == "/" {
		return nil
	}
	return strings.Split(strings.Trim(p, "/"), "/")
}

// Contains reports whether p is root or a file inside it. A remote root
// matches on slashes, including on Windows.
func Contains(root, p string) bool {
	if root == "" {
		return false
	}
	if Is(root) || Is(p) {
		root, p = filepath.ToSlash(Clean(root)), filepath.ToSlash(Clean(p))
		return p == root || strings.HasPrefix(p, root+"/")
	}
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
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
