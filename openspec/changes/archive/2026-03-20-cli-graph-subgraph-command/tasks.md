# Tasks: cli-graph-subgraph-command

## Task 1: Add CreateSubgraph to SDK client
- [x] Add `SubgraphObjectRequest`, `SubgraphRelationshipRequest`, `CreateSubgraphRequest`, `CreateSubgraphResponse` DTOs to `apps/server/pkg/sdk/graph/client.go`
- [x] Add `CreateSubgraph(ctx, req) (*CreateSubgraphResponse, error)` method to `Client` that POSTs to `/api/graph/subgraph`

## Task 2: Extend create-batch with subgraph format detection
- [x] In `tools/cli/internal/cmd/graph.go`, add `subgraphInput` struct (mirrors `CreateSubgraphRequest` shape for CLI input: `objects []subgraphObjectInput`, `relationships []subgraphRelationshipInput`)
- [x] Add `subgraphObjectInput` struct: `_ref`, `type`, `key`, `name`, `description`, `properties`
- [x] Add `subgraphRelationshipInput` struct: `type`, `src_ref`, `dst_ref`, `properties`
- [x] In `graphObjectsCreateBatchCmd.RunE`: peek first non-whitespace byte of file; `[` → existing flat-array path; `{` → subgraph path
- [x] Subgraph path: validate ≤100 objects and ≤200 relationships (exit non-zero with clear message if exceeded)
- [x] Subgraph path: detect relationship-array mistake (array where items have `from`/`to` but no `type` matching object pattern) and return actionable error
- [x] Subgraph path: map `subgraphObjectInput` → `sdkgraph.SubgraphObjectRequest` (merge `name`/`description` into `Properties`)
- [x] Subgraph path: call `g.CreateSubgraph(ctx, req)` and handle error
- [x] Subgraph path, text output: print one `<entity-id>  <type>  <name>` line per object, then `Created N objects, M relationships`
- [x] Subgraph path, `--output json`: print full `CreateSubgraphResponse` JSON
- [x] Update `Long` help text on `graphObjectsCreateBatchCmd` to document both formats

## Task 3: Update memory-graph skill
- [x] In `tools/cli/internal/skillsfs/skills/memory-graph/SKILL.md`, add a new **Subgraph format (preferred when relationships are needed)** section before the existing Step 2
- [x] Show complete worked example: objects with `_ref` + `key`, relationships with `src_ref`/`dst_ref`, single `create-batch` call with `--output json` to capture `ref_map`
- [x] Show chunking pattern for >100 objects: use `key` on all objects, split into chunks, reference cross-chunk objects by re-fetching via `objects list` after first chunk
- [x] Update the existing two-pass workflow section to be labelled "Objects-only (no relationships)" and note that subgraph format is preferred when relationships are involved
- [x] Sync updated skill to `.agents/skills/memory-graph/SKILL.md` and `/root/legalplant-api/.agents/skills/memory-graph/SKILL.md`
