package branches_test

import (
	"encoding/json"
	"net/http"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// branchOriginBody is the subset of the create response this test asserts on.
type branchOriginBody struct {
	Origin      string  `json:"origin"`
	OriginID    *string `json:"origin_id"`
	OriginLabel *string `json:"origin_label"`
}

func decodeBranchOrigin(t interface {
	Helper()
	Fatalf(string, ...any)
}, resp *testutil.HTTPResponse) branchOriginBody {
	var out branchOriginBody
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		t.Fatalf("decode create response %s: %v", resp.String(), err)
	}
	return out
}

// TestBranchOriginRoundTrip covers the attribution columns end-to-end through
// the REST surface: a branch created without an origin reads back as manual, an
// explicit origin with its id and label round-trips, and an unknown origin is
// rejected rather than stored.
func (s *BranchesMembershipSuite) TestBranchOriginRoundTrip() {
	// Default: no origin supplied means manual.
	resp := s.Client.POST("/api/graph/branches",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{"project_id": s.ProjectID, "name": "origin-default"}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode, resp.String())

	defaulted := decodeBranchOrigin(s.T(), resp)
	s.Equal("manual", defaulted.Origin)
	s.Nil(defaulted.OriginID)
	s.Nil(defaulted.OriginLabel)

	// Explicit attribution: what the extraction worker sends.
	jobID := "11111111-1111-1111-1111-111111111111"
	resp = s.Client.POST("/api/graph/branches",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"project_id":   s.ProjectID,
			"name":         "origin-extraction",
			"origin":       "extraction",
			"origin_id":    jobID,
			"origin_label": "Invoice.pdf",
		}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode, resp.String())

	attributed := decodeBranchOrigin(s.T(), resp)
	s.Require().NotNil(attributed.OriginID)
	s.Require().NotNil(attributed.OriginLabel)
	s.Equal("extraction", attributed.Origin)
	s.Equal(jobID, *attributed.OriginID)
	s.Equal("Invoice.pdf", *attributed.OriginLabel)

	// Unknown origin is a client error, not a stored value.
	resp = s.Client.POST("/api/graph/branches",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"project_id": s.ProjectID,
			"name":       "origin-bogus",
			"origin":     "scheduled",
		}))
	s.Equal(http.StatusBadRequest, resp.StatusCode,
		"an unknown origin must be rejected, got %d: %s", resp.StatusCode, resp.String())
}
