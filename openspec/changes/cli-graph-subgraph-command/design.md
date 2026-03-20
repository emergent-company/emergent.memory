## Context

`memory graph objects create-batch` currently unmarshals its input file directly into `[]batchObjectItem` (flat array). If the top-level JSON is an object with `objects` + `relationships` keys, the unmarshal fails. The server already has `POST /api/graph/subgraph` (`CreateSubgraph`) which accepts exactly that shape and returns a `ref_map` of placeholder → UUID. The CLI just needs to detect the format and route accordingly.

## Goals / Non-Goals

**Goals:**
- `create-batch` accepts both the existing flat-array format and the new subgraph format transparently
- Subgraph format supports `_ref` string placeholders so relationships can reference objects in the same file without UUIDs
- `--output json` works for both formats
- No new commands, no new flags

**Non-Goals:**
- JSONL support (both formats remain JSON; JSONL adds parsing complexity for no agent benefit)
- Streaming / incremental output for large files
- Changing the relationships `create-batch` command

## Decisions

**Format detection via `json.RawMessage`**
Unmarshal the file into `json.RawMessage`, peek the first non-whitespace byte: `[` → flat array (existing path), `{` → subgraph format (new path). Simple, zero-dependency, no ambiguity.

Alternative considered: separate `--subgraph` flag. Rejected — agents shouldn't need to know which flag to pass; the file format is self-describing.

**Reuse `CreateSubgraphRequest` / `CreateSubgraphResponse` DTOs directly**
The SDK already exposes these types. Map `batchObjectItem`-style fields (`name`, `description` shortcuts) into `SubgraphObjectRequest.Properties` for consistency with the existing flat-array path.

**Output for subgraph format**
- Default (text): one line per created object `<entity-id>  <type>  <name>`, then a summary line `Created N objects, M relationships`
- `--output json`: full `CreateSubgraphResponse` including `ref_map`

The `ref_map` is only meaningful in JSON output — agents that need to chain calls should use `--output json` and parse it.

**Chunking is the caller's responsibility**
Server limit is 100 objects + 200 relationships per call. The CLI validates and returns a clear error if exceeded. The `memory-graph` skill documents the chunking pattern.

## Risks / Trade-offs

- [Risk] Agents pass a flat array of relationships (not objects) to `create-batch` and get a confusing error → Mitigation: error message explicitly says "expected array of objects or subgraph object; got array of relationships — use `memory graph relationships create-batch`"
- [Risk] `_ref` values collide across chunks when an agent splits a large subgraph → Mitigation: document in skill that `_ref` scope is per-call; use globally unique slugs (e.g. prefixed with chunk index)
- [Trade-off] `create-batch` now silently does more than its name implies — acceptable because "batch" describes the operation style, not the content shape
