package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Opt in with an already installed gopls; the fixture needs only the standard library.
func TestSaveCodeActionsGopls(t *testing.T) {
	gopls := os.Getenv("MAYOI_GOPLS")
	if gopls == "" {
		t.Skip("set MAYOI_GOPLS to an installed gopls executable")
	}
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOTELEMETRY", "off")
	t.Setenv("GOPLSCACHE", t.TempDir())
	for _, fix := range []bool{false, true} {
		name := "imports"
		if fix {
			name = "importsAndFixAll"
		}
		t.Run(name, func(t *testing.T) {
			hs := newHarness(t, false)
			a := hs.a
			a.settings.AutoSave = false
			a.settings.OrganizeImportsOnSave = map[string]bool{"go": true}
			a.settings.FixAllOnSave = map[string]bool{"go": fix}
			a.settings.FormatOnSave = map[string]bool{"go": true}
			manifest, _ := json.Marshal(map[string]any{"name": "gopls-test", "contributes": map[string]any{
				"languageServers": []any{map[string]any{"id": "gopls-test.server", "languages": []string{"go"}, "command": gopls}},
			}})
			withPlugins(hs, map[string]map[string]string{"gopls-test": {"plugin.json": string(manifest)}})
			t.Cleanup(stopServers)
			if err := os.WriteFile(hs.path("go.mod"), []byte("module example.com/saveactions\n\ngo 1.27.0\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			source := "package main\n\nimport (\n\"strings\"\n\"os\"\n)\n\nfunc main() {\nfor _ = range os.Args { fmt.Println(\"hello\") }\n}\n"
			// The harness has another main.go; replace it so there is one package entry point.
			if err := os.WriteFile(hs.path("main.go"), []byte(source), 0o644); err != nil {
				t.Fatal(err)
			}
			a.openFile(hs.path("main.go"), true)
			d := a.activeEditor()
			if _, sc := serverContrib(languageOf(d.document)); sc == nil {
				t.Fatal("no Go language server contribution")
			}
			hs.until("gopls ready", func() bool {
				s := serverOf(serverKey(a.root, "gopls-test.server"))
				if s != nil && s.state() == lspFailed {
					t.Fatalf("gopls failed: %v", s.err)
				}
				if s != nil && s.state() == lspStopped {
					t.Fatal("gopls stopped")
				}
				return s != nil && s.live() != nil && s.live().IsOpen(serverURI(d.path))
			})
			// Initialization opens the document before gopls finishes loading its package.
			hs.until("initial gopls diagnostics", func() bool { return len(plugins.diagnosticsOf(d.path)) > 0 })
			got := saveAndRead(t, hs, d)
			if !strings.Contains(got, `"fmt"`) || strings.Contains(got, `"strings"`) || strings.Index(got, `"fmt"`) > strings.Index(got, `"os"`) {
				t.Fatalf("imports were not organized: %q", got)
			}
			if fix && strings.Contains(got, "for _ = range") {
				t.Fatalf("Fix All did not simplify range: %q", got)
			}
			if d.dirty() {
				t.Fatal("saved buffer is dirty")
			}
			version := d.buf.Version()
			if again := saveAndRead(t, hs, d); again != got || d.buf.Version() != version {
				t.Fatal("repeated save was not idempotent")
			}
			cmd := exec.Command("go", "test", "-run", "^$", ".")
			cmd.Dir = hs.root
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("saved Go file does not compile: %v\n%s", err, output)
			}
		})
	}
}
