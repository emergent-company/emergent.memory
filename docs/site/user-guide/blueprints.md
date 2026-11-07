# Blueprints

A **blueprint** is a versioned, declarative bundle that a project can apply as a
unit: schema packs, agent definitions, skills, and seed graph objects and
relationships. Applying a blueprint materializes its content into the project;
un-applying removes only the content that blueprint owned.

Blueprints are how a project gets a working domain model without hand-building
every type, agent, and seed row. They are also the recommended way to share a
setup across projects or teams.

---

## What a blueprint contains

| Part | What it does | Apply semantics |
|---|---|---|
| **Schema packs** (`packs`) | Object types and relationship types | Created if absent; **always assigned** to the applying project |
| **Agent definitions** (`agents`) | Runtime agents (prompt, model, tools) | Create-or-update by name |
| **Skills** (`skills`) | Markdown skill files (agentskills.io format) | Create-or-update by name, global scope |
| **Seed** (`seed`) | Graph objects and relationships | Idempotent — keyed objects upsert; objects without a `key` are skipped |

A blueprint may carry any subset. Of the built-ins, `operator` ships only an
agent definition and `agent-notes`/`dreaming-agent`/`session-message-types` ship
only a schema pack; the example `code-memory-blueprint` in the product repo's
`blueprints/` directory is schema-only too.

---

## Where blueprints come from

Every blueprint lives in a registry and has an `origin`:

| Origin | Scope | Who can change it |
|---|---|---|
| `builtin` | Global (visible to every project) | Nobody — immutable, fork it |
| `user` | Private to the project that created it | The owning project |
| `import` | Private to the project, imported from a GitHub repo | The owning project |

Global built-in blueprints are seeded at server startup and are readable and
apply-able by every project, but never editable. To change one, fork it with
`blueprint-new-version` and edit your own copy.

### Built-in blueprints

| Blueprint | What it installs |
|---|---|
| `task-board` | Board-enabled `Task` object type, a reaction-triggered task-worker agent with its `task-workflow` skill, one example seed task |
| `agent-notes` | The `agent-notes` schema pack only (no agent definition) |
| `dreaming-agent` | The `dreaming-agent` schema pack only — the `Dream` object type and its relationships, no agent definition |
| `operator` | One general-assistant agent that can read and change platform settings (every change proposed, then applied on acceptance) |
| `session-message-types` | The `session-message-types` schema pack (object types for chat session messages) |

More example blueprints live in the product repository under
[`blueprints/`](https://github.com/emergent-company/emergent.memory/tree/main/blueprints)
and install as GitHub imports (`multi-agent`, `code-memory-blueprint`,
`blueprint-architect`, `security-review`, `webpage-feedback`, …).

---

## Lifecycle

```
create / import  ──▶  draft  ──publish──▶  published  ──apply──▶  applied
                          ▲                    │                     │
                          └── new-version ─────┘                 unapply
                                                                     │
                                                                  unapplied
```

- **Draft** — editable. Description, author, and manifest can change.
- **Publish** — computes a `sha256` checksum over the manifest and freezes the
  version. A published blueprint is immutable; edits need a new version.
- **Apply** — materializes the manifest into the calling project and records the
  application as provenance for everything it created.
- **Unapply** — reverses that application (see [Un-applying](#un-applying)).
- **Deprecate** — marks a published blueprint as deprecated. It stays readable,
  but it can no longer be applied: apply rejects anything that is not draft or
  published. It is a signal, not a removal.
- **Delete** — only for `user`/`import` blueprints, and only while unapplied.
  Built-ins cannot be deleted.

Versions are addressed by `(name, version)`. `blueprint-versions` (or
`GET /api/blueprints/name/:name/versions`) lists every version you can see,
newest first; `blueprint-new-version` clones any readable blueprint — including
a built-in — into a **draft in your project**, where you can edit and publish
it.

---

## Creating a blueprint

### With MCP

```jsonc
// blueprint-create
{
  "name": "crm",
  "version": "1.0.0",
  "description": "Sales pipeline over the knowledge graph",
  "manifest": {
    "packs": [ /* see the manifest shape below */ ],
    "agents": [],
    "skills": [],
    "seed": { "objects": [], "relationships": [] }
  }
}
```

`blueprint-create` returns the draft. Edit it with `blueprint-update`, freeze it
with `blueprint-publish`, then `blueprint-apply`.

### Manifest shape

The manifest is the same document a CLI blueprint directory describes. Both
`camelCase` and `snake_case` spellings of `objectTypes`/`relationshipTypes`
(`objectTypeSchemas`/`relationshipTypeSchemas`) decode the same way:

```yaml
name: crm
version: "1.0.0"
description: Sales pipeline over the knowledge graph
author: platform-team

packs:
  - name: crm
    version: "1.0.0"
    description: Companies, contacts, and deals
    objectTypes:
      - name: Company
        label: Company
        properties:
          name: { type: string, title: Company name }
          homepage: { type: url, title: Homepage }
      - name: Deal
        label: Deal
        properties:
          amount: { type: number, title: Amount }
    relationshipTypes:
      - name: works_at
        label: Works At
        sourceType: Contact
        targetType: Company

agents:
  - name: deal-desk
    systemPrompt: |
      Answer questions about the sales pipeline.
    model: { name: gemini-2.5-flash }

skills: []

seed:
  objects:
    - type: Company
      key: acme
      properties: { name: Acme Corp }
  relationships:
    - type: works_at
      srcKey: alice
      dstKey: acme
```

Property `title` is the label the UI shows (the key stays visible beneath it);
`url`, `sha256`, and `date` are validated property types. See
[Schema § Property definitions](../developer-guide/schema.md#property-definitions) for the full vocabulary.

### With the CLI (a directory)

The CLI applies a directory (or GitHub repo) directly — it does not go through
the registry, so there is no draft/publish step:

```bash
memory blueprints inspect ./my-blueprint     # list contents, no API calls
memory blueprints validate ./my-blueprint    # offline, no API calls
memory blueprints install ./my-blueprint --dry-run
memory blueprints install ./my-blueprint --project my-project
```

Directory layout:

```
my-blueprint/
  schemas/                        # preferred; packs/ is accepted for compatibility
    crm.yaml
  agents/
    deal-desk.yaml
  skills/
    deal-review/SKILL.md          # agentskills.io: frontmatter + Markdown body
  seed/
    objects/Company.jsonl
    relationships/works_at.jsonl
```

To get a registry blueprint *from* a directory, import it as a GitHub repo (see
[GitHub import](#importing-from-github)) — or create the draft over MCP/API and
pass the same manifest.

---

## Applying a blueprint

=== "MCP"

    ```jsonc
    // blueprint-apply
    { "id": "<blueprint-uuid>" }
    ```

=== "REST"

    ```bash
    curl -X POST https://api.example.com/api/blueprints/<id>/apply \
      -H "Authorization: Bearer <token>" \
      -H "X-Project-ID: <projectId>"
    ```

=== "Web UI"

    Open **Blueprints**, choose a blueprint, and select **Install**.

Apply is **not atomic**: packs, agents, and skills fail fast and seed data is
best-effort (counted in the result). Every step is idempotent, so a failed apply
can be retried after fixing the manifest and will converge — the application
(provenance) is recorded only after all steps succeed.

Built-in packs are shared: every project that applies the same built-in blueprint
is assigned the same global `source=builtin` pack. `user` and `import` blueprints
materialize project-scoped packs instead. Re-applying a built-in blueprint
refreshes the pack definitions from the manifest, even under an unchanged
`(name, version)`, so a content fix reaches projects without a version bump.

### Model mapping

Blueprint agents pin a model. At install time you can map them to a model your
project actually has configured:

```bash
memory blueprints install ./my-blueprint --map-model deal-desk=google/gemini-2.5-pro
memory blueprints install ./my-blueprint --map-model deal-desk=    # project default
memory blueprints install ./my-blueprint --no-map                  # keep pinned models
```

Overrides are persisted per project and keyed by blueprint name
(`GET`/`PUT /api/blueprints/model-overrides`), and are applied before
materialization — precedence: explicit request > stored override > manifest.

---

## Un-applying

`blueprint-unapply` (or `POST /api/blueprints/:id/unapply`) removes what the
blueprint owned:

| Content | On unapply |
|---|---|
| Agent definitions | Deleted, scoped by `source_blueprint_id` |
| Pack assignments | Soft-deleted if this blueprint owns them |
| Global pack row | Kept — shared with other projects |
| Skills | Left in place (global scope) |
| Seed objects/relationships | Left in place (shared data, not owned) |

An assignment is only removed when no other applied blueprint still claims the
pack. Applying a newer version of the same blueprint re-points the assignment to
the newer version; it never steals an assignment owned by a different blueprint
name.

---

## Importing from GitHub

```bash
# Preview what the repo contains — creates nothing
curl -X POST https://api.example.com/api/blueprints/import/preview \
  -H "Authorization: Bearer <token>" \
  -H "X-Project-ID: <projectId>" \
  -d '{"url":"https://github.com/acme/memory-blueprints"}'

# Import (and optionally publish + apply)
curl -X POST https://api.example.com/api/blueprints/import \
  -H "Authorization: Bearer <token>" \
  -H "X-Project-ID: <projectId>" \
  -d '{"url":"https://github.com/acme/memory-blueprints","ref":"v1.2.0","publish":true,"apply":true}'
```

- Private repositories: store a project-scoped token with
  `memory blueprints github-token set` (encrypted at rest; never echoed back).
- Re-importing an existing `(name, version)` **adopts** that row — refreshing a
  draft, reusing a published version — instead of failing with a conflict.
- The server records the repo URL and ref as provenance and checks for upstream
  updates on a schedule.

### Checking for updates

```bash
memory blueprints check-updates            # status of every applied blueprint
memory blueprints check-updates crm        # trigger a check and print the result
memory blueprints update crm --wait        # apply the available update
```

Equivalent REST: `GET /api/blueprints/:name/updates`,
`POST /api/blueprints/:name/updates/check`,
`POST /api/blueprints/:name/updates/apply`.

---

## CLI reference

All blueprint commands resolve the project like the rest of the CLI
(`--project`, or `MEMORY_PROJECT` / `MEMORY_PROJECT_ID` from `.env.local`).

| Command | Purpose |
|---|---|
| `memory blueprints install <source>` | Apply a directory or GitHub repo |
| `memory blueprints inspect <source>` | List contents without installing |
| `memory blueprints validate <source>` | Offline structural + cross-reference validation |
| `memory blueprints dump <dir>` | Export a project's objects/relationships as JSONL seed files |
| `memory blueprints check-updates [name]` | Check GitHub-imported blueprints for newer upstream versions |
| `memory blueprints update <name>` | Apply an available update |
| `memory blueprints github-token set` | Store the project's GitHub PAT for private repos |

### `install` flags

| Flag | Description |
|---|---|
| `--project <id>` | Target project ID or name |
| `--upgrade` | Update existing resources instead of skipping them |
| `--dry-run` | Preview actions without writing anything (it still reads project context) |
| `--token <tok>` | GitHub PAT for private repos (also `MEMORY_GITHUB_TOKEN`) |
| `--map-model agent=provider/model` | Map a blueprint agent to a project model (repeatable) |
| `--no-map` | Skip install-time model mapping; keep the blueprint's pinned models |

### `dump` flags

| Flag | Description |
|---|---|
| `--project <id>` | Source project ID or name |
| `--types <list>` | Comma-separated object/relationship type filter (default: all) |

`validate` exits `0` on clean or warnings-only, `1` on any error, and is fully
offline — always run it before `install`. `dump` exports graph data only; it
does not export packs or agent definitions.

### Importing a dump into a branch

`dump` writes `seed/objects/<Type>.jsonl` and `seed/relationships/<Type>.jsonl`
with object keys and `srcKey`/`dstKey` references. To review a migration on a
branch before promoting it, feed the dump directory straight back to the graph
importer:

```bash
memory blueprints dump ./migration
# edit ./migration/seed/...
memory graph objects create-batch --file ./migration --branch migration-review
```

The importer upserts objects by `key`, resolves relationship keys after the
objects are written, reads `_untyped.jsonl` back as type `Untyped`, and reports
unreadable rows individually without aborting the rest. It accepts the dump
root, a `seed/` directory, or a single `.jsonl` file. See
[Branches § Bulk importing a dump into a branch](branches.md#bulk-importing-a-dump-into-a-branch).

---

## Registry commands (MCP and REST)

The registry lifecycle — browse, draft, publish, version, apply, unapply, delete
— is available over MCP and the REST API. The CLI covers the directory workflow
and update checks, not registry browse/apply.

### MCP tools

| Tool | Scope | Purpose |
|---|---|---|
| `blueprint-list` | `schema:read` | Global built-ins plus your own private blueprints |
| `blueprint-get` | `schema:read` | One blueprint, including its manifest |
| `blueprint-versions` | `schema:read` | Every version of a name, newest first |
| `blueprint-list-applied` | `schema:read` | Blueprints currently applied to the project |
| `blueprint-create` | `schema:write` | Create a draft (project-private) |
| `blueprint-update` | `schema:write` | Edit a draft's description, author, manifest |
| `blueprint-publish` | `schema:write` | Freeze a draft, computing its checksum |
| `blueprint-new-version` | `schema:write` | Fork any readable blueprint into a new draft |
| `blueprint-apply` | `schema:write` | Materialize a blueprint into the project |
| `blueprint-unapply` | `schema:write` | Reverse a previous apply |

Creating a **global** blueprint (`project_id` NULL) requires `superadmin_full`;
project members can author project-private drafts.

> The REST API also exposes `DELETE /api/blueprints/:id` (delete an unapplied
> `user`/`import` blueprint) and `POST /api/blueprints/:id/deprecate`, plus the
> GitHub import, update-check, model-override, and GitHub-token endpoints. Those
> have no MCP tool equivalent yet.

### REST endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/blueprints` | List visible blueprints (optional `?name=`) |
| `POST` | `/api/blueprints` | Create a draft |
| `GET` | `/api/blueprints/:id` | Get one blueprint |
| `PUT` | `/api/blueprints/:id` | Update a draft |
| `DELETE` | `/api/blueprints/:id` | Delete an unapplied `user`/`import` blueprint |
| `POST` | `/api/blueprints/:id/publish` | Publish a draft |
| `POST` | `/api/blueprints/:id/deprecate` | Mark published as deprecated |
| `POST` | `/api/blueprints/:id/versions` | Fork into a new draft version |
| `GET` | `/api/blueprints/name/:name/versions` | Versions by name |
| `POST` | `/api/blueprints/:id/apply` | Apply to the project |
| `POST` | `/api/blueprints/:id/unapply` | Unapply from the project |
| `GET` | `/api/blueprints/applied` | Applied blueprints |
| `POST` | `/api/blueprints/import` | Import from a GitHub repo |
| `POST` | `/api/blueprints/import/preview` | Preview an import |
| `GET` | `/api/blueprints/:name/updates` | Update status |
| `POST` | `/api/blueprints/:name/updates/check` | Check upstream |
| `POST` | `/api/blueprints/:name/updates/apply` | Apply the upstream update |
| `GET`/`PUT` | `/api/blueprints/model-overrides` | Per-project agent model mapping |
| `PUT` | `/api/blueprints/github-credentials` | Store the project's GitHub PAT |

---

## Schema packs and provenance

Applying a blueprint is what makes a **schema pack** active in a project. The
Schema area's **Schema packs** page (`/schema/packs`) lists each active pack with
its types, an owner badge (**Global** for a shared built-in pack, **Project** for
a project-owned one), and the applied blueprint that claims it.

For a given project, `GET /api/schemas/projects/:projectId/pack-claims` returns
the packs claimed by at least one applied blueprint. Unapplied and superseded
blueprints are omitted.

See [Schema § Schema packs](../developer-guide/schema.md#schema-packs) for the pack view and
[Schema Registry](../developer-guide/schema-registry.md) for the type-level model.

---

## Notes and gotchas

- **Packs are always assigned on apply**, even when the pack row already existed.
  That is what registers the types in the target project.
- **Set a `key` on every seed object.** In the registry apply path (MCP, web UI,
  import-and-apply) an object with no `key` is **skipped** with a warning —
  keyless duplicates on re-apply are worse than a skip. The CLI's directory
  install is the opposite: it creates keyless objects on every run, so a
  re-install duplicates them.
- **Use `srcKey`/`dstKey` in relationships**, not raw entity IDs — IDs do not
  survive an apply into another project.
- **Published means immutable.** Correcting a published version requires
  `new-version`; re-publishing is refused.
- **Unapply is scoped.** Skills and seed data stay; only agents and pack
  assignments owned by that blueprint are removed.
- **Global writes need `superadmin_full`.** Project members can only create
  project-private (`user`) blueprints.
