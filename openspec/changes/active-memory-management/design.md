## Context

The full design rationale and architecture is in `docs/features/schema-emergence/design.md`. This document records the key decisions made during design, the alternatives considered, and the constraints that governed each choice.

Key existing infrastructure available:
- `domain/mcp/service.go`: MCP tool execution and prompt definitions
- `domain/graph/service.go`: `Create`, `Search`, `HybridSearch`, `Version`, `Delete`, `BatchUpdate`
- `domain/scheduler`: Job queue with `FOR UPDATE SKIP LOCKED` dequeue, retry, dead-letter
- `domain/chat/service.go`: SSE stream with system prompt construction
- Google ADK-Go: LLM calls via `SequentialAgent` and `LlmAgent` primitives
- pgvector: `embedding <=> $vector` for cosine distance queries

## Goals / Non-Goals

**Goals:**
- Schema emergence — the knowledge graph schema grows from what agents actually observe, not from what is designed upfront
- Candidate staging — observations without a schema type are staged, not discarded
- Janitor discovery — periodic background process that finds patterns, proposes schema extensions, promotes Candidates to typed entities
- Guaranteed standing instruction injection at every session start (no LLM compliance required)
- LLM-in-the-loop dedup at both Candidate write time and promotion time
- Bi-temporal tracking (`event_time` + `ingestion_time`) for temporal contradiction resolution
- Query-time score decay applied at retrieval using `S_final = S_semantic · e^(-λ·age_days)`
- Confidence decay with configurable thresholds and auto-discard for Candidates; auto-archival for typed entities
- Intent-aware retrieval classifying query type before executing search

**Non-Goals:**
- Cross-user observation sharing (remains user-scoped via `actor_id`)
- Fine-tuning a model on schema discovery decisions
- A separate observations service or infrastructure (no new databases or services)
- Storing agent observations as properties on existing entity types (re-embedding cost, schema coupling, retrieval quality degradation)

## Decisions

### Decision 1: Candidate Type as Staging Area — Not a Permanent Type

**Decision**: Introduce a `Candidate` graph object type as an ephemeral staging area for observations that don't fit any existing schema. `Candidate` objects have a defined lifecycle: `staged` → `promoted` (archived after typed entity created) or `discarded` (confidence decayed below floor).

**Rationale**: You cannot type an observation until you know what type it should be. You don't know the type until you've seen enough instances of it. The Candidate is the mechanism that holds observations while the system accumulates evidence for a new type. It is not a permanent representation — it is a transitional state.

**Alternative considered**: `Note` as a permanent first-class type (the agent-notes design). Rejected: a generic `Note{category: preference}` is strictly worse than a typed `Preference` entity — less queryable, less meaningful, lower embedding quality (generic category label vs specific preference content), requires category filtering instead of type traversal.

**Alternative considered**: Properties on existing entity types (e.g., `agent_notes: string[]` on `Person`). Rejected: re-embeds entire entity on every addition; degrades entity embedding quality; requires schema modification per type; mixing structured data with observational content reduces retrieval precision for both.

### Decision 2: Store API — Single Operation with Typed/Candidate Fallback

**Decision**: One write operation. If the requested type exists in the installed schemas, create a typed entity and relationship. If not, create a `Candidate` with the type hint and source context. The caller gets back which path was taken.

**Rationale**: The agent calling the tool is in context — it just processed a document or conversation. It has the best signal for what type the data should be. Capturing that type hint at write time is more valuable than having the Janitor guess the type later from content alone. The fallback to Candidate is transparent — the agent doesn't need to handle the distinction.

**Key property**: `proposed_type` on a Candidate is a hint, not authoritative. The Janitor may discover that 12 Candidates labeled `DeploymentPattern` actually represent two distinct types. The LLM schema proposal step resolves this.

### Decision 3: Janitor Clustering — Proposed Type + Embedding Similarity

**Decision**: Janitor clusters Candidates by `proposed_type` label first, then by embedding cosine distance ≤ 0.25 within each label group. Both signals are required: type label prevents cross-type confusion; embedding similarity catches observations the agent labeled differently but are semantically equivalent.

**Rationale**: Using only embedding similarity would merge observations that happen to be semantically close but describe different phenomena. Using only type labels ignores agents that label the same thing differently across sessions. The two-signal approach is more robust.

**Threshold gate**: configurable per signal category. Behavioral observations (preferences, patterns) require 5+ Candidates. Structural observations (policies, processes) require 3+. Corrections require 1 (high signal, low noise by design).

### Decision 4: SchemaProposal as First-Class Graph Object

**Decision**: When the Janitor decides a new type is warranted, it creates a `SchemaProposal` graph object with `status: pending`. Auto-apply when `confidence ≥ 0.90 AND evidence_count ≥ 10`. Otherwise surface for review.

**Rationale**: Schema modification is consequential — once a type exists and entities are promoted, removing it is expensive. Surfacing proposals as auditable objects gives users visibility into what the system is discovering. It also enables rollback: rejecting a proposal keeps Candidates staged rather than discarding them.

**Protection**: rejected proposals prevent re-proposal of the same type for a configurable cooldown period (default: 30 days). Prevents the Janitor from repeatedly proposing types the user doesn't want.

### Decision 5: LLM Merge at Write Time — Candidate Dedup

**Decision**: Before creating a Candidate, run hybrid search for similar Candidates with the same `proposed_type`. If any have cosine similarity ≥ 0.70, invoke LLM merge decision (ADD/UPDATE/NOOP). Same structured output schema as the memory merge decision.

**Rationale**: Without dedup at write time, the Candidate pool accumulates many near-duplicate observations of the same phenomenon. The Janitor would then be trying to cluster duplicates rather than finding genuine patterns. Write-time dedup keeps the Candidate pool clean and the clustering signal strong.

**No DELETE_OLD_ADD_NEW at Candidate level**: Candidates don't yet have enough structure to determine temporal supersession. Contradiction resolution happens at promotion time when the full typed schema is available.

### Decision 6: LLM Merge at Promotion Time — Typed Entity Dedup

**Decision**: Before creating a typed entity from a Candidate, check existing typed entities of the same type for the same `source_entity_id`. If similar entities exist (cosine similarity ≥ 0.70), invoke LLM merge decision including DELETE_OLD_ADD_NEW for contradiction handling.

**Rationale**: The Candidate pool may contain observations that were added at different times about the same phenomenon. Without promotion-time dedup, the same person ends up with two `Preference` entities saying contradictory things. Promotion is the right point to resolve this because the full typed schema is now available and temporal reasoning (via `event_time`) can operate on structured fields.

### Decision 7: Standing Instructions — Typed, Not Promoted, Not Decayed

**Decision**: Cross-cutting always-apply rules (user preferences, project conventions, standing orders) are stored as typed `Instruction` entities — a built-in schema type, not a discovered one. They are injected synchronously into every session system prompt. They are exempt from confidence decay and auto-archival.

**Rationale**: The original `tier=core` design promoted arbitrary Notes to always-inject. This was too broad — it made the core tier a general mechanism rather than a specific one. Standing Instructions are a specific, named concept: rules the user wants the agent to always know. Making them a typed entity (rather than a tier property on a generic type) makes the concept explicit, queryable, and documentable.

**Cap**: 10 Standing Instructions per user (configurable). Exceeding the cap triggers a warning at write time — the user must explicitly demote an existing instruction to add a new one.

**Injection**: synchronous at session start in `domain/chat/service.go`, before any LLM call. Label-filtered query (no vector computation). Guaranteed presence regardless of LLM compliance.

### Decision 8: Two-Layer Decay — Candidate + Typed Entity

**Decision**:
1. **Candidate decay** (weekly): `confidence *= 0.95` for Candidates that have not been referenced by a growing cluster. Auto-discard below 0.1. No grace period for Candidates — they are staging objects, not permanent records.
2. **Typed entity decay** (weekly): `confidence *= 0.95` for entities not recalled within staleness threshold. Flag `needs_review` below 0.3. Auto-archive below 0.1 after 7-day grace period. Exempt: `category=instruction`, Standing Instructions.
3. **Query-time score decay**: `S_final = S_semantic · e^(-λ · age_days)`. Uses `event_time` if set, else `created_at`. Per-category λ: default=0.003, correction=0.0005, fact=0.005, instruction=0.0.

**Rationale**: Candidate decay ensures the staging area self-cleans. Without it, unpromotable Candidates (observations that never reach threshold) accumulate indefinitely. Typed entity decay ensures the knowledge graph quality improves over time as stale knowledge is retired.

### Decision 9: Bi-Temporal Tracking on Candidates

**Decision**: `event_time` (optional, ISO 8601) captured on `Candidate` at write time alongside `created_at`. Carries forward to typed entity at promotion. Relative date expressions normalised to absolute ISO timestamps before storage.

**Rationale**: The agent is in context at write time and can provide `event_time`. The Janitor runs out of context and cannot recover this signal later. Capturing temporal information at write time — even on a staging Candidate — is critical for contradiction resolution at promotion time.

### Decision 10: Intent-Aware Retrieval

**Decision**: Optional intent classification at the start of recall queries. Gated by `NOTES_INTENT_AWARE_RETRIEVAL=false` (default off in v1). Strategies operate on typed entities and Candidates.

| Intent | Strategy |
|---|---|
| `PREFERENCE` | Semantic + filter typed Preference entities + Instruction entities |
| `CHRONOLOGICAL` | Date-range filter on `event_time`/`created_at` primary |
| `FACTUAL` | Hybrid + higher λ decay (recent facts preferred) |
| `ANALYTICAL` | Full hybrid including Cluster summary objects |
| `INSTRUCTIONAL` | Standing Instructions first, supplement with semantic if < 3 results |

## Risks / Trade-offs

**[Risk] Janitor proposes wrong schema type** → Mitigation: SchemaProposal surfaces for review below confidence threshold; rejected proposals have cooldown period; Candidates are not discarded on rejection.

**[Risk] Candidates accumulate faster than Janitor can promote** → Mitigation: Candidate decay self-cleans the staging pool; high-frequency observation types reach threshold quickly and get promoted; configurable thresholds.

**[Risk] Promoted typed entities have low-quality property extraction** → Mitigation: LLM property extraction at promotion time uses the full `raw_content` plus `surrounding_text` captured at write time; promotion-time dedup resolves contradictions.

**[Risk] Schema proliferation — too many narrow types** → Mitigation: evidence threshold gates prevent low-signal types from being proposed; rejected proposals have cooldown; users can review and reject proposals.

**[Risk] Standing instruction injection bloats system prompt** → Mitigation: hard cap (default 10); explicit write required; no automatic promotion from typed entities.

## Success Criteria

| Metric | Baseline | Target |
|---|---|---|
| M1 Standing Instruction Hit Rate | 0% | 100% |
| M2 Dedup Precision | ~40% | ≥ 85% |
| M3 Recall Precision@5 | measure | baseline + 10% |
| M4 Recency Ranking | ~50% | ≥ 90% |
| M5 Token Efficiency | 1.0× | ≤ 0.40× |
| M6 Temporal Accuracy | ~50% | ≥ 90% |
| M7 Recall p95 | measure | ≤ baseline + 50ms |
| M8 Store merge p95 | N/A | ≤ 600ms |
| M9 Schema Coverage Rate | N/A | ≥ 70% of Candidates promoted within 3 Janitor cycles |
