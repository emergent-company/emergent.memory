# Agent Notes — Design Document

**Status:** Superseded by `docs/features/schema-emergence/design.md`
**Date:** 2026-03-18

---

## The Core Insight

When an agent learns something about an entity in the graph — "John prefers async communication", "this file breaks when you touch session handling" — the instinct is to store it as a property on the existing object. But that has a fundamental problem:

**Every property update re-embeds the entire object.**

A `Person` object with 15 typed properties produces an embedding that represents all 15 fields blended together. Adding an agent observation to those 15 fields dilutes both signals — the structured data (name, email, role) and the soft knowledge (communication style, preferences, quirks) get mixed into one vector optimised for neither.

A separate `Note` object with its own embedding gives you a **focused retrieval signal** for exactly what the agent cares about. The entity's embedding stays clean. The note's embedding is dense with behavioural and contextual knowledge.

This is not a memory system. It is a **universal annotation layer** — a generic mechanism for enriching any knowledge graph with agent observations that live in their own dedicated retrieval space.

---

## What Changes From the Original Design

The original `agent-memory-design.md` proposed a standalone `Memory` type for user preferences and cross-session recall. That framing was too narrow:

| Original framing | This design |
|---|---|
| "Agent memory" — stores what the LLM remembers | "Graph annotations" — enriches any entity with observations |
| Primarily global user preferences | Primarily entity-attached observations + global preferences |
| Parallel to the graph | Layered on top of the graph |
| Schema pack for memory-specific use cases | Universal schema pack that works with any existing schema |
| Re-embedding happens on the Memory object | Re-embedding is isolated to the Note — never touches the source entity |

The type is renamed `Note` (was `Memory`). The grouping type is renamed `NoteCluster` (was `MemoryContext`). The schema pack is renamed `agent-notes` (was `agent-memory`).

---

## Architecture

```
Any graph (any schema combination)     Annotation layer (agent-notes schema pack)
────────────────────────────────────   ───────────────────────────────────────────
Person: John                      ←──  Note: "Prefers async, Slack over email"
  name, email, org, role               Note: "Responds faster Fri–Mon"
  [own embedding: structured data]     [own embedding: behavioural observation]

File: auth/middleware.go          ←──  Note: "Breaks when session handling changes"
  path, size, language                 Note: "Sarah owns this — always tag on PRs"
  [own embedding: code context]        [own embedding: operational knowledge]

Project: payments-api             ←──  Note: "Deploy staging before prod, always"
  name, description, stack             Note: "Uses feature flags in config/features.go"
  [own embedding: project context]     [own embedding: process knowledge]

(no entity — global)                   Note: "User always prefers TypeScript over JS"
                                        Note: "Never use ORMs — raw SQL only"
                                        [own embedding: cross-cutting preference]
```

Notes link to source entities via `ANNOTATES` relationships. Notes without an entity are free-standing (global preferences, cross-cutting instructions).

### The Two Retrieval Paths

When recalling notes for a given context:

1. **Entity-anchored retrieval**: traverse `ANNOTATES` edges from entities currently in scope → surface notes directly attached to those entities
2. **Semantic retrieval**: hybrid search (embedding + FTS) across all notes for the current user

Both paths operate on the note's own embedding — never on the source entity. Results are merged and ranked. Entity-anchored results are boosted because they are explicitly scoped to what the agent is working with.

### The Background Improvement Loop

```
Agent or user session
        │
        ├── observes pattern about an entity
        │         ↓
        │   save_note(content, entity_id?)
        │         ↓
        │   Note created with its own embedding
        │         ↓
        │   ANNOTATES edge to source entity (if entity_id provided)
        │
Scheduler (background, periodic)
        │
        ├── clusters semantically related Notes
        │         ↓
        │   NoteCluster created (LLM-synthesised summary)
        │         ↓
        │   BELONGS_TO_CLUSTER edges for members
        │
        ├── confidence decay on stale Notes
        │         ↓
        │   low-confidence Notes flagged → archived
        │
Next session
        ↓
NoteCluster summaries injected into system prompt
(compressed, token-efficient — not raw Notes)
```

The base graph is never modified. The annotation layer accumulates on top of it.

---

## Schema Pack: `agent-notes`

### Object Type: `Note`

A single agent observation — a fact, preference, pattern, correction, or instruction.

| Property | Type | Required | Description |
|---|---|---|---|
| `content` | string | yes | The observation in natural language. Self-contained, specific. |
| `category` | string | yes | `preference` \| `pattern` \| `correction` \| `fact` \| `instruction` \| `convention` |
| `source` | string | no | `explicit` (user said it), `inferred` (agent noticed), `corrected` (user corrected agent) |
| `confidence` | number | no | 0.0–1.0. Default: explicit=1.0, corrected=0.9, inferred=0.7 |
| `tier` | string | no | `archival` (default) \| `core` (injected into every session) |
| `event_time` | string | no | ISO 8601. When the described event occurred (for temporal contradiction resolution) |
| `last_used` | string | no | ISO 8601. Last time this note was recalled |
| `use_count` | number | no | Times recalled |
| `needs_review` | boolean | no | Flagged by decay job — cleared on next recall |
| `superseded_by` | string | no | ID of a newer note that replaces this one |

**Embedding target:** `content` field only. Not mixed with entity properties.

**Labels:** `[note, <category>, <tier>]` for fast label-filtered queries without embedding search.

**Extraction:** `Do NOT extract Note objects from documents. Notes are created only by agent tool calls.`

### Object Type: `NoteCluster`

A synthesised summary of a group of semantically related Notes. Used for token-efficient context injection.

| Property | Type | Required | Description |
|---|---|---|---|
| `summary` | string | no | LLM-generated description of the cluster theme |
| `confidence` | number | no | Starts at 0.6 (synthesised) — rises as member notes are validated |

### Relationship Types

| Type | Source → Target | Description |
|---|---|---|
| `ANNOTATES` | Note → (any object) | This note is an observation about the target entity |
| `BELONGS_TO_CLUSTER` | Note → NoteCluster | Note is grouped under this cluster |
| `SUPERSEDES` | Note → Note | This note replaces an older one |

`ANNOTATES` targets are untyped — any object in the graph, regardless of schema pack, can be annotated. This is what makes `agent-notes` a universal extension.

---

## MCP Tools

### `save_note`

Create or update a note. If a semantically similar note exists (cosine similarity ≥ 0.70), an LLM merge call decides whether to ADD, UPDATE, DELETE_OLD_ADD_NEW, or NOOP.

```
content      (string, required)   — The observation. Be specific and self-contained.
category     (string, required)   — preference | pattern | correction | fact | instruction | convention
entity_id    (string, optional)   — ID of the graph entity this note annotates.
                                    If omitted, note is free-standing (global preference).
source       (string, optional)   — explicit | inferred | corrected. Default: inferred
event_time   (string, optional)   — ISO 8601 timestamp of when the described event occurred.
                                    Provide for temporal facts ("user moved to London in 2026").
```

### `recall_notes`

Retrieve relevant notes for the current context.

```
query        (string, required)   — What you are looking for.
entity_ids   (string[], optional) — If provided, entity-anchored retrieval runs first:
                                    traverse ANNOTATES edges from these entities, then
                                    supplement with semantic search.
category     (string, optional)   — Filter by category.
limit        (number, optional)   — Default: 10. Max: 50.
```

### `manage_notes`

List, update, delete notes. Also used to promote a note to `tier=core`.

```
action       (string, required)   — list | update | delete | promote_to_core
note_id      (string, optional)   — Required for update, delete, promote_to_core
updates      (object, optional)   — For update: { content, category, confidence, tier }
category     (string, optional)   — For list: filter by category
entity_id    (string, optional)   — For list: filter to notes annotating this entity
limit        (number, optional)   — For list: default 20
```

---

## Session Injection

At the start of each chat session, if the `agent-notes` schema pack is installed in the project:

1. Fetch all `tier=core` notes for the current user (synchronous, label-filtered — no vector computation)
2. Inject as `## Core Notes` block in the system prompt (cap: 10 notes, configurable)
3. LLM sees them without needing to call `recall_notes`

This guarantees that the highest-priority observations (standing instructions, critical corrections) are always present — regardless of whether the LLM decides to call any tool.

---

## Why Not Properties on Existing Types

The alternative — adding `agent_notes: string[]` to existing typed objects — has three problems:

1. **Re-embedding cost**: every note addition re-embeds the entire entity. Entities with many properties produce large embeddings that blend structured and observational signals.

2. **Schema coupling**: requires modifying every schema pack that wants annotation support. `agent-notes` needs no changes to existing schemas — it attaches via relationships.

3. **Retrieval quality**: a vector search for "who communicates better async?" should search *note embeddings*, not entity embeddings. Note embeddings are pure observations. Entity embeddings are structural. Keeping them separate means each retrieval path is high-precision for its purpose.

---

## What This Replaces

- `docs/features/agent-memory-design.md` — the original design. Superseded by this document.
- The `agent-memory` schema pack and `Memory` / `MemoryContext` types — replaced by `agent-notes` with `Note` / `NoteCluster`.
- All references to `save_memory` / `recall_memories` / `manage_memory` MCP tools — replaced by `save_note` / `recall_notes` / `manage_notes`.

The `active-memory-management` openspec change should be updated to target `Note` instead of `Memory` and reflect the entity-anchored retrieval path as a first-class feature.
