# Schemas

Schemas are versioned bundles of object type schemas, relationship type schemas, UI configurations, and extraction prompts. They let you define a domain model once and apply it to many projects.

## Concepts

| Concept | Description |
|---|---|
| **Schema** | A versioned bundle: type schemas, relationship schemas, UI configs, extraction prompts |
| **Assignment** | A link between a schema and a project |
| **Compiled types** | The merged schema registry view for a project, combining all active schemas |

---

## Schema lifecycle

```
draft → published → (deprecated)
```

- **Draft** schemas are not visible to projects.
- **Published** schemas appear in the `available` list for all projects.
- **Deprecated** schemas remain installed on existing projects but cannot be newly assigned.

---

## Managing schemas (admin)

### Create a schema

```bash
curl -X POST https://api.dev.emergent-company.ai/api/schemas \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "legal-entities",
    "version": "1.0.0",
    "description": "Object types for legal entity extraction",
    "author": "Emergent Company",
    "source": "official",
    "license": "MIT",
    "draft": false,
    "objectTypeSchemas": {
      "Contract": {
        "type": "object",
        "properties": {
          "title":       { "type": "string" },
          "parties":     { "type": "array", "items": { "type": "string" } },
          "signedDate":  { "type": "string", "format": "date" },
          "jurisdiction":{ "type": "string" }
        },
        "required": ["title"]
      },
      "Clause": {
        "type": "object",
        "properties": {
          "clauseType": { "type": "string" },
          "text":       { "type": "string" }
        }
      }
    },
    "relationshipTypeSchemas": {
      "CONTAINS_CLAUSE": {
        "from": "Contract",
        "to": "Clause"
      }
    },
    "uiConfigs": {
      "Contract": { "icon": "file-text", "color": "#0ea5e9", "displayProperty": "title" },
      "Clause":    { "icon": "paragraph", "color": "#8b5cf6", "displayProperty": "clauseType" }
    },
    "extractionPrompts": {
      "Contract": {
        "systemPrompt": "Extract contract entities from the document.",
        "exampleJson": "{\"title\": \"Service Agreement\", \"parties\": [\"Acme\", \"Widgets Inc.\"]}"
      }
    }
  }'
```

### Get a schema

```bash
curl https://api.dev.emergent-company.ai/api/schemas/<schemaId> \
  -H "Authorization: Bearer <token>"
```

### Update a schema

```bash
curl -X PUT https://api.dev.emergent-company.ai/api/schemas/<schemaId> \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"version": "1.0.1", "description": "Updated description"}'
```

### Delete a schema

```bash
curl -X DELETE https://api.dev.emergent-company.ai/api/schemas/<schemaId> \
  -H "Authorization: Bearer <token>"
```

!!! warning "In-use schemas"
    Deleting a schema that is currently assigned to any project returns `409 Conflict`. Remove all assignments first.

---

## Schema field reference

| Field | Type | Description |
|---|---|---|
| `name` | string | Unique identifier name |
| `version` | string | Semver string, e.g. `1.0.0` |
| `description` | string | Human-readable description |
| `author` | string | Schema author |
| `source` | string | `official`, `community`, or custom label |
| `license` | string | SPDX license ID, e.g. `MIT` |
| `repositoryUrl` | string | Source repository URL |
| `documentationUrl` | string | Docs URL |
| `objectTypeSchemas` | object | Map of type name → JSON Schema |
| `relationshipTypeSchemas` | object | Map of relationship type → schema |
| `uiConfigs` | object | Map of type name → UI config |
| `extractionPrompts` | object | Map of type name → prompt config |
| `migrations` | object | Optional migration hints block (see [Schema migrations](#schema-migrations)) |
| `checksum` | string | SHA-256 of canonical content (auto-computed) |
| `draft` | bool | `true` = not visible to projects |
| `publishedAt` | timestamp | When the schema was published |
| `deprecatedAt` | timestamp | When the schema was deprecated |

---

## Assigning schemas to projects

### Browse available schemas

```bash
curl https://api.dev.emergent-company.ai/api/schemas/projects/<projectId>/available \
  -H "Authorization: Bearer <token>"
```

### List installed schemas

```bash
curl https://api.dev.emergent-company.ai/api/schemas/projects/<projectId>/installed \
  -H "Authorization: Bearer <token>"
```

### Assign a schema

```bash
curl -X POST https://api.dev.emergent-company.ai/api/schemas/projects/<projectId>/assign \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"schemaId": "<schemaId>"}'
```

This creates an assignment with `active: true` by default.

### Enable / disable an assignment

```bash
curl -X PATCH https://api.dev.emergent-company.ai/api/schemas/projects/<projectId>/assignments/<assignmentId> \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"active": false}'
```

Inactive assignments do not contribute types to the compiled registry.

### Remove an assignment

```bash
curl -X DELETE https://api.dev.emergent-company.ai/api/schemas/projects/<projectId>/assignments/<assignmentId> \
  -H "Authorization: Bearer <token>"
```

---

## Compiled types

The compiled types endpoint merges all **active** schema assignments for a project into a single flat type map. Later-assigned schemas override earlier ones for the same type name.

```bash
curl https://api.dev.emergent-company.ai/api/schemas/projects/<projectId>/compiled-types \
  -H "Authorization: Bearer <token>"
```

This is the schema registry the extraction pipeline uses. Types registered here that are not yet in the project's schema registry table are automatically added with `source = "template"` on the next extraction run.

---

## Schema history

The history endpoint returns **all** schema assignments for a project including ones that have been uninstalled (soft-deleted). Useful for auditing which schemas were used over time.

```bash
curl https://api.dev.emergent-company.ai/api/schemas/projects/<projectId>/history \
  -H "Authorization: Bearer <token>"
```

Each item includes `status: "installed"` or `status: "uninstalled"` and an optional `removed_at` timestamp.

---

## Schema migrations

Schemas can carry an optional `migrations` block that declares how to migrate live graph data when upgrading from a previous version. The server uses this block to run an **async background migration job** automatically when you install the new schema version.

### migrations block (YAML)

```yaml
name: legal-entities
version: "2.0.0"
description: Legal entity schema v2

objectTypeSchemas:
  Agreement:
    type: object
    properties:
      title:      { type: string }
      parties:    { type: array, items: { type: string } }
      signed_at:  { type: string, format: date }      # renamed from signedDate
      jurisdiction: { type: string }
    required: [title]

# Optional migrations block — describes what changed since from_version
migrations:
  from_version: "1.0.0"         # previous version this schema upgrades from
  type_renames:
    - from: Contract
      to: Agreement
  property_renames:
    - type_name: Agreement
      from: signedDate
      to: signed_at
  removed_properties:
    - type_name: Agreement
      name: legacyId
```

| Field | Description |
|---|---|
| `from_version` | **Required.** The schema version this migration upgrades from. Must match an installed schema with that name and version. |
| `type_renames` | List of `{from, to}` type-name renames to apply to live data. |
| `property_renames` | List of `{type_name, from, to}` property-key renames. |
| `removed_properties` | List of `{type_name, name}` properties that are intentionally removed. Data is archived, not warned. |

### How auto-migration works

1. You install the new schema version (`memory schemas install --file schema-v2.yaml`).
2. If the schema has a `migrations.from_version` block, the server resolves a **migration chain** (walking backwards through `from_version` pointers, up to 10 hops).
3. A preview runs synchronously on a sample of objects to assess risk (`safe` / `cautious` / `risky` / `dangerous`).
4. If risk is `dangerous` and `--force` is not set, the assign returns `migration_status: "blocked"` — no job is enqueued.
5. Otherwise an async background job is created. The job ID is returned in the assign response.
6. The worker executes each hop sequentially, updating `objects_migrated` / `objects_failed` as it goes.

**Risk levels:**

| Level | Meaning |
|---|---|
| `safe` | No data loss; only renames and additions. |
| `cautious` | Minor type coercions that could lose precision. |
| `risky` | 1–2 properties will be dropped (not in `removed_properties`). Use `--force`. |
| `dangerous` | 3+ properties will be dropped. Use `--force`. |

### Migrating types (System B — SQL renames)

The original migrate endpoint performs direct SQL renames on graph data without schema-version tracking. Use this for quick, low-risk renames when you don't have schema version metadata:

```bash
curl -X POST https://api.dev.emergent-company.ai/api/schemas/projects/<projectId>/migrate \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "type_renames": [
      {"from": "OldType", "to": "NewType"}
    ],
    "property_renames": [
      {"type_name": "NewType", "from": "oldProp", "to": "newProp"}
    ],
    "dry_run": true
  }'
```

### Migration REST API (System A — async, schema-version aware)

#### Preview a migration

Runs migration logic on all matching objects in dry-run mode and returns aggregated risk results without making any changes.

```bash
curl -X POST https://api.dev.emergent-company.ai/api/schemas/projects/<projectId>/migrate/preview \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"from_schema_id": "<fromId>", "to_schema_id": "<toId>"}'
```

#### Execute a migration

```bash
curl -X POST https://api.dev.emergent-company.ai/api/schemas/projects/<projectId>/migrate/execute \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "from_schema_id": "<fromId>",
    "to_schema_id": "<toId>",
    "force": false,
    "max_objects": 0
  }'
```

#### Rollback a migration

Restores property data from `migration_archive`. Optionally re-installs the old schema types.

```bash
curl -X POST https://api.dev.emergent-company.ai/api/schemas/projects/<projectId>/migrate/rollback \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"to_version": "1.0.0", "restore_type_registry": false}'
```

#### Commit (prune) migration archive

Strips archive entries up to and including `through_version` from all project objects, freeing storage.

```bash
curl -X POST https://api.dev.emergent-company.ai/api/schemas/projects/<projectId>/migrate/commit \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"through_version": "1.0.0"}'
```

#### Get migration job status

```bash
curl https://api.dev.emergent-company.ai/api/schemas/projects/<projectId>/migration-jobs/<jobId> \
  -H "Authorization: Bearer <token>"
```

---

## CLI reference

The `memory schemas` subcommands cover all schema management operations.

### YAML support

Schema files can be written in **JSON or YAML**. The CLI detects format by file extension (`.yaml` / `.yml` for YAML, otherwise JSON):

```bash
memory schemas create --file my-schema.yaml --project <projectId>
memory schemas create --file my-schema.json --project <projectId>
```

### Diff two schemas

Compare two schema files (JSON or YAML) side-by-side without making any API calls:

```bash
memory schemas diff schema-v1.yaml schema-v2.yaml
```

The diff output includes a property-level breakdown (added/removed/type-changed properties per type) and a suggested `migrations:` YAML block auto-populated from the detected differences. Copy this block into your new schema file as a starting point.

### Install a schema (with auto-migration)

When the schema file contains a `migrations` block, the server automatically enqueues a background migration job after the assignment is created.

```bash
# Install and auto-migrate (job ID printed; TTY streams progress)
memory schemas install --file schema-v2.yaml --project <projectId>

# Force migration even if risk level is dangerous
memory schemas install --file schema-v2.yaml --project <projectId> --force

# Uninstall the from-version schema automatically after the migration chain completes
memory schemas install --file schema-v2.yaml --project <projectId> --auto-uninstall

# Dry run: preview migration without installing
memory schemas install --file schema-v2.yaml --project <projectId> --dry-run
```

If a migration job is started and stdout is a TTY, the CLI automatically polls and streams progress until the job completes. In non-TTY mode it prints:
```
Migration job started: <jobId>
Run: memory schemas migrate job --job-id <jobId> --wait
```



Show the merged type registry for a project:

```bash
memory schemas compiled-types --project <projectId>

# Include schema source metadata and shadow warnings
memory schemas compiled-types --project <projectId> --verbose
```

`--verbose` adds `schema_id`, `schema_name`, `schema_version`, and a `shadowed` flag (true when two installed schemas define the same type name).

### Schema history

List all schema assignments including uninstalled ones:

```bash
memory schemas history --project <projectId>
```

### Migrate types

#### System B — SQL renames (no version tracking)

Rename types or property keys across live data in a single transaction:

```bash
# Dry run (preview only)
memory schemas migrate --project <projectId> \
  --rename-type OldType=NewType \
  --rename-prop "NewType.oldProp=newProp" \
  --dry-run

# Apply
memory schemas migrate --project <projectId> \
  --rename-type OldType=NewType \
  --rename-prop "NewType.oldProp=newProp"
```

#### System A — Schema-version-aware migration subcommands

Preview risk before migrating:

```bash
memory schemas migrate preview \
  --project <projectId> \
  --from <fromSchemaId> \
  --to <toSchemaId>
```

Execute a migration (with TTY confirmation for risky/dangerous):

```bash
memory schemas migrate execute \
  --project <projectId> \
  --from <fromSchemaId> \
  --to <toSchemaId> \
  --force            # skip confirmation for risky/dangerous
  --max-objects 500  # limit batch size (0 = no limit)
```

Rollback to a previous version:

```bash
memory schemas migrate rollback \
  --project <projectId> \
  --to-version 1.0.0 \
  --restore-registry  # also reinstall old schema types
```

Prune migration archive entries (commit):

```bash
memory schemas migrate commit \
  --project <projectId> \
  --through-version 1.0.0
```

Poll a background migration job:

```bash
# Check status once
memory schemas migrate job --project <projectId> --job-id <jobId>

# Block until complete, streaming progress
memory schemas migrate job --project <projectId> --job-id <jobId> --wait
```

### Bulk uninstall

```bash
# Uninstall all schemas except named ones (comma-separated)
memory schemas uninstall --project <projectId> --all-except "core,legal-entities"

# Keep only the most recent version of each schema name
memory schemas uninstall --project <projectId> --keep-latest

# Preview what would be uninstalled (no changes made)
memory schemas uninstall --project <projectId> --all-except "core" --dry-run
```

---

## MCP tools

The following MCP tools are available for schema management:

| Tool | Description |
|---|---|
| `schema-list-available` | Browse schemas available to install |
| `schema-list-installed` | List installed schemas (excludes uninstalled) |
| `schema-assign` | Install a schema into the project |
| `schema-assignment-update` | Enable/disable an assignment |
| `schema-uninstall` | Uninstall a schema assignment |
| `schema-create` | Create a new schema in the registry |
| `schema-delete` | Delete a schema from the registry |
| `schema-history` | Full installation history including uninstalled schemas |
| `schema-compiled-types` | Merged type registry; use `verbose: true` for shadow metadata |
| `schema-migrate-preview` | Preview migration risk across all objects (dry-run) |
| `schema-migrate-execute` | Execute a schema-version-aware migration |
| `schema-migrate-rollback` | Rollback live data to a previous schema version |
| `schema-migrate-commit` | Prune migration archive entries up to a given version |
| `schema-migration-job-status` | Get the status of an async migration job |

---

## Blueprints

You can also install schemas via the CLI blueprints workflow:

```bash
memory blueprints ./my-schema-dir --project <projectId>
```

See the [Agents — Blueprints](../user-guide/agents.md#blueprints-gitops) section for the blueprint file format. Schema blueprints use the same YAML-based declarative config.
