package agents

import "testing"

func TestHarnessOfCoAuthor(t *testing.T) {
	for _, tc := range []struct {
		name, email string
		want        Harness
		ok          bool
	}{
		{"Claude", "noreply@anthropic.com", Claude, true},
		{"Claude Opus 4", "noreply@anthropic.com", Claude, true},
		{"Codex", "", Codex, true},
		{"codex", "codex@users.noreply.github.com", Codex, true},
		{"Claude Monet", "claude@monet.fr", "", false},
		{"Jane Doe", "jane@x.io", "", false},
	} {
		h, ok := HarnessOfCoAuthor(tc.name, tc.email)
		if h != tc.want || ok != tc.ok {
			t.Errorf("HarnessOfCoAuthor(%q, %q) = %q, %v; want %q, %v", tc.name, tc.email, h, ok, tc.want, tc.ok)
		}
	}
}
