# Agent Memory System — Design Document

## Overview

Add cross-session memory to Emergent so AI agents (via MCP) can remember user preferences, learned patterns, coding conventions, and project-specific knowledge across conversations. This builds entirely on top of the existing graph infrastructure — no new database tables, no external services.

## Problem

Today, every MCP session starts from zero. The LLM has no memory of:

- User preferences (code style, framework choices, communication style)
- Learned patterns (project conventions, architecture decisions, naming patterns)
- Corrections ("I told you last week to always use `useApi` not raw `fetch`")
- Project-specific context (deployment procedures, team norms, environment details)

Existing graph objects store **domain knowledge** (entities about the project's subject matter). What's missing is **meta-knowledge** — knowledge about how the user works, what they prefer, and what the agent has learned.

## Design Principle

**Thin layer, maximum reuse.** The graph already provides: versioned objects, JSONB properties, embeddings, hybrid search, FTS, labels, relationships, and full MCP tooling (32 tools). Memory is just a new **template pack** (schema) + **3 thin MCP tools** (convenience wrappers) + **system prompt guidance** (tells the LLM when to save/recall).

---

## 1. Template Pack: `agent-memory`

A new template pack defining memory-specific object types and relationship types.

### Object Types

#### `Memory`

The core memory unit. Each memory is a single fact, preference, pattern, or instruction.

| Property        | Type   | Required | Description                                                                                                                                      |
| --------------- | ------ | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| `content`       | string | yes      | The memory itself, in natural language. E.g., "User prefers single quotes and no semicolons in TypeScript"                                       |
| `category`      | string | yes      | One of: `preference`, `pattern`, `correction`, `fact`, `instruction`, `convention`                                                               |
| `confidence`    | number | no       | 0.0–1.0, how confident the agent is in this memory. Starts at 0.7 for inferred, 1.0 for explicit                                                 |
| `source`        | string | no       | How the memory was created: `explicit` (user said "remember this"), `inferred` (agent noticed a pattern), `corrected` (user corrected the agent) |
| `scope`         | string | no       | `user` (applies everywhere for this user), `project` (applies only in this project), `global` (applies for all users — rare, admin only)         |
| `last_used`     | date   | no       | Last time this memory was recalled and used by an agent                                                                                          |
| `use_count`     | number | no       | How many times this memory has been recalled                                                                                                     |
| `superseded_by` | string | no       | ID of a newer memory that replaces this one (soft deprecation)                                                                                   |

**Labels** are used for fast filtering: the `category` value is duplicated as a label (e.g., `["memory", "preference"]`), and `scope` is also a label.

**Key**: Auto-generated slug from content hash or explicit user-provided name.

**Embedding**: The `content` field is embedded via the standard graph embedding pipeline, enabling semantic search ("find memories about code style" retrieves the single-quotes preference even if the query doesn't say "quotes").

#### `MemoryContext`

Optional grouping for related memories. E.g., "TypeScript Conventions" groups 5 memories about TS preferences.

| Property      | Type   | Required | Description                                          |
| ------------- | ------ | -------- | ---------------------------------------------------- |
| `description` | string | no       | What this context group represents                   |
| `priority`    | number | no       | 0.0–1.0, higher priority contexts are surfaced first |

### Relationship Types

| Type                 | Source → Target        | Description                                      |
| -------------------- | ---------------------- | ------------------------------------------------ |
| `BELONGS_TO_CONTEXT` | Memory → MemoryContext | Groups memories under a context                  |
| `SUPERSEDES`         | Memory → Memory        | Newer memory replaces older one                  |
| `DERIVED_FROM`       | Memory → (any entity)  | Memory was inferred from observing this entity   |
| `RELEVANT_TO`        | Memory → (any entity)  | Memory is relevant when working with this entity |

### Template Pack Definition (JSON)

```json
{
  "name": "agent-memory",
  "version": "1.0.0",
  "description": "Cross-session memory for AI agents. Stores user preferences, learned patterns, corrections, and project-specific knowledge.",
  "author": "Emergent",
  "source": "system",
  "object_type_schemas": {
    "Memory": {
      "name": "Memory",
      "description": "A single unit of agent memory — a fact, preference, pattern, or instruction that persists across sessions.",
      "properties": {
        "content": {
          "type": "string",
          "description": "The memory content in natural language"
        },
        "category": {
          "type": "string",
          "description": "Category: preference, pattern, correction, fact, instruction, convention"
        },
        "confidence": {
          "type": "number",
          "description": "Confidence score 0.0-1.0"
        },
        "source": {
          "type": "string",
          "description": "How created: explicit, inferred, corrected"
        },
        "scope": {
          "type": "string",
          "description": "Scope: user, project, global"
        },
        "last_used": {
          "type": "date",
          "description": "Last time this memory was recalled"
        },
        "use_count": {
          "type": "number",
          "description": "Number of times recalled"
        },
        "superseded_by": {
          "type": "string",
          "description": "ID of newer memory that replaces this one"
        }
      },
      "required": ["content", "category"],
      "extraction_guidelines": "Do NOT extract Memory objects from documents. Memories are only created by agent tool calls."
    },
    "MemoryContext": {
      "name": "MemoryContext",
      "description": "A grouping of related memories under a common theme or topic.",
      "properties": {
        "description": {
          "type": "string",
          "description": "What this context group represents"
        },
        "priority": {
          "type": "number",
          "description": "Priority 0.0-1.0, higher = surfaced first"
        }
      },
      "required": [],
      "extraction_guidelines": "Do NOT extract MemoryContext objects from documents."
    }
  },
  "relationship_type_schemas": {
    "BELONGS_TO_CONTEXT": {
      "name": "BELONGS_TO_CONTEXT",
      "description": "Groups a memory under a context",
      "source_types": ["Memory"],
      "target_types": ["MemoryContext"]
    },
    "SUPERSEDES": {
      "name": "SUPERSEDES",
      "description": "Newer memory replaces an older one",
      "source_types": ["Memory"],
      "target_types": ["Memory"]
    },
    "DERIVED_FROM": {
      "name": "DERIVED_FROM",
      "description": "Memory was inferred from this entity",
      "source_types": ["Memory"],
      "target_types": []
    },
    "RELEVANT_TO": {
      "name": "RELEVANT_TO",
      "description": "Memory is relevant when working with this entity",
      "source_types": ["Memory"],
      "target_types": []
    }
  }
}
```

---

## 2. MCP Tools (3 new tools)

These are **thin convenience wrappers** around existing graph operations. They exist so the LLM doesn't need to know about graph objects, types, labels, or embedding search — it just calls `save_memory` / `recall_memories` / `manage_memory`.

### Tool 1: `save_memory`

**Purpose**: Create or update a memory.

```
Name:        save_memory
Description: Save a memory that persists across sessions. Use this to remember user
             preferences, learned patterns, corrections, conventions, or important facts.
             If a similar memory already exists, it will be updated (superseded) rather
             than duplicated.

InputSchema:
  content     (string, required)  — The memory to save, in clear natural language.
                                    Be specific and self-contained.
  category    (string, required)  — One of: preference, pattern, correction, fact,
                                    instruction, convention
  source      (string, optional)  — How this memory was learned: explicit (user said
                                    "remember this"), inferred (agent noticed pattern),
                                    corrected (user corrected agent). Default: inferred
  scope       (string, optional)  — user (default, applies everywhere for this user),
                                    project (only this project)
  context     (string, optional)  — Name of a memory context group to file this under.
                                    Created automatically if it doesn't exist.
  related_to  (string[], optional) — IDs of entities this memory is relevant to.
```

**Implementation logic** (in `service.go`):

1. Run semantic search on existing Memory objects (type=Memory, query=content, threshold=0.85)
2. If a near-duplicate is found:
   - Create new Memory object with updated content
   - Create SUPERSEDES relationship (new → old)
   - Set `superseded_by` on old memory
   - Return "updated" result with old and new IDs
3. If no duplicate:
   - Create new Memory object with labels `["memory", category, scope]`
   - Set `confidence` based on source (explicit=1.0, corrected=0.9, inferred=0.7)
   - Set `actor_id` to current user (memory is user-scoped via actor_id)
4. If `context` provided:
   - Find or create MemoryContext object with that name
   - Create BELONGS_TO_CONTEXT relationship
5. If `related_to` provided:
   - Create RELEVANT_TO relationships to each entity
6. Return the created memory with its ID

### Tool 2: `recall_memories`

**Purpose**: Search for relevant memories. The agent should call this at the start of complex tasks or when it needs to check for user preferences.

```
Name:        recall_memories
Description: Search for relevant memories from past sessions. Call this when starting
             a new task, when you need to check user preferences, or when the user
             references something you should already know. Returns the most relevant
             memories sorted by relevance.

InputSchema:
  query       (string, required)  — What you're looking for. Be descriptive.
                                    E.g., "user's TypeScript coding preferences"
  category    (string, optional)  — Filter by category: preference, pattern, correction,
                                    fact, instruction, convention
  scope       (string, optional)  — Filter by scope: user, project
  limit       (number, optional)  — Max memories to return (default: 10, max: 50)
```

**Implementation logic**:

1. Build a hybrid search query against graph objects:
   - Filter: `type = 'Memory'`
   - Filter: `actor_id = current_user_id` (user-scoped) OR `scope = 'global'`
   - Filter: `superseded_by IS NULL` (only active memories)
   - If `category` provided: filter by label
   - If `scope` provided: filter by label
   - Search mode: hybrid (FTS + vector) on `content`
2. For each returned memory:
   - Update `last_used` to now
   - Increment `use_count`
3. Return memories sorted by relevance score, with their properties, categories, and related contexts

### Tool 3: `manage_memory`

**Purpose**: Update, delete, or list memories. Less frequently used than save/recall.

```
Name:        manage_memory
Description: Manage existing memories: list all memories, delete outdated ones,
             or update memory properties. Use when the user asks to see, modify,
             or forget specific memories.

InputSchema:
  action      (string, required)  — One of: list, delete, update, forget_all
  memory_id   (string, optional)  — Required for delete and update actions
  updates     (object, optional)  — For update action: { content, category, confidence }
  category    (string, optional)  — For list action: filter by category
  limit       (number, optional)  — For list action: max results (default: 20)
```

**Implementation logic**:

- `list`: Query all Memory objects for current user, optionally filtered by category, sorted by `use_count` desc then `created_at` desc. Return with contexts.
- `delete`: Soft-delete the memory (uses existing graph `Delete`).
- `update`: Create new version of the memory (uses existing graph `Version` mechanism).
- `forget_all`: Soft-delete all Memory objects for current user in current project. Requires confirmation parameter.

---

## 3. System Prompt Guidance

This is the critical piece. The LLM needs clear instructions on **when** and **how** to save and recall memories. This should be injected as a system prompt section by MCP clients (or as an MCP prompt).

### New MCP Prompt: `memory_guidelines`

```
Name: memory_guidelines
Description: Guidelines for using the agent memory system effectively.
             Include this in your system prompt to enable cross-session memory.

Content:

## Memory System

You have access to a persistent memory system that lets you remember information
across sessions. Use it proactively.

### When to RECALL memories (call `recall_memories`)

- At the START of any significant task, recall relevant memories:
  - "user coding preferences" before writing code
  - "project conventions" before architectural decisions
  - "user communication preferences" before explaining something
- When the user says "like I told you", "as usual", "the way I like it",
  "remember when", or similar references to past interactions
- When you're unsure about a style choice, convention, or preference

### When to SAVE memories (call `save_memory`)

Save immediately when the user expresses:
- **Preferences**: "I prefer...", "always use...", "never do...", "I like..."
- **Corrections**: "No, do it this way...", "That's wrong, it should be...",
  "I told you to..."
- **Conventions**: "In this project we always...", "Our team convention is..."
- **Facts**: "Our API runs on...", "The database schema uses...",
  "We deploy via..."
- **Instructions**: "When you write tests, always...", "For PRs, include..."

Also save when you INFER a pattern:
- The user consistently corrects the same thing → save as pattern
- The user's code consistently follows a style → save as convention
- You notice architectural patterns in the codebase → save as fact

### How to write good memories

- Be SPECIFIC and SELF-CONTAINED. Bad: "User likes clean code".
  Good: "User requires all TypeScript functions to have explicit return types,
  no implicit any."
- Include CONTEXT. Bad: "Use prettier". Good: "Project uses Prettier with
  singleQuote: true, semi: false, tabWidth: 2."
- One FACT per memory. Don't bundle multiple preferences into one memory.
- Use PRESENT TENSE. "User prefers..." not "User preferred..."

### Memory categories

- `preference` — User's personal preferences (code style, tools, communication)
- `pattern` — Recurring patterns observed in user's behavior or codebase
- `correction` — Something the user explicitly corrected (high priority to recall)
- `fact` — Objective facts about the project, team, or environment
- `instruction` — Explicit instructions from the user for how the agent should behave
- `convention` — Team or project conventions (naming, architecture, workflow)

### What NOT to save

- Transient task details ("user asked me to fix bug #123")
- Obvious information that doesn't need persistence
- Sensitive credentials, tokens, or passwords (NEVER save these)
- Information already stored as entities in the knowledge graph
```

---

## 4. Architecture Summary

```
┌─────────────────────────────────────────────────┐
│                  LLM Agent                       │
│  (guided by memory_guidelines system prompt)     │
├─────────────────────────────────────────────────┤
│                                                  │
│  save_memory    recall_memories    manage_memory  │  ← 3 new MCP tools
│       │               │                │         │
├───────┼───────────────┼────────────────┼─────────┤
│       ▼               ▼                ▼         │
│  ┌─────────────────────────────────────────┐     │
│  │         MCP Service (service.go)         │     │  Thin handlers:
│  │  executeSaveMemory()                     │     │  - dedup via semantic search
│  │  executeRecallMemories()                 │     │  - user scoping via actor_id
│  │  executeManageMemory()                   │     │  - auto-supersede old memories
│  └──────────────┬──────────────────────────┘     │
│                 │                                 │
│                 ▼                                 │
│  ┌─────────────────────────────────────────┐     │
│  │         graph.Service (existing)         │     │  No changes needed:
│  │  Create() / Search() / Version()         │     │  - JSONB properties
│  │  HybridSearch() / Delete()               │     │  - embeddings
│  │                                          │     │  - FTS
│  └──────────────┬──────────────────────────┘     │  - labels
│                 │                                 │  - versioning
│                 ▼                                 │  - actor_id scoping
│  ┌─────────────────────────────────────────┐     │
│  │     PostgreSQL (kb.graph_objects)        │     │
│  │     pgvector + FTS + JSONB              │     │
│  └─────────────────────────────────────────┘     │
└─────────────────────────────────────────────────┘
```

---

## 5. Scoping and Access Control

| Scope     | Stored as                                    | Visibility                                        |
| --------- | -------------------------------------------- | ------------------------------------------------- |
| `user`    | `actor_id = user_uuid`, label `user`         | Only the creating user, across all their projects |
| `project` | `project_id = project_uuid`, label `project` | Only the creating user, only in this project      |
| `global`  | label `global`, no actor_id filter           | All users in the org (admin-created only)         |

The `recall_memories` tool queries: `(actor_id = me AND scope IN ('user', 'project')) OR scope = 'global'`.

For `project` scope, an additional `project_id = current_project` filter is applied.

---

## 6. Memory Lifecycle

```
Created (confidence: 0.7-1.0)
    │
    ├── Recalled (use_count++, last_used = now)
    │       │
    │       └── Still relevant → stays active
    │
    ├── Corrected by user → new Memory created
    │       │                 (source: corrected, confidence: 0.9)
    │       │
    │       └── Old memory gets superseded_by = new_id
    │           SUPERSEDES relationship created
    │
    ├── Superseded by newer fact → superseded_by set
    │       │
    │       └── Excluded from recall (superseded_by IS NOT NULL)
    │
    └── Explicitly forgotten → soft-deleted
```

### Future: Decay (not in v1)

In a future version, memories that haven't been recalled in N days could have their `confidence` reduced. Very low confidence memories could be flagged for review or auto-archived. This is NOT needed for v1 — keep it simple.

---

## 7. Implementation Plan

### Phase 1: Template Pack (1 day)

1. Create the `agent-memory` template pack JSON
2. Register it as a system template pack (seed data or migration)
3. Ensure extraction pipeline SKIPS Memory/MemoryContext types (via `extraction_guidelines`)

### Phase 2: MCP Tools (2-3 days)

1. Add 3 tool definitions to `GetToolDefinitions()` in `service.go`
2. Add 3 execute methods: `executeSaveMemory`, `executeRecallMemories`, `executeManageMemory`
3. Add tool names to `ExecuteTool` switch statement
4. Implement dedup logic in `executeSaveMemory` (semantic search → supersede)
5. Implement user-scoped recall with hybrid search
6. Add `memory_guidelines` to `GetPromptDefinitions()`

### Phase 3: Testing (1 day)

1. Unit tests for tool handlers
2. E2E test: save → recall → update → delete lifecycle
3. E2E test: dedup (save similar memory twice → superseded)
4. E2E test: user scoping (user A can't see user B's memories)

### Phase 4: System Prompt Integration (0.5 day)

1. Add `memory_guidelines` as a default MCP prompt
2. Document how MCP clients (OpenCode, Claude Desktop) should include the prompt
3. Optionally: auto-inject memory guidelines when memory template pack is installed

### Total: ~5 days of implementation

---

## 8. What This Design Does NOT Include (Intentionally)

- **No new database tables** — uses existing `kb.graph_objects` and `kb.graph_relationships`
- **No automatic extraction** — memories are only created by explicit agent tool calls
- **No memory decay** — v1 keeps all memories until explicitly superseded or deleted
- **No cross-user memory sharing** — each user's memories are private (except `global` scope)
- **No memory consolidation** — no periodic job to merge/summarize related memories
- **No separate embedding model** — uses the same embedding pipeline as all graph objects

These can all be added incrementally later without schema changes.

---

## 9. Comparison with Mem0

| Aspect         | Mem0                          | Emergent Memory (this design)                   |
| -------------- | ----------------------------- | ----------------------------------------------- |
| Storage        | Separate vector DB + graph DB | Existing graph (pgvector + Postgres)            |
| Search         | Vector-only                   | Hybrid (vector + FTS + graph traversal)         |
| Dedup          | Basic hash-based              | Semantic similarity (embedding distance)        |
| Relationships  | Standalone memories           | Full graph relationships to project entities    |
| Schema         | Flat key-value                | Typed, validated, with categories               |
| Context        | No project context            | Deep integration with project knowledge graph   |
| User scoping   | API key based                 | Native actor_id + project_id                    |
| Infrastructure | Additional service to deploy  | Zero new infrastructure                         |
| MCP tools      | 9 generic tools               | 3 purpose-built tools + 32 existing graph tools |
