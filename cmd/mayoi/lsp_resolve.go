package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"mayoi/internal/lsp"
	"mayoi/internal/remote"
	"mayoi/internal/vfs"
)

// serverLaunch is the host-independent result of a plugin's setup command.
// The command exits before the actual language server is started.
type serverLaunch struct {
	Command          string                `json:"command"`
	Args             []string              `json:"args,omitempty"`
	Env              map[string]string     `json:"env,omitempty"`
	UnsetEnv         []string              `json:"unsetEnv,omitempty"`
	Options          json.RawMessage       `json:"initializationOptions,omitempty"`
	Configuration    map[string]any        `json:"configuration,omitempty"`
	WorkspaceFolders []lsp.WorkspaceFolder `json:"workspaceFolders,omitempty"`
	PathMappings     []lsp.PathMapping     `json:"pathMappings,omitempty"`
}

// expandServerArg substitutes whole setting references without a shell.
func expandServerArg(arg string, settings map[string]json.RawMessage) (string, error) {
	var out strings.Builder
	for {
		prefix, rest, found := strings.Cut(arg, "${settings.")
		out.WriteString(prefix)
		if !found {
			return out.String(), nil
		}
		key, tail, ok := strings.Cut(rest, "}")
		var value string
		if !ok || json.Unmarshal(settings[key], &value) != nil || value == "" {
			return "", fmt.Errorf("language server setting %q must be a nonempty string", key)
		}
		out.WriteString(value)
		arg = tail
	}
}

// limitedBuffer bounds untrusted resolver output while continuing to drain it.
type limitedBuffer struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if available := (4 << 20) - b.buffer.Len(); len(p) > available {
		b.overflow = true
		p = p[:available]
	}
	b.buffer.Write(p)
	return n, nil
}

func (b *limitedBuffer) String() string { return b.buffer.String() }

func (s *lspServer) resolve(ctx context.Context) (serverLaunch, error) {
	launch := serverLaunch{Command: s.contrib.Command, Args: s.contrib.Args, Options: s.contrib.Options}
	if s.contrib.Resolve == nil {
		return launch, nil
	}
	settings := map[string]json.RawMessage{}
	for key, value := range s.plugin.Contributes.Settings {
		settings[key] = value.Default
	}
	explicit := map[string]json.RawMessage{}
	if plugins != nil {
		json.Unmarshal(plugins.Settings(s.plugin.Name), &explicit)
	}
	delete(explicit, "enabled")
	for key, value := range explicit {
		settings[key] = value
	}
	command, err := expandServerArg(s.contrib.Resolve.Command, settings)
	if err != nil {
		return launch, err
	}
	args := make([]string, len(s.contrib.Resolve.Args))
	for i, arg := range s.contrib.Resolve.Args {
		args[i], err = expandServerArg(arg, settings)
		if err != nil {
			return launch, err
		}
	}
	root := s.root
	if _, path, ok := remote.Split(root); ok {
		root = path
	}
	input, _ := json.Marshal(map[string]any{"root": root, "settings": explicit})
	cmd, err := vfs.Command(s.root, nil, bytes.NewReader(input), command, args...)
	if err != nil {
		return launch, err
	}
	var stdout, stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = time.Second
	if err = cmd.Start(); err != nil {
		return launch, err
	}
	stop := context.AfterFunc(ctx, func() { cmd.Process.Kill() })
	err = cmd.Wait()
	stop()
	if ctx.Err() != nil {
		return launch, ctx.Err()
	}
	if err != nil {
		return launch, fmt.Errorf("%s: %w: %s", command, err, strings.TrimSpace(stderr.String()))
	}
	if stdout.overflow {
		return launch, fmt.Errorf("%s: resolver output exceeds 4 MiB", command)
	}
	launch = serverLaunch{}
	decoder := json.NewDecoder(&stdout.buffer)
	decoder.UseNumber()
	if err = decoder.Decode(&launch); err != nil {
		return launch, fmt.Errorf("%s: invalid launch description: %w", command, err)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return launch, fmt.Errorf("%s: unexpected resolver output", command)
	}
	return launch, launch.validate()
}

func (l serverLaunch) validate() error {
	if strings.TrimSpace(l.Command) == "" {
		return fmt.Errorf("resolved language server has no command")
	}
	for key := range l.Env {
		if !validEnvKey(key) {
			return fmt.Errorf("invalid environment key %q", key)
		}
	}
	for _, key := range l.UnsetEnv {
		if !validEnvKey(key) {
			return fmt.Errorf("invalid environment key %q", key)
		}
	}
	fileURI := func(value string) bool {
		u, err := url.Parse(value)
		return err == nil && u.Scheme == "file" && strings.HasPrefix(u.Path, "/")
	}
	for _, f := range l.WorkspaceFolders {
		if !fileURI(f.URI) {
			return fmt.Errorf("invalid workspace folder URI %q", f.URI)
		}
	}
	for _, m := range l.PathMappings {
		if !fileURI(m.ClientURI) || !fileURI(m.ServerURI) {
			return fmt.Errorf("path mappings must contain absolute file URIs")
		}
	}
	return nil
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func (l serverLaunch) environment() []string {
	keys := make([]string, 0, len(l.Env))
	for key := range l.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+l.Env[key])
	}
	return env
}
