package e2e

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent/internal/testutil"
)

type ExtractionTestSuite struct {
	testutil.BaseSuite
}

func TestExtractionSuite(t *testing.T) {
	suite.Run(t, new(ExtractionTestSuite))
}

func (s *ExtractionTestSuite) SetupSuite() {
	s.SetDBSuffix("extraction")
	s.BaseSuite.SetupSuite()
}

func (s *ExtractionTestSuite) TestCreateExtractionJob_ManualSource_ExtractsEntities() {
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

	documentText := `John Smith is a 35-year-old software engineer who works at Acme Corporation. 
Acme Corporation is a technology company headquartered in San Francisco, California. 
John has been working there for 5 years and leads the backend team.
Sarah Johnson is the CEO of Acme Corporation and has been with the company since its founding.`

	body := map[string]any{
		"project_id":  s.ProjectID,
		"source_type": "manual",
		"source_metadata": map[string]any{
			"text": documentText,
		},
		"extraction_config": map[string]any{
			"object_schemas":       objectSchemas,
			"relationship_schemas": relationshipSchemas,
			"target_types":         []string{"Person", "Organization", "Location"},
		},
	}

	resp := s.Client.POST("/api/admin/extraction-jobs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)

	s.Require().Equal(http.StatusCreated, resp.StatusCode, "Response: %s", resp.String())

	var createResp map[string]any
	err := json.Unmarshal(resp.Body, &createResp)
	s.Require().NoError(err)

	s.True(createResp["success"].(bool), "Response should be successful")

	data := createResp["data"].(map[string]any)
	jobID := data["id"].(string)
	s.NotEmpty(jobID)
	s.Equal("queued", data["status"])

	// Poll for completion (max 120 seconds for LLM processing)
	var finalStatus string
	var jobData map[string]any
	maxAttempts := 60
	for i := 0; i < maxAttempts; i++ {
		time.Sleep(2 * time.Second)

		getResp := s.Client.GET("/api/admin/extraction-jobs/"+jobID,
			testutil.WithAuth("e2e-test-user"),
			testutil.WithProjectID(s.ProjectID),
		)
		s.Require().Equal(http.StatusOK, getResp.StatusCode)

		var getResult map[string]any
		err = json.Unmarshal(getResp.Body, &getResult)
		s.Require().NoError(err)

		jobData = getResult["data"].(map[string]any)
		finalStatus = jobData["status"].(string)

		if finalStatus == "completed" || finalStatus == "failed" {
			break
		}
	}

	s.Require().Equal("completed", finalStatus, "Job should complete successfully. Error: %v", jobData["error_message"])

	discoveredTypes := jobData["discovered_types"].([]any)
	s.NotEmpty(discoveredTypes, "Should discover entity types")

	createdObjects := jobData["created_objects"].([]any)
	s.NotEmpty(createdObjects, "Should create graph objects")

	s.T().Logf("Extraction completed: discovered %d types, created %d objects",
		len(discoveredTypes), len(createdObjects))

	// Verify graph objects were created
	graphResp := s.Client.GET("/api/graph/objects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, graphResp.StatusCode)

	var graphResult map[string]any
	err = json.Unmarshal(graphResp.Body, &graphResult)
	s.Require().NoError(err)

	objects := graphResult["objects"].([]any)
	s.GreaterOrEqual(len(objects), 2, "Should have at least 2 graph objects (persons, orgs, locations)")

	// Check we have expected entity types
	typeCount := make(map[string]int)
	for _, obj := range objects {
		objMap := obj.(map[string]any)
		objType := objMap["type"].(string)
		typeCount[objType]++
	}

	s.T().Logf("Created objects by type: %v", typeCount)
	s.Contains(typeCount, "Person", "Should have Person entities")
}

func (s *ExtractionTestSuite) TestCreateExtractionJob_DocumentSource_ExtractsFromDocument() {
	docID := uuid.New().String()
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

	// Create document in database
	err := testutil.CreateTestDocument(s.Ctx, s.TestDB.GetDB(), testutil.TestDocument{
		ID:        docID,
		ProjectID: s.ProjectID,
		Filename:  testutil.StringPtr("meeting-notes.txt"),
		MimeType:  testutil.StringPtr("text/plain"),
		Content:   testutil.StringPtr(docContent),
	})
	s.Require().NoError(err)

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

	body := map[string]any{
		"project_id":  s.ProjectID,
		"source_type": "document",
		"source_id":   docID,
		"extraction_config": map[string]any{
			"object_schemas":       objectSchemas,
			"relationship_schemas": relationshipSchemas,
			"target_types":         []string{"Person", "Event", "ActionItem"},
		},
	}

	resp := s.Client.POST("/api/admin/extraction-jobs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)

	s.Require().Equal(http.StatusCreated, resp.StatusCode, "Response: %s", resp.String())

	var createResp map[string]any
	err = json.Unmarshal(resp.Body, &createResp)
	s.Require().NoError(err)

	data := createResp["data"].(map[string]any)
	jobID := data["id"].(string)

	// Poll for completion
	var finalStatus string
	var jobData map[string]any
	maxAttempts := 60
	for i := 0; i < maxAttempts; i++ {
		time.Sleep(2 * time.Second)

		getResp := s.Client.GET("/api/admin/extraction-jobs/"+jobID,
			testutil.WithAuth("e2e-test-user"),
			testutil.WithProjectID(s.ProjectID),
		)
		s.Require().Equal(http.StatusOK, getResp.StatusCode)

		var getResult map[string]any
		err = json.Unmarshal(getResp.Body, &getResult)
		s.Require().NoError(err)

		jobData = getResult["data"].(map[string]any)
		finalStatus = jobData["status"].(string)

		if finalStatus == "completed" || finalStatus == "failed" {
			break
		}
	}

	s.Require().Equal("completed", finalStatus, "Job should complete. Error: %v", jobData["error_message"])

	discoveredTypes := jobData["discovered_types"].([]any)
	createdObjects := jobData["created_objects"].([]any)

	s.NotEmpty(discoveredTypes, "Should discover types from meeting notes")
	s.NotEmpty(createdObjects, "Should create objects from meeting notes")

	s.T().Logf("Document extraction completed: %d types, %d objects", len(discoveredTypes), len(createdObjects))
}

func (s *ExtractionTestSuite) TestGetExtractionJobLogs_ReturnsAgentExecutionDetails() {
	documentText := `Alice is a data scientist at TechCorp. Bob is her manager.`

	body := map[string]any{
		"project_id":  s.ProjectID,
		"source_type": "manual",
		"source_metadata": map[string]any{
			"text": documentText,
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
	}

	resp := s.Client.POST("/api/admin/extraction-jobs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode)

	var createResp map[string]any
	err := json.Unmarshal(resp.Body, &createResp)
	s.Require().NoError(err)

	data := createResp["data"].(map[string]any)
	jobID := data["id"].(string)

	// Wait for job to complete
	var finalStatus string
	maxAttempts := 60
	for i := 0; i < maxAttempts; i++ {
		time.Sleep(2 * time.Second)

		getResp := s.Client.GET("/api/admin/extraction-jobs/"+jobID,
			testutil.WithAuth("e2e-test-user"),
			testutil.WithProjectID(s.ProjectID),
		)

		var getResult map[string]any
		_ = json.Unmarshal(getResp.Body, &getResult)
		jobData := getResult["data"].(map[string]any)
		finalStatus = jobData["status"].(string)

		if finalStatus == "completed" || finalStatus == "failed" {
			break
		}
	}

	s.Require().Equal("completed", finalStatus)

	// Get logs
	logsResp := s.Client.GET("/api/admin/extraction-jobs/"+jobID+"/logs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, logsResp.StatusCode)

	var logsResult map[string]any
	err = json.Unmarshal(logsResp.Body, &logsResult)
	s.Require().NoError(err)

	logsData := logsResult["data"].(map[string]any)

	summary := logsData["summary"].(map[string]any)
	s.GreaterOrEqual(int(summary["totalSteps"].(float64)), 1, "Should have at least 1 step logged")

	s.T().Logf("Job logs: %d total steps, %d success, %d errors",
		int(summary["totalSteps"].(float64)),
		int(summary["successSteps"].(float64)),
		int(summary["errorSteps"].(float64)))
}

func (s *ExtractionTestSuite) TestCreateExtractionJob_RequiresAuth() {
	body := map[string]any{
		"project_id":  s.ProjectID,
		"source_type": "manual",
		"source_metadata": map[string]any{
			"text": "Test text",
		},
		"extraction_config": map[string]any{},
	}

	resp := s.Client.POST("/api/admin/extraction-jobs",
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ExtractionTestSuite) TestCreateExtractionJob_RequiresAdminWriteScope() {
	body := map[string]any{
		"project_id":  s.ProjectID,
		"source_type": "manual",
		"source_metadata": map[string]any{
			"text": "Test text",
		},
		"extraction_config": map[string]any{},
	}

	resp := s.Client.POST("/api/admin/extraction-jobs",
		testutil.WithAuth("read-only"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)

	s.Equal(http.StatusForbidden, resp.StatusCode)
}

func (s *ExtractionTestSuite) TestListExtractionJobs_ReturnsPaginatedResults() {
	// Create a few extraction jobs
	for i := 0; i < 3; i++ {
		body := map[string]any{
			"project_id":  s.ProjectID,
			"source_type": "manual",
			"source_metadata": map[string]any{
				"text": "Sample text for job " + string(rune('A'+i)),
			},
			"extraction_config": map[string]any{
				"object_schemas": map[string]any{
					"Item": map[string]any{"name": "Item"},
				},
				"target_types": []string{"Item"},
			},
		}

		resp := s.Client.POST("/api/admin/extraction-jobs",
			testutil.WithAuth("e2e-test-user"),
			testutil.WithProjectID(s.ProjectID),
			testutil.WithJSONBody(body),
		)
		s.Require().Equal(http.StatusCreated, resp.StatusCode)
	}

	// List jobs
	listResp := s.Client.GET("/api/admin/extraction-jobs/projects/"+s.ProjectID+"?limit=2",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, listResp.StatusCode)

	var listResult map[string]any
	err := json.Unmarshal(listResp.Body, &listResult)
	s.Require().NoError(err)

	data := listResult["data"].(map[string]any)
	jobs := data["jobs"].([]any)
	total := int(data["total"].(float64))
	limit := int(data["limit"].(float64))

	s.Equal(2, limit)
	s.GreaterOrEqual(total, 3)
	s.Len(jobs, 2, "Should return limited number of jobs")
}

func (s *ExtractionTestSuite) TestCancelExtractionJob_CancelsPendingJob() {
	body := map[string]any{
		"project_id":  s.ProjectID,
		"source_type": "manual",
		"source_metadata": map[string]any{
			"text": "Text to extract from",
		},
		"extraction_config": map[string]any{
			"object_schemas": map[string]any{
				"Entity": map[string]any{"name": "Entity"},
			},
		},
	}

	resp := s.Client.POST("/api/admin/extraction-jobs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode)

	var createResp map[string]any
	err := json.Unmarshal(resp.Body, &createResp)
	s.Require().NoError(err)

	jobID := createResp["data"].(map[string]any)["id"].(string)

	// Cancel immediately
	cancelResp := s.Client.POST("/api/admin/extraction-jobs/"+jobID+"/cancel",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	// Job may have already started processing, so accept either OK or error
	if cancelResp.StatusCode == http.StatusOK {
		var cancelResult map[string]any
		err = json.Unmarshal(cancelResp.Body, &cancelResult)
		s.Require().NoError(err)

		data := cancelResult["data"].(map[string]any)
		s.Equal("cancelled", data["status"])
	}
}

func (s *ExtractionTestSuite) TestGetExtractionJobStatistics_ReturnsAggregates() {
	// Create and wait for at least one job to complete for meaningful stats
	body := map[string]any{
		"project_id":  s.ProjectID,
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
	}

	resp := s.Client.POST("/api/admin/extraction-jobs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode)

	var createResp map[string]any
	_ = json.Unmarshal(resp.Body, &createResp)
	jobID := createResp["data"].(map[string]any)["id"].(string)

	// Wait for completion
	maxAttempts := 60
	for i := 0; i < maxAttempts; i++ {
		time.Sleep(2 * time.Second)

		getResp := s.Client.GET("/api/admin/extraction-jobs/"+jobID,
			testutil.WithAuth("e2e-test-user"),
			testutil.WithProjectID(s.ProjectID),
		)

		var getResult map[string]any
		_ = json.Unmarshal(getResp.Body, &getResult)
		status := getResult["data"].(map[string]any)["status"].(string)

		if status == "completed" || status == "failed" {
			break
		}
	}

	// Get statistics
	statsResp := s.Client.GET("/api/admin/extraction-jobs/projects/"+s.ProjectID+"/statistics",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, statsResp.StatusCode)

	var statsResult map[string]any
	err := json.Unmarshal(statsResp.Body, &statsResult)
	s.Require().NoError(err)

	data := statsResult["data"].(map[string]any)
	totalJobs := int(data["total_jobs"].(float64))
	s.GreaterOrEqual(totalJobs, 1, "Should have at least 1 job in statistics")

	s.T().Logf("Statistics: total=%d, success_rate=%.2f%%",
		totalJobs, data["success_rate"].(float64)*100)
}
