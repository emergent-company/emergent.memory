// Package api_test — helpers_test.go
//
// Shared helpers for all API tests: HTTP client wrappers, fixture creation,
// and teardown utilities.
package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// ─────────────────────────────────────────────────────────────────────────────
// Type aliases
// ─────────────────────────────────────────────────────────────────────────────

type runLog = framework.RunLog

// ─────────────────────────────────────────────────────────────────────────────
// RunLog helpers
// ─────────────────────────────────────────────────────────────────────────────

func newRunLog(t *testing.T) *runLog {
	t.Helper()
	return framework.NewRunLog(t)
}

// ─────────────────────────────────────────────────────────────────────────────
// Server helpers
// ─────────────────────────────────────────────────────────────────────────────

func serverURL() string    { return framework.ServerURL() }
func e2eTestToken() string { return framework.E2ETestToken() }

func skipIfServerDown(t *testing.T, rl *runLog) {
	t.Helper()
	framework.SkipIfServerDown(t, rl)
}

// skipIfStandaloneMode skips tests that require scope enforcement,
// which standalone mode does not implement.
func skipIfStandaloneMode(t *testing.T) {
	t.Helper()
	if framework.AuthMode() == "standalone" {
		t.Skip("skipping scope-enforcement test in standalone mode")
	}
}

// skipIfNoLLMProvider skips tests that require a real LLM call when no provider is configured.
func skipIfNoLLMProvider(t *testing.T) {
	t.Helper()
	provider, _, model := framework.ProviderFromEnv()
	if provider == "" || model == "" {
		t.Skip("skipping LLM test: no provider configured")
	}
}

// apiURL returns the full URL for the given API path.
func apiURL(path string) string {
	return serverURL() + path
}

// configureProjectModel sets a generative model on the project using env-configured provider.
// Also sets the project-level provider config with API key when available.
// orgID is the organization that owns the project; pass empty string to skip provider config.
// No-op when provider env vars are absent.
func configureProjectModel(t *testing.T, projectID string, orgIDs ...string) {
	t.Helper()
	provider, apiKey, model := framework.ProviderFromEnv()
	if provider == "" || model == "" {
		return
	}
	cfg, _ := json.Marshal(map[string]any{"generativeModel": provider + "/" + model})
	resp := doAPI(t, "PUT", "/api/v1/projects/"+projectID+"/model-config", e2eTestToken(), projectID, cfg)
	resp.Body.Close()

	// Also set the project-level provider config with API key so extraction/LLM calls work.
	if apiKey != "" && (provider == "deepseek" || provider == "openai") {
		orgID := ""
		if len(orgIDs) > 0 && orgIDs[0] != "" {
			orgID = orgIDs[0]
		}
		if orgID == "" {
			return
		}
		provCfg, _ := json.Marshal(map[string]any{"apiKey": apiKey, "generativeModel": model})
		resp2 := doAPIWithOrg(t, "PUT", fmt.Sprintf("/api/v1/projects/%s/providers/%s", projectID, provider),
			e2eTestToken(), projectID, orgID, provCfg)
		resp2.Body.Close()
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP helpers
// ─────────────────────────────────────────────────────────────────────────────

// doAPI performs a JSON HTTP request against the server.
// token is the Bearer token, projectID is the X-Project-ID header (may be empty).
func doAPI(t *testing.T, method, path, token, projectID string, body []byte) *http.Response {
	t.Helper()
	return framework.DoJSON(t, method, apiURL(path), token, projectID, body)
}

// doAPILogged is like doAPI but also emits an http_call event with full
// request/response details to the runlog. The response body is preserved
// for subsequent mustStatus / parseBodyJSON calls.
func doAPILogged(t *testing.T, rl *framework.RunLog, method, path, token, projectID string, reqBody []byte) *http.Response {
	t.Helper()
	resp := doAPI(t, method, path, token, projectID, reqBody)

	details := map[string]any{
		"method":      method,
		"url":         apiURL(path),
		"status_code": resp.StatusCode,
	}
	if projectID != "" {
		details["project_id"] = projectID
	}
	if len(reqBody) > 0 {
		var parsed any
		if json.Unmarshal(reqBody, &parsed) == nil {
			details["request_body"] = parsed
		}
	}

	// Read body for logging, then re-create it so mustStatus can use it.
	bodyStr := readBody(t, resp)
	var parsed any
	if json.Unmarshal([]byte(bodyStr), &parsed) == nil {
		details["response_body"] = parsed
	} else if len(bodyStr) < 500 {
		details["response_body"] = bodyStr
	}
	resp.Body = io.NopCloser(strings.NewReader(bodyStr))

	rl.Event("http_call", fmt.Sprintf("%s %s", method, path), details)
	return resp
}

// doAPIWithOrg performs a JSON HTTP request adding both X-Project-ID and X-Org-ID headers.
func doAPIWithOrg(t *testing.T, method, path, token, projectID, orgID string, body []byte) *http.Response {
	t.Helper()

	req, err := http.NewRequest(method, apiURL(path), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create request %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	framework.SetAuthHeader(req, token)
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}
	if orgID != "" {
		req.Header.Set("X-Org-ID", orgID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, path, err)
	}
	return resp
}

// readBody reads and returns the response body as a string, closing the body.
func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	return framework.ReadBody(t, resp)
}

// mustStatus asserts that the response has the expected status code.
// It reads and returns the body string for use in further assertions.
func mustStatus(t *testing.T, resp *http.Response, want int) string {
	t.Helper()
	body := readBody(t, resp)
	if resp.StatusCode != want {
		t.Errorf("expected status %d, got %d\nbody: %s", want, resp.StatusCode, body)
	}
	return body
}

// parseBodyJSON reads the response body and unmarshals it into v.
func parseBodyJSON(t *testing.T, body string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), v); err != nil {
		t.Fatalf("unmarshal response JSON: %v\nbody: %s", err, body)
	}
}

// parseID reads the "id" field from a JSON response body string.
func parseIDFromBody(t *testing.T, body string) string {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("parse id: unmarshal: %v\nbody: %s", err, body)
	}
	id, _ := result["id"].(string)
	if id == "" {
		t.Fatalf("parse id: no 'id' field in response\nbody: %s", body)
	}
	return id
}

// jsonBody marshals v to JSON bytes. Panics on error (test helper).
func jsonBody(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("jsonBody: %v", err))
	}
	return b
}

// readRespBody reads the full body from an http.Response without closing it via readBody.
func readRespBody(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return string(b)
}

// ─────────────────────────────────────────────────────────────────────────────
// Fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// createOrg creates an organization via POST /api/orgs and registers t.Cleanup
// to delete it. Returns the org ID.
func createOrg(t *testing.T, name string) string {
	t.Helper()
	token := e2eTestToken()

	resp := doAPI(t, "POST", "/api/orgs", token, "", jsonBody(map[string]any{"name": name}))
	body := mustStatus(t, resp, http.StatusCreated)
	orgID := parseIDFromBody(t, body)

	t.Cleanup(func() { deleteOrg(t, orgID) })
	return orgID
}

// deleteOrg deletes an organization. Safe for t.Cleanup (does not fail the test).
func deleteOrg(t *testing.T, orgID string) {
	t.Helper()
	token := e2eTestToken()
	resp := doAPI(t, "DELETE", "/api/orgs/"+orgID, token, "", nil)
	resp.Body.Close()
}

// createProject creates a project in the given org via POST /api/projects.
// Returns the project ID.
func createProject(t *testing.T, orgID, name string) string {
	t.Helper()
	token := e2eTestToken()

	resp := doAPIWithOrg(t, "POST", "/api/projects", token, "", orgID,
		jsonBody(map[string]any{"name": name, "orgId": orgID}))
	body := mustStatus(t, resp, http.StatusCreated)
	return parseIDFromBody(t, body)
}

// setupProject creates an org + project and registers cleanup of the org.
// Returns (projectID, orgID). Use this as the standard per-test fixture setup.
func setupProject(t *testing.T) (projectID, orgID string) {
	t.Helper()
	ts := time.Now().UnixMilli()
	orgID = createOrg(t, fmt.Sprintf("e2e-api-%d", ts))
	projectID = createProject(t, orgID, fmt.Sprintf("e2e-project-%d", ts))
	return projectID, orgID
}

// setupProjectLogged is like setupProject but also emits LogStep events
// with project and org IDs in details.
func setupProjectLogged(t *testing.T, rl *framework.RunLog) (projectID, orgID string) {
	t.Helper()
	projectID, orgID = setupProject(t)
	rl.LogStep("setup project", map[string]any{
		"project_id": projectID,
		"org_id":     orgID,
	})
	return projectID, orgID
}

// uniqueName returns a unique name with the given prefix using a millisecond timestamp.
func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixMilli())
}

// ─────────────────────────────────────────────────────────────────────────────
// Document helpers
// ─────────────────────────────────────────────────────────────────────────────

// createDocument creates a document via POST /api/documents and registers
// t.Cleanup to delete it. Returns the document ID.
// Accepts both 200 (deduplicated) and 201 (created).
func createDocument(t *testing.T, projectID, filename, content string) string {
	t.Helper()
	token := e2eTestToken()
	body := jsonBody(map[string]any{"filename": filename, "content": content})
	resp := doAPI(t, "POST", "/api/documents", token, projectID, body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b := readRespBody(resp)
		t.Fatalf("createDocument: expected 200 or 201, got %d: %s", resp.StatusCode, b)
	}
	b := readRespBody(resp)
	docID := parseIDFromBody(t, b)
	t.Cleanup(func() { deleteDocument(t, projectID, docID) })
	return docID
}

// deleteDocument deletes a document. Safe for t.Cleanup (does not fail the test).
func deleteDocument(t *testing.T, projectID, docID string) {
	t.Helper()
	resp := doAPI(t, "DELETE", "/api/documents/"+docID, e2eTestToken(), projectID, nil)
	resp.Body.Close()
}

// ─────────────────────────────────────────────────────────────────────────────
// Multipart helpers
// ─────────────────────────────────────────────────────────────────────────────

// doRawRequest sends an HTTP request with an explicit content type and body.
func doRawRequest(t *testing.T, method, path, token, projectID, contentType string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, apiURL(path), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("doRawRequest: %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", contentType)
	framework.SetAuthHeader(req, token)
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("doRawRequest: do %s %s: %v", method, path, err)
	}
	return resp
}

// doMultipartWithFile sends a multipart/form-data POST with a single file.
func doMultipartWithFile(t *testing.T, path, token, projectID, fieldName, filename string, fileContent []byte) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatalf("doMultipartWithFile: create form file: %v", err)
	}
	if _, err = fw.Write(fileContent); err != nil {
		t.Fatalf("doMultipartWithFile: write: %v", err)
	}
	w.Close()
	return doRawRequest(t, "POST", path, token, projectID, w.FormDataContentType(), buf.Bytes())
}

// doMultipartWithFiles sends a multipart/form-data POST with multiple files
// all under the same field name (for batch upload).
func doMultipartWithFiles(t *testing.T, path, token, projectID, fieldName string, files map[string][]byte) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for name, content := range files {
		fw, err := w.CreateFormFile(fieldName, name)
		if err != nil {
			t.Fatalf("doMultipartWithFiles: create form file %s: %v", name, err)
		}
		if _, err = fw.Write(content); err != nil {
			t.Fatalf("doMultipartWithFiles: write %s: %v", name, err)
		}
	}
	w.Close()
	return doRawRequest(t, "POST", path, token, projectID, w.FormDataContentType(), buf.Bytes())
}

// doMultipartWithField sends a multipart/form-data POST with a single text field
// and no file (used to test missing-file validation).
func doMultipartWithField(t *testing.T, path, token, projectID, fieldName, fieldValue string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField(fieldName, fieldValue); err != nil {
		t.Fatalf("doMultipartWithField: write field: %v", err)
	}
	w.Close()
	return doRawRequest(t, "POST", path, token, projectID, w.FormDataContentType(), buf.Bytes())
}

// ─────────────────────────────────────────────────────────────────────────────
// Chunk helpers
// ─────────────────────────────────────────────────────────────────────────────

// waitForChunks polls GET /api/chunks?documentId=... until at least one chunk
// appears or the timeout elapses. Returns the chunk IDs found (may be empty).
func waitForChunks(t *testing.T, projectID, docID string, timeout time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp := doAPI(t, "GET", "/api/chunks?documentId="+docID, e2eTestToken(), projectID, nil)
		body := mustStatus(t, resp, http.StatusOK)
		var result struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		parseBodyJSON(t, body, &result)
		if len(result.Data) > 0 {
			ids := make([]string, len(result.Data))
			for i, c := range result.Data {
				ids[i] = c.ID
			}
			return ids
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// String assertion helpers
// ─────────────────────────────────────────────────────────────────────────────

// assertContains fails the test if s does not contain substr.
func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected %q to contain %q", s, substr)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Graph helpers
// ─────────────────────────────────────────────────────────────────────────────

// createGraphObject creates a graph object via POST /api/graph/objects and
// registers t.Cleanup to delete it. Returns the object ID.
func createGraphObject(t *testing.T, projectID, objectType, name string, properties map[string]any) string {
	t.Helper()
	token := e2eTestToken()

	if properties == nil {
		properties = map[string]any{}
	}
	if name != "" {
		properties["name"] = name
	}

	body := jsonBody(map[string]any{
		"type":       objectType,
		"properties": properties,
	})
	resp := doAPI(t, "POST", "/api/graph/objects", token, projectID, body)
	b := mustStatus(t, resp, http.StatusCreated)
	objID := parseIDFromBody(t, b)
	t.Cleanup(func() { deleteGraphObject(t, projectID, objID) })
	return objID
}

// deleteGraphObject deletes a graph object. Safe for t.Cleanup (does not fail the test).
func deleteGraphObject(t *testing.T, projectID, objectID string) {
	t.Helper()
	token := e2eTestToken()
	resp := doAPI(t, "DELETE", "/api/graph/objects/"+objectID, token, projectID, nil)
	resp.Body.Close()
}

// createRelationship creates a graph relationship via POST /api/graph/relationships
// and registers t.Cleanup to delete it. Returns the relationship ID.
func createRelationship(t *testing.T, projectID, fromID, toID, relType string, properties map[string]any) string {
	t.Helper()
	token := e2eTestToken()

	body := map[string]any{
		"type":   relType,
		"src_id": fromID,
		"dst_id": toID,
	}
	if properties != nil {
		body["properties"] = properties
	}

	resp := doAPI(t, "POST", "/api/graph/relationships", token, projectID, jsonBody(body))
	b := mustStatus(t, resp, http.StatusCreated)
	relID := parseIDFromBody(t, b)
	t.Cleanup(func() { deleteRelationship(t, projectID, relID) })
	return relID
}

// deleteRelationship deletes a graph relationship. Safe for t.Cleanup (does not fail the test).
func deleteRelationship(t *testing.T, projectID, relID string) {
	t.Helper()
	token := e2eTestToken()
	resp := doAPI(t, "DELETE", "/api/graph/relationships/"+relID, token, projectID, nil)
	resp.Body.Close()
}
