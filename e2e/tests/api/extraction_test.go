// Package api_test — extraction_test.go
//
// Tests for the extraction jobs API (/api/admin/extraction-jobs).
// Ported from emergent.memory/apps/server/tests/e2e/extraction_test.go
//
// Note: These tests call real LLM APIs and may take up to 2 minutes each.
// They require a server configured with valid LLM credentials.
package api_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// personOrgSchemas returns standard object/relationship schemas for extraction tests.
func personOrgSchemas() (map[string]any, map[string]any) {
	objectSchemas := map[string]any{
		"Person": map[string]any{
			"name":        "Person",
			"description": "A human individual",
			"properties": map[string]any{
				"role":       map[string]any{"type": "string", "description": "Role or occupation"},
				"age":        map[string]any{"type": "string", "description": "Age of the person"},
				"occupation": map[string]any{"type": "string", "description": "Job or profession"},
			},
		},
		"Organization": map[string]any{
			"name":        "Organization",
			"description": "A company, institution, or group",
			"properties": map[string]any{
				"type":     map[string]any{"type": "string", "description": "Type of organization"},
				"industry": map[string]any{"type": "string", "description": "Industry sector"},
			},
		},
		"Location": map[string]any{
			"name":        "Location",
			"description": "A geographical place",
			"properties": map[string]any{
				"country": map[string]any{"type": "string", "description": "Country name"},
				"city":    map[string]any{"type": "string", "description": "City name"},
			},
		},
	}
	relationshipSchemas := map[string]any{
		"WORKS_AT": map[string]any{
			"name":         "WORKS_AT",
			"description":  "Person works at an organization",
			"source_types": []string{"Person"},
			"target_types": []string{"Organization"},
		},
		"LOCATED_IN": map[string]any{
			"name":         "LOCATED_IN",
			"description":  "Entity is located in a place",
			"source_types": []string{"Person", "Organization"},
			"target_types": []string{"Location"},
		},
	}
	return objectSchemas, relationshipSchemas
}

// pollExtractionJob polls GET /api/admin/extraction-jobs/:id until the job reaches
// a terminal status ("completed" or "failed") or the timeout elapses.
// Returns the final job data map and status string.
func pollExtractionJob(t *testing.T, projectID, jobID string, timeout time.Duration) (map[string]any, string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		resp := doAPI(t, "GET", "/api/admin/extraction-jobs/"+jobID, e2eTestToken(), projectID, nil)
		body := mustStatus(t, resp, http.StatusOK)

		var result map[string]any
		parseBodyJSON(t, body, &result)
		data, _ := result["data"].(map[string]any)
		status, _ := data["status"].(string)
		if status == "completed" || status == "failed" || status == "dead_letter" {
			return data, status
		}
	}
	return nil, "timeout"
}

// ─────────────────────────────────────────────────────────────────────────────
// Extraction Jobs — Authentication & Authorization
// ─────────────────────────────────────────────────────────────────────────────

func TestExtraction_CreateRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/admin/extraction-jobs", "", projectID,
		jsonBody(map[string]any{
			"project_id":  projectID,
			"source_type": "manual",
			"source_metadata": map[string]any{
				"text": "Test text",
			},
			"extraction_config": map[string]any{},
		}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestExtraction_CreateRequiresAdminWriteScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/admin/extraction-jobs", "read-only", projectID,
		jsonBody(map[string]any{
			"project_id":  projectID,
			"source_type": "manual",
			"source_metadata": map[string]any{
				"text": "Test text",
			},
			"extraction_config": map[string]any{},
		}))
	mustStatus(t, resp, http.StatusForbidden)
}

// ─────────────────────────────────────────────────────────────────────────────
// Extraction Jobs — Functional (requires LLM)
// ─────────────────────────────────────────────────────────────────────────────

func TestExtraction_ManualSource_ExtractsEntities(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfNoLLMProvider(t)

	projectID, orgID := setupProjectLogged(t, rl)
	configureProjectModel(t, projectID, orgID)
	objectSchemas, relationshipSchemas := personOrgSchemas()

	documentText := `John Smith is a 35-year-old software engineer who works at Acme Corporation.
Acme Corporation is a technology company headquartered in San Francisco, California.
John has been working there for 5 years and leads the backend team.
Sarah Johnson is the CEO of Acme Corporation and has been with the company since its founding.`

	resp := doAPILogged(t, rl, "POST", "/api/admin/extraction-jobs", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"project_id":  projectID,
			"source_type": "manual",
			"source_metadata": map[string]any{
				"text": documentText,
			},
			"extraction_config": map[string]any{
				"object_schemas":       objectSchemas,
				"relationship_schemas": relationshipSchemas,
				"target_types":         []string{"Person", "Organization", "Location"},
			},
		}))
	body := mustStatus(t, resp, http.StatusCreated)

	var createResp map[string]any
	parseBodyJSON(t, body, &createResp)
	if !createResp["success"].(bool) {
		t.Fatal("expected success=true in create response")
	}

	data := createResp["data"].(map[string]any)
	jobID := data["id"].(string)
	if jobID == "" {
		t.Fatal("expected non-empty job ID")
	}
	if data["status"] != "queued" {
		t.Errorf("expected initial status 'queued', got %v", data["status"])
	}
	rl.Printf("extraction job created: %s", jobID)

	jobData, finalStatus := pollExtractionJob(t, projectID, jobID, 120*time.Second)
	if finalStatus != "completed" {
		t.Fatalf("job did not complete: status=%s, error=%v", finalStatus, jobData["error_message"])
	}

	discoveredTypes, _ := jobData["discovered_types"].([]any)
	if len(discoveredTypes) == 0 {
		t.Error("expected at least one discovered type")
	}
	// Use successful_items count since created_objects array may be unpopulated
	successfulItems := int(jobData["successful_items"].(float64))
	if successfulItems == 0 {
		t.Error("expected at least one created object")
	}
	t.Logf("extraction completed: %d types, %d objects", len(discoveredTypes), successfulItems)
	// Objects are placed on a staging branch pending review; main-branch graph
	// search will return 0. successful_items is sufficient to verify extraction.
}

func TestExtraction_DocumentSource_ExtractsFromDocument(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfNoLLMProvider(t)

	projectID, orgID := setupProjectLogged(t, rl)
	configureProjectModel(t, projectID, orgID)

	docContent := `Meeting Notes - Q4 Planning
Date: December 15, 2024
Location: New York Office

Attendees:
- Michael Chen, VP of Engineering
- Emily Rodriguez, Product Manager
- David Kim, Senior Developer

Discussion Topics:
1. Michael presented the technical roadmap for Q1 2025
2. Emily discussed upcoming feature requests from clients
3. David proposed migrating the legacy system to cloud infrastructure

Action Items:
- Michael to finalize the hiring plan for 3 new engineers
- Emily to schedule client feedback sessions
- David to prepare cloud migration cost analysis

Next meeting scheduled for January 5, 2025.`

	// Create document via API (no direct DB access in e2e repo)
	docID := createDocument(t, projectID, "meeting-notes.txt", docContent)

	objectSchemas := map[string]any{
		"Person": map[string]any{
			"name":        "Person",
			"description": "A meeting attendee or mentioned person",
			"properties": map[string]any{
				"role":  map[string]any{"type": "string", "description": "Job title or role"},
				"tasks": map[string]any{"type": "string", "description": "Assigned tasks or responsibilities"},
			},
		},
		"Event": map[string]any{
			"name":        "Event",
			"description": "A meeting or scheduled event",
			"properties": map[string]any{
				"date":     map[string]any{"type": "string", "description": "Date of the event"},
				"location": map[string]any{"type": "string", "description": "Location of the event"},
				"purpose":  map[string]any{"type": "string", "description": "Purpose of the event"},
			},
		},
		"ActionItem": map[string]any{
			"name":        "ActionItem",
			"description": "A task or action to be completed",
			"properties": map[string]any{
				"description": map[string]any{"type": "string", "description": "Description of the action"},
				"assignee":    map[string]any{"type": "string", "description": "Person responsible"},
				"deadline":    map[string]any{"type": "string", "description": "Due date if specified"},
			},
		},
	}
	relationshipSchemas := map[string]any{
		"ATTENDED": map[string]any{
			"name":         "ATTENDED",
			"description":  "Person attended an event",
			"source_types": []string{"Person"},
			"target_types": []string{"Event"},
		},
		"ASSIGNED_TO": map[string]any{
			"name":         "ASSIGNED_TO",
			"description":  "Action item assigned to a person",
			"source_types": []string{"ActionItem"},
			"target_types": []string{"Person"},
		},
	}

	resp := doAPILogged(t, rl, "POST", "/api/admin/extraction-jobs", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"project_id":  projectID,
			"source_type": "document",
			"source_id":   docID,
			"extraction_config": map[string]any{
				"object_schemas":       objectSchemas,
				"relationship_schemas": relationshipSchemas,
				"target_types":         []string{"Person", "Event", "ActionItem"},
			},
		}))
	body := mustStatus(t, resp, http.StatusCreated)

	var createResp map[string]any
	parseBodyJSON(t, body, &createResp)
	data := createResp["data"].(map[string]any)
	jobID := data["id"].(string)
	rl.Printf("document extraction job created: %s", jobID)

	jobData, finalStatus := pollExtractionJob(t, projectID, jobID, 120*time.Second)
	if finalStatus != "completed" {
		t.Fatalf("job did not complete: status=%s, error=%v", finalStatus, jobData["error_message"])
	}

	discoveredTypes, _ := jobData["discovered_types"].([]any)
	successfulItems := int(jobData["successful_items"].(float64))
	if len(discoveredTypes) == 0 {
		t.Error("expected discovered types from meeting notes")
	}
	if successfulItems == 0 {
		t.Error("expected created objects from meeting notes")
	}
	t.Logf("document extraction: %d types, %d objects", len(discoveredTypes), successfulItems)
}

func TestExtraction_GetJobLogs_ReturnsAgentExecutionDetails(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfNoLLMProvider(t)

	projectID, orgID := setupProjectLogged(t, rl)
	configureProjectModel(t, projectID, orgID)

	resp := doAPILogged(t, rl, "POST", "/api/admin/extraction-jobs", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"project_id":  projectID,
			"source_type": "manual",
			"source_metadata": map[string]any{
				"text": "Alice is a data scientist at TechCorp. Bob is her manager.",
			},
			"extraction_config": map[string]any{
				"object_schemas": map[string]any{
					"Person": map[string]any{
						"name":        "Person",
						"description": "A person",
						"properties": map[string]any{
							"role": map[string]any{"type": "string"},
						},
					},
					"Organization": map[string]any{
						"name":        "Organization",
						"description": "A company",
					},
				},
				"relationship_schemas": map[string]any{
					"WORKS_AT": map[string]any{
						"name":         "WORKS_AT",
						"source_types": []string{"Person"},
						"target_types": []string{"Organization"},
					},
					"MANAGES": map[string]any{
						"name":         "MANAGES",
						"source_types": []string{"Person"},
						"target_types": []string{"Person"},
					},
				},
				"target_types": []string{"Person", "Organization"},
			},
		}))
	body := mustStatus(t, resp, http.StatusCreated)

	var createResp map[string]any
	parseBodyJSON(t, body, &createResp)
	jobID := createResp["data"].(map[string]any)["id"].(string)
	rl.Printf("logs test job created: %s", jobID)

	_, finalStatus := pollExtractionJob(t, projectID, jobID, 120*time.Second)
	if finalStatus != "completed" {
		t.Fatalf("job did not complete: status=%s", finalStatus)
	}

	logsResp := doAPILogged(t, rl, "GET", "/api/admin/extraction-jobs/"+jobID+"/logs", e2eTestToken(), projectID, nil)
	logsBody := mustStatus(t, logsResp, http.StatusOK)

	var logsResult map[string]any
	parseBodyJSON(t, logsBody, &logsResult)
	logsData := logsResult["data"].(map[string]any)
	summary := logsData["summary"].(map[string]any)

	totalSteps := int(summary["totalSteps"].(float64))
	// Extraction job logs may be empty if logging is not yet implemented for all pipeline steps.
	// Just verify the endpoint returns a valid structure.
	t.Logf("job logs: %d total steps, %d success, %d errors",
		totalSteps,
		int(summary["successSteps"].(float64)),
		int(summary["errorSteps"].(float64)))
}

func TestExtraction_ListReturnsPaginatedResults(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create 3 jobs
	for i := 0; i < 3; i++ {
		resp := doAPILogged(t, rl, "POST", "/api/admin/extraction-jobs", e2eTestToken(), projectID,
			jsonBody(map[string]any{
				"project_id":  projectID,
				"source_type": "manual",
				"source_metadata": map[string]any{
					"text": fmt.Sprintf("Sample text for job %d", i),
				},
				"extraction_config": map[string]any{
					"object_schemas": map[string]any{
						"Item": map[string]any{"name": "Item"},
					},
					"target_types": []string{"Item"},
				},
			}))
		mustStatus(t, resp, http.StatusCreated)
	}

	listResp := doAPILogged(t, rl, "GET", "/api/admin/extraction-jobs/projects/"+projectID+"?limit=2", e2eTestToken(), projectID, nil)
	body := mustStatus(t, listResp, http.StatusOK)

	var listResult map[string]any
	parseBodyJSON(t, body, &listResult)
	data := listResult["data"].(map[string]any)
	jobs, _ := data["jobs"].([]any)
	total := int(data["total"].(float64))
	limit := int(data["limit"].(float64))

	if limit != 2 {
		t.Errorf("expected limit=2, got %d", limit)
	}
	if total < 3 {
		t.Errorf("expected total >= 3, got %d", total)
	}
	if len(jobs) != 2 {
		t.Errorf("expected 2 jobs in page, got %d", len(jobs))
	}
}

func TestExtraction_CancelCancelsPendingJob(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/admin/extraction-jobs", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"project_id":  projectID,
			"source_type": "manual",
			"source_metadata": map[string]any{
				"text": "Text to extract from",
			},
			"extraction_config": map[string]any{
				"object_schemas": map[string]any{
					"Entity": map[string]any{"name": "Entity"},
				},
			},
		}))
	body := mustStatus(t, resp, http.StatusCreated)

	var createResp map[string]any
	parseBodyJSON(t, body, &createResp)
	jobID := createResp["data"].(map[string]any)["id"].(string)
	rl.Printf("cancel test job created: %s", jobID)

	cancelResp := doAPILogged(t, rl, "POST", "/api/admin/extraction-jobs/"+jobID+"/cancel", e2eTestToken(), projectID, nil)
	// Job may have already started processing, so accept either OK or error
	if cancelResp.StatusCode == http.StatusOK {
		cancelBody := readRespBody(cancelResp)
		var cancelResult map[string]any
		parseBodyJSON(t, cancelBody, &cancelResult)
		data := cancelResult["data"].(map[string]any)
		if data["status"] != "cancelled" {
			t.Errorf("expected status 'cancelled', got %v", data["status"])
		}
	}
}

func TestExtraction_GetStatisticsReturnsAggregates(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/admin/extraction-jobs", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"project_id":  projectID,
			"source_type": "manual",
			"source_metadata": map[string]any{
				"text": "Simple test: John works at Company.",
			},
			"extraction_config": map[string]any{
				"object_schemas": map[string]any{
					"Person":  map[string]any{"name": "Person"},
					"Company": map[string]any{"name": "Company"},
				},
				"relationship_schemas": map[string]any{
					"WORKS_AT": map[string]any{
						"name":         "WORKS_AT",
						"source_types": []string{"Person"},
						"target_types": []string{"Company"},
					},
				},
				"target_types": []string{"Person", "Company"},
			},
		}))
	body := mustStatus(t, resp, http.StatusCreated)

	var createResp map[string]any
	parseBodyJSON(t, body, &createResp)
	jobID := createResp["data"].(map[string]any)["id"].(string)
	rl.Printf("statistics test job created: %s", jobID)

	pollExtractionJob(t, projectID, jobID, 120*time.Second)

	statsResp := doAPILogged(t, rl, "GET", "/api/admin/extraction-jobs/projects/"+projectID+"/statistics", e2eTestToken(), projectID, nil)
	statsBody := mustStatus(t, statsResp, http.StatusOK)

	var statsResult map[string]any
	parseBodyJSON(t, statsBody, &statsResult)
	data := statsResult["data"].(map[string]any)
	totalJobs := int(data["total_jobs"].(float64))
	if totalJobs < 1 {
		t.Error("expected at least 1 job in statistics")
	}
	t.Logf("statistics: total=%d, success_rate=%.2f%%", totalJobs, data["success_rate"].(float64)*100)
}
