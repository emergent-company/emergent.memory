## Why

Backup and restore are server-API-only. GitHub issue #592 surfaced the gap: a user who wants to create, download, import, or clone-restore a project backup has no CLI surface — only the gateway web UI and raw HTTP calls. The SDK already models most domains as a typed client package; backups has no SDK package and no `memory` subcommand.

## What Changes

- Add an SDK `backups` package (`apps/server/pkg/sdk/backups`) with typed `Backup` / `Restore` models and a client covering: create, list, get, download (streamed), delete, import (streamed multipart), clone-restore create, and restore status.
- Add `memory backups` and `memory restores` command groups to the CLI, registered under the `account` group.
- `memory backups` subcommands: `create` (with optional `--wait` polling), `list`, `get`, `download`, `delete`, `import`.
- `memory restores` subcommands: `create` (clone restore, with optional `--wait` polling), `get`.
- Stream the import archive and download responses rather than buffering them in memory (server import cap is 1 GiB).
- Reuse existing CLI helpers for org/project resolution, output formats (`table|json|yaml|csv`), and table rendering.
- Fix the server's `ListBackups` handler to actually decode the `cursor` query parameter (base64url JSON), so the CLI's `--cursor` pagination round-trips instead of being silently discarded.

## Capabilities

### New Capabilities

- `cli-backups`: the `memory backups` / `memory restores` CLI surface (create/list/get/download/delete/import + clone-restore create/get, org scoping, and status polling semantics).

### Modified Capabilities

- `backup-restore`: the `ListBackups` handler now decodes the `cursor` query parameter (base64url JSON) and returns a 400 on an invalid cursor; the response `nextCursor` object shape is unchanged.

## Impact

- `apps/server/pkg/sdk/backups/`: new `types.go`, `client.go`, `client_test.go`.
- `apps/server/pkg/sdk/sdk.go`: register the `Backups` sub-client.
- `apps/cli/internal/cmd/`: new `backups.go`, `restores.go`, `backups_test.go`.
- `apps/cli/internal/skillsfs/skills/memory-cli-reference/SKILL.md`: regenerated reference.
- `apps/server/domain/backups/handler.go`: decode the `cursor` query parameter (`ParseCursor`).
- No `.templ` files, no new server routes, no migrations.

## Notes

- The server's clone-restore route forces clone mode (`mode` in the body is ignored), so the CLI does not expose a `--mode` flag.
- The runlog CLI e2e for the backup/restore round-trip belongs in the private `emergent-company/emergent.memory.e2e` repo (separate follow-up; not part of this PR).
