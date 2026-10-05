// Package embeddingtext builds the semantic text that is embedded for a graph
// object and hashes it.
//
// It exists as a neutral, dependency-free home for this logic because two
// packages need it and must not drift:
//
//   - the extraction worker builds the text that is actually embedded, and
//   - the graph write path hashes the old vs new state to decide whether an
//     embedding must be regenerated (issue #1478: a property-only PATCH/upsert
//     re-enqueued an embedding unconditionally).
//
// The extraction package imports graph, so graph cannot import extraction to
// reuse its builder without creating an import cycle. This package breaks that
// cycle: both sides call into it.
//
// Text output is deterministic — property keys are visited in sorted order at
// every level — so hashing the same logical content always yields the same
// digest. (The historical extractText iterated the top-level property map in Go
// map order, which is randomised per call; making it deterministic is required
// for a content guard to be sound.)
package embeddingtext

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// MaxChars is the character budget for embedding text. Gemini embedding models
// accept ~3 000 tokens; at ~5 chars/word this gives plenty of headroom while
// preventing runaway concatenation on large objects.
const MaxChars = 8000

// skipKeys lists property keys that carry no semantic value for embedding.
var skipKeys = map[string]bool{
	"id":         true,
	"url":        true,
	"uri":        true,
	"href":       true,
	"license":    true,
	"version":    true,
	"citations":  true,
	"references": true,
	"hash":       true,
	"checksum":   true,
	"created_at": true,
	"updated_at": true,
	"deleted_at": true,
}

// boostedKeys are semantic fields prepended (and repeated ×2) to increase their
// weight in the embedding vector. Order is significant: more important fields
// first so they are never truncated by the char budget.
var boostedKeys = []string{"name", "title", "description", "summary", "label"}

// IsSkippedKey reports whether a property key carries no semantic value for
// embedding (shared with the extraction sweep worker, which reuses this list to
// enrich relationship triplet text).
func IsSkippedKey(key string) bool { return skipKeys[key] }

// Text builds the deterministic embedding text for a graph object.
//
// Token order (most important → least important):
//  1. objectType
//  2. stripped key (namespace prefix removed)
//  3. boosted fields (name/title/description/summary/label) — each repeated ×2
//  4. remaining string/numeric/bool property values, visited in sorted key order
//
// Skips: IDs, UUIDs, URLs, internal metadata keys (skipKeys), large numerics.
// Output is capped at MaxChars characters (word-boundary truncation).
func Text(objectType string, key *string, properties map[string]any) string {
	tokens := make([]string, 0, 16)

	tokens = append(tokens, objectType)

	// Strip namespace prefix from key (e.g. "ns:sarah" → "sarah").
	if key != nil && *key != "" {
		k := *key
		if i := strings.LastIndex(k, ":"); i >= 0 {
			k = k[i+1:]
		}
		if k != "" && !isUUID(k) {
			tokens = append(tokens, k)
		}
	}

	// Phase 1: boosted keys — prepend each value twice to increase weight.
	for _, bk := range boostedKeys {
		v, ok := properties[bk]
		if !ok {
			continue
		}
		if s, ok := v.(string); ok && isEmbeddableString(s) {
			tokens = append(tokens, s, s) // ×2 repetition
		}
	}

	// Phase 2: remaining properties (boosted keys skipped to avoid
	// triple-counting), in deterministic sorted key order.
	boostedSet := make(map[string]bool, len(boostedKeys))
	for _, bk := range boostedKeys {
		boostedSet[bk] = true
	}

	var walk func(key string, v any)
	walk = func(key string, v any) {
		if v == nil {
			return
		}
		lk := strings.ToLower(key)
		if skipKeys[lk] || boostedSet[lk] {
			return
		}
		switch val := v.(type) {
		case string:
			if isEmbeddableString(val) {
				tokens = append(tokens, val)
			}
		case float64:
			// Skip large numeric IDs (>= 1e9); include small counts/years.
			if val < 1e9 {
				tokens = append(tokens, fmt.Sprintf("%v", val))
			}
		case bool:
			tokens = append(tokens, fmt.Sprintf("%v", val))
		case []any:
			for _, x := range val {
				walk(key, x)
			}
		case map[string]any:
			nested := make([]string, 0, len(val))
			for k := range val {
				nested = append(nested, k)
			}
			sort.Strings(nested)
			for _, k := range nested {
				walk(k, val[k])
			}
		}
	}

	keys := make([]string, 0, len(properties))
	for k := range properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		walk(k, properties[k])
	}

	full := strings.Join(tokens, " ")

	// Apply character budget — truncate at word boundary.
	if len(full) <= MaxChars {
		return full
	}
	truncated := full[:MaxChars]
	if i := strings.LastIndex(truncated, " "); i > 0 {
		truncated = truncated[:i]
	}
	return truncated + "..."
}

// Hash returns the SHA-256 digest of Text for the given object state. Two
// states with the same digest have byte-identical embedding text.
func Hash(objectType string, key *string, properties map[string]any) [32]byte {
	return sha256.Sum256([]byte(Text(objectType, key, properties)))
}

// Normalize returns props converted to the concrete Go types encoding/json
// produces when decoding JSON into any: float64, string, bool, []any, and
// map[string]any. The embedding worker reads properties back from JSONB, so it
// always sees those types; the graph write path hashes pre-persistence Go
// values, which programmatic callers may build from other JSON-compatible types
// (int, []string, map[string]string, …). Normalizing before hashing keeps the
// two views identical, so a real content change can never be missed by the
// enqueue guard. An empty or non-JSON-marshalable map is returned unchanged.
func Normalize(props map[string]any) map[string]any {
	if len(props) == 0 {
		return props
	}
	raw, err := json.Marshal(props)
	if err != nil {
		return props
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return props
	}
	return out
}

// isEmbeddableString reports whether s carries embedding signal.
func isEmbeddableString(s string) bool {
	if s == "" || isUUID(s) {
		return false
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return false
	}
	return true
}

// isHexDigit reports whether c is a hexadecimal digit ([0-9a-fA-F]).
func isHexDigit(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// isUUID reports whether s looks like a UUID (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !isHexDigit(c) {
			return false
		}
	}
	return true
}
