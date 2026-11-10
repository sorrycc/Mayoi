package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mayoi/internal/lsp"
	"mayoi/internal/plugin"
	"mayoi/internal/vfs"
)

// An opt-in acceptance test for a real resolver and server, using the same
// configuration, transport and path mapping as the app. Inputs remain outside
// the repository so it can also verify a person's existing workspace.
func TestResolvedLanguageServerIntegration(t *testing.T) {
	helper, root, casesPath := os.Getenv("MAYOI_LSP_HELPER"), os.Getenv("MAYOI_LSP_ROOT"), os.Getenv("MAYOI_LSP_CASES")
	if helper == "" || root == "" || casesPath == "" {
		t.Skip("set MAYOI_LSP_HELPER, MAYOI_LSP_ROOT and MAYOI_LSP_CASES")
	}
	var cases []struct {
		Path         string
		Line, Column int
		WantPath     string
		WantLine     int
	}
	data, err := os.ReadFile(casesPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &cases); err != nil || len(cases) == 0 {
		t.Fatalf("cases: %v", err)
	}
	s := &lspServer{root: root, plugin: &plugin.Plugin{}, contrib: plugin.ServerContrib{Resolve: &plugin.ServerResolver{Command: helper, Args: []string{"resolve"}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	plan, err := s.resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := vfs.CommandEnvironment(root, plan.environment(), plan.UnsetEnv, nil, plan.Command, plan.Args...)
	if err != nil {
		t.Fatal(err)
	}
	initOptions := plan.Options
	rootURI := serverURI(root)
	if len(plan.WorkspaceFolders) > 0 {
		rootURI = plan.WorkspaceFolders[0].URI
	}
	ready := make(chan struct{}, 1)
	client, err := lsp.Start(ctx, lsp.Options{Cmd: cmd, RootURI: rootURI, InitOptions: initOptions, Configuration: plan.Configuration, WorkspaceFolders: plan.WorkspaceFolders, PathMappings: plan.PathMappings, Handlers: lsp.Handlers{Notification: func(method string, params json.RawMessage) {
		if method == os.Getenv("MAYOI_LSP_READY_METHOD") {
			var status struct {
				Type string `json:"type"`
			}
			json.Unmarshal(params, &status)
			if status.Type == os.Getenv("MAYOI_LSP_READY_STATE") {
				select {
				case ready <- struct{}{}:
				default:
				}
			}
		}
	}, Log: func(level, msg string) {
		if level == "error" {
			t.Logf("server: %s", msg)
		}
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if os.Getenv("MAYOI_LSP_READY_METHOD") != "" {
		select {
		case <-ready:
		case <-time.After(2 * time.Minute):
			t.Fatal("server did not report readiness")
		}
	}
	deadline := time.Now().Add(3 * time.Minute)
	for round := 0; round < 2; round++ {
		for _, c := range cases {
			path := filepath.Join(root, c.Path)
			text, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			client.Open(serverURI(path), "java", string(text))
			var last []lsp.Location
			for {
				request, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				started := time.Now()
				locations, err := client.DefinitionAt(request, serverURI(path), lsp.Position{Line: c.Line - 1, Character: c.Column - 1})
				cancel()
				last = locations
				want := filepath.Join(root, c.WantPath)
				if err == nil && len(locations) > 0 && serverPath(root, locations[0].URI) == want && locations[0].Range.Start.Line == c.WantLine-1 {
					t.Logf("PASS %s:%d:%d -> %s:%d (%s)", c.Path, c.Line, c.Column, c.WantPath, c.WantLine, time.Since(started).Round(time.Millisecond))
					if strings.Contains(locations[0].URI, ".worktrees/") && !strings.Contains(root, ".worktrees/") {
						t.Fatal("canonical path escaped into editor")
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("%s:%d:%d: got %+v, %v; wanted %s:%d", c.Path, c.Line, c.Column, last, err, want, c.WantLine)
				}
				time.Sleep(time.Second)
			}
		}
	}
}
