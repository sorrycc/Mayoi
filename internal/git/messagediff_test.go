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
	git("add", ".env", "config.py")
	git("commit", "-q", "-m", "config")
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
		" web/package-lock.json", " certs/server.pem", " notes.txt", " .env.example", "+PORT=\n", secretNote} {
		if !strings.Contains(d, want) {
			t.Errorf("all changes lack %q:\n%s", want, d)
		}
	}
	for _, leak := range []string{"lockfileVersion", "SECRET=abc", "AKIAIOSFODNN7EXAMPLE", "PRIVATE KEY", "MIIEowIBAAKCAQEA"} {
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
	d, staged, err = r.MessageDiff(false, 0, nil)
	if err != nil || !staged {
		t.Fatalf("staged %v err %v", staged, err)
	}
	if !strings.Contains(d, "+fresh") || !strings.Contains(d, "package-lock.json") || !strings.Contains(d, " .env ") ||
		strings.Contains(d, "-two") || strings.Contains(d, "lockfileVersion") || strings.Contains(d, "SECRET=abc") {
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
		".env", ".env.local", ".envrc", "prod.env", "config/secrets.yml", "secrets.json", "secret.txt",
		"credentials", "aws/credentials.csv", "gcp/service-account-prod.json", "certs/server.pem", "id_rsa", "id_rsa.pub",
		"infra/prod.tfvars", "infra/prod.auto.tfvars.json", ".htpasswd", ".git-credentials", ".pypirc", ".s3cfg", "kubeconfig",
		"home/.ssh/config", ".aws/credentials", "x/.config/gcloud/access_tokens.db", ".docker/config.json",
		"token", "api.token", "master.key", "keys/private.gpg", "vpn/office.ovpn", "pw.kdbx",
	} {
		if !secretIn(name, nil) {
			t.Errorf("%s is not secret", name)
		}
	}
	for _, name := range []string{
		".env.example", ".env.sample", "config/.env.template", ".env.dist", "settings.example",
		"README.md", "environment.ts", "token_test.go", "tokens.go", "docker/config.json", ".dockerignore",
		"secretary.txt", "src/.ssh", "a/b.txt", "key.go", "passwords.md",
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
	} {
		if got := secretIn(c.name, c.more); got != c.want {
			t.Errorf("%s with %q: %v", c.name, c.more, got)
		}
	}
}

// The lines that look like secrets, and the settings of them that only
// name them.
func TestSecretLines(t *testing.T) {
	for _, line := range []string{
		"+AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"+api_key: \"sk-ant-api03-abcdefghijklmnopqrstuvwxyz\"",
		"+token = 'ghp_" + strings.Repeat("a", 36) + "'",
		"+url = postgres://user:hunter22@db/app",
		"+-----BEGIN OPENSSH PRIVATE KEY-----",
		"+Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcdefghij",
		" password: s3cr3tpassw0rd",
		"-  \"client_secret\": \"GOCSPX-abcdefghijklmnop\",",
		"+SLACK_TOKEN=xoxb-123456789012-abcdefghij",
		"+key: AIza" + strings.Repeat("A", 35),
	} {
		if !secretLines(line + "\n") {
			t.Errorf("%q is not secret", line)
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
		"+++ b/.env",
		"--- a/secrets/private.key",
		"+secretFiles = []string{",
		"+password_hash = hash(password)",
		"+Tokenizer = 'bert-base-uncased'",
		"+SECRET=abc",
	} {
		if secretLines(line + "\n") {
			t.Errorf("%q is secret", line)
		}
	}
}

// A diff's sections keep their headers, and the lines of a file that may
// hold secrets, by its name, by its old name as it is deleted, or by
// what it says, are replaced.
func TestRedactPatch(t *testing.T) {
	patch := "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-one\n+1\n" +
		"diff --git a/.env b/.env\n--- a/.env\n+++ b/.env\n@@ -1 +1 @@\n-PORT=1\n+PORT=2\n" +
		"diff --git a/old.pem b/old.pem\ndeleted file mode 100644\n--- a/old.pem\n+++ /dev/null\n@@ -1 +0,0 @@\n-KEYDATA\n" +
		"diff --git a/\"sp ace.key\" b/\"sp ace.key\"\n--- a/\"sp ace.key\"\n+++ b/\"sp ace.key\"\n@@ -1 +1 @@\n+QUOTED\n" +
		"diff --git a/c.go b/c.go\n--- a/c.go\n+++ b/c.go\n@@ -1 +1 @@\n+const token = \"ghp_" + strings.Repeat("b", 36) + "\"\n" +
		"diff --git a/x.bin b/y.bin\nsimilarity index 100%\nrename from x.bin\nrename to y.bin\n"
	got := redactPatch(patch, nil)
	want := "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-one\n+1\n" +
		"diff --git a/.env b/.env\n" + secretNote +
		"diff --git a/old.pem b/old.pem\n" + secretNote +
		"diff --git a/\"sp ace.key\" b/\"sp ace.key\"\n" + secretNote +
		"diff --git a/c.go b/c.go\n" + secretNote +
		"diff --git a/x.bin b/y.bin\nsimilarity index 100%\nrename from x.bin\nrename to y.bin\n"
	if got != want {
		t.Errorf("redacted:\n%s\nwant:\n%s", got, want)
	}
	if got := redactPatch(patch, []string{"!.env", "*.txt"}); !strings.Contains(got, "+PORT=2") || strings.Contains(got, "+1\n") {
		t.Errorf("with the person's patterns:\n%s", got)
	}
	if got := redactPatch("", nil); got != "" {
		t.Errorf("empty: %q", got)
	}
	if got := redactPatch("not a diff\n", nil); got != "not a diff\n" {
		t.Errorf("not a diff: %q", got)
	}
}
