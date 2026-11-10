package lsp

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/url"
	"sort"
	"strings"
)

// WorkspaceFolder is a workspace the server imports.
type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// PathMapping preserves the path a person opened when a server resolves links.
// URI prefixes are matched by path component, not by substring.
type PathMapping struct {
	ClientURI string `json:"clientURI"`
	ServerURI string `json:"serverURI"`
}

func mappedURI(value string, mappings []PathMapping, toServer bool) string {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "file" {
		return value
	}
	for _, mapping := range mappings {
		from, to := mapping.ServerURI, mapping.ClientURI
		if toServer {
			from, to = mapping.ClientURI, mapping.ServerURI
		}
		f, e1 := url.Parse(from)
		t, e2 := url.Parse(to)
		if e1 != nil || e2 != nil || f.Scheme != "file" || t.Scheme != "file" || !strings.EqualFold(u.Host, f.Host) {
			continue
		}
		prefix := strings.TrimRight(f.Path, "/")
		if u.Path != prefix && !strings.HasPrefix(u.Path, prefix+"/") {
			continue
		}
		u.Host = t.Host
		u.Path = strings.TrimRight(t.Path, "/") + strings.TrimPrefix(u.Path, prefix)
		u.RawPath = ""
		return u.String()
	}
	return value
}

func sortedMappings(mappings []PathMapping, toServer bool) []PathMapping {
	var out []PathMapping
	for _, mapping := range mappings {
		if mapping.ClientURI != mapping.ServerURI {
			out = append(out, mapping)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if toServer {
			return len(out[i].ClientURI) > len(out[j].ClientURI)
		}
		return len(out[i].ServerURI) > len(out[j].ServerURI)
	})
	return out
}

// uriFields describes only URI-bearing portions of a protocol message. A nil
// child is a URI string, "*" selects array elements, and "$keys" maps the keys
// of WorkspaceEdit.changes. Unlisted fields are opaque JSON, including source,
// markup, server extension data, initialization options and configuration.
type uriFields map[string]uriFields

var documentURI = uriFields{"textDocument": {"uri": nil}}
var folderURIs = uriFields{"*": {"uri": nil}}
var locationURIs = uriFields{
	"uri": nil, "targetUri": nil,
	"*": {"uri": nil, "targetUri": nil},
}
var editURIs = uriFields{
	"changes": {"$keys": {}},
	"documentChanges": {"*": {
		"textDocument": {"uri": nil}, "uri": nil, "oldUri": nil, "newUri": nil,
	}},
}

func parameterURIs(method string) uriFields {
	switch method {
	case "initialize":
		return uriFields{"rootUri": nil, "workspaceFolders": folderURIs}
	case "textDocument/didOpen", "textDocument/didChange", "textDocument/didSave", "textDocument/didClose",
		"textDocument/hover", "textDocument/definition", "textDocument/completion", "textDocument/formatting":
		return documentURI
	case "textDocument/publishDiagnostics":
		return uriFields{"uri": nil, "diagnostics": {"*": {
			"relatedInformation": {"*": {"location": {"uri": nil}}},
		}}}
	case "workspace/configuration":
		return uriFields{"items": {"*": {"scopeUri": nil}}}
	case "workspace/applyEdit":
		return uriFields{"edit": editURIs}
	case "workspace/didChangeWorkspaceFolders":
		return uriFields{"event": {"added": folderURIs, "removed": folderURIs}}
	case "workspace/didChangeWatchedFiles":
		return uriFields{"changes": folderURIs}
	default:
		return nil
	}
}

func mapParams(method string, data json.RawMessage, mappings []PathMapping, toServer bool) json.RawMessage {
	fields := parameterURIs(method)
	if fields == nil {
		return data
	}
	return mapURIFields(data, fields, mappings, toServer)
}

// Outgoing parameters are already Go values. Map their small URI-bearing
// branches before encoding, so a full document's source is encoded only once.
func mapParameterValues(method string, params any, mappings []PathMapping) any {
	fields := parameterURIs(method)
	if len(mappings) == 0 || fields == nil {
		return params
	}
	mapped, _ := mapURIValues(params, fields, mappings)
	return mapped
}

func mapURIValues(value any, fields uriFields, mappings []PathMapping) (any, bool) {
	if fields == nil {
		if s, ok := value.(string); ok {
			mapped := mappedURI(s, mappings, true)
			return mapped, mapped != s
		}
		return value, false
	}
	switch v := value.(type) {
	case map[string]any:
		var out map[string]any
		if _, keys := fields["$keys"]; keys {
			for key := range v {
				if target := mappedURI(key, mappings, true); target != key {
					if out == nil {
						out = maps.Clone(v)
					}
					delete(out, key)
					out[target] = v[key]
				}
			}
		} else {
			for key, child := range fields {
				if item, ok := v[key]; ok {
					if mapped, changed := mapURIValues(item, child, mappings); changed {
						if out == nil {
							out = maps.Clone(v)
						}
						out[key] = mapped
					}
				}
			}
		}
		if out != nil {
			return out, true
		}
		return value, false
	case []any:
		child, ok := fields["*"]
		if !ok {
			return value, false
		}
		var out []any
		for i, item := range v {
			if mapped, changed := mapURIValues(item, child, mappings); changed {
				if out == nil {
					out = append([]any(nil), v...)
				}
				out[i] = mapped
			}
		}
		if out != nil {
			return out, true
		}
		return value, false
	default:
		// Typed protocol values, such as []WorkspaceFolder, and RawMessages
		// use the same field rules without decoding their opaque data.
		raw, err := json.Marshal(value)
		if err != nil {
			return value, false
		}
		mapped := mapURIFields(raw, fields, mappings, true)
		if !bytes.Equal(mapped, raw) {
			return mapped, true
		}
		return value, false
	}
}

func mapResult(method string, data json.RawMessage, mappings []PathMapping, toServer bool) json.RawMessage {
	switch method {
	case "textDocument/definition":
		return mapURIFields(data, locationURIs, mappings, toServer)
	case "workspace/workspaceFolders":
		return mapURIFields(data, folderURIs, mappings, toServer)
	default:
		return data
	}
}

func mapURIFields(data json.RawMessage, fields uriFields, mappings []PathMapping, toServer bool) json.RawMessage {
	if len(mappings) == 0 || len(data) == 0 {
		return data
	}
	if fields == nil {
		var value string
		if json.Unmarshal(data, &value) != nil {
			return data
		}
		mapped := mappedURI(value, mappings, toServer)
		if mapped == value {
			return data
		}
		out, _ := json.Marshal(mapped)
		return out
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return data
	}
	var value any
	changed := false
	switch trimmed[0] {
	case '[':
		child, ok := fields["*"]
		if !ok {
			return data
		}
		var list []json.RawMessage
		if json.Unmarshal(data, &list) != nil {
			return data
		}
		for i, item := range list {
			mapped := mapURIFields(item, child, mappings, toServer)
			if !bytes.Equal(mapped, item) {
				list[i] = mapped
				changed = true
			}
		}
		value = list
	case '{':
		var object map[string]json.RawMessage
		if json.Unmarshal(data, &object) != nil {
			return data
		}
		if _, keys := fields["$keys"]; keys {
			mapped := make(map[string]json.RawMessage, len(object))
			for key, item := range object {
				target := mappedURI(key, mappings, toServer)
				if target != key {
					changed = true
				}
				mapped[target] = item
			}
			object = mapped
		} else {
			for key, child := range fields {
				if item, ok := object[key]; ok {
					mapped := mapURIFields(item, child, mappings, toServer)
					if !bytes.Equal(mapped, item) {
						object[key] = mapped
						changed = true
					}
				}
			}
		}
		value = object
	default:
		return data
	}
	if !changed {
		return data
	}
	out, err := json.Marshal(value)
	if err != nil {
		return data
	}
	return out
}
