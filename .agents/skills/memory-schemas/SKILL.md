---
name: memory-schemas
description: Install, list, or remove schemas (object and relationship type definitions) in an existing project. Use after onboarding when managing or updating the type registry.
metadata:
  author: emergent
  version: "2.1"
---

Manage schemas using `memory schemas`. Schemas define reusable sets of object types and relationship types that can be installed into a project's knowledge graph schema.

> **New to Emergent?** Load the `memory-onboard` skill first — it walks through designing and installing a schema from scratch.

## Rules

- **Project context is auto-discovered** — the CLI walks up the directory tree to find `.env.local` containing `MEMORY_PROJECT` or `MEMORY_PROJECT_ID`. If `.env.local` is present anywhere above the current directory, `--project` is not needed. Only pass `--project <id>` explicitly when overriding or when no `.env.local` exists.

## Concepts

- **Schema** — a versioned bundle of `objectTypeSchemas` and `relationshipTypeSchemas`. Immutable once created; new versions get new IDs.
- **Installed schema** — a schema assigned to a specific project. Multiple schemas can be installed; their types are merged into the project's compiled type registry.
- **Compiled types** — the merged view of all object + relationship types from all installed schemas in a project.

---

## Commands

### List schemas on this project
```bash
memory schemas list
memory schemas list --output json
```
Shows schemas currently installed on the project — this is the default and what you almost always want. Schemas installed via `memory blueprints install` appear here.

To see schemas available in the global registry (not yet installed):
```bash
memory schemas list --available
```
> **Note:** On most self-hosted installs the registry is empty. Schemas come from blueprints, not the registry.

### List installed schemas (alias)
```bash
memory schemas installed
memory schemas installed --output json
```

### Get schema details
```bash
memory schemas get <schema-id>
```
Shows object types, relationship types, version, description.

### Create a new schema

Both JSON and YAML files are accepted:
```bash
memory schemas create --file pack.json
memory schemas create --file pack.yaml
```

Schema file structure (JSON or YAML):
```yaml
name: my-pack
version: "1.0"
description: Object types for my domain
objectTypeSchemas:
  - name: Requirement
    label: Requirement
    description: A product requirement
    properties: {}
relationshipTypeSchemas:
  - name: implements
    label: Implements
    fromTypes: [Task]
    toTypes: [Requirement]
```

### Install a schema into the current project
```bash
# Install an existing schema by ID:
memory schemas install <schema-id>

# Create from JSON or YAML file and install in one step:
memory schemas install --file pack.json
memory schemas install --file pack.yaml

# Preview without making changes:
memory schemas install --file pack.json --dry-run

# Merge into existing registered types:
memory schemas install --file pack.json --merge
```

### Preview schema changes before installing (diff)
```bash
# Compare an installed schema against a new file before upgrading:
memory schemas diff <schema-id> --file new-version.json
memory schemas diff <schema-id> --file new-version.yaml
memory schemas diff <schema-id> --file new-version.json --output json
```
Shows added, removed, and modified object/relationship types. Use this before every upgrade to understand the impact on live data.

### Uninstall a schema from the current project
```bash
# Uninstall a single schema by assignment ID:
memory schemas uninstall <assignment-id>

# Uninstall all schemas except one (keep only the specified assignment):
memory schemas uninstall --all-except <assignment-id>

# Keep only the latest version of each schema name:
memory schemas uninstall --keep-latest

# Preview what would be uninstalled without making changes:
memory schemas uninstall --keep-latest --dry-run
memory schemas uninstall --all-except <id> --dry-run
```
Use `memory schemas installed` to find assignment IDs.

### Delete a schema from the registry
```bash
memory schemas delete <schema-id>
```

### View compiled types (merged schema)
```bash
memory schemas compiled-types
memory schemas compiled-types --output json

# Show which schema version each type came from, and warn about shadows:
memory schemas compiled-types --verbose
```
`--verbose` adds `schemaVersion` provenance per type and prints stderr warnings when the same type name appears in multiple installed schemas (shadowing).

### View schema installation history
```bash
memory schemas history
memory schemas history --output json
```
Shows all schema assignments ever made on the project, including uninstalled ones (with their removal date). Useful for auditing schema lineage.

### Migrate live graph data

Two migration systems are available:

#### System B — SQL renames (fast, no version tracking)
```bash
# Rename an object/edge type across all live graph objects:
memory schemas migrate --rename-type OldType:NewType

# Rename a property key within a type:
memory schemas migrate --rename-property OldType.old_key:OldType.new_key

# Multiple renames in one transaction:
memory schemas migrate \
  --rename-type handles_route:handles \
  --rename-property Method.endpoint:Method.route

# Preview affected row counts without making changes:
memory schemas migrate --rename-type OldType:NewType --dry-run
```
Runs in a single database transaction. Zero-row results warn you to check the type/property name spelling.

#### System A — Schema-version-aware migration (async, archives dropped data)

Preview risk without making changes:
```bash
memory schemas migrate preview \
  --project <projectId> \
  --from <fromSchemaId> \
  --to <toSchemaId>
```

Execute migration (prompts for confirmation when risky/dangerous on a TTY):
```bash
memory schemas migrate execute \
  --project <projectId> \
  --from <fromSchemaId> \
  --to <toSchemaId> \
  [--force]            # skip confirmation for risky/dangerous
  [--max-objects 500]  # limit batch (0 = no limit)
```

Rollback to a prior schema version:
```bash
memory schemas migrate rollback \
  --project <projectId> \
  --to-version 1.0.0 \
  [--restore-registry]  # also reinstall old schema types
```

Prune migration archive entries (commit):
```bash
memory schemas migrate commit \
  --project <projectId> \
  --through-version 1.0.0
```

Poll a background migration job:
```bash
memory schemas migrate job --project <projectId> --job-id <jobId>
memory schemas migrate job --project <projectId> --job-id <jobId> --wait  # block until done
```

---

## MCP tools

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
| `schema-migrate-preview` | Preview migration risk across all objects (dry-run, no changes) |
| `schema-migrate-execute` | Execute a schema-version-aware object migration |
| `schema-migrate-rollback` | Rollback live data to a previous schema version from archive |
| `schema-migrate-commit` | Prune migration archive entries up to a given version |
| `schema-migration-job-status` | Get the status of an async background migration job |

---

## Workflow: Upgrading a schema (with auto-migration)

1. **Edit** your schema file (JSON or YAML), adding a `migrations:` block with `from_version` and any renames
2. **Preview** property-level diffs: `memory schemas diff <old-schema-id> --file new.yaml` — the output includes a suggested `migrations:` YAML block
3. **Install** the new version: `memory schemas install --file new.yaml --dry-run` then without `--dry-run`
   - If the `migrations` block is present, a background migration job starts automatically
   - Add `--force` to allow risky/dangerous migrations; `--auto-uninstall` to remove the old schema after migration completes
4. **Monitor** the job: `memory schemas migrate job --job-id <jobId> --wait`
5. **Clean up** old assignments (if not using `--auto-uninstall`): `memory schemas uninstall --all-except <new-assignment-id>`
6. **Prune** migration archive when satisfied: `memory schemas migrate commit --through-version 1.0.0`
7. **Verify**: `memory schemas compiled-types --verbose`

## Workflow: Initial setup

1. **Set up a project schema**: `list` to find existing schemas → `install <schema-id>` to add to project → `compiled-types` to verify
2. **Create a custom schema**: write a YAML/JSON file → `install --file pack.yaml --dry-run` to preview → `install --file pack.yaml` to create and install
3. **Inspect project schema**: `compiled-types` to see all available types before creating objects
4. **Remove a schema**: `uninstall <assignment-id>` — use `installed` to find the assignment ID first

## Notes

- Schema IDs are UUIDs; use `list --output json` to find by name
- Schemas are immutable — creating a schema with the same name but different content creates a new version with a new ID
- `--project` global flag selects the project for `installed`, `install`, `uninstall`, `compiled-types`, `history`, `migrate`, and `diff`
- `list` and `create` are org-scoped (no project needed)
- Both `camelCase` (`objectTypeSchemas`) and `snake_case` (`object_type_schemas`) field names are accepted in schema files
