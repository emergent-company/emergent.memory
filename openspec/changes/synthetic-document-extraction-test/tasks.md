## 1. Project Scaffolding

- [x] 1.1 Create directory `apps/server-go/cmd/extraction-bench/`
- [x] 1.2 Create `apps/server-go/cmd/extraction-bench/main.go` with package declaration, usage comment block (matching `imdb-bench` style), and import skeleton
- [x] 1.3 Define CLI flag variables using `flag` package: `--host`, `--api-key`, `--project-id`, `--min-entity-recall`, `--min-entity-precision`, `--min-rel-recall`, `--min-rel-precision`, `--log-file`, `--poll-timeout`
- [x] 1.4 Add `flag.Parse()` with validation that all required flags are non-empty; print usage and `os.Exit(1)` on missing flags

## 2. Hardcoded Dataset

- [x] 2.1 Define `Movie` struct with fields: `ID`, `Title`, `Year`, `Genre`, `RuntimeMins`
- [x] 2.2 Define `Person` struct with fields: `ID`, `Name`, `BirthYear`
- [x] 2.3 Define `Relationship` struct with fields: `Type` (ACTED_IN/DIRECTED/WROTE), `PersonID`, `MovieID`
- [x] 2.4 Declare `groundTruthMovies []Movie` with at least 8 IMDB-style entries (real well-known films)
- [x] 2.5 Declare `groundTruthPeople []Person` with at least 12 entries covering directors, writers, and actors from the movies
- [x] 2.6 Declare `groundTruthRelationships []Relationship` with at least 20 entries covering all three types (ACTED_IN, DIRECTED, WROTE)

## 3. Synthetic Document Generator

- [x] 3.1 Implement `generateDocument(movies []Movie, people []Person, rels []Relationship) string` that produces deterministic English prose
- [x] 3.2 For each movie, write a paragraph that mentions: title, release year, genre, director name(s), writer name(s), and lead actor name(s) in natural sentence form
- [x] 3.3 Verify determinism: function must produce identical output for identical inputs (no random ordering, no `time.Now()` in text)

## 4. Document Upload

- [x] 4.1 Implement `uploadDocument(host, apiKey, projectID, content string) (docID string, err error)` using `mime/multipart` to POST to `{host}/api/documents/upload`
- [x] 4.2 Set `Authorization: Bearer {apiKey}` and `X-Project-ID: {projectID}` headers on the request
- [x] 4.3 Parse the JSON response and extract the document ID; return a clear error if the response is non-2xx
- [x] 4.4 Print progress line: `"[1/4] Uploading synthetic document..."` and `"    ✓ Uploaded document <id>"` on success

## 5. Extraction Job

- [x] 5.1 Implement `createExtractionJob(host, apiKey, projectID, docID string) (jobID string, err error)` calling `POST {host}/api/admin/extraction-jobs`
- [x] 5.2 Build the request body with `source_type: "document"`, `source_id: docID`, and the full extraction config (Movie + Person object schemas, ACTED_IN/DIRECTED/WROTE relationship schemas)
- [x] 5.3 Parse the response to extract the job ID; return a clear error if non-201
- [x] 5.4 Print progress: `"[2/4] Creating extraction job..."` and `"    ✓ Job <id> queued"`

## 6. Polling for Completion

- [x] 6.1 Implement `pollJob(host, apiKey, projectID, jobID string, timeoutSecs int) (status string, err error)` that loops with 2-second sleeps calling `GET {host}/api/admin/extraction-jobs/{jobID}`
- [x] 6.2 Print a dot or status update each poll iteration so the user sees progress
- [x] 6.3 Return when status is `"completed"` or `"failed"`, or when timeout is exceeded
- [x] 6.4 On `"failed"`, return the `error_message` field from the response as the error
- [x] 6.5 On timeout, return a descriptive timeout error
- [x] 6.6 Print `"[3/4] Waiting for extraction to complete..."` before polling starts and `"    ✓ Completed in <N>s"` on success

## 7. Graph Query

- [x] 7.1 Implement `fetchGraphObjects(host, apiKey, projectID string) ([]map[string]any, error)` calling `GET {host}/api/graph/objects`
- [x] 7.2 Implement `fetchGraphRelationships(host, apiKey, projectID string) ([]map[string]any, error)` calling `GET {host}/api/graph/relationships`
- [x] 7.3 Both functions SHALL set the required auth and project ID headers
- [x] 7.4 Print `"[4/4] Querying graph for extracted results..."`

## 8. Scoring Engine

- [x] 8.1 Implement `fuzzyMatch(extracted, groundTruth string) bool` using `strings.Contains` on both lowercase values (check if either is a substring of the other)
- [x] 8.2 Implement `scoreEntities(extracted []map[string]any, people []Person, movies []Movie) EntityScore` that returns matched, missing, and spurious entity lists plus precision and recall floats
- [x] 8.3 Implement `scoreRelationships(extracted []map[string]any, rels []Relationship, entityScore EntityScore) RelScore` that checks whether source and target of each ground-truth relationship were both matched
- [x] 8.4 Derive entity name from extracted objects using the `name` or `title` property (try both)

## 9. Results Output

- [x] 9.1 Implement `printResultsTable(entityScore, relScore, thresholds)` that prints a formatted table to stdout with columns: Metric | Value | Threshold | Status
- [x] 9.2 Print matched entities list, missing entities list, and spurious entities list
- [x] 9.3 Print matched relationships list, missing relationships list, and spurious relationships list
- [x] 9.4 Use clear symbols (✓ / ✗) for pass/fail per metric

## 10. JSONL Run Log

- [x] 10.1 Define `RunRecord` struct with all fields: timestamp, host, projectID, docID, jobID, entityRecall, entityPrecision, relRecall, relPrecision, thresholdsMet, durationSecs
- [x] 10.2 Implement `appendRunLog(logFile string, record RunRecord) error` that JSON-marshals the record and appends one line to the file (creating it if absent)
- [x] 10.3 Call `appendRunLog` after scoring regardless of pass/fail result

## 11. Exit Code and Threshold Enforcement

- [x] 11.1 After printing results, compare all four metrics to their thresholds
- [x] 11.2 Collect all failing metrics and print a summary message listing which ones failed
- [x] 11.3 Call `os.Exit(1)` if any threshold is not met; `os.Exit(0)` on full pass

## 12. Verification

- [x] 12.1 Run `go build ./cmd/extraction-bench/` from `apps/server-go/` and confirm it compiles without errors
-[x] 12.2 Run the script against the dev server with a valid project ID and API key; confirm output table appears and JSONL log is written
-[x] 12.3 Verify the script exits non-zero when a threshold is deliberately set very high (e.g., `--min-entity-recall=1.0` with an imperfect extraction)
-[x] 12.4 Run the script twice and confirm the JSONL log has two entries and the synthetic document content is identical between runs
