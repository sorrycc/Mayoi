package git

import (
	"path"
	"regexp"
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

// secretFiles are the patterns of the files that hold secrets, as keys
// and .env files: a message diff names them, but leaves their lines out,
// as these would leave the machine. A pattern without "/" is of a file's
// name, one with "/" of the end of its path, and one ending in "/**" of a
// folder anywhere in it; "!" before a pattern takes back what matched
// before it, as .gitignore does, so the last pattern that matches decides.
// The templates of such files, which name the settings and not their
// values, go along.
var secretFiles = []string{
	".env*", "*.env", "secret.*", "secrets.*", "*.secret", "*.secrets",
	"credentials", "credentials.*", "*credentials*.json", "service-account*.json", "service_account*.json",
	"*.pem", "*.key", "*.p12", "*.pfx", "*.keystore", "*.jks", "*.ppk",
	"id_rsa*", "id_dsa*", "id_ecdsa*", "id_ed25519*",
	"*.tfvars", "*.tfvars.json", "*.gpg", "*.pgp", "*.asc", "*.ovpn", "*.kdbx",
	".netrc", ".npmrc", ".pgpass", ".htpasswd", ".git-credentials", ".pypirc", ".s3cfg", ".boto",
	"kubeconfig", "*.kubeconfig", "master.key", "wallet.dat", "token", "*.token",
	".ssh/**", ".aws/**", ".gnupg/**", ".kube/**", ".config/gcloud/**", ".docker/config.json",
	"!.env.example", "!.env.sample", "!.env.template", "!.env.dist", "!.env.schema",
	"!*.example", "!*.sample", "!*.template", "!*.dist",
}

// secretIn reports whether the file at name, a path with "/", may hold
// secrets: by secretFiles, then by the patterns of more, which win.
func secretIn(name string, more []string) bool {
	hit := false
	for _, list := range [][]string{secretFiles, more} {
		for _, p := range list {
			p, not := strings.CutPrefix(p, "!")
			if p = strings.TrimSpace(p); p == "" {
				continue
			}
			if matchPath(p, name) {
				hit = !not
			}
		}
	}
	return hit
}

// matchPath reports whether the file at name matches pattern, as
// secretFiles says.
func matchPath(pattern, name string) bool {
	if !strings.Contains(pattern, "/") {
		ok, _ := path.Match(pattern, path.Base(name))
		return ok
	}
	dir, deep := strings.CutSuffix(pattern, "/**")
	segs := strings.Split(name, "/")
	for i := range segs {
		if deep {
			// The folder is any run of segments but the file's.
			for j := i + 1; j < len(segs); j++ {
				if ok, _ := path.Match(dir, strings.Join(segs[i:j], "/")); ok {
					return true
				}
			}
		} else if ok, _ := path.Match(pattern, strings.Join(segs[i:], "/")); ok {
			return true
		}
	}
	return false
}

// secretText matches what is a secret wherever it is: private keys and
// the tokens of AWS, GitHub, OpenAI and Anthropic, Stripe, Slack and
// Google, a JWT, a URL with a password, and a bearer token.
var secretText = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY( BLOCK)?-----`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}\b`),
	regexp.MustCompile(`\bsk-(ant-)?[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\b[sr]k_(live|test)_[A-Za-z0-9]{16,}\b`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.`),
	regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^/\s:@]+:[^@\s]{4,}@`),
	regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9+/=_.-]{20,}`),
}

// secretAssign matches a setting of a secret's name given a value, which
// is one unless it is a placeholder or code (see secretValue).
var secretAssign = regexp.MustCompile(
	`(?i)(api[_-]?key|secret|token|passw(or)?d|private[_-]?key|client[_-]?secret|access[_-]?key)["']?\s*[:=]\s*["']?([^\s"',;]{8,})`)

// secretPlaceholder is a value that stands for a secret rather than is
// one: xxx, ***, changeme, <your key>, ${KEY}, $KEY, your-key, example…
var secretPlaceholder = regexp.MustCompile(
	`(?i)^(x+|\*+|\.+|changeme|change_me|placeholder|redacted|dummy|none|null|undefined|true|false|<.*>?|\$\{.*\}?|\$[A-Z_]+|%[A-Z_]+%|your[_-].*|example.*|sample.*)$`)

// secretValue reports whether v, the value of a setting of a secret, is
// one: not a placeholder, and not code that reads it from elsewhere.
func secretValue(v string) bool {
	if secretPlaceholder.MatchString(v) || strings.ContainsAny(v, "()[]{}") {
		return false
	}
	for _, code := range []string{"os.", "process.", "env.", "getenv", "ENV", "System.", "config.", "settings.", "self.", "this."} {
		if strings.HasPrefix(v, code) {
			return false
		}
	}
	return true
}

// secretLines reports whether the lines of a diff hold what looks like a
// secret.
func secretLines(body string) bool {
	for line := range strings.Lines(body) {
		if strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- ") {
			continue
		}
		for _, re := range secretText {
			if re.MatchString(line) {
				return true
			}
		}
		if m := secretAssign.FindStringSubmatch(line); m != nil && secretValue(m[3]) {
			return true
		}
	}
	return false
}

// secretNote is what a message diff says in place of the lines of a file
// that may hold secrets.
const secretNote = "[lines left out: the file may hold secrets]\n"

// redactPatch returns patch, a diff of files, with the lines of each file
// that may hold secrets, by its name as secretIn says with more or by
// what they say, replaced by secretNote.
func redactPatch(patch string, more []string) string {
	const head = "diff --git "
	if !strings.HasPrefix(patch, head) {
		return patch
	}
	var b strings.Builder
	sections := strings.Split(patch[len(head):], "\n"+head)
	for i, sec := range sections {
		if i < len(sections)-1 {
			sec += "\n" // what the split took
		}
		header, body, _ := strings.Cut(sec, "\n")
		b.WriteString(head + header + "\n")
		if secretIn(patchPath(header, body), more) || secretLines(body) {
			b.WriteString(secretNote)
		} else {
			b.WriteString(body)
		}
	}
	return b.String()
}

// patchPath is the path of the file a section of a diff is of: of its
// +++ line, or its --- line for a file deleted, else of the header after
// " b/", as of a file renamed or binary. Git quotes a path with odd
// characters, which is left as it is but for the quotes.
func patchPath(header, body string) string {
	unquote := func(p string) string { return strings.Trim(strings.TrimRight(p, "\n"), `"`) }
	var old string
	for line := range strings.Lines(body) {
		switch {
		case strings.HasPrefix(line, "--- a/"):
			old = unquote(line[len("--- a/"):])
		case strings.HasPrefix(line, "+++ b/"):
			return unquote(line[len("+++ b/"):])
		case strings.HasPrefix(line, "+++ /dev/null") && old != "":
			return old
		case strings.HasPrefix(line, "@@ "):
			return old
		}
	}
	if i := strings.LastIndex(header, " b/"); i >= 0 {
		return strings.Trim(header[i+len(" b/"):], `"`)
	}
	return header
}

// maxUntracked is how many new files a message diff shows the lines of.
const maxUntracked = 20

// MessageDiff returns the changes a commit message is to describe, as a
// diff after a summary of its files: the staged changes, or every change
// when none is staged, the new files' included; with amend, the last
// commit's and the staged ones. staged reports whether it is what is
// staged. The lines of lockfiles, and of files that may hold secrets, by
// their names as secretFiles and then secretFiles' patterns say, or by
// their lines, are left out, and the diff is cut to about max bytes. It
// is "" when nothing changed.
func (r *Repo) MessageDiff(amend bool, max int, secretFiles []string) (diff string, staged bool, err error) {
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
	b.WriteString(redactPatch(string(patch), secretFiles))
	if !staged {
		r.writeUntracked(&b, secretFiles)
	}
	return cut(b.String(), max), staged, nil
}

// writeUntracked writes the new files that Git does not ignore to b: their
// names, and the lines of the first of them as a diff, but of lockfiles and
// of files that may hold secrets, by their names as more says too or by
// their lines.
func (r *Repo) writeUntracked(b *strings.Builder, more []string) {
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
		if slices.Contains(lockfiles, path.Base(n)) {
			continue
		}
		if secretIn(n, more) {
			// Named, with its lines left out, without reading it.
			b.WriteString("diff --git a/" + n + " b/" + n + "\n" + secretNote)
			shown++
			continue
		}
		// It exits with 1 as the files differ, which they always do.
		d, _ := r.run(nil, "diff", "--no-index", "--no-color", "--no-ext-diff", "--", "/dev/null", n)
		b.WriteString(redactPatch(string(d), more))
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
