package mcp

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Intra-run tool-result cache (issue #1192).
//
// A single agent run frequently issues the same read-only tool call more than
// once (duplicate searches, repeated entity fetches), re-running byte-identical
// queries for no new information. This cache memoises the results of an explicit
// allowlist of read-only tools for the lifetime of exactly one run.
//
// Correctness is the whole point, so the design is deliberately conservative:
//
//   - Lifetime is one run. The cache is created in the agent executor and
//     carried in that run's context; it is never global, never shared across
//     runs, and never inherited by a nested child run (the child installs its
//     own fresh cache).
//   - Only tools on cacheableReadOnlyTools are eligible.
//   - Any tool not on the allowlist is treated as a potential mutation and
//     clears the cache for the remainder of the run, so a cached read can never
//     serve stale data after a write.
//   - Errors, cancelled/expired contexts, and payloads that mark themselves
//     truncated/incomplete are never stored (this keeps #1187's timeout-shaped
//     empty success from being served to a retry).
//   - Memory is bounded by entry count and total bytes, evicting oldest first.

const (
	defaultToolResultCacheMaxEntries = 64
	defaultToolResultCacheMaxBytes   = 8 << 20 // 8 MiB
)

// ToolResultCache is a per-run, bounded, in-memory memoisation of read-only tool
// results. Its zero value is not usable; construct it with NewToolResultCache.
type ToolResultCache struct {
	mu         sync.Mutex
	entries    map[string]*list.Element
	order      *list.List // front = oldest
	curBytes   int
	maxEntries int
	maxBytes   int
}

type toolResultCacheEntry struct {
	key  string
	data []byte
}

// toolResultCacheContextKey is the context key carrying the run's cache. It is a
// distinct unexported type so no other package can collide with it.
type toolResultCacheContextKey struct{}

// NewToolResultCache creates an empty cache with the default bounds.
func NewToolResultCache() *ToolResultCache {
	return &ToolResultCache{
		entries:    make(map[string]*list.Element),
		order:      list.New(),
		maxEntries: defaultToolResultCacheMaxEntries,
		maxBytes:   defaultToolResultCacheMaxBytes,
	}
}

// ContextWithToolResultCache returns a context carrying cache.
func ContextWithToolResultCache(ctx context.Context, cache *ToolResultCache) context.Context {
	return context.WithValue(ctx, toolResultCacheContextKey{}, cache)
}

// ContextWithNewToolResultCache installs a fresh cache in ctx. Callers at the
// agent-run boundary use this so every run starts cold and a parent run's cache
// can never leak into a child.
func ContextWithNewToolResultCache(ctx context.Context) context.Context {
	return ContextWithToolResultCache(ctx, NewToolResultCache())
}

// toolResultCacheFromContext returns the run's cache, or nil when the call is
// not part of an agent run (e.g. an HTTP transport call). Nil means "do not
// cache" — never "use a default global cache".
func toolResultCacheFromContext(ctx context.Context) *ToolResultCache {
	c, _ := ctx.Value(toolResultCacheContextKey{}).(*ToolResultCache)
	return c
}

// get returns a fresh copy of the cached result. Each entry is stored as
// marshalled bytes so a hit can never alias a caller's mutable map/slice.
func (c *ToolResultCache) get(key string) (*ToolResult, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	entry, _ := el.Value.(*toolResultCacheEntry)
	if entry == nil {
		c.removeElementLocked(el)
		return nil, false
	}
	var res ToolResult
	if err := json.Unmarshal(entry.data, &res); err != nil {
		// Corrupt/unreadable entry: drop it and treat as a miss.
		c.removeElementLocked(el)
		return nil, false
	}
	c.order.MoveToBack(el)
	return &res, true
}

// put stores a result, evicting oldest entries until both bounds hold. A single
// result larger than maxBytes is not stored.
func (c *ToolResultCache) put(key string, res *ToolResult) {
	if c == nil || res == nil {
		return
	}
	data, err := json.Marshal(res)
	if err != nil {
		return
	}
	if c.maxBytes > 0 && len(data) > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		if entry, _ := el.Value.(*toolResultCacheEntry); entry != nil {
			c.curBytes -= len(entry.data)
			entry.data = data
			c.curBytes += len(data)
		}
		c.order.MoveToBack(el)
	} else {
		el := c.order.PushBack(&toolResultCacheEntry{key: key, data: data})
		c.entries[key] = el
		c.curBytes += len(data)
	}
	c.evictLocked()
}

// invalidate clears every entry. Called before dispatching any non-read-only
// tool so a later read re-executes rather than serving pre-mutation data.
func (c *ToolResultCache) invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*list.Element)
	c.order.Init()
	c.curBytes = 0
}

func (c *ToolResultCache) evictLocked() {
	for {
		overCount := c.maxEntries > 0 && len(c.entries) > c.maxEntries
		overBytes := c.maxBytes > 0 && c.curBytes > c.maxBytes
		if !overCount && !overBytes {
			return
		}
		front := c.order.Front()
		if front == nil {
			return
		}
		c.removeElementLocked(front)
	}
}

func (c *ToolResultCache) removeElementLocked(el *list.Element) {
	entry, _ := el.Value.(*toolResultCacheEntry)
	if entry != nil {
		c.curBytes -= len(entry.data)
		delete(c.entries, entry.key)
	}
	c.order.Remove(el)
}

// cacheableReadOnlyTools is the explicit allowlist of read-only tools eligible
// for intra-run caching. A tool belongs here only if it is a direct read with no
// write side effect and no nested agent run. Everything else both misses the
// cache and invalidates it.
var cacheableReadOnlyTools = map[string]bool{
	// Graph reads.
	"entity-query":      true,
	"entity-search":     true,
	"entity-history":    true,
	"entity-type-list":  true,
	"entity-edges-get":  true,
	"relationship-list": true,
	"tag-list":          true,
	"graph-traverse":    true,
	"search-hybrid":     true,
	"search-semantic":   true,
	"search-similar":    true,
	// Schema reads.
	"schema-version":        true,
	"schema-list":           true,
	"schema-get":            true,
	"schema-compiled-types": true,
}

// isCacheableReadOnlyTool reports whether toolName is on the read-only allowlist.
func isCacheableReadOnlyTool(toolName string) bool {
	return cacheableReadOnlyTools[toolName]
}

// executeWithToolResultCache wraps a single tool dispatch with the per-run cache
// carried in ctx. With no cache in ctx (HTTP transports, tests) it dispatches
// unchanged.
func executeWithToolResultCache(ctx context.Context, projectID, toolName string, args map[string]any, dispatch func() (*ToolResult, error)) (*ToolResult, error) {
	cache := toolResultCacheFromContext(ctx)
	if cache == nil {
		return dispatch()
	}
	if !isCacheableReadOnlyTool(toolName) {
		// Potential mutation (or an unknown tool): drop everything cached so far
		// so a later read cannot serve pre-mutation data.
		cache.invalidate()
		return dispatch()
	}
	key, ok := canonicalToolCacheKey(projectID, toolName, args, ctx)
	if !ok {
		// Arguments could not be canonicalised — never cache an ambiguous key.
		return dispatch()
	}
	if res, hit := cache.get(key); hit {
		return res, nil
	}
	res, err := dispatch()
	if err != nil {
		// A failed read cannot have changed graph state; leave existing entries
		// intact and do not cache the failure.
		return res, err
	}
	if isCacheableToolResult(ctx, res) {
		cache.put(key, res)
	}
	return res, nil
}

// canonicalToolCacheKey builds a collision-resistant key over the tenant, tool,
// canonicalised arguments, and the caller's ambient authorization state. It
// returns ok=false when the arguments cannot be canonicalised.
func canonicalToolCacheKey(projectID, toolName string, args map[string]any, ctx context.Context) (string, bool) {
	argJSON, err := canonicalJSON(args)
	if err != nil {
		return "", false
	}
	h := sha256.New()
	h.Write([]byte(projectID))
	h.Write([]byte{0})
	h.Write([]byte(toolName))
	h.Write([]byte{0})
	h.Write([]byte(authFingerprint(ctx)))
	h.Write([]byte{0})
	h.Write(argJSON)
	return hex.EncodeToString(h.Sum(nil)), true
}

// authFingerprint captures the authorization-relevant ambient state that is not
// already part of the arguments. Within one run the principal is fixed, so this
// is defence in depth against a namespace/trust change mid-run; the raw token is
// never read or stored.
func authFingerprint(ctx context.Context) string {
	var sb strings.Builder
	sb.WriteString("trust=")
	sb.WriteString(strconv.FormatBool(TrustedInternalFromContext(ctx)))
	sb.WriteString(";ns=")
	sb.WriteString(auth.NamespaceFromContext(ctx))
	return sb.String()
}

// canonicalJSON serialises an argument value deterministically: map keys sorted,
// numbers normalised, so semantically identical argument maps produce identical
// bytes regardless of Go numeric type. Unsupported value kinds are an error
// (the caller then declines to cache).
func canonicalJSON(v any) ([]byte, error) {
	var sb strings.Builder
	if err := writeCanonical(&sb, v); err != nil {
		return nil, err
	}
	return []byte(sb.String()), nil
}

func writeCanonical(sb *strings.Builder, v any) error {
	switch t := v.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		sb.WriteString(strconv.FormatBool(t))
	case string:
		b, err := json.Marshal(t)
		if err != nil {
			return err
		}
		sb.Write(b)
	case float64:
		s, err := canonicalFloat(t)
		if err != nil {
			return err
		}
		sb.WriteString(s)
	case float32:
		s, err := canonicalFloat(float64(t))
		if err != nil {
			return err
		}
		sb.WriteString(s)
	case int:
		sb.WriteString(strconv.Itoa(t))
	case int64:
		sb.WriteString(strconv.FormatInt(t, 10))
	case int32:
		sb.WriteString(strconv.FormatInt(int64(t), 10))
	case uint:
		sb.WriteString(strconv.FormatUint(uint64(t), 10))
	case uint64:
		sb.WriteString(strconv.FormatUint(t, 10))
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return fmt.Errorf("canonical json: invalid number %q", t.String())
		}
		s, err := canonicalFloat(f)
		if err != nil {
			return err
		}
		sb.WriteString(s)
	case []any:
		sb.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				sb.WriteByte(',')
			}
			if err := writeCanonical(sb, e); err != nil {
				return err
			}
		}
		sb.WriteByte(']')
	case []string:
		sb.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				sb.WriteByte(',')
			}
			b, err := json.Marshal(e)
			if err != nil {
				return err
			}
			sb.Write(b)
		}
		sb.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return err
			}
			sb.Write(kb)
			sb.WriteByte(':')
			if err := writeCanonical(sb, t[k]); err != nil {
				return err
			}
		}
		sb.WriteByte('}')
	case map[string]string:
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[k] = val
		}
		return writeCanonical(sb, m)
	default:
		return fmt.Errorf("canonical json: unsupported argument type %T", v)
	}
	return nil
}

// canonicalFloat renders a finite float; integral values render without a
// decimal part so int(1) and float64(1) canonicalise identically.
func canonicalFloat(f float64) (string, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("canonical json: non-finite number")
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10), nil
	}
	return strconv.FormatFloat(f, 'g', -1, 64), nil
}

// isCacheableToolResult reports whether a successful dispatch result may be
// stored. Errors, cancelled contexts, error-shaped payloads, and payloads that
// mark themselves truncated/incomplete are excluded so a retry is never masked.
func isCacheableToolResult(ctx context.Context, res *ToolResult) bool {
	if res == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		// The run timed out or was cancelled around this call — the payload may
		// be partial (see #1187).
		return false
	}
	if res.IsError {
		return false
	}
	if sc := res.StructuredContent; len(sc) > 0 {
		if okv, isBool := sc["ok"].(bool); isBool && !okv {
			return false
		}
		if containsTrueFlag(sc, "truncated") {
			return false
		}
	} else {
		// Prose/legacy results may not populate StructuredContent; inspect the
		// text payload when it parses as a JSON object.
		for _, block := range res.Content {
			if block.Text == "" {
				continue
			}
			var parsed any
			if json.Unmarshal([]byte(block.Text), &parsed) != nil {
				continue
			}
			if m, ok := parsed.(map[string]any); ok {
				if okv, isBool := m["ok"].(bool); isBool && !okv {
					return false
				}
				if containsTrueFlag(m, "truncated") {
					return false
				}
			}
		}
	}
	if _, err := json.Marshal(res); err != nil {
		return false
	}
	return true
}

// containsTrueFlag reports whether v contains key with boolean value true at any
// depth. Used to reject incomplete payloads (`truncated: true`).
func containsTrueFlag(v any, key string) bool {
	switch t := v.(type) {
	case map[string]any:
		if b, ok := t[key].(bool); ok && b {
			return true
		}
		for _, e := range t {
			if containsTrueFlag(e, key) {
				return true
			}
		}
	case []any:
		for _, e := range t {
			if containsTrueFlag(e, key) {
				return true
			}
		}
	}
	return false
}
