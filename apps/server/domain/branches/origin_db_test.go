package branches_test

import (
	"encoding/json"
	"net/http"

	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/branchorigin"
)

// branchOriginBody is the subset of the create response this test asserts on.
type branchOriginBody struct {
	Origin      string  `json:"origin"`
	OriginID    *string `json:"origin_id"`
	OriginLabel *string `json:"origin_label"`
}

func decodeBranchOrigin(resp *testutil.HTTPResponse) branchOriginBody {
	var out branchOriginBody
	_ = json.Unmarshal(resp.Body, &out)
	return out
}

// TestBranchOriginIgnoresClientAttribution proves the API's half of the
// attribution contract: a branch created without one reads back as manual, and
// an origin/origin_id/origin_label in the request body is ignored — a caller
// must not be able to claim to be another source, because cleanup deletes
// branches on origin (#246).
func (s *BranchesMembershipSuite) TestBranchOriginIgnoresClientAttribution() {
	resp := s.Client.POST("/api/graph/branches",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{"project_id": s.ProjectID, "name": "origin-default"}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode, resp.String())

	defaulted := decodeBranchOrigin(resp)
	s.Equal(branchorigin.Manual, defaulted.Origin)
	s.Nil(defaulted.OriginID)
	s.Nil(defaulted.OriginLabel)

	resp = s.Client.POST("/api/graph/branches",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"project_id":   s.ProjectID,
			"name":         "origin-claimed",
			"origin":       "extraction",
			"origin_id":    "11111111-1111-1111-1111-111111111111",
			"origin_label": "Invoice.pdf",
		}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode, resp.String())

	claimed := decodeBranchOrigin(resp)
	s.Equal(branchorigin.Manual, claimed.Origin, "a request body must not bind the attribution")
	s.Nil(claimed.OriginID, "a request body must not bind the attribution")
	s.Nil(claimed.OriginLabel, "a request body must not bind the attribution")
}

// TestBranchOriginServiceAttributionRoundTrip covers the columns end-to-end for
// the server-side caller (the extraction worker, and later an agent run): the
// attribution the service is handed is what is stored and read back.
func (s *BranchesMembershipSuite) TestBranchOriginServiceAttributionRoundTrip() {
	svc := branches.NewService(branches.NewStore(s.DB()))

	jobID := "11111111-1111-1111-1111-111111111111"
	label := "Invoice.pdf"
	created, err := svc.Create(s.Ctx, &branches.CreateBranchRequest{
		ProjectID: &s.ProjectID,
		Name:      "extraction/aaaa/bbbb",
		Attribution: &branchorigin.Attribution{
			Origin: branchorigin.Extraction,
			ID:     &jobID,
			Label:  &label,
		},
	})
	s.Require().NoError(err)

	read, err := svc.GetByID(s.Ctx, created.ID, s.ProjectID)
	s.Require().NoError(err)
	s.Require().NotNil(read)
	s.Equal(branchorigin.Extraction, read.Origin)
	s.Require().NotNil(read.OriginID)
	s.Require().NotNil(read.OriginLabel)
	s.Equal(jobID, *read.OriginID)
	s.Equal(label, *read.OriginLabel)

	// An unattributed create is manual, and stores NULL rather than ''.
	plain, err := svc.Create(s.Ctx, &branches.CreateBranchRequest{
		ProjectID: &s.ProjectID,
		Name:      "plan/hand-made",
	})
	s.Require().NoError(err)
	s.Equal(branchorigin.Manual, plain.Origin)
	s.Nil(plain.OriginID)
	s.Nil(plain.OriginLabel)

	// A malformed attribution is a client error, not a database error.
	bad := "not-a-uuid"
	_, err = svc.Create(s.Ctx, &branches.CreateBranchRequest{
		ProjectID:   &s.ProjectID,
		Name:        "extraction/bad",
		Attribution: &branchorigin.Attribution{Origin: branchorigin.Extraction, ID: &bad},
	})
	s.Require().Error(err)
	s.Contains(err.Error(), "origin_id", "a malformed id must name the field")
}
