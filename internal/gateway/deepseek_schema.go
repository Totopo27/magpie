package gateway

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/yetone/magpie/internal/provider"
)

// deepseekToolPatterns normalizes the ECMAScript null escape \0 in tool schemas
// to the Unicode escape \u0000. DeepSeek's schema validator rejects \0 with:
// "Invalid schema for function ... is not a 'regex'".
//
// To avoid deserializing and re-encoding multi-megabyte payloads (such as
// conversation histories containing base64 images), it scans only the "tools"
// field using gjson and splices the normalized tools back into the original body.
func deepseekToolPatterns(p provider.Provider, to provider.Protocol, body []byte) []byte {
	if p.Preset != "deepseek" && provider.HostOf(p.Base(to)) != "api.deepseek.com" {
		return body
	}
	if !bytes.Contains(body, []byte(`\\0`)) {
		return body
	}
	toolsRes := gjson.GetBytes(body, "tools")
	if !toolsRes.Exists() || toolsRes.Index <= 0 || !toolsRes.IsArray() {
		return body
	}

	var tools []any
	dec := json.NewDecoder(strings.NewReader(toolsRes.Raw))
	dec.UseNumber()
	if err := dec.Decode(&tools); err != nil {
		return body
	}

	changed := false
	for _, value := range tools {
		tool, ok := value.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"input_schema", "parameters"} {
			changed = normalizeNullPatterns(tool[key]) || changed
		}
		if fn, ok := tool["function"].(map[string]any); ok {
			changed = normalizeNullPatterns(fn["parameters"]) || changed
		}
	}
	if !changed {
		return body
	}

	normTools, err := json.Marshal(tools)
	if err != nil {
		return body
	}

	out := make([]byte, 0, len(body)-len(toolsRes.Raw)+len(normTools))
	out = append(out, body[:toolsRes.Index]...)
	out = append(out, normTools...)
	out = append(out, body[toolsRes.Index+len(toolsRes.Raw):]...)
	return out
}

func normalizeNullPatterns(value any) bool {
	changed := false
	switch node := value.(type) {
	case map[string]any:
		if pattern, ok := node["pattern"].(string); ok {
			if normalized := unicodeNullEscape(pattern); normalized != pattern {
				node["pattern"], changed = normalized, true
			}
		}
		for key, child := range node {
			switch key {
			case "properties", "$defs", "definitions", "dependentSchemas", "patternProperties":
				if entries, ok := child.(map[string]any); ok {
					for _, schema := range entries {
						changed = normalizeNullPatterns(schema) || changed
					}
				}
			case "items", "additionalProperties", "contains", "propertyNames", "not", "if", "then", "else", "allOf", "anyOf", "oneOf", "prefixItems", "unevaluatedProperties", "unevaluatedItems", "contentSchema":
				changed = normalizeNullPatterns(child) || changed
			}
		}
	case []any:
		for _, child := range node {
			changed = normalizeNullPatterns(child) || changed
		}
	}
	return changed
}

func unicodeNullEscape(pattern string) string {
	var out strings.Builder
	for i := 0; i < len(pattern); i++ {
		if pattern[i] != '\\' || i+1 == len(pattern) {
			out.WriteByte(pattern[i])
			continue
		}
		next := pattern[i+1]
		// A pair of backslashes is literal; an octal escape has its own
		// meaning and must not be shortened to a null character.
		if next == '0' && (i+2 == len(pattern) || pattern[i+2] < '0' || pattern[i+2] > '9') {
			out.WriteString(`\u0000`)
		} else {
			out.WriteByte('\\')
			out.WriteByte(next)
		}
		i++
	}
	return out.String()
}
