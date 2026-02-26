# Emergent E2E Suite

A unified Go CLI for end-to-end testing against a live Emergent server.
It replaces three bespoke scripts with one shared infrastructure layer
that handles **upload → poll → report** for any data source.

---

## Quick Start

```bash
cd tools/e2e-suite

# Build
go build -o e2e-suite ./cmd/e2e-suite/

# Run the IMDB benchmark
EMERGENT_SERVER_URL=http://mcj-emergent:3002 \
EMERGENT_API_KEY=emt_... \
EMERGENT_PROJECT_ID=dfe2febb-... \
IMDB_AGENT_DEF_ID=70356e5f-... \
./e2e-suite imdb

# Run all suites with JSON output
./e2e-suite --output json all
```

---

## Available Suites

| Suite | What it does |
|-------|-------------|
| `imdb` | Downloads IMDB TSV datasets, seeds Movie/Person graph objects and relationships, runs 3 natural-language agent queries to verify graph traversal works |
| `huma` | Uploads pre-cached HUMA Energy documents (`.md`, `.pdf`, `.docx`) from a local directory, polls extraction jobs until completion |
| `niezatapialni` | Uploads pre-downloaded Niezatapialni podcast `.mp3` files, polls transcription jobs until completion |
| `all` | Runs all three suites sequentially with a combined summary |

---

## CLI Reference

```
Usage: e2e-suite [flags] <suite>

Flags:
  --server-url   string     Server URL             [$EMERGENT_SERVER_URL]
  --api-key      string     API key                [$EMERGENT_API_KEY]
  --org-id       string     Org ID                 [$EMERGENT_ORG_ID]
  --project-id   string     Project ID             [$EMERGENT_PROJECT_ID]
  --concurrency  int        Worker count           (default 4)
  --timeout      duration   Suite timeout          (default 30m)
  --dry-run                 Show plan, don't run
  --output       string     text | json            (default text)
  --env-file     string     .env file path         (default .env)
```

All flags can be set via environment variables or a `.env` file — flags take precedence over env vars.

---

## Environment Variables

### Shared (all suites)

| Variable | Default | Description |
|----------|---------|-------------|
| `EMERGENT_SERVER_URL` | `http://mcj-emergent:3002` | Target server |
| `EMERGENT_API_KEY` | — | API key (required) |
| `EMERGENT_ORG_ID` | — | Organisation ID |
| `EMERGENT_PROJECT_ID` | — | Project ID (required) |
| `E2E_CONCURRENCY` | `4` | Upload/poll worker count |
| `E2E_TIMEOUT` | `30m` | Per-suite timeout |
| `E2E_OUTPUT` | `text` | `text` or `json` |

### IMDB suite

| Variable | Default | Description |
|----------|---------|-------------|
| `IMDB_AGENT_DEF_ID` | — | Agent definition UUID (required) |
| `IMDB_MIN_VOTES` | `20000` | Minimum vote threshold for title filtering |
| `IMDB_DATASET_URL` | `https://datasets.imdbws.com` | Base URL for IMDB dataset files |

### HUMA suite

| Variable | Default | Description |
|----------|---------|-------------|
| `HUMA_CACHE_DIR` | `/root/data` | Directory of pre-downloaded HUMA documents |

### Niezatapialni suite

| Variable | Default | Description |
|----------|---------|-------------|
| `NIEZATAPIALNI_MP3_DIR` | `tools/niezatapialni-scraper/all_mp3s` | Directory of pre-downloaded MP3 files |

---

## Examples

### Run a single suite against production

```bash
EMERGENT_SERVER_URL=https://api.emergent-company.ai \
EMERGENT_API_KEY=emt_abc123 \
EMERGENT_PROJECT_ID=my-project-id \
IMDB_AGENT_DEF_ID=my-agent-id \
./e2e-suite --concurrency 8 --timeout 1h imdb
```

### Use a .env file

```bash
# .env
EMERGENT_SERVER_URL=http://mcj-emergent:3002
EMERGENT_API_KEY=emt_...
EMERGENT_PROJECT_ID=dfe2febb-...
IMDB_AGENT_DEF_ID=70356e5f-...
HUMA_CACHE_DIR=/root/data
NIEZATAPIALNI_MP3_DIR=/data/mp3s
```

```bash
./e2e-suite --env-file .env all
```

### Dry-run to verify config

```bash
./e2e-suite --dry-run all
# Prints config + lists which suites would run — no network calls made
```

### JSON output for CI

```bash
./e2e-suite --output json imdb | tee results.json
echo "Exit code: $?"   # non-zero if any item failed or timed out
```

### HUMA: download first, then run suite

```bash
# Step 1: download from Google Drive (separate tool)
cd tools/huma-test-suite
./huma-test --phase download

# Step 2: run the e2e suite against the cached files
cd ../e2e-suite
HUMA_CACHE_DIR=/root/data ./e2e-suite huma
```

### Niezatapialni: scrape first, then run suite

```bash
# Step 1: scrape MP3s (separate tool)
cd tools/niezatapialni-scraper
go run . --download

# Step 2: run the e2e suite
cd ../e2e-suite
NIEZATAPIALNI_MP3_DIR=../niezatapialni-scraper/all_mp3s ./e2e-suite niezatapialni
```

---

## Output

### Text (default)

```
=== imdb ===
Duration: 4m32s

STATUS      NAME                                    DURATION  ERROR
------      ----                                    --------  -----
✓ passed    ActorIntersection: Tom Hanks & Meg Ryan  12.3s
✓ passed    ComplexTraversal: Spielberg 1990s movies  9.8s
✓ passed    GenreAndRating: top sci-fi after 2010    11.1s

Summary: 3 passed, 0 failed, 0 skipped, 0 timeout  (total: 3)
```

### JSON (`--output json`)

```json
{
  "suite": "imdb",
  "start_time": "2026-02-26T10:00:00Z",
  "duration_ms": 272000000000,
  "items": [
    {
      "id": "actor_intersection",
      "name": "ActorIntersection: Tom Hanks & Meg Ryan",
      "status": "passed",
      "duration_ms": 12300000000
    }
  ],
  "passed": 3,
  "failed": 0,
  "skipped": 0,
  "timeout": 0
}
```

---

## Architecture

```
tools/e2e-suite/
├── cmd/e2e-suite/main.go     CLI entry point — flags, suite resolution, runner
├── suite/
│   ├── config.go             Shared Config: env/flag loading, validation
│   ├── result.go             Result, ItemResult, Status — print in text or JSON
│   ├── upload.go             UploadFiles: concurrent uploads with retry/backoff
│   ├── poll.go               PollExtractionJobs: concurrent polling
│   └── suite.go              Suite interface + Runner + PrintSummary
└── suites/
    ├── imdb/suite.go         IMDB graph seeding + agent query verification
    ├── huma/suite.go         HUMA document upload + extraction polling
    └── niezatapialni/suite.go   MP3 upload + transcription polling
```

The `suite/` package is the shared infrastructure. Each `suites/<name>/suite.go` implements one test scenario and is free to use any combination of the shared primitives or its own direct SDK calls.

---

## Writing a New Suite

See [CONTRIBUTING.md](CONTRIBUTING.md) for the step-by-step guide.
