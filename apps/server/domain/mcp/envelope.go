package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// envelopeResult builds a uniform MCP tool result: a single text content block
// whose text is compact JSON of the form
//
//	{"ok": <bool>, "error": <string>?, "data": <tool payload>, "meta": <object>?}
//
// Contract (specs/mcp-tool-results):
//   - ok is true on success, false on failure.
//   - error is present (and non-empty) only when ok is false; omitted when ok.
//   - data carries the tool-specific payload with existing field names.
//   - meta is present only when it carries non-empty metadata (counts, etc.).
//   - message (when present) is human-readable prose only, never a status
//     signal — status is read from ok/error alone.
//
// Every envelope-producing MCP tool must build its result through this helper
// so consumers can classify success/error generically (issue #319).
func envelopeResult(ok bool, data any, meta map[string]any, errMsg string) (*ToolResult, error) {
	env := map[string]any{
		"ok":   ok,
		"data": data,
	}
	if !ok && errMsg != "" {
		env["error"] = errMsg
	}
	if len(meta) > 0 {
		env["meta"] = meta
	}

	jsonBytes, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal envelope result: %w", err)
	}

	// The structured content is the SAME object marshalled into the text block,
	// so text and structuredContent are byte-identical JSON. This is a single
	// source of truth (issue #586).
	return &ToolResult{
		Content:           []ContentBlock{{Type: "text", Text: string(jsonBytes)}},
		StructuredContent: env,
	}, nil
}

// envelopeOutputSchema returns the JSON schema for the envelope-shaped result
// produced by envelopeResult/envelopeSearchResponse. It is declared as the
// `outputSchema` on every envelope-producing tool so programmatic consumers
// can validate `structuredContent` against it.
func envelopeOutputSchema() *InputSchema {
	return &InputSchema{
		Type: "object",
		Properties: map[string]PropertySchema{
			"ok":    {Type: "boolean"},
			"error": {Type: "string"},
			"data":  {Type: "object"},
			"meta":  {Type: "object"},
		},
		Required: []string{"ok", "data"},
	}
}

// envelopeSearchResponse wraps a graph search response in the uniform result
// envelope. The slim payload ({data, total, has_more}) moves under data with
// its field names preserved. The verbose meta block is surfaced as envelope
// meta only when it was previously included (Verbose && res.Meta != nil) —
// the default slim result carries no envelope meta.
func envelopeSearchResponse(res *graph.SearchResponse, opts ResponseOpts) (*ToolResult, error) {
	payload := slimSearchResponse(res, opts)
	var meta map[string]any
	if m, ok := payload["meta"]; ok && m != nil {
		// res.Meta is a *SearchResponseMeta struct — convert to a plain map
		// so it can ride as the envelope-level meta object.
		if raw, err := json.Marshal(m); err == nil {
			_ = json.Unmarshal(raw, &meta)
		}
		delete(payload, "meta")
	}
	return envelopeResult(true, payload, meta, "")
}
