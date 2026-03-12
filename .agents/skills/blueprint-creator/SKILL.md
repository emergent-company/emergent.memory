---
name: blueprint-creator
description: Guide the user through creating a Blueprints directory — the declarative config format applied with `memory blueprints <source>`. Use when the user wants to create, scaffold, or understand the blueprint file format for template packs, agent definitions, and seed data.
metadata:
  author: emergent
  version: "1.0"
---

Guide the user through creating a new Emergent Blueprint from scratch — a directory of YAML/JSON files and JSONL seed data that can be applied to any Memory project with `memory blueprints <source>`.

> **Already have a project graph to export?** Use `memory blueprints dump <output-dir>` to generate seed files from an existing project instead of writing them by hand, then layer in your `packs/` and `agents/` directories.

## Rules

- **Never run `memory browse`** — it launches a full interactive TUI that blocks on terminal input and will hang in an automated agent context.
- **Always prefix `memory` commands with `NO_PROMPT=1`** (e.g. `NO_PROMPT=1 memory <cmd>`). Without it, the CLI may show interactive pickers when no project, agent, MCP server, skill, or agent-definition ID is provided. Do not add this to `.env.local` — it must only apply to agent-driven invocations.
- **Always supply a project** with `--project <id>` on project-scoped commands, or ensure `MEMORY_PROJECT` is set.

---

## What is a Blueprint?

A **blueprint** is a portable, reusable directory that declaratively defines:

| Subdirectory | Contents | Required? |
|---|---|---|
| `packs/` | Template pack YAML files (object + relationship type schemas) | At least one of the three |
| `agents/` | Agent definition YAML files | Optional |
| `seed/` | Initial graph objects and relationships as JSONL | Optional |

Applied with:
```bash
memory blueprints <source>               # local path or GitHub URL
memory blueprints <source> --dry-run     # preview without mutating
memory blueprints <source> --upgrade     # update existing resources
```

---

## Workflow

### Step 1 — Understand the domain

Before writing any files, clarify what the blueprint is for:

- **What domain/product does this blueprint serve?** (e.g. AI news tracking, multi-agent tasks, legal corpus, media database)
- **What are the key entities?** List nouns: things the user wants to store and query.
- **What are the key relationships?** List verbs: how entities connect to each other.
- **Will agents be included?** If so, what do they do? What tools do they need?
- **Is there seed data?** Pre-defined objects (e.g. search queries, pool definitions, model records) that should be created on first apply.

Explore any existing repository if one is present (`README.md`, `AGENTS.md`, source files). Form a hypothesis and confirm with the user before designing:

> "I'm thinking the key types are: `Movie`, `Person`, `Genre` with relationships `ACTED_IN`, `DIRECTED`, `IN_GENRE`. Does that match what you have in mind?"

---

### Step 2 — Design the template pack

Create `packs/<pack-name>.yaml`. Naming: lowercase-with-hyphens.

#### Pack YAML structure

```yaml
name: my-pack                   # must match the filename (without .yaml)
version: "1.0.0"
description: >
  Short description of what this pack covers.
author: Your Name / Org
license: MIT

objectTypes:

  - name: TypeName               # PascalCase
    label: Human Label           # spaces allowed
    description: >
      What this type represents. Include when to create it.
    properties:
      title:
        type: string
        description: Primary title or name
      summary:
        type: string
        description: 2-3 sentence summary
      url:
        type: string
        description: Source URL
      discovered_date:
        type: string
        description: Date this item was discovered (YYYY-MM-DD)

relationshipTypes:

  - name: relates_to             # snake_case verb
    label: Relates To
    description: >
      Connects two objects that reference the same concept.

  - name: implements             # optionally constrain source/target
    label: Implements
    description: >
      Links an implementation to the artifact it implements.
    sourceType: ToolRelease      # optional — omit to allow any type
    targetType: ResearchPaper    # optional — omit to allow any type
```

**Design guidelines:**
- Start with **4–10 object types**. Fewer is better for a first version.
- Every object type needs at least `title`/`name` and `description`/`summary` properties.
- Use `type: string` for almost everything. Use `type: integer` / `type: number` for numeric KPIs or counts.
- Relationship names are snake_case verbs: `relates_to`, `implements`, `funded_by`, `supersedes`.
- Only add `sourceType`/`targetType` when the relationship is meaningfully constrained.
- Pack `name` must be globally unique within the Memory instance.

#### Real-world examples to study

| Blueprint | Pack highlights |
|---|---|
| `ai-news-memory-blueprint` | 8 types, 6 relationships — rich property sets with `discovered_date` on every type |
| `workspace-memory-blueprint-v3` | 14 types, 19 relationships — two-layer design (domain + system meta-layer) |
| `imdb-memory-blueprint` | 6 types, 11 relationships — constrained `sourceType`/`targetType` on every relationship |
| `norwegian-law-memory-blueprint` | 7 types, 13 relationships — external dataset-driven |

---

### Step 3 — Design agent definitions (optional)

If the blueprint includes agents, create one YAML file per agent in `agents/`.

#### Agent YAML structure

```yaml
name: my-agent-name              # slug, used with `memory agents create --definition`
description: >
  What this agent does, what it produces, and when it should run.

model:
  name: gemini-2.5-flash         # model identifier
  temperature: 0.3               # 0.0–1.0; lower = more deterministic
  maxTokens: 8192

flowType: agentic                # agentic | retrieval | chat
maxSteps: 20                     # max tool-call iterations per run
isDefault: false
defaultTimeout: 300              # seconds
visibility: project              # project | org | public

tools:
  - brave_web_search             # MCP or built-in tools the agent may call
  - query_entities
  - create_entity
  - create_relationship

config:                          # arbitrary metadata stored on the definition
  category: research
  objectType: MyTypeName
  scheduledNote: "Intended daily schedule: 06:00 UTC"

systemPrompt: |
  You are a [role]. Your job is to [goal].

  ## Key behaviour rules
  - Rule 1
  - Rule 2

  ## Output format
  [describe what the agent should produce]
```

**Design guidelines:**
- `name` must be unique within the project; use hyphens, no spaces.
- `flowType: agentic` is correct for agents that call tools in a loop.
- Set `maxSteps` generously (20–50) for research agents that loop over many searches.
- Always instruct agents to **deduplicate** before creating objects (call `query_entities` first).
- Keep `temperature` low (0.2–0.4) for agents that write structured data; higher (0.6–0.8) for synthesis/digest agents.
- The `config` block is free-form — use it to store scheduling notes, category tags, or any metadata useful for the README.

#### Tool reference (common built-in tools)

| Tool | Use case |
|---|---|
| `query_entities` | Search/list graph objects before creating (deduplication) |
| `create_entity` | Write new objects to the graph |
| `create_relationship` | Write edges between objects |
| `brave_web_search` | Web research (requires Brave MCP server) |
| `spawn_agents` | Spawn child agents (orchestrator pattern) |

#### Agent types

| `config.type` | Purpose |
|---|---|
| `leaf` | Executes a single task; writes results to graph |
| `pool_manager` | Owns a pool of leaf agents; quality gate |
| `orchestrator` | Decomposes work packages; manages child agents |
| `janitor` | Periodic system analysis; proposes improvements |

---

### Step 4 — Design seed data (optional)

Seed data pre-populates the graph on first apply. Create JSONL files under `seed/objects/` and `seed/relationships/`.

#### Object seed file (`seed/objects/<TypeName>.jsonl`)

One JSON object per line. **Always add a `key`** to prevent duplicates on re-apply:

```jsonl
{"type":"SearchQuery","key":"rp-query-arxiv-today","labels":["search-query"],"properties":{"category":"research-papers","query":"arxiv AI machine learning papers [TODAY]"}}
{"type":"SearchQuery","key":"rp-query-llm-safety","labels":["search-query"],"properties":{"category":"research-papers","query":"LLM safety alignment paper [TODAY]"}}
```

Key naming convention: `<type-prefix>-<slug>` (all lowercase, hyphens).

#### Relationship seed file (`seed/relationships/<RelName>.jsonl`)

Prefer `srcKey`/`dstKey` over IDs — IDs are project-specific and break on re-apply:

```jsonl
{"type":"member_of_pool","srcKey":"python-coder","dstKey":"pool-coding"}
{"type":"managed_by","srcKey":"pool-coding","dstKey":"coding-manager"}
{"type":"uses_model","srcKey":"python-coder","dstKey":"model-gpt-4o"}
```

#### What to seed

| Data | Seed it when... |
|---|---|
| Search queries / topics | Agent loops over them at runtime |
| Pool objects | Multi-agent system with named pools |
| AgentDefinitionRecord objects | System that tracks agent metadata in the graph |
| Model objects | System that registers available LLMs in the graph |
| KPI objects | System that tracks performance metrics |
| Lookup tables / taxonomies | Static reference data (genres, categories, legal areas) |

**Do not seed** objects that agents are expected to discover and create dynamically.

---

### Step 5 — Write the README

Every blueprint directory must have a `README.md`. Include:

1. **What it does** — 2–3 sentence summary
2. **What it installs** — tables listing types, agents, seed objects
3. **Prerequisites** — required MCP servers, LLM providers, external accounts
4. **Installation** — `memory blueprints` command(s)
5. **Post-install setup** — schedule configuration, first run instructions
6. **Directory layout** — tree showing all files
7. **Customising** — how to extend (add types, change models, add agents)
8. **Rollback** — how to remove (delete agents, uninstall pack)

---

### Step 6 — Validate and apply

```bash
# Dry-run against a real project (catches type errors, missing keys, bad YAML)
memory blueprints ./my-blueprint --project <project-id> --dry-run

# Apply
memory blueprints ./my-blueprint --project <project-id>

# Re-apply after edits (skip existing, only add new)
memory blueprints ./my-blueprint --project <project-id>

# Update existing resources
memory blueprints ./my-blueprint --project <project-id> --upgrade
```

Fix any errors reported by `--dry-run` before proceeding to a live apply.

---

## Complete Directory Layout

```
my-blueprint/
  README.md                          <- required
  packs/
    <pack-name>.yaml                 <- one file per template pack
  agents/
    <agent-name>.yaml                <- one file per agent definition
  seed/
    objects/
      <TypeName>.jsonl               <- one JSON object per line
    relationships/
      <RelName>.jsonl                <- one JSON object per line
  tools/                             <- optional: Go/Python seeders for large datasets
    seeder/
      main.go
  skills/                            <- optional: agent workflow skills
    <skill-name>/
      SKILL.md
```

---

## Blueprint Patterns

### Pattern 1 — Pack-only (schema distribution)

Just `packs/`. No agents, no seed data. Use when you want to share a reusable schema without opinionated agents or data.

```
my-blueprint/
  packs/
    my-schema.yaml
  README.md
```

### Pattern 2 — Pack + agents (automated intelligence)

Add agents that populate the graph automatically. Common for news trackers, research pipelines, and monitoring systems. Seed the search queries or topic lists that agents loop over at runtime.

```
my-blueprint/
  packs/
    my-schema.yaml
  agents/
    researcher.yaml
    digest.yaml
  seed/
    objects/
      SearchQuery.jsonl    <- queries agents will iterate over
  README.md
```

Reference: `ai-news-memory-blueprint`

### Pattern 3 — Pack + agents + seed (full system)

Full multi-agent orchestration system where the graph is the control plane. Seed pools, model records, KPIs, and agent definition records so agents can query system state at runtime.

```
my-blueprint/
  packs/
    multi-agent-pack.yaml
  agents/
    orchestrator.yaml
    coding-manager.yaml
    python-coder.yaml
    reviewer.yaml
    janitor.yaml
  seed/
    objects/
      AgentPool.jsonl
      AgentDefinitionRecord.jsonl
      Model.jsonl
      KPI.jsonl
    relationships/
      member_of_pool.jsonl
      managed_by.jsonl
      uses_model.jsonl
      kpi_for_pool.jsonl
  README.md
```

Reference: `workspace-memory-blueprint-v3`

### Pattern 4 — Pack + custom seeder (large datasets)

When seed data is too large for JSONL files (millions of records), write a standalone Go/Python seeder using the Memory SDK. Keep `packs/` for the schema and `cmd/seeder/` for ingestion.

```
my-blueprint/
  packs/
    my-schema.yaml
  cmd/
    seeder/
      main.go
  README.md
```

Reference: `imdb-memory-blueprint`, `norwegian-law-memory-blueprint`

---

## Checklist Before Publishing

- [ ] Every object type has at least `title`/`name` and `description`/`summary` properties
- [ ] Every seed object has a `key` (unless intentionally keyless / always-insert)
- [ ] Relationship seed files use `srcKey`/`dstKey`, not `srcId`/`dstId`
- [ ] Pack `name` is unique and matches the filename (without `.yaml`)
- [ ] Agent `name` values are unique within the blueprint
- [ ] `--dry-run` passes against a real project with no errors
- [ ] `README.md` documents prerequisites, installation, and customisation
- [ ] Git repository is clean and committed before pushing to GitHub

---

## Notes

- Pack names must be **globally unique** within the Memory instance. Prefix with a project or org name to avoid collisions (e.g. `acme-task-pack` not just `task-pack`).
- Agents reference tools by name. If a tool requires an MCP server (e.g. `brave_web_search`), document this as a prerequisite in the README.
- `memory blueprints dump` exports only graph objects and relationships — **not** pack definitions or agent definitions. You must author `packs/` and `agents/` manually.
- Use `--upgrade` when iterating on agent prompts or pack properties after initial apply; without it, existing resources are silently skipped.
- Blueprints can be applied from GitHub: `memory blueprints https://github.com/org/repo#v1.0.0` — tag releases for reproducibility.
- The `skills/` subdirectory is a convention used in `workspace-memory-blueprint-v3`; skills are not applied by `memory blueprints` but can be loaded by agents at runtime.
