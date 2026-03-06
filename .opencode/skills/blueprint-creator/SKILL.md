---
name: blueprint-creator
description: Guide the user through creating a Blueprints directory — the declarative config format applied with `memory blueprints <source>`. Use when the user wants to create, scaffold, or understand the blueprint file format for template packs and agent definitions.
metadata:
  author: emergent
  version: "1.0"
  trigger: create blueprint, write a blueprint, scaffold blueprints, blueprint format, how do I use blueprints
---

A blueprint is a directory of YAML or JSON files that describes template packs and agent definitions for an Emergent project. Running `memory blueprints <source>` applies the directory to a live project — creating or updating resources via the Emergent API. This skill walks you through creating a blueprint from scratch.

**Input**: Optional scope — a description of what the blueprint should contain (e.g. "a CRM pack and a sales agent"), or nothing to scaffold a minimal example.

---

## What is a Blueprint?

A blueprint is a plain directory with this structure:

```
my-blueprint/
  packs/        ← one file per template pack
  agents/       ← one file per agent definition
```

Both `packs/` and `agents/` are optional. Files may be `.yaml`, `.yml`, or `.json`. Files with any other extension are silently skipped. Subdirectories inside `packs/` or `agents/` are also ignored — keep files flat.

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

If the user hasn't specified, scaffold a minimal working example (one pack, one agent) and explain each field as you go.

### 2. Create the directory structure

```bash
mkdir -p my-blueprint/packs
mkdir -p my-blueprint/agents
```

Use `workdir` so paths stay relative to where the user wants the blueprint to live.

### 3. Write pack files

**Location**: `packs/<name>.yaml` (or `.json`)

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

**Minimal valid example** (`packs/research.yaml`):

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

### 5. Validate the blueprint before applying

Run a dry run to preview what will happen without making any API calls:

```bash
memory blueprints ./my-blueprint --dry-run
```

Expected output:
```
[dry-run] would create pack "research-pack"
[dry-run] would create agent "research-assistant"
Dry run complete: 2 would be created, 0 would be updated, 0 would be skipped
```

If there are validation errors (missing `name`, missing `version`, empty `objectTypes`), they appear here. Fix them before proceeding.

### 6. Apply the blueprint

**Apply to the default project** (from `EMERGENT_PROJECT_ID` or config):
```bash
memory blueprints ./my-blueprint
```

**Apply to a specific project**:
```bash
memory blueprints ./my-blueprint --project <project-id>
```

**Apply and update existing resources** (additive + update):
```bash
memory blueprints ./my-blueprint --upgrade
```

Expected output:
```
created pack "research-pack"
created agent "research-assistant"
Blueprints complete: 2 created, 0 updated, 0 skipped, 0 errors
```

Exit code is non-zero if any resource produced an error.

### 7. (Optional) Publish to GitHub

To share the blueprint as a reusable repo, push the directory to GitHub. Others can then apply it directly:

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

Pin to a specific version using a URL fragment:
```bash
memory blueprints https://github.com/org/my-blueprint#v1.0.0
```

The fragment can be a branch name, tag, or full commit SHA.

---

## Reference: Validation Rules

| Resource | Field | Rule |
|---|---|---|
| Pack | `name` | Must be non-empty string |
| Pack | `version` | Must be non-empty string |
| Pack | `objectTypes` | Must contain at least one entry |
| Agent | `name` | Must be non-empty string |
| All | file extension | Must be `.json`, `.yaml`, or `.yml` |

Files that fail validation are reported as errors but do not stop processing of other files. The run exits non-zero if any errors occurred.

---

## Reference: CLI Flags

| Flag | Description |
|---|---|
| `--project <id>` | Target project ID or name. Overrides config/env. |
| `--upgrade` | Update resources that already exist (by `name`). Default: skip. |
| `--dry-run` | Preview only — no API calls, no mutations. |
| `--token <tok>` | GitHub PAT for private repos. Falls back to `MEMORY_GITHUB_TOKEN`. |

---

## Reference: Matching Logic

Resources are matched by `name` field — not by filename. If a pack named `"research-pack"` already exists in the project:
- Without `--upgrade`: it is **skipped** and a hint is printed.
- With `--upgrade`: it is **updated** in place.

Renaming a resource in the file (changing the `name` field) creates a **new** resource and leaves the old one untouched. This is intentional — `name` is the stable identity.

---

## Guardrails

- **Never guess field names** — only use the field names documented here; the API will silently ignore unknown fields but the intent will be lost
- **Never put multiple resources in one file** — each file must describe exactly one pack or one agent
- **Never nest subdirectories** inside `packs/` or `agents/` — files inside nested dirs are silently ignored
- **Always dry-run first** in production environments — `--dry-run` is free and catches validation errors before any mutations occur
- **Never hardcode tokens** in blueprint files — use `MEMORY_GITHUB_TOKEN` or `--token` at apply time; blueprint files should be safe to commit to git
