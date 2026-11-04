package git

import (
	"path"
	"slices"
	"strings"
)

// lockfiles are the files of a package manager's resolved versions, whose
// changes say little and take much room: a message diff counts them, but
// leaves their lines out.
var lockfiles = []string{
	"package-lock.json", "npm-shrinkwrap.json", "yarn.lock", "pnpm-lock.yaml", "bun.lock", "bun.lockb",
	"go.sum", "Cargo.lock", "Gemfile.lock", "composer.lock", "poetry.lock", "uv.lock", "Pipfile.lock",
	"pubspec.lock", "Podfile.lock", "flake.lock",
}

// secretFiles are the patterns of the names of files that hold secrets, as
// keys and .env files: a message diff names them when they are new, but
// leaves their lines out, as these would leave the machine.
var secretFiles = []string{
	".env", ".env.*", "*.pem", "*.key", "*.p12", "*.pfx", "*.keystore", "*.jks",
	"id_rsa*", "id_dsa*", "id_ecdsa*", "id_ed25519*", "*.secret", "credentials*.json",
	".netrc", ".npmrc", ".pgpass",
}

// secret reports whether the file at name may hold secrets.
func secret(name string) bool {
	base := path.Base(name)
	return slices.ContainsFunc(secretFiles, func(p string) bool {
		ok, _ := path.Match(p, base)
		return ok
	})
}

// maxUntracked is how many new files a message diff shows the lines of.
const maxUntracked = 20

// MessageDiff returns the changes a commit message is to describe, as a
// diff after a summary of its files: the staged changes, or every change
// when none is staged, the new files' included; with amend, the last
// commit's and the staged ones. staged reports whether it is what is
// staged. The lines of lockfiles and of new files that may hold secrets are
// left out, and the diff is cut to
// about max bytes. It is "" when nothing changed.
func (r *Repo) MessageDiff(amend bool, max int) (diff string, staged bool, err error) {
	base := "HEAD"
	if amend {
		base = "HEAD^"
	}
	if _, err := r.run(nil, "rev-parse", "--verify", "-q", base+"^{commit}"); err != nil {
		// Before the first commit, or amending it: against nothing.
		out, err := r.run([]byte{}, "hash-object", "-t", "tree", "--stdin")
		if err != nil {
			return "", false, err
		}
		base = strings.TrimSpace(string(out))
	}
	staged = amend
	if !staged {
		out, err := r.run(nil, "diff", "--cached", "--name-only", "-z", base, "--")
		if err != nil {
			return "", false, err
		}
		staged = len(out) > 0
	}
	args := func(opts ...string) []string {
		a := append([]string{"diff", "--no-color", "--no-ext-diff", "-M"}, opts...)
		if staged {
			a = append(a, "--cached")
		}
		return append(a, base, "--")
	}
	stat, err := r.run(nil, args("--stat=120")...)
	if err != nil {
		return "", staged, err
	}
	excludes := make([]string, len(lockfiles))
	for i, f := range lockfiles {
		excludes[i] = ":(exclude,glob)**/" + f
	}
	patch, err := r.run(nil, append(args(), excludes...)...)
	if err != nil {
		return "", staged, err
	}
	var b strings.Builder
	b.Write(stat)
	if len(stat) > 0 {
		b.WriteString("\n")
	}
	b.Write(patch)
	if !staged {
		r.writeUntracked(&b)
	}
	return cut(b.String(), max), staged, nil
}

// writeUntracked writes the new files that Git does not ignore to b: their
// names, and the lines of the first of them as a diff, but of lockfiles and
// of files that may hold secrets.
func (r *Repo) writeUntracked(b *strings.Builder) {
	out, err := r.run(nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil || len(out) == 0 {
		return
	}
	names := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	b.WriteString("\nNew files:\n")
	for _, n := range names {
		b.WriteString(" " + n + "\n")
	}
	shown := 0
	for _, n := range names {
		if shown == maxUntracked {
			break
		}
		if slices.Contains(lockfiles, path.Base(n)) || secret(n) {
			continue
		}
		// It exits with 1 as the files differ, which they always do.
		d, _ := r.run(nil, "diff", "--no-index", "--no-color", "--no-ext-diff", "--", "/dev/null", n)
		b.Write(d)
		shown++
	}
}

// cut cuts s to at most max bytes, at the end of a line, saying so.
func cut(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	s = s[:max]
	if i := strings.LastIndexByte(s, '\n'); i > 0 {
		s = s[:i+1]
	}
	return s + "[… the rest of the diff is left out]\n"
}
