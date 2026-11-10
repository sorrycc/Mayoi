package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mayoi/internal/lsp"
	"mayoi/internal/plugin"
)

func init() {
	if os.Getenv("MAYOI_TEST_RESOLVER") == "1" {
		var request struct {
			Root     string                     `json:"root"`
			Settings map[string]json.RawMessage `json:"settings"`
		}
		data, _ := io.ReadAll(os.Stdin)
		if json.Unmarshal(data, &request) != nil || request.Root == "" {
			os.Exit(2)
		}
		json.NewEncoder(os.Stdout).Encode(serverLaunch{Command: os.Args[0], Args: []string{"-test.run=^$"}, Configuration: map[string]any{"root": request.Root}, WorkspaceFolders: []lsp.WorkspaceFolder{{URI: lsp.URI(request.Root), Name: "test"}}, Env: map[string]string{"RESOLVED": "yes"}})
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
	// Static declarations retain their old behavior.
	s.contrib = plugin.ServerContrib{Command: "static", Args: []string{"a"}}
	launch, err = s.resolve(ctx)
	if err != nil || launch.Command != "static" {
		t.Fatalf("static: %+v %v", launch, err)
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
