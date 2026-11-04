package useractivity_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// UserActivityMembershipSuite proves the record endpoint derives authorization
// from real organization membership, not from the client-supplied ?project_id
// query param (issue #913). The recent/delete routes are user-scoped (user.ID)
// and carry no project source, so only Record is exercised here.
type UserActivityMembershipSuite struct {
	testutil.BaseSuite
}

func TestUserActivityMembershipSuite(t *testing.T) {
	suite.Run(t, new(UserActivityMembershipSuite))
}

func (s *UserActivityMembershipSuite) SetupSuite() {
	s.SetDBSuffix("useractivity_membership")
	s.BaseSuite.SetupSuite()
}

func (s *UserActivityMembershipSuite) newForeignProject() string {
	orgB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), orgB, "Org B"))
	projectB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestProject(s.Ctx, s.DB(), testutil.TestProject{
		ID:    projectB,
		OrgID: orgB,
		Name:  "Project B",
	}, testutil.AdminUser.ID))
	return projectB
}

func (s *UserActivityMembershipSuite) TestCrossProjectRecordForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.POST("/api/user-activity/record?project_id="+projectB,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"resourceType": "document",
			"resourceId":   uuid.New().String(),
			"actionType":   "viewed",
		}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project activity record must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

func (s *UserActivityMembershipSuite) TestOwnProjectRecordOK() {
	resp := s.Client.POST("/api/user-activity/record?project_id="+s.ProjectID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"resourceType": "document",
			"resourceId":   uuid.New().String(),
			"actionType":   "viewed",
		}))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project activity record must succeed, got %d: %s", resp.StatusCode, resp.String())
}

// TestMalformedProjectIDBadRequest pins that a non-UUID ?project_id is a 400
// (format validation), not a 500 from the uuid column cast.
func (s *UserActivityMembershipSuite) TestMalformedProjectIDBadRequest() {
	resp := s.Client.POST("/api/user-activity/record?project_id=invalid-uuid",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"resourceType": "document",
			"resourceId":   uuid.New().String(),
			"actionType":   "viewed",
		}))
	s.Require().Equal(http.StatusBadRequest, resp.StatusCode,
		"malformed project_id must be 400, got %d: %s", resp.StatusCode, resp.String())
}
