package gateway

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"github.com/yetone/magpie/internal/provider"
)

// deepseekToolPatterns normalizes the ECMAScript null escape \0 in tool schemas
// to the Unicode escape \u0000. DeepSeek's schema validator rejects \0 with:
// "Invalid schema for function ... is not a 'regex'".
//
// To avoid re-encoding the entire payload or tool array (which reorders keys,
// escapes <, > and &, and breaks prompt caching), it finds only the offending
// "pattern" fields in tool schemas using gjson and updates them in place using sjson.
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

	var targets []patternTarget
	collectNullPatterns(toolsRes, "tools", false, &targets)
	if len(targets) == 0 {
		return body
	}

	out := body
	for _, t := range targets {
		var err error
		out, err = sjson.SetBytes(out, t.path, t.norm)
		if err != nil {
			return body
		}
	}
	return out
}

type patternTarget struct {
	path string
	norm string
}

func escapePathKey(k string) string {
	var b strings.Builder
	for _, r := range k {
		if r == '.' || r == '*' || r == '?' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func collectNullPatterns(node gjson.Result, pathPrefix string, inSchema bool, targets *[]patternTarget) {
	if !node.Exists() {
		return
	}
	if node.IsObject() {
		node.ForEach(func(key, value gjson.Result) bool {
			k := key.String()
			childPath := escapePathKey(k)
			if pathPrefix != "" {
				childPath = pathPrefix + "." + childPath
			}

			if inSchema {
				if k == "pattern" && value.Type == gjson.String {
					var pattern string
					if err := json.Unmarshal([]byte(value.Raw), &pattern); err == nil {
						if norm := unicodeNullEscape(pattern); norm != pattern {
							*targets = append(*targets, patternTarget{
								path: childPath,
								norm: norm,
							})
						}
					}
				}
				switch k {
				case "properties", "$defs", "definitions", "dependentSchemas", "patternProperties":
					if value.IsObject() {
						value.ForEach(func(propKey, schema gjson.Result) bool {
							collectNullPatterns(schema, childPath+"."+escapePathKey(propKey.String()), true, targets)
							return true
						})
					}
				case "items", "additionalProperties", "contains", "propertyNames", "not", "if", "then", "else", "allOf", "anyOf", "oneOf", "prefixItems", "unevaluatedProperties", "unevaluatedItems", "contentSchema":
					collectNullPatterns(value, childPath, true, targets)
				}
			} else {
				if k == "input_schema" || k == "parameters" {
					collectNullPatterns(value, childPath, true, targets)
				} else if k == "function" {
					collectNullPatterns(value, childPath, false, targets)
				}
			}
			return true
		})
	} else if node.IsArray() {
		idx := 0
		node.ForEach(func(_, item gjson.Result) bool {
			childPath := pathPrefix + "." + strconv.Itoa(idx)
			collectNullPatterns(item, childPath, inSchema, targets)
			idx++
			return true
		})
	}
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
