<!-- Baseline failures (pre-existing, not introduced by this change):
- old_insert.go, patch_imdb.go, patch_throttle.go, clean_syntax.go, fix_agent_id.go: stray main() files in repo root — pre-existing, unrelated to CLI blueprints package
- search/client_test.go and health/client_test.go: known pre-existing compile errors per AGENTS.md
-->

## 1. Seed file types and loader

- [x] 1.1 Revert the partial types.go edit; add `SeedObjectRecord` and `SeedRelationshipRecord` structs (flat JSONL line format) to `tools/cli/internal/blueprints/types.go`
- [x] 1.2 Implement `loadSeedObjects(dir string)` in `loader.go` — reads `seed/objects/*.jsonl` (including split files `*.001.jsonl` etc.), returns `[]SeedObjectRecord` and any parse errors
- [x] 1.3 Implement `loadSeedRelationships(dir string)` in `loader.go` — reads `seed/relationships/*.jsonl` (including split files), returns `[]SeedRelationshipRecord`
- [x] 1.4 Add `validateSeedObject` — require non-empty `type` field
- [x] 1.5 Add `validateSeedRelationship` — require non-empty `type`; require either (`srcKey`+`dstKey`) or (`srcId`+`dstId`)
- [x] 1.6 Update `LoadDir` to also return `objects []SeedObjectRecord` and `rels []SeedRelationshipRecord`; missing `seed/` directory is not an error
- [x] 1.7 Add unit tests for seed loader: valid objects JSONL, valid relationships JSONL, missing seed dir, parse error, validation error, split files loaded in order, unsupported extension skipped

## 2. Seed applier

- [x] 2.1 Create `seeder.go` in `tools/cli/internal/blueprints/` with a `Seeder` struct holding `graph *sdkgraph.Client`, `dryRun bool`, `upgrade bool`, `out io.Writer`
- [x] 2.2 Implement key-lookup: collect all non-empty keys from a batch of `SeedObjectRecord`; call `ListObjects` filtering by those keys to build `key → entityID` map
- [x] 2.3 Implement object apply loop:
  - Chunk objects into batches of 100
  - For each batch: call key-lookup to find existing keys
  - New objects (key not found, or keyless): collect into bulk-create list
  - Existing objects with `--upgrade`: call `UpsertObject` individually (content-hash no-op means no unnecessary writes)
  - Existing objects without `--upgrade`: record as skipped
  - Call `BulkCreateObjects` for the new-objects list
  - Accumulate `key → entityID` from all created/upserted objects for relationship resolution
- [x] 2.4 Implement relationship apply loop:
  - Resolve `srcKey`/`dstKey` → entityID using the key map built during object phase
  - Fall back to raw `srcId`/`dstId` when keys are absent
  - Unresolvable references: record error, continue
  - Chunk into batches of 100; call `BulkCreateRelationships`
- [x] 2.5 Implement dry-run path: print intended actions without API calls
- [x] 2.6 `Run(ctx, objects, rels)` returns `SeedResult` summary: objects created, updated, skipped, failed; rels created, failed
- [x] 2.7 Add unit tests for seeder: create path, skip path (existing key, no upgrade), update path (existing key + upgrade), unresolvable key error, dry-run output, relationship dedup (server returns success on duplicate)

## 3. Wire seed into blueprints apply command

- [x] 3.1 Update `blueprints.go` CLI command: call `LoadDir` for seed objects+rels, instantiate `Seeder` with `c.SDK.Graph`, call `seeder.Run` after pack and agent phases
- [x] 3.2 Extend output to include seed summary: `N objects created, M relationships created`
- [x] 3.3 Ensure `--dry-run` and `--upgrade` flags propagate to the seeder
- [x] 3.4 If no packs, agents, or seed data found: print "Nothing to apply" and return

## 4. Dump subcommand — core

- [x] 4.1 Create `dumper.go` in `tools/cli/internal/blueprints/` with a `Dumper` struct holding `graph *sdkgraph.Client`, `types []string`, `out io.Writer`
- [x] 4.2 Implement `dumpObjects(ctx, outputDir)`:
  - Paginate `ListObjects` with cursor (page size 250), filtering by `--types` if set
  - Group by type; for each type maintain a current JSONL writer
  - When current file reaches 50 MB, open next split file (`<Type>.001.jsonl`, `<Type>.002.jsonl`, …)
  - Write each object as a `SeedObjectRecord` JSONL line
  - Build and return `entityID → key` map (for relationship export)
  - Print progress to `out`: `  objects: N fetched…`
- [x] 4.3 Implement `dumpRelationships(ctx, outputDir, entityKeyMap)`:
  - Paginate `ListRelationships` with cursor (page size 250)
  - When `--types` filter active: only keep relationships where both endpoints are in the exported object set
  - For each relationship: use `srcKey`/`dstKey` when both endpoints have keys, else `srcId`/`dstId`
  - Group by type with same 50 MB split logic
  - Print progress to `out`
- [x] 4.4 Implement `Run(ctx, outputDir)`: create `seed/objects/` and `seed/relationships/` dirs, call dumpObjects then dumpRelationships, print final summary: `Dumped N objects, M relationships → <path>`
- [x] 4.5 Add unit tests for dumper: output files created, type filter applied, key references used when available, ID fallback when key missing, split triggered at 50 MB boundary

## 5. Dump subcommand — CLI wiring

- [x] 5.1 Add `blueprintsDumpCmd` cobra subcommand in `blueprints.go`: `memory blueprints dump <output-dir>`
- [x] 5.2 Add flags: `--types` (comma-separated, default empty)
- [x] 5.3 Register `blueprintsDumpCmd` as a subcommand of `blueprintsCmd`
- [x] 5.4 Update help text on `blueprintsCmd` to mention the `dump` subcommand

## 6. Verification

- [x] 6.1 Run `go build ./tools/cli/...` — zero errors
- [x] 6.2 Run `go test ./tools/cli/internal/blueprints/...` — all tests pass
- [ ] 6.3 Manual smoke test: apply a blueprint with a `seed/objects/` directory containing objects; verify created in project
- [ ] 6.4 Manual smoke test: apply a blueprint with `seed/relationships/` referencing objects by key; verify relationships created
- [ ] 6.5 Manual smoke test: `--dry-run` on seed apply prints actions without creating objects
- [ ] 6.6 Manual smoke test: `--upgrade` upserts existing keyed objects; without flag they are skipped
- [ ] 6.7 Manual smoke test: run `memory blueprints dump ./out` on a project with data; verify `out/seed/objects/` and `out/seed/relationships/` are valid and re-applyable
