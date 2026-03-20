---
name: memory-graph
description: Create, update, or delete graph objects and relationships imperatively — including batch inserts from parsed source files. Use for any direct write to the knowledge graph after the project is set up.
metadata:
  author: emergent
  version: "1.0"
---

Write to (and look up from) the Memory knowledge graph — creating, updating, and deleting objects and relationships.

## Rules

- **Never run `memory browse`** — it launches a full interactive TUI that blocks on terminal input and will hang in an automated agent context.
- **Always prefix `memory` commands with `NO_PROMPT=1`** (e.g. `NO_PROMPT=1 memory <cmd>`). Without it, the CLI may show interactive pickers. Do not add this to `.env.local` — it must only apply to agent-driven invocations.
- **Always supply a project** with `--project <id>` on project-scoped commands, or ensure `MEMORY_PROJECT` is set.
- **Use only `memory` CLI commands** — never `curl`, raw HTTP requests, or direct API calls.

---

## When to use this skill vs others

| Skill | Use for |
|---|---|
| **memory-graph** (this) | Writing to the graph — creating, updating, deleting objects and relationships |
| **memory-query** | Reading from the graph — natural language questions, search |
| **memory-onboard** | First-time setup — project creation, schema design, initial population |
| **memory-blueprints** | Declarative bulk seeding from a directory of JSONL files |
| **memory-schemas** | Managing object and relationship type definitions |

---

## Core principle: always batch

> **When creating more than one object or relationship, always use `create-batch`. Never call single-create in a loop.**

Each individual `memory graph objects create` call is a separate API round-trip. A `create-batch` call with 50 objects takes the same time as one with 1. There is no size limit that makes looping preferable.

**Wrong:**
```bash
memory graph objects create --type Service --name "auth"
memory graph objects create --type Service --name "gateway"
memory graph objects create --type Service --name "worker"
# 3 round-trips, 3× the latency
```

**Right:**
```bash
memory graph objects create-batch --file /tmp/objects.json
# 1 round-trip, regardless of count
```

---

## Workflow: adding objects and relationships

### Step 1 — Check available types

Before inserting, confirm the types you need exist in the schema:

```bash
NO_PROMPT=1 MEMORY_PROJECT=$MP memory schemas compiled-types
```

If a type is missing, use the `memory-schemas` skill to install it first.

### Step 2 — Write the objects batch file

```bash
cat > /tmp/objects.json << 'EOF'
[
  {"type": "Service",            "name": "auth-service",  "description": "Handles JWT validation and session management"},
  {"type": "Service",            "name": "api-gateway",   "description": "Routes requests to downstream services, enforces rate limits"},
  {"type": "Service",            "name": "worker",        "description": "Processes background jobs from the River queue"},
  {"type": "Database",           "name": "PostgreSQL",    "description": "Primary relational store for all application data"},
  {"type": "ExternalDependency", "name": "stripe",        "description": "Payment processing API used by the billing service"}
]
EOF
```

### Step 3 — Create objects and capture IDs

```bash
OUTPUT=$(NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects create-batch --file /tmp/objects.json)
echo "$OUTPUT"
```

Output format is one line per object: `<entity-id>  <type>  <name>`

Parse IDs immediately from the output:

```bash
AUTH_ID=$(echo "$OUTPUT"    | awk '/auth-service/ {print $1}')
GATEWAY_ID=$(echo "$OUTPUT" | awk '/api-gateway/  {print $1}')
WORKER_ID=$(echo "$OUTPUT"  | awk '/worker/       {print $1}')
DB_ID=$(echo "$OUTPUT"      | awk '/PostgreSQL/   {print $1}')
STRIPE_ID=$(echo "$OUTPUT"  | awk '/stripe/       {print $1}')

# Verify
echo "auth=$AUTH_ID gateway=$GATEWAY_ID db=$DB_ID"
```

### Step 4 — Write the relationships batch file

Use the captured IDs directly via a heredoc:

```bash
cat > /tmp/relationships.json << EOF
[
  {"type": "depends_on",      "from": "$AUTH_ID",    "to": "$DB_ID"},
  {"type": "depends_on",      "from": "$GATEWAY_ID", "to": "$AUTH_ID"},
  {"type": "depends_on",      "from": "$WORKER_ID",  "to": "$DB_ID"},
  {"type": "uses_dependency", "from": "$AUTH_ID",    "to": "$STRIPE_ID"}
]
EOF
```

### Step 5 — Create relationships

```bash
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph relationships create-batch --file /tmp/relationships.json
```

### Step 6 — Verify

```bash
NO_PROMPT=1 MEMORY_PROJECT=$MP memory query "what services exist and what do they depend on?"
# or inspect a specific object's edges:
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects edges $AUTH_ID
```

---

## Updating objects

`update` merges properties — it does not replace the whole object:

```bash
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects update <id> \
  --properties '{"status": "deprecated", "replacement": "auth-service-v2"}'
```

To find the ID first, see **Lookups** below.

---

## Idempotent creates with `--key`

Use `--key` when a script may re-run and you want skip-or-update semantics:

```bash
# Skip if already exists (default):
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects create \
  --type Service --key "svc-auth" --name "auth-service" --description "..."

# Create-or-update (upsert):
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects create \
  --type Service --key "svc-auth" --name "auth-service" --description "..." --upsert
```

Keys are stable identifiers you control — use slugs like `svc-auth`, `db-postgres`, `dep-stripe`.

---

## Lookups

Find an object ID by type and name when you don't have it:

```bash
# List all objects of a type (table view):
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects list --type Service

# Get ID for a specific name (JSON + jq):
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects list --type Service --output json \
  | jq -r '.objects[] | select(.properties.name=="auth-service") | .entityId'

# Get full details for a known ID:
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects get <id>

# Show all edges (relationships) for an object:
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects edges <id>

# List relationships of a specific type:
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph relationships list --type depends_on
```

---

## Deleting

Deletes are soft — objects are marked deleted but not purged:

```bash
# Delete an object:
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects delete <id>

# Delete a relationship (get its ID from `relationships list` first):
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph relationships delete <id>
```

---

## Single-create (fallback only)

Use single-create **only** when adding one isolated object after the graph is already populated:

```bash
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects create \
  --type Service --name "new-service" --description "..."

NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph relationships create \
  --type depends_on --from <source-id> --to <target-id>
```

---

## See also

- **memory-query** — verify what was inserted with natural language questions
- **memory-schemas** — check or install object/relationship types before inserting
- **memory-blueprints** — for large declarative seed operations from JSONL files
