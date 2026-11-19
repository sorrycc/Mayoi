package lsp

import (
	"context"
	"encoding/json"
	"slices"
)

// Navigation is a request for locations at a position.
type Navigation string

const (
	Definition     Navigation = "textDocument/definition"
	TypeDefinition Navigation = "textDocument/typeDefinition"
	Implementation Navigation = "textDocument/implementation"
	References     Navigation = "textDocument/references"
)

func (c *Client) CanNavigate(kind Navigation) bool {
	switch kind {
	case Definition:
		return c.Definition()
	case TypeDefinition:
		return c.caps.TypeDefinition
	case Implementation:
		return c.caps.Implementation
	case References:
		return c.caps.References
	}
	return false
}

func (c *Client) SupportsCommand(name string) bool {
	return name != "" && slices.Contains(c.caps.Commands, name)
}

func (c *Client) NavigateAt(ctx context.Context, kind Navigation, uri string, pos Position) ([]Location, error) {
	params := docPos(uri, pos)
	if kind == References {
		params["context"] = map[string]bool{"includeDeclaration": true}
	}
	res, err := c.Call(ctx, string(kind), params)
	if err != nil {
		return nil, err
	}
	return locations(res)
}

// SourceDefinitionAt calls the source lookup declared by the plugin. Only
// this command's URI argument and location result get path mappings.
func (c *Client) SourceDefinitionAt(ctx context.Context, command, uri string, pos Position) ([]Location, error) {
	res, err := c.Call(ctx, "workspace/executeCommand", map[string]any{
		"command": command, "arguments": []any{mappedURI(uri, c.toServer, true), pos},
	})
	if err != nil {
		return nil, err
	}
	return locations(mapURIFields(res, locationURIs, c.toClient, false))
}

func locations(res json.RawMessage) ([]Location, error) {
	if len(res) == 0 || string(res) == "null" {
		return nil, nil
	}
	var one Location
	if json.Unmarshal(res, &one) == nil && one.URI != "" {
		return []Location{one}, nil
	}
	var many []struct {
		Location
		TargetURI string `json:"targetUri"`
		Selection Range  `json:"targetSelectionRange"`
	}
	if err := json.Unmarshal(res, &many); err != nil {
		return nil, err
	}
	out := make([]Location, 0, len(many))
	for _, l := range many {
		if l.TargetURI != "" {
			out = append(out, Location{URI: l.TargetURI, Range: l.Selection})
		} else if l.URI != "" {
			out = append(out, l.Location)
		}
	}
	return out, nil
}
