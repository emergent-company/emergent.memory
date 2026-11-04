package auth

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestDBOrgMemberBindsOrgAndUser pins the org membership SQL: a membership in
// organization A must not count in B, and a different user of A must not count.
func TestDBOrgMemberBindsOrgAndUser(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	memberID := seedAuthTestUser(t, ctx, db)
	strangerID := seedAuthTestUser(t, ctx, db)

	if _, err := db.ExecContext(ctx,
		`INSERT INTO kb.organization_memberships (organization_id, user_id, role) VALUES (?, ?, 'member')`,
		orgA, memberID); err != nil {
		t.Fatalf("seed org membership: %v", err)
	}

	m := &Middleware{db: db, cfg: &config.Config{}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	isMember := func(t *testing.T, org, user string) bool {
		t.Helper()
		ok, err := m.dbOrgMember(ctx, org, user)
		if err != nil {
			t.Fatalf("dbOrgMember(%s, %s): %v", org, user, err)
		}
		return ok
	}

	if !isMember(t, orgA, memberID) {
		t.Fatal("member of A should be a member of A")
	}
	if isMember(t, orgB, memberID) {
		t.Fatal("member of A must not be a member of B (org binding)")
	}
	if isMember(t, orgA, strangerID) {
		t.Fatal("a different user of A must not be a member (user binding)")
	}
}

// TestRequireProjectMemberEndToEnd is the fail-first reproducer for the #861
// cross-tenant gap: a session caller who is not a member of the addressed
// project's owning org must be denied (403), an unknown project must be 404
// (no existence oracle), a missing user must be 401, and a genuine member keeps
// full access. The owning org is resolved server-side from kb.projects, so a
// caller-supplied :projectId can never self-satisfy the check.
func TestRequireProjectMemberEndToEnd(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	memberID := seedAuthTestUser(t, ctx, db)
	strangerID := seedAuthTestUser(t, ctx, db)

	if _, err := db.ExecContext(ctx,
		`INSERT INTO kb.orgs (id, name) VALUES (?, ?), (?, ?)`, orgA, "A", orgB, "B"); err != nil {
		t.Fatalf("seed orgs: %v", err)
	}
	proj := uuid.NewString()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, proj, orgA, "p"); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO kb.organization_memberships (organization_id, user_id, role) VALUES (?, ?, 'member')`,
		orgA, memberID); err != nil {
		t.Fatalf("seed org membership: %v", err)
	}

	m := &Middleware{db: db, cfg: &config.Config{}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	run := func(user *AuthUser, projectID string) error {
		handler := m.RequireProjectMember()(func(c echo.Context) error { return nil })
		c := makeEchoCtx(projectID, user)
		return handler(c)
	}

	t.Run("no user -> 401", func(t *testing.T) {
		err := run(nil, proj)
		if err == nil {
			t.Fatal("want error for missing user")
		}
		if status, _ := apperror.ToHTTPError(err); status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", status)
		}
	})

	t.Run("member -> allowed", func(t *testing.T) {
		if err := run(&AuthUser{ID: memberID}, proj); err != nil {
			t.Fatalf("member denied: %v", err)
		}
	})

	t.Run("stranger -> 403", func(t *testing.T) {
		err := run(&AuthUser{ID: strangerID}, proj)
		if err == nil {
			t.Fatal("want error for non-member")
		}
		if status, _ := apperror.ToHTTPError(err); status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", status)
		}
	})

	t.Run("unknown project -> 404", func(t *testing.T) {
		err := run(&AuthUser{ID: memberID}, uuid.NewString())
		if err == nil {
			t.Fatal("want error for unknown project")
		}
		if status, _ := apperror.ToHTTPError(err); status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", status)
		}
	})

	t.Run("malformed project id -> 404, not 500", func(t *testing.T) {
		// A non-UUID must be treated as "no such project" so the raw string is
		// never cast to the uuid column (which Postgres rejects with a 500).
		err := run(&AuthUser{ID: memberID}, "not-a-uuid")
		if err == nil {
			t.Fatal("want error for malformed project id")
		}
		if status, _ := apperror.ToHTTPError(err); status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", status)
		}
	})

	t.Run("api token -> pass through (scoped by token)", func(t *testing.T) {
		err := run(&AuthUser{ID: memberID, APITokenID: "tok-1", APITokenProjectID: proj}, proj)
		if err != nil {
			t.Fatalf("api-token caller denied: %v", err)
		}
	})

	t.Run("account token (member owner) -> allowed", func(t *testing.T) {
		err := run(&AuthUser{ID: memberID, APITokenID: "tok-1", APITokenProjectID: ""}, proj)
		if err != nil {
			t.Fatalf("account-token caller with a member owner denied: %v", err)
		}
	})

	t.Run("account token (non-member owner) -> 403", func(t *testing.T) {
		err := run(&AuthUser{ID: strangerID, APITokenID: "tok-1", APITokenProjectID: ""}, proj)
		if err == nil {
			t.Fatal("want error for an account token whose owner is not a member")
		}
		if status, _ := apperror.ToHTTPError(err); status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", status)
		}
	})
}
