## Context

The core monorepo already contains a fully working IMDb seeder at `apps/server/cmd/seed-imdb/main.go` — a 1100-line Go program that streams public IMDb TSV datasets, filters, and bulk-ingests objects and relationships into a Memory project via the graph SDK. It was built as an internal benchmark/test tool and has hardcoded values (project ID, state dir paths) that make it unsuitable for distribution.

This design covers porting that seeder into a standalone public repo structured as a Memory Blueprint, so any developer can install the template pack and seed a project in two commands.

## Goals / Non-Goals

**Goals:**
- Standalone public repo at `github.com/emergent-company/imdb-memory-blueprint`
- `packs/` directory with Memory-compatible YAML defining the IMDb type schema
- `cmd/seeder/` — Go program, ported from `seed-imdb/main.go`, configurable via flags/env vars (no hardcoded project IDs or paths)
- `README.md` with a two-step quickstart: `memory blueprints <url>` then `go run ./cmd/seeder`
- Repo is self-contained — no dependency on internal monorepo packages; uses only the public graph SDK or plain HTTP

**Non-Goals:**
- No `seed/` directory or `blueprint-seed-data` format integration (future)
- No CI-based dataset regeneration
- No agent definitions or embedding/search features
- No changes to the Memory monorepo

## Decisions

### Decision 1: Repo structure

```
imdb-memory-blueprint/
├── packs/
│   └── imdb.yaml          # template pack: Movie, Person + relationships
├── cmd/
│   └── seeder/
│       └── main.go        # ported seeder CLI
├── go.mod                 # standalone Go module
└── README.md
```

**Rationale:** `memory blueprints` requires `packs/` and/or `agents/` at the repo root — this is the prescribed layout. The seeder lives in `cmd/seeder/` following Go conventions for executables. No monorepo tooling (no Taskfile, no Nx).

### Decision 2: Seeder configuration via flags and env vars

The existing `seed-imdb/main.go` has hardcoded `ProjectID` and `/tmp/imdb_seed_state` paths. The ported version will accept:

| Flag | Env var | Description |
|---|---|---|
| `--server` | `MEMORY_SERVER` | Memory server URL |
| `--token` | `MEMORY_PROJECT_TOKEN` | Project token |
| `--project` | `MEMORY_PROJECT_ID` | Project ID |
| `--state-dir` | `MEMORY_STATE_DIR` | Checkpoint dir (default `~/.imdb-seed-state`) |
| `--limit` | `SEED_LIMIT` | Cap number of titles (default: no limit) |
| `--votes` | `SEED_MIN_VOTES` | Minimum vote threshold (default: 5000) |

**Rationale:** Makes the tool usable against any Memory instance without source edits. Follows the same pattern as the `memory` CLI.

### Decision 3: SDK dependency — use internal graph SDK via replace directive OR plain HTTP

Two options:
- **A) Import `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph`** — works today but ties the standalone repo to the monorepo's versioning; requires `go.mod replace` for local dev or a published module tag
- **B) Call the Memory REST API directly** (plain `net/http`) — fully standalone, no SDK dependency, slightly more boilerplate

**Decision: Option A initially** — the graph SDK is the fastest path to a working port with minimal rewriting. Add a `replace` directive in `go.mod` for local development; when the SDK is published as a versioned module, update the import. Document this in the README.

**Alternative considered:** Option B would be cleaner long-term but requires reimplementing batch logic already proven in the SDK.

### Decision 4: Checkpoint/resume kept as-is

The existing seeder has a robust checkpoint system (`state.json`, `idmap.json`, `rels_done.txt`) that survives SIGINT and allows resume. This is kept in the port — the full IMDb dataset takes 20-40 minutes to ingest and resume is essential for developer experience.

### Decision 5: Template pack content

The `packs/imdb.yaml` will define exactly the types the seeder inserts:

**Object types:** `Movie`, `Person`, `Genre`, `Character`, `Season`, `Profession`
**Relationship types:** `ACTED_IN`, `DIRECTED`, `WROTE`, `IN_GENRE`, `HAS_PROFESSION`, `KNOWN_FOR`, `PLAYED`, `APPEARS_IN`, `EPISODE_OF`, `IN_SEASON`, `SEASON_OF`

**Rationale:** Match the seeder's actual output exactly — the pack must be applied before the seeder runs, so the types must exist.

## Risks / Trade-offs

- **SDK is not a public module yet** → Developers cloning the repo need a local checkout of the monorepo or a `replace` in `go.mod`. Mitigated by clear README instructions and a future SDK publish.
- **IMDb dataset changes** → IMDB occasionally changes TSV column layout. The seeder may break silently. Mitigated by column index validation and error reporting in the port.
- **Ingest time** → Full dataset takes 20-40 min. Checkpoint/resume addresses this, but developers need to be aware. Mitigated by `--limit` flag for quick testing (e.g., `--limit 1000`).
- **Hardcoded project ID in existing code** → Must be carefully removed; leaving it in would cause confusing failures. Mitigated by making `--project` a required flag with a clear error if missing.

## Migration Plan

1. Create the repo at `/root/imdb-memory-blueprint`, init as `github.com/emergent-company/imdb-memory-blueprint`
2. Copy `apps/server/cmd/seed-imdb/main.go` into `cmd/seeder/main.go`
3. Replace hardcoded `ProjectID`, `stateDir`, vote threshold with flag/env parsing
4. Add `go.mod` with SDK dependency + `replace` directive pointing to local monorepo
5. Create `packs/imdb.yaml` matching all object/relationship types the seeder emits
6. Write `README.md` with quickstart
7. Push to `github.com/emergent-company/imdb-memory-blueprint`

No rollback needed — additive, standalone repo.

## Open Questions

- Should `--votes` default to `5000` (current seeder default) or `20000` (the benchmark test spec)? Leaning toward `5000` for richer data in a dev context.
- When will the graph SDK be published as a versioned Go module? Until then, the `go.mod replace` requirement is a friction point for developers without a local monorepo clone.
- Should the pack install be idempotent with `--upgrade` by default in the README, or leave it to the user?
