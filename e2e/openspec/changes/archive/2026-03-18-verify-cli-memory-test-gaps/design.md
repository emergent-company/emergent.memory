## Context

The e2e test suite covers ~35 CLI commands across 41 test files. An audit reveals five clusters of commands with zero or near-zero functional coverage: (1) `browse` TUI, (2) `upgrade` binary replacement, (3) `server` lifecycle subcommands beyond `doctor`, (4) `--output csv` format, and (5) `install-memory-skills` edge paths introduced when runlog v0.1.2 embedded skills directly into the binary. All existing CLI tests live in `tests/cli/` and share helpers from `helpers_test.go`; new tests follow the same package and runlog patterns.

## Goals / Non-Goals

**Goals:**
- Add functional e2e coverage for every CLI command that currently has none
- Test `--output csv` for tabular commands and `--output json` for `memory status`
- Test `install-memory-skills --dir`, idempotency (no `--force`), and `--force` overwrite with embedded v0.1.2 skills
- Keep all new tests hermetic: no real TTY allocation, no binary replacement, no server install/uninstall

**Non-Goals:**
- Do not test the interactive TUI internals of `memory browse` — only no-TTY exit behavior
- Do not actually run `memory upgrade` end-to-end (would replace the binary under test)
- Do not test `memory server install/uninstall` against a real system (destructive)
- Do not add performance or load tests

## Decisions

**Decision: Browse — test no-TTY exit, not TUI rendering**
`memory browse` requires an interactive terminal. In CI, stdin is not a TTY, so the command either errors immediately or falls through to a usage message. We assert a non-zero exit with a recognizable message rather than mocking a terminal.
_Alternative considered_: Use a pty library (e.g. `creack/pty`) to allocate a pseudo-terminal. Rejected: adds a dependency, increases flakiness, and TTY behavior is not the gap being closed.

**Decision: Upgrade — test --help and --force with dev build only**
Running `memory upgrade` replaces `/usr/local/bin/memory` (or wherever it lives) and would corrupt the test environment. We test: (a) `--help` output integrity, (b) `--force` against a dev build returns a non-destructive "already up to date" or a well-formed error without actually writing files.
_Alternative_: Test in a tmpdir with a fake binary. Rejected: tests the OS file-copy path, not CLI behavior.

**Decision: Server subcommands — help-flag and doctor-level tests only**
`server install/uninstall` would mutate the host system. `server ctl start/stop` requires a running installation. Tests cover `--help` output (subcommand presence, flag documentation) and `server doctor` (already exists). Adding `server ctl status` as a smoke test that exits 0 or a known error without side effects.
_Alternative_: Run server in a Docker container and test full lifecycle. Deferred as a separate initiative.

**Decision: CSV output — test `projects list --output csv`**
CSV is the least-tested output format. `projects list` is the simplest idempotent command with tabular output. One test asserting header row presence and comma-delimited data rows is sufficient.

**Decision: install-memory-skills — use t.TempDir() as --dir target**
Embedded skills (runlog v0.1.2) are written from binary, not filesystem. Tests use `t.TempDir()` for `--dir` so no real `.agents/skills/` is touched. Idempotency test: run twice without `--force`, assert second run does not error. Force test: write a dummy file into the skill dir, run with `--force`, assert it was replaced.

## Risks / Trade-offs

- [`memory upgrade --force` on a dev build] May behave differently across environments → Mitigation: skip if not a dev build; assert only that the command exits with a recognizable message, not that it downloaded anything.
- [`server ctl status` test] If no server is installed, the command returns an error; test must tolerate both "running" and "not installed" outputs → Mitigation: use `runCLI` (non-fatal) and assert only that the command exits without panic.
- [CSV output] Quoting rules vary by platform → Mitigation: assert header field names are present as substrings, not exact CSV row matching.

## Migration Plan

No schema or data migrations. New test files only:
1. `tests/cli/browse_test.go`
2. `tests/cli/upgrade_test.go`
3. `tests/cli/server_ctl_test.go`
4. `tests/cli/output_formats_test.go`
5. `tests/cli/install_skills_coverage_test.go`

Each file follows existing `package cli_test` conventions with `newRunLog(t)` and `mustRunCLI`/`runCLI` helpers.

## Open Questions

- Does `memory upgrade --force` on a dev build attempt a network download in CI? If so, the test must be guarded by `t.Skip` or a network-availability check.
- Is `memory server ctl status` safe to run on the test host (mcj-emergent)? It is a read-only status check but may output a wall of service info — confirm with ops before enabling by default.
