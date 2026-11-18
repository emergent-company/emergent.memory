package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// testToolResult builds a ToolResult with a structured content map parsed from
// jsonText, mirroring what the real envelope/wrapResult helpers produce.
func testToolResult(t *testing.T, jsonText string) *ToolResult {
	t.Helper()
	sc := StructuredContentFromJSON([]byte(jsonText))
	if sc == nil {
		t.Fatalf("test result is not a JSON object: %s", jsonText)
	}
	return &ToolResult{
		Content:           []ContentBlock{{Type: "text", Text: jsonText}},
		StructuredContent: sc,
	}
}

func okResult(t *testing.T, extra string) *ToolResult {
	t.Helper()
	body := `{"ok":true,"data":{"value":"v"`
	if extra != "" {
		body += "," + extra
	}
	body += "}}"
	return testToolResult(t, body)
}

func TestExecuteWithToolResultCache_IdenticalReadExecutesOnce(t *testing.T) {
	ctx := ContextWithNewToolResultCache(context.Background())
	calls := 0
	dispatch := func() (*ToolResult, error) {
		calls++
		return okResult(t, ""), nil
	}
	const n = 3
	for i := 0; i < n; i++ {
		res, err := executeWithToolResultCache(ctx, "proj-1", "entity-search",
			map[string]any{"q": "same", "limit": 10}, dispatch)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if res == nil {
			t.Fatalf("call %d: nil result", i)
		}
	}
	if calls != 1 {
		t.Fatalf("underlying query executed %d times, want 1 (intra-run cache miss)", calls)
	}
}

func TestExecuteWithToolResultCache_DifferentArgsMiss(t *testing.T) {
	ctx := ContextWithNewToolResultCache(context.Background())
	calls := 0
	dispatch := func() (*ToolResult, error) { calls++; return okResult(t, ""), nil }

	if _, err := executeWithToolResultCache(ctx, "proj-1", "entity-search", map[string]any{"q": "a"}, dispatch); err != nil {
		t.Fatal(err)
	}
	if _, err := executeWithToolResultCache(ctx, "proj-1", "entity-search", map[string]any{"q": "b"}, dispatch); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("executed %d times, want 2 (distinct args must miss)", calls)
	}
}

func TestExecuteWithToolResultCache_MutationInvalidates(t *testing.T) {
	ctx := ContextWithNewToolResultCache(context.Background())
	readCalls := 0
	mutationCalls := 0
	read := func() (*ToolResult, error) { readCalls++; return okResult(t, ""), nil }
	mutate := func() (*ToolResult, error) { mutationCalls++; return okResult(t, ""), nil }

	args := map[string]any{"q": "same"}
	// 1. read (miss, cached)
	if _, err := executeWithToolResultCache(ctx, "proj-1", "entity-search", args, read); err != nil {
		t.Fatal(err)
	}
	// 2. mutation (non-allowlisted -> invalidates)
	if _, err := executeWithToolResultCache(ctx, "proj-1", "entity-create", args, mutate); err != nil {
		t.Fatal(err)
	}
	// 3. same read (must re-execute, not serve the pre-mutation cached value)
	if _, err := executeWithToolResultCache(ctx, "proj-1", "entity-search", args, read); err != nil {
		t.Fatal(err)
	}

	if readCalls != 2 {
		t.Fatalf("read executed %d times, want 2 (mutation must invalidate)", readCalls)
	}
	if mutationCalls != 1 {
		t.Fatalf("mutation executed %d times, want 1", mutationCalls)
	}
}

func TestExecuteWithToolResultCache_FailedMutationStillInvalidates(t *testing.T) {
	ctx := ContextWithNewToolResultCache(context.Background())
	readCalls := 0
	read := func() (*ToolResult, error) { readCalls++; return okResult(t, ""), nil }
	fail := func() (*ToolResult, error) { return nil, errors.New("write failed") }

	args := map[string]any{"q": "same"}
	if _, err := executeWithToolResultCache(ctx, "proj-1", "entity-search", args, read); err != nil {
		t.Fatal(err)
	}
	if _, err := executeWithToolResultCache(ctx, "proj-1", "entity-create", args, fail); err == nil {
		t.Fatal("expected mutation error")
	}
	if _, err := executeWithToolResultCache(ctx, "proj-1", "entity-search", args, read); err != nil {
		t.Fatal(err)
	}
	if readCalls != 2 {
		t.Fatalf("read executed %d times, want 2 (failed mutation still invalidates)", readCalls)
	}
}

func TestExecuteWithToolResultCache_NoReuseAcrossRuns(t *testing.T) {
	runA := ContextWithNewToolResultCache(context.Background())
	runB := ContextWithNewToolResultCache(context.Background())
	calls := 0
	dispatch := func() (*ToolResult, error) { calls++; return okResult(t, ""), nil }
	args := map[string]any{"q": "same"}

	if _, err := executeWithToolResultCache(runA, "proj-1", "entity-search", args, dispatch); err != nil {
		t.Fatal(err)
	}
	if _, err := executeWithToolResultCache(runB, "proj-1", "entity-search", args, dispatch); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("executed %d times, want 2 (caches must not be shared across runs)", calls)
	}
}

func TestExecuteWithToolResultCache_NoReuseAcrossProjects(t *testing.T) {
	ctx := ContextWithNewToolResultCache(context.Background())
	calls := 0
	dispatch := func() (*ToolResult, error) { calls++; return okResult(t, ""), nil }
	args := map[string]any{"q": "same"}

	if _, err := executeWithToolResultCache(ctx, "proj-1", "entity-search", args, dispatch); err != nil {
		t.Fatal(err)
	}
	if _, err := executeWithToolResultCache(ctx, "proj-2", "entity-search", args, dispatch); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("executed %d times, want 2 (projects must not collide)", calls)
	}
}

func TestExecuteWithToolResultCache_ErrorNotCached(t *testing.T) {
	ctx := ContextWithNewToolResultCache(context.Background())
	calls := 0
	dispatch := func() (*ToolResult, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("transient")
		}
		return okResult(t, ""), nil
	}
	args := map[string]any{"q": "same"}

	if _, err := executeWithToolResultCache(ctx, "proj-1", "entity-search", args, dispatch); err == nil {
		t.Fatal("expected first call to error")
	}
	if _, err := executeWithToolResultCache(ctx, "proj-1", "entity-search", args, dispatch); err != nil {
		t.Fatalf("second call should succeed and re-execute, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("executed %d times, want 2 (errors must not be cached)", calls)
	}
}

func TestExecuteWithToolResultCache_TruncatedNotCached(t *testing.T) {
	ctx := ContextWithNewToolResultCache(context.Background())
	calls := 0
	dispatch := func() (*ToolResult, error) {
		calls++
		// The #1187 timeout shape: success-shaped but truncated/empty.
		return testToolResult(t, `{"answer":"","truncated":true}`), nil
	}
	args := map[string]any{"question": "same"}

	for i := 0; i < 2; i++ {
		if _, err := executeWithToolResultCache(ctx, "proj-1", "search-hybrid", args, dispatch); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("executed %d times, want 2 (truncated payloads must not be cached)", calls)
	}
}

func TestExecuteWithToolResultCache_NestedTruncatedNotCached(t *testing.T) {
	ctx := ContextWithNewToolResultCache(context.Background())
	calls := 0
	dispatch := func() (*ToolResult, error) {
		calls++
		return testToolResult(t, `{"ok":true,"data":{"nodes":[],"truncated":true}}`), nil
	}
	args := map[string]any{"root": "x"}
	for i := 0; i < 2; i++ {
		if _, err := executeWithToolResultCache(ctx, "proj-1", "graph-traverse", args, dispatch); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("executed %d times, want 2 (nested truncated must not be cached)", calls)
	}
}

func TestExecuteWithToolResultCache_ErrorShapedNotCached(t *testing.T) {
	ctx := ContextWithNewToolResultCache(context.Background())
	calls := 0
	dispatch := func() (*ToolResult, error) {
		calls++
		return testToolResult(t, `{"ok":false,"error":"boom"}`), nil
	}
	args := map[string]any{"q": "same"}
	for i := 0; i < 2; i++ {
		if _, err := executeWithToolResultCache(ctx, "proj-1", "entity-search", args, dispatch); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("executed %d times, want 2 (ok:false payloads must not be cached)", calls)
	}
}

func TestExecuteWithToolResultCache_NilCacheDispatchesUnchanged(t *testing.T) {
	// No cache installed (HTTP transport path): every call dispatches.
	ctx := context.Background()
	calls := 0
	dispatch := func() (*ToolResult, error) { calls++; return okResult(t, ""), nil }
	args := map[string]any{"q": "same"}
	for i := 0; i < 2; i++ {
		if _, err := executeWithToolResultCache(ctx, "proj-1", "entity-search", args, dispatch); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("executed %d times, want 2 (no cache -> no memoisation)", calls)
	}
}

func TestToolResultCache_HitReturnsIndependentCopy(t *testing.T) {
	c := NewToolResultCache()
	key := "k"
	orig := okResult(t, `"nested":{"v":1}`)
	c.put(key, orig)

	first, ok := c.get(key)
	if !ok {
		t.Fatal("expected hit")
	}
	// Mutate the returned result; the stored entry must be unaffected.
	first.StructuredContent["ok"] = false
	first.Content[0].Text = "corrupted"

	second, ok := c.get(key)
	if !ok {
		t.Fatal("expected second hit")
	}
	if okv, _ := second.StructuredContent["ok"].(bool); !okv {
		t.Fatal("cached entry was mutated through a returned copy")
	}
	if second.Content[0].Text == "corrupted" {
		t.Fatal("cached text was mutated through a returned copy")
	}
}

func TestToolResultCache_EntryBound(t *testing.T) {
	c := NewToolResultCache()
	c.maxEntries = 2
	c.put("a", okResult(t, ""))
	c.put("b", okResult(t, ""))
	c.put("c", okResult(t, ""))

	if len(c.entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(c.entries))
	}
	if _, ok := c.get("a"); ok {
		t.Fatal("oldest entry should have been evicted")
	}
	if _, ok := c.get("c"); !ok {
		t.Fatal("newest entry should be present")
	}
}

func TestToolResultCache_ByteBoundRejectsOversize(t *testing.T) {
	c := NewToolResultCache()
	c.maxBytes = 64
	huge := testToolResult(t, `{"ok":true,"data":{"blob":"`+strings.Repeat("x", 256)+`"}}`)
	c.put("big", huge)
	if _, ok := c.get("big"); ok {
		t.Fatal("oversize result must not be stored")
	}
}

func TestToolResultCache_InvalidateClears(t *testing.T) {
	c := NewToolResultCache()
	c.put("a", okResult(t, ""))
	c.invalidate()
	if len(c.entries) != 0 || c.curBytes != 0 {
		t.Fatalf("invalidate left state: entries=%d bytes=%d", len(c.entries), c.curBytes)
	}
	if _, ok := c.get("a"); ok {
		t.Fatal("entry survived invalidate")
	}
}

func TestCanonicalToolCacheKey_NumberAndOrderInsensitive(t *testing.T) {
	ctx := context.Background()
	k1, ok1 := canonicalToolCacheKey("p", "entity-search", map[string]any{"a": 1, "b": "x"}, ctx)
	k2, ok2 := canonicalToolCacheKey("p", "entity-search", map[string]any{"b": "x", "a": float64(1)}, ctx)
	if !ok1 || !ok2 {
		t.Fatal("keys should be canonicalisable")
	}
	if k1 != k2 {
		t.Fatalf("keys differ for equivalent args:\n%s\n%s", k1, k2)
	}
}

func TestCanonicalToolCacheKey_ProjectAndToolScoped(t *testing.T) {
	ctx := context.Background()
	base, _ := canonicalToolCacheKey("p1", "entity-search", map[string]any{"a": 1}, ctx)
	if other, _ := canonicalToolCacheKey("p2", "entity-search", map[string]any{"a": 1}, ctx); other == base {
		t.Fatal("different projects produced the same key")
	}
	if other, _ := canonicalToolCacheKey("p1", "entity-query", map[string]any{"a": 1}, ctx); other == base {
		t.Fatal("different tools produced the same key")
	}
}

func TestIsCacheableReadOnlyTool(t *testing.T) {
	if !isCacheableReadOnlyTool("entity-search") {
		t.Fatal("entity-search should be cacheable")
	}
	for _, name := range []string{"entity-create", "entity-delete", "search-knowledge", "trigger_agent", "remember"} {
		if isCacheableReadOnlyTool(name) {
			t.Fatalf("%s must not be cacheable", name)
		}
	}
}
