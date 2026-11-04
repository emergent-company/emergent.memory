package integration

// CompiledTypesUISuite verifies that type-level UI metadata and property
// widget hints survive the full round-trip: a schema pack declaring an inline
// "ui" block (icon/color) on an object type and enum/widget hints on its
// properties is installed into a project, sample objects are created, and the
// compiled-types view still carries the ui block plus the property metadata.
//
// This is the contract the web console relies on to render schema-driven
// icons and inputs for object types (see the alfred gateway's CompiledType
// consumption). It runs against either an in-process test server or an
// external one via TEST_SERVER_URL (e.g. the dev server on :3002).
//
// Run:
//
//	task server:test:integration -- -run TestCompiledTypesUISuite
//	TEST_SERVER_URL=http://localhost:3002 go test ./tests/integration/ -run TestCompiledTypesUISuite -v -count=1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestCompiledTypesUISuite is the testify runner for CompiledTypesUISuite.
func TestCompiledTypesUISuite(t *testing.T) {
	suite.Run(t, new(CompiledTypesUISuite))
}

type CompiledTypesUISuite struct {
	testutil.BaseSuite
}

func (s *CompiledTypesUISuite) SetupSuite() {
	s.SetDBSuffix("ctui")
	s.BaseSuite.SetupSuite()
}

// TestCompiledTypesCarryUIMetadata installs a pack whose types declare inline
// ui blocks and property enum/widget hints, creates sample objects, then
// asserts the compiled-types endpoint returns the ui metadata intact.
func (s *CompiledTypesUISuite) TestCompiledTypesCarryUIMetadata() {
	// ---- 1. Create a schema pack with two types. -----------------------
	// "widget" declares ui (iconify icon + color) and an enum property;
	// "notepage" declares a ui icon and a widget:textarea property.
	schemaName := "ct-ui-" + uuid.NewString()[:8]
	createResp := s.Client.POST("/api/schemas",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithOrgID(s.OrgID),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name":        schemaName,
			"version":     "1.0.0",
			"description": "compiled-types ui verification fixture",
			"objectTypeSchemas": []map[string]any{
				{
					"name":        "widget",
					"label":       "Widget",
					"description": "sample widget type",
					"properties": map[string]any{
						"title":    map[string]any{"type": "string", "description": "widget title"},
						"priority": map[string]any{"type": "string", "enum": []string{"low", "high", "urgent"}},
					},
					"ui": map[string]any{"icon": "lucide--widget", "color": "#0EA5E9"},
				},
				{
					"name":        "notepage",
					"label":       "Note page",
					"description": "sample note type",
					"properties": map[string]any{
						"content": map[string]any{"type": "string", "widget": "textarea", "description": "note body"},
					},
					"ui": map[string]any{"icon": "lucide--note"},
				},
			},
		}),
	)
	s.Require().Equal(http.StatusCreated, createResp.StatusCode,
		"create schema pack: %s", createResp.Body)

	var created map[string]any
	s.Require().NoError(json.Unmarshal(createResp.Body, &created), "parse create response")
	schemaID, _ := created["id"].(string)
	s.Require().NotEmpty(schemaID, "created schema id")

	// ---- 2. Install the pack into the test project. --------------------
	assignResp := s.Client.POST(fmt.Sprintf("/api/schemas/projects/%s/assign", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithOrgID(s.OrgID),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"schema_id": schemaID,
			"dry_run":   false,
			"merge":     false,
		}),
	)
	s.Require().Equal(http.StatusCreated, assignResp.StatusCode,
		"assign schema to project: %s", assignResp.Body)

	// ---- 3. Create sample objects of both types. ------------------------
	widgetID := s.createSampleObject("widget", "w-1", map[string]any{
		"title": "Sample widget", "priority": "high",
	})
	noteID := s.createSampleObject("notepage", "n-1", map[string]any{
		"content": "This note body is deliberately long so that a UI would render it in a textarea.",
	})

	// ---- 4. The compiled-types view must carry ui + property metadata. --
	ctResp := s.Client.GET(fmt.Sprintf("/api/schemas/projects/%s/compiled-types", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithOrgID(s.OrgID),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, ctResp.StatusCode, "compiled-types: %s", ctResp.Body)

	var compiled struct {
		ObjectTypes []struct {
			Name       string          `json:"name"`
			UI         json.RawMessage `json:"ui"`
			Properties json.RawMessage `json:"properties"`
		} `json:"objectTypes"`
	}
	s.Require().NoError(json.Unmarshal(ctResp.Body, &compiled), "parse compiled-types")

	byName := map[string]struct {
		UI         json.RawMessage
		Properties json.RawMessage
	}{}
	for _, ot := range compiled.ObjectTypes {
		byName[ot.Name] = struct {
			UI         json.RawMessage
			Properties json.RawMessage
		}{ot.UI, ot.Properties}
	}

	widget, ok := byName["widget"]
	s.Require().Truef(ok, "compiled types missing installed type %q in %s", "widget", ctResp.Body)
	s.Assert().Contains(string(widget.UI), `"icon":"lucide--widget"`, "type-level ui icon missing")
	s.Assert().Contains(string(widget.UI), `"color":"#0EA5E9"`, "type-level ui color missing")
	s.Assert().Contains(string(widget.Properties), `"enum":["low","high","urgent"]`, "enum property metadata lost")

	note, ok := byName["notepage"]
	s.Require().Truef(ok, "compiled types missing installed type %q in %s", "notepage", ctResp.Body)
	s.Assert().Contains(string(note.UI), `"icon":"lucide--note"`, "note ui icon missing")
	s.Assert().Contains(string(note.Properties), `"widget":"textarea"`, "widget textarea hint lost")

	// ---- 5. The sample objects exist and are typed correctly. ----------
	s.Assert().NotEmpty(widgetID, "widget object id")
	s.Assert().NotEmpty(noteID, "note object id")

	for _, id := range []string{widgetID, noteID} {
		getResp := s.Client.GET(fmt.Sprintf("/api/graph/objects/%s", id),
			testutil.WithAuth("e2e-test-user"),
			testutil.WithOrgID(s.OrgID),
			testutil.WithProjectID(s.ProjectID),
		)
		s.Assert().Equalf(http.StatusOK, getResp.StatusCode, "fetch created object %s: %s", id, getResp.Body)
	}
}

// createSampleObject posts one graph object and returns its id.
func (s *CompiledTypesUISuite) createSampleObject(typ, key string, props map[string]any) string {
	resp := s.Client.POST("/api/graph/objects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithOrgID(s.OrgID),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"type":       typ,
			"key":        key,
			"properties": props,
			"labels":     []string{},
		}),
	)
	s.Require().Equalf(http.StatusCreated, resp.StatusCode, "create %s object: %s", typ, resp.Body)
	var obj map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &obj), "parse created object")
	id, _ := obj["id"].(string)
	s.Require().NotEmptyf(id, "created %s object has no id in %s", typ, resp.Body)
	return id
}
