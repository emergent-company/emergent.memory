# Branches

Branches let you work on isolated copies of the knowledge graph — making changes, reviewing them, and merging back — without affecting the main graph until you're ready.

Think of it like git branches, but for your knowledge graph.

---

## When to use branches

- **Reviewing AI-extracted content** before it enters the main graph
- **Exploratory work** — adding objects and relationships you may want to roll back
- **Parallel editing** — multiple people working on separate sets of changes
- **Staged imports** — loading a new data source into a branch, validating, then merging

---

## Creating a Branch

=== "API"
    ```http
    POST /api/graph/branches
    Content-Type: application/json

    {
      "name": "q4-planning-review",
      "project_id": "proj_xyz789"
    }
    ```

    Response:
    ```json
    {
      "id": "branch_abc123",
      "name": "q4-planning-review",
      "project_id": "proj_xyz789",
      "parent_branch_id": null,
      "created_at": "2026-03-08T10:00:00Z"
    }
    ```

### Branch from an existing branch

```http
POST /api/graph/branches
Content-Type: application/json

{
  "name": "sub-feature",
  "project_id": "proj_xyz789",
  "parent_branch_id": "branch_abc123"
}
```

---

## Working on a Branch

Pass `branch_id` in the request body when creating objects or relationships on a branch:

```http
POST /api/graph/objects
Content-Type: application/json

{
  "type": "Decision",
  "branch_id": "branch_abc123",
  "properties": { "title": "Adopt event sourcing" }
}
```

For list/get requests, pass `branch_id` as a query parameter:

```http
GET /api/graph/objects?branch_id=branch_abc123
```

Objects created on a branch are isolated — they are not visible in the main graph until you merge.

---

## Listing Branches

```http
GET /api/graph/branches
```

---

## Getting a Branch

```http
GET /api/graph/branches/{id}
```

---

## Merging a Branch

When you are ready to promote changes from a source branch into a target branch, merge it.

**Direction: source → target.** The source branch is read; the target branch receives the changes.

By default the merge is a **dry run** — it shows what would change without mutating state. Pass `"execute": true` to apply.

```http
POST /api/graph/branches/main/merge
Content-Type: application/json

{
  "source_branch_id": "branch_abc123"
}
```

Use `main` as the path segment to merge into the main graph. Use a branch UUID to merge into another branch.

Dry-run response shows each object classified as:

| Classification | Meaning |
|---|---|
| `added` | Exists on source only — will be created on target |
| `fast_forward` | Changed on source only — target will be updated |
| `conflict` | Changed on both branches — **blocks execute until resolved** |
| `unchanged` | Identical on both branches — nothing to do |

To execute after reviewing:

```http
POST /api/graph/branches/main/merge
Content-Type: application/json

{
  "source_branch_id": "branch_abc123",
  "execute": true
}
```

The merge runs in a single database transaction — it is all-or-nothing. If any conflict exists, `execute` is rejected until conflicts are resolved manually.

---

## Deleting a Branch

```http
DELETE /api/graph/branches/{id}
```

!!! warning
    Deleting a branch removes all objects that exist **only** on that branch. Objects that have been merged into the parent are unaffected.

---

## Renaming a Branch

```http
PATCH /api/graph/branches/{id}
{ "name": "new-name" }
```

---

## Branch hierarchy

Branches can optionally record a `parent_branch_id` — which branch they were forked from. This is **lineage metadata only** and does not affect merge behavior. The merge compares object content hashes directly between source and target regardless of parentage.

A null `parent_branch_id` means the branch was created off the main graph.

```
main graph (no branch ID)
├── feature-a          (parent_branch_id: null)
│   └── feature-a-sub  (parent_branch_id: feature-a)
└── feature-b          (parent_branch_id: null)
```

!!! note
    The main graph itself is not a branch and has no ID. To merge a branch into the main graph, you need the main graph's branch record ID — use `GET /api/graph/branches` to list all branches and identify the one representing main (typically the one with no parent and the earliest creation date).

---

## Letting an agent read unmerged branches

By default an agent searches **the main graph only** — pending work sitting on an
unmerged branch is invisible to it. An agent definition can opt into reading
branches that have not been merged yet, narrowed by why a branch exists and by
its name:

```http
PATCH /api/projects/{projectId}/agent-definitions/{id}
Content-Type: application/json

{
  "branchScope": {
    "unmerged": true,
    "origins": ["extraction"],
    "branches": ["extraction/*"],
    "maxBranches": 3
  }
}
```

| Field | Meaning |
|---|---|
| `unmerged` | `true` enables branch reads. Absent or `false` means main only. |
| `origins` | Only branches created by these sources: `extraction`, `agent`, `manual`, `system`. Empty/omitted allows any. |
| `branches` | Name globs. `*` is the only wildcard and it matches across `/`, so `extraction/*` covers every extraction branch. |
| `maxBranches` | How many branches one search may read, newest first. Default 3, maximum 20. |

When both `origins` and `branches` are set, a branch must match both.

A search under a scope reads the main graph plus each matching branch and merges
the results. Every object that exists only on a branch comes back flagged with
`branch_id`, `branch_name`, `origin` and `origin_label`, so a model can say
"on the pending extraction branch for Invoice.pdf…" rather than presenting it as
settled fact. When the cap leaves branches unread, the response metadata carries a
`branch_scope_note` saying so.

!!! note "It narrows reads, and bounds every branch a run can name"
    A branch scope grants no permission of its own: it only restricts. It bounds
    every branch a run can name on any tool, so a scoped run cannot reach a branch
    outside its scope — on a read or a write. Whether it may change an **in-scope**
    branch at all is still decided by its ordinary write permissions, not by this
    setting, so give read scope to a reviewing agent you would not give write
    access to. A delegated sub-agent additionally receives the intersection of its
    own scope and its caller's — a child can never read more than its parent, and a
    child that declares nothing of its own reads the main graph only.
