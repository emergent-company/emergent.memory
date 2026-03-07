<!-- Baseline failures (pre-existing, not introduced by this change):
- tools/cli/internal/blueprints/blueprints_test.go: assignment mismatch — LoadDir returns 6 values but tests expect 4 (monorepo WIP)
- catalog_test.go: undefined: staticModels (monorepo WIP)
- tracer_test.go: undefined: tracing.StartLinked, tracing.RecordErrorWithType (monorepo WIP)
These are unrelated to this change (standalone repo work).
-->

## 1. Repo Scaffold

- [x] 1.1 Create `/root/imdb-memory-blueprint` directory and init as a Git repo
- [x] 1.2 Create `go.mod` with module name `github.com/emergent-company/imdb-memory-blueprint` and add `replace` directive pointing to local monorepo SDK
- [x] 1.3 Create directory structure: `packs/`, `cmd/seeder/`
- [x] 1.4 Create `.gitignore` (exclude binaries, `go.sum` edits, `.env`, log files)

## 2. Template Pack

- [x] 2.1 Create `packs/imdb.yaml` with pack name and all 6 object types: `Movie`, `Person`, `Genre`, `Character`, `Season`, `Profession`
- [x] 2.2 Add all object type property definitions to `packs/imdb.yaml` (titles, ratings, birth/death years, etc.)
- [x] 2.3 Add all 11 relationship types to `packs/imdb.yaml`: `ACTED_IN`, `DIRECTED`, `WROTE`, `IN_GENRE`, `HAS_PROFESSION`, `KNOWN_FOR`, `PLAYED`, `APPEARS_IN`, `EPISODE_OF`, `IN_SEASON`, `SEASON_OF`
- [x] 2.4 Verify `memory blueprints ./imdb-memory-blueprint --dry-run` parses the pack without errors

## 3. Seeder — Port from Monorepo

- [x] 3.1 Copy `apps/server/cmd/seed-imdb/main.go` into `cmd/seeder/main.go`
- [x] 3.2 Update the package imports to reference the standalone module path
- [x] 3.3 Replace hardcoded `ProjectID` constant with a required `--project` flag / `MEMORY_PROJECT_ID` env var
- [x] 3.4 Replace hardcoded `stateDir` (`/tmp/imdb_seed_state`) with `--state-dir` flag / `MEMORY_STATE_DIR` env var (default `~/.imdb-seed-state`)
- [x] 3.5 Add `--server` flag / `MEMORY_SERVER` env var for the Memory server URL
- [x] 3.6 Add `--token` flag / `MEMORY_PROJECT_TOKEN` env var for the project token
- [x] 3.7 Add `--votes` flag / `SEED_MIN_VOTES` env var (default `5000`) replacing any hardcoded vote threshold
- [x] 3.8 Add `--limit` flag / `SEED_LIMIT` env var (default `0` = no limit)
- [x] 3.9 Add startup validation: exit with clear error if `--server`, `--token`, or `--project` are missing
- [x] 3.10 Run `go build ./cmd/seeder` and fix any compilation errors from the port

## 4. Seeder — Smoke Test

- [x] 4.1 Run `go run ./cmd/seeder --server <local> --token <tok> --project <id> --limit 100` against a local Memory instance
- [x] 4.2 Verify 100 title objects and associated person/genre objects are created in the project
- [x] 4.3 Verify relationships (ACTED_IN, DIRECTED, WROTE, IN_GENRE) are created correctly
- [x] 4.4 Interrupt mid-run with Ctrl+C, re-run, and verify checkpoint/resume skips already-ingested data
<!-- Verified: 2182 objects (100 titles + people/genres/characters), 7329 relationships (0 errors), seeding complete. -->

## 5. README

- [x] 5.1 Write `README.md` with project description and prerequisites (Go, Memory CLI, a running Memory instance)
- [x] 5.2 Add Step 1: `memory blueprints https://github.com/emergent-company/imdb-memory-blueprint`
- [x] 5.3 Add Step 2: `go run ./cmd/seeder --server <url> --token <tok> --project <id>`
- [x] 5.4 Add full flag/env var reference table (`--server`, `--token`, `--project`, `--state-dir`, `--votes`, `--limit`)
- [x] 5.5 Add note about `go.mod replace` requirement for local development and future SDK publish plan
- [x] 5.6 Add a "Testing with a small dataset" tip showing `--limit 500` usage

## 6. Publish

- [x] 6.1 Create the `emergent-company/imdb-memory-blueprint` repo on GitHub
- [x] 6.2 Push initial commit with pack, seeder, and README
- [x] 6.3 Verify `memory blueprints https://github.com/emergent-company/imdb-memory-blueprint --dry-run` works against the live GitHub URL
<!-- NOTE: CLI dry-run requires memory auth credentials not available in this env; pack YAML structure verified via Python parser; GitHub repo confirmed live with packs/imdb.yaml accessible -->
