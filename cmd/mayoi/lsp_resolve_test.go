package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mayoi/internal/lsp"
	"mayoi/internal/plugin"
)

func init() {
	if os.Getenv("MAYOI_TEST_RESOLVER") == "1" {
		switch os.Getenv("MAYOI_TEST_RESOLVER_MODE") {
		case "timeout":
			time.Sleep(time.Hour)
		case "malformed":
			fmt.Fprint(os.Stdout, `{invalid`)
			os.Exit(0)
		case "extra":
			fmt.Fprint(os.Stdout, `{"command":"server"} {}`)
			os.Exit(0)
		case "oversized":
			fmt.Fprint(os.Stdout, `{"command":"server"}`)
			fmt.Fprint(os.Stdout, strings.Repeat(" ", (4<<20)+1))
			os.Exit(0)
		case "exit":
			fmt.Fprint(os.Stderr, "resolver setup failed")
			os.Exit(3)
		case "no-command":
			fmt.Fprint(os.Stdout, `{"args":["server"]}`)
			os.Exit(0)
		}
		var request struct {
			Root     string                     `json:"root"`
			Settings map[string]json.RawMessage `json:"settings"`
		}
		data, _ := io.ReadAll(os.Stdin)
		if json.Unmarshal(data, &request) != nil || request.Root == "" {
			os.Exit(2)
		}
		json.NewEncoder(os.Stdout).Encode(serverLaunch{
			Command: os.Args[0], Args: []string{"-test.run=^$"},
			Configuration:    map[string]any{"root": request.Root, "token": json.Number("9007199254740993")},
			WorkspaceFolders: []lsp.WorkspaceFolder{{URI: lsp.URI(request.Root), Name: "test"}},
			Env:              map[string]string{"RESOLVED": "yes", "MAYOI_FAKE_LSP": "1"},
			UnsetEnv:         []string{"MAYOI_TEST_RESOLVER"},
		})
		os.Exit(0)
	}
}

func TestResolveLanguageServer(t *testing.T) {
	t.Setenv("MAYOI_TEST_RESOLVER", "1")
	root := t.TempDir()
	s := &lspServer{root: root, plugin: &plugin.Plugin{}, contrib: plugin.ServerContrib{Resolve: &plugin.ServerResolver{Command: os.Args[0]}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	launch, err := s.resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Configuration["root"] != root || launch.Env["RESOLVED"] != "yes" {
		t.Fatalf("launch: %+v", launch)
	}
	if launch.WorkspaceFolders[0].URI != lsp.URI(root) {
		t.Fatal(launch.WorkspaceFolders)
	}
	if n, ok := launch.Configuration["token"].(json.Number); !ok || n.String() != "9007199254740993" {
		t.Fatalf("configuration number changed: %+v", launch.Configuration)
	}
	// Static declarations retain their old behavior.
	s.contrib = plugin.ServerContrib{Command: "static", Args: []string{"a"}}
	launch, err = s.resolve(ctx)
	if err != nil || launch.Command != "static" {
		t.Fatalf("static: %+v %v", launch, err)
	}
	// A static command takes the plugin's settings, as a resolver does.
	s.plugin = &plugin.Plugin{Manifest: plugin.Manifest{Contributes: plugin.Contributes{Settings: map[string]plugin.SettingContrib{
		"path": {Type: "string", Default: json.RawMessage(`"/opt/gopls"`)},
	}}}}
	s.contrib = plugin.ServerContrib{Command: "${settings.path}", Args: []string{"-remote=${settings.path}"}}
	launch, err = s.resolve(ctx)
	if err != nil || launch.Command != "/opt/gopls" || launch.Args[0] != "-remote=/opt/gopls" {
		t.Fatalf("static with settings: %+v %v", launch, err)
	}
	if got := serverProgram(s.plugin, &s.contrib); got != "/opt/gopls" {
		t.Fatalf("serverProgram = %q", got)
	}
}

func TestResolverFailures(t *testing.T) {
	t.Setenv("MAYOI_TEST_RESOLVER", "1")
	s := &lspServer{root: t.TempDir(), plugin: &plugin.Plugin{}, contrib: plugin.ServerContrib{Resolve: &plugin.ServerResolver{Command: os.Args[0]}}}
	for _, tc := range []struct{ mode, want string }{
		{"malformed", "invalid launch description"},
		{"extra", "unexpected resolver output"},
		{"oversized", "resolver output exceeds 4 MiB"},
		{"exit", "resolver setup failed"},
		{"no-command", "has no command"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Setenv("MAYOI_TEST_RESOLVER_MODE", tc.mode)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := s.resolve(ctx); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("resolve: %v; want %q", err, tc.want)
			}
		})
	}
	t.Run("timeout", func(t *testing.T) {
		t.Setenv("MAYOI_TEST_RESOLVER_MODE", "timeout")
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		started := time.Now()
		if _, err := s.resolve(ctx); err == nil || !strings.Contains(err.Error(), "did not finish in time") {
			t.Fatalf("resolve: %v", err)
		}
		if time.Since(started) > 3*time.Second {
			t.Fatal("resolver outlived its deadline")
		}
	})
}

func TestResolverOutputLimitDrainsCopy(t *testing.T) {
	var out limitedBuffer
	const size = (4 << 20) + 1024
	n, err := io.Copy(&out, io.LimitReader(strings.NewReader(strings.Repeat("x", size)), size))
	if err != nil || n != size {
		t.Fatalf("copy did not drain: %d, %v", n, err)
	}
	if !out.overflow || out.buffer.Len() != 4<<20 {
		t.Fatalf("unbounded copy: len=%d overflow=%v", out.buffer.Len(), out.overflow)
	}
}

func TestResolveSettingsExpansion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helper $` space")
	value, _ := json.Marshal(path)
	got, err := expandServerArg("${settings.helperPath}", map[string]json.RawMessage{"helperPath": value})
	if err != nil || got != path {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err = expandServerArg("${settings.missing}", nil); err == nil {
		t.Fatal("accepted missing setting")
	}
	if err = (serverLaunch{Command: "java", Env: map[string]string{"-u=oops": "x"}}).validate(); err == nil {
		t.Fatal("accepted env option injection")
	}
	if err = (serverLaunch{Command: "java", PathMappings: []lsp.PathMapping{{ClientURI: "relative", ServerURI: "file:///real"}}}).validate(); err == nil {
		t.Fatal("accepted invalid mapping")
	}
}

// A resolver installed after a failed setup is retried on opening a file,
// and its launch description still starts a server that sends diagnostics.
func TestResolvedLanguageServerRetryOnOpen(t *testing.T) {
	t.Setenv("MAYOI_TEST_RESOLVER", "1")
	old := lspRetryPause
	lspRetryPause = 0
	t.Cleanup(func() { lspRetryPause = old })
	resolver := filepath.Join(t.TempDir(), "mayoi-fake-resolver")
	if runtime.GOOS == "windows" {
		resolver += ".exe"
	}
	manifest, _ := json.Marshal(map[string]any{
		"name": "fake",
		"contributes": map[string]any{
			"languages": []any{map[string]any{"id": "fake", "extensions": []string{".fake"}}},
			"languageServers": []any{map[string]any{
				"id": "fake.server", "languages": []string{"fake"},
				"resolve": map[string]any{"command": resolver},
			}},
		},
	})
	hs := newHarness(t, false)
	h := withPlugins(hs, map[string]map[string]string{"fake": {"plugin.json": string(manifest)}})
	t.Cleanup(stopServers)
	open := func(name string) string {
		path := hs.path(name)
		if err := os.WriteFile(path, []byte("so bad\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		hs.a.openFile(path, true)
		return path
	}
	open("a.fake")
	hs.until("the failed resolution", func() bool { return hs.a.fails == 1 })
	// Copy the test executable: Windows needs an .exe suffix and should not
	// require symlink privileges just to exercise retry after installation.
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resolver, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	path := open("b.fake")
	hs.until("the resolved server's diagnostics", func() bool { return len(h.diagnosticsOf(path)) == 1 })
	lspServers.Lock()
	s := lspServers.m[hs.a.root+"\x00fake.server"]
	lspServers.Unlock()
	if s == nil || s.starts != 2 || s.state() != lspRunning || hs.a.fails != 1 {
		t.Fatalf("after installing the resolver: server %+v, failures %d", s, hs.a.fails)
	}
}

func TestResolverManifest(t *testing.T) {
	for _, body := range []string{
		`{"resolve":{"command":"helper","args":["resolve"]}}`,
		`{"command":"java"}`,
	} {
		var contribution map[string]any
		json.Unmarshal([]byte(body), &contribution)
		contribution["id"], contribution["languages"] = "test.server", []string{"java"}
		data, _ := json.Marshal(map[string]any{"name": "test", "contributes": map[string]any{"languageServers": []any{contribution}}})
		if _, err := plugin.ParseManifest(data); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{`"resolve":{}`, `"command":"java","resolve":{"command":"helper"}`} {
		data := fmt.Sprintf(`{"name":"test","contributes":{"languageServers":[{"id":"test.server",%s}]}}`, body)
		if _, err := plugin.ParseManifest([]byte(data)); err == nil {
			t.Fatal("accepted", body)
		}
	}
}

// An argument that is all of a setting that is a list is its words, as
// many arguments; a string of initializationOptions that is all of a
// setting is its value, and goes with its key when the setting is empty.
func TestServerSettingLists(t *testing.T) {
	settings := map[string]json.RawMessage{
		"path": json.RawMessage(`"tsc"`), "args": json.RawMessage(`["--lsp", "--stdio"]`),
		"none": json.RawMessage(`[]`), "tsserverPath": json.RawMessage(`""`), "log": json.RawMessage(`"verbose"`),
	}
	command, args, err := expandServerCommand("${settings.path}", []string{"${settings.args}", "-x=${settings.log}", "${settings.none}"}, settings)
	if err != nil || command != "tsc" || strings.Join(args, " ") != "--lsp --stdio -x=verbose" {
		t.Fatalf("%q %q %v", command, args, err)
	}
	got := expandServerOptions(json.RawMessage(`{"tsserver": {"path": "${settings.tsserverPath}", "logVerbosity": "${settings.log}"}, "keep": "${not a setting}"}`), settings)
	var v map[string]any
	json.Unmarshal(got, &v)
	if ts := v["tsserver"].(map[string]any); len(ts) != 1 || ts["logVerbosity"] != "verbose" || v["keep"] != "${not a setting}" {
		t.Fatalf("options %s", got)
	}

	// The bundled TypeScript plugin, with its defaults, runs as before.
	data, err := os.ReadFile(filepath.Join("..", "..", "plugins", "typescript-language-server", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m plugin.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	defaults := map[string]json.RawMessage{}
	for key, s := range m.Contributes.Settings {
		defaults[key] = s.Default
	}
	sc := m.Contributes.LanguageServers[0]
	command, args, err = expandServerCommand(sc.Command, sc.Args, defaults)
	if err != nil || command != "typescript-language-server" || strings.Join(args, " ") != "--stdio" {
		t.Fatalf("defaults: %q %q %v", command, args, err)
	}
	if got := string(expandServerOptions(sc.Options, defaults)); got != `{"tsserver":{}}` {
		t.Fatalf("default options %s", got)
	}
}
