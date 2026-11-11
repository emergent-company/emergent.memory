---
id: blueprint-authoring
title: Blueprint Authoring Guide
category: guides
tags: [blueprints, schemas, mcp, api, cli]
description: Create, publish, apply, and version blueprints over MCP, the REST API, or a directory
lastUpdated: 2026-11-03
readTime: 12
related: [schema-creation]
---

# Blueprint Authoring Guide

A **blueprint** is a versioned bundle of schema packs, agent definitions, skills,
and seed graph data that a project applies as a unit. This guide covers authoring
one and getting it applied, over MCP (agents), the REST API, and the CLI
(directory workflow).

For the concept and lifecycle overview, see the Blueprints page of the docs site.

## Lifecycle

```
create / import  ──▶  draft  ──publish──▶  published  ──apply──▶  applied
                          ▲                    │
                          └── new-version ─────┘
```

- **draft** — editable (`blueprint-update`).
- **publish** — computes a `sha256` checksum and freezes the version. Published
  blueprints are immutable; change one by forking with `blueprint-new-version`.
- **apply** — materializes the manifest into the calling project.
- **unapply** — removes the agent definitions and pack assignments the blueprint
  owns; leaves the shared pack row, skills, and seed data.

Scope: a `builtin` blueprint is global and immutable; `user` and `import`
blueprints are private to a project. Creating a **global** blueprint requires
`superadmin_full` — project members author project-private drafts.

## Manifest shape

`camelCase` and `snake_case` spellings of the pack's type lists decode the same
way (`objectTypes`/`objectTypeSchemas`).

```jsonc
{
  "packs": [
    {
      "name": "crm",
      "version": "1.0.0",
      "description": "Companies, contacts, and deals",
      "objectTypes": [
        {
          "name": "Company",
          "label": "Company",
          "properties": {
            "name":     { "type": "string", "title": "Company name" },
            "homepage": { "type": "url",    "title": "Homepage" }
          },
          "required": ["name"]
        }
      ],
      "relationshipTypes": [
        { "name": "works_at", "label": "Works At", "sourceType": "Contact", "targetType": "Company" }
      ]
    }
  ],
  "agents": [
    {
      "name": "deal-desk",
      "systemPrompt": "Answer questions about the sales pipeline.",
      "model": { "name": "google/gemini-2.5-flash" }
    }
  ],
  "skills": [],
  "seed": {
    "objects": [
      { "type": "Company", "key": "acme", "properties": { "name": "Acme Corp" } }
    ],
    "relationships": [
      { "type": "works_at", "srcKey": "alice", "dstKey": "acme" }
    ]
  }
}
```

Property keywords the registry validates: `type` (`string`, `number`, `integer`,
`boolean`, `date`, `time`, `url`, `email`, `phone`, `country`, `currency`,
`md5`/`sha1`/`sha256`/`sha512`, `array`, `object`), `title` (display label),
`description`, `items` (array element schema), `format` (semantic string
format), `enum` (allowed values for a primitive property; `items.enum` for array
elements), `minimum`/`maximum` (enforced on `number`/`integer`), and a
type-level `required` list. See the Schema Creation Guide for details.

## MCP workflow

| Step | Tool | Notes |
|---|---|---|
| Create draft | `blueprint-create` | `name`, `version`, `description`, `author`, `manifest` |
| Edit draft | `blueprint-update` | `id`, plus any of description/author/manifest |
| Freeze | `blueprint-publish` | `id` — fails unless the blueprint is a draft |
| Apply | `blueprint-apply` | `id`; project comes from the token/session |
| Inspect | `blueprint-list`, `blueprint-get`, `blueprint-versions`, `blueprint-list-applied` | `schema:read` |
| Fork | `blueprint-new-version` | `id`, `version` — clones into a draft in your project |
| Reverse | `blueprint-unapply` | `id` |

All write tools require `schema:write`.

```jsonc
// blueprint-create
{ "name": "crm", "version": "1.0.0", "description": "Sales pipeline", "manifest": { /* above */ } }
```

## REST workflow

```bash
# create + publish + apply
curl -X POST https://api.example.com/api/blueprints \
  -H "Authorization: Bearer <token>" -H "X-Project-ID: <projectId>" \
  -d '{"name":"crm","version":"1.0.0","manifest":{...}}'

curl -X POST https://api.example.com/api/blueprints/<id>/publish \
  -H "Authorization: Bearer <token>" -H "X-Project-ID: <projectId>"

curl -X POST https://api.example.com/api/blueprints/<id>/apply \
  -H "Authorization: Bearer <token>" -H "X-Project-ID: <projectId>"
```

Other endpoints: `GET /api/blueprints`, `GET|PUT|DELETE /api/blueprints/:id`,
`POST /api/blueprints/:id/versions`, `GET /api/blueprints/name/:name/versions`,
`POST /api/blueprints/:id/unapply`, `GET /api/blueprints/applied`,
`POST /api/blueprints/import[/preview]`,
`GET|POST /api/blueprints/:name/updates[...]`,
`GET|PUT /api/blueprints/model-overrides`,
`PUT /api/blueprints/github-credentials`.

## CLI workflow (directory)

The CLI applies a directory or GitHub repo without the registry draft/publish
step:

```bash
memory blueprints inspect ./my-blueprint     # list contents, no API calls
memory blueprints validate ./my-blueprint    # offline validation
memory blueprints install ./my-blueprint --dry-run
memory blueprints install ./my-blueprint --project my-project
```

Layout:

```
my-blueprint/
  schemas/                     # preferred; packs/ accepted for compatibility
    crm.yaml
  agents/
    deal-desk.yaml
  skills/
    deal-review/SKILL.md
  seed/
    objects/Company.jsonl
    relationships/works_at.jsonl
```

Flags for `install`: `--project`, `--upgrade`, `--dry-run`, `--token`,
`--map-model agent=provider/model`, `--no-map`. Exports live project graph data
with `memory blueprints dump <dir> [--types Person,Company]`.

## Apply semantics

| Content | Behaviour |
|---|---|
| Packs | Created if absent (by name+version); **always assigned** to the project |
| Built-in packs | Shared global row; definitions refreshed on every re-apply |
| Agents | Create-or-update by name |
| Skills | Create-or-update by name, global scope |
| Seed objects | Upserted by `key`; objects with **no** `key` are skipped (registry apply path only) |
| Seed relationships | Deduplicated by the server (`Upsert` on type+endpoints) |

Apply is **not atomic** — packs, agents, and skills fail fast, seed data is
best-effort — but every step is idempotent, so a failed apply is retried after
the manifest is fixed. Provenance is recorded only after all steps succeed.

The registry apply path and the CLI's directory install differ on one point: the
registry **skips** a seed object that has no `key`, while `memory blueprints
install` creates it, every run. Always set `key`.

## Gotchas

- Set `key` on seed objects — the registry apply path skips keyless ones, and
  prefer `srcKey`/`dstKey` over entity IDs in relationships.
- A published version cannot be edited or re-published — fork it.
- Deleting a blueprint is refused while it is applied; unapply first. Built-ins
  are immutable and cannot be deleted.
- Unapply does not delete skills or seed objects (shared scope).
- Model mapping precedence: explicit request > stored per-project override >
  the manifest's pinned model.
