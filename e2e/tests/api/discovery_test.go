// Package api_test — discovery_test.go
//
// Tests for the discovery jobs API (/api/discovery-jobs) and schema finalization.
// Covers: start job, get status, finalize (create/extend schema), verify schema installed.
package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// skipIfAPIReachable skips the test only if the server is completely unreachable
// (connection refused). An unhealthy health endpoint (503) due to storage being
// down is acceptable — DB and core API are still functional.
func skipIfAPIReachable(t *testing.T) {
	t.Helper()
	resp, err := http.Get(serverURL() + "/health")
	if err != nil {
		t.Skipf("server unreachable: %v", err)
	}
	resp.Body.Close()
	// 503 from storage is OK — skip only on hard unreachability (handled above).
}

// doDiscovery performs a JSON request sending the token as X-API-Key (standalone mode).
// The runlog DoJSON sends emt_-prefixed tokens as Bearer, which standalone doesn't accept.
func doDiscovery(t *testing.T, method, path, token, projectID string, body []byte) *http.Response {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, serverURL()+path, reqBody)
	if err != nil {
		t.Fatalf("doDiscovery: build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", token)
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("doDiscovery: %s %s: %v", method, path, err)
	}
	return resp
}

// mustStatusDiscovery reads body and asserts status code.
func mustStatusDiscovery(t *testing.T, resp *http.Response, want int) string {
	t.Helper()
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	body := string(b)
	if resp.StatusCode != want {
		t.Fatalf("expected status %d, got %d\n  body: %s", want, resp.StatusCode, body)
	}
	return body
}

// parseIDDiscovery extracts "id" or "schemaId"/"schema_id" from a JSON body.
func parseIDDiscovery(t *testing.T, body string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("parseIDDiscovery: %v\nbody: %s", err, body)
	}
	for _, k := range []string{"id", "job_id", "schemaId", "schema_id"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	t.Fatalf("parseIDDiscovery: no id field in: %s", body)
	return ""
}

// createDiscoveryProject creates a temporary org + project, returning projectID.
// projectInfo sets the KB purpose used by the discovery LLM prompt; pass a
// meaningful domain description for accurate type discovery.
func createDiscoveryProject(t *testing.T, token, projectInfo string) string {
	t.Helper()
	ts := time.Now().UnixMilli()

	// Create org.
	orgBody, _ := json.Marshal(map[string]any{"name": fmt.Sprintf("discovery-e2e-org-%d", ts)})
	orgResp := doDiscovery(t, "POST", "/api/orgs", token, "", orgBody)
	orgRaw := mustStatusDiscovery(t, orgResp, http.StatusCreated)
	orgID := parseIDDiscovery(t, orgRaw)
	t.Cleanup(func() { doDiscovery(t, "DELETE", "/api/orgs/"+orgID, token, "", nil) })

	// Create project with KB purpose so the discovery LLM gets domain context.
	projBody, _ := json.Marshal(map[string]any{
		"name":         fmt.Sprintf("discovery-e2e-proj-%d", ts),
		"orgId":        orgID,
		"project_info": projectInfo,
	})
	req, _ := http.NewRequest("POST", serverURL()+"/api/projects", bytes.NewReader(projBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", token)
	req.Header.Set("X-Org-ID", orgID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d\n  body: %s", resp.StatusCode, string(b))
	}
	projectID := parseIDDiscovery(t, string(b))
	t.Cleanup(func() { doDiscovery(t, "DELETE", "/api/projects/"+projectID, token, "", nil) })
	t.Logf("created project: %s (org: %s, purpose: %q)", projectID, orgID, projectInfo)
	// Configure model so discovery LLM calls succeed.
	configureProjectModel(t, projectID)
	return projectID
}

// assertNoReifiedTypes fails if any discovered type name looks like a reified
// relationship or event — concepts that should be modelled as graph edges, not nodes.
// Reification produces low-quality schemas where relationships become entity types.
func assertNoReifiedTypes(t *testing.T, discoveredTypes []map[string]any) {
	t.Helper()
	reified := regexp.MustCompile(`(?i)(Relationship|Event|Activity)$`)
	for _, dt := range discoveredTypes {
		name, _ := dt["type_name"].(string)
		if reified.MatchString(name) {
			t.Errorf("reified type discovered: %q — model converted a relationship/event into an entity type; check anti-reification prompt rule", name)
		}
	}
}

// createDiscoveryDocument uploads a text document and returns its ID.
func createDiscoveryDocument(t *testing.T, token, projectID, filename, content string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"filename": filename,
		"content":  content,
	})
	resp := doDiscovery(t, "POST", "/api/documents", token, projectID, body)
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	raw := string(b)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("createDiscoveryDocument: expected 200/201, got %d\n  body: %s", resp.StatusCode, raw)
	}
	docID := parseIDDiscovery(t, raw)
	t.Cleanup(func() { doDiscovery(t, "DELETE", "/api/documents/"+docID, token, projectID, nil) })
	t.Logf("created document: %s", docID)
	return docID
}

// pollJobCompletion polls until status is completed/failed or timeout.
// Returns (status, lastRawBody) — callers can inspect discovered_types from the body.
func pollJobCompletion(t *testing.T, token, projectID, jobID string, timeout time.Duration) (string, string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastStatus, lastRaw string
	for time.Now().Before(deadline) {
		r := doDiscovery(t, "GET", "/api/discovery-jobs/"+jobID, token, projectID, nil)
		lastRaw = mustStatusDiscovery(t, r, http.StatusOK)
		var m map[string]any
		json.Unmarshal([]byte(lastRaw), &m)
		lastStatus, _ = m["status"].(string)
		t.Logf("job %s status: %s", jobID, lastStatus)
		if lastStatus == "completed" || lastStatus == "failed" {
			return lastStatus, lastRaw
		}
		time.Sleep(3 * time.Second)
	}
	return lastStatus, lastRaw
}

// runDiscoveryJob starts a discovery job for docIDs, registers cleanup, polls to
// completion, and fails the test if it does not reach "completed".
// Returns (jobID, finalRawBody) — body contains discovered_types for assertions.
func runDiscoveryJob(t *testing.T, token, projectID string, docIDs []string) (string, string) {
	t.Helper()
	startBody, _ := json.Marshal(map[string]any{
		"document_ids":          docIDs,
		"batch_size":            1,
		"include_relationships": true,
	})
	startResp := doDiscovery(t, "POST",
		fmt.Sprintf("/api/discovery-jobs/projects/%s/start", projectID),
		token, projectID, startBody)
	startRaw := mustStatusDiscovery(t, startResp, http.StatusOK)
	jobID := parseIDDiscovery(t, startRaw)
	t.Logf("started discovery job: %s", jobID)
	t.Cleanup(func() {
		doDiscovery(t, "DELETE", "/api/discovery-jobs/"+jobID, token, projectID, nil)
	})

	status, raw := pollJobCompletion(t, token, projectID, jobID, 90*time.Second)
	if status != "completed" {
		t.Fatalf("job %s did not complete; last status: %s", jobID, status)
	}
	return jobID, raw
}

// finalizeDiscoverySchema POSTs a finalize request and returns (schemaID, message).
// req must include "mode", "packName", "includedTypes", and optionally "existingPackId".
func finalizeDiscoverySchema(t *testing.T, token, projectID, jobID string, req map[string]any) (schemaID, message string) {
	t.Helper()
	body, _ := json.Marshal(req)
	resp := doDiscovery(t, "POST", "/api/discovery-jobs/"+jobID+"/finalize",
		token, projectID, body)
	raw := mustStatusDiscovery(t, resp, http.StatusOK)
	t.Logf("finalize response: %s", raw)

	var m map[string]any
	json.Unmarshal([]byte(raw), &m)
	schemaID, _ = m["schema_id"].(string)
	message, _ = m["message"].(string)
	if schemaID == "" {
		t.Fatalf("finalizeDiscoverySchema: no schema_id in response: %s", raw)
	}
	return schemaID, message
}

// assertSchemaInstalled verifies schemaID appears in the project's installed schema list.
// Returns the matching pack entry for further assertions.
func assertSchemaInstalled(t *testing.T, token, projectID, schemaID string) map[string]any {
	t.Helper()
	resp := doDiscovery(t, "GET", "/api/schemas/projects/"+projectID+"/installed",
		token, projectID, nil)
	raw := mustStatusDiscovery(t, resp, http.StatusOK)
	var packs []map[string]any
	json.Unmarshal([]byte(raw), &packs)
	for _, pack := range packs {
		if sid, _ := pack["schemaId"].(string); sid == schemaID {
			return pack
		}
	}
	t.Errorf("schema %s not found in installed packs for project %s\npacks: %s",
		schemaID, projectID, raw)
	return nil
}

// installedPackCount returns the number of schemas installed in the project.
func installedPackCount(t *testing.T, token, projectID string) int {
	t.Helper()
	resp := doDiscovery(t, "GET", "/api/schemas/projects/"+projectID+"/installed",
		token, projectID, nil)
	raw := mustStatusDiscovery(t, resp, http.StatusOK)
	var packs []map[string]any
	json.Unmarshal([]byte(raw), &packs)
	return len(packs)
}

// ─────────────────────────────────────────────────────────────────────────────

// TestDiscovery_StartAndFinalizeSchema runs a full discovery flow:
//  1. Create a project + document
//  2. Start a discovery job and poll until complete
//  3. Assert no reified relationship/event types were discovered
//  4. Finalize with a schema (create mode)
//  5. Verify the schema was installed into the project
func TestDiscovery_StartAndFinalizeSchema(t *testing.T) {
	rl := newRunLog(t)
	_ = rl
	skipIfAPIReachable(t)
	skipIfNoLLMProvider(t)

	token := e2eTestToken()
	projectID := createDiscoveryProject(t, token,
		"Track people, organizations, and reporting relationships in a company org chart")

	docID := createDiscoveryDocument(t, token, projectID, "org-chart.txt",
		`Alice is the CEO of Acme Corp, headquartered in San Francisco.
Bob is a software engineer at Acme Corp. He reports to Alice.
Carol founded StartupXYZ in New York and serves as CTO.`)

	jobID, jobRaw := runDiscoveryJob(t, token, projectID, []string{docID})

	// Assert anti-reification: no type should be named *Relationship or *Event.
	var jobBody map[string]any
	json.Unmarshal([]byte(jobRaw), &jobBody)
	if rawTypes, ok := jobBody["discovered_types"].([]any); ok {
		types := make([]map[string]any, 0, len(rawTypes))
		for _, rt := range rawTypes {
			if m, ok := rt.(map[string]any); ok {
				types = append(types, m)
			}
		}
		assertNoReifiedTypes(t, types)
	}

	schemaID, _ := finalizeDiscoverySchema(t, token, projectID, jobID, map[string]any{
		"mode":     "create",
		"packName": fmt.Sprintf("e2e-discovery-schema-%d", time.Now().UnixMilli()),
		"includedTypes": []map[string]any{
			{"type_name": "Person", "description": "A human individual",
				"properties": map[string]any{"role": map[string]any{"type": "string"}}, "frequency": 2},
			{"type_name": "Organization", "description": "A company or institution",
				"properties": map[string]any{"industry": map[string]any{"type": "string"}}, "frequency": 2},
		},
		"includedRelationships": []map[string]any{
			{"source_type": "Person", "target_type": "Organization",
				"relation_type": "WORKS_AT", "description": "Person works at org", "cardinality": "many-to-one"},
		},
	})
	t.Logf("schema created: %s", schemaID)

	assertSchemaInstalled(t, token, projectID, schemaID)
}

// TestDiscovery_FinalizeWithoutOrgID verifies finalize works without X-Org-ID header
// (org resolved from project — core change from this refactor).
func TestDiscovery_FinalizeWithoutOrgID(t *testing.T) {
	rl := newRunLog(t)
	_ = rl
	skipIfAPIReachable(t)
	skipIfNoLLMProvider(t)

	token := e2eTestToken()
	projectID := createDiscoveryProject(t, token,
		"Track medical professionals, hospitals, and specializations for a healthcare directory")

	docID := createDiscoveryDocument(t, token, projectID, "notes.txt",
		"Dr. Smith works at General Hospital as a cardiologist.")

	jobID, _ := runDiscoveryJob(t, token, projectID, []string{docID})

	// Finalize — no X-Org-ID sent (doDiscovery only sets X-Project-ID).
	schemaID, _ := finalizeDiscoverySchema(t, token, projectID, jobID, map[string]any{
		"mode":     "create",
		"packName": fmt.Sprintf("e2e-no-org-schema-%d", time.Now().UnixMilli()),
		"includedTypes": []map[string]any{
			{"type_name": "Doctor", "description": "A medical professional",
				"properties": map[string]any{}, "frequency": 1},
		},
		"includedRelationships": []map[string]any{},
	})
	t.Logf("schema created without X-Org-ID: %s", schemaID)
}

// TestDiscovery_ExtendExistingSchema verifies the extend mode:
// Run 1 creates a schema; Run 2 extends the same schema with new types.
// Asserts: same schemaID returned, message says "Extended", no duplicate installation.
func TestDiscovery_ExtendExistingSchema(t *testing.T) {
	rl := newRunLog(t)
	_ = rl
	skipIfAPIReachable(t)
	skipIfNoLLMProvider(t)

	token := e2eTestToken()
	projectID := createDiscoveryProject(t, token,
		"Track products and product categories in an e-commerce catalog with pricing and SKU data")

	// ── Run 1: create schema from an org-chart document ──────────────────────
	doc1ID := createDiscoveryDocument(t, token, projectID, "org-chart.txt",
		`Alice is the CEO of Acme Corp, headquartered in San Francisco.
Bob is a software engineer at Acme Corp. He reports to Alice.
Carol founded StartupXYZ in New York and serves as CTO.`)

	job1ID, _ := runDiscoveryJob(t, token, projectID, []string{doc1ID})

	schemaID, msg1 := finalizeDiscoverySchema(t, token, projectID, job1ID, map[string]any{
		"mode":     "create",
		"packName": fmt.Sprintf("e2e-extend-schema-%d", time.Now().UnixMilli()),
		"includedTypes": []map[string]any{
			{"type_name": "Person", "description": "A human individual",
				"properties": map[string]any{"role": map[string]any{"type": "string"}}, "frequency": 2},
			{"type_name": "Organization", "description": "A company or institution",
				"properties": map[string]any{"industry": map[string]any{"type": "string"}}, "frequency": 2},
		},
		"includedRelationships": []map[string]any{
			{"source_type": "Person", "target_type": "Organization",
				"relation_type": "WORKS_AT", "description": "Person works at org", "cardinality": "many-to-one"},
		},
	})
	t.Logf("run 1: schema created: %s (message: %s)", schemaID, msg1)

	assertSchemaInstalled(t, token, projectID, schemaID)
	countAfterRun1 := installedPackCount(t, token, projectID)

	// ── Run 2: extend the same schema from a product catalog document ─────────
	doc2ID := createDiscoveryDocument(t, token, projectID, "products.txt",
		`Widget Pro is a product in the Electronics category, priced at $49.99.
Gadget Mini belongs to the Accessories category and weighs 120g.
SuperGrip is a tool in the Hardware category with SKU TL-991.`)

	job2ID, _ := runDiscoveryJob(t, token, projectID, []string{doc2ID})

	schemaID2, msg2 := finalizeDiscoverySchema(t, token, projectID, job2ID, map[string]any{
		"mode":           "extend",
		"packName":       fmt.Sprintf("e2e-extend-schema-%d", time.Now().UnixMilli()),
		"existingPackId": schemaID,
		"includedTypes": []map[string]any{
			{"type_name": "Product", "description": "A purchasable item",
				"properties": map[string]any{"price": map[string]any{"type": "number"}}, "frequency": 3},
			{"type_name": "Category", "description": "A product classification",
				"properties": map[string]any{"name": map[string]any{"type": "string"}}, "frequency": 3},
		},
		"includedRelationships": []map[string]any{
			{"source_type": "Product", "target_type": "Category",
				"relation_type": "BELONGS_TO", "description": "Product belongs to category", "cardinality": "many-to-one"},
		},
	})
	t.Logf("run 2: schema returned: %s (message: %s)", schemaID2, msg2)

	// Same schema updated — not a new one.
	if schemaID2 != schemaID {
		t.Errorf("extend mode returned different schemaID: got %s, want %s", schemaID2, schemaID)
	}

	// Message should say Extended.
	if !strings.Contains(strings.ToLower(msg2), "extended") {
		t.Errorf("expected message to contain 'extended', got: %s", msg2)
	}

	// Schema still present in installed list.
	assertSchemaInstalled(t, token, projectID, schemaID)

	// No duplicate installation — count must not grow.
	countAfterRun2 := installedPackCount(t, token, projectID)
	if countAfterRun2 != countAfterRun1 {
		t.Errorf("installed pack count changed after extend: was %d, now %d (duplicate install?)",
			countAfterRun1, countAfterRun2)
	}
}
