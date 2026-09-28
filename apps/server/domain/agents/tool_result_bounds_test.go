package agents

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// functionResponseRequest builds a model request whose contents contain one
// FunctionResponse part per (name, response) pair, oldest first.
func functionResponseRequest(pairs ...[2]any) *model.LLMRequest {
	req := &model.LLMRequest{}
	for _, p := range pairs {
		name, _ := p[0].(string)
		resp, _ := p[1].(map[string]any)
		req.Contents = append(req.Contents, &genai.Content{
			Role: "user",
			Parts: []*genai.Part{
				{FunctionResponse: &genai.FunctionResponse{Name: name, Response: resp}},
			},
		})
	}
	return req
}

func mustJSONSize(t *testing.T, v any) int {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return len(b)
}

// A result that already fits the cap must be returned unchanged: tools that
// legitimately need full content within the cap are not disturbed.
func TestBoundToolResultResponse_UnderCapUnchanged(t *testing.T) {
	resp := map[string]any{
		"ok":    true,
		"items": []any{"a", "b"},
	}
	got, changed := boundToolResultResponse("entity-query", resp, 64<<10)

	assert.False(t, changed)
	assert.Equal(t, resp, got)
}

// An oversized result is replaced by a bounded envelope carrying an actionable
// truncation marker. The bound must hold regardless of how large the input was.
func TestBoundToolResultResponse_OversizedBoundedWithActionableMarker(t *testing.T) {
	const cap = 8 << 10
	resp := map[string]any{
		"ok":       true,
		"blob":     strings.Repeat("x", 200<<10),
		"entities": []any{"a", "b", "c"},
	}
	original := mustJSONSize(t, resp)
	require.Greater(t, original, cap)

	got, changed := boundToolResultResponse("entity-query", resp, cap)

	require.True(t, changed)
	require.NotNil(t, got)
	assert.LessOrEqual(t, mustJSONSize(t, got), cap, "bounded envelope must fit the cap")

	// The marker must be part of the result the model sees, and actionable.
	assert.Equal(t, true, got["truncated"])
	marker, _ := got["truncation"].(string)
	require.NotEmpty(t, marker)
	assert.Contains(t, marker, "truncated")
	assert.Contains(t, marker, "narrow with filters/fields[]/limit/offset")
	assert.Contains(t, marker, "kept")
	assert.Contains(t, marker, "of")
	assert.NotEmpty(t, got["preview"], "bounded envelope must keep a prefix the model can inspect")

	// ok is preserved so a truncated success is still classifiable.
	assert.Equal(t, true, got["ok"])
}

// Representative oversized result: an entity-query full-strategy payload of ~25
// rows of ~14 KB each, the shape behind the observed 361 KB / 87 s step. The
// bound must reduce it to at most the default per-result cap while keeping an
// actionable marker. Run with -v for the before/after sizes.
func TestBoundToolResultResponse_RepresentativeOversized(t *testing.T) {
	rows := make([]any, 0, 25)
	content := strings.Repeat("legal paragraph text. ", 700) // ~14 KB
	for i := 0; i < 25; i++ {
		rows = append(rows, map[string]any{
			"id":    "entity-id-00000000000000000000",
			"key":   "law#section",
			"name":  "paragraph",
			"props": map[string]any{"content": content, "chapter_id": "kapittel-2"},
		})
	}
	resp := map[string]any{"ok": true, "entities": rows}
	before := mustJSONSize(t, resp)
	require.Greater(t, before, defaultToolResultMaxBytes)

	got, changed := boundToolResultResponse("entity-query", resp, defaultToolResultMaxBytes)
	require.True(t, changed)
	after := mustJSONSize(t, got)
	t.Logf("representative entity-query result: before=%d bytes after=%d bytes (cap=%d, reduction=%.1f%%)",
		before, after, defaultToolResultMaxBytes, 100*float64(before-after)/float64(before))

	assert.LessOrEqual(t, after, defaultToolResultMaxBytes)
	assert.Equal(t, true, got["truncated"])
}

// Bounding must not mutate the caller's map: the same payload is persisted and
// streamed in full elsewhere, so only the model-facing copy may change.
func TestBoundToolResultResponse_DoesNotMutateInput(t *testing.T) {
	resp := map[string]any{"ok": true, "blob": strings.Repeat("y", 100<<10)}
	_, changed := boundToolResultResponse("entity-query", resp, 4<<10)
	require.True(t, changed)

	assert.Equal(t, strings.Repeat("y", 100<<10), resp["blob"], "input map must be left intact")
}

// A negative cap disables per-result bounding.
func TestBoundToolResultResponse_NegativeCapDisables(t *testing.T) {
	resp := map[string]any{"ok": true, "blob": strings.Repeat("z", 50<<10)}
	got, changed := boundToolResultResponse("entity-query", resp, -1)
	assert.False(t, changed)
	assert.Equal(t, resp, got)
}

func TestToolResultBounds_MaxBytesFor(t *testing.T) {
	// Zero values fall back to the generous default.
	assert.Equal(t, defaultToolResultMaxBytes, ToolResultBounds{}.maxBytesFor("entity-query"))
	// Explicit global cap applies to every tool...
	assert.Equal(t, 1234, ToolResultBounds{MaxBytes: 1234}.maxBytesFor("entity-query"))
	// ...unless a per-tool override exists.
	b := ToolResultBounds{
		MaxBytes:        1234,
		PerToolMaxBytes: map[string]int{"entity-edges-get": 9999},
	}
	assert.Equal(t, 9999, b.maxBytesFor("entity-edges-get"))
	assert.Equal(t, 1234, b.maxBytesFor("entity-query"))
	// Non-positive overrides are ignored (fall through to the global cap).
	b.PerToolMaxBytes["bad"] = 0
	assert.Equal(t, 1234, b.maxBytesFor("bad"))
	// A negative global cap disables the layer for every tool: a positive
	// per-tool override must NOT re-enable it.
	disabled := ToolResultBounds{MaxBytes: -1, PerToolMaxBytes: map[string]int{"entity-edges-get": 9999}}
	assert.Equal(t, -1, disabled.maxBytesFor("entity-edges-get"))
	assert.Equal(t, -1, disabled.maxBytesFor("entity-query"))
}

func TestToolResultBoundsFromConfig_DefaultsAndOverrides(t *testing.T) {
	// Default config: zero values fall back at use time.
	b := toolResultBoundsFromConfig(config.MCPConfig{})
	assert.Equal(t, defaultToolResultMaxBytes, b.maxBytesFor("entity-query"))
	assert.Equal(t, defaultToolResultTotalBudgetBytes, b.totalBudget())

	// Configured global cap + per-tool override + total budget.
	b = toolResultBoundsFromConfig(config.MCPConfig{
		ToolResultMaxBytes:          64 << 10,
		ToolResultMaxBytesOverrides: "entity-edges-get=262144",
		ToolResultTotalBudgetBytes:  1 << 20,
	})
	assert.Equal(t, 64<<10, b.maxBytesFor("entity-query"))
	assert.Equal(t, 262144, b.maxBytesFor("entity-edges-get"))
	assert.Equal(t, 1<<20, b.totalBudget())
}

func TestParseToolResultMaxBytesOverrides(t *testing.T) {
	got := parseToolResultMaxBytesOverrides("entity-edges-get=262144, entity-query=65536 ,,garbage,=5,x=notanint")

	assert.Equal(t, 262144, got["entity-edges-get"])
	assert.Equal(t, 65536, got["entity-query"])
	_, hasGarbage := got["garbage"]
	assert.False(t, hasGarbage)
	_, hasEmpty := got[""]
	assert.False(t, hasEmpty)
	_, hasX := got["x"]
	assert.False(t, hasX)
	assert.Nil(t, parseToolResultMaxBytesOverrides(""))
}

// Every oversized tool result in the request is bounded; a result under the cap
// and a non-tool part are left untouched.
func TestBoundModelRequestToolResults_TruncatesEachOversizedResult(t *testing.T) {
	small := map[string]any{"ok": true, "note": "small"}
	big := map[string]any{"ok": true, "blob": strings.Repeat("a", 40<<10)}
	req := functionResponseRequest([2]any{"entity-edges-get", big}, [2]any{"entity-query", small})
	// A plain text part must never be touched.
	req.Contents = append(req.Contents, &genai.Content{Role: "model", Parts: []*genai.Part{{Text: strings.Repeat("t", 100<<10)}}})

	stats := boundModelRequestToolResults(req, ToolResultBounds{MaxBytes: 8 << 10, TotalBudgetBytes: -1})

	assert.Equal(t, 1, stats.TruncatedResults)
	assert.Equal(t, 0, stats.ElidedResults)
	assert.Greater(t, stats.BytesBefore, stats.BytesAfter)

	// big -> bounded
	assert.Equal(t, true, req.Contents[0].Parts[0].FunctionResponse.Response["truncated"])
	assert.LessOrEqual(t, mustJSONSize(t, req.Contents[0].Parts[0].FunctionResponse.Response), 8<<10)
	// small -> unchanged
	assert.Equal(t, small, req.Contents[1].Parts[0].FunctionResponse.Response)
	// text part -> unchanged
	assert.Equal(t, strings.Repeat("t", 100<<10), req.Contents[2].Parts[0].Text)
}

// When the sum of tool results exceeds the total budget, the oldest results are
// elided first so the most recent context survives. Elision is marked, not
// silent.
func TestBoundModelRequestToolResults_TotalBudgetElidesOldest(t *testing.T) {
	mk := func(marker string) map[string]any {
		return map[string]any{"ok": true, "data": strings.Repeat(marker, 3000)}
	}
	oldest := mk("a")
	middle := mk("b")
	newest := mk("c")
	req := functionResponseRequest(
		[2]any{"entity-query", oldest},
		[2]any{"entity-query", middle},
		[2]any{"entity-query", newest},
	)
	// Each result is ~3015 bytes; the budget sits below the sum of all three
	// but comfortably above the sum of the two most recent (plus one elision
	// placeholder), so exactly the oldest is elided.
	budget := 7500

	stats := boundModelRequestToolResults(req, ToolResultBounds{MaxBytes: 1 << 20, TotalBudgetBytes: budget})

	assert.GreaterOrEqual(t, stats.ElidedResults, 1)
	// oldest elided
	r0 := req.Contents[0].Parts[0].FunctionResponse.Response
	assert.Equal(t, true, r0["elided"])
	marker, _ := r0["truncation"].(string)
	assert.Contains(t, marker, "elided")
	assert.Contains(t, marker, "re-run the tool")
	// newest kept in full
	assert.Equal(t, newest, req.Contents[2].Parts[0].FunctionResponse.Response)
	assert.Equal(t, true, req.Contents[2].Parts[0].FunctionResponse.Response["ok"])

	// Total bounded.
	sum := 0
	for _, c := range req.Contents {
		for _, p := range c.Parts {
			if p.FunctionResponse != nil {
				sum += mustJSONSize(t, p.FunctionResponse.Response)
			}
		}
	}
	assert.LessOrEqual(t, sum, budget)
}

// The most recent tool result is never elided, even under an absurdly small
// budget, because it is the one the current model step is reasoning about.
func TestBoundModelRequestToolResults_NeverElidesNewest(t *testing.T) {
	oldest := map[string]any{"ok": true, "data": strings.Repeat("a", 500)}
	newest := map[string]any{"ok": true, "data": strings.Repeat("b", 500)}
	req := functionResponseRequest([2]any{"entity-query", oldest}, [2]any{"entity-query", newest})

	stats := boundModelRequestToolResults(req, ToolResultBounds{MaxBytes: 1 << 20, TotalBudgetBytes: 50})

	assert.GreaterOrEqual(t, stats.ElidedResults, 1)
	assert.Equal(t, newest, req.Contents[1].Parts[0].FunctionResponse.Response)
	assert.NotEqual(t, true, req.Contents[1].Parts[0].FunctionResponse.Response["elided"])
}

// The total budget is soft: when the single most recent result alone exceeds
// it, that result is retained whole rather than silently dropped. This is the
// documented exception to "the sum is bounded" and is reachable when total <
// per-result, or when the per-result layer is disabled.
func TestBoundModelRequestToolResults_NewestOverTotalBudgetRetainedWhole(t *testing.T) {
	newest := map[string]any{"ok": true, "data": strings.Repeat("n", 4000)}
	req := functionResponseRequest([2]any{"entity-query", newest})

	// Per-result disabled (negative) + a tiny total budget: the newest result
	// alone is far over budget.
	stats := boundModelRequestToolResults(req, ToolResultBounds{MaxBytes: -1, TotalBudgetBytes: 500})

	// Nothing is silently dropped and nothing is elided (there is no older
	// result to elide).
	assert.Equal(t, 0, stats.ElidedResults)
	assert.Equal(t, 0, stats.TruncatedResults)
	got := req.Contents[0].Parts[0].FunctionResponse.Response
	assert.Equal(t, newest, got, "the newest result must be retained whole")
	assert.NotEqual(t, true, got["elided"])
	// The sum is intentionally not bounded in this single-result case.
	sum := mustJSONSize(t, got)
	assert.Greater(t, sum, 500)
}

// When the newest result alone exceeds the total budget but older results also
// exist, the older results are still elided (the soft budget only spares the
// newest).
func TestBoundModelRequestToolResults_NewestOverTotalBudget_ElidesOlder(t *testing.T) {
	oldest := map[string]any{"ok": true, "data": strings.Repeat("a", 4000)}
	middle := map[string]any{"ok": true, "data": strings.Repeat("b", 4000)}
	newest := map[string]any{"ok": true, "data": strings.Repeat("c", 4000)}
	req := functionResponseRequest(
		[2]any{"entity-query", oldest},
		[2]any{"entity-query", middle},
		[2]any{"entity-query", newest},
	)

	stats := boundModelRequestToolResults(req, ToolResultBounds{MaxBytes: -1, TotalBudgetBytes: 500})

	assert.Equal(t, 2, stats.ElidedResults)
	assert.Equal(t, true, req.Contents[0].Parts[0].FunctionResponse.Response["elided"])
	assert.Equal(t, true, req.Contents[1].Parts[0].FunctionResponse.Response["elided"])
	assert.Equal(t, newest, req.Contents[2].Parts[0].FunctionResponse.Response)
}

func TestBoundModelRequestToolResults_NilSafe(t *testing.T) {
	assert.NotPanics(t, func() {
		_ = boundModelRequestToolResults(nil, ToolResultBounds{})
	})
	req := &model.LLMRequest{Contents: []*genai.Content{nil, {Parts: []*genai.Part{nil, {Text: "x"}}}}}
	stats := boundModelRequestToolResults(req, ToolResultBounds{})
	assert.Zero(t, stats.TruncatedResults)
}
