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
	"mayoi/internal/plugin"
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

// serverSettings are the settings of a plugin: those stored, explicit, and
// with the manifest's defaults for the others, all.
func serverSettings(p *plugin.Plugin) (all, explicit map[string]json.RawMessage) {
	all = map[string]json.RawMessage{}
	for key, value := range p.Contributes.Settings {
		all[key] = value.Default
	}
	explicit = map[string]json.RawMessage{}
	if plugins != nil {
		json.Unmarshal(plugins.Settings(p.Name), &explicit)
	}
	delete(explicit, "enabled")
	for key, value := range explicit {
		all[key] = value
	}
	return all, explicit
}

// expandServerCommand substitutes the settings in a command and its
// arguments.
func expandServerCommand(command string, args []string, settings map[string]json.RawMessage) (string, []string, error) {
	command, err := expandServerArg(command, settings)
	if err != nil {
		return "", nil, err
	}
	out := make([]string, len(args))
	for i, arg := range args {
		if out[i], err = expandServerArg(arg, settings); err != nil {
			return "", nil, err
		}
	}
	return command, out, nil
}

// serverProgram is the program a plain server's command starts, with its
// plugin's settings, "" for one that resolves or a setting left empty.
func serverProgram(p *plugin.Plugin, s *plugin.ServerContrib) string {
	if s.Resolve != nil {
		return ""
	}
	settings, _ := serverSettings(p)
	command, err := expandServerArg(s.Command, settings)
	if err != nil {
		return ""
	}
	return command
}

func (s *lspServer) resolve(ctx context.Context) (serverLaunch, error) {
	settings, explicit := serverSettings(s.plugin)
	if s.contrib.Resolve == nil {
		command, args, err := expandServerCommand(s.contrib.Command, s.contrib.Args, settings)
		return serverLaunch{Command: command, Args: args, Options: s.contrib.Options}, err
	}
	launch := serverLaunch{Command: s.contrib.Command, Args: s.contrib.Args, Options: s.contrib.Options}
	command, args, err := expandServerCommand(s.contrib.Resolve.Command, s.contrib.Resolve.Args, settings)
	if err != nil {
		return launch, err
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
		return launch, fmt.Errorf("%s did not finish in time", command)
	}
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return launch, fmt.Errorf("%s: %w: %s", command, err, msg)
		}
		return launch, fmt.Errorf("%s: %w", command, err)
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
