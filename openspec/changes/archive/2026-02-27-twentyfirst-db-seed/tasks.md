## 1. Dump Script

- [ ] 1.1 Create `scripts/dump-twentyfirst-db.sh` with gcloud auth check (fail fast with helpful message if no active account)
- [ ] 1.2 Add Cloud SQL Auth Proxy download logic (cache at `/tmp/cloud-sql-proxy`, pin version `v2.14.1`, skip download if already present)
- [ ] 1.3 Start Auth Proxy as background process tunneling `twentyfirst-io:us-central1:dep-database-dev-b3db59nu` to `localhost:5433`
- [ ] 1.4 Add `pg_dump --format=custom` call writing to `/tmp/twentyfirst_dump/twentyfirst-db.dump`
- [ ] 1.5 Add schema introspection step: connect via proxy, run `\dt *.*`, print table list to stdout
- [ ] 1.6 Add `COPY <table> TO STDOUT CSV HEADER` export loop for each configured table, gzip output to `/tmp/twentyfirst_dump/<table>.csv.gz`
- [ ] 1.7 Kill Auth Proxy on exit (trap EXIT signal), print summary of files written
- [ ] 1.8 Make script executable (`chmod +x`) and test end-to-end after gcloud auth

## 2. Standalone Go Module

- [ ] 2.1 Create directory `scripts/seed-twentyfirst-db/`
- [ ] 2.2 Write `scripts/seed-twentyfirst-db/go.mod` with module name `seed-twentyfirst-db`, Go 1.22, dependency on `github.com/lib/pq` (for any direct Postgres reads if needed)
- [ ] 2.3 Run `go mod tidy` inside `scripts/seed-twentyfirst-db/` to generate `go.sum`

## 3. Seeder Core — State & Config

- [ ] 3.1 Write `scripts/seed-twentyfirst-db/main.go` with env var config (`SERVER_URL`, `API_KEY`, `PROJECT_ID`, `DRY_RUN`, `SEED_LIMIT`, `DUMP_DIR` defaulting to `/tmp/twentyfirst_dump/`)
- [ ] 3.2 Implement checkpoint state system: `loadState()`, `saveState()` using `/tmp/twentyfirst_seed_state/state.json` with phases `objects_pending` → `objects_done` → `rels_pending` → `done`
- [ ] 3.3 Implement ID map: `loadIDMap()`, `saveIDMap()` persisting source-row-key → emergent-canonical-id mapping to `/tmp/twentyfirst_seed_state/idmap.json`
- [ ] 3.4 Implement `loadRelsDone()` / `appendRelDone()` for tracking completed relationship batches via `/tmp/twentyfirst_seed_state/rels_done.txt`
- [ ] 3.5 Implement `appendRelFailed()` writing failed batches to `/tmp/twentyfirst_seed_state/rels_failed.jsonl`
- [ ] 3.6 Add SIGINT handler: finish in-flight batch, save state, exit cleanly

## 4. Seeder Core — CSV Reading

- [ ] 4.1 Implement `streamCSV(path string) (<-chan []string, error)` that opens a `.csv.gz` file and streams rows as string slices
- [ ] 4.2 Add `DRY_RUN` / `SEED_LIMIT` row cap to the streaming layer

## 5. Seeder Core — Companies & Version History

- [ ] 5.1 Load `company_diffs.csv.gz` into memory map grouped by `company_id` and sorted by `created_at` ASC
- [ ] 5.2 Implement `Company` object building, reading current state from `companies.csv.gz`
- [ ] 5.3 For companies with diffs: reconstruct oldest state by applying `backward` patches in reverse chronological order
- [ ] 5.4 For companies with diffs: Upsert oldest state (via `POST /api/graph/objects/upsert` with `key=org_no`), then walk forward applying `forward` patches via `PATCH /api/graph/objects/:id` to build native version history
- [ ] 5.5 For companies without diffs: Batch insert current state via `POST /api/graph/objects/bulk`
- [ ] 5.6 Save `canonical_id` mapping to `idmap.json` and transition state to `companies_done`

## 6. Seeder Core — People & Financial Reports

- [ ] 6.1 Stream `people.csv.gz`, filter `is_test=t`, map to `Person` objects (`key=id`), bulk insert, save to idmap
- [ ] 6.2 Stream `accounts_reports.csv.gz`, map to `FinancialReport` objects (`key=company_id+from_date+to_date`), bulk insert, save to idmap
- [ ] 6.3 Transition state to `objects_done`

## 7. Seeder Core — Relationship Loading

- [ ] 7.1 Load `company_role_groups.csv.gz` and `shareholders_reports.csv.gz` into memory maps for property lookups
- [ ] 7.2 Implement relationship building for `SUBSIDIARY_OF` (from `companies.parent_id`)
- [ ] 7.3 Implement relationship building for `HAS_ROLE` (from `company_roles` + groups map)
- [ ] 7.4 Implement relationship building for `OWNS_SHARES_IN` (from `shareholders_report_entries` + reports map)
- [ ] 7.5 Implement relationship building for `HAS_FINANCIAL_REPORT` (from `accounts_reports`)
- [ ] 7.6 Bulk insert relationships in batches, skipping already-completed batches via `rels_done.txt`
- [ ] 7.7 Transition state to `done` and print final summary

## 7. Convenience Script

- [ ] 7.1 Create `scripts/seed-twentyfirst-db.sh` that exports `SERVER_URL`, `API_KEY`, `PROJECT_ID` (dev defaults), and runs `go run scripts/seed-twentyfirst-db/main.go`
- [ ] 7.2 Make script executable (`chmod +x`)

## 8. Verification

- [ ] 8.1 Run `scripts/dump-twentyfirst-db.sh` after `gcloud auth login` — confirm files land in `/tmp/twentyfirst_dump/`
- [ ] 8.2 Run seeder in `DRY_RUN=true` mode — confirm it prints objects/relationships without hitting the API
- [ ] 8.3 Run seeder with `SEED_LIMIT=10` against dev emergent — confirm 10 objects appear in the graph
- [ ] 8.4 Interrupt seeder mid-run (Ctrl+C), re-run — confirm it resumes from checkpoint without duplicating data
- [ ] 8.5 Run full seed — confirm all objects and relationships load successfully
