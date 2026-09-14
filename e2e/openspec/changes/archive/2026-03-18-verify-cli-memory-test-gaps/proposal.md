## Why

The CLI has grown to 35+ commands but several were added or changed without corresponding e2e tests — including the `browse` TUI, `upgrade`, `server ctl/install/uninstall/upgrade`, `memory status --json`, and `config set`. The runlog v0.1.2 upgrade also embedded skills directly into the binary, creating a new code path in `install-memory-skills` that has no dedicated coverage for its `--dir` and idempotency behaviors.

## What Changes

- Add dedicated e2e tests for `memory browse` (no-TTY graceful exit, --help integrity)
- Add dedicated e2e tests for `memory upgrade` (help flags, `--force` with already-latest version, error output)
- Add e2e tests for untested `memory server` subcommands: `ctl status`, `ctl stop/start` (smoke), `upgrade --help`, `uninstall --help`
- Add `memory status --json` output format test (JSON schema check for CLI/server version, auth mode, project count)
- Add `memory config set <key> <value>` test for the generic setter subcommand
- Add `--output csv` smoke test for at least one tabular command (e.g. `memory projects list`)
- Expand `install-memory-skills` coverage: `--dir <custom-path>`, idempotency without `--force`, `--force` overwrite behavior

## Capabilities

### New Capabilities

- `cli-browse-smoke`: Tests that `memory browse` exits gracefully (non-zero) when stdin is not a TTY, and that `--help` output is well-formed
- `cli-upgrade-tests`: Tests for `memory upgrade` and `memory server upgrade` covering help flags, `--force` with a dev build, and expected output
- `cli-server-ctl-tests`: Smoke tests for `memory server ctl status` and help output for `server install/uninstall/upgrade`
- `cli-output-formats`: Tests for `--output csv` and `--output json` across key commands (`projects list`, `graph list`, `status`)
- `cli-install-skills-coverage`: Dedicated tests for `install-memory-skills --dir`, idempotency (no-force re-run), and `--force` overwrite

### Modified Capabilities

- `e2e-framework`: No spec-level changes (only new test helpers may be needed)

## Impact

- New test files in `tests/cli/`: `browse_test.go`, `upgrade_test.go`, `server_ctl_test.go`, `output_formats_test.go`, `install_skills_coverage_test.go`
- No server-side changes required — all gaps are client-side CLI behaviors
- `browse` and `upgrade` tests must not touch real TTY or replace the running binary; they test only help/error paths
