## 1. CLI Flag Implementation

- [x] 1.1 Add `--filter` (repeatable `[]string`) and `--filter-op` (string, default `"eq"`) flags to `graphObjectsListCmd` in `tools/cli/internal/cmd/graph.go`
- [x] 1.2 Add `graphFilterFlag []string` and `graphFilterOpFlag string` package-level flag vars alongside the existing flag vars
- [x] 1.3 Write `parsePropertyFilters(filters []string, op string) ([]sdkgraph.PropertyFilter, error)` helper that splits on first `=`, validates op, handles `in` comma-split and `exists` (value-less)
- [x] 1.4 Call `parsePropertyFilters` inside `graphObjectsListCmd.RunE` and assign result to `opts.PropertyFilters`
- [x] 1.5 Return a user-friendly error (non-zero exit) when `--filter` format is invalid or `--filter-op` is unrecognised

## 2. Tests

- [x] 2.1 Add unit tests for `parsePropertyFilters` covering: single eq, multiple AND, missing `=` error, `gte` operator, `contains` operator, `exists` (value ignored), `in` comma-split, unsupported operator error
- [x] 2.2 Verify `go test ./tools/cli/...` passes

## 3. Help Text & Docs

- [x] 3.1 Update `graphObjectsListCmd.Long` description to mention `--filter` and `--filter-op` with examples
- [x] 3.2 Update `tools/cli/internal/skillsfs/skills/memory-cli-reference/SKILL.md` — add `--filter` and `--filter-op` entries to the `memory graph objects list` flags table

## 4. Build Verification

- [x] 4.1 Run `task build` from repo root and confirm no compile errors
- [x] 4.2 Run `task cli:install` and smoke-test `memory graph objects list --filter status=active` against a running server
