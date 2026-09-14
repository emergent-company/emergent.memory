---
name: memory-onboard
description: First-time setup only — connect a codebase to Memory, design a schema from scratch, and configure the project. Use once per project, not for ongoing graph population.
metadata:
  author: emergent
  version: "2.0"
---

Onboard the current project into Memory by understanding what it is, selecting or creating a Memory project, designing a matching knowledge graph schema, installing it, and guiding the user through populating the graph.

## Rules

- **Project context is auto-discovered** — the CLI walks up the directory tree to find `.env.local` containing `MEMORY_PROJECT` or `MEMORY_PROJECT_ID`. If `.env.local` is present anywhere above the current directory, `--project` is not needed. Only pass `--project <id>` explicitly when overriding or when no `.env.local` exists.
- **Use only `memory` CLI commands** throughout this workflow. Never use `curl`, raw HTTP requests, or direct API calls — the CLI handles authentication and project context automatically.

---

## What is Memory?

**Memory** is a knowledge graph platform. It stores information about a project as structured **objects** (typed nodes) connected by **relationships** (typed edges). Agents and users can query the graph in natural language, and the graph is automatically populated by extracting knowledge from documents.

Key concepts:
- **Project** — the top-level container. One project per codebase/product/domain.
- **Schema** — defines the *types* of objects and relationships that exist in a project. Must be designed before objects can be created.
- **Object** — a typed node in the graph (e.g. a `Service`, `Requirement`, `Person`).
- **Relationship** — a typed directed edge between two objects (e.g. `Service` -> `depends_on` -> `Service`).
- **Document** — raw text ingested into the project; objects are extracted from documents automatically.

---

## Workflow

### Step 1 — Understand the project

> **IMPORTANT: All file exploration must be anchored to the current working directory (CWD).** Do NOT navigate to or read files from other directories (e.g., `/root/emergent.memory` or any path that isn't the user's project CWD). The project being onboarded is whatever is in the CWD.

First, confirm the CWD and list its contents:
```bash
pwd
ls -la
```

Then explore the repository/codebase to answer:
- What does this project do? (product, library, service, data pipeline, etc.)
- What are the main *things* it deals with? (entities, components, people, concepts)
- What are the important *relationships* between those things?
- What questions would a developer/user want to ask about this project?

Read `README.md`, `AGENTS.md`, `package.json`, `go.mod`, or any top-level documentation **in the CWD only**. Do **not** ask the user generic questions — form a hypothesis first, then confirm it.

Example questions to confirm with the user:
> "This looks like a Go microservice for X. I'm thinking the key entities are: Service, Endpoint, Migration, and Dependency. Does that sound right? Anything to add or change?"

### Step 2 — Choose or create a Memory project

Before designing anything, establish which Memory project this repository will use.

#### 2a. Check if already configured

Check `.env.local` for existing server and project configuration:

```bash
cat .env.local 2>/dev/null | grep -E "MEMORY_(SERVER_URL|PROJECT)"
```

- **If `MEMORY_SERVER_URL` is set:** the CLI already knows which server to talk to. Do NOT run `memory init` or `memory login` — the agent context handles authentication automatically.
- **If `MEMORY_PROJECT=<id>` (or `MEMORY_PROJECT_ID=<id>`) is set:** show the user the project ID and name (`memory projects get <id>` if available, otherwise just the ID), then ask:
  > "This repo is already connected to Memory project `<name>` (`<id>`). Continue with this project, or switch to a different one?"
  - If they confirm: proceed to Step 3.
  - If they want to switch: continue with Step 2b below.
- **If neither project var is found:** continue with Step 2b.

> **Important:** Never run `memory init` or `memory login` from within an agent — these are interactive commands designed for human CLI sessions. The agent context provides authentication automatically. If `.env.local` has `MEMORY_SERVER_URL` and `MEMORY_PROJECT`, everything needed is already configured.

#### 2b. List existing projects

```bash
memory projects list
```

- **If projects are listed:** present them to the user and ask which one to use for this repo, or whether they want to create a new project.
- **If no projects are returned:** skip straight to creating a new one (Step 2c).

#### 2c. Create a new project (if needed)

Suggest a project name derived from the repository directory name or the project's product name:

```bash
memory projects create --name "<suggested-name>"
```

Note the returned project ID.

#### 2d. Write project ID to .env.local

If `MEMORY_PROJECT` or `MEMORY_PROJECT_ID` already exists in `.env.local`, the project is already configured — skip writing and confirm with the user.

Otherwise, write (or update) `MEMORY_PROJECT` in `.env.local`:

```bash
# If .env.local does not exist:
echo "MEMORY_PROJECT=<project-id>" > .env.local

# If .env.local exists but has no MEMORY_PROJECT line:
echo "MEMORY_PROJECT=<project-id>" >> .env.local

# If .env.local already has MEMORY_PROJECT (switching projects):
# Replace the existing line (use sed or rewrite the file)
```

Confirm with the user:
> "Set `MEMORY_PROJECT=<project-id>` in `.env.local`. All subsequent `memory` CLI commands in this directory will now use this project."

Also remind the user to add `.env.local` to `.gitignore` if it is not already there (it may contain project tokens or other credentials).

### Step 2.5 — Configure LLM provider credentials

Memory needs a live LLM provider to extract knowledge from documents and answer queries. Credentials are configured at the **organization level**. During onboarding, check whether credentials are set and, if not, configure them now — document extraction won't work without them.

#### Check if credentials are configured

```bash
memory provider list
```

**If a provider is listed:** run a live test to confirm it works:

```bash
memory provider test <provider>
# provider is one of: google-ai, vertex-ai
```

- If the test **passes**: proceed to Step 3.
- If the test **fails**: tell the user which provider is configured and that the credentials appear to be invalid, then offer two options:
  1. Re-configure them now (follow the "no credentials" path below)
  2. Skip for now (extraction won't work until credentials are fixed)

**If no providers are listed:** configure one now. The recommended path is **Google AI** (simplest — just an API key). Vertex AI is the alternative for GCP-native environments.

**Recommended path — Google AI:**

Ask the user:
> "No LLM provider is configured yet. To enable document extraction, I need a Google AI API key. You can get one at https://aistudio.google.com/app/apikey — do you have one handy?"

Once they provide the key:

```bash
memory provider configure google-ai --api-key <key>
```

This stores the credentials, syncs the model catalog, auto-selects models, and runs a live test — all in one step. If it succeeds, you will see the chosen generative and embedding models in the output. Proceed to Step 3.

If it **fails**: show the error output, ask the user to double-check the API key, and retry. If it still fails after a second attempt, let the user know extraction will be unavailable until credentials are fixed, and continue with Step 3 anyway.

**Alternative path — Vertex AI** (for GCP environments):

If the user prefers Vertex AI, ask for:
- A service account JSON key file path (or paste the JSON inline)
- GCP project ID
- Region (e.g. `us-central1`)

```bash
memory provider configure vertex-ai \
  --key-file <path-to-service-account.json> \
  --gcp-project <gcp-project-id> \
  --location <region>
```

This also syncs the model catalog and auto-selects models atomically. Proceed to Step 3 on success.

### Step 2.6 — Set the project info document

The **project info** is a Markdown description that agents and MCP clients read (via the `get_project_info` tool) to orient themselves before working with the project. It should capture the project's purpose, goals, audience, and high-level context. Setting it now ensures every subsequent agent interaction — extraction, querying, schema design — has the right context.

Based on what you learned in Step 1, compose a concise Markdown description and upload it directly to the server:

```bash
memory projects set-info --text "# My Project

A Go microservice that manages user authentication and authorization for the
Acme platform. It exposes REST and gRPC APIs consumed by frontend apps and
other backend services.

**Audience:** Backend engineers working on the Acme platform.

**Key capabilities:**
- OAuth2 / OIDC login flows
- Role-based access control (RBAC)
- API key management
- Audit logging"
```

The document should answer:
- What does this project do?
- Who is the intended audience (developers, end-users, data analysts, etc.)?
- What are the main goals or capabilities?
- What domain or industry does it serve?

> **Note:** `set-info` uploads the content directly to the server — there is no local copy and no sync mechanism. Do not save a local `.memory/project-info.md` file. To update the info later, call `set-info` again with new content.

Confirm with the user:
> "Set the project info document. Agents and MCP tools will now use this to understand the project's context."

### Step 3 — Design the schema

Based on your understanding from Step 1, design a schema JSON file and save it to:
```
.memory/templates/<pack-name>/pack.json
```

Create the `.memory/templates/<pack-name>/` directory if it doesn't exist.

**Pack naming convention:** use lowercase-with-hyphens, matching the project domain.  
Examples: `go-microservice`, `react-app`, `data-pipeline`, `research-papers`

**Schema JSON structure:**

```json
{
  "name": "<pack-name>",
  "version": "1.0.0",
  "description": "Knowledge graph schema for <project description>",
  "author": "<inferred from git config or package.json>",
  "object_type_schemas": [
    {
      "name": "TypeName",
      "description": "What this type represents",
      "extraction_guidelines": "When to extract this type and what to look for",
      "properties": {
        "name":        { "type": "string", "description": "Primary identifier" },
        "description": { "type": "string", "description": "What it does" }
      }
    }
  ],
  "relationship_type_schemas": [
    {
      "name": "relationship_name",
      "label": "Human Readable Label",
      "description": "What this relationship means",
      "fromTypes": ["SourceType"],
      "toTypes": ["TargetType"]
    }
  ],
  "ui_configs": {
    "TypeName": { "icon": "Box", "color": "#3B82F6", "category": "Core" }
  }
}
```

**Design guidelines:**
- Start with 3-8 object types. More than 10 is usually too many for a first pass.
- Every type needs at minimum: `name` (string) and `description` (string) in `properties`.
- Both `object_type_schemas` and `relationship_type_schemas` are **arrays**, not maps — each entry has a `"name"` field.
- Relationship names should be snake_case verbs: `depends_on`, `implements`, `owned_by`.
- Use `fromTypes`/`toTypes` arrays (multiple source/target types are allowed).
- `extraction_guidelines` tells the AI extractor what to look for in documents — be specific.
- `ui_configs` icon names come from Lucide icons (e.g. `Box`, `Layers`, `User`, `FileText`, `GitBranch`, `Database`, `Globe`, `Tag`, `Shield`, `Zap`).

**Present the pack design to the user** and confirm before proceeding:
> "Here's the schema I designed. Object types: Service, Endpoint, Migration. Relationships: Service -> depends_on -> Service, Endpoint -> defined_in -> Service. Does this look right?"

### Step 4 — Install the schema

Once the user confirms the design, create and install in one step:

```bash
memory schemas install --file .memory/templates/<pack-name>/pack.json
```

This creates the schema from the JSON file and installs it into the project in a single operation.

To preview what would be installed without making changes:
```bash
memory schemas install --file .memory/templates/<pack-name>/pack.json --dry-run
```

Verify the types are available:
```bash
memory schemas compiled-types
```

### Step 5 — Populate the graph

The recommended approach is to ingest documents and let Emergent extract objects automatically using the `extraction_guidelines` in the schema.

#### Upload documents

```bash
memory documents upload AGENTS.md --auto-extract
memory documents upload README.md --auto-extract
# Upload any other relevant files (architecture docs, specs, etc.)
```

The `--auto-extract` flag triggers chunking, embedding, and automatic object extraction after upload.

#### Query the result

```bash
memory query "what are the main components and how do they relate?"
```

---

#### Creating objects and relationships manually

> **Use the `memory-graph` skill for all graph writes.** It covers batch creation, ID capture, updates, lookups, and idempotent upserts with full worked examples. The summary below is a quick reference — load `memory-graph` for the complete workflow.

> **Always batch. Never loop.** When creating more than one object or relationship, use `create-batch` — a single API call for any number of items. Never call `memory graph objects create` or `memory graph relationships create` in a loop or sequence. Each individual call is a separate round-trip; batching 20 objects takes the same time as batching 1.

##### Batch-create objects (preferred)

Write all objects to a JSON file, then create them in one call:

```bash
# Write objects.json
cat > /tmp/objects.json << 'EOF'
[
  {"type": "Service", "name": "auth-service", "description": "Handles authentication and JWT validation"},
  {"type": "Service", "name": "api-gateway", "description": "Routes requests to downstream services"},
  {"type": "Database", "name": "PostgreSQL", "description": "Primary relational store"},
  {"type": "ExternalDependency", "name": "stripe", "description": "Payment processing API"}
]
EOF

# Create all objects in one call — output is one line per object: <id>  <type>  <name>
MEMORY_PROJECT=$MP memory graph objects create-batch --file /tmp/objects.json
```

Capture the IDs from the output immediately — you need them for relationships:

```bash
# Parse IDs into shell variables for use in the relationships batch
AUTH_ID=$(MEMORY_PROJECT=$MP memory graph objects create-batch --file /tmp/objects.json | awk '/auth-service/ {print $1}')
# Or capture the full output and parse with grep/awk
```

**Practical pattern:** write the batch file, run `create-batch`, capture stdout, parse IDs with `awk` or `grep`, then write the relationships file.

##### Batch-create relationships (preferred)

Once you have the object IDs, write all relationships to a JSON file and create them in one call:

```bash
cat > /tmp/relationships.json << 'EOF'
[
  {"type": "depends_on", "from": "<auth-service-id>", "to": "<postgres-id>"},
  {"type": "depends_on", "from": "<api-gateway-id>", "to": "<auth-service-id>"},
  {"type": "uses_dependency", "from": "<auth-service-id>", "to": "<stripe-id>"}
]
EOF

MEMORY_PROJECT=$MP memory graph relationships create-batch --file /tmp/relationships.json
```

##### Single-object creation (fallback only)

Use the single-create commands **only** when adding one isolated object after the initial population:

```bash
# Single object — only when truly adding just one:
MEMORY_PROJECT=$MP memory graph objects create \
  --type Service --name "auth-service" --description "Handles authentication"

# With a stable key for idempotent re-runs (skip if already exists):
MEMORY_PROJECT=$MP memory graph objects create \
  --type Service --key "svc-auth" --name "auth-service"

# With --upsert: create-or-update semantics:
MEMORY_PROJECT=$MP memory graph objects create \
  --type Service --key "svc-auth" --name "auth-service" --upsert

# Single relationship — only when adding one:
MEMORY_PROJECT=$MP memory graph relationships create \
  --type depends_on --from <source-id> --to <target-id>
```

> **Important:** Always use `memory` CLI commands — never construct raw `curl` API calls. The CLI handles authentication and project context automatically.

---

## After Onboarding

Remind the user:
- `.env.local` contains `MEMORY_PROJECT=<id>` — keep this out of git (add to `.gitignore`)
- The schema definition is saved at `.memory/templates/<pack-name>/pack.json` — commit this to the repo
- `.memory/journal.md` and `.memory/graph-state.md` are agent session journals — add `.memory/journal.md` and `.memory/graph-state.md` to `.gitignore` (or gitignore the whole `.memory/` dir except `templates/`)
- To modify the schema, edit the JSON and run `memory schemas install --file pack.json --merge` to additively merge changes
- To update the project info: call `memory projects set-info --text "..."` or `--file <path>` — content is uploaded to the server, no local copy is kept
- The `memory-query` skill can be used to explore the populated graph
- The `memory-schemas` skill has full reference for managing schemas

---

## Notes

- If `.memory/templates/` already exists with a pack, confirm with the user whether to update or keep it
- Only `.memory/templates/` should be committed — `.memory/journal.md`, `.memory/graph-state.md`, and `.env.local` are gitignored
- Schema IDs are UUIDs; use `memory schemas installed` to find them after installation
