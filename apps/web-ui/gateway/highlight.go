package main

import (
	"bytes"
	"encoding/json"
	"html"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// jsonHighlightStyle is the chroma theme used for tool-result JSON. github-dark
// matches the syntax highlighting go-daisy already uses for code blocks
// elsewhere in the app.
var jsonHighlightStyle = styles.Get("github-dark")

// highlightJSONValue pretty-prints and syntax-highlights a JSON tool result,
// recursively unwrapping double-encoded string fields ({"result":"[...]"}) so
// they render as structured JSON instead of a blob of \n escapes. It returns a
// fragment of bare inline-styled <span> tokens (no <pre> wrapper — the client
// supplies its own container) and true only when raw is a non-empty JSON
// object or array. For empty/invalid input or JSON scalars it returns
// ("", false) so callers fall back to plain rendering.
func highlightJSONValue(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", false
	}
	switch v.(type) {
	case map[string]any, []any:
	default:
		return "", false
	}
	v = unwrapJSONStrings(v, 0)
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", false
	}
	return highlightJSON(string(pretty)), true
}

// highlightJSON syntax-highlights JSON source into bare inline-styled <span>
// tokens (no <pre> wrapper). Falls back to HTML-escaped plain text on failure.
func highlightJSON(src string) string {
	l := lexers.Get("json")
	if l == nil {
		l = lexers.Fallback
	}
	l = chroma.Coalesce(l)

	it, err := l.Tokenise(nil, src)
	if err != nil {
		return html.EscapeString(src)
	}

	formatter := chromahtml.New(
		chromahtml.WithClasses(false),
		chromahtml.PreventSurroundingPre(true),
		chromahtml.TabWidth(2),
	)
	var buf bytes.Buffer
	if err := formatter.Format(&buf, jsonHighlightStyle, it); err != nil {
		return html.EscapeString(src)
	}
	return buf.String()
}

// unwrapJSONStrings recursively replaces JSON string values that themselves
// parse as a JSON object/array with the parsed value, fixing double-encoded
// results like {"ok":true,"result":"[{...}]"}. Scalar parses ("123", "true",
// "null") are left as strings so plain text survives, and depth caps runaway
// recursion.
func unwrapJSONStrings(v any, depth int) any {
	if depth >= 8 {
		return v
	}
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = unwrapJSONStrings(val, depth+1)
		}
		return t
	case []any:
		for i := range t {
			t[i] = unwrapJSONStrings(t[i], depth+1)
		}
		return t
	case string:
		s := strings.TrimSpace(t)
		if len(s) >= 2 && (s[0] == '{' || s[0] == '[') {
			var inner any
			if err := json.Unmarshal([]byte(s), &inner); err == nil {
				switch inner.(type) {
				case map[string]any, []any:
					return unwrapJSONStrings(inner, depth+1)
				}
			}
		}
		return t
	default:
		return v
	}
}
