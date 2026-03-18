# Schema Emergence — Design Document

**Status:** Design
**Date:** 2026-03-18
**Supersedes:** `docs/features/agent-notes/design.md`

---

## Evolution of Thinking

This document records the full design progression, including the decisions that were reconsidered, so the reasoning is preserved.

### Stage 1 — agent-notes (rejected as final design)

The initial direction introduced a `Note` type as a first-class graph object with its own dedicated embedding. Notes would attach to any existing entity via an `ANNOTATES` relationship, forming a universal annotation layer. The argument was that agent observations should live in a separate retrieval space from entity embeddings to avoid polluting structured data with soft knowledge.

This was a sound insight about embedding separation but the wrong conclusion about storage type.

### Stage 2 — The typed objects challenge

If an agent observes "John prefers async communication", the right model is not:

```
Note{content: "John prefers async comms", category: preference}
  ── ANNOTATES ──► Person: John
```

It is:

```
Preference{domain: "communication", value: "async over sync", strength: strong}
  ── HAS_PREFERENCE ◄── Person: John
```

A typed `Preference` object is more meaningful, more queryable, and has a cleaner embedding. The `Note` type was a shortcut that avoided the harder question: what type should this actually be?

The counter-argument was "you can't type something until you know the type." That is the right problem — and it leads to a different solution.

### Stage 3 — Schema emergence (this design)

The schema should emerge from the data rather than be designed upfront. The mechanism:

1. When an agent wants to store an observation that doesn't fit any existing schema, it creates a **Candidate** — a lightweight staging node with the raw content and a proposed type hint
2. Candidates accumulate over time
3. A **Janitor** process runs periodically, clusters Candidates by embedding similarity and proposed type, and when enough evidence exists, proposes a schema extension
4. On approval (or auto-approval above a confidence threshold), the schema is applied and Candidates are promoted to properly typed entities

This makes the schema **emergent** — it grows from what agents actually observe, not from what developers predict they will observe.

---

## The Staging Layer — Candidate Type

A `Candidate` is not a permanent type. It is an ephemeral staging node that exists only until it is promoted or discarded.

```
Candidate
  raw_content:      string     The original observation text. Self-contained.
  proposed_type:    string?    Agent's best guess at what type this should become.
  source_entity_id: string?    The graph entity this observation is about.
  source_context:   string?    Document path, conversation ID, or other provenance.
  surrounding_text: string?    A few sentences of context window around the observation.
  confidence:       number     Starts at agent-provided level; decays if never promoted.
  event_time:       string?    ISO 8601. When the described event occurred.
  created_at:       string     Ingestion timestamp (automatic).
```

**Embedding target:** `raw_content` — the same embedding isolation principle that motivated Notes applies here. The Candidate's embedding is dense with observational signal.

**Lifecycle:** `staged` → `promoted` (archived after typed entity created) or `discarded` (decayed below confidence floor without enough cluster evidence).

---

## The Write API

A single store operation degrades gracefully based on schema availability:

```
graph.Store({
  content:    "Sarah requires two reviewers on auth PRs",
  type:       "ReviewPolicy",         // hint — not required, not authoritative
  entity_id:  "person:sarah-uuid",    // optional anchor
  context:    { source: "...", surrounding: "..." }
})
```

**If `type` matches an existing schema** → typed entity created, typed relationship to `entity_id` created, returns `{id: "reviewpolicy:abc", status: "created"}`.

**If `type` is unknown or omitted** → Candidate created, returns `{id: "candidate:xyz", status: "staged", proposed_type: "ReviewPolicy"}`.

The caller gets back which path was taken. The agent does not need to handle the distinction differently — it just tries to store what it observed.

**Dedup at write time:** before creating a Candidate, check for existing Candidates with the same `proposed_type` and cosine similarity ≥ 0.70. Apply LLM merge decision (ADD/UPDATE/NOOP) — same algorithm as save_memory. Prevents duplicate staging of the same observation.

---

## The Janitor — Schema Discovery Process

The Janitor is a scheduled background job (default: weekly) that looks at the accumulated Candidate pool and decides what new schema types to propose.

### Step 1 — Cluster by content + proposed_type

Group Candidates by embedding cosine distance ≤ 0.25 within the same `proposed_type` label. Each cluster is a body of evidence for a potential schema type.

```
Cluster A: 12 candidates, proposed_type="DeploymentPattern"
  "John deploys staging before prod"
  "Sarah always runs smoke tests before pushing"
  "Deploy runbook says: staging gate required"

Cluster B: 7 candidates, proposed_type="CommunicationPreference"
  "John prefers Slack over email"
  "Sarah doesn't answer messages before 10am"
```

### Step 2 — Threshold gate

Only proceed when evidence is sufficient. A single Candidate suggesting a type is noise. The threshold is configurable per-category of observation:

| Signal type | Default threshold |
|---|---|
| Behavioral (preferences, patterns) | 5 candidates |
| Structural (process, policy) | 3 candidates |
| Correction | 1 candidate (corrections are high-confidence) |

### Step 3 — LLM schema proposal

For each cluster above threshold, the Janitor sends the cluster members to the LLM and asks for a schema proposal:

```json
{
  "proposed_type": "DeploymentPattern",
  "proposed_properties": [
    {"name": "gate", "type": "string", "description": "Required deployment step"},
    {"name": "scope", "type": "string", "description": "What deployments this applies to"},
    {"name": "enforced_by", "type": "string", "description": "Who or what enforces this"}
  ],
  "proposed_relationship": {
    "type": "FOLLOWS_PATTERN",
    "source": "Person",
    "target": "DeploymentPattern"
  },
  "confidence": 0.87,
  "evidence_count": 12
}
```

### Step 4 — SchemaProposal object

The proposal is stored as a `SchemaProposal` graph object — a visible, auditable artifact:

```
SchemaProposal
  proposed_type:         string
  proposed_properties:   JSON
  proposed_relationship: JSON
  confidence:            number
  evidence_count:        number
  sample_candidate_ids:  string[]
  status:                pending | approved | applied | rejected
```

**Auto-approval:** if `confidence ≥ 0.90 AND evidence_count ≥ 10`, the proposal is auto-applied without human review. Otherwise it waits for approval.

### Step 5 — Schema application and Candidate promotion

Once a SchemaProposal is approved:

1. Schema extension applied to the schema pack
2. For each staged Candidate in the evidence cluster:
   - LLM extracts typed properties from `raw_content` using the new schema
   - Typed entity created from extracted properties
   - Typed relationship created to `source_entity_id` (if present)
   - Candidate archived
3. Future Candidates with the same `proposed_type` are now created as typed entities directly (they hit the first branch of the Store API)

---

## Standing Instructions

Some observations deliberately don't belong to a specific entity and shouldn't become schema types — they are always-apply cross-cutting rules:

```
"Never use ORMs — raw SQL only"
"Always check staging before touching prod"
"User prefers TypeScript in all new code"
```

These are **Standing Instructions** — a lightweight, small, permanent set of user-level rules. They are:
- Stored as typed `Instruction` entities (a built-in schema type, not discovered)
- Injected synchronously into every session system prompt before any LLM call
- Capped at a configurable limit (default: 10)
- Not subject to decay (category exemption)

This is the narrower, more honest version of what the `tier=core` concept was trying to achieve. The mechanism is the same — guaranteed session injection without LLM compliance required. The scope is limited to rules that are truly cross-cutting, not to arbitrary promoted notes.

---

## How the Original Algorithms Fit

All algorithms from the active memory management research apply to the new architecture. The scope widens from "Notes" to "Candidates + typed entities."

### LLM merge / dedup

Runs at two points:
1. **Write time** — before creating a Candidate, check for similar existing Candidates. ADD/UPDATE/NOOP decision prevents duplicate staging.
2. **Promotion time** — before creating a typed entity from a Candidate, check existing typed entities of that type for duplicates.

Algorithm is identical. Target changes from Memory → Candidate → typed entity.

### Confidence decay (batch)

`confidence *= decay_rate` applies to both Candidates and typed entities via a `confidence` property convention. Candidates that never accumulate enough evidence decay and get discarded. Typed entities that are never recalled decay and get flagged for review.

Exemptions: `category=instruction` and Standing Instructions are never auto-archived.

### Query-time score decay

`S_final = S_semantic · e^(-λ · age_days)` is a retrieval-layer concern. It applies to any search result regardless of whether the target is a Candidate or a typed entity. No change.

### Bi-temporal tracking

`event_time` (when the event occurred) vs `created_at` (ingestion time) is captured on Candidates at write time and carries forward to typed entities at promotion. Temporal contradiction resolution in the LLM merge prompt still works — now on typed entities instead of Notes.

### Clustering / reflection

The reflection algorithm serves two distinct purposes in the new design:

1. **Janitor schema discovery** — cluster Candidates by `proposed_type` + embedding similarity to detect emerging types (the primary clustering job)
2. **Prompt injection efficiency** — cluster related typed entities to generate compressed `Summary` objects for token-efficient session context

The `NoteCluster` concept generalises to a `Cluster` that can summarise any collection of related entities. Token efficiency goal (M5) is unchanged.

### Entity-anchored retrieval

Graph traversal from entity → typed relationship → typed observation objects. Now uses meaningful typed relationships (`HAS_PREFERENCE`, `FOLLOWS_PATTERN`, `REQUIRES`) instead of the generic `ANNOTATES`. Retrieval precision improves because the relationship type is part of the query.

### Intent-aware retrieval

Query intent classification (PREFERENCE / CHRONOLOGICAL / FACTUAL / ANALYTICAL / INSTRUCTIONAL) is independent of storage type. Strategy selection works the same — it just searches typed entities and Candidates instead of Notes.

---

## The Full Picture

```
Agent observes something during session
        │
        ├── matches existing schema type
        │         ↓
        │   Typed entity created
        │   Typed relationship to source entity
        │   (normal graph write path)
        │
        └── type unknown / no schema match
                  ↓
            Candidate created
            (raw_content + proposed_type + source context)
            Dedup check: LLM merge against similar Candidates
                  │
            [Candidates accumulate]
                  │
            Janitor runs (weekly)
                  │
            Cluster by embedding + proposed_type
                  │
            Threshold reached?
            ├── No → keep waiting, confidence decays
            └── Yes
                  │
                  ▼
            LLM proposes schema (properties, relationship type)
                  │
            SchemaProposal created (status: pending)
                  │
            Auto-apply (confidence ≥ 0.90) or await approval
                  │
                  ▼
            Schema extension applied
            Candidates promoted to typed entities
            Candidates archived
                  │
                  ▼
            Future observations of this type → typed entity directly
            (Candidate path no longer needed for this type)
```

---

## Benchmark Metrics (M1–M8)

All eight metrics from the original benchmark design remain valid. The storage mechanism changed but the quality measurements are schema-agnostic.

| Metric | What it measures | Notes on new architecture |
|---|---|---|
| M1 Core Hit Rate | Standing instructions present in every session | Same mechanism, narrower scope |
| M2 Dedup Precision | Contradictions resolved correctly | Now runs on Candidates + typed entities |
| M3 Recall Precision@K | Relevant results in top-K | Entity-anchored now uses typed relationships — expected improvement |
| M4 Recency Ranking | Newer observations rank above older | Query-time decay unchanged |
| M5 Token Efficiency | Cluster summaries vs raw expansion | Cluster summaries on typed entities |
| M6 Temporal Accuracy | Temporal contradictions resolved to newer event | event_time on Candidates + typed entities |
| M7 Recall p95 | Retrieval latency | Unchanged |
| M8 Store merge p95 | Write + dedup latency | Now measures Candidate write + LLM merge path |

A ninth metric is worth adding:

| M9 Schema Coverage Rate | % of Candidates promoted to typed entities within N Janitor cycles | Measures growth loop health |

---

## What This Replaces

- `docs/features/agent-notes/design.md` — the intermediate design. Superseded by this document.
- `docs/features/agent-memory-design.md` — the original design. Already superseded.
- The `Note`, `NoteCluster`, `ANNOTATES`, `BELONGS_TO_CLUSTER` types from agent-notes — replaced by `Candidate`, `SchemaProposal`, typed entities, typed relationships.
- `save_note` / `recall_notes` / `manage_notes` MCP tools — replaced by the generalized Store API and the Janitor process.
- `tier=core` on a generic Note — replaced by Standing Instructions (`Instruction` type, always injected).
