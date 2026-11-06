package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMessageDiff(t *testing.T) {
	r, git := repo(t)
	// Secret files committed by mistake, then changed: tracked.
	write(t, r.Root, ".env", "PORT=3000\n")
	write(t, r.Root, "config.py", "DEBUG = True\n")
	write(t, r.Root, "配置/.env", "HUNTER=1\n")
	write(t, r.Root, ".env.local", "A=1\nC=3\nD=4\nE=5\nF=6\n")
	git("add", ".env", "config.py", "配置/.env", ".env.local")
	git("commit", "-q", "-m", "config")
	// Settings that would change the paths a diff names.
	git("config", "diff.noprefix", "true")
	git("config", "diff.mnemonicPrefix", "true")
	write(t, r.Root, "配置/.env", "HUNTER=hunter2pass\n")
	write(t, r.Root, "a.txt", "one\n2\nthree\n")
	write(t, r.Root, ".env", "PORT=3000\nSECRET=abc\n")
	write(t, r.Root, "config.py", "DEBUG = True\nAWS_KEY = \"AKIAIOSFODNN7EXAMPLE\"\n")
	write(t, r.Root, "new.txt", "fresh\n")
	write(t, r.Root, "web/package-lock.json", "{\"lockfileVersion\": 3}\n")
	write(t, r.Root, "certs/server.pem", "PRIVATE KEY\n")
	write(t, r.Root, "notes.txt", "keep\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n")
	write(t, r.Root, ".env.example", "PORT=\nSECRET=\n")

	// Nothing staged: every change, the new files' too, but a lockfile's
	// lines and those of files that may hold secrets, by name or by what
	// they say, tracked or new.
	d, staged, err := r.MessageDiff(false, 0, nil)
	if err != nil || staged {
		t.Fatalf("staged %v err %v", staged, err)
	}
	for _, want := range []string{"-two", "+2", " .env ", " config.py ", "New files:", " new.txt", "+fresh",
		" web/package-lock.json", " certs/server.pem", " notes.txt", " .env.example", "+PORT=\n", secretNote,
		"+AWS_KEY = \"[left out]\"", "+keep", "配置/.env"} {
		if !strings.Contains(d, want) {
			t.Errorf("all changes lack %q:\n%s", want, d)
		}
	}
	for _, leak := range []string{"lockfileVersion", "SECRET=abc", "AKIAIOSFODNN7EXAMPLE", "PRIVATE KEY", "MIIEowIBAAKCAQEA",
		"hunter2pass"} {
		if strings.Contains(d, leak) {
			t.Errorf("all changes show %q:\n%s", leak, d)
		}
	}

	// A pattern of the person's sends a file Mayoi's would not, but what
	// looks like a key stays out.
	d, _, err = r.MessageDiff(false, 0, []string{"!.env"})
	if err != nil || !strings.Contains(d, "+SECRET=abc") || strings.Contains(d, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("with !.env: %v\n%s", err, d)
	}

	// Staged: only what is, the secret file's lines left out too.
	git("add", "new.txt", "web/package-lock.json", ".env")
	git("mv", ".env.local", "plain.txt") // checked by its old name too
	write(t, r.Root, "plain.txt", "A=1\nC=3\nD=4\nE=5\nF=6\nB=moved\n")
	git("add", "plain.txt")
	d, staged, err = r.MessageDiff(false, 0, nil)
	if err != nil || !staged {
		t.Fatalf("staged %v err %v", staged, err)
	}
	if !strings.Contains(d, "+fresh") || !strings.Contains(d, "package-lock.json") || !strings.Contains(d, " .env ") ||
		strings.Contains(d, "-two") || strings.Contains(d, "lockfileVersion") || strings.Contains(d, "SECRET=abc") ||
		!strings.Contains(d, "plain.txt") || strings.Contains(d, "B=moved") {
		t.Errorf("staged changes:\n%s", d)
	}

	// Amending: the last commit's changes and the staged ones.
	git("commit", "-q", "-m", "second")
	write(t, r.Root, "b.txt", "bee\nbuzz\n")
	git("add", "b.txt")
	d, staged, err = r.MessageDiff(true, 0, nil)
	if err != nil || !staged || !strings.Contains(d, "+fresh") || !strings.Contains(d, "+buzz") || strings.Contains(d, "-two") ||
		strings.Contains(d, "SECRET=abc") {
		t.Errorf("amend: staged %v err %v\n%s", staged, err, d)
	}

	// Cut at a line's end.
	d, _, _ = r.MessageDiff(false, 80, nil)
	if len(d) > 80+len("[… the rest of the diff is left out]\n") || !strings.HasSuffix(d, "\n[… the rest of the diff is left out]\n") {
		t.Errorf("cut diff:\n%q", d)
	}

	// Nothing changed.
	for _, n := range []string{".env.example", "notes.txt", "certs"} {
		os.RemoveAll(filepath.Join(r.Root, n))
	}
	git("commit", "-q", "-am", "third")
	if d, _, err := r.MessageDiff(false, 0, nil); d != "" || err != nil {
		t.Errorf("no changes: %q %v", d, err)
	}
}

func TestMessageDiffBeforeTheFirstCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	write(t, dir, "a.txt", "first\n")
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d, staged, err := r.MessageDiff(false, 0, nil); err != nil || staged || !strings.Contains(d, "+first") {
		t.Errorf("unstaged: %v %v\n%s", staged, err, d)
	}
	exec.Command("git", "-C", dir, "add", "a.txt").Run()
	if d, staged, err := r.MessageDiff(false, 0, nil); err != nil || !staged || !strings.Contains(d, "+first") {
		t.Errorf("staged: %v %v\n%s", staged, err, d)
	}
}

// The files that may hold secrets, by their names and folders, the
// templates of them that don't, and the person's patterns over Mayoi's.
func TestSecretIn(t *testing.T) {
	for _, name := range []string{
		".env", ".env.local", ".envrc", "prod.env", "config/secrets.yml", "secrets.json", "secret.txt", "配置/.env",
		"credentials", "aws/credentials.csv", "gcp/service-account-prod.json", "certs/server.pem", "id_rsa", "id_rsa.pub",
		"infra/prod.tfvars", "infra/prod.auto.tfvars.json", ".htpasswd", ".git-credentials", ".pypirc", ".s3cfg", "kubeconfig",
		"home/.ssh/config", ".aws/credentials", "x/.config/gcloud/access_tokens.db", ".docker/config.json",
		"api.token", "master.key", "keys/private.gpg", "vpn/office.ovpn", "pw.kdbx",
	} {
		if !secretIn(name, nil) {
			t.Errorf("%s is not secret", name)
		}
	}
	for _, name := range []string{
		".env.example", ".env.sample", "config/.env.template", ".env.dist", "settings.example",
		"README.md", "environment.ts", "token_test.go", "tokens.go", "docker/config.json", ".dockerignore",
		"secretary.txt", "src/.ssh", "a/b.txt", "key.go", "passwords.md",
		"internal/auth/secrets.go", "pkg/credentials.py", "src/lexer/token", "KEYS.asc",
	} {
		if secretIn(name, nil) {
			t.Errorf("%s is secret", name)
		}
	}
	for _, c := range []struct {
		name string
		more []string
		want bool
	}{
		{".env.example", []string{".env.*"}, true},
		{".env", []string{"!.env"}, false},
		{".env", []string{"!.env", ".env"}, true},
		{"config/prod.yaml", []string{"config/prod.*"}, true},
		{"app/config/prod.yaml", []string{"config/prod.*"}, true},
		{"config/prod/db.yaml", []string{"config/prod/**"}, true},
		{"docs/x.md", []string{"*.md", "!docs/*.md"}, false},
		{"x.md", []string{"*.md", "!docs/*.md"}, true},
		{"a.txt", []string{"", " ", "!"}, false},
		{"config/a.yml", []string{"/config/a.yml"}, true},
		{"app/config/a.yml", []string{"/config/a.yml"}, false},
		{"deploy/a.yml", []string{"/deploy/**"}, true},
		{"app/deploy/a.yml", []string{"/deploy/**"}, false},
		{"config/a.yml", []string{"config/"}, true},
		{"x/config/b/a.yml", []string{"config/"}, true},
		{"config", []string{"config/"}, false},
		{"prod.yml", []string{"**/prod.yml"}, true},
		{"a/b/prod.yml", []string{"**/prod.yml"}, true},
		{"a/x/y/b.yml", []string{"a/**/b.yml"}, true},
		{"a/b.yml", []string{"a/**/b.yml"}, true},
	} {
		if got := secretIn(c.name, c.more); got != c.want {
			t.Errorf("%s with %q: %v", c.name, c.more, got)
		}
	}
}

// The lines that look like secrets lose them, and the settings of them
// that only name them, or are code, stay.
func TestRedactLine(t *testing.T) {
	for _, c := range []struct{ line, want string }{
		{"+AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "+AWS_SECRET_ACCESS_KEY=[left out]"},
		{"+api_key: \"sk-ant-api03-abcdefghijklmnopqrstuvwxyz\"", "+api_key: \"[left out]\""},
		{"+token = 'ghp_" + strings.Repeat("a", 36) + "'", "+token = '[left out]'"},
		{"+url = postgres://user:hunter22@db/app", "+url = postgres://user:[left out]@db/app"},
		{"+Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcdefghij", "+Authorization: Bearer [left out]"},
		{" password: s3cr3tpassw0rd", " password: [left out]"},
		{"-  \"client_secret\": \"GOCSPX-abcdefghijklmnop\",", "-  \"client_secret\": \"[left out]\","},
		{"+SLACK_TOKEN=xoxb-123456789012-abcdefghij", "+SLACK_TOKEN=[left out]"},
		{"+key: AIza" + strings.Repeat("A", 35), "+key: [left out]"},
		{"+SECRET_KEY = 'django-insecure-abc123'", "+SECRET_KEY = '[left out]'"},
		{"+\ttoken := \"abcdefgh12345678\"", "+\ttoken := \"[left out]\""},
	} {
		if got, ok := redactLine(c.line); !ok || got != c.want {
			t.Errorf("%q: %q %v, want %q", c.line, got, ok, c.want)
		}
	}
	for _, line := range []string{
		"+API_KEY=${API_KEY}",
		"+API_KEY=$API_KEY",
		"+password: \"changeme\"",
		"+password = \"<your password>\"",
		"+api_key: xxxxxxxxxxxx",
		"+SECRET_KEY = os.environ[\"SECRET_KEY\"]",
		"+const token = process.env.TOKEN",
		"+token: string;",
		"+const token = await getToken();",
		"+PORT=3000",
		"+secretFiles = []string{",
		"+password_hash = hash(password)",
		"+Tokenizer = 'bert-base-uncased'",
		"+SECRET=abc",
		"+token = accessToken",
		"+\tToken: tok.AccessToken,",
		"+\tsecret: cfg.ClientSecret,",
		"+dsn := \"https://user:${PASSWORD}@host/db\"",
		"+url = postgres://user:password@localhost/app",
	} {
		if got, ok := redactLine(line); ok {
			t.Errorf("%q is secret: %q", line, got)
		}
	}
}

// A diff's sections keep their headers, and the lines of a file that may
// hold secrets, by its name, by its old name as it is deleted, or by
// what it says, are replaced.
func TestRedactPatch(t *testing.T) {
	key := "-----BEGIN RSA PRIVATE KEY-----"
	patch := "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-one\n+1\n" +
		"diff --git a/.env b/.env\n--- a/.env\n+++ b/.env\n@@ -1 +1 @@\n-PORT=1\n+PORT=2\n" +
		"diff --git a/old.pem b/old.pem\ndeleted file mode 100644\n--- a/old.pem\n+++ /dev/null\n@@ -1 +0,0 @@\n-KEYDATA\n" +
		"diff --git a/sp ace.key b/sp ace.key\n--- a/sp ace.key\t\n+++ b/sp ace.key\t\n@@ -1 +1 @@\n+SPACED\n" +
		"diff --git \"a/\\351\\205\\215/.env\" \"b/\\351\\205\\215/.env\"\n--- \"a/\\351\\205\\215/.env\"\n+++ \"b/\\351\\205\\215/.env\"\n@@ -1 +1 @@\n+QUOTED\n" +
		"diff --git a/c.go b/c.go\n--- a/c.go\n+++ b/c.go\n@@ -1,2 +1,2 @@\n func f() {\n+\tconst token = \"ghp_" + strings.Repeat("b", 36) + "\"\n" +
		"diff --git a/n.txt b/n.txt\n--- a/n.txt\n+++ b/n.txt\n@@ -1,4 +1,4 @@\n keep\n+" + key + "\n+MIIEowIBAAKCAQEA\n+-----END RSA PRIVATE KEY-----\n--- a sql comment\n" +
		"diff --git a/.env.local b/plain.txt\nsimilarity index 90%\nrename from .env.local\nrename to plain.txt\n--- a/.env.local\n+++ b/plain.txt\n@@ -1 +1,2 @@\n+B=2\n" +
		"diff --git a/x.bin b/y.bin\nsimilarity index 100%\nrename from x.bin\nrename to y.bin\n" +
		"diff --git a/id_rsa b/id_rsa\nBinary files differ\n"
	got := redactPatch(patch, nil, 0)
	want := "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-one\n+1\n" +
		"diff --git a/.env b/.env\n" + secretNote +
		"diff --git a/old.pem b/old.pem\n" + secretNote +
		"diff --git a/sp ace.key b/sp ace.key\n" + secretNote +
		"diff --git \"a/\\351\\205\\215/.env\" \"b/\\351\\205\\215/.env\"\n" + secretNote +
		"diff --git a/c.go b/c.go\n--- a/c.go\n+++ b/c.go\n@@ -1,2 +1,2 @@\n func f() {\n+\tconst token = \"[left out]\"\n" +
		"diff --git a/n.txt b/n.txt\n--- a/n.txt\n+++ b/n.txt\n@@ -1,4 +1,4 @@\n keep\n+[left out]\n+[left out]\n+[left out]\n--- a sql comment\n" +
		"diff --git a/.env.local b/plain.txt\n" + secretNote +
		"diff --git a/x.bin b/y.bin\nsimilarity index 100%\nrename from x.bin\nrename to y.bin\n" +
		"diff --git a/id_rsa b/id_rsa\n" + secretNote
	if got != want {
		t.Errorf("redacted:\n%s\nwant:\n%s", got, want)
	}
	if got := redactPatch(patch, []string{"!.env", "*.txt"}, 0); !strings.Contains(got, "+PORT=2") || strings.Contains(got, "+1\n") {
		t.Errorf("with the person's patterns:\n%s", got)
	}
	if got := redactPatch(patch, nil, 10); strings.Count(got, "diff --git ") != 1 {
		t.Errorf("past max:\n%s", got)
	}
	if got := redactPatch("", nil, 0); got != "" {
		t.Errorf("empty: %q", got)
	}
	if got := redactPatch("not a diff\n", nil, 0); got != "not a diff\n" {
		t.Errorf("not a diff: %q", got)
	}
}
