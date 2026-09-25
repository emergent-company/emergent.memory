## Why

The `memory` CLI has no commands for managing extraction jobs. Tests must fall back to raw HTTP calls against the admin API to create, poll, and inspect jobs — making it impossible to write CLI-first e2e tests and leaving operators without tooling to monitor or control the extraction pipeline.

## What Changes

- Add `memory extraction jobs` command group to the CLI (new top-level `extraction` command with `jobs` subgroup)
- Add subcommands: `create`, `get`, `list`, `cancel`, `retry`, `logs`, `stats`
- Replace raw HTTP helpers in e2e tests (`triggerExtractionJob`, `waitForExtractionJob`) with CLI-driven equivalents using `rl.CLI()`
- New e2e test `TestCLIInstalled_DocumentConversion` added alongside the extraction test, using a PDF fixture to exercise Kreuzberg

## Capabilities

### New Capabilities

- `extraction-jobs-cli`: Full CLI management of extraction jobs — create, get, list, cancel, retry, logs, stats — backed by the existing `/api/admin/extraction-jobs` server API

### Modified Capabilities

- `module-extraction`: The e2e test for document extraction with schema (`TestCLIInstalled_DocumentExtractionWithSchema`) changes from using raw HTTP helpers to using the new `memory extraction jobs` CLI commands

## Impact

- **CLI source**: `apps/cli/internal/cmd/` — new file `extraction.go`; registered in `root.go`
- **Server API**: No changes — all endpoints already exist under `/api/admin/extraction-jobs`
- **SDK**: May need a thin extraction jobs client in `apps/server/pkg/sdk/` (or call raw HTTP in the CLI, same as other admin commands)
- **E2e tests**: `tests/cli/documents_test.go` — `triggerExtractionJob()` and `waitForExtractionJob()` replaced with CLI wrappers; new `TestCLIInstalled_DocumentConversion` test added
- **Test fixtures**: `tests/cli/testdata/` — small PDF fixture added for conversion test
