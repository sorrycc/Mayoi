package lsp

import (
	"encoding/json"
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

func mapValue(value any, mappings []PathMapping, toServer bool) any {
	switch v := value.(type) {
	case string:
		return mappedURI(v, mappings, toServer)
	case []any:
		for i := range v {
			v[i] = mapValue(v[i], mappings, toServer)
		}
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			switch key {
			case "text", "newText", "insertText", "documentation", "message":
				// Source and user-facing prose are not URI fields.
			default:
				item = mapValue(item, mappings, toServer)
			}
			out[mappedURI(key, mappings, toServer)] = item
		}
		return out
	}
	return value
}

func mapJSON(data json.RawMessage, mappings []PathMapping, toServer bool) json.RawMessage {
	if len(mappings) == 0 || len(data) == 0 {
		return data
	}
	var value any
	if json.Unmarshal(data, &value) != nil {
		return data
	}
	out, err := json.Marshal(mapValue(value, mappings, toServer))
	if err != nil {
		return data
	}
	return out
}
