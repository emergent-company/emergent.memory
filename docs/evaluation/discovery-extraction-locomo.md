# Discovery, Extraction & LoCoMo Evaluation

**Status:** Living document — updated after each evaluation run  
**Supersedes:** `docs/improvements/014-extraction-evaluation-enhancements.md`  
**Last updated:** 2026-05-26

---

## Purpose

Single source of truth for evaluating the quality of three interconnected pipelines:

1. **Discovery** — LLM-driven schema type discovery from documents
2. **Extraction** — entity and relationship extraction guided by a schema
3. **LoCoMo** — end-to-end recall benchmark using the LoCoMo10 conversation dataset

Each section lists explicit assumptions with judgment criteria and a verdict column updated after runs.

---

## 1. Discovery Pipeline Assumptions

Discovery takes a set of documents and a KB purpose (`project_info`) and produces candidate entity types and relationships. The pipeline runs in batches, refines across batches, and optionally discovers relationships between finalized types.

### Test coverage

| Test | File |
|---|---|
| `TestDiscovery_StartAndFinalizeSchema` | `tests/api/discovery_test.go` |
| `TestDiscovery_FinalizeWithoutOrgID` | `tests/api/discovery_test.go` |
| `TestDiscovery_ExtendExistingSchema` | `tests/api/discovery_test.go` |

---

### D1 — KB purpose signal

**Claim:** A meaningful `project_info` description produces more accurate, domain-relevant types than a UUID placeholder. The LLM uses the KB purpose as the primary framing signal.

**Verify:** Run discovery on identical documents with (a) a rich domain description and (b) an auto-generated placeholder name. Compare: type count, average confidence, absence of noise types, and presence of expected domain types.

**Gate:** Rich-purpose run produces ≥ as many correct types AND ≥ 1 type absent from the placeholder run that is clearly domain-relevant.

**Implementation note:** All three e2e tests now set meaningful `project_info` on project creation. Previously tests used UUID-style placeholders, giving the LLM zero domain signal.

| Run | Date | Result | Notes |
|---|---|---|---|
| — | — | — | Baseline not yet recorded |

---

### D2 — Anti-reification

**Claim:** Discovery on org-chart text does not produce entity types named `*Relationship`, `*Event`, or `*Activity`. These are relational concepts that should appear as graph edges, not nodes.

**Verify:** `TestDiscovery_StartAndFinalizeSchema` calls `assertNoReifiedTypes()` on the `discovered_types` array after job completion. Fails if any type name matches `(?i)(Relationship|Event|Activity)$`.

**Gate:** Zero reified type names in `discovered_types`.

**Known prior state:** Before the anti-reification prompt rule (added in service.go `buildTypeDiscoveryPrompt`), `ReportingRelationship` and `FoundingEvent` were produced. The rule is partially effective on deepseek-v4-flash — model still occasionally reifies in its CoT but less frequently in the final output.

| Run | Date | Result | Notes |
|---|---|---|---|
| Post-prompt-fix | 2026-05-12 | Partial — Category now splits correctly; ReportingRelationship still appears sometimes | Model-dependent; rule works ~70% of runs |

---

### D3 — Short-document category splitting

**Claim:** A products document with repeated classifiable values (e.g. "Electronics", "Accessories") causes the LLM to produce `Category` as a separate entity type rather than collapsing it into a string property of `Product`.

**Verify:** `TestDiscovery_ExtendExistingSchema` (run 2, products.txt document) — the finalize step includes `Category` as an `includedType`, verifying the LLM discovered it as a candidate.

**Gate:** Discovery run on products.txt contains `Category` (or synonym) in `discovered_types`.

| Run | Date | Result | Notes |
|---|---|---|---|
| Post-prompt-fix | 2026-05-12 | PASS — Category discovered with confidence 0.7, 3 occurrences | Short-doc splitting rule working |

---

### D4 — Cross-batch deduplication

**Claim:** If the same concept (e.g. `Person`) appears independently in two separate document batches, the refinement pass merges them into one type rather than producing two near-duplicate entries.

**Verify:** New test needed — feed 2-document batch where both docs mention Person entities, assert `discovered_types` contains exactly one Person-like type after refinement.

**Gate:** `count(types with name matching /Person/i) == 1`

| Run | Date | Result | Notes |
|---|---|---|---|
| — | — | — | Test not yet written |

---

### D5 — Unknown includedTypes warning

**Claim:** If a caller passes a type name in `includedTypes` at finalize time that was not in the discovered candidates, the server logs a warning (not a hard error).

**Verify:** POST finalize with `includedTypes: [{type_name: "FakeTypeXYZ123"}]`, check server logs for `WARN` entry mentioning the unknown type.

**Gate:** Server responds 200 (not 400/500); warning appears in `logs/server/server.log`.

**Note:** This is intentionally warn-not-reject. Callers may pass curated types that were manually defined rather than discovered. Upgrade to error only after establishing that callers reliably use discovered types.

| Run | Date | Result | Notes |
|---|---|---|---|
| — | — | — | Log-level assertion only; not in automated tests |

---

### D6 — Extend mode idempotency

**Claim:** Running finalize in `extend` mode on an existing schema returns the same `schema_id`, includes "extended" in the message, and does not create a duplicate installation.

**Verify:** `TestDiscovery_ExtendExistingSchema` — asserts `schemaID == schemaID2`, message contains "extended", installed pack count unchanged.

**Gate:** All three assertions pass.

| Run | Date | Result | Notes |
|---|---|---|---|
| Current | 2026-05-12 | PASS | Test passing |

---

### D7 — Relationship discovery gating

**Claim:** `discoverRelationships` runs only when both `include_relationships: true` AND `len(refinedTypes) > 1`. A single-type schema never triggers relationship discovery.

**Verify:** `TestDiscovery_FinalizeWithoutOrgID` — single-doc job with one type (`Doctor`) — relationship discovery should be skipped. Check Tempo traces for absence of relationship discovery span.

**Gate:** Job completes without a `discoverRelationships` span when only one type is finalized.

| Run | Date | Result | Notes |
|---|---|---|---|
| — | — | — | Trace verification not yet done |

---

### D8 — Schema property cross-references

**Claim:** When an entity type has a property that references another entity type, the LLM produces `{"type": "string", "description": "reference to <TypeName>"}` rather than an embedded object.

**Verify:** Inspect `inferred_schema` in finalize response — look for any property whose value is a nested object with an `id` sub-property (the bad pattern) vs a plain string type (the correct pattern).

**Gate:** Zero properties in finalized schema have object type with `id` sub-property.

| Run | Date | Result | Notes |
|---|---|---|---|
| — | — | — | Prompt guidance added; not yet asserting in tests |

---

## 2. Extraction Pipeline Assumptions

Extraction takes text and a schema (entity types + relationship types) and produces a knowledge graph. The pipeline is pure Go under `apps/server/domain/extraction/`.

### Test coverage

| Test | File |
|---|---|
| `TestExtraction_ManualSource_ExtractsEntities` | `tests/api/extraction_test.go` |
| `TestExtraction_DocumentSource_ExtractsFromDocument` | `tests/api/extraction_test.go` |
| `ExtractionEval_*` (benchmark) | `tests/experiments/extraction_eval_test.go` |

---

### E1 — Entity recall

**Claim:** The extractor identifies ≥ 80% of named entities present in the source text that match the installed schema types.

**Metric:** Entity recall = `|extracted ∩ golden| / |golden|`

**Golden dataset:** LoCoMo conv-0, sessions 1–3, `observation` field (pre-extracted natural language facts used as ground truth). See `tests/experiments/extraction_eval_test.go`.

| Run | Date | Entity Recall | Notes |
|---|---|---|---|
| Baseline (pre-eval) | — | ~0.80–0.96 (estimated, from extraction_test assertions) | No formal measurement yet |

**Target:** ≥ 0.90

---

### E2 — Relationship recall

**Claim:** The extractor identifies ≥ 85% of relationships between entities that are present in the source text and covered by the installed relationship types.

**Metric:** Relationship recall = `|extracted_rels ∩ golden_rels| / |golden_rels|`

**Note:** Relationship recall is highly volatile because: (a) the golden set is small, (b) type-name mismatches count as misses (see E3).

| Run | Date | Rel Recall | Notes |
|---|---|---|---|
| Baseline | — | 0–100% (high variance per document) | No stable measurement |

**Target:** ≥ 0.85

---

### E3 — Semantic relationship type matching

**Claim:** Near-equivalent relationship type names (e.g. `TRAVELS_TO` vs `VISITED`, `WORKS_AT` vs `EMPLOYED_BY`) should not count as misses. A semantic matching layer closes this gap.

**Metric:** Semantic Rel F1 — relationship pairs scored as match if cosine similarity of type names ≥ 0.8 (using embedding model) OR if the type pair is in a curated `SEMANTIC_EQUIVALENT_TYPES` map.

**Current state:** No semantic matching implemented. Raw string matching causes artificially low recall numbers when the LLM uses synonymous relationship names.

**Proposed implementation (from doc 014):**
- Curated map: `SEMANTIC_EQUIVALENT_TYPES map[string][]string` in extraction eval package
- Embedding-based fallback: compute embeddings of type name strings, threshold at 0.8

| Run | Date | Strict F1 | Semantic F1 | Notes |
|---|---|---|---|---|
| Baseline | — | ~0.31–0.44 | unmeasured | High variance; semantic F1 not implemented |

**Target:** Semantic F1 ≥ 0.60

---

### E4 — Over-extraction ratio

**Claim:** The LLM does not extract more than 1.5× the expected number of relationships. Current behaviour is 3–4× over-extraction, which dilutes precision and pollutes the knowledge graph.

**Metric:** Over-extraction ratio = `|extracted_rels| / |golden_rels|`

**Causes:** LLM creates implicit/inferred relationships not stated in text; low confidence threshold; no extraction budget in prompt.

**Proposed fix (from doc 014):** Add confidence threshold filter (discard rels < 0.7 confidence) and extraction budget hint in prompt (`"Extract only relationships explicitly stated in the text"`).

| Run | Date | Ratio | Notes |
|---|---|---|---|
| Baseline | — | ~3–4× | From manual inspection of extraction test runs |

**Target:** ≤ 1.5×

---

### E5 — Schema-guided vs schema-less delta

**Claim:** Running extraction with a discovery-produced schema installed produces higher entity F1 than running without any schema (schema-less mode defaults to generic entity types).

**Metric:** Entity F1 delta = `F1(with_schema) - F1(without_schema)` on same text.

**Status:** Not measured. Requires adding a schema-less baseline run to the extraction eval benchmark.

| Run | Date | With Schema | Without Schema | Delta | Notes |
|---|---|---|---|---|---|
| — | — | — | — | — | Not yet run |

**Target:** Delta ≥ +0.10

---

### E6 — Discovery → extract pipeline lift on LoCoMo

**Claim:** Running discovery on LoCoMo sessions first, then installing the discovered schema, then ingesting with schema-guided extraction, produces higher Token F1 on category 1 (single-hop) than raw ingest.

**Metric:** Token F1 delta on LoCoMo category 1.

**Status:** Not measured. Requires LoCoMo benchmark run in `obs` mode with a pre-installed discovery schema.

| Run | Date | Raw Ingest F1 | Schema-guided F1 | Delta | Notes |
|---|---|---|---|---|---|
| — | — | — | — | — | Not yet run |

**Target:** Delta ≥ +0.05

---

## 3. LoCoMo Benchmark

LoCoMo10 is a multi-session conversational memory benchmark. 10 conversations, up to 35 sessions each, 199 QA pairs per conversation across 5 categories.

**Dataset location:** `tools/benchmarks/locomo/locomo10.json`  
**Python runner:** `tools/benchmarks/locomo/run.sh`  
**Go smoke test:** `tests/experiments/locomo_benchmark_test.go` (build tag: `locomo_benchmark`)

### QA categories

| Num | Name | Description |
|---|---|---|
| 1 | single-hop | Fact stated explicitly in one session |
| 2 | temporal | Requires reasoning about when events occurred |
| 3 | open-domain | General knowledge question tied to conversation context |
| 4 | single-session | Fact contained within a single session (no cross-session reasoning) |
| 5 | adversarial | Question designed to mislead (correct answer is "no" or contradicts expectation) |

### 3.1 Baseline Results

| Run ID | Date | Sessions | Categories | Questions | Token F1 | Exact Match | Errors | Notes |
|---|---|---|---|---|---|---|---|---|
| smoke-v1 | 2026-05-10 | 1–5 | 1, 4 | 25 | **0.352** | 0.200 | 0 | Best result; only cats 1+4 |
| smoke-v3 | 2026-05-12 | 1–5 | 1,2,3,4 | 27/42 | 0.112 | 0.037 | 15 (quota) | Temporal added; 15 Google AI quota errors |

**Category breakdown — smoke-v3 (27 scored questions):**

| Category | Token F1 | Count |
|---|---|---|
| temporal | 0.083 | 12 |
| single-session | 0.133 | 6 |
| single-hop | 0.109 | 5 |
| open-domain | 0.167 | 4 |

**Root cause of v3 degradation:**
- 15 `Google AI quota exceeded` errors — not a quality regression, infra issue
- Even non-errored predictions are mostly empty strings — retrieval not surfacing stored facts
- **Fix:** Switch benchmark to DeepSeek model via `MEMORY_LLM_MODEL` env var (key already configured)

### 3.2 LoCoMo Assumptions

#### L1 — Single-hop recoverable after quota fix

**Claim:** Single-hop Token F1 returns to ≥ 0.38 (smoke-v1 level) after fixing the quota issue (switching to DeepSeek model for queries).

**Verify:** Re-run smoke with `MEMORY_LLM_MODEL=deepseek-v4-flash`, sessions 1–5, category 1.

**Gate:** Token F1 cat-1 ≥ 0.38.

| Run | Date | Cat-1 F1 | Notes |
|---|---|---|---|
| — | — | — | Not yet re-run with DeepSeek |

---

#### L2 — Temporal recall is structurally weak with raw ingest

**Claim:** Raw ingest (unstructured text) does not preserve temporal context in a form that the retrieval system can reason over. Temporal F1 will remain ≤ 0.15 with raw ingest regardless of quota fix.

**Rationale:** Raw ingest creates text chunks; temporal questions require knowing *when* an event occurred relative to other events. Without structured date/event extraction, semantic search cannot disambiguate temporal queries.

**Verify:** Re-run with quota fix — if temporal F1 stays ≤ 0.15 after quota fix, assumption confirmed.

| Run | Date | Temporal F1 | Notes |
|---|---|---|---|
| smoke-v3 | 2026-05-12 | 0.083 | Likely quota-inflated-low; need clean re-run |

---

#### L3 — Observations ingest mode improves temporal recall

**Claim:** Ingesting pre-extracted observations (the `observation` field in locomo10.json) rather than raw dialogue text increases temporal Token F1 by ≥ 0.10.

**Rationale:** Observations are atomic fact sentences ("Caroline attended an LGBTQ support group on 8 May 2023") — timestamped, single-speaker, easier to retrieve precisely.

**Verify:** Run benchmark twice — once `--ingest-mode raw`, once `--ingest-mode observations` — same sessions and questions, compare temporal F1.

**Gate:** Observations F1 ≥ raw F1 + 0.10.

| Run | Date | Mode | Temporal F1 | Notes |
|---|---|---|---|---|
| — | — | raw | — | Baseline needed |
| — | — | obs | — | Comparison needed |

---

#### L4 — Schema-guided ingest improves category-1 recall

**Claim:** Installing a discovery-derived schema before ingestion (so the MCP `remember` tool uses structured extraction) improves single-hop Token F1 vs raw ingest.

**Rationale:** Raw ingest creates generic text chunks; schema-guided ingest creates typed graph objects (Person, Activity, etc.) enabling more precise entity-level retrieval.

**Gate:** Delta ≥ +0.05 on category-1 Token F1.

| Run | Date | Mode | Cat-1 F1 | Delta | Notes |
|---|---|---|---|---|---|
| — | — | raw | — | — | Baseline needed |
| — | — | schema-guided | — | — | Requires discovery run first |

---

#### L5 — Empty predictions caused by retrieval failure, not missing data

**Claim:** The high rate of empty predictions in smoke-v3 (non-error rows) is caused by the retrieval system not surfacing stored facts, not by ingest failure. The facts are in the graph but the query doesn't find them.

**Verify:** After a successful ingest run, use the Memory API to manually query a known fact (e.g. "What is Caroline's identity?") directly. If the fact returns, retrieval routing in the benchmark query script is the problem, not ingest.

**Gate:** Manual graph search for a known entity returns the expected entity within 200ms.

| Run | Date | Result | Notes |
|---|---|---|---|
| — | — | — | Manual verification pending |

---

### 3.3 Running the Benchmark

#### Python full runner (10 conversations)

```bash
cd tools/benchmarks/locomo

# Set env
export MEMORY_API_URL=http://localhost:3012
export MEMORY_ACCOUNT_API_KEY=<your-key>
export MEMORY_PROJECT_ID=<project-id>
export MEMORY_LLM_MODEL=deepseek-v4-flash   # avoid Google AI quota issues

# Ingest sessions 1–5, all conversations
python ingest.py --sessions 1-5 --ingest-mode raw

# Query all categories
python query.py --sessions 1-5 --categories 1,2,3,4,5

# Evaluate
python evaluate.py --results results/query_results.jsonl --output results/eval_summary.json
```

#### Go smoke test (CI-friendly, ~3 min)

```bash
cd tests/experiments
go test -v -tags locomo_benchmark -run TestLoCoMo_Smoke ./...
```

Covers: conv-0, sessions 1–3, category 1, 5 questions. Gate: Token F1 ≥ 0.30.

---

## 4. Test Inventory

| Test | Assumptions covered | Build tag | Runtime |
|---|---|---|---|
| `TestDiscovery_StartAndFinalizeSchema` | D1, D2, D6, D8 | (none — standard e2e) | ~90s |
| `TestDiscovery_FinalizeWithoutOrgID` | D1, D7 | (none) | ~90s |
| `TestDiscovery_ExtendExistingSchema` | D1, D3, D6 | (none) | ~180s |
| `ExtractionEval_GoldenDataset` | E1, E2, E3, E4 | `extraction_eval` | ~5 min |
| `TestLoCoMo_Smoke` | L1, L5 | `locomo_benchmark` | ~3 min |

---

## 5. Results Log

Record structured results here after each significant evaluation run.

```
Format:
## YYYY-MM-DD — <description>
- Server version: <git sha>
- Model: <model name>
- Assumptions verified: D2 PASS, D3 PASS, L1 FAIL
- Notes: <any relevant context>
```

### 2026-05-12 — Post anti-reification prompt fix

- Server: main branch (post-migration 00120)
- Model: deepseek-v4-flash
- D2: PARTIAL — Category now splits correctly (PASS); ReportingRelationship still appears occasionally in CoT (model-dependent)
- D3: PASS — Category discovered with confidence 0.7, 3 occurrences
- D6: PASS — Extend mode returns same schemaID, message contains "extended"
- LoCoMo smoke-v3: Token F1 0.112 (15 quota errors; not a quality measurement)

### 2026-05-26 — D2/D7/D8 PASS; L1/L5 FAIL (retrieval gap)

- Server: main branch (post-migration 00120)
- Model: deepseek-v4-flash (new key sk-c9bf6...)
- D2: PASS — `TestDiscovery_StartAndComplete_D2_D8`: 4 types discovered, zero reified names
- D8: PASS — `TestDiscovery_StartAndComplete_D2_D8`: zero embedded property objects
- D7: PASS — `TestDiscovery_RelationshipGating_D7`: both subtests pass (no-rels empty, with-rels field present)
- L5: FAIL — all 5 predictions empty; retrieval not surfacing stored facts
- L1: FAIL — avg Token F1 = 0.000 (gate: ≥ 0.30)
- Root cause: LoCoMo sessions ingested as plain text documents but `remember` endpoint may not be triggering extraction+indexing pipeline, or query endpoint not searching the right index. Needs investigation.

---

### Why kbPurpose matters

`project_info` is fetched from `kb.projects` in `GetProjectInfo()` and injected as the first line of every discovery prompt: `"You are analyzing a knowledge base with the following purpose: ..."`. Without a meaningful description, the LLM produces generic types (Person, Organization) that match any domain, rather than domain-specific types (Doctor, Hospital, MedicalSpecialty) that are actually useful.

**Default fallback** (when `project_info` is empty):
```
"General purpose knowledge base for project documentation and knowledge management."
```
This fallback is better than empty but still provides no domain signal. Always set `project_info` when creating projects that will use discovery.

### Anti-reification prompt rule

Added to `buildTypeDiscoveryPrompt` in `apps/server/domain/discoveryjobs/service.go`. Rule text: relationships between entities (e.g. "reports to", "founded") must NOT become entity types; they belong as relationship edges. Effective ~70% of the time on deepseek-v4-flash — the model still occasionally reifies in CoT but less often in final JSON output. Callers control which types are included at `finalize` time, providing a second filter.

### LoCoMo quota issue

smoke-v3 used Google AI as the LLM backend. After 27 questions the quota was exhausted. Switch to `MEMORY_LLM_MODEL=deepseek-v4-flash` or configure `OPENAI_API_KEY` pointing to DeepSeek endpoint for all benchmark runs. The DeepSeek key is already set in the server env (`sk-749cd91dcfa44c0280740c0726423966`).

### Relationship discovery gate

```go
if config.IncludeRelationships && len(refinedTypes) > 1 {
    // discoverRelationships()
}
```

Single-type finalize calls (e.g. Doctor only) never trigger relationship discovery, saving LLM calls and avoiding trivial self-relationships.
