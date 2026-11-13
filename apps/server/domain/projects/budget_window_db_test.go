package projects_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ProjectBudgetWindowSuite covers the per-window budget behaviour exposed by the
// projects API: the weekly defaults applied on create, and the admin-only
// POST /api/projects/{id}/budget/reset that restarts one window without touching
// the other.
type ProjectBudgetWindowSuite struct {
	testutil.BaseSuite
}

func TestProjectBudgetWindowSuite(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	suite.Run(t, new(ProjectBudgetWindowSuite))
}

func (s *ProjectBudgetWindowSuite) SetupSuite() {
	s.SetDBSuffix("project_budget_windows")
	s.BaseSuite.SetupSuite()
}

// anchors reads the project's two cycle anchors (nil = calendar window).
func (s *ProjectBudgetWindowSuite) anchors(projectID string) (week, month *time.Time) {
	s.T().Helper()
	var row struct {
		WeekAnchor  *time.Time `bun:"budget_week_anchor"`
		MonthAnchor *time.Time `bun:"budget_month_anchor"`
	}
	err := s.DB().NewSelect().
		TableExpr("kb.projects").
		Column("budget_week_anchor", "budget_month_anchor").
		Where("id = ?", projectID).
		Scan(s.Ctx, &row)
	s.Require().NoError(err)
	return row.WeekAnchor, row.MonthAnchor
}

func (s *ProjectBudgetWindowSuite) reset(projectID, window, token string) *testutil.HTTPResponse {
	return s.Client.POST("/api/projects/"+projectID+"/budget/reset",
		testutil.WithAuth(token),
		testutil.WithJSONBody(map[string]any{"window": window}),
	)
}

// TestCreate_DefaultsWeeklyWindow proves a new project gets a weekly cap of a
// quarter of the monthly one, with the same alert threshold — the default that
// catches a runaway week while the month is still repairable.
func (s *ProjectBudgetWindowSuite) TestCreate_DefaultsWeeklyWindow() {
	resp := s.Client.POST("/api/projects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":  "Weekly Defaults " + uuid.NewString()[:8],
			"orgId": s.OrgID,
		}),
	)
	s.Require().Equal(201, resp.StatusCode, resp.String())

	var project struct {
		BudgetUSD                  float64 `json:"budget_usd"`
		BudgetWeeklyUSD            float64 `json:"budget_weekly_usd"`
		BudgetAlertThreshold       float64 `json:"budget_alert_threshold"`
		BudgetWeeklyAlertThreshold float64 `json:"budget_weekly_alert_threshold"`
	}
	s.Require().NoError(resp.JSON(&project))

	// $10 monthly default => $2.50 weekly, both at the 80% schema default.
	s.InDelta(10.0, project.BudgetUSD, 1e-9)
	s.InDelta(2.5, project.BudgetWeeklyUSD, 1e-9)
	s.InDelta(0.8, project.BudgetAlertThreshold, 1e-9)
	s.InDelta(0.8, project.BudgetWeeklyAlertThreshold, 1e-9)
}

// TestReset_WeekLeavesMonthAlone is the core of "reset each window separately":
// moving the week anchor must not move the month's.
func (s *ProjectBudgetWindowSuite) TestReset_WeekLeavesMonthAlone() {
	resp := s.reset(s.ProjectID, "week", "e2e-test-user")
	s.Require().Equal(200, resp.StatusCode, resp.String())

	var period struct {
		Window string `json:"window"`
	}
	s.Require().NoError(resp.JSON(&period))
	s.Equal("week", period.Window)

	week, month := s.anchors(s.ProjectID)
	s.Require().NotNil(week, "resetting the week must set budget_week_anchor")
	s.Nil(month, "resetting the week must not touch budget_month_anchor")

	// Resetting the month afterwards must leave the week's anchor in place.
	resp = s.reset(s.ProjectID, "month", "e2e-test-user")
	s.Require().Equal(200, resp.StatusCode, resp.String())

	week2, month2 := s.anchors(s.ProjectID)
	s.Require().NotNil(week2)
	s.Require().NotNil(month2)
	s.WithinDuration(*week, *week2, time.Second, "the month reset must not move the week anchor")
}

// TestReset_RequiresProjectAdmin proves the mutation is authority-gated: an org
// member with no project role cannot restart a window (it would unblock agent
// runs).
func (s *ProjectBudgetWindowSuite) TestReset_RequiresProjectAdmin() {
	s.Require().NoError(testutil.CreateTestOrgMembership(s.Ctx, s.DB(), s.OrgID, testutil.AllScopesUser.ID, "member"))

	resp := s.reset(s.ProjectID, "week", "all-scopes")
	s.Equal(403, resp.StatusCode, resp.String())

	week, _ := s.anchors(s.ProjectID)
	s.Nil(week, "a rejected reset must not move the anchor")
}

// TestReset_RejectsUnknownWindow proves the window is validated before any
// write, so a typo cannot silently reset the wrong window.
func (s *ProjectBudgetWindowSuite) TestReset_RejectsUnknownWindow() {
	resp := s.reset(s.ProjectID, "day", "e2e-test-user")
	s.Equal(400, resp.StatusCode, resp.String())

	week, month := s.anchors(s.ProjectID)
	s.Nil(week)
	s.Nil(month)
}

// TestUpdate_CannotMoveTheAnchors proves a routine settings save cannot move a
// cycle boundary. PATCH loads the row, edits it and writes the model back, so
// without excluding the anchors a save would write back the anchor it loaded —
// reverting an admin's reset that landed in between, and moving a boundary the
// only the reset endpoint is allowed to move.
func (s *ProjectBudgetWindowSuite) TestUpdate_CannotMoveTheAnchors() {
	resp := s.reset(s.ProjectID, "week", "e2e-test-user")
	s.Require().Equal(200, resp.StatusCode, resp.String())
	weekBefore, _ := s.anchors(s.ProjectID)
	s.Require().NotNil(weekBefore)

	resp = s.Client.PATCH("/api/projects/"+s.ProjectID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{"name": "Renamed " + uuid.NewString()[:8]}),
	)
	s.Require().Equal(200, resp.StatusCode, resp.String())

	weekAfter, _ := s.anchors(s.ProjectID)
	s.Require().NotNil(weekAfter, "a settings save must not clear the weekly anchor")
	s.WithinDuration(*weekBefore, *weekAfter, 0, "a settings save must not move the weekly anchor")
}

// TestReset_ProjectNotFound keeps the 404 shape the other project routes use.
func (s *ProjectBudgetWindowSuite) TestReset_ProjectNotFound() {
	resp := s.reset(uuid.NewString(), "week", "e2e-test-user")
	s.Equal(404, resp.StatusCode, resp.String())
}
