## Context

The Memory agentic system lets agents execute multi-step workflows via LLM + tools. Agent instructions are currently fully baked into `AgentDefinition.SystemPrompt` at definition time — there is no mechanism to load reusable, updateable workflow playbooks on demand. OpenCode introduced a skill system that solves this, but its approach (list all skills in the tool description) only works at small scale (<50 skills). Memory is a platform where skill libraries will grow to hundreds or thousands of entries, so we need semantic retrieval to surface only relevant skills.

Current state:
- `AgentDefinition.Tools []string` already acts as an opt-in whitelist for tools
- `executor.go:runPipeline()` assembles tools in a composable pattern (pool → coordination → workspace → ask_user)
- `*embeddings.Service` (768-dim, gemini-embedding-001) is available via fx throughout the server
- pgvector with IVFFlat indexes is already in use for semantic search over chunks, graph objects, and relationships
- 8 SKILL.md files already exist in `.agents/skills/` following the OpenCode frontmatter format

## Goals / Non-Goals

**Goals:**
- DB-managed skill library with global (org-wide) and project-scoped visibility
- Semantic retrieval: at agent startup, embed the agent task and retrieve the top-K most relevant skills to surface in the `skill` tool description
- Opt-in per agent via `"skill"` in `AgentDefinition.Tools` — zero impact on existing agents
- Full REST API for CRUD on skills
- SDK + CLI with `import` command that parses existing SKILL.md frontmatter
- Seed path: existing `.agents/skills/` SKILL.md files importable via `memory skills import`

**Non-Goals:**
- UI (no frontend changes)
- Filesystem scanning at runtime (DB-only, no walk-up directory discovery)
- Per-call re-ranking (retrieval happens once at agent startup per run, not per LLM turn)
- Versioning or history of skill content
- Skill execution sandboxing

## Decisions

### Decision 1: Semantic retrieval at run start, not per turn

**Choice:** Embed the agent's initial task/trigger message once at `runPipeline()` start, retrieve top-K relevant skills, and bake them into the `skill` tool description for the entire run.

**Alternatives considered:**
- *OpenCode approach (list all):* Works for <50 skills, infeasible at thousands — a skill list of 1000 skills at ~200 tokens each = 200k tokens injected into every tool description.
- *Re-rank on every LLM turn:* Would allow adapting to mid-run context drift but adds an embedding call + DB round-trip to every turn, significantly increasing latency and cost.
- *Tag/category pre-filter:* Simpler and deterministic, but requires maintaining a manual taxonomy. Semantic handles novel skills without taxonomy updates.

**Rationale:** A single embedding call per run (not per turn) is negligible overhead. The agent's initial task message is the strongest signal for what skills will be needed. If a skill isn't retrieved, the agent can still call `skill({name: "..."})` by exact name if it knows it.

### Decision 2: Top-K = 10, threshold = 50

**Choice:** If the total accessible skill count ≤ 50, list all (OpenCode-style). If > 50, use semantic retrieval and surface top-10.

**Rationale:** Below 50 skills, listing all is safe and avoids the overhead of an embedding call for small deployments. Above 50, semantic retrieval kicks in. K=10 gives the LLM a reasonable menu without overloading the description field. Both values are constants that can be tuned.

### Decision 3: Embedding stored on `kb.skills`, generated at create/update time

**Choice:** Store `description_embedding vector(768)` on `kb.skills`. Generate it via `embeddingsSvc.EmbedQuery(ctx, description)` at create and update time in the handler (not lazily). Accept that skills without embeddings (e.g. created when embedding service is disabled) are excluded from semantic results but still accessible by exact name.

**Alternatives considered:**
- *Embed at retrieval time (query-time lazy):* Would require storing unembedded skills separately and computing on first use — more complex with no benefit.
- *Separate embedding job queue:* Overkill for skills (unlike chunks/graph objects, skills are low-volume, rarely created).

**Rationale:** Skills are created infrequently. Generating the embedding synchronously at write time is simple, consistent with how the rest of the codebase handles it for low-volume entities, and means retrieval is always fast.

### Decision 4: Scope model — NULL project_id = global

**Choice:** `project_id IS NULL` means global (visible to all agents across all projects). A non-null `project_id` means project-scoped. When resolving skills for an agent, the store returns global skills merged with project-scoped skills, with project-scoped winning on name collision.

**Rationale:** Matches the pattern used by `AgentDefinition` and `TemplatePack` scoping. NULL-for-global avoids a separate "global project" sentinel value and is easy to query with a `WHERE project_id = ? OR project_id IS NULL` pattern.

### Decision 5: Retrieval embeds the trigger message, fallback to agent name

**Choice:** At `runPipeline()` start, use `run.TriggerMessage` (the user's initial message or trigger payload) as the query text for skill retrieval. If empty, fall back to the agent's name + description.

**Rationale:** The trigger message is the best available signal for what the agent is about to do. The agent name/description is a reasonable fallback for scheduled or programmatic triggers that have no user message.

### Decision 6: `skill` tool always available to opted-in agents, no per-skill permission rules

**Choice:** If `"skill"` is in `AgentDefinition.Tools`, the agent gets the skill tool with all accessible skills (global + project). No per-skill ACL beyond the global/project scope.

**Rationale:** Per-skill ACLs (like OpenCode's `permission.skill.*` rules) add complexity without clear need in our platform model. Project scoping already provides the primary isolation boundary. This can be added later if needed.

## Risks / Trade-offs

- **Embedding service down at skill create time** → skill is stored without embedding, excluded from semantic retrieval, still callable by exact name. Mitigation: log a warning; consider a background re-embed job if this becomes common.
- **Cold start — no trigger message** → fallback to agent name may retrieve less-relevant skills. Mitigation: documented fallback behavior; agents can still call `skill({name})` directly.
- **IVFFlat recall at low skill counts** → IVFFlat recall degrades below ~1000 rows. Mitigation: the ≤50 threshold bypasses vector search entirely for small libraries; for medium libraries (50–1000), probes=10 provides adequate recall.
- **Description quality determines retrieval quality** → poor descriptions = poor retrieval. Mitigation: `import` command encourages well-structured SKILL.md files with meaningful `description` frontmatter.
- **Context window budget** → K=10 skills at ~200 tokens each = ~2000 tokens added to the tool description per run. Acceptable. Can reduce K if needed.

## Migration Plan

1. Run migration `00052_create_skills.sql` — creates `kb.skills` with IVFFlat index
2. Deploy server (no breaking changes to existing API)
3. Optionally seed: `for f in .agents/skills/*/SKILL.md; do memory skills import "$f"; done`
4. Opt agents into the skill tool by adding `"skill"` to their `AgentDefinition.Tools` array

Rollback: drop the `kb.skills` table; remove the `skills` domain module from `main.go`; no other tables or APIs are affected.

## Open Questions

- Should `memory skills import` also accept a directory (import all SKILL.md files under a path) for easier bulk seeding?
- Should there be a `memory skills embed` command to re-generate embeddings for skills created while the embedding service was disabled?
