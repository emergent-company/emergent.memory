## Why

Schema iteration is painful today: the `--file` install path silently drops type definitions (now fixed), YAML files require manual conversion to JSON, there is no way to preview changes before installing, and cleaning up old schema versions requires tedious per-assignment commands. These gaps create real friction for users who are actively evolving schemas across many iterations.

## What Changes

- **YAML/JSON auto-detect** in `memory schemas install --file` and `memory schemas create --file` — accept `.yaml`/`.yml` natively, no manual conversion step required.
- **`memory schemas diff`** — new subcommand that computes and prints a human-readable diff between an installed schema and an incoming file, including affected-object counts per changed type.
- **`memory schemas uninstall --all-except <id>`** — new flag to remove all assignments except one in a single command; `--keep-latest` variant keeps only the most recently installed assignment.
- **`memory schemas compiled-types --verbose`** — augments the existing command to show schema version alongside each type and warn about shadowed types.
- **`memory schemas history`** — new subcommand that shows all schema assignments including previously removed ones; requires soft-delete migration.
- **`memory schemas migrate`** — new subcommand to migrate live object/relationship data when schema type names or property names change between versions.

## Capabilities

### New Capabilities

- `schema-yaml-support`: Accept YAML files natively in `--file` flags across schema commands.
- `schema-diff`: Preview the difference between an installed schema version and an incoming file before installing.
- `schema-bulk-uninstall`: Remove all-but-one schema assignments in a single command.
- `schema-compiled-types-verbose`: Show schema version provenance and shadow warnings in compiled-types output.
- `schema-history`: Soft-delete assignments and expose full assignment history via CLI.
- `schema-migrate`: Migrate live graph data (objects + relationships) when schema type/property names change.

### Modified Capabilities

## Impact

- **CLI**: `tools/cli/internal/cmd/schemas.go` — new subcommands (`diff`, `history`, `migrate`), new flags (`--all-except`, `--keep-latest` on uninstall; `--verbose` on compiled-types), YAML detection in file-reading helpers.
- **Server handlers**: `apps/server/domain/schemas/handler.go` — new endpoints for diff, history list, migrate.
- **Server service + repository**: `apps/server/domain/schemas/service.go`, `repository.go` — diff computation, soft-delete, history query, migrate transaction.
- **Server entity**: `apps/server/domain/schemas/entity.go` — new request/response types for diff, migrate, history.
- **SDK client**: `apps/server/pkg/sdk/schemas/client.go` — new methods for diff, history, migrate.
- **DB migration**: soft-delete `removed_at` column on `kb.project_schemas`; no destructive changes to existing data.
- **Dependencies**: `gopkg.in/yaml.v3` already present in `tools/cli/go.mod`.
