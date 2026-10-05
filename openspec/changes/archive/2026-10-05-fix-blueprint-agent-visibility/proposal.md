## Why

The CLI blueprint validator and the server disagree on the allowed agent `visibility` enum. `memory blueprints validate` accepts `public|private|system`, while the server's write path (`apps/server/domain/agents/entity.go`, `NormalizeVisibility`) accepts only `project|external|internal` and returns HTTP 400 otherwise. A blueprint that validates cleanly offline therefore fails at install time with a contradictory message (issue #1480). No spec documents the blueprint agent visibility enum, so the drift was unguarded.

## What Changes

- Align the CLI validator (`apps/cli/internal/blueprints/validator.go`) with the server: accept `project|external|internal` (and empty, which the server normalizes to `project`), reject `public|private|system`, and report the canonical set in the error message.
- Add unit coverage in `validator_test.go` asserting the accepted/rejected sets and the error message (visibility was previously untested).
- Document the blueprint agent `visibility` enum in the `blueprint-api` capability so the CLI and server sets stay pinned to one canonical definition.
- No server change: the server already validates the enum correctly; only the CLI copy was wrong.

## Impact

- `apps/cli/internal/blueprints/validator.go` — `validVisibilities` + error message.
- `apps/cli/internal/blueprints/validator_test.go` — visibility subtests.
- `openspec/specs/blueprint-api/spec.md` (via delta) — new requirement pinning the enum.
- No server, SDK, or blueprint-format change. The SDK exposes agent visibility only as a plain `string`, so the CLI set is hardcoded with a comment pointing at the server's authoritative constants.
