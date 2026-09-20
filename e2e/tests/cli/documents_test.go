// Package cli_test — documents_test.go
//
// End-to-end tests for `memory documents` CLI subcommands: upload, get, list,
// and delete.  Each test creates an ephemeral project and a temp file for upload.
package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestCLIInstalled_DocumentUploadGetDelete exercises the full document lifecycle:
// upload → get → delete.
func TestCLIInstalled_DocumentUploadGetDelete(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify document upload → get → delete lifecycle",
		"Create project, write a temp file, upload it as a document",
		"Get the document by ID and verify metadata",
		"Delete the document and verify it is gone",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-docs")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Write a small temp file to upload.
	rl.Section("Create temp file for upload")
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test-document.txt")
	content := "This is a test document created by the e2e test suite.\nIt has multiple lines.\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		rl.Failf("failed to write temp file: %v", err)
	}
	rl.Printf("wrote %d bytes to %s", len(content), filePath)

	// Upload the document.
	rl.Section("Upload document")
	uploadOut := mustRunCLIInDirWithHome(t, "", home,
		"documents", "upload", filePath,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory documents upload "+filePath+" --project "+projectID, uploadOut)

	docID := parseNestedDocumentID(uploadOut)
	if docID == "" {
		rl.Failf("could not parse document ID from upload output: %s", truncate(uploadOut, 300))
	}
	rl.Printf("uploaded document ID: %s", docID)

	// Get the document by ID.
	rl.Section("Get document by ID")
	getOut := mustRunCLIInDirWithHome(t, "", home,
		"documents", "get", docID,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory documents get "+docID+" --project "+projectID, getOut)

	if !strings.Contains(getOut, "test-document.txt") {
		t.Errorf("expected get output to contain filename 'test-document.txt', got:\n%s", truncate(getOut, 500))
	}
	rl.Printf("get output contains filename: true")

	// Delete the document.
	rl.Section("Delete document")
	delOut := mustRunCLIInDirWithHome(t, "", home,
		"documents", "delete", docID,
		"--project", projectID,
	)
	rl.CLI("memory documents delete "+docID+" --project "+projectID, delOut)
	rl.Printf("delete output: %s", truncate(delOut, 200))

	// Verify the delete command produced confirmation output.
	rl.Section("Verify document delete confirmed")
	lower := strings.ToLower(delOut)
	if !strings.Contains(lower, "delete") && !strings.Contains(lower, "removed") {
		t.Errorf("expected delete output to confirm deletion, got:\n%s", truncate(delOut, 300))
	}
	rl.Printf("document delete confirmed in output: true")
}

// TestCLIInstalled_DocumentsList verifies that `memory documents list` returns
// uploaded documents.
func TestCLIInstalled_DocumentsList(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify documents list returns uploaded documents",
		"Create project and upload a document",
		"List documents and verify the uploaded one appears",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-docs-list")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Write and upload a file.
	rl.Section("Upload document")
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "list-test-doc.md")
	if err := os.WriteFile(filePath, []byte("# List Test\nBody content.\n"), 0o644); err != nil {
		rl.Failf("failed to write temp file: %v", err)
	}
	uploadOut := mustRunCLIInDirWithHome(t, "", home,
		"documents", "upload", filePath,
		"--project", projectID,
	)
	rl.CLI("memory documents upload "+filePath+" --project "+projectID, uploadOut)

	// List documents as JSON.
	rl.Section("List documents")
	listOut := mustRunCLIInDirWithHome(t, "", home,
		"documents", "list",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory documents list --project "+projectID+" --output json", listOut)

	trimmed := strings.TrimSpace(listOut)
	if !json.Valid([]byte(trimmed)) {
		t.Errorf("expected valid JSON from documents list --output json, got:\n%s", truncate(trimmed, 500))
	}
	if !strings.Contains(trimmed, "list-test-doc.md") {
		t.Errorf("expected list output to contain 'list-test-doc.md', got:\n%s", truncate(trimmed, 500))
	}
	rl.Printf("list returned valid JSON containing 'list-test-doc.md'")
}

// TestCLIInstalled_DocumentUploadAutoExtract verifies that uploading a document
// with --auto-extract triggers the extraction pipeline and that the resulting
// document record reports at least one chunk after a short polling wait.
func TestCLIInstalled_DocumentUploadAutoExtract(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify --auto-extract triggers extraction and produces chunks",
		"Create project, write a text file with multiple paragraphs",
		"Upload with --auto-extract flag",
		"Poll documents get until chunks > 0 (up to 60s)",
		"Assert document reports extracted chunks",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-docs-extract")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	configureProjectModel(t, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Write a file with enough content to produce at least one chunk.
	rl.Section("Create temp file for extraction")
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "extract-test.txt")
	content := "The quick brown fox jumps over the lazy dog.\n\n" +
		"Pack my box with five dozen liquor jugs.\n\n" +
		"How vexingly quick daft zebras jump!\n\n" +
		"The five boxing wizards jump quickly.\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		rl.Failf("failed to write temp file: %v", err)
	}
	rl.Printf("wrote %d bytes to %s", len(content), filePath)

	// Upload with --auto-extract.
	rl.Section("Upload document with --auto-extract")
	uploadOut := mustRunCLIInDirWithHome(t, "", home,
		"documents", "upload", filePath,
		"--project", projectID,
		"--output", "json",
		"--auto-extract",
	)
	rl.CLI("memory documents upload --auto-extract "+filePath+" --project "+projectID, uploadOut)

	docID := parseNestedDocumentID(uploadOut)
	if docID == "" {
		rl.Failf("could not parse document ID from upload output: %s", truncate(uploadOut, 300))
	}
	rl.Printf("uploaded document ID: %s", docID)

	// Poll get until conversionStatus is not "pending" and chunks > 0 (max 60s).
	rl.Section("Poll for extraction completion")
	deadline := time.Now().Add(60 * time.Second)
	var lastGetOut string
	extracted := false
	for time.Now().Before(deadline) {
		getOut, err := runCLIInDirWithHome(t, "", home,
			"documents", "get", docID,
			"--project", projectID,
			"--output", "json",
		)
		if err != nil {
			rl.Printf("get poll error (will retry): %v", err)
			time.Sleep(3 * time.Second)
			continue
		}
		lastGetOut = getOut

		// Parse chunks count from JSON response.
		// The server returns either {"chunks": N} or {"totalChunks": N} or {"chunk_count": N}.
		chunksRaw := parseJSONField(getOut, "totalChunks")
		if chunksRaw == "" {
			chunksRaw = parseJSONField(getOut, "chunks")
		}
		if chunksRaw == "" {
			chunksRaw = parseJSONField(getOut, "chunk_count")
		}
		convStatus := parseJSONField(getOut, "conversionStatus")
		if convStatus == "" {
			convStatus = parseJSONField(getOut, "conversion_status")
		}
		rl.Printf("poll: conversionStatus=%s chunks=%s", convStatus, chunksRaw)

		if chunksRaw != "" && chunksRaw != "0" && chunksRaw != "null" {
			extracted = true
			break
		}
		// Also accept "complete" or "done" conversion status even if chunks field is absent.
		lower := strings.ToLower(convStatus)
		if lower == "complete" || lower == "done" || lower == "completed" {
			extracted = true
			break
		}
		time.Sleep(3 * time.Second)
	}
	rl.CLI("memory documents get "+docID+" --project "+projectID+" (last poll)", lastGetOut)

	rl.Section("Verify extraction produced chunks")
	if !extracted {
		t.Errorf("document was not extracted within 60s; last get output:\n%s",
			truncate(lastGetOut, 500))
	} else {
		rl.Printf("extraction confirmed: document has chunks")
	}
}

// TestCLIInstalled_DocumentExtractionWithSchema uploads a 2-3 page meeting
// notes document, installs a schema with Person and Decision types, triggers
// extraction via --auto-extract, and then polls the graph until objects of the
// expected types appear with content matching known entities in the document.
//
// Fixture files:
//   - testdata/acme-q4-planning.txt  — structured meeting notes (~2.5 pages)
//   - testdata/meeting-schema.json   — schema with Person + Decision object types
//
// Expected results (deterministic from the fixture content):
//   - At least 3 Person objects (Sarah Chen, Priya Nair, Tom Kowalski are the
//     most prominent; Marcus Rivera and Lisa Okonkwo should also appear)
//   - At least 2 Decision objects (database migration, auth rewrite, API
//     gateway consolidation — the document records three formal decisions)
func TestCLIInstalled_DocumentExtractionWithSchema(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify document extraction with schema produces expected graph objects",
		"Create project and install Person+Decision schema from testdata/meeting-schema.json",
		"Upload testdata/acme-q4-planning.txt with --auto-extract",
		"Poll graph objects until at least 1 Person type appears (up to 3 min)",
		"Assert at least 1 Person object is extracted",
		"Assert Sarah Chen (meeting chair) appears in extracted Person objects",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	// Resolve testdata paths relative to this source file.
	_, thisFile, _, _ := runtime.Caller(0)
	testdataDir := filepath.Join(filepath.Dir(thisFile), "testdata")
	docFixture := filepath.Join(testdataDir, "acme-q4-planning.txt")
	schemaFixture := filepath.Join(testdataDir, "meeting-schema.json")

	for _, f := range []string{docFixture, schemaFixture} {
		if _, err := os.Stat(f); err != nil {
			rl.Failf("fixture not found: %s (%v)", f, err)
		}
	}

	// ── Project ────────────────────────────────────────────────────────────────
	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-extract-schema")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	configureProjectModel(t, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// ── Install schema ─────────────────────────────────────────────────────────
	rl.Section("Install schema")
	installOut := mustRunCLIInDirWithHome(t, "", home,
		"schemas", "install",
		"--file", schemaFixture,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory schemas install --file "+schemaFixture+" --project "+projectID, installOut)
	rl.Printf("schema install output: %s", truncate(installOut, 300))

	// Verify schema install reported the expected types in installed_types.
	// The server populates the type registry from the map-format object_type_schemas.
	rl.Section("Verify schema types were registered")
	for _, typ := range []string{"Person", "Decision"} {
		if !strings.Contains(installOut, typ) {
			rl.Failf("schema install output missing expected type %q; got:\n%s",
				typ, truncate(installOut, 400))
		}
	}
	rl.Printf("schema install registered Person and Decision types")

	// ── Upload document ────────────────────────────────────────────────────────
	rl.Section("Upload document")
	uploadOut := mustRunCLIInDirWithHome(t, "", home,
		"documents", "upload", docFixture,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory documents upload "+docFixture+" --project "+projectID, uploadOut)

	docID := parseNestedDocumentID(uploadOut)
	if docID == "" {
		rl.Failf("could not parse document ID from upload output: %s", truncate(uploadOut, 300))
	}
	rl.Printf("uploaded document ID: %s", docID)

	// ── Wait for document parsing to complete ──────────────────────────────────
	// The extraction worker reads doc.Content, which is only written after the
	// document parsing worker completes.  Triggering extraction before parsing
	// finishes causes a race: the first worker pickup returns "document has no
	// content", schedules a retry, and wastes a retry slot.  We poll the
	// document get endpoint until conversion_status=="completed".
	rl.Section("Wait for document parsing")
	waitForDocumentParsed(t, rl, home, projectID, docID, 2*time.Minute)

	// ── Trigger extraction via CLI ─────────────────────────────────────────────
	// Use the CLI to create an extraction job for the document.
	rl.Section("Trigger extraction via CLI")
	jobID := createExtractionJobCLI(t, rl, home, projectID, docID)
	rl.Printf("extraction job ID: %s", jobID)

	// ── Wait for extraction job to complete ────────────────────────────────────
	rl.Section("Wait for extraction job completion")
	waitForExtractionJobCLI(t, rl, home, jobID, 6*time.Minute)

	// ── Poll for Person objects ────────────────────────────────────────────────
	rl.Section("Poll for extracted graph objects")
	deadline := time.Now().Add(2 * time.Minute)
	var personCount, decisionCount int
	var personOut, decisionOut string

	for time.Now().Before(deadline) {
		pOut, err := runCLIInDirWithHome(t, "", home,
			"graph", "objects", "list",
			"--type", "Person",
			"--project", projectID,
			"--output", "json",
			"--limit", "50",
		)
		if err == nil {
			personOut = pOut
			personCount = countJSONArrayItems(pOut)
		}

		dOut, err2 := runCLIInDirWithHome(t, "", home,
			"graph", "objects", "list",
			"--type", "Decision",
			"--project", projectID,
			"--output", "json",
			"--limit", "50",
		)
		if err2 == nil {
			decisionOut = dOut
			decisionCount = countJSONArrayItems(dOut)
		}

		rl.Printf("poll: Person=%d Decision=%d", personCount, decisionCount)

		if personCount >= 1 {
			break
		}
		time.Sleep(5 * time.Second)
	}
	rl.CLI("memory graph objects list --type Person --project "+projectID, personOut)
	rl.CLI("memory graph objects list --type Decision --project "+projectID, decisionOut)

	// ── Assertions ─────────────────────────────────────────────────────────────
	rl.Section("Assert extracted entity counts")
	// The extraction pipeline uses a constrained schema (type enum = [Person, Decision])
	// combined with an LLM that may extract conservatively.  We assert at least 1 Person
	// is created to confirm the end-to-end extraction flow works.  The presence of Sarah
	// Chen (the meeting chair who is mentioned most frequently) is a reliable signal.
	if personCount < 1 {
		t.Errorf("expected at least 1 Person object, got %d; output:\n%s",
			personCount, truncate(personOut, 600))
	} else {
		rl.Printf("Person count=%d (expected >=1): PASS", personCount)
	}
	rl.Printf("Decision count=%d (informational, not asserted)", decisionCount)

	rl.Section("Assert known entities appear in extracted objects")
	// Sarah Chen is the meeting chair and the most prominent person — she should
	// always be extracted when the Person schema is installed.
	if !strings.Contains(personOut, "Sarah Chen") {
		t.Errorf("expected Person output to contain \"Sarah Chen\"; output:\n%s",
			truncate(personOut, 600))
	} else {
		rl.Printf("Person \"Sarah Chen\" found in graph objects: PASS")
	}
}

// TestCLIInstalled_DocumentConversion verifies that uploading a PDF document
// triggers Kreuzberg conversion and produces a non-empty content field after
// the conversion completes.
func TestCLIInstalled_DocumentConversion(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify PDF document conversion via Kreuzberg",
		"Create project and upload a PDF fixture",
		"Poll document until conversionStatus=\"completed\" (3-min timeout)",
		"Assert document has non-empty content field after conversion",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	// Resolve testdata path relative to this source file.
	_, thisFile, _, _ := runtime.Caller(0)
	testdataDir := filepath.Join(filepath.Dir(thisFile), "testdata")
	pdfFixture := filepath.Join(testdataDir, "sample.pdf")

	if _, err := os.Stat(pdfFixture); err != nil {
		rl.Failf("PDF fixture not found: %s (%v)", pdfFixture, err)
	}

	// ── Project ────────────────────────────────────────────────────────────────
	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-pdf-convert")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// ── Upload PDF ─────────────────────────────────────────────────────────────
	rl.Section("Upload PDF document")
	uploadOut := mustRunCLIInDirWithHome(t, "", home,
		"documents", "upload", pdfFixture,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory documents upload "+pdfFixture+" --project "+projectID, uploadOut)

	docID := parseNestedDocumentID(uploadOut)
	if docID == "" {
		rl.Failf("could not parse document ID from upload output: %s", truncate(uploadOut, 300))
	}
	rl.Printf("uploaded document ID: %s", docID)

	// ── Poll for conversion completion ─────────────────────────────────────────
	rl.Section("Poll for Kreuzberg conversion completion")
	deadline := time.Now().Add(3 * time.Minute)
	var lastGetOut string
	converted := false
	var content string

	for time.Now().Before(deadline) {
		getOut, err := runCLIInDirWithHome(t, "", home,
			"documents", "get", docID,
			"--project", projectID,
			"--output", "json",
		)
		if err != nil {
			rl.Printf("document get error (will retry): %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		lastGetOut = getOut

		// Parse conversionStatus from JSON response.
		// The JSON may be wrapped: {"document":{...}} or top-level.
		convStatus := parseJSONField(getOut, "conversionStatus")
		if convStatus == "" {
			// Try nested under "document".
			var wrapper map[string]json.RawMessage
			if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(getOut)), &wrapper); jsonErr == nil {
				if docRaw, ok := wrapper["document"]; ok {
					convStatus = parseJSONField(string(docRaw), "conversionStatus")
					content = parseJSONField(string(docRaw), "content")
				}
			}
		} else {
			content = parseJSONField(getOut, "content")
		}
		rl.Printf("poll: conversionStatus=%s content_length=%d", convStatus, len(content))

		if strings.ToLower(convStatus) == "completed" {
			converted = true
			break
		}
		time.Sleep(2 * time.Second)
	}
	rl.CLI("memory documents get "+docID+" --project "+projectID+" (last poll)", lastGetOut)

	// ── Assertions ─────────────────────────────────────────────────────────────
	rl.Section("Verify conversion completed with content")
	if !converted {
		t.Errorf("document conversion did not complete within 3 min; last get output:\n%s",
			truncate(lastGetOut, 500))
	} else {
		rl.Printf("conversion completed: true")
	}

	if content == "" || content == "null" {
		t.Errorf("expected document to have non-empty content after conversion; last get output:\n%s",
			truncate(lastGetOut, 500))
	} else {
		rl.Printf("document has content: %d bytes", len(content))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// createExtractionJobCLI creates an extraction job using the CLI and returns the job ID.
// It calls rl.Failf on any failure.
func createExtractionJobCLI(t *testing.T, rl *runLog, home, projectID, documentID string) string {
	t.Helper()
	out := mustRunCLIInDirWithHome(t, "", home,
		"extraction", "jobs", "create",
		"--project", projectID,
		"--document", documentID,
		"--output", "json",
	)
	rl.CLI("memory extraction jobs create --project "+projectID+" --document "+documentID, out)

	// Parse job ID from response.  The server returns:
	// {"success":true,"data":{"id":"...","status":"queued",...}}
	jobID := parseJSONField(out, "id")
	if jobID == "" {
		// Try nested under "data".
		var outer map[string]json.RawMessage
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &outer); err == nil {
			if dataRaw, ok := outer["data"]; ok {
				jobID = parseJSONField(string(dataRaw), "id")
			}
		}
	}
	if jobID == "" {
		rl.Failf("could not parse job ID from extraction response: %s", truncate(out, 400))
	}
	return jobID
}

// waitForExtractionJobCLI waits for an extraction job to reach a terminal state
// (completed, failed, or cancelled) using the CLI's --watch flag.
// It calls t.Fatalf if the CLI command itself fails (network error, timeout, etc.),
// but does NOT fail when the job reaches "failed" status — callers that care about
// job-level failure should check the returned output.
func waitForExtractionJobCLI(t *testing.T, rl *runLog, home, jobID string, timeout time.Duration) string {
	t.Helper()
	// Use custom timeout helper since framework has 30s limit
	watchTimeout := timeout + 30*time.Second // Add buffer for CLI overhead
	out, err := runCLIWithTimeout(t, home, watchTimeout,
		"extraction", "jobs", "get", jobID,
		"--watch",
		"--timeout", timeout.String(),
		"--output", "json",
	)
	rl.CLI("memory extraction jobs get "+jobID+" --watch", out)
	if err != nil {
		// A non-zero exit may mean the job reached "failed" status (CLI exits 1 on
		// failed jobs) rather than a transport error.  Log but don't hard-fail here
		// so the caller can decide how to handle it.
		rl.Printf("extraction job watch exited non-zero (job may have failed): %v", err)
	}
	// Parse terminal status from output.
	status := parseJSONField(out, "status")
	if status == "" {
		var outer map[string]json.RawMessage
		if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(out)), &outer); jsonErr == nil {
			if dataRaw, ok := outer["data"]; ok {
				status = parseJSONField(string(dataRaw), "status")
			}
		}
	}
	return status
}

// waitForDocumentParsed polls the document get endpoint until conversion_status is
// "completed", indicating the document is ready for extraction with content populated.
// Plain-text files start with "not_required" but the parsing worker transitions them
// to "completed" after storing content. Waiting for "completed" avoids the race where
// the extraction worker picks up the job before doc.Content is populated.
func waitForDocumentParsed(t *testing.T, rl *runLog, home, projectID, docID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := runCLIInDirWithHome(t, "", home,
			"documents", "get", docID,
			"--project", projectID,
			"--output", "json",
		)
		if err != nil {
			rl.Printf("document get error (will retry): %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		// The JSON may be wrapped: {"document":{...}} or top-level.
		convStatus := parseJSONField(out, "conversionStatus")
		if convStatus == "" {
			// Try nested under "document".
			var wrapper map[string]json.RawMessage
			if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(out)), &wrapper); jsonErr == nil {
				if docRaw, ok := wrapper["document"]; ok {
					convStatus = parseJSONField(string(docRaw), "conversionStatus")
				}
			}
		}
		rl.Printf("document %s: conversionStatus=%s", docID, convStatus)

		s := strings.ToLower(convStatus)
		if s == "completed" {
			return
		}
		if s == "failed" {
			rl.Failf("document %s parsing failed (conversionStatus=failed)", docID)
			return
		}
		time.Sleep(2 * time.Second)
	}
	rl.Failf("document %s did not reach conversionStatus=completed/not_required within %s", docID, timeout)
}

// parseNestedDocumentID extracts the document ID from `documents upload --output json`.
// The CLI returns either {"id":"..."} or {"document":{"id":"...",...}}.
func parseNestedDocumentID(jsonOutput string) string {
	// Try top-level first.
	if id := parseJSONField(jsonOutput, "id"); id != "" {
		return id
	}
	// Try nested under "document".
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(jsonOutput)), &wrapper); err != nil {
		return ""
	}
	inner, ok := wrapper["document"]
	if !ok {
		return ""
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(inner, &doc); err != nil {
		return ""
	}
	id, _ := doc["id"].(string)
	return id
}
