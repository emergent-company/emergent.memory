package branches

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// stubService satisfies the handler's dependency on *Service without a real DB.
type stubListService struct {
	capturedProjectID *string
	branches          []*BranchResponse
	err               error
}

func (s *stubListService) List(_ context.Context, projectID *string) ([]*BranchResponse, error) {
	s.capturedProjectID = projectID
	return s.branches, s.err
}

// runListHandler builds an echo context + handler that uses a stub list func.
// It returns the HTTP status written to the recorder and the error returned by
// the handler (handlers return apperror values directly, which Echo's error
// middleware would normally translate into an HTTP status).
func runListHandler(t *testing.T, stub *stubListService, queryProjectID, headerProjectID, tokenProjectID string) (int, error) {
	t.Helper()

	e := echo.New()
	url := "/api/graph/branches"
	if queryProjectID != "" {
		url += "?project_id=" + queryProjectID
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	if headerProjectID != "" {
		req.Header.Set("X-Project-ID", headerProjectID)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set up auth user
	user := &auth.AuthUser{
		ID:                "user-1",
		ProjectID:         headerProjectID,
		APITokenProjectID: tokenProjectID,
	}
	c.Set(string(auth.UserContextKey), user)

	h := &testableListHandler{svc: stub}
	err := h.List(c)
	return rec.Code, err
}

// testableListHandler duplicates the List handler logic for testing, using the stubListService.
type testableListHandler struct {
	svc interface {
		List(ctx context.Context, projectID *string) ([]*BranchResponse, error)
	}
}

func (h *testableListHandler) List(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return echo.ErrUnauthorized
	}

	projectID, err := resolveProjectID(c)
	if err != nil {
		return err
	}

	branches, err := h.svc.List(c.Request().Context(), &projectID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, branches)
}

// =============================================================================
// Tests
// =============================================================================

func TestListHandler_ProjectScoping(t *testing.T) {
	const projectA = "aaaaaaaa-0000-0000-0000-000000000001"
	const projectB = "bbbbbbbb-0000-0000-0000-000000000002"

	tests := []struct {
		name            string
		queryProjectID  string
		headerProjectID string
		tokenProjectID  string
		wantProjectID   *string
		wantErr         bool
	}{
		{
			name:            "query param takes precedence over header",
			queryProjectID:  projectA,
			headerProjectID: projectB,
			wantProjectID:   ptr(projectA),
		},
		{
			name:            "falls back to X-Project-ID header when no query param",
			headerProjectID: projectA,
			wantProjectID:   ptr(projectA),
		},
		{
			name:           "falls back to API token project when no query param or header",
			tokenProjectID: projectA,
			wantProjectID:  ptr(projectA),
		},
		{
			name:    "no project context returns bad request",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubListService{branches: []*BranchResponse{}}
			code, err := runListHandler(t, stub, tt.queryProjectID, tt.headerProjectID, tt.tokenProjectID)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, stub.capturedProjectID)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, code)
			require.NotNil(t, stub.capturedProjectID)
			assert.Equal(t, *tt.wantProjectID, *stub.capturedProjectID)
		})
	}
}

func ptr(s string) *string { return &s }
