---
name: blueprint-creator
description: Guide the user through creating a Blueprints directory — the declarative config format applied with `memory blueprints <source>`. Use when the user wants to create, scaffold, or understand the blueprint file format for template packs, agent definitions, and seed data.
metadata:
  author: emergent
  version: "2.0"
  trigger: create blueprint, write a blueprint, scaffold blueprints, blueprint format, how do I use blueprints, seed data blueprint, blueprint dump
---

A blueprint is a directory of YAML/JSON files and JSONL seed data that describes template packs, agent definitions, and initial graph objects/relationships for an Emergent project. Running `memory blueprints <source>` applies the directory to a live project — creating or updating resources via the Emergent API. This skill walks you through creating a blueprint from scratch.

**Input**: Optional scope — a description of what the blueprint should contain (e.g. "a CRM pack, a sales agent, and 50 seed contacts"), or nothing to scaffold a minimal example.

---

## What is a Blueprint?

A blueprint is a plain directory with this structure:

```
my-blueprint/
  schemas/                  ← one file per template pack (.yaml/.yml/.json) [preferred]
  packs/                    ← backward-compatible alias for schemas/
  agents/                   ← one file per agent definition (.yaml/.yml/.json)
  seed/
    objects/                ← per-type JSONL files with graph objects to create
    relationships/          ← per-type JSONL files with graph relationships to create
```

All subdirectories are optional — you only need to include what you have. Use `schemas/` for new blueprints; `packs/` is still supported for backward compatibility. Files with unsupported extensions are silently skipped. Subdirectories inside `schemas/` or `agents/` are also ignored — keep files flat.

Blueprints can be applied from:
- A **local path**: `memory blueprints ./my-blueprint`
- A **GitHub repo**: `memory blueprints https://github.com/org/repo`
- A **GitHub repo at a specific ref**: `memory blueprints https://github.com/org/repo#v1.2.0`

---

## Steps

### 1. Understand what the user wants to create

Ask or infer:
- What **template packs** are needed? (e.g. "CRM pack", "Research pack")
  - For each pack: what object types? (e.g. `Contact`, `Deal`, `Company`)
  - Any relationship types? (e.g. `Contact → works_at → Company`)
- What **agent definitions** are needed?
  - For each agent: name, purpose, model, tools, system prompt
- Is there **seed data**?
  - Pre-defined objects to load (e.g. a list of known companies, canonical tags)
  - Relationships between those objects (e.g. which person works at which company)

If the user hasn't specified, scaffold a minimal working example (one pack, one agent) and explain each field as you go.

### 2. Create the directory structure

```bash
mkdir -p my-blueprint/schemas
mkdir -p my-blueprint/agents
mkdir -p my-blueprint/seed/objects
mkdir -p my-blueprint/seed/relationships
```

Only create `seed/` subdirectories if there is seed data to write — the apply command treats a missing `seed/` directory as "no seed data" (not an error). Use `schemas/` (preferred) for the pack directory; `packs/` is accepted for backward compatibility.

### 3. Write pack files

**Location**: `schemas/<name>.yaml` (or `.json`) — also accepted from `packs/`

**Required fields** — the CLI will reject the file if any of these are missing:
- `name` — unique identifier for the pack (string)
- `version` — semantic version string (e.g. `"1.0.0"`)
- `objectTypes` — at least one entry

**Full pack schema**:

```yaml
name: my-pack                   # required — unique pack name
version: "1.0.0"                # required — semantic version
description: Optional summary   # optional
author: Your Name               # optional
license: MIT                    # optional
repositoryUrl: https://...      # optional
documentationUrl: https://...   # optional

objectTypes:                    # required — at least one entry
  - name: Person                # object type identifier
    label: Person               # human-readable label (optional)
    description: A human individual  # optional
    properties: {}              # optional — arbitrary shape, passed through to API

  - name: Company
    label: Company
    description: An organisation

relationshipTypes:              # optional
  - name: works_at
    label: Works At
    description: Person works at a Company
    sourceType: Person          # optional — source object type name
    targetType: Company         # optional — target object type name

uiConfigs: {}                   # optional — arbitrary shape, passed through to API
extractionPrompts: {}           # optional — arbitrary shape, passed through to API
```

**Minimal valid example** (`schemas/research.yaml`):

```yaml
name: research-pack
version: "1.0.0"
description: Objects for tracking research sources and findings
objectTypes:
  - name: Source
    label: Source
    description: A research source (paper, article, book)
  - name: Finding
    label: Finding
    description: A key insight or result from a source
relationshipTypes:
  - name: supports
    label: Supports
    sourceType: Source
    targetType: Finding
```

### 4. Write agent files

**Location**: `agents/<name>.yaml` (or `.json`)

**Required fields** — only `name` is validated:
- `name` — unique identifier for the agent

**Full agent schema**:

```yaml
name: my-agent                  # required — unique agent name
description: What this agent does  # optional
systemPrompt: |                 # optional — the agent's system instructions
  You are a helpful assistant.

model:                          # optional
  name: gpt-4o                  # model identifier
  temperature: 0.7              # optional — 0.0–2.0
  maxTokens: 2048               # optional — integer

tools:                          # optional — list of tool names
  - search
  - graph_query

flowType: conversational        # optional
isDefault: false                # optional — bool, defaults to false
maxSteps: 10                    # optional — integer
defaultTimeout: 30              # optional — integer (seconds)
visibility: workspace           # optional

config:                         # optional — arbitrary key-value map
  someKey: someValue

workspaceConfig:                # optional — arbitrary key-value map
  anotherKey: anotherValue
```

**Minimal valid example** (`agents/assistant.yaml`):

```yaml
name: research-assistant
description: Answers questions about research sources and findings
systemPrompt: |
  You are a research assistant. Use available tools to find and
  synthesise information from the knowledge graph.
model:
  name: gpt-4o
  temperature: 0.5
tools:
  - search
  - graph_query
isDefault: true
```

### 5. Write seed data files (optional)

Seed data pre-populates the graph with objects and relationships. The apply command creates new objects and — with `--upgrade` — updates existing ones.

#### Seed object files

**Location**: `seed/objects/<TypeName>.jsonl`

One JSON object per line. Each line must have a `type` field. All other fields are optional.

**Object record schema**:

```jsonc
{
  "type": "Person",        // required — must match an installed object type
  "key": "alice-smith",    // optional — stable identity key; enables upsert on re-apply
  "status": "active",      // optional
  "labels": ["vip"],       // optional — array of string labels
  "properties": {          // optional — arbitrary key-value map
    "name": "Alice Smith",
    "email": "alice@example.com",
    "role": "Engineer"
  }
}
```

**Key field behaviour**:
- Objects **with** a `key`: idempotent — on re-apply, without `--upgrade` they are skipped if the key exists; with `--upgrade` they are upserted.
- Objects **without** a `key`: always created (a new object is inserted on every apply).

**Example** (`seed/objects/Person.jsonl`):
```jsonl
{"type":"Person","key":"alice-smith","properties":{"name":"Alice Smith","role":"Engineer"}}
{"type":"Person","key":"bob-jones","properties":{"name":"Bob Jones","role":"Manager"}}
```

**Split files**: if a type file exceeds 50 MB, name subsequent parts `<TypeName>.001.jsonl`, `<TypeName>.002.jsonl`, etc. The loader reads all matching files in order.

#### Seed relationship files

**Location**: `seed/relationships/<TypeName>.jsonl`

One JSON object per line. Each line must have a `type` field and endpoint references. Prefer key-based references when both objects have keys.

**Relationship record schema** (key-based — preferred):

```jsonc
{
  "type": "works_at",      // required — must match an installed relationship type
  "srcKey": "alice-smith", // source object key (use when source has a key)
  "dstKey": "acme-corp",   // destination object key (use when destination has a key)
  "weight": 1.0,           // optional — float
  "properties": {}         // optional — arbitrary key-value map
}
```

**Relationship record schema** (ID-based — fallback for keyless objects):

```jsonc
{
  "type": "works_at",
  "srcId": "eid-abc123",   // source entity ID (from a prior dump or object creation)
  "dstId": "eid-def456",   // destination entity ID
  "weight": 1.0,
  "properties": {}
}
```

**Validation rules**:
- `type` must be non-empty.
- Either (`srcKey` + `dstKey`) OR (`srcId` + `dstId`) must be provided — not a mix.
- Relationships with unresolvable keys are recorded as errors and skipped; apply continues.
- Duplicate relationships are silently ignored by the server (idempotent).

**Example** (`seed/relationships/works_at.jsonl`):
```jsonl
{"type":"works_at","srcKey":"alice-smith","dstKey":"acme-corp"}
{"type":"works_at","srcKey":"bob-jones","dstKey":"acme-corp"}
```

### 6. Validate and apply

Run a dry run to preview all actions without any API calls:

```bash
memory blueprints ./my-blueprint --dry-run
```

Expected output:
```
[dry-run] would create pack "research-pack"
[dry-run] would create agent "research-assistant"
[dry-run] would create object Person "alice-smith"
[dry-run] would create object Person "bob-jones"
[dry-run] would create relationship works_at alice-smith → acme-corp
Dry run complete: 2 packs/agents would be created; 2 objects, 1 relationship
```

**Apply to the default project**:
```bash
memory blueprints ./my-blueprint
```

**Apply to a specific project**:
```bash
memory blueprints ./my-blueprint --project <project-id>
```

**Apply and update existing resources**:
```bash
memory blueprints ./my-blueprint --upgrade
```

With `--upgrade`:
- Packs and agents that already exist are updated (not skipped).
- Seed objects with a `key` that already exists are upserted (content-hash no-op if unchanged).
- Keyless objects are always created regardless of `--upgrade`.

Expected output:
```
created pack "research-pack"
created agent "research-assistant"
  seed: 2 objects created, 0 updated, 0 skipped, 0 failed; 1 relationships created, 0 failed
Blueprints complete: 2 created, 0 updated, 0 skipped, 0 errors
```

Exit code is non-zero if any resource produced an error.

### 7. Export an existing project as seed data (dump)

To export a live project's graph as seed files that can be re-applied elsewhere:

```bash
memory blueprints dump ./exported
```

This creates:
```
exported/
  seed/
    objects/<TypeName>.jsonl
    relationships/<TypeName>.jsonl
```

**Export only specific types**:
```bash
memory blueprints dump ./exported --types Person,Company,works_at
```

**Export targeting a specific project**:
```bash
memory blueprints dump ./exported --project <project-id>
```

The dump command:
- Paginates through all objects and relationships (page size 250).
- Groups output by type — one file per type.
- Automatically splits files at 50 MB (producing `<Type>.001.jsonl`, `<Type>.002.jsonl`, …).
- Prefers `key`-based references in relationship files when both endpoints have keys; falls back to raw entity IDs when keys are unavailable.
- Prints progress: `objects: N fetched…` and a final summary line.

The resulting seed files are directly re-applyable:
```bash
memory blueprints dump ./exported
memory blueprints ./exported --project <other-project-id>
```

### 8. (Optional) Publish to GitHub

Push the directory to GitHub to share as a reusable blueprint repo:

```bash
memory blueprints https://github.com/org/my-blueprint
```

For a **private repo**, provide a GitHub personal access token:
```bash
memory blueprints https://github.com/org/my-blueprint --token ghp_...
# or
export MEMORY_GITHUB_TOKEN=ghp_...
memory blueprints https://github.com/org/my-blueprint
```

Pin to a specific version using a URL fragment (branch, tag, or commit SHA):
```bash
memory blueprints https://github.com/org/my-blueprint#v1.0.0
```

---

## Reference: Directory Layout

```
my-blueprint/
  schemas/                        # preferred (packs/ also supported for backward compat)
    <pack-name>.yaml          # one file per template pack
  agents/
    <agent-name>.yaml         # one file per agent definition
  seed/
    objects/
      <TypeName>.jsonl        # one file per object type
      <TypeName>.001.jsonl    # split files for types > 50 MB
    relationships/
      <TypeName>.jsonl        # one file per relationship type
```

---

## Reference: Validation Rules

| Resource | Field | Rule |
|---|---|---|
| Pack | `name` | Must be non-empty string |
| Pack | `version` | Must be non-empty string |
| Pack | `objectTypes` | Must contain at least one entry |
| Agent | `name` | Must be non-empty string |
| Seed object | `type` | Must be non-empty string |
| Seed relationship | `type` | Must be non-empty string |
| Seed relationship | endpoints | Either (`srcKey`+`dstKey`) or (`srcId`+`dstId`) required |
| All | file extension | `schemas/`(`packs/`)+`agents/`: `.json`, `.yaml`, `.yml`; `seed/`: `.jsonl` only |

Files that fail validation are reported as warnings but do not stop processing of other files. The run exits non-zero if any errors occurred.

---

## Reference: CLI Flags

### `memory blueprints <source>` (apply)

| Flag | Description |
|---|---|
| `--project <id>` | Target project ID or name. Overrides config/env. |
| `--upgrade` | Update resources that already exist (by `name` for packs/agents; by `key` for seed objects). Default: skip. |
| `--dry-run` | Preview only — no API calls, no mutations. |
| `--token <tok>` | GitHub PAT for private repos. Falls back to `MEMORY_GITHUB_TOKEN`. |

### `memory blueprints dump <output-dir>` (export)

| Flag | Description |
|---|---|
| `--project <id>` | Source project ID or name. Overrides config/env. |
| `--types <list>` | Comma-separated list of object/relationship types to export. Default: all types. |

---

## Reference: Matching Logic

**Packs and agents** are matched by `name` field — not by filename. Renaming the `name` field in a file creates a new resource and leaves the old one untouched.

**Seed objects** are matched by `key` field when present:
- Without `--upgrade`: objects whose `key` already exists are **skipped**.
- With `--upgrade`: objects whose `key` already exists are **upserted** (content-hash no-op if unchanged).
- Keyless objects: **always created** on every apply.

**Seed relationships** are always idempotent — the server ignores duplicates.

---

## Guardrails

- **Never guess field names** — only use the field names documented here; unknown fields are silently ignored
- **Never put multiple resources in one file** — each `schemas/` (or `packs/`) or `agents/` file must describe exactly one resource
- **Never nest subdirectories** inside `schemas/` (or `packs/`) or `agents/` — nested files are silently ignored
- **Seed files must be `.jsonl`** — one JSON object per line; files with other extensions in `seed/` are skipped
- **Always dry-run first** in production environments — `--dry-run` is free and catches validation errors before any mutations occur
- **Never hardcode tokens** in blueprint files — use `MEMORY_GITHUB_TOKEN` or `--token` at apply time
- **Prefer `key` on seed objects** — keyless objects are always re-created on every apply, which leads to duplicates
- **Use `srcKey`/`dstKey` in relationships** whenever both endpoints have keys — ID-based refs break when re-applying to a different project
