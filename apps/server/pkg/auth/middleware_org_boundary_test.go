package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// #811 — the raw X-Org-ID header must not be trusted as the request
// organization. These tests drive RequireAuth over a real (throwaway) database
// so the project→org ownership lookup is exercised end to end. They pin the
// boundary: a caller acting on a project in org A who also sends
// X-Org-ID: <org B> must not gain org-B-scoped context.

// runRequireAuth exercises RequireAuth with the given headers over the provided
// database and returns whether the wrapped handler ran, the AuthUser that
// reached the handler (nil when rejected), and the recorded HTTP status.
func runRequireAuth(t *testing.T, m *Middleware, headers map[string]string) (nextCalled bool, user *AuthUser, code int) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer read-only")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	nextCalled = false
	h := m.RequireAuth()(func(c echo.Context) error {
		nextCalled = true
		user = GetUser(c)
		return nil
	})
	_ = h(c)
	return nextCalled, user, rec.Code
}

func seedProjectInOrgA(t *testing.T, m *Middleware) {
	t.Helper()
	ctx := context.Background()
	if _, err := m.db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?) ON CONFLICT (id) DO NOTHING`, orgA, "A"); err != nil {
		t.Fatalf("seed org A: %v", err)
	}
	if _, err := m.db.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?) ON CONFLICT (id) DO NOTHING`, projID, orgA, "proj"); err != nil {
		t.Fatalf("seed project: %v", err)
	}
}

// Fail-first: a caller with org:read acting on a project in org A who sends
// X-Org-ID: <org B> must be rejected — the raw header must never widen access.
// Before the fix this test FAILS (next is called and user.OrgID == org B).
func TestRequireAuthRejectsForgedOrgHeader(t *testing.T) {
	db := setupAuthDBTest(t)
	m := newTestMiddleware(t)
	m.db = db
	seedProjectInOrgA(t, m)

	nextCalled, user, code := runRequireAuth(t, m, map[string]string{
		"X-Project-ID": projID,
		"X-Org-ID":     orgB,
	})

	if nextCalled {
		got := ""
		if user != nil {
			got = user.OrgID
		}
		t.Fatalf("forged X-Org-ID accepted: handler ran with user.OrgID=%q; want 403 rejection (raw header must not widen access)", got)
	}
	if code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a forged/mismatched X-Org-ID", code)
	}
}

// The owning org of the declared project is authoritative: it is derived from
// the database, whether or not the header is present, and a matching header is
// accepted while a mismatched one is rejected (covered by the reject test).
func TestRequireAuthDerivesOrgFromProject(t *testing.T) {
	db := setupAuthDBTest(t)
	m := newTestMiddleware(t)
	m.db = db
	seedProjectInOrgA(t, m)

	t.Run("no header derives org from project", func(t *testing.T) {
		nextCalled, user, code := runRequireAuth(t, m, map[string]string{"X-Project-ID": projID})
		if !nextCalled {
			t.Fatalf("handler did not run (code=%d); want pass-through with project-derived org", code)
		}
		if user == nil || user.OrgID != orgA {
			t.Fatalf("user.OrgID = %v, want %q (project-derived, authoritative)", user, orgA)
		}
	})

	t.Run("matching header is accepted and does not override", func(t *testing.T) {
		nextCalled, user, code := runRequireAuth(t, m, map[string]string{
			"X-Project-ID": projID,
			"X-Org-ID":     orgA,
		})
		if !nextCalled {
			t.Fatalf("handler did not run (code=%d); want pass-through for a matching header", code)
		}
		if user == nil || user.OrgID != orgA {
			t.Fatalf("user.OrgID = %v, want %q", user, orgA)
		}
	})
}

// A bare X-Org-ID with no project context is not a trusted org source: the org
// stays empty (fail closed), because org-scoped routes derive the org from the
// :orgId path parameter rather than this header.
func TestRequireAuthBareOrgHeaderNotTrusted(t *testing.T) {
	db := setupAuthDBTest(t)
	m := newTestMiddleware(t)
	m.db = db

	nextCalled, user, code := runRequireAuth(t, m, map[string]string{"X-Org-ID": orgB})
	if !nextCalled {
		t.Fatalf("handler did not run (code=%d); want pass-through with empty org", code)
	}
	if user == nil || user.OrgID != "" {
		t.Fatalf("user.OrgID = %v, want empty (bare X-Org-ID is not a trusted org source)", user)
	}
}
