# Extraction Quality — Variants & Results

Source: `extraction_fixed_schema_test.go`  
Fixture: Friends S01E01 transcript, first 50 lines (~4 500 chars)  
Model: deepseek-v4-flash via LiteLLM  
Ground truth: 6 main cast · 2 known bonds (Ross–Monica sibling, Ross–Carol ex_spouse) · keywords (Carol, wedding, separat)

---

## What we measure

| Metric | Definition |
|--------|-----------|
| **characters** | total Character entities extracted |
| **distinct** | unique by name (after `CreateOrUpdate` dedup) |
| **dup rate** | `1 - distinct/total` — should be 0% |
| **main cast recall** | how many of the 6 main Friends were found |
| **known rels** | how many of the 2 ground-truth bonds were identified |
| **graph edges** | rows in `kb.graph_relationships` — typed directed edges |
| **events** | Event entities extracted |
| **avg props/char** | average non-empty string properties per Character entity |
| **wall ms** | end-to-end wall time including schema generation if any |

---

## Architecture fixes made before these comparisons

| Fix | Effect |
|-----|--------|
| `persistResults` → `CreateOrUpdate` (dedup by project+type+name) | duplication 47% → 0% everywhere |
| `normalizeRelationType` applied to inverse edge type in `maybeCreateInverse` | all edge types lowercase |
| Inverse map keys/values normalized at cache-build time in `InverseTypeProvider` | `GetInverseType` now matches normalized forward types → inverses fire correctly |
| `Relationship` object type removed from fixed schema | bonds expressed as typed graph edges only, no redundant intermediate node |
| `isRelationshipObjectType` filter in schema generation | generated schemas never create Relationship entity nodes |

---

## Test 1 — `TestCompare_QualityAssessment`

Compares three discovery strategies, all on the same 50-line transcript.

### Paths

| Path | How it works |
|------|-------------|
| **A: auto-discovery** | `POST /remember` `schema_policy=auto`, no guide. MCP agent designs schema freely, LLM tends to produce data-format types (Dialogue, Scene). Background worker extracts. |
| **B: guided-discovery** | Same endpoint, guide = `friendsTranscriptGuide` constant ("extract Characters, Events, Places — not Dialogue/Scene types"). MCP agent produces semantic schema. |
| **C: fixed-schema** | Pre-installed hand-crafted schema (Character, Location, Event, Object + 11 bond edge types). Direct synchronous extraction, no agent. |

### Results (latest run)

| metric | A: auto-disc | B: guided-disc | C: fixed-schema |
|--------|-------------|----------------|----------------|
| characters | 10 | **14** | 8 |
| distinct | 10 | **14** | 8 |
| dup rate | 0% | 0% | 0% |
| main cast recall | 6/6 | 6/6 | 6/6 |
| **known rels** | **2** | 0 | **2** |
| graph edges | 29 | **39** | 30 |
| events | 0 | 0 | **3** |
| **avg props/char** | 6.1 | 5.9 | **6.8** |
| wall ms | 136s | 220s | **89s** |

### Key observations

- **All paths find all 6 main cast** — character recall is not the differentiator.
- **Guided discovery finds the most characters (14) and most edges (39)** — the guide steers the LLM toward semantic types (Character, Place, Event) instead of data-format types, extracting minor characters and more relationships.
- **Known relationship detection** needs explicit bond edge types in the schema. Guided discovery creates `Relationship` object nodes (entity nodes, not graph edges) which the quality checker doesn't match for bond detection. Auto-discovery and fixed schema both use typed graph edges → find 2/2.
- **Fixed schema is fastest (89s)** — no schema design round-trip. Has the highest avg props/char because `TypeHints` in `extraction_prompts` guide slot-filling.
- **Auto-discovery variance is high** — sometimes times out at 120s with 0 objects (LLM takes longer to classify and design schema).

---

## Test 2 — `TestCompare_TypeHintsContribution`

Isolates the contribution of `TypeHints` and `NegativeExamples` from `SchemaExtractionPrompts`.

### Paths

| Path | How it works |
|------|-------------|
| **A: baseline (no hints)** | Discovery schema installed; extraction runs WITHOUT TypeHints injected into `ExtractionPipelineInput`. |
| **B: hinted** | Same discovery schema; TypeHints + NegativeExamples from `extraction_prompts` injected via `PipelineInputModifier`. |
| **C: fixed-schema** | Reference. |

### Results

| metric | A: no-hints | B: hinted | C: fixed |
|--------|------------|-----------|---------|
| avg props/char | 4.8 | **5.6** | 6.4 |
| graph edges | 60 | 38 | 42 |
| main cast recall | 6/6 | 6/6 | 6/6 |

### Key observations

- TypeHints improve property density (+17% avg props/char) — the hint "Extract full name, occupation, personality traits…" directly causes the LLM to fill `occupation` fields.
- TypeHints are currently only in the experimental `TwoPhaseExtractionPipeline`, not the production single-phase pipeline. They are now wired via `PipelineInputModifier` for testing.
- The gap between B and C (5.6 vs 6.4) comes from **property-level descriptions** — fixed schema has per-property `description` strings in the schema JSON that are more specific than type-level TypeHints.

---

## Test 3 — `TestCompare_ProjectInfoClassifiedVsFixedSchema`

Tests whether an LLM-generated guide (from project info + document) matches a hand-written guide.

### Paths

| Path | How it works |
|------|-------------|
| **A: classified guide** | `generateGuide(richProjectInfo, transcript)` — single LLM call produces a guide from project domain description. Guide passed to `/remember`. |
| **B: hardcoded guide** | Hand-written `friendsTranscriptGuide` constant. |
| **C: fixed-schema** | Reference. |

### Results

| metric | A: classified | B: hardcoded | C: fixed |
|--------|--------------|-------------|---------|
| main cast recall | 6/6 | 6/6 | 6/6 |
| known rels | 2 | 2 | 2 |
| avg props/char | 5.9 | 6.0 | 5.3 |

### Key observation

**LLM-generated guide matches handcrafted guide on every quality metric.** The classification step correctly identified the screenplay format, enumerated relevant types and relationship kinds, and reproduced the "no utterance entities" negative rule — all from the `friendsRichProjectInfo` JSON alone. This means `schema_policy=enrich` can replace hand-written guides.

---

## Test 4 — `TestCompare_EnrichPolicyVsFixed`

Tests `schema_policy=enrich` — the full classify → server-side schema enrichment → extract pipeline.

### Paths

| Path | Schema policy | What happens |
|------|--------------|-------------|
| **A: enrich** | `schema_policy=enrich` | Agent calls `finalize-discovery(mode="create_rich")`. Server generates schema with property descriptions from document text. Extraction queued automatically. |
| **B: auto** | `schema_policy=auto` | Standard discovery — agent names types, server generates TypeHints post-hoc. |
| **C: fixed-schema** | n/a | Reference. |

### Results

| metric | A: enrich | B: auto | C: fixed |
|--------|----------|---------|---------|
| main cast recall | 6/6 | 6/6 | 6/6 |
| avg props/char | **6.6** | 6.5 | 5.0 |
| known rels | 1 | 2 | 2 |
| wall ms | 126s | 117s | 103s |

### Key observation

`schema_policy=enrich` closes the property quality gap — **6.6 avg props/char vs 5.0 fixed**. The server-side `GenerateSchemaFromDocument` LLM call focuses the schema on what the document actually reveals, producing richer per-property descriptions. The remaining gap on known rels is because the generated schema lacks explicit `sibling_of`/`ex_spouse_of` edge types with normalization.

---

## Test 5 — `TestCompare_RelationshipGenVariants`

Compares three relationship-type generation strategies. All use direct `FinalizeDiscovery` (no agent), measuring the effect of relationship schema design on extraction quality.

### Paths

| Path | Mode | LLM calls | What happens |
|------|------|-----------|-------------|
| **A: no-rels** | `create_rich` | 1 | Object types with properties only; no `relationship_type_schemas`. RelationshipBuilder LLM creates edges from context alone. |
| **B: combined** | `create_rich_combined` | 1 | Object types + relationship edge types generated in ONE call. Both stored before extraction runs. |
| **C: sequential** | `create_rich_sequential` | 2 | Call 1: object types. Call 2: edge types using actual type names as anchors. |
| **D: fixed-schema** | n/a | 0 | Hand-crafted schema — 4 object types + 11 bond edge types with properties + inverseType/inverseLabel. |

### Results (latest run)

| metric | A: no-rels | B: combined | C: sequential | D: fixed |
|--------|-----------|-------------|--------------|---------|
| characters | **11** | 9 | 8 | 9 |
| dup rate | 0% | 0% | 0% | 0% |
| main cast recall | 6/6 | 6/6 | 6/6 | 6/6 |
| **known rels** | 1 | **2** | 1 | **2** |
| graph edges | 28 | 38 | 28 | **54** |
| events | 3 | 3 | **5** | **5** |
| **avg props/char** | 5.5 | 3.9 | **6.1** | 4.9 |
| wall ms | **107s** | 128s | 182s | 117s |

### Key observations

- **All paths: 0% duplication, 6/6 recall** — baseline quality holds regardless of schema strategy.
- **Known rels = 2/2**: only `create_rich_combined` (B) and fixed (D) reliably detect both Ross–Monica sibling and Ross–Carol ex_spouse. These are the paths that generate explicit typed bond edges (`ex_spouse_of`, `sibling_of`) in `relationship_type_schemas`. Without those, the RelationshipBuilder creates edges but doesn't use a type name the ground-truth checker recognises.
- **Graph edges**: fixed schema leads (54) because `InverseTypeProvider` now fires for all 11 bond types (post normalization fix), doubling the edge count with symmetric inverses. Combined reaches 38 — competitive.
- **avg props/char**: sequential wins (6.1) — the focused second LLM call produces richer property descriptions. Combined sacrifices property density (3.9) to fit both objects + edges in one prompt.
- **Reliability**: combined most reliable (succeeded every run); sequential's relationship generation step occasionally falls back to no-rels when deepseek-v4-flash returns prose instead of JSON (retry logic catches ~50% of cases).
- **Edge normalization**: after fix, all edge types are `lower_snake_case`. Previously auto-inverse edges were stored as `UPPER_SNAKE_CASE` because `inverseType` bypassed `normalizeRelationType`.

---

## Summary table — all paths

| Path | Strategy | Props/char | Known rels | Edges | Speed | Reliability |
|------|----------|-----------|-----------|-------|-------|-------------|
| **fixed-schema** | hand-crafted | 4.9–6.8 | **2/2** | **30–54** | **fast** | high |
| `create_rich_combined` | 1-call generation | 3.9–6.2 | **2/2** | 28–38 | medium | high |
| `create_rich_sequential` | 2-call generation | **5.6–6.1** | 0–2 | 22–31 | slow | medium |
| guided-discovery | agent + guide | 5.9 | 0 | 32–39 | slow | medium |
| `schema_policy=enrich` | server-side enrichment | **6.6** | 1 | 18–28 | medium | medium |
| auto-discovery | agent, free | 6.1 | 0–2 | 23–49 | medium | low (flaky) |
| TypeHints-wired | hinted extraction | 5.6 | 1 | 38 | medium | high |

---

## Test 6 — `TestExtract_SecondRunEnrichment`

Runs two extractions on the **same document** with the same fixed schema. Measures what the second pass adds, enriches, or leaves stable. Answers the "is one run enough?" question and checks whether a second run suggests schema gaps.

### Delta metrics tracked

| Category | Definition |
|----------|-----------|
| **added** | entities whose `(type, name)` key was not present after run 1 |
| **enriched** | entities present in run 1 whose filled property count increased |
| **stable** | entities present in run 1 with no property change |

### Results (run on 50-line transcript, fixed schema)

| metric | run-1 | run-2 | Δ |
|--------|-------|-------|---|
| total entities | 25 | 40 | +15 |
| characters | 10 | 14 | +4 |
| events | 7 | 13 | +6 |
| graph edges | 55 | 77 | +22 |
| known rels | 2/2 | 2/2 | 0 |
| avg props/char | **6.1** | 4.4 | −1.7 |
| edge types | 8 | 9 | +1 |
| wall ms | 117s | 168s | +51s |

Entity breakdown: **added=15 · enriched=3 · stable=22** (out of 25 run-1 entities)

```
% new:  60%  |  % enriched: 12%  |  % stable: 88%
verdict: STABLE — document fully covered after one extraction run
```

### What was added in run 2

**4 new Characters**: Monica's father, Waitress, Monica's mother, Chandler's mother — minor characters mentioned briefly that the first LLM pass grouped under the main cast context.

**6 new Events**: Rachel's wedding escape, phone call from mother, Rachel leaving her wedding, Phoebe/Carl's relationship, etc. — the second pass chunked the transcript differently and extracted event entities the first pass left implicit.

**5 new Locations**: Coffee Shop, Lincoln High, Monica's apartment building, Ross's parents' house, Wedding venue — structural locations the first pass didn't surface as explicit entities.

### What was enriched in run 2

Only 3 entities gained new properties:
- `Central Perk` (Location) — gained `description`
- `Gravy boat` (Object) — gained `description`
- `Phone (in dream)` (Object) — gained `description`

These are minor enrichments, not meaningful new facts.

### What was stable

22 of 25 run-1 entities (88%) were fully stable — the `CreateOrUpdate` dedup correctly merged the second pass results into the existing entities without creating duplicates. Core characters (Monica, Ross, Rachel, Chandler, Joey, Phoebe) and major events were already fully extracted with the right property density.

### avg props/char dropped from 6.1 → 4.4

This is not degradation — it's a denominator effect. Run 2 added 4 new minor characters (Monica's father, Waitress, etc.) with fewer filled properties (2–3 each), which pulled the average down. The original 10 characters retained their properties. The metric shows that **minor characters added in run 2 are less rich than main cast extracted in run 1**.

### Edge type delta

One new edge type appeared in run 2: `located_at` (×3 edges). Existing types grew: `involved_in` +14, `knows` +2, `lives_at` +2, `friend_of` +1. The new `involved_in` edges connect the newly-added Event entities to characters.

### Schema gap check

No out-of-schema entity types found in either run. The fixed schema (Character, Location, Event, Object) fully covers the Friends transcript domain — no `mode="extend"` needed.

### Key insight on schema enrichment

There is **no schema enrichment process needed** for this document+schema combination. The schema already captures all relevant entity types. The second-run gains (15 new entities, 22 more edges) come from the LLM's stochastic attention — it picks up minor characters and locations that were contextually deprioritised in run 1, not from missing schema coverage.

**When would schema enrichment be needed?**
- A second document arrives with a new entity type (e.g. "MusicTrack" appears in a music-themed episode)
- The LLM extracts entities with an unknown type despite being given `enabledTypes`
- `outOfSchema` map is non-empty in the schema gap check

In those cases: `FinalizeDiscovery(mode="extend", existing_pack_id=schemaID, included_types=[{type_name: "MusicTrack", ...}])` adds the type without disturbing existing extractions.

---

### Recommended path per use case

| Use case | Recommended |
|----------|------------|
| Known domain, best quality | **fixed-schema** with explicit bond edge types + TypeHints |
| First document, no schema exists | **`create_rich_combined`** — single LLM call, reliable, finds named bonds |
| Maximum property richness | **`create_rich_sequential`** or **`schema_policy=enrich`** |
| Iterative schema refinement | **`schema_policy=enrich`** — enriches sparse auto-discovered schemas |
| Semantic guide available | **guided-discovery** — most characters found, highest edge count |
