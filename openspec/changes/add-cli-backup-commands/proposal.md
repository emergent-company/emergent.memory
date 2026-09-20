## Why

Backup and restore are server-API-only. GitHub issue #592 surfaced the gap: a user who wants to create, download, import, or clone-restore a project backup has no CLI surface — only the gateway web UI and raw HTTP calls. The SDK already models most domains as a typed client package; backups has no SDK package and no `memory` subcommand.

## What Changes

- Add an SDK `backups` package (`apps/server/pkg/sdk/backups`) with typed `Backup` / `Restore` models and a client covering: create, list, get, download (streamed), delete, import (streamed multipart), clone-restore create, and restore status.
- Add `memory backups` and `memory restores` command groups to the CLI, registered under the `account` group.
- `memory backups` subcommands: `create` (with optional `--wait` polling), `list`, `get`, `download`, `delete`, `import`.
- `memory restores` subcommands: `create` (clone restore, with optional `--wait` polling), `get`.
- Stream the import archive and download responses rather than buffering them in memory (server import cap is 1 GiB).
- Reuse existing CLI helpers for org/project resolution, output formats (`table|json|yaml|csv`), and table rendering.

## Capabilities

### New Capabilities

- `cli-backups`: the `memory backups` / `memory restores` CLI surface (create/list/get/download/delete/import + clone-restore create/get, org scoping, and status polling semantics).

### Modified Capabilities

<!-- none — the server backup/restore pipeline is unchanged and already covered by `backup-restore`. -->

## Impact

- `apps/server/pkg/sdk/backups/`: new `types.go`, `client.go`, `client_test.go`.
- `apps/server/pkg/sdk/sdk.go`: register the `Backups` sub-client.
- `apps/cli/internal/cmd/`: new `backups.go`, `restores.go`, `backups_test.go`.
- `apps/cli/internal/skillsfs/skills/memory-cli-reference/SKILL.md`: regenerated reference.
- No `.templ` files, no server routes, no migrations.

## Notes

- The server's clone-restore route forces clone mode (`mode` in the body is ignored), so the CLI does not expose a `--mode` flag.
- The runlog CLI e2e for the backup/restore round-trip belongs in the private `emergent-company/emergent.memory.e2e` repo (separate follow-up; not part of this PR).
