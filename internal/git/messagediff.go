package git

import (
	"path"
	"regexp"
	"slices"
	"strconv"
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
// as these would leave the machine. The last pattern that matches decides,
// and "!" before one takes back what matched before it. A pattern without
// "/" is of a file's name; one with "/" is of the end of its path, or of
// its start when it begins with "/"; "**" is any run of folders, and a
// pattern ending in "/" is of a folder. The templates of such files, which
// name the settings and not their values, go along. Code named after
// secrets, as secrets.go, is left to the check of its lines.
var secretFiles = slices.Concat([]string{
	".env*", "*.env", "*.secret", "*.secrets",
	"credentials", "*credentials*.json", "service-account*.json", "service_account*.json",
	"*.pem", "*.key", "*.p12", "*.pfx", "*.keystore", "*.jks", "*.ppk",
	"id_rsa*", "id_dsa*", "id_ecdsa*", "id_ed25519*",
	"*.tfvars", "*.tfvars.json", "*.gpg", "*.pgp", "*.ovpn", "*.kdbx",
	".netrc", ".npmrc", ".pgpass", ".htpasswd", ".git-credentials", ".pypirc", ".s3cfg", ".boto",
	"kubeconfig", "*.kubeconfig", "master.key", "wallet.dat", "*.token",
	".ssh/", ".aws/", ".gnupg/", ".kube/", ".config/gcloud/", ".docker/config.json",
}, dataFiles("secret", "secrets", "credentials"), []string{
	"!.env.example", "!.env.sample", "!.env.template", "!.env.dist", "!.env.schema",
	"!*.example", "!*.sample", "!*.template", "!*.dist",
})

// dataFiles returns the names of files of settings, not code, called
// each of names.
func dataFiles(names ...string) []string {
	var files []string
	for _, n := range names {
		for _, ext := range []string{"json", "yml", "yaml", "toml", "ini", "txt", "csv", "xml", "conf", "cfg", "properties", "plist"} {
			files = append(files, n+"."+ext)
		}
	}
	return files
}

// secretIn reports whether the file at name, a path with "/", may hold
// secrets: by secretFiles, then by the patterns of more, which win.
func secretIn(name string, more []string) bool {
	hit := false
	for _, list := range [][]string{secretFiles, more} {
		for _, p := range list {
			p, not := strings.CutPrefix(strings.TrimSpace(p), "!")
			if p == "" {
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
	if dir, ok := strings.CutSuffix(pattern, "/"); ok {
		pattern = dir + "/**"
	}
	pattern, anchored := strings.CutPrefix(pattern, "/")
	pat, segs := strings.Split(pattern, "/"), strings.Split(name, "/")
	for i := range segs {
		if matchSegs(pat, segs[i:]) {
			return true
		}
		if anchored {
			break
		}
	}
	return false
}

// matchSegs reports whether the folders and name of segs match those of
// pat, where "**" is any of them, or at least one at the end.
func matchSegs(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		if len(pat) == 1 {
			return len(segs) > 0
		}
		for i := range len(segs) + 1 {
			if matchSegs(pat[1:], segs[i:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	ok, _ := path.Match(pat[0], segs[0])
	return ok && matchSegs(pat[1:], segs[1:])
}

// secretText matches what is a secret wherever it is: the tokens of AWS,
// GitHub, OpenAI and Anthropic, Stripe, Slack and Google, a JWT, and a
// bearer token.
var secretText = []*regexp.Regexp{
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}\b`),
	regexp.MustCompile(`\bsk-(ant-)?[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\b[sr]k_(live|test)_[A-Za-z0-9]{16,}\b`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]*`),
	regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9+/=_.-]{20,}`),
}

// secretKey matches the first line of a private key, whose lines follow
// it up to its last, which secretKeyEnd matches.
var (
	secretKey    = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY( BLOCK)?-----`)
	secretKeyEnd = regexp.MustCompile(`-----END [A-Z ]*PRIVATE KEY( BLOCK)?-----`)
)

// secretURL matches a URL with a password, its second group.
var secretURL = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://[^/\s:@]+:)([^@\s]{4,})@`)

// secretAssign matches a setting of a secret's name given a value, its
// fifth group, quoted when its fourth is not "", which is a secret unless
// it is a placeholder or code (see secretValue).
var secretAssign = regexp.MustCompile(
	`(?i)(api[_-]?key|secret([_-]?key)?|token|passw(or)?d|private[_-]?key|access[_-]?key)["']?\s*(?::=|[:=])\s*(["']?)([^\s"',;]{8,})`)

// secretPlaceholder is a value that stands for a secret rather than is
// one: xxx, ***, changeme, <your key>, ${KEY}, $KEY, your-key, example…
var secretPlaceholder = regexp.MustCompile(
	`(?i)^(x+|\*+|\.+|changeme|change_me|placeholder|redacted|dummy|none|null|undefined|true|false|pass|passw(or)?d|secret|<.*>?|\$\{.*\}?|\$[A-Z_]+|%[A-Z_]+%|your[_-].*|example.*|sample.*)$`)

// secretCode matches a name in code, as accessToken or cfg.ClientSecret.
var secretCode = regexp.MustCompile(`^[A-Za-z_$][\w$]*(\.[A-Za-z_$][\w$]*)*$`)

// secretValue reports whether v, the value of a setting of a secret, is
// one: not a placeholder, and not code that reads it from elsewhere. A
// bare name counts as code but for one with digits, as hunter2.
func secretValue(v string, quoted bool) bool {
	if secretPlaceholder.MatchString(v) || strings.ContainsAny(v, "()[]{}") {
		return false
	}
	for _, code := range []string{"os.", "process.", "env.", "getenv", "ENV", "System.", "config.", "settings.", "self.", "this."} {
		if strings.HasPrefix(v, code) {
			return false
		}
	}
	return quoted || !secretCode.MatchString(v) || strings.ContainsAny(v, "0123456789") && !strings.Contains(v, ".")
}

// secretMark is what a message diff says in place of a secret.
const secretMark = "[left out]"

// redactLine returns line with what looks like a secret in it replaced
// by secretMark, and whether it was.
func redactLine(line string) (string, bool) {
	out := line
	for _, re := range secretText {
		out = re.ReplaceAllLiteralString(out, secretMark)
	}
	out = secretURL.ReplaceAllStringFunc(out, func(m string) string {
		sm := secretURL.FindStringSubmatch(m)
		if secretPlaceholder.MatchString(sm[2]) {
			return m
		}
		return sm[1] + secretMark + "@"
	})
	ms := secretAssign.FindAllStringSubmatchIndex(out, -1)
	for i := len(ms) - 1; i >= 0; i-- {
		m := ms[i]
		if secretValue(out[m[10]:m[11]], m[9] > m[8]) {
			out = out[:m[10]] + secretMark + out[m[11]:]
		}
	}
	return out, out != line
}

// secretNote is what a message diff says in place of the lines of a file
// that may hold secrets by its name.
const secretNote = "[lines left out: the file may hold secrets]\n"

// redactPatch returns patch, a diff of files, with the lines of each file
// that may hold secrets by its name, old or new, as secretIn says with
// more, replaced by secretNote, and what looks like a secret in the lines
// of the others by secretMark. With max over 0, it stops once it has
// passed max bytes, as the rest would be cut.
func redactPatch(patch string, more []string, max int) string {
	const head = "diff --git "
	if !strings.HasPrefix(patch, head) {
		return patch
	}
	var b strings.Builder
	sections := strings.Split(patch[len(head):], "\n"+head)
	for i, sec := range sections {
		if max > 0 && b.Len() > max {
			break
		}
		if i < len(sections)-1 {
			sec += "\n" // what the split took
		}
		header, body, _ := strings.Cut(sec, "\n")
		b.WriteString(head + header + "\n")
		if slices.ContainsFunc(patchPaths(header, body), func(p string) bool { return secretIn(p, more) }) {
			b.WriteString(secretNote)
		} else {
			redactLines(&b, body)
		}
	}
	return b.String()
}

// redactLines writes body, of a section of a diff, to b, with what looks
// like a secret in its lines replaced by secretMark, and the lines of a
// private key by one each.
func redactLines(b *strings.Builder, body string) {
	hunk, key := false, false
	for line := range strings.Lines(body) {
		if strings.HasPrefix(line, "@@ ") {
			hunk = true
		}
		if !hunk || line == "" || !strings.ContainsRune(" +-", rune(line[0])) {
			b.WriteString(line)
			continue
		}
		switch {
		case key:
			key = !secretKeyEnd.MatchString(line)
			b.WriteString(line[:1] + secretMark + "\n")
		case secretKey.MatchString(line):
			key = !secretKeyEnd.MatchString(line)
			b.WriteString(line[:1] + secretMark + "\n")
		default:
			out, _ := redactLine(line)
			b.WriteString(out)
		}
	}
}

// patchPaths are the paths, old and new, of the file a section of a diff
// is of: of its --- and +++ lines, of a rename's or copy's, else of its
// header, as of a binary file's.
func patchPaths(header, body string) []string {
	var paths []string
	for line := range strings.Lines(body) {
		line = strings.TrimSuffix(line, "\n")
		if strings.HasPrefix(line, "@@ ") {
			break
		}
		for _, p := range []string{"--- ", "+++ ", "rename from ", "rename to ", "copy from ", "copy to "} {
			if rest, ok := strings.CutPrefix(line, p); ok {
				if n := gitPath(rest); n != "/dev/null" {
					paths = append(paths, n)
				}
			}
		}
	}
	if len(paths) == 0 {
		// "a/old b/new", either quoted.
		old, new := header, ""
		if q, err := strconv.QuotedPrefix(header); err == nil {
			old, new = q, strings.TrimSpace(header[len(q):])
		} else if i := strings.LastIndex(header, ` "b/`); i >= 0 {
			old, new = header[:i], header[i+1:]
		} else if i := strings.LastIndex(header, " b/"); i >= 0 {
			old, new = header[:i], header[i+1:]
		}
		paths = []string{gitPath(old), gitPath(new)}
	}
	for i, p := range paths {
		if a, ok := strings.CutPrefix(p, "a/"); ok {
			paths[i] = a
		} else if b, ok := strings.CutPrefix(p, "b/"); ok {
			paths[i] = b
		}
	}
	return paths
}

// gitPath is the path Git writes as s in a diff, which it quotes, as C
// does, when it has odd characters, and ends with a tab when it has a
// space.
func gitPath(s string) string {
	s = strings.TrimSuffix(s, "\t")
	if strings.HasPrefix(s, `"`) {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}

// maxUntracked is how many new files a message diff shows the lines of.
const maxUntracked = 20

// MessageDiff returns the changes a commit message is to describe, as a
// diff after a summary of its files: the staged changes, or every change
// when none is staged, the new files' included; with amend, the last
// commit's and the staged ones. staged reports whether it is what is
// staged. The lines of lockfiles, and of files that may hold secrets by
// their names, as secretFiles and then secretFiles' patterns say, are
// left out, as is what looks like a secret in the lines of the others,
// and the diff is cut to about max bytes. It is "" when nothing changed.
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
		a := append(diffArgs("-M"), opts...)
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
	b.WriteString(redactPatch(string(patch), secretFiles, max))
	if !staged {
		r.writeUntracked(&b, secretFiles, max)
	}
	return cut(b.String(), max), staged, nil
}

// diffArgs are the arguments of a git diff with opts that the diffs of
// MessageDiff read the same whatever Git's settings: paths unquoted but
// for odd characters, after a/ and b/.
func diffArgs(opts ...string) []string {
	return append([]string{"-c", "core.quotePath=false", "diff", "--no-color", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/"}, opts...)
}

// writeUntracked writes the new files that Git does not ignore to b: their
// names, and the lines of the first of them as a diff, but of lockfiles and
// of files that may hold secrets, by their names as more says too, and
// what looks like a secret in their lines. It reads no more once b passes
// max bytes, with max over 0.
func (r *Repo) writeUntracked(b *strings.Builder, more []string, max int) {
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
		if shown == maxUntracked || max > 0 && b.Len() > max {
			break
		}
		if slices.Contains(lockfiles, path.Base(n)) {
			continue
		}
		if secretIn(n, more) {
			// Named, with its lines left out, without reading it, and
			// not counted, so that keys don't take the place of code.
			b.WriteString("diff --git a/" + n + " b/" + n + "\n" + secretNote)
			continue
		}
		// It exits with 1 as the files differ, which they always do.
		d, _ := r.run(nil, diffArgs("--no-index", "--", "/dev/null", n)...)
		b.WriteString(redactPatch(string(d), more, 0))
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
