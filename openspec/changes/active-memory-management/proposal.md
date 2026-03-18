## Evaluation-First Approach

Before any implementation, we define and measure baseline metrics for every failure mode we claim to fix. The same benchmark suite runs post-implementation to prove improvement. Eight metrics, fixed test corpus. See `docs/features/active-memory-management/evaluation/benchmark-design.md`.

---

## Why

Every session starts from zero. The agent has no persistent knowledge of user preferences, entity-specific observations, or project conventions — and when it does try to recall, the recall is unreliable. Three concrete failure modes:

1. **No guaranteed context injection** — the LLM is instructed to recall but doesn't always comply, leaving sessions without critical context
2. **Contradictory observations coexist** — a fixed cosine similarity threshold can't detect logical conflicts; "user lives in NY" and "user lives in London" survive alongside each other
3. **Observations accumulate without quality management** — stale, low-confidence data is never retired; the recall pool degrades over time

A fourth, deeper problem: the original design stored agent observations as a generic `Note` type. This is too narrow a solution. The right model is for **observations to become properly typed schema entities** — a preference should become a `Preference`, a deployment pattern a `DeploymentPattern`, a review policy a `ReviewPolicy`. But you can't type something until you know what type it should be, and you don't know until you've seen enough examples of it.

## What Changes

- **New**: `Candidate` graph type — staging area for observations that don't yet fit any existing schema. Stores `raw_content`, `proposed_type` hint, source entity anchor, and context. Lifecycle: staged → promoted (to typed entity) or discarded (confidence decay).
- **New**: Janitor scheduled job — clusters Candidates by embedding similarity and proposed type; when enough evidence accumulates, generates a `SchemaProposal` via LLM; on approval applies the schema extension and promotes Candidates to properly typed entities
- **New**: `SchemaProposal` graph type — visible, auditable artifact of the schema discovery process; carries confidence, evidence count, proposed properties, and status (`pending` / `approved` / `applied` / `rejected`)
- **New**: Store API — single write operation that creates a typed entity if the schema exists, or falls back to a Candidate if not; caller gets back which path was taken
- **New**: Standing Instructions — typed `Instruction` entities auto-injected into every session system prompt synchronously; no LLM compliance required; replaces the `tier=core` concept from the earlier Note-based design
- **New**: LLM merge decision at write time — before creating a Candidate, dedup check against similar existing Candidates; same ADD/UPDATE/NOOP logic as memory merge
- **New**: LLM merge decision at promotion time — before creating a typed entity from a Candidate, dedup check against existing typed entities of that type
- **New**: Confidence decay on Candidates — Candidates that never accumulate enough evidence decay and are discarded; same batch decay job as memory decay
- **Retained**: All active management algorithms apply to the new architecture with widened scope: LLM merge, confidence decay, query-time score decay, bi-temporal tracking, cosine clustering, intent-aware retrieval
- **Removed**: `Note` type, `NoteCluster` type, `ANNOTATES` relationship, `save_note` / `recall_notes` / `manage_notes` MCP tools — replaced by the Store API, typed entities, and typed relationships

## Capabilities

### New Capabilities

- `schema-emergence`: Observations that don't fit existing schemas are staged as Candidates. The Janitor discovers patterns, proposes schema extensions, and promotes Candidates to properly typed entities. The schema grows from what agents actually observe.
- `standing-instructions`: Typed `Instruction` entities injected into every session system prompt synchronously — eliminates recall-compliance gap for cross-cutting always-apply rules
- `candidate-dedup`: LLM merge decision at Candidate write time — prevents duplicate staging of the same observation
- `schema-proposal-audit`: `SchemaProposal` objects are first-class graph nodes — visible, inspectable, approvable by users or auto-applied above confidence threshold
- `promotion-dedup`: LLM merge decision at promotion time — deduplicates against existing typed entities before creating new ones
- `note-llm-merge` (retained): LLM-in-the-loop dedup for all writes — structured decision (ADD/UPDATE/DELETE_OLD_ADD_NEW/NOOP)
- `note-decay` (retained): Batch confidence decay + query-time score decay `S_final = S_semantic · e^(-λ·age_days)` per observation category
- `temporal-anchoring` (retained): `event_time` on Candidates and typed entities for bi-temporal contradiction resolution
- `intent-aware-retrieval` (retained): Query intent classification selecting optimal search strategy per query type

## Impact

- `domain/graph`: new `Candidate` and `SchemaProposal` object types; `Store` API with typed/Candidate fallback
- `domain/scheduler`: `JanitorJob` (weekly) — clustering, schema proposal, Candidate promotion; `CandidateDecayJob` (weekly) — confidence decay on staged Candidates
- `domain/chat/service.go`: session start — inject Standing Instructions synchronously when `Instruction` objects exist for current user
- `domain/mcp/service.go`: updated write tool uses Store API; updated recall tool uses typed relationship traversal
- No new database tables — all operations use existing `kb.graph_objects` and `kb.graph_relationships`
- No breaking API changes — additive only

## Full Design

See `docs/features/schema-emergence/design.md` for full architecture, Candidate lifecycle, Janitor process, algorithm mapping, and M1–M9 benchmark metrics.
