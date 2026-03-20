---
name: memory-graph
description: Create, update, or delete graph objects and relationships imperatively — including batch inserts from parsed source files. Use for any direct write to the knowledge graph after the project is set up.
metadata:
  author: emergent
  version: "1.1"
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

Each individual `memory graph objects create` call is a separate API round-trip. A `create-batch` call with 50 objects takes the same time as one with 1.

---

## Two formats for `create-batch`

`memory graph objects create-batch` auto-detects the input format:

| Format | When to use | Top-level JSON |
|---|---|---|
| **Subgraph** (preferred when relationships needed) | Objects + relationships in one atomic call | `{ "objects": [...], "relationships": [...] }` |
| **Flat array** (objects only) | Objects with no relationships | `[{...}, ...]` |

---

## Workflow A — Subgraph format (preferred when wiring relationships)

Use this when you need to create objects **and** wire relationships between them. One call, no ID capture required.

### Step 1 — Check available types

```bash
NO_PROMPT=1 MEMORY_PROJECT=$MP memory schemas compiled-types
```

### Step 2 — Write the subgraph file

```bash
cat > /tmp/subgraph.json << 'EOF'
{
  "objects": [
    {"_ref": "auth",    "type": "Service",  "key": "svc-auth",    "name": "auth-service",  "description": "Handles JWT validation"},
    {"_ref": "gateway", "type": "Service",  "key": "svc-gateway", "name": "api-gateway",   "description": "Routes requests"},
    {"_ref": "db",      "type": "Database", "key": "db-postgres",  "name": "PostgreSQL",    "description": "Primary relational store"},
    {"_ref": "stripe",  "type": "ExternalDependency", "key": "dep-stripe", "name": "stripe", "description": "Payment API"}
  ],
  "relationships": [
    {"type": "depends_on",      "src_ref": "auth",    "dst_ref": "db"},
    {"type": "depends_on",      "src_ref": "gateway", "dst_ref": "auth"},
    {"type": "uses_dependency", "src_ref": "auth",    "dst_ref": "stripe"}
  ]
}
EOF
```

**Key fields:**
- `_ref` — client-side placeholder (any unique string); used by `src_ref`/`dst_ref` in relationships
- `key` — optional stable identifier for idempotent re-runs (skip if already exists)
- `name`, `description` — convenience shortcuts placed into `properties`

### Step 3 — Create the subgraph

```bash
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects create-batch --file /tmp/subgraph.json
```

Text output: one `<entity-id>  <type>  <name>` line per object, then `Created N objects, M relationships`.

To capture the `ref_map` (placeholder → UUID) for chaining:

```bash
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects create-batch \
  --file /tmp/subgraph.json --output json | tee /tmp/subgraph_result.json

# Extract a specific ID:
AUTH_ID=$(python3 -c "import json,sys; d=json.load(open('/tmp/subgraph_result.json')); print(d['ref_map']['auth'])")
```

### Step 4 — Verify

```bash
NO_PROMPT=1 MEMORY_PROJECT=$MP memory query "what services exist and what do they depend on?"
```

---

## Chunking for large populations (>100 objects)

The server limit is 100 objects and 200 relationships per subgraph call. For larger populations, use `key` on all objects and split into chunks. Cross-chunk relationships reference objects by their `key` via a lookup after the first chunk.

```python
#!/usr/bin/env python3
"""Split a large object+relationship list into subgraph chunks."""
import json, subprocess, sys

OBJECTS = [
    {"_ref": f"obj-{i}", "type": "APIEndpoint", "key": f"ep-{i}", "name": f"endpoint-{i}"}
    for i in range(250)
]
RELATIONSHIPS = [
    {"type": "depends_on", "src_ref": f"obj-{i}", "dst_ref": f"obj-{i-1}"}
    for i in range(1, 250)
]

CHUNK_SIZE = 90  # stay under 100-object limit

def chunk_subgraph(objects, relationships, chunk_size):
    """Split objects into chunks; assign relationships to the chunk that owns src_ref."""
    chunks = []
    for i in range(0, len(objects), chunk_size):
        obj_chunk = objects[i:i+chunk_size]
        refs_in_chunk = {o["_ref"] for o in obj_chunk}
        # Only include relationships where both src and dst are in this chunk.
        # Cross-chunk relationships must be created separately after all objects exist.
        rel_chunk = [r for r in relationships
                     if r["src_ref"] in refs_in_chunk and r["dst_ref"] in refs_in_chunk]
        chunks.append({"objects": obj_chunk, "relationships": rel_chunk})
    return chunks

chunks = chunk_subgraph(OBJECTS, RELATIONSHIPS, CHUNK_SIZE)
print(f"{len(OBJECTS)} objects → {len(chunks)} chunks")

for i, chunk in enumerate(chunks):
    path = f"/tmp/subgraph_chunk_{i+1}.json"
    with open(path, "w") as f:
        json.dump(chunk, f)
    print(f"Chunk {i+1}: {len(chunk['objects'])} objects, {len(chunk['relationships'])} relationships → {path}")
```

Then run each chunk:

```bash
for f in /tmp/subgraph_chunk_*.json; do
  NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects create-batch --file "$f"
done
```

For cross-chunk relationships (where src and dst are in different chunks), create them after all objects exist using `memory graph relationships create-batch` with the UUIDs from the `ref_map` outputs.

---

## Workflow B — Flat array format (objects only, no relationships)

Use this when creating objects with no relationships to wire.

### Step 1 — Check available types

```bash
NO_PROMPT=1 MEMORY_PROJECT=$MP memory schemas compiled-types
```

### Step 2 — Write the objects batch file

```bash
cat > /tmp/objects.json << 'EOF'
[
  {"type": "Service",            "name": "auth-service",  "description": "Handles JWT validation"},
  {"type": "Service",            "name": "api-gateway",   "description": "Routes requests"},
  {"type": "Database",           "name": "PostgreSQL",    "description": "Primary relational store"},
  {"type": "ExternalDependency", "name": "stripe",        "description": "Payment processing API"}
]
EOF
```

### Step 3 — Create objects and capture IDs

```bash
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects create-batch --file /tmp/objects.json \
  | tee /tmp/batch_output.txt
```

Output format is one line per object: `<entity-id>  <type>  <name>`

**Always tee to a file.** The IDs only appear in this stdout — do not try to re-fetch them via `objects list`. Parse from the saved file:

```bash
AUTH_ID=$(awk '/auth-service/ {print $1}' /tmp/batch_output.txt)
GATEWAY_ID=$(awk '/api-gateway/  {print $1}' /tmp/batch_output.txt)
DB_ID=$(awk '/PostgreSQL/   {print $1}' /tmp/batch_output.txt)
```

**Batches > 200 items:** `create-batch` has a 200-item limit for the flat-array format. Split before running:

```bash
python3 -c "
import json
with open('/tmp/objects.json') as f: data = json.load(f)
for i, chunk in enumerate([data[i:i+200] for i in range(0, len(data), 200)]):
    with open(f'/tmp/objects_batch_{i+1}.json', 'w') as f: json.dump(chunk, f)
print(f'{len(data)} objects → {-(-len(data)//200)} batches')
"

for f in /tmp/objects_batch_*.json; do
  NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects create-batch --file "$f" \
    | tee -a /tmp/batch_output.txt
done
```

### Step 4 — Create relationships separately

```bash
cat > /tmp/relationships.json << EOF
[
  {"type": "depends_on",      "from": "$AUTH_ID",    "to": "$DB_ID"},
  {"type": "depends_on",      "from": "$GATEWAY_ID", "to": "$AUTH_ID"}
]
EOF

NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph relationships create-batch --file /tmp/relationships.json
```

---

## Script-generated batches

When populating from source files (routes, SQL queries, config vars), write a Python script that parses the source and writes the subgraph JSON, then run it:

```python
#!/usr/bin/env python3
"""Parse server.go routes → /tmp/subgraph.json"""
import json

ROUTES = [
    # (method, path, handler_func, domain, auth_required)
    ("GET",  "/api/v1/cases",  "listCases",  "cases", True),
    ("POST", "/api/v1/cases",  "createCase", "cases", True),
]

objects = []
for method, path, func, domain, auth in ROUTES:
    ref = f"ep-{method.lower()}-{path.replace('/', '-').strip('-')}"
    objects.append({
        "_ref": ref,
        "type": "APIEndpoint",
        "key": ref,
        "name": f"{method} {path}",
        "properties": {
            "method": method, "path": path,
            "handler_func": func, "domain": domain,
            "auth_required": auth
        }
    })

subgraph = {"objects": objects, "relationships": []}
with open("/tmp/subgraph.json", "w") as f:
    json.dump(subgraph, f, indent=2)

print(f"{len(objects)} objects written to /tmp/subgraph.json")
```

```bash
python3 /tmp/gen_subgraph.py
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects create-batch --file /tmp/subgraph.json
```

---

## Updating objects

`update` merges properties — it does not replace the whole object:

```bash
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects update <id> \
  --properties '{"status": "deprecated", "replacement": "auth-service-v2"}'
```

---

## Idempotent creates with `key`

Use `key` when a script may re-run and you want skip-or-update semantics. Works in both subgraph format (as a field on each object) and single-create:

```bash
# Single-create with key (skip if already exists):
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects create \
  --type Service --key "svc-auth" --name "auth-service" --description "..."

# Single-create with key + upsert (create-or-update):
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects create \
  --type Service --key "svc-auth" --name "auth-service" --description "..." --upsert
```

Keys are stable identifiers you control — use slugs like `svc-auth`, `db-postgres`, `dep-stripe`.

---

## Lookups

Find an object ID by type and name when you don't have it.

**`list` output format** — JSON output is `{"objects": [...]}` where each object has an `entity_id` field (not `entityId`). There is **no `--offset` flag** — use the batch output file or `ref_map` to get IDs instead of re-fetching.

```bash
# List all objects of a type (table view):
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects list --type Service

# Get ID for a specific name (JSON + python):
NO_PROMPT=1 MEMORY_PROJECT=$MP memory graph objects list --type Service --output json \
  | python3 -c "import json,sys; objs=json.load(sys.stdin)['objects']; \
    print(next(o['entity_id'] for o in objs if o['properties']['name']=='auth-service'))"

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
