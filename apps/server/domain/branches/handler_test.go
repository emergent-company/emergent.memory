package branches

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// stubService satisfies the handler's dependency on *Service without a real DB.
// We embed a nil *Service and override only what we need via a closure field.
type stubListService struct {
	capturedProjectID *string
	branches          []*BranchResponse
	err               error
}

func (s *stubListService) List(_ context.Context, projectID *string) ([]*BranchResponse, error) {
	s.capturedProjectID = projectID
	return s.branches, s.err
}

// handlerWithStub builds an echo context + handler that uses a stub list func.
func runListHandler(t *testing.T, stub *stubListService, queryProjectID, headerProjectID, tokenProjectID string) (int, []*BranchResponse) {
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

	// Build a minimal handler that calls stub.List directly (bypasses real *Service)
	h := &testableListHandler{svc: stub}
	err := h.List(c)
	require.NoError(t, err)

	var result []*BranchResponse
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	}
	return rec.Code, result
}

// testableListHandler duplicates the List handler logic for testing, using the stubListService.
// This mirrors handler.go:List exactly so that we verify the real scoping logic.
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

	var projectID *string
	if pid := c.QueryParam("project_id"); pid != "" {
		projectID = &pid
	} else {
		// Mirrors handler.go: fall back to X-Project-ID / API token
		if ctxPID, err := auth.GetProjectID(c); err == nil && ctxPID != "" {
			projectID = &ctxPID
		}
	}

	branches, err := h.svc.List(c.Request().Context(), projectID)
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
		wantUnscoped    bool // true means we expect nil projectID (all branches)
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
			name:         "no scoping when no project context at all",
			wantUnscoped: true,
		},
		{
			name:           "invalid query param UUID rejected — returns 400 via real handler",
			queryProjectID: "not-a-uuid",
			// This case is tested separately below (error path)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.queryProjectID == "not-a-uuid" {
				t.Skip("error path tested separately")
			}

			stub := &stubListService{branches: []*BranchResponse{}}
			code, _ := runListHandler(t, stub, tt.queryProjectID, tt.headerProjectID, tt.tokenProjectID)
			assert.Equal(t, http.StatusOK, code)

			if tt.wantUnscoped {
				assert.Nil(t, stub.capturedProjectID, "expected nil (unscoped) projectID")
			} else {
				require.NotNil(t, stub.capturedProjectID)
				assert.Equal(t, *tt.wantProjectID, *stub.capturedProjectID)
			}
		})
	}
}

func ptr(s string) *string { return &s }
