package agents

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// Bounded tool-result policy (issue #1205).
//
// Tool results are fed back into the model context verbatim, so an unbounded
// result (a 361 KB entity-query/entity-edges-get payload was observed) inflates
// every subsequent model call and makes per-step latency grow with the run.
// This file applies a single, central bound at the tool-result → model boundary:
// the *model request only* (model.LLMRequest.Contents) is truncated. The full
// payload is untouched everywhere it is persisted or streamed — the ADK session
// events, kb.agent_run_tool_calls, kb.agent_run_messages rows and the
// StreamEventToolCallEnd SSE event all keep the complete result. ADK deep-clones
// session content into LLMRequest.Contents before the before-model callback, so
// mutating the request cannot corrupt the persisted transcript.
//
// The bound is deliberately honest: a truncated result carries an explicit,
// actionable marker the model can act on, never a silent drop.
const (
	// defaultToolResultMaxBytes bounds one tool result as seen by the model.
	// 128 KiB is generous: normal read/search results observed in production
	// (11–56 KB) pass through unchanged; only pathological payloads (e.g. a
	// 361 KB entity-edges-get) are truncated.
	defaultToolResultMaxBytes = 128 << 10

	// defaultToolResultTotalBudgetBytes bounds the SUM of all tool results in a
	// single model request. When exceeded, the oldest results are elided
	// (most-recent retained) so per-step context does not grow with run length.
	//
	// Rationale for the defaults (issue #1205): 128 KiB ≈ 32k tokens per result
	// and 512 KiB ≈ 128k tokens total. The per-result cap removes the pathological
	// single payload; the total budget only trims the extreme tail once more than
	// four capped results have accumulated in one request.
	//
	// The budget is SOFT for a single oversized newest result: older results are
	// elided first, and if the most recent result alone still exceeds the budget
	// it is retained whole rather than silently dropped (see the total-budget
	// pass in boundModelRequestToolResults).
	defaultToolResultTotalBudgetBytes = 512 << 10
)

// truncationMarkerFmt is appended to a bounded result. It tells the model what
// it is looking at and how to get the rest narrowly, so it never answers as if
// the visible prefix were the whole payload.
const truncationMarkerFmt = "\u2026 [truncated: kept %d of %d bytes; narrow with filters/fields[]/limit/offset or fetch a specific key]"

// elisionMarkerFmt replaces an older tool result dropped to stay within the
// total model-context budget.
const elisionMarkerFmt = "\u2026 [elided: older tool result (%d bytes) omitted to bound the model context; re-run the tool with filters/fields[]/limit/offset if you need it]"

// ToolResultBounds is the resolved per-result / total policy applied to tool
// results before they enter the model context. A zero MaxBytes or
// TotalBudgetBytes falls back to the package defaults; a negative value
// disables that layer.
type ToolResultBounds struct {
	// MaxBytes is the per-result cap (JSON bytes). Zero → default; <0 → disabled.
	// A negative value disables the layer for every tool and also ignores
	// PerToolMaxBytes (a per-tool override cannot re-enable a globally disabled
	// cap).
	MaxBytes int
	// PerToolMaxBytes overrides MaxBytes for named tools (positive values only).
	// The override applies only while the global per-result cap is enabled
	// (MaxBytes >= 0).
	PerToolMaxBytes map[string]int
	// TotalBudgetBytes is the cap on the sum of all tool results in one model
	// request. Zero → default; <0 → disabled. The budget is soft: older results
	// are elided first, but the most recent result is never elided, so a single
	// newest result larger than the budget is retained whole.
	TotalBudgetBytes int
}

// toolResultBoundsFromConfig resolves the model-context bound from config. Zero
// values remain zero here and fall back to the package defaults at use time, so
// a server configured without these env vars still gets a generous bound.
func toolResultBoundsFromConfig(cfg config.MCPConfig) ToolResultBounds {
	return ToolResultBounds{
		MaxBytes:         cfg.ToolResultMaxBytes,
		PerToolMaxBytes:  parseToolResultMaxBytesOverrides(cfg.ToolResultMaxBytesOverrides),
		TotalBudgetBytes: cfg.ToolResultTotalBudgetBytes,
	}
}

func (b ToolResultBounds) maxBytesFor(tool string) int {
	// A negative global cap disables per-result bounding outright: a per-tool
	// override must not re-enable a layer the operator turned off.
	if b.MaxBytes < 0 {
		return b.MaxBytes
	}
	if v, ok := b.PerToolMaxBytes[tool]; ok && v > 0 {
		return v
	}
	if b.MaxBytes > 0 {
		return b.MaxBytes
	}
	return defaultToolResultMaxBytes
}

func (b ToolResultBounds) totalBudget() int {
	if b.TotalBudgetBytes != 0 {
		return b.TotalBudgetBytes
	}
	return defaultToolResultTotalBudgetBytes
}

// ToolResultBoundStats summarises what boundModelRequestToolResults changed.
type ToolResultBoundStats struct {
	TruncatedResults int
	ElidedResults    int
	BytesBefore      int
	BytesAfter       int
}

// boundModelRequestToolResults mutates req in place, bounding the
// FunctionResponse payloads the model will see. It returns stats for logging.
// It never panics on nil/empty requests or parts.
func boundModelRequestToolResults(req *model.LLMRequest, bounds ToolResultBounds) ToolResultBoundStats {
	var stats ToolResultBoundStats
	if req == nil {
		return stats
	}

	type frPart struct {
		part *genai.Part
		size int
	}
	var parts []frPart

	// Pass 1: per-result bound, oldest-first in contents order.
	for _, content := range req.Contents {
		if content == nil {
			continue
		}
		for _, part := range content.Parts {
			if part == nil || part.FunctionResponse == nil || part.FunctionResponse.Response == nil {
				continue
			}
			toolName := part.FunctionResponse.Name
			resp := part.FunctionResponse.Response
			encoded, err := json.Marshal(resp)
			if err != nil {
				continue
			}
			stats.BytesBefore += len(encoded)

			bounded, changed := boundToolResultResponse(toolName, resp, bounds.maxBytesFor(toolName))
			if changed {
				part.FunctionResponse.Response = bounded
				stats.TruncatedResults++
				encoded, err = json.Marshal(bounded)
				if err != nil {
					continue
				}
			}
			parts = append(parts, frPart{part: part, size: len(encoded)})
		}
	}

	// Pass 2: total budget. Elide oldest results first; never elide the most
	// recent result (index len(parts)-1), which the current step is reasoning
	// about. The replacement is written back onto the part's FunctionResponse so
	// the model sees the marker, not the dropped payload.
	//
	// The budget is soft by design: if the most recent result alone exceeds the
	// budget (reachable when total < per-result, or when the per-result layer is
	// disabled), every older result is elided and the newest is still retained
	// whole. Silently dropping the result the current step is reasoning about
	// would be worse than the extra tokens, and the caller can always tighten the
	// per-result cap. The loop below therefore stops before the newest part and
	// leaves the sum above budget in that single-result case.
	if budget := bounds.totalBudget(); budget > 0 && len(parts) > 0 {
		sum := 0
		for _, p := range parts {
			sum += p.size
		}
		for i := 0; i < len(parts)-1 && sum > budget; i++ {
			before := parts[i].size
			elided := elidedToolResultResponse(parts[i].part.FunctionResponse.Response)
			encoded, err := json.Marshal(elided)
			if err != nil {
				continue
			}
			parts[i].part.FunctionResponse.Response = elided
			parts[i].size = len(encoded)
			sum += len(encoded) - before
			stats.ElidedResults++
		}
	}

	// Recompute sizes after both passes.
	stats.BytesAfter = 0
	for _, content := range req.Contents {
		if content == nil {
			continue
		}
		for _, part := range content.Parts {
			if part == nil || part.FunctionResponse == nil || part.FunctionResponse.Response == nil {
				continue
			}
			stats.BytesAfter += jsonSize(part.FunctionResponse.Response)
		}
	}
	return stats
}

// boundToolResultResponse returns resp unchanged when its JSON encoding fits
// maxBytes. Otherwise it returns a bounded envelope carrying the actionable
// truncation marker and a preview prefix. It never mutates resp. A negative
// maxBytes disables bounding.
func boundToolResultResponse(toolName string, resp map[string]any, maxBytes int) (map[string]any, bool) {
	if resp == nil || maxBytes < 0 {
		return resp, false
	}
	encoded, err := json.Marshal(resp)
	if err != nil || len(encoded) <= maxBytes {
		return resp, false
	}
	_ = toolName // reserved for future per-tool marker wording

	total := len(encoded)
	okVal := any(true)
	if v, ok := resp["ok"]; ok {
		okVal = v
	}

	preview := safeUTF8Prefix(encoded, maxBytes)
	for {
		env := map[string]any{
			"ok":         okVal,
			"truncated":  true,
			"truncation": fmt.Sprintf(truncationMarkerFmt, len(preview), total),
			"preview":    preview,
		}
		b, err := json.Marshal(env)
		if err != nil {
			// Fail open: an unmarshalable envelope must not make the tool call
			// fail; hand the model the original (still bounded by nothing, but
			// this path is unreachable for JSON-derived maps).
			return resp, false
		}
		if len(b) <= maxBytes {
			return env, true
		}
		// Shrink the preview to make room for the envelope overhead.
		shrink := len(b) - maxBytes
		if shrink < 1 {
			shrink = 1
		}
		if len(preview) <= shrink {
			preview = ""
		} else {
			preview = safeUTF8Prefix([]byte(preview), len(preview)-shrink)
		}
		if preview == "" {
			// Envelope fixed overhead alone exceeds the cap (an absurdly small
			// configured cap). Return the marker envelope; it is bounded by the
			// fixed overhead.
			return map[string]any{
				"ok":         okVal,
				"truncated":  true,
				"truncation": fmt.Sprintf(truncationMarkerFmt, 0, total),
				"preview":    "",
			}, true
		}
	}
}

// elidedToolResultResponse replaces an older tool result with a short, marked
// placeholder. It preserves ok so success/failure classification survives.
func elidedToolResultResponse(resp map[string]any) map[string]any {
	okVal := any(true)
	size := 0
	if resp != nil {
		if v, ok := resp["ok"]; ok {
			okVal = v
		}
		size = jsonSize(resp)
	}
	return map[string]any{
		"ok":         okVal,
		"elided":     true,
		"truncation": fmt.Sprintf(elisionMarkerFmt, size),
	}
}

// safeUTF8Prefix returns the longest valid UTF-8 prefix of b no longer than n
// bytes.
func safeUTF8Prefix(b []byte, n int) string {
	if n >= len(b) {
		return string(b)
	}
	if n < 0 {
		n = 0
	}
	b = b[:n]
	// Trim at most a few continuation bytes to land on a rune boundary.
	for i := 0; i < 4 && len(b) > 0 && !utf8.Valid(b); i++ {
		b = b[:len(b)-1]
	}
	return string(b)
}

// jsonSize returns the JSON-encoded byte length of v, or 0 when v cannot be
// encoded.
func jsonSize(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(b)
}

// parseToolResultMaxBytesOverrides parses "name=bytes,name=bytes" into a map.
// Empty, malformed, non-numeric or non-positive entries are dropped.
func parseToolResultMaxBytesOverrides(s string) map[string]int {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	out := make(map[string]int)
	for _, entry := range strings.Split(s, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name, val, ok := strings.Cut(entry, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil || n <= 0 {
			continue
		}
		out[name] = n
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
