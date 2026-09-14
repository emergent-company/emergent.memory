---
name: memory-branches
description: Create and manage graph branches — isolated workspaces for staging changes before merging to main. Use for what-if analysis, planning, or any work that should not immediately affect the main graph.
metadata:
  author: emergent
  version: "1.0"
---

Work with Memory graph branches — isolated copies of the graph where you can create, update, and delete objects without affecting the main branch until you explicitly merge.

## Rules

- **Project context is auto-discovered** — the CLI walks up the directory tree to find `.env.local` containing `MEMORY_PROJECT` or `MEMORY_PROJECT_ID`. If `.env.local` is present anywhere above the current directory, `--project` is not needed. Only pass `--project <id>` explicitly when overriding or when no `.env.local` exists.
- **Use `--branch <id>` on all graph write commands** when working on a branch — objects and relationships created without `--branch` go to the main branch.
- **Trust this skill over `--help` output** — `--help` text may lag behind the installed binary.

---

## What is a branch?

A branch is an isolated workspace scoped to a project. Objects and relationships created on a branch are invisible to the main branch (and vice versa) until you merge. Branches are useful for:

- **What-if analysis** — model a scenario without polluting the main graph
- **Planning** — stage a set of changes for review before committing
- **Parallel work** — multiple agents working on different aspects simultaneously

The main branch has no ID — it is the default when `--branch` is omitted.

---

## Branch lifecycle

```
create branch → write objects/relationships with --branch <id> → verify → merge → delete branch
```

---

## Step 1 — Create a branch

```bash
# Create a branch scoped to the current project:
memory graph branches create \
  --name "plan/add-auth-service" \
  --description "Staging area for auth service v2 design"
```

Output:
```
ID:       7602d370-64c2-451b-81a2-0b50ba74343a
Name:     plan/add-auth-service
Desc:     Staging area for auth service v2 design
Project:  ea62f9f7-396a-4b1e-912b-3b5579a7cf0a
Created:  2026-03-15T10:00:00Z
```

**Capture the branch ID immediately:**
```bash
BRANCH_ID=$(memory graph branches create \
  --name "plan/add-auth-service" \
  --description "Staging area for auth service v2 design" \
  --output json | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
echo "Branch: $BRANCH_ID"
```

---

## Step 2 — Write to the branch

Pass `--branch <id>` to every graph write command. Without it, writes go to the main branch.

```bash
# Create an object on the branch:
memory graph objects create \
  --type Service --key "svc-auth-v2" --name "auth-service-v2" \
  --status planned \
  --branch "$BRANCH_ID"

# Create a relationship on the branch:
memory graph relationships create \
  --type depends_on --from <src-entity-id> --to <dst-entity-id> \
  --branch "$BRANCH_ID"

# Batch-create objects on the branch (subgraph format):
# Note: --branch is not yet supported on create-batch; use single-create or
# set branch_id in the JSON body via --output json piped to curl if needed.
```

---

## Step 3 — List branch contents

```bash
# List objects on the branch:
memory graph objects list --branch "$BRANCH_ID"

# List objects of a specific type on the branch:
memory graph objects list --type Service --branch "$BRANCH_ID"

# List relationships on the branch:
memory graph relationships list --branch "$BRANCH_ID"

# List all branches for the project:
memory graph branches list
```

---

## Step 4 — Preview the merge (dry run)

Before merging, always preview what will change. The dry run classifies each diverged object:

| Status | Meaning |
|---|---|
| `added` | Exists on source branch only — will be added to target |
| `fast_forward` | Changed on source only — will be updated on target |
| `conflict` | Changed on both branches — requires manual resolution |
| `unchanged` | Identical on both branches — no action taken |

```bash
# Dry run (default — no changes made):
memory graph branches merge main \
  --source "$BRANCH_ID"

# Get full conflict details as JSON:
memory graph branches merge main \
  --source "$BRANCH_ID" --output json
```

> **Merging into main:** Use the special keyword `main` as the target — the main graph has no branch ID and does not appear in `branches list`.
> ```bash
> memory graph branches merge main --source "$BRANCH_ID"
> ```

---

## Step 5 — Execute the merge

```bash
memory graph branches merge main \
  --source "$BRANCH_ID" --execute
```

Output:
```
Merge APPLIED
Source:  7602d370-64c2-451b-81a2-0b50ba74343a
Target:  <main-branch-id>

Objects (3 total):
  added:        2
  fast_forward: 1
  conflict:     0
  unchanged:    0

2 object(s) applied to target branch.
```

---

## Step 6 — Delete the branch (cleanup)

After a successful merge, delete the branch to keep the list clean:

```bash
memory graph branches delete "$BRANCH_ID"
```

---

## Full example — end to end

```bash
export MP=ea62f9f7-396a-4b1e-912b-3b5579a7cf0a

# 1. Create branch
BRANCH_ID=$(memory graph branches create \
  --name "plan/add-payment-service" --output json \
  | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
echo "Working on branch: $BRANCH_ID"

# 2. Create objects on the branch
memory graph objects create \
  --type Service --key "svc-payments" --name "payments-service" \
  --status planned --branch "$BRANCH_ID"

memory graph objects create \
  --type ExternalDependency --key "dep-stripe" --name "Stripe" \
  --branch "$BRANCH_ID"

# 3. Wire relationships on the branch
PAYMENTS_ID=$(memory graph objects list \
  --type Service --branch "$BRANCH_ID" --output json \
  | python3 -c "import json,sys; d=json.load(sys.stdin); \
    print(next(o['entity_id'] for o in d['items'] if o.get('key')=='svc-payments'))")

STRIPE_ID=$(memory graph objects list \
  --type ExternalDependency --branch "$BRANCH_ID" --output json \
  | python3 -c "import json,sys; d=json.load(sys.stdin); \
    print(next(o['entity_id'] for o in d['items'] if o.get('key')=='dep-stripe'))")

memory graph relationships create \
  --type uses_dependency --from "$PAYMENTS_ID" --to "$STRIPE_ID" \
  --branch "$BRANCH_ID"

# 4. Verify branch contents
memory graph objects list --branch "$BRANCH_ID"

# 5. Preview merge into main (use the keyword "main" — no ID needed)
memory graph branches merge main \
  --source "$BRANCH_ID"

# 6. Execute merge into main
memory graph branches merge main \
  --source "$BRANCH_ID" --execute

# 8. Cleanup
memory graph branches delete "$BRANCH_ID"
```

---

## Branch management commands

```bash
# List all branches for the project (shows ID, name, description, parent, created):
memory graph branches list

# Get details for a specific branch:
memory graph branches get <branch-id>

# Rename a branch:
memory graph branches update <branch-id> --name "new-name"

# Update description only:
memory graph branches update <branch-id> --description "new purpose"

# Rename and update description:
memory graph branches update <branch-id> --name "new-name" --description "new purpose"

# Delete a branch:
memory graph branches delete <branch-id>
```

## Fork a branch (copy-on-fork)

`fork` creates a new branch **and immediately copies all HEAD objects and relationships** from the source. This is different from `create --parent`, which only records lineage metadata without copying any data.

Copied objects preserve their canonical IDs so a subsequent merge back into the source is identity-aware.

```bash
# Fork from main — copies everything:
FORK_ID=$(memory graph branches fork main \
  --name "what-if/add-payment-service" --output json \
  | python3 -c "import json,sys; print(json.load(sys.stdin)['branch_id'])")

# Fork from an existing branch:
memory graph branches fork "$SOURCE_BRANCH_ID" \
  --name "child-branch" --description "subset of parent"

# Fork with type filter — only copy Service and API objects:
memory graph branches fork main \
  --name "services-only" \
  --filter-type Service \
  --filter-type API
```

Output:
```
Branch forked successfully.
Branch ID:             7602d370-64c2-451b-81a2-0b50ba74343a
Branch Name:           what-if/add-payment-service
Source:                main
Copied Objects:        42
Copied Relationships:  18
```

**Flags:**
- `--name <name>` — new branch name (required)
- `--description <text>` — optional description
- `--filter-type <type>` — only copy objects of this type (repeatable); relationships where one endpoint was excluded are skipped

**When to use fork vs create:**

| | `branches create` | `branches fork` |
|---|---|---|
| Creates a branch | ✓ | ✓ |
| Records parent lineage | via `--parent` | ✓ (automatic) |
| Copies objects/relationships | ✗ | ✓ |
| Use for | empty staging area | what-if with existing data |

## Querying a branch

Use `memory query --mode=search --branch <id>` to search a specific branch.
Without `--branch`, the main graph is searched. `--branch` is not supported in agent mode.

```bash
# Search the main graph:
memory query --mode=search "planned services"

# Search a specific branch:
memory query --mode=search --branch "$BRANCH_ID" "planned services"
```

---

## Merge edge cases

| Situation | What happens |
|---|---|
| `--execute` with conflicts | Blocked — merge is not applied. Resolve conflicts manually then retry. |
| `--execute` with no conflicts | Runs in a single DB transaction. All-or-nothing: if any write fails, the whole merge rolls back. |
| Object added on branch, relationship points to it | The relationship's `src_id`/`dst_id` is remapped to the new canonical ID on the target branch automatically. |
| Same property key, same value on both branches | Not a conflict — classified as `fast_forward` or `unchanged`. |
| Same property key, different values on both branches | Conflict — listed under `conflicts` in `--output json`. |
| `status`, `key`, or `labels` changed on branch | Correctly detected and merged (included in content hash). |
| Graph > 500 objects | Preview is truncated — a warning is printed. Run with `--output json` to see all. Merge execution is not affected by the preview limit. |
| Merge of an already-merged branch | Objects that are identical on both sides are classified `unchanged` and skipped — safe to re-run. |

## Common mistakes

| Mistake | Fix |
|---|---|
| Forgetting `--branch` on writes | Objects silently go to main branch — always pass `--branch $BRANCH_ID` |
| Using `--branch` on `branches create` | `branches create` doesn't take `--branch`; use `--parent` for child branches |
| Trying `MEMORY_BRANCH` env var | Not supported — always pass `--branch <id>` explicitly |
| Trying `X-Branch-ID` header | Not a header — branch is a body field (for create) or query param (for list) |
| Merging without a dry run first | Always preview with `branches merge <target> --source <src>` before `--execute` |
| Not capturing the branch ID | Run `branches list` to recover it, or always capture at creation time |
| Using `fork` output `id` field | `fork` returns `branch_id`, not `id` — use `json.load(sys.stdin)['branch_id']` |

---

## See also

- **memory-graph** — creating and updating objects and relationships
- **memory-query** — querying the graph with natural language
