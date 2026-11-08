package toolresult

import "encoding/json"

// batchTally reads a batch create's counts out of the payload. It mirrors the
// shipped iOS client's ToolResultBatch.tally (#2277): `entity-create` answers
// with `data.results` plus a `meta` tally beside the envelope, and the readable
// list keeps only created records, so this is the one place the failure count
// can come from.
//
// Nil means the payload is not a batch. Per-item `ok` flags are the fallback
// when `meta` is absent; a plain search `results` array is never a batch.
func batchTally(value any) *BatchTally {
	record, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	// The MCP envelope uses `data`; the agent's wrapper uses `result`.
	payload, ok := record["data"].(map[string]any)
	if !ok {
		payload, ok = record["result"].(map[string]any)
	}
	if !ok {
		payload = record
	}
	results, ok := payload["results"].([]any)
	if !ok {
		return nil
	}

	meta, _ := record["meta"].(map[string]any)
	if meta == nil {
		meta, _ = payload["meta"].(map[string]any)
	}
	failedItems := 0
	failedInlineRelationships := 0
	for _, element := range results {
		item, ok := element.(map[string]any)
		if !ok {
			continue
		}
		if succeeded, ok := item["ok"].(bool); ok && !succeeded {
			failedItems++
		}
		// An entity can be created while an inline relationship it asked for
		// failed (issue #401). The item's ok stays true, so those failures are
		// counted from the item's own field.
		if n, ok := integer(item["relationships_failed"]); ok {
			failedInlineRelationships += n
		}
	}
	similar := 0
	if n, ok := integer(meta["similar_count"]); ok {
		similar = n
	}

	if created, ok := integer(meta["created"]); ok {
		if failed, ok := integer(meta["failed"]); ok {
			// meta.failed counts entity failures only; inline relationship
			// failures would otherwise make a partial write look clean.
			if n, ok := integer(meta["relationships_failed"]); ok {
				failed += n
			}
			return &BatchTally{Created: created, Failed: failed, Similar: similar}
		}
	}

	// No tally: only a batch-shaped result may be counted from its items, so a
	// web-search `results` array is never reported as "0 created".
	if !looksLikeBatch(results) {
		return nil
	}
	created := len(results) - failedItems
	if created < 0 {
		created = 0
	}
	return &BatchTally{Created: created, Failed: failedItems + failedInlineRelationships, Similar: similar}
}

// looksLikeBatch reports whether the items are records a batch would produce: an
// entity, an index, or an ok flag. A search hit has none of these.
func looksLikeBatch(results []any) bool {
	for _, element := range results {
		item, ok := element.(map[string]any)
		if !ok {
			continue
		}
		if _, exists := item["entity"]; exists {
			return true
		}
		if _, exists := item["index"]; exists {
			return true
		}
		if _, exists := item["ok"].(bool); exists {
			return true
		}
	}
	return false
}

// integer reads a whole number the payload carries. Counts are written as JSON
// integers; a float or an int from a hand-built value is accepted too, but a
// string is not a count.
func integer(value any) (int, bool) {
	switch v := value.(type) {
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	case float64:
		return int(v), true
	case int:
		return v, true
	default:
		return 0, false
	}
}
