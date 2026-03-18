## 0. Evaluation Infrastructure & Baseline (DO THIS FIRST)

> **These tasks must be completed before any implementation work begins.**
> Record baseline metrics so we can prove improvement after implementation.

- [ ] 0.1 Create `domain/mcp/testdata/memory_benchmark/corpus.json` — 50 synthetic observations covering all categories (preference, pattern, correction, fact, instruction, convention), with realistic `content`, `proposed_type`, `confidence`, and `source` values
- [ ] 0.2 Create `domain/mcp/testdata/memory_benchmark/queries.json` — 40 queries (10 per intent type: PREFERENCE, CHRONOLOGICAL, FACTUAL, INSTRUCTIONAL) with `relevant_entity_ids` ground truth
- [ ] 0.3 Create `domain/mcp/testdata/memory_benchmark/contradiction_pairs.json` — 20 test pairs with `content_a`, `content_b`, `event_time_a`, `event_time_b`, `ground_truth_action`, `rationale`
- [ ] 0.4 Create `domain/mcp/testdata/memory_benchmark/temporal_pairs.json` — 10 pairs where observations describe the same fact at different `event_time` values; ground truth = newer event is correct
- [ ] 0.5 Create benchmark test file (build tag: `//go:build benchmark`) with test setup, corpus seeding, and M1–M9 measurement functions
- [ ] 0.6 Implement M1: count sessions with standing instruction injection / total sessions (expected baseline: 0.0)
- [ ] 0.7 Implement M2: run dedup against contradiction_pairs.json; record precision (expected baseline: ~0.40)
- [ ] 0.8 Implement M3: run recall against query set; LLM judge rates top-5 results; record precision@5 per intent type
- [ ] 0.9 Implement M4: for each temporal pair, verify equal semantic scores, then check which ranks higher; record recency_ranking_score (expected baseline: ~0.50)
- [ ] 0.10 Implement M5: count total tokens in injected context for 20-observation cluster (baseline = full expansion)
- [ ] 0.11 Implement M6: run dedup against temporal_pairs.json; record % resolved to more recent event (expected baseline: ~0.50)
- [ ] 0.12 Implement M7: run 100 recall calls against 200-entity corpus; record p50 and p95 latency in ms
- [ ] 0.13 Implement M8: run 50 store calls (fast path) + 50 store calls (merge path); record p50 and p95 for each path
- [ ] 0.14 Implement M9: seed Candidates and run 3 Janitor cycles; record % promoted to typed entities
- [ ] 0.15 **Run benchmark against current passive system and save results to `testdata/memory_benchmark/results/YYYY-MM-DD_baseline.json`**
- [ ] 0.16 Add result comparison logic: post-implementation benchmark asserts all M1–M9 targets are met

## 1. Candidate Type and Store API

- [ ] 1.1 Define `Candidate` object type in a new `schema-emergence` schema pack JSON: properties `raw_content`, `proposed_type`, `source_entity_id`, `source_context`, `surrounding_text`, `confidence`, `event_time`; embedding target: `raw_content`
- [ ] 1.2 Define `SchemaProposal` object type in the same schema pack: properties `proposed_type`, `proposed_properties` (JSON), `proposed_relationship` (JSON), `confidence`, `evidence_count`, `sample_candidate_ids`, `status`
- [ ] 1.3 Add `Store(content, type?, entity_id?, context?)` operation to `domain/graph/service.go`:
  - If `type` matches installed schema → create typed entity + relationship → return `{status: "created"}`
  - If `type` unknown or omitted → create `Candidate` → return `{status: "staged", proposed_type}`
- [ ] 1.4 Implement Candidate write-time dedup: before creating Candidate, hybrid search for similar Candidates with same `proposed_type` (cosine similarity ≥ 0.70); invoke LLM merge decision (ADD/UPDATE/NOOP); write only on ADD or UPDATE
- [ ] 1.5 Capture agent context at write time: `surrounding_text` (configurable window, default 500 chars), `source_context` (document path / conversation ID passed by caller)
- [ ] 1.6 Implement relative date normalisation: detect and convert temporal expressions ("yesterday", "last week") to absolute ISO 8601 before storing `event_time` on Candidate
- [ ] 1.7 Write unit tests: Store with known type → typed entity; Store with unknown type → Candidate; Store duplicate → NOOP
- [ ] 1.8 Write unit test: relative date in content → normalised `event_time` stored

## 2. LLM Merge Decision

- [ ] 2.1 Define merge decision prompt template: structured output schema `{action: ADD|UPDATE|DELETE_OLD_ADD_NEW|NOOP, target_id?: string, merged_content?: string}`; include `event_time` values when present for temporal reasoning
- [ ] 2.2 Add `shouldRunMergeDecision(similarity float64) bool` — returns true when similarity ≥ 0.70
- [ ] 2.3 Add `executeMergeDecision(ctx, newContent string, similar []GraphObject) (*MergeDecision, error)` — calls LLM, returns structured decision; falls back to threshold behavior on error
- [ ] 2.4 Implement `applyMergeDecision`: handle ADD / UPDATE / DELETE_OLD_ADD_NEW / NOOP branches
- [ ] 2.5 Add protection: skip merge deletion if target has `source = corrected`; override to ADD; log reason
- [ ] 2.6 Add structured logging: content hash, similar IDs, action, target_id, latency_ms
- [ ] 2.7 Add fallback: on LLM error, fall back to threshold-based supersession and log failure
- [ ] 2.8 Write unit tests: each action branch; corrected-source protection; error fallback

## 3. Janitor Job — Schema Discovery

- [ ] 3.1 Create `JanitorJob` struct in `domain/scheduler`
- [ ] 3.2 Implement Candidate clustering: group by `proposed_type` label first, then by embedding cosine distance ≤ 0.25 within group; use pgvector `<=>` operator
- [ ] 3.3 Implement threshold gate: configurable per signal category; default behavioral=5, structural=3, correction=1
- [ ] 3.4 Implement `proposeSchema(ctx, cluster []Candidate) (*SchemaProposal, error)` — LLM call producing `proposed_type`, `proposed_properties`, `proposed_relationship`, `confidence`
- [ ] 3.5 Create `SchemaProposal` graph object; set `status = pending` or `approved` based on auto-approval thresholds (`confidence ≥ 0.90 AND evidence_count ≥ 10`)
- [ ] 3.6 Add proposal cooldown: track rejected proposals; suppress re-proposal for same type within cooldown period (default: 30 days)
- [ ] 3.7 Register job in scheduler with configurable cron (default: weekly Sunday 02:00 UTC, env: `JANITOR_SCHEDULE`)
- [ ] 3.8 Add per-user isolation: one job per user with ≥ threshold unclustered Candidates; failure of one does not block others
- [ ] 3.9 Write integration test: N Candidates of same proposed_type → Janitor → SchemaProposal created
- [ ] 3.10 Write integration test: fewer than threshold Candidates → no proposal created
- [ ] 3.11 Write integration test: rejected proposal suppressed for cooldown period

## 4. Schema Application and Candidate Promotion

- [ ] 4.1 Implement `applySchemaProposal(ctx, proposal SchemaProposal) error` — extends schema pack with new type and relationship type; idempotent
- [ ] 4.2 Implement `promoteCandidate(ctx, candidate Candidate, schema SchemaType) error`:
  - LLM extracts typed properties from `raw_content` using new schema definition
  - Creates typed entity from extracted properties; preserves `event_time`, `confidence`
  - Creates typed relationship to `source_entity_id` (if present)
  - Archives the Candidate
- [ ] 4.3 Implement promotion-time dedup: before creating typed entity, hybrid search for similar existing typed entities of same type + same `source_entity_id`; invoke LLM merge decision including DELETE_OLD_ADD_NEW for contradiction handling
- [ ] 4.4 Run promotion for all Candidates in the evidence cluster when a SchemaProposal is applied
- [ ] 4.5 After promotion, future Store calls with the newly known type → typed entity directly (no Candidate created)
- [ ] 4.6 Write integration test: SchemaProposal approved → schema applied → Candidates promoted → Candidates archived
- [ ] 4.7 Write unit test: promotion-time dedup resolves contradiction to newer event_time
- [ ] 4.8 Write unit test: promotion is idempotent (second run produces no duplicates)

## 5. Standing Instructions

- [ ] 5.1 Define `Instruction` object type in the `schema-emergence` pack: properties `content`, `scope` (global | project), `source` (explicit | inferred), `use_count`, `last_seen`
- [ ] 5.2 Add `save_instruction` MCP tool: creates `Instruction` entity; warns if at cap (default 10); requires explicit `source = explicit` (instructions are never inferred)
- [ ] 5.3 Add `manage_instructions` MCP tool: `list | update | delete` actions
- [ ] 5.4 Add `queryStandingInstructions(ctx, userID, projectID string, limit int) ([]Instruction, error)` in `domain/chat/` — label-filtered query, no vector computation
- [ ] 5.5 Add `formatInstructionBlock(instructions []Instruction) string` — renders `## Standing Instructions\n- [content]\n...`
- [ ] 5.6 In `domain/chat/service.go`, at system prompt construction: fetch and prepend standing instructions block; enforce cap; append truncation note if applicable
- [ ] 5.7 Write integration test: instructions appear in system prompt without LLM tool call
- [ ] 5.8 Write integration test: no block injected when no instructions exist
- [ ] 5.9 Write integration test: truncation note when over cap

## 6. Confidence Decay

- [ ] 6.1 Create `CandidateDecayJob` in `domain/scheduler` — separate from typed entity decay
- [ ] 6.2 Candidate decay: `confidence *= 0.95` weekly for Candidates not referenced by a growing cluster; auto-discard below 0.1 with no grace period
- [ ] 6.3 Create `EntityDecayJob` — targets all typed graph entities with `confidence` property
- [ ] 6.4 Entity decay: `confidence *= 0.95` for entities where `last_used < now - staleness_threshold`; flag `needs_review` below 0.3; auto-archive below 0.1 after 7-day grace period
- [ ] 6.5 Exemptions: `category = instruction` and `Instruction` type entities get `needs_review = true` but are NOT auto-archived
- [ ] 6.6 In recall: when returned entity has `needs_review = true`, clear flag and restore confidence to recovery value (default: 0.5)
- [ ] 6.7 Add config vars: `DECAY_STALENESS_DAYS` (30), `DECAY_RATE` (0.95), `DECAY_REVIEW_THRESHOLD` (0.3), `DECAY_ARCHIVE_FLOOR` (0.1), `DECAY_GRACE_PERIOD_DAYS` (7), `DECAY_RECOVERY_VALUE` (0.5)
- [ ] 6.8 Write unit tests: Candidate decay, entity decay, needs_review flagging, instruction exemption, recall restores confidence

## 7. Temporal Anchoring

- [ ] 7.1 Ensure `event_time` (ISO 8601, optional) is included in Store API input and stored on both Candidate and typed entity
- [ ] 7.2 Pass `event_time` values of both new and existing entities to LLM merge decision prompt; instruct temporal contradiction resolution (newer `event_time` wins)
- [ ] 7.3 Implement query-time score decay in recall: `S_final = S_semantic * exp(-λ * age_days)`; use `event_time` if set, else `created_at`; per-category λ from config; `instruction` → λ=0
- [ ] 7.4 Add config vars: `DECAY_LAMBDA_DEFAULT` (0.003), `DECAY_LAMBDA_CORRECTION` (0.0005), `DECAY_LAMBDA_FACT` (0.005), `DECAY_LAMBDA_INSTRUCTION` (0.0)
- [ ] 7.5 Write unit tests: score decay ranking, instruction exemption (λ=0), merge decision resolves to newer event_time

## 8. Intent-Aware Retrieval

- [ ] 8.1 Define `QueryIntent` type: `PREFERENCE | CHRONOLOGICAL | FACTUAL | ANALYTICAL | INSTRUCTIONAL | UNKNOWN`
- [ ] 8.2 Define `RetrievalPlan` struct: `{intent, typeFilters, categoryFilter, dateRange, entityHints, strategy}`
- [ ] 8.3 Implement `classifyQueryIntent(ctx, query string) (*RetrievalPlan, error)` — LLM call returning structured plan; returns `UNKNOWN` on error
- [ ] 8.4 Implement strategy execution per intent type:
  - `PREFERENCE`: hybrid search + filter typed Preference entities + Instruction entities
  - `CHRONOLOGICAL`: date-range filter on `event_time`/`created_at` primary
  - `FACTUAL`: hybrid search with higher λ decay weight
  - `ANALYTICAL`: full hybrid including Cluster summary objects
  - `INSTRUCTIONAL`: Standing Instructions first, supplement with semantic if < 3 results
- [ ] 8.5 Gate behind feature flag `INTENT_AWARE_RETRIEVAL` (default: false)
- [ ] 8.6 Write unit tests: intent → strategy mapping; classification failure → hybrid fallback; user scoping preserved

## 9. Quick Wins from OSS Research

- [ ] 9.1 Add MD5 hash fast-path dedup: compute `MD5(raw_content)` before LLM merge call; if identical Candidate or typed entity exists, return NOOP immediately
- [ ] 9.2 Add `chunkThreshold` filter in recall: discard results with `S_semantic < 0.6` before returning (configurable via `RECALL_CHUNK_THRESHOLD`, default: 0.6)
- [ ] 9.3 Route Janitor and decay LLM calls to lightweight model (Haiku / Gemini Flash); configure via `JANITOR_MODEL` and `DECAY_MODEL` env vars

## 10. MCP Tool Updates

- [ ] 10.1 Update existing write tool to use the new Store API (typed/Candidate fallback)
- [ ] 10.2 Update recall tool: entity-anchored retrieval traverses typed relationships (not generic ANNOTATES); supplement with semantic search; apply query-time decay
- [ ] 10.3 Add `save_instruction` and `manage_instructions` tool definitions to `GetToolDefinitions()`
- [ ] 10.4 Update `memory_guidelines` MCP prompt to document: Store API typed/Candidate paths, Standing Instructions, when to use `save_instruction`, decay and confidence lifecycle, `event_time` temporal anchoring

## 11. E2E and Integration Tests

- [ ] 11.1 E2E test: Store with unknown type → Candidate created; Janitor run → SchemaProposal; approve → typed entity promoted
- [ ] 11.2 E2E test: Store contradictory observations → LLM decides DELETE_OLD_ADD_NEW → only newer entity active
- [ ] 11.3 E2E test: save_instruction → next chat session → instruction in system prompt without recall tool call
- [ ] 11.4 E2E test: multi-session flow with standing instruction persistence across sessions
- [ ] 11.5 E2E test: decay lifecycle — stale Candidate → discarded; stale typed entity → flagged → recalled → restored
- [ ] 11.6 E2E test: temporal contradiction — "User lives in NY" (event_time 2023) + "User lives in London" (event_time 2026) → London wins at promotion
- [ ] 11.7 E2E test: intent-aware retrieval enabled → CHRONOLOGICAL query returns date-filtered results
- [ ] 11.8 E2E test: score decay — same query, newer entity ranks above older entity with equal semantic score
- [ ] 11.9 E2E test: MD5 fast-path dedup — storing identical content twice returns NOOP without LLM call
- [ ] 11.10 E2E test: chunk threshold filter — low-similarity results excluded from recall response
- [ ] 11.11 E2E test: M9 — 3 Janitor cycles promote ≥ 70% of seeded Candidates to typed entities
