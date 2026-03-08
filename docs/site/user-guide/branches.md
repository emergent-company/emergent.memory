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
      "projectId": "proj_xyz789"
    }
    ```

    Response:
    ```json
    {
      "id": "branch_abc123",
      "name": "q4-planning-review",
      "projectId": "proj_xyz789",
      "parentBranchId": null,
      "createdAt": "2026-03-08T10:00:00Z"
    }
    ```

### Branch from an existing branch

```http
POST /api/graph/branches
{
  "name": "sub-feature",
  "projectId": "proj_xyz789",
  "parentBranchId": "branch_abc123"
}
```

---

## Working on a Branch

Pass the `branchId` in your graph API requests to read and write objects on that branch:

```http
POST /api/graph/objects
X-Branch-ID: branch_abc123

{
  "type": "Decision",
  "properties": { "title": "Adopt event sourcing" }
}
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

When you are ready to promote changes from a branch into the main graph (or into a parent branch), merge it:

```http
POST /api/graph/branches/{targetBranchId}/merge
Content-Type: application/json

{
  "sourceBranchId": "branch_abc123"
}
```

The merge applies all objects and relationships from the source branch onto the target. Conflicts are resolved by the merge strategy (last-write-wins by default).

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

Branches form a tree: each branch has an optional `parentBranchId`. A null parent means the branch is off the main graph. The lineage is tracked in `kb.branch_lineage` and used during merge to compute the delta.

```
main graph (no branch)
├── feature-a
│   └── feature-a-sub
└── feature-b
```
