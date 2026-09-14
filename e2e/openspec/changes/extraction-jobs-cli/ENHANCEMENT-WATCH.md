# Enhancement: --watch Flag for Extraction Jobs

## Problem

Currently, users and tests need to manually poll extraction jobs in a loop to wait for completion:

```bash
# Manual polling loop (inefficient, noisy)
while true; do
  STATUS=$(memory extraction jobs get $JOB_ID --output json | jq -r '.data.status')
  echo "Status: $STATUS"
  [[ "$STATUS" == "completed" ]] && break
  [[ "$STATUS" == "failed" ]] && exit 1
  sleep 5
done
```

This creates:
- **81 seconds of polling** in the test run log (16+ repeated CLI calls)
- **Noisy logs** with duplicate status messages
- **Inefficient network usage** (polling every 5 seconds)
- **Complex test code** (manual polling logic in every test)

## Proposed Solution

Add `--watch` flag to `memory extraction jobs get <id>`:

```bash
# Clean, single-line wait
memory extraction jobs get $JOB_ID --watch
```

### Behavior

**Without --watch** (current):
- Single GET request, returns immediately with current status
- Exit code 0 always (even if job failed)

**With --watch**:
- Polls job status every 3-5 seconds until terminal state
- Terminal states: `completed`, `failed`, `cancelled`
- Shows live progress updates (optional with `--quiet` to suppress)
- Exit code 0 if `completed`, exit code 1 if `failed` or `cancelled`
- Timeout after 10 minutes (configurable with `--timeout`)

### CLI Interface

```bash
# Wait for job to complete (default behavior)
memory extraction jobs get <job-id> --watch

# Wait with custom timeout
memory extraction jobs get <job-id> --watch --timeout 5m

# Quiet mode (suppress progress updates, only final status)
memory extraction jobs get <job-id> --watch --quiet

# JSON output (returns final state as JSON)
memory extraction jobs get <job-id> --watch --output json
```

### Example Output

**Table mode** (default):
```
Watching extraction job 600927e0-28f0-4f91-9d5d-bd0e4c8be61a...
Status: queued (0s)
Status: running (5s)
Status: running (10s)
Status: completed (15s)

Job ID:          600927e0-28f0-4f91-9d5d-bd0e4c8be61a
Status:          completed
Objects Created: 1
Elapsed:         15s
```

**Quiet mode** (`--quiet`):
```
Status: completed
Objects Created: 1
```

**JSON mode** (`--output json`):
```json
{
  "success": true,
  "data": {
    "id": "600927e0-28f0-4f91-9d5d-bd0e4c8be61a",
    "status": "completed",
    "objects_created": 1,
    ...
  }
}
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--watch` | bool | `false` | Poll until terminal state (completed/failed/cancelled) |
| `--timeout` | duration | `10m` | Maximum time to wait (e.g., `5m`, `30s`, `1h`) |
| `--interval` | duration | `3s` | Poll interval (minimum 1s) |
| `--quiet` | bool | `false` | Suppress progress updates, only show final result |

### Exit Codes

| Status | Exit Code | Description |
|--------|-----------|-------------|
| `completed` | 0 | Job completed successfully |
| `failed` | 1 | Job failed (error in extraction pipeline) |
| `cancelled` | 1 | Job was cancelled by user |
| timeout | 124 | Watch timeout exceeded (same as `timeout` command) |
| error | 1 | Network error, invalid job ID, etc. |

## Implementation Plan

### 1. Add Flags to `extraction.go`

```go
var (
    extractionWatchFlag    bool
    extractionTimeoutFlag  time.Duration
    extractionIntervalFlag time.Duration
    extractionQuietFlag    bool
)

func init() {
    extractionJobsGetCmd.Flags().BoolVar(&extractionWatchFlag, "watch", false, "Poll until terminal state")
    extractionJobsGetCmd.Flags().DurationVar(&extractionTimeoutFlag, "timeout", 10*time.Minute, "Maximum watch duration")
    extractionJobsGetCmd.Flags().DurationVar(&extractionIntervalFlag, "interval", 3*time.Second, "Poll interval (min 1s)")
    extractionJobsGetCmd.Flags().BoolVar(&extractionQuietFlag, "quiet", false, "Suppress progress updates")
}
```

### 2. Modify `extractionJobsGetCmd.RunE`

```go
if extractionWatchFlag {
    return watchExtractionJob(cmd, jobID)
}
// ... existing single-get logic
```

### 3. Add `watchExtractionJob` Helper

```go
func watchExtractionJob(cmd *cobra.Command, jobID string) error {
    baseURL, apiKey, httpClient, err := getExtractionHTTPClient(cmd)
    if err != nil {
        return err
    }
    
    out := cmd.OutOrStdout()
    if !extractionQuietFlag && extractionOutputFlag != "json" {
        fmt.Fprintf(out, "Watching extraction job %s...\n", jobID)
    }
    
    deadline := time.Now().Add(extractionTimeoutFlag)
    interval := extractionIntervalFlag
    if interval < time.Second {
        interval = time.Second
    }
    
    start := time.Now()
    var lastStatus string
    
    for time.Now().Before(deadline) {
        // GET job status
        url := baseURL + "/api/admin/extraction-jobs/" + jobID
        req, _ := http.NewRequest(http.MethodGet, url, nil)
        req.Header.Set("X-API-Key", apiKey)
        
        resp, err := httpClient.Do(req)
        if err != nil {
            return fmt.Errorf("failed to get extraction job: %w", err)
        }
        
        body, _ := io.ReadAll(resp.Body)
        resp.Body.Close()
        
        if resp.StatusCode < 200 || resp.StatusCode >= 300 {
            return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
        }
        
        // Parse response
        var result struct {
            Success bool `json:"success"`
            Data    struct {
                Status         string `json:"status"`
                ObjectsCreated int    `json:"objects_created"`
                Error          string `json:"error,omitempty"`
            } `json:"data"`
        }
        json.Unmarshal(body, &result)
        
        status := result.Data.Status
        elapsed := time.Since(start).Round(time.Second)
        
        // Show progress (unless quiet or JSON mode)
        if !extractionQuietFlag && extractionOutputFlag != "json" && status != lastStatus {
            fmt.Fprintf(out, "Status: %s (%s)\n", status, elapsed)
        }
        lastStatus = status
        
        // Check terminal states
        switch strings.ToLower(status) {
        case "completed":
            if extractionOutputFlag == "json" {
                fmt.Fprintln(out, string(body))
            } else {
                fmt.Fprintf(out, "\nJob completed successfully\n")
                fmt.Fprintf(out, "Objects Created: %d\n", result.Data.ObjectsCreated)
                fmt.Fprintf(out, "Elapsed:         %s\n", elapsed)
            }
            return nil
            
        case "failed":
            if extractionOutputFlag == "json" {
                fmt.Fprintln(out, string(body))
            } else {
                fmt.Fprintf(out, "\nJob failed: %s\n", result.Data.Error)
            }
            return fmt.Errorf("extraction job failed")
            
        case "cancelled":
            if extractionOutputFlag == "json" {
                fmt.Fprintln(out, string(body))
            } else {
                fmt.Fprintf(out, "\nJob was cancelled\n")
            }
            return fmt.Errorf("extraction job cancelled")
        }
        
        time.Sleep(interval)
    }
    
    return fmt.Errorf("watch timeout exceeded (%s)", extractionTimeoutFlag)
}
```

### 4. Update E2E Test Helper

Simplify `waitForExtractionJobCLI` in `documents_test.go`:

```go
// Before (36 lines of polling logic):
func waitForExtractionJobCLI(t *testing.T, rl *runLog, home, jobID string, timeout time.Duration) {
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        out, err := runCLIInDirWithHome(...)
        // ... parse JSON, check status, sleep 5s ...
    }
    rl.Failf("timeout")
}

// After (3 lines):
func waitForExtractionJobCLI(t *testing.T, rl *runLog, home, jobID string, timeout time.Duration) {
    t.Helper()
    out := mustRunCLIInDirWithHome(t, "", home,
        "extraction", "jobs", "get", jobID, "--watch", "--timeout", timeout.String())
    rl.CLI("memory extraction jobs get "+jobID+" --watch", out)
}
```

## Benefits

### For Tests
- **81s → ~15s**: Single `--watch` call instead of 16+ poll iterations
- **Cleaner logs**: One "watching..." line + final result instead of 16 status lines
- **Simpler code**: 3 lines instead of 36 lines of polling logic
- **Better error handling**: Exit code 1 on failure (no need to parse JSON)

### For Users
- **Better UX**: Single command instead of manual polling loops
- **Scriptable**: Use in CI/CD without writing polling logic
- **Consistent**: Same pattern as `kubectl wait`, `docker wait`, `gh run watch`

### For Debugging
- **Progress visibility**: See live status updates during long extractions
- **Timeout control**: Configure timeout based on expected job duration
- **Quiet mode**: Suppress progress for scripting/automation

## Alternative: Similar Pattern for Documents

The same pattern applies to `memory documents get` for watching conversion status:

```bash
# Watch document conversion
memory documents get $DOC_ID --project $PROJECT_ID --watch --until conversion-complete
```

This would eliminate the 180s of polling in `TestCLIInstalled_DocumentConversion`.

## Prior Art

Similar `--watch` flags exist in:
- `kubectl wait --for=condition=Ready pod/foo` (Kubernetes)
- `gh run watch <run-id>` (GitHub CLI)
- `docker wait <container>` (Docker)
- `aws cloudformation wait stack-create-complete` (AWS CLI)
- `gcloud builds log --stream` (Google Cloud)

## Next Steps

1. Implement `--watch` flag in `extraction.go`
2. Update e2e test helper to use `--watch`
3. Verify tests still pass (with much cleaner logs)
4. Consider adding `--watch` to `documents get` for conversion status
5. Document in CLI help text and user guide
