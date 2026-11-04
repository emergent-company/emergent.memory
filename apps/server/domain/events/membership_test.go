package events_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// EventsMembershipSuite exercises the query-param-scoped /api/events/stream
// route through the full in-process server to prove that a caller's
// authorization is derived from the authenticated context (token-bound project
// or X-Project-ID header), not from the client-supplied ?projectId query
// parameter (issue #913). The events routes register via RegisterRoutesManual,
// which is why previous domain/*/routes.go sweeps never saw them.
type EventsMembershipSuite struct {
	testutil.BaseSuite
}

func TestEventsMembershipSuite(t *testing.T) {
	suite.Run(t, new(EventsMembershipSuite))
}

func (s *EventsMembershipSuite) SetupSuite() {
	s.SetDBSuffix("events_membership")
	s.BaseSuite.SetupSuite()
}

// newForeignProject creates an organization the admin user is NOT a member of,
// plus a project owned by it, and returns the foreign project ID.
func (s *EventsMembershipSuite) newForeignProject() string {
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

// streamEstablished drives the SSE handler on a request whose context is
// cancelled after a short window, then reports whether the "connected" event was
// flushed (i.e. the stream was established) rather than an authorization denial.
// Because the handler blocks until client disconnect, this is the only way to
// assert stream establishment synchronously without a data race on the recorder.
func (s *EventsMembershipSuite) streamEstablished(path string, opts ...testutil.RequestOption) bool {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, opt := range opts {
		opt(req)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	s.Server.Echo.ServeHTTP(rec, req)

	return rec.Flushed && strings.Contains(rec.Body.String(), "event: connected")
}

// TestCrossProjectStreamForbidden is the fail-first reproducer: a member of
// org A must not subscribe to org B's entity stream via the ?projectId query
// parameter. Before the fix this returned 200 and streamed B's events because
// the handler subscribed to whatever projectId the client supplied.
func (s *EventsMembershipSuite) TestCrossProjectStreamForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.GET("/api/events/stream?projectId="+projectB,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project stream must be 403, got %d: %s", resp.StatusCode, resp.String())
}

// TestCrossProjectStreamWithHeaderForbidden covers the header-disagreement path:
// a member of org A who declares org A's project via X-Project-ID but requests
// org B's project via the query parameter must fail closed (403).
func (s *EventsMembershipSuite) TestCrossProjectStreamWithHeaderForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.GET("/api/events/stream?projectId="+projectB,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"query project disagreeing with the header project must be 403, got %d: %s",
		resp.StatusCode, resp.String())
}

// TestOwnProjectStreamEstablishes proves a member subscribing to their own
// project's stream is still admitted: the stream establishes (connected event
// flushed) rather than being denied.
func (s *EventsMembershipSuite) TestOwnProjectStreamEstablishes() {
	ok := s.streamEstablished("/api/events/stream?projectId="+s.ProjectID,
		testutil.WithAuth("e2e-test-user"))
	s.Require().True(ok, "own-project stream must establish")
}

// TestMissingProjectBadRequest asserts the missing-projectId 400 is unchanged.
func (s *EventsMembershipSuite) TestMissingProjectBadRequest() {
	resp := s.Client.GET("/api/events/stream", testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusBadRequest, resp.StatusCode,
		"missing projectId must be 400, got %d: %s", resp.StatusCode, resp.String())
}

// TestUnknownProjectNotFound asserts a session caller addressing a non-existent
// project via the query param receives 404 (no existence oracle), not 200.
func (s *EventsMembershipSuite) TestUnknownProjectNotFound() {
	resp := s.Client.GET("/api/events/stream?projectId="+uuid.New().String(),
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"unknown-project stream must be 404, got %d: %s", resp.StatusCode, resp.String())
}

// TestTokenProjectBindingForbidden proves a project-bound emt_* token that
// addresses a different project via the query parameter is rejected 403 (the
// handler-level token-binding check mirrors RequireProjectTokenScope).
func (s *EventsMembershipSuite) TestTokenProjectBindingForbidden() {
	projectB := s.newForeignProject()

	token := "emt_test_913_events_binding"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"chat:use"}, s.ProjectID))

	resp := s.Client.GET("/api/events/stream?projectId="+projectB,
		testutil.WithAuth(token))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"project token addressing a different project via query must be 403, got %d: %s",
		resp.StatusCode, resp.String())
}
