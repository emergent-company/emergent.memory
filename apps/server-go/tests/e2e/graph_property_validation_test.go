package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent/domain/graph"
	"github.com/emergent-company/emergent/internal/testutil"
)

type GraphPropertyValidationSuite struct {
	testutil.BaseSuite
}

func (s *GraphPropertyValidationSuite) SetupSuite() {
	s.SetDBSuffix("graph_prop_validation")
	s.BaseSuite.SetupSuite()
}

func (s *GraphPropertyValidationSuite) SetupTest() {
	s.BaseSuite.SetupTest()

	templatePackID := s.createTemplatePackWithTypedProperties()
	projectUUID, err := uuid.Parse(s.ProjectID)
	s.Require().NoError(err, "Failed to parse project ID")
	s.linkTemplatePackToProject(templatePackID, projectUUID)
}

// createTemplatePackWithTypedProperties creates a template pack with number, boolean, date, and required properties
func (s *GraphPropertyValidationSuite) createTemplatePackWithTypedProperties() uuid.UUID {
	templatePackID := uuid.New()

	// Object schema with typed properties
	objectSchemas := map[string]any{
		"Person": map[string]any{
			"name":        "Person",
			"description": "A person entity for testing property validation",
			"properties": map[string]any{
				"age": map[string]any{
					"type":        "number",
					"description": "Person's age in years",
				},
				"income": map[string]any{
					"type":        "number",
					"description": "Annual income",
				},
				"active": map[string]any{
					"type":        "boolean",
					"description": "Whether the person is active",
				},
				"verified": map[string]any{
					"type":        "boolean",
					"description": "Whether the person is verified",
				},
				"birth_date": map[string]any{
					"type":        "date",
					"description": "Date of birth",
				},
				"hire_date": map[string]any{
					"type":        "date",
					"description": "Date of hire",
				},
				"name": map[string]any{
					"type":        "string",
					"description": "Person's full name",
				},
			},
			"required": []string{"name", "age"}, // name and age are required
		},
	}

	objectSchemasJSON, _ := json.Marshal(objectSchemas)

	// Insert template pack directly into database
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.graph_template_packs (
			id, name, version, description, object_type_schemas, relationship_type_schemas, created_at, updated_at
		) VALUES (
			?, ?, '1.0.0', ?, ?::jsonb, '{}'::jsonb, NOW(), NOW()
		)
	`, templatePackID, "Property Validation Test Pack", "Template pack for testing property type validation", string(objectSchemasJSON)).Exec(s.Ctx)

	s.Require().NoError(err, "Failed to create template pack")

	return templatePackID
}

// linkTemplatePackToProject links a template pack to a project
func (s *GraphPropertyValidationSuite) linkTemplatePackToProject(templatePackID, projectID uuid.UUID) {
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.project_template_packs (
			id, project_id, template_pack_id, installed_at, active, created_at, updated_at
		) VALUES (
			?, ?, ?, NOW(), true, NOW(), NOW()
		)
	`, uuid.New(), projectID, templatePackID).Exec(s.Ctx)

	s.Require().NoError(err, "Failed to link template pack to project")
}

// ============ Type Coercion Tests ============

func (s *GraphPropertyValidationSuite) TestCreateObject_NumberCoercion() {
	rec := s.Client.POST(
		"/api/graph/objects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"type": "Person",
			"properties": map[string]any{
				"name":   "John Doe",
				"age":    "25",
				"income": "75000.5",
			},
		}),
	)

	s.Equal(http.StatusCreated, rec.StatusCode, "Response: %s", rec.String())

	var response graph.GraphObjectResponse
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	// Verify age is a number, not a string
	age, ok := response.Properties["age"].(float64)
	s.True(ok, "age should be a float64, got %T: %v", response.Properties["age"], response.Properties["age"])
	s.Equal(float64(25), age)

	// Verify income is a number with decimals
	income, ok := response.Properties["income"].(float64)
	s.True(ok, "income should be a float64, got %T: %v", response.Properties["income"], response.Properties["income"])
	s.Equal(75000.5, income)

	// Verify in database that it's stored as number in JSONB
	var ageInDB any
	err = s.DB().NewRaw(`
		SELECT properties->>'age' FROM kb.graph_objects WHERE id = ?
	`, response.ID).Scan(s.Ctx, &ageInDB)
	s.Require().NoError(err)
	s.Equal("25", ageInDB, "age should be stored as numeric value in JSONB")
}

func (s *GraphPropertyValidationSuite) TestCreateObject_BooleanCoercion() {
	testCases := []struct {
		name     string
		input    string
		expected bool
	}{
		{"string true", "true", true},
		{"string t", "t", true},
		{"string yes", "yes", true},
		{"string y", "y", true},
		{"string 1", "1", true},
		{"string false", "false", false},
		{"string f", "f", false},
		{"string no", "no", false},
		{"string n", "n", false},
		{"string 0", "0", false},
		{"empty string", "", false},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			rec := s.Client.POST(
				"/api/graph/objects",
				testutil.WithAuth("e2e-test-user"),
				testutil.WithProjectID(s.ProjectID),
				testutil.WithJSONBody(map[string]any{
					"type": "Person",
					"properties": map[string]any{
						"name":     "Jane Doe",
						"age":      30,
						"active":   tc.input, // String should be coerced to boolean
						"verified": tc.input,
					},
				}),
			)

			s.Equal(http.StatusCreated, rec.StatusCode, "Response: %s", rec.String())

			var response graph.GraphObjectResponse
			err := json.Unmarshal(rec.Body, &response)
			s.Require().NoError(err)

			// Verify active is a boolean
			active, ok := response.Properties["active"].(bool)
			s.True(ok, "active should be a bool, got %T: %v", response.Properties["active"], response.Properties["active"])
			s.Equal(tc.expected, active, "Input '%s' should coerce to %v", tc.input, tc.expected)

			// Verify verified is also coerced correctly
			verified, ok := response.Properties["verified"].(bool)
			s.True(ok, "verified should be a bool, got %T: %v", response.Properties["verified"], response.Properties["verified"])
			s.Equal(tc.expected, verified)
		})
	}
}

func (s *GraphPropertyValidationSuite) TestCreateObject_DateCoercion() {
	testCases := []struct {
		name         string
		input        string
		expectedDate string // ISO 8601 date expected
	}{
		{"ISO date", "2024-02-10", "2024-02-10T00:00:00Z"},
		{"ISO datetime", "2024-02-10T15:30:00Z", "2024-02-10T15:30:00Z"},
		{"US format", "02/10/2024", "2024-02-10T00:00:00Z"},
		{"datetime with space", "2024-02-10 15:30:00", "2024-02-10T15:30:00Z"},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			rec := s.Client.POST(
				"/api/graph/objects",
				testutil.WithAuth("e2e-test-user"),
				testutil.WithProjectID(s.ProjectID),
				testutil.WithJSONBody(map[string]any{
					"type": "Person",
					"properties": map[string]any{
						"name":       "Bob Smith",
						"age":        40,
						"birth_date": tc.input, // Various date formats should be normalized to ISO 8601
					},
				}),
			)

			s.Equal(http.StatusCreated, rec.StatusCode, "Response: %s", rec.String())

			var response graph.GraphObjectResponse
			err := json.Unmarshal(rec.Body, &response)
			s.Require().NoError(err)

			// Verify birth_date is a string in ISO 8601 format
			birthDate, ok := response.Properties["birth_date"].(string)
			s.True(ok, "birth_date should be a string, got %T: %v", response.Properties["birth_date"], response.Properties["birth_date"])
			s.Equal(tc.expectedDate, birthDate, "Input '%s' should be normalized to '%s'", tc.input, tc.expectedDate)
		})
	}
}

// ============ Validation Error Tests ============

func (s *GraphPropertyValidationSuite) TestCreateObject_InvalidNumber() {
	rec := s.Client.POST(
		"/api/graph/objects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"type": "Person",
			"properties": map[string]any{
				"name": "Invalid Person",
				"age":  "not-a-number", // Invalid number should fail
			},
		}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode, "Response: %s", rec.String())

	var errorResp map[string]any
	err := json.Unmarshal(rec.Body, &errorResp)
	s.Require().NoError(err)

	// Verify error message mentions property validation
	if errorMap, ok := errorResp["error"].(map[string]any); ok {
		message, _ := errorMap["message"].(string)
		s.Contains(message, "property validation failed", "Error message should mention property validation")
		s.Contains(message, "age", "Error message should mention the failing property")
	}
}

func (s *GraphPropertyValidationSuite) TestCreateObject_InvalidDate() {
	rec := s.Client.POST(
		"/api/graph/objects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"type": "Person",
			"properties": map[string]any{
				"name":       "Invalid Person",
				"age":        25,
				"birth_date": "not-a-date", // Invalid date should fail
			},
		}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode, "Response: %s", rec.String())

	var errorResp map[string]any
	err := json.Unmarshal(rec.Body, &errorResp)
	s.Require().NoError(err)

	// Verify error message mentions property validation
	if errorMap, ok := errorResp["error"].(map[string]any); ok {
		message, _ := errorMap["message"].(string)
		s.Contains(message, "property validation failed", "Error message should mention property validation")
		s.Contains(message, "birth_date", "Error message should mention the failing property")
	}
}

func (s *GraphPropertyValidationSuite) TestCreateObject_MissingRequiredProperty() {
	rec := s.Client.POST(
		"/api/graph/objects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"type": "Person",
			"properties": map[string]any{
				"name": "Missing Age Person",
				// Missing "age" which is required
			},
		}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode, "Response: %s", rec.String())

	var errorResp map[string]any
	err := json.Unmarshal(rec.Body, &errorResp)
	s.Require().NoError(err)

	// Verify error message mentions required property
	if errorMap, ok := errorResp["error"].(map[string]any); ok {
		message, _ := errorMap["message"].(string)
		s.Contains(message, "property validation failed", "Error message should mention property validation")
		s.Contains(message, "age", "Error message should mention the missing required property")
		s.Contains(message, "required", "Error message should mention that the property is required")
	}
}

// ============ Patch Operation Tests ============

func (s *GraphPropertyValidationSuite) TestPatchObject_ValidatesProperties() {
	// First, create an object
	createRec := s.Client.POST(
		"/api/graph/objects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"type": "Person",
			"properties": map[string]any{
				"name": "Test Person",
				"age":  30,
			},
		}),
	)

	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResp graph.GraphObjectResponse
	err := json.Unmarshal(createRec.Body, &createResp)
	s.Require().NoError(err)

	// Now patch with valid coercion
	patchRec := s.Client.PATCH(
		"/api/graph/objects/"+createResp.ID.String(),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"properties": map[string]any{
				"age":    "35",   // String should be coerced to number
				"active": "true", // String should be coerced to boolean
			},
		}),
	)

	s.Equal(http.StatusOK, patchRec.StatusCode, "Response: %s", patchRec.String())

	var patchResp graph.GraphObjectResponse
	err = json.Unmarshal(patchRec.Body, &patchResp)
	s.Require().NoError(err)

	// Verify age is coerced to number
	age, ok := patchResp.Properties["age"].(float64)
	s.True(ok, "age should be a float64, got %T", patchResp.Properties["age"])
	s.Equal(float64(35), age)

	// Verify active is coerced to boolean
	active, ok := patchResp.Properties["active"].(bool)
	s.True(ok, "active should be a bool, got %T", patchResp.Properties["active"])
	s.True(active)
}

func (s *GraphPropertyValidationSuite) TestPatchObject_InvalidPropertyValue() {
	// First, create an object
	createRec := s.Client.POST(
		"/api/graph/objects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"type": "Person",
			"properties": map[string]any{
				"name": "Test Person",
				"age":  30,
			},
		}),
	)

	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResp graph.GraphObjectResponse
	err := json.Unmarshal(createRec.Body, &createResp)
	s.Require().NoError(err)

	// Try to patch with invalid number
	patchRec := s.Client.PATCH(
		"/api/graph/objects/"+createResp.ID.String(),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"properties": map[string]any{
				"age": "invalid-number", // Should fail validation
			},
		}),
	)

	s.Equal(http.StatusBadRequest, patchRec.StatusCode, "Response: %s", patchRec.String())

	var errorResp map[string]any
	err = json.Unmarshal(patchRec.Body, &errorResp)
	s.Require().NoError(err)

	// Verify error message
	if errorMap, ok := errorResp["error"].(map[string]any); ok {
		message, _ := errorMap["message"].(string)
		s.Contains(message, "property validation failed", "Error message should mention property validation")
		s.Contains(message, "age", "Error message should mention the failing property")
	}
}

// ============ Unknown Properties Tests ============

func (s *GraphPropertyValidationSuite) TestCreateObject_AllowsUnknownProperties() {
	rec := s.Client.POST(
		"/api/graph/objects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"type": "Person",
			"properties": map[string]any{
				"name":             "John Doe",
				"age":              25,
				"unknown_property": "some value", // Unknown property should be allowed
				"another_unknown":  123,
			},
		}),
	)

	s.Equal(http.StatusCreated, rec.StatusCode, "Response: %s", rec.String())

	var response graph.GraphObjectResponse
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	// Verify unknown properties are preserved
	s.Equal("some value", response.Properties["unknown_property"])
	s.Equal(float64(123), response.Properties["another_unknown"])
}

func TestGraphPropertyValidationSuite(t *testing.T) {
	suite.Run(t, new(GraphPropertyValidationSuite))
}
