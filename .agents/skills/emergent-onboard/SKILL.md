---
name: emergent-onboard
description: Onboard a project into Emergent — understand what the project is, choose or create an Emergent project, design and install a template pack, then guide on creating objects and relationships. Use when setting up Emergent for a new project or codebase for the first time.
metadata:
  author: emergent
  version: "1.2"
---

Onboard the current project into Emergent by understanding what it is, selecting or creating an Emergent project, designing a matching knowledge graph schema (template pack), installing it, and guiding the user through populating the graph.

> **Rule:** Use only `emergent` CLI commands throughout this workflow. Never use `curl`, raw HTTP requests, or direct API calls — the CLI handles authentication and project context automatically.

---

## What is Emergent?

**Emergent** is a knowledge graph platform. It stores information about a project as structured **objects** (typed nodes) connected by **relationships** (typed edges). Agents and users can query the graph in natural language, and the graph is automatically populated by extracting knowledge from documents.

Key concepts:
- **Project** — the top-level container. One project per codebase/product/domain.
- **Template pack** — defines the *types* of objects and relationships that exist in a project. Must be designed before objects can be created.
- **Object** — a typed node in the graph (e.g. a `Service`, `Requirement`, `Person`).
- **Relationship** — a typed directed edge between two objects (e.g. `Service` → `depends_on` → `Service`).
- **Document** — raw text ingested into the project; objects are extracted from documents automatically.

---

## Workflow

### Step 1 — Understand the project

Explore the repository/codebase to answer:
- What does this project do? (product, library, service, data pipeline, etc.)
- What are the main *things* it deals with? (entities, components, people, concepts)
- What are the important *relationships* between those things?
- What questions would a developer/user want to ask about this project?

Use file reading, directory listings, README files, and existing documentation. Do **not** ask the user generic questions — form a hypothesis first, then confirm it.

Example questions to confirm with the user:
> "This looks like a Go microservice for X. I'm thinking the key entities are: Service, Endpoint, Migration, and Dependency. Does that sound right? Anything to add or change?"

### Step 2 — Choose or create an Emergent project

Before designing anything, establish which Emergent project this repository will use.

#### 2a. Check if already configured

Check whether `.env.local` already contains `EMERGENT_PROJECT_ID`:

```bash
cat .env.local 2>/dev/null | grep EMERGENT_PROJECT
```

- **If `EMERGENT_PROJECT_ID=<id>` is found:** show the user the project ID and name (`emergent projects get <id>` if available, otherwise just the ID), then ask:
  > "This repo is already connected to Emergent project `<name>` (`<id>`). Continue with this project, or switch to a different one?"
  - If they confirm: proceed to Step 3.
  - If they want to switch: continue with Step 2b below.

- **If not found:** continue with Step 2b.

#### 2b. List existing projects

```bash
emergent projects list
```

- **If projects are listed:** present them to the user and ask which one to use for this repo, or whether they want to create a new project.
- **If no projects are returned:** skip straight to creating a new one (Step 2c).

#### 2c. Create a new project (if needed)

Suggest a project name derived from the repository directory name or the project's product name:

```bash
emergent projects create --name "<suggested-name>"
```

Note the returned project ID.

#### 2d. Write project ID to .env.local

Write (or update) `EMERGENT_PROJECT_ID` in `.env.local`:

```bash
# If .env.local does not exist:
echo "EMERGENT_PROJECT_ID=<project-id>" > .env.local

# If .env.local exists but has no EMERGENT_PROJECT_ID line:
echo "EMERGENT_PROJECT_ID=<project-id>" >> .env.local

# If .env.local already has EMERGENT_PROJECT_ID (switching projects):
# Replace the existing line (use sed or rewrite the file)
```

Confirm with the user:
> "Set `EMERGENT_PROJECT_ID=<project-id>` in `.env.local`. All subsequent `emergent` CLI commands in this directory will now use this project automatically — no flags needed."

Also remind the user to add `.env.local` to `.gitignore` if it is not already there (it may contain project tokens or other credentials).

### Step 2.5 — Verify LLM provider credentials

Emergent needs a live LLM provider to extract knowledge from documents and answer queries. Credentials are configured at the **organization level** (not per-project), so this check applies to the whole org — not just this project.

#### Check configured credentials

```bash
emergent provider list-credentials
```

- **If credentials are listed:** run a live test to confirm they work end-to-end:

  ```bash
  emergent provider test <provider>
  # provider is one of: google-ai, vertex-ai, vertex-ai-express
  ```

  Expected output:
  ```
  Testing vertex-ai-express... OK (1386ms)
    Model:  gemini-2.5-flash-lite
    Reply:  Hello!
  ```

  If the test passes, proceed to Step 3.

  If the test **fails**, the credentials are invalid — guide the user through re-saving them (see below).

- **If no credentials are listed:** no LLM provider has been configured for this organization yet. Ask the user which provider they want to use:

  > "No LLM provider credentials are configured for this organization. Emergent supports three providers — which do you have credentials for?"
  > - **Vertex AI Express** — just an API key (starts with `AQ.`), no GCP project needed
  > - **Google AI** — Gemini API key (starts with `AIza`)
  > - **Vertex AI** — GCP service account JSON file

  **Vertex AI Express (API key, recommended if available):**
  ```bash
  emergent provider set-vertex-express "AQ.Ab8RN6..."
  ```

  **Google AI (API key):**
  ```bash
  emergent provider set-key "AIza..."
  ```

  **Vertex AI (service account):**
  ```bash
  emergent provider set-vertex \
    --project <gcp-project-id> \
    --location us-central1 \
    --credentials-file /path/to/service-account.json
  ```

  After saving, verify the credentials work:
  ```bash
  emergent provider test <provider>
  ```

  If the test fails, show the error to the user and help them correct the credentials before continuing.

### Step 3 — Design the template pack

Based on your understanding from Step 1, design a template pack JSON file and save it to:
```
.memory/templates/<pack-name>/pack.json
```

Create the `.memory/templates/<pack-name>/` directory if it doesn't exist.

**Pack naming convention:** use lowercase-with-hyphens, matching the project domain.  
Examples: `go-microservice`, `react-app`, `data-pipeline`, `research-papers`

**Template pack JSON structure:**

```json
{
  "name": "<pack-name>",
  "version": "1.0.0",
  "description": "Knowledge graph schema for <project description>",
  "author": "<inferred from git config or package.json>",
  "object_type_schemas": {
    "TypeName": {
      "type": "object",
      "description": "What this type represents",
      "required": ["name"],
      "properties": {
        "name":        { "type": "string", "description": "Primary identifier" },
        "description": { "type": "string", "description": "What it does" }
      },
      "extraction_guidelines": "When to extract this type and what to look for"
    }
  },
  "relationship_type_schemas": {
    "relationship_name": {
      "label": "Human Readable Label",
      "description": "What this relationship means",
      "fromTypes": ["SourceType"],
      "toTypes": ["TargetType"]
    }
  },
  "ui_configs": {
    "TypeName": { "icon": "Box", "color": "#3B82F6", "category": "Core" }
  }
}
```

**Design guidelines:**
- Start with 3–8 object types. More than 10 is usually too many for a first pass.
- Every type needs at minimum: `name` (string, required) and `description` (string).
- Relationship names should be snake_case verbs: `depends_on`, `implements`, `owned_by`.
- Use `fromTypes`/`toTypes` arrays (multiple source/target types are allowed).
- `extraction_guidelines` tells the AI extractor what to look for in documents — be specific.
- `ui_configs` icon names come from Lucide icons (e.g. `Box`, `Layers`, `User`, `FileText`, `GitBranch`, `Database`, `Globe`, `Tag`, `Shield`, `Zap`).

**Present the pack design to the user** and confirm before proceeding:
> "Here's the schema I designed. Object types: Service, Endpoint, Migration. Relationships: Service → depends_on → Service, Endpoint → defined_in → Service. Does this look right?"

### Step 4 — Install the template pack

Once the user confirms the design:

```bash
emergent template-packs install --file .memory/templates/<pack-name>/pack.json
```

This creates the pack in the registry and installs it into the current project in one step.
The output includes the Assignment ID and Pack ID. Save the pack ID for future reference:

```bash
echo "<pack-id>" > .memory/templates/<pack-name>/pack-id.txt
```

Verify the types are available:
```bash
emergent template-packs compiled-types
```

### Step 5 — Populate the graph

The recommended approach is to ingest documents and let Emergent extract objects automatically using the `extraction_guidelines` in the pack.

#### Upload documents

```bash
emergent documents upload AGENTS.md
emergent documents upload README.md
# Upload any other relevant files (architecture docs, specs, etc.)
```

> **Note:** Upload each file only once — the server deduplicates by content hash; uploading the same file again returns the existing document. Extraction is triggered separately after upload.

#### Query the result

```bash
emergent query "what are the main components and how do they relate?"
```

The `emergent query` command reads `EMERGENT_PROJECT_ID` and `EMERGENT_PROJECT_TOKEN` from `.env.local` automatically — no flags are needed when run from the project workspace directory.

> **Important:** Use `--mode search` if the default agent mode fails (default `--mode agent` requires a pre-existing agent definition). Prefer `--mode search` for freshly created projects:
> ```bash
> emergent query --mode search "what are the main components and how do they relate?"
> ```

#### Create objects manually (optional)

For small numbers of objects, use single-create:

```bash
emergent graph objects create --type Service --name "auth-service" --description "Handles authentication"
```

For many objects at once, use **batch create** (much more efficient):

1. Write a JSON file (e.g. `.memory/objects.json`):
   ```json
   [
     {"type": "Service", "name": "auth-service", "description": "Handles authentication"},
     {"type": "Service", "name": "api-gateway", "description": "Routes requests"},
     {"type": "Database", "name": "users-db", "description": "Stores user accounts"}
   ]
   ```

2. Run the batch create command:
   ```bash
   emergent graph objects create-batch --file .memory/objects.json
   ```

   Output is one line per created object: `<entity-id>  <type>  <name>`

   **Save the entity IDs** — you will need them to create relationships.

#### Create relationships manually (optional)

For small numbers of relationships, use single-create:

```bash
emergent graph relationships create --type depends_on --from <source-entity-id> --to <target-entity-id>
```

For many relationships at once, use **batch create**:

1. Write a JSON file (e.g. `.memory/relationships.json`) using the entity IDs from object creation:
   ```json
   [
     {"type": "depends_on", "from": "<api-gateway-entity-id>", "to": "<auth-service-entity-id>"},
     {"type": "stores_data_in", "from": "<auth-service-entity-id>", "to": "<users-db-entity-id>"}
   ]
   ```

2. Run the batch create command:
   ```bash
   emergent graph relationships create-batch --file .memory/relationships.json
   ```

> **Important:** Always use `emergent` CLI commands — never construct raw `curl` API calls. The CLI handles authentication and project context automatically.

---

## After Onboarding

Remind the user:
- `.env.local` contains `EMERGENT_PROJECT_ID=<id>` — keep this out of git (add to `.gitignore`)
- The template pack definition is saved at `.memory/templates/<pack-name>/pack.json` — commit this to the repo
- To modify the schema, edit the JSON and create a new pack version (packs are immutable once created)
- The `emergent-query` skill can be used to explore the populated graph
- The `emergent-template-packs` skill has full reference for managing packs

---

## Notes

- If `.memory/templates/` already exists with a pack, confirm with the user whether to update or keep it
- Keep `.memory/` committed to the repo — it documents the project's knowledge graph schema
- Pack IDs are UUIDs; always save them in `.memory/templates/<pack-name>/pack-id.txt` after creation
