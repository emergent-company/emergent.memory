# Writing a New E2E Suite

This guide walks through adding a new test suite to `tools/e2e-suite/`.
A suite is any scenario that exercises a live Emergent server end-to-end —
uploading data, querying the graph, calling agents, checking extraction results, etc.

---

## The `Suite` Interface

Every suite implements one interface in `suite/suite.go`:

```go
type Suite interface {
    Name()        string
    Description() string
    Run(ctx context.Context, client *sdk.Client, cfg *Config) (*Result, error)
}
```

| Method | What to return |
|--------|---------------|
| `Name()` | Short identifier, lowercase, no spaces — used as the CLI argument |
| `Description()` | One-line human description, shown in `--help` and at runtime |
| `Run(...)` | A `*Result` with one `ItemResult` per logical test item |

`Run` receives:
- `ctx` — carries the global timeout; respect it in all blocking calls
- `client` — pre-configured SDK client (auth, project, org already set)
- `cfg` — shared config (ServerURL, APIKey, ProjectID, Concurrency, Timeout, …)

---

## Step-by-Step: Adding a Suite

### 1. Create the package

```
tools/e2e-suite/suites/<yourname>/suite.go
```

Name the package after your suite (e.g. `package mydata`).

### 2. Implement the interface

Minimal skeleton:

```go
package mydata

import (
    "context"
    "fmt"
    "time"

    sdk "github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
    "github.com/emergent-company/emergent/tools/e2e-suite/suite"
)

type Suite struct{}

func (s *Suite) Name()        string { return "mydata" }
func (s *Suite) Description() string { return "Upload MyData files and verify extraction" }

func (s *Suite) Run(ctx context.Context, client *sdk.Client, cfg *suite.Config) (*suite.Result, error) {
    result := suite.NewResult(s.Name())

    // --- your logic here ---

    return result, nil
}
```

### 3. Add suite-specific config

Read env vars with a suite-specific prefix so they don't collide with others:

```go
import "os"

type config struct {
    DataDir string // MYDATA_DIR
    APIKey  string // falls back to shared cfg.APIKey
}

func loadConfig() (*config, error) {
    dir := os.Getenv("MYDATA_DIR")
    if dir == "" {
        return nil, fmt.Errorf("MYDATA_DIR is required for the mydata suite")
    }
    return &config{DataDir: dir}, nil
}
```

### 4. Record results with `ItemResult`

Each logical test item — a document, a query, a scenario — gets one `ItemResult`:

```go
start := time.Now()
err := doSomething(ctx, client)

result.AddItem(suite.ItemResult{
    ID:       "unique-item-id",
    Name:     "Human-readable name",
    Status:   suite.StatusPassed,    // or StatusFailed, StatusSkipped, StatusTimeout
    Duration: time.Since(start),
    Error:    "",                    // set on failure
})
```

`result.AddItem` automatically updates the derived `Passed`/`Failed`/… counters.

### 5. Use shared upload utilities

For file uploads, use `suite.UploadFiles` — it handles the worker pool,
retry/backoff, and text-vs-binary detection automatically:

```go
files := []suite.FileInput{
    {Path: "/path/to/file.pdf", Filename: "file.pdf"},
    {Path: "/path/to/note.md",  Filename: "note.md"},
    // In-memory content (no Path needed):
    {Filename: "inline.txt", Content: []byte("hello world")},
}

records := suite.UploadFiles(ctx, client, files, suite.UploadOptions{
    Concurrency: cfg.Concurrency,
    AutoExtract: true,
    ServerURL:   cfg.ServerURL,
    ProjectID:   cfg.ProjectID,
})

for _, r := range records {
    if r.Error != nil {
        result.AddItem(suite.ItemResult{
            ID: r.Filename, Name: r.Filename,
            Status: suite.StatusFailed, Error: r.Error.Error(),
        })
    }
}
```

`FileInput.ContentType` is auto-detected from the extension if left empty.
Override it explicitly for non-standard types (e.g. `"audio/mpeg"` for `.mp3`).

### 6. Use shared polling utilities

After uploading, poll extraction jobs with `suite.PollExtractionJobs`:

```go
// Build the list of (docID, filename) pairs from your upload records
var pollDocs []struct{ DocID, Filename string }
for _, r := range records {
    if r.Status == suite.StatusPassed && r.DocumentID != "" {
        pollDocs = append(pollDocs, struct{ DocID, Filename string }{r.DocumentID, r.Filename})
    }
}

// Wrap in a sub-deadline so polling doesn't consume the full timeout
pollCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
defer cancel()

extractionResults := suite.PollExtractionJobs(
    pollCtx,
    cfg.ServerURL, cfg.APIKey, cfg.ProjectID,
    pollDocs,
    suite.PollOptions{Concurrency: cfg.Concurrency},
)

for _, er := range extractionResults {
    result.AddItem(suite.ItemResult{
        ID:       er.DocumentID,
        Name:     er.Filename,
        Status:   er.Status,
        Duration: er.Duration,
        Error:    er.Error,
    })
}
```

Polling terminates when each document's job reaches `completed` or `failed`,
or when the context deadline is hit (yielding `StatusTimeout`).

### 7. Make direct SDK calls for graph/agent scenarios

For scenarios that don't use upload+poll, use the SDK client directly:

```go
// Graph queries
objects, err := client.Graph.ListObjects(ctx, &graph.ListObjectsOptions{
    Type:  "Movie",
    Limit: 100,
})

// Bulk insert
_, err = client.Graph.BulkCreateObjects(ctx, &graph.BulkCreateObjectsRequest{
    Items: []graph.CreateObjectRequest{...},
})

// Agent chat (SSE)
req, _ := http.NewRequestWithContext(ctx, "POST",
    cfg.ServerURL+"/api/chat/stream",
    strings.NewReader(`{"message":"...","agentDefinitionId":"..."}`),
)
req.Header.Set("Content-Type", "application/json")
resp, err := client.Do(ctx, req)
```

`client.Do` injects auth headers automatically. Use `cfg.ServerURL` for the base URL.

### 8. Register the suite in `main.go`

Open `cmd/e2e-suite/main.go` and add two lines:

```go
import (
    // ... existing imports ...
    "github.com/emergent-company/emergent/tools/e2e-suite/suites/mydata"
)

func resolveSuites(name string) ([]suite.Suite, error) {
    all := []suite.Suite{
        &imdbsuite.Suite{},
        &huma.Suite{},
        &niezatapialni.Suite{},
        &mydata.Suite{},   // ← add here
    }

    switch name {
    case "all":
        return all, nil
    // ... existing cases ...
    case "mydata":             // ← add here
        return []suite.Suite{&mydata.Suite{}}, nil
    }
}
```

Also add a line in the `usage()` function under "Suites:".

### 9. Build and verify

```bash
cd tools/e2e-suite
go build ./cmd/e2e-suite/

# Dry-run: confirms config + shows the suite would be invoked
MYDATA_DIR=/tmp/test-data ./e2e-suite --dry-run mydata

# Full run
EMERGENT_API_KEY=emt_... EMERGENT_PROJECT_ID=... \
MYDATA_DIR=/tmp/test-data \
./e2e-suite mydata
```

---

## Complete Minimal Example

Below is a self-contained suite that creates a single graph object and verifies it can be read back.

```go
// suites/graphsmoke/suite.go
package graphsmoke

import (
    "context"
    "fmt"
    "time"

    sdk "github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
    "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/graph"
    "github.com/emergent-company/emergent/tools/e2e-suite/suite"
)

type Suite struct{}

func (s *Suite) Name()        string { return "graphsmoke" }
func (s *Suite) Description() string { return "Create a graph object and read it back" }

func (s *Suite) Run(ctx context.Context, client *sdk.Client, cfg *suite.Config) (*suite.Result, error) {
    result := suite.NewResult(s.Name())

    // --- Create ---
    start := time.Now()
    key := fmt.Sprintf("smoke-test-%d", time.Now().Unix())
    obj, err := client.Graph.CreateObject(ctx, &graph.CreateObjectRequest{
        Type: "SmokeTest",
        Key:  &key,
        Properties: map[string]any{"label": "hello from e2e-suite"},
    })
    if err != nil {
        result.AddItem(suite.ItemResult{
            ID: "create", Name: "Create graph object",
            Status: suite.StatusFailed, Duration: time.Since(start), Error: err.Error(),
        })
        return result, nil
    }
    result.AddItem(suite.ItemResult{
        ID: "create", Name: "Create graph object",
        Status: suite.StatusPassed, Duration: time.Since(start),
    })

    // --- Read back ---
    start = time.Now()
    fetched, err := client.Graph.GetObject(ctx, obj.EntityID)
    if err != nil {
        result.AddItem(suite.ItemResult{
            ID: "read", Name: "Read graph object",
            Status: suite.StatusFailed, Duration: time.Since(start), Error: err.Error(),
        })
        return result, nil
    }
    if fetched.EntityID != obj.EntityID {
        result.AddItem(suite.ItemResult{
            ID: "read", Name: "Read graph object",
            Status: suite.StatusFailed, Duration: time.Since(start),
            Error: fmt.Sprintf("got entity_id %q, want %q", fetched.EntityID, obj.EntityID),
        })
        return result, nil
    }
    result.AddItem(suite.ItemResult{
        ID: "read", Name: "Read graph object",
        Status: suite.StatusPassed, Duration: time.Since(start),
    })

    return result, nil
}
```

Register it in `main.go`:

```go
import "github.com/emergent-company/emergent/tools/e2e-suite/suites/graphsmoke"

// in resolveSuites:
case "graphsmoke":
    return []suite.Suite{&graphsmoke.Suite{}}, nil
```

Run it:

```bash
EMERGENT_API_KEY=emt_... EMERGENT_PROJECT_ID=... ./e2e-suite graphsmoke
```

---

## Shared Infrastructure Reference

### `suite.UploadFiles`

```go
func UploadFiles(
    ctx         context.Context,
    client      *sdk.Client,
    files       []FileInput,
    opts        UploadOptions,
) []UploadRecord
```

- Runs a bounded worker pool (`opts.Concurrency`, default 4)
- Text files (`.md`, `.txt`) → `Documents.Create` (inline content) + extraction trigger
- Binary files (`.pdf`, `.docx`, `.mp3`, …) → `Documents.UploadWithOptions(autoExtract=true)`
- Retries up to `opts.MaxRetries` times (default 5) on HTTP 429/503 with exponential backoff

### `suite.PollExtractionJobs`

```go
func PollExtractionJobs(
    ctx        context.Context,
    serverURL  string,
    apiKey     string,
    projectID  string,
    docs       []struct{ DocID, Filename string },
    opts       PollOptions,
) []ExtractionResult
```

- Polls `GET /api/admin/extraction-jobs/projects/:id?source_id=<docID>`
- Default poll interval: 10s
- Terminates per-document when status is `completed` or `failed`
- Terminates via context deadline → `StatusTimeout`
- After 90s with no job created, treats the document as `StatusPassed`
  (some inline-text documents don't create extraction jobs)

### `suite.Result`

```go
result := suite.NewResult("mysuite")      // captures start time
result.AddItem(suite.ItemResult{...})     // updates Passed/Failed/... counters
result.Finalize()                         // sets Duration (called by Runner automatically)
result.Print()                            // text table to stdout
result.PrintJSON()                        // JSON to stdout
```

### Status values

| Constant | Meaning |
|----------|---------|
| `suite.StatusPassed` | Item completed successfully |
| `suite.StatusFailed` | Item failed with an error |
| `suite.StatusSkipped` | Item was intentionally skipped (e.g. duplicate) |
| `suite.StatusTimeout` | Context deadline exceeded before completion |

---

## Design Conventions

- **One `ItemResult` per logical unit** — a document, a query, a CRUD round-trip. Don't bundle multiple things into one item; granular results are more useful for debugging.
- **Return early on fatal errors** — if you can't load config or connect, return `result, err`. Don't fabricate a failed item for infrastructure errors.
- **Respect `ctx` everywhere** — pass it to all SDK calls and HTTP requests. The runner applies the suite timeout via the context; leaking goroutines beyond the deadline defeats it.
- **No panics** — return errors; let the runner handle them.
- **Suite-prefixed env vars** — prefix your env vars (e.g. `MYDATA_`) to avoid clashes with other suites or the shared config.
- **Idempotency where possible** — check if the data already exists before seeding. The IMDB suite does this by counting objects before attempting insertion.
