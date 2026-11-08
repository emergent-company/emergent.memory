package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"
)

// maxClientNameRunes bounds the client name carried into tool calls. The value
// comes from the initialize params, is client-controlled, and is rendered into
// a GitHub issue body, so it is treated as untrusted display text: trimmed,
// stripped of control characters and capped.
const maxClientNameRunes = 64

// clientNameContextKey is the unexported key for the MCP client name carried on
// the tools/call context. A context value (rather than a package map) keeps two
// sessions from bleeding into each other.
type clientNameContextKey struct{}

// withClientName attaches the sanitized client name to ctx. An empty or
// unstorable name leaves the context unchanged, and the tool falls back to
// "MCP".
func withClientName(ctx context.Context, name string) context.Context {
	name = sanitizeClientName(name)
	if name == "" {
		return ctx
	}
	return context.WithValue(ctx, clientNameContextKey{}, name)
}

// clientNameFromContext returns the session's client name, or "" when the
// transport has none (for example the per-agent endpoint, which has no session
// map).
func clientNameFromContext(ctx context.Context) string {
	name, _ := ctx.Value(clientNameContextKey{}).(string)
	return name
}

// sanitizeClientName makes an untrusted clientInfo.name safe for a display row:
// it replaces control characters (including newlines and tabs, which would
// break the markdown table) with spaces, collapses whitespace runs, trims, and
// caps the result at maxClientNameRunes runes.
func sanitizeClientName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	runes := []rune(name)
	if len(runes) > maxClientNameRunes {
		name = string(runes[:maxClientNameRunes])
	}
	return strings.TrimSpace(name)
}

// initializeClientName extracts clientInfo.name from an initialize request.
// Malformed params yield "" and the initialize handler reports the real error.
func initializeClientName(req *Request) string {
	if req == nil || len(req.Params) == 0 {
		return ""
	}
	var params InitializeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return ""
	}
	return params.ClientInfo.Name
}
