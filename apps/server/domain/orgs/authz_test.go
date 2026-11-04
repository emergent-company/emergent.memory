package orgs_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOrgWriteAuthorityMatrix pins the tier-correct authority for every
// org-tier write/read-PII route in domain/orgs: org_admin of the addressed org
// succeeds, while an unauthenticated caller (401), a bare user (403), a plain
// member of the same org (403), and an org_admin of a different org (403) are
// all refused. This is the caller-class matrix for mechanism-2 (wrong authority
// level) from the surface→authority matrix.
func TestOrgWriteAuthorityMatrix(t *testing.T) {
	testDB, e := newAuthzServer(t)
	f := seedAuthzFixture(t, testDB.DB)

	renameBody := `{"name":"renamed"}`
	toolBody := `{"enabled":true}`
	checks := []struct {
		name   string
		method string
		path   string
		body   string
		token  string
		want   int
	}{
		// rename org (PATCH)
		{"rename/unauth", http.MethodPatch, "/api/orgs/" + f.orgA, renameBody, "", http.StatusUnauthorized},
		{"rename/bare", http.MethodPatch, "/api/orgs/" + f.orgA, renameBody, f.bareToken, http.StatusForbidden},
		{"rename/member-A", http.MethodPatch, "/api/orgs/" + f.orgA, renameBody, f.memberAToken, http.StatusForbidden},
		{"rename/member-B", http.MethodPatch, "/api/orgs/" + f.orgA, renameBody, f.orgAdminBToken, http.StatusForbidden},
		{"rename/superadmin-not-org-admin", http.MethodPatch, "/api/orgs/" + f.orgA, renameBody, f.superadminToken, http.StatusForbidden},
		{"rename/org_admin-A", http.MethodPatch, "/api/orgs/" + f.orgA, renameBody, f.orgAdminAToken, http.StatusOK},
		// member listing (PII)
		{"members/unauth", http.MethodGet, "/api/orgs/" + f.orgA + "/members", "", "", http.StatusUnauthorized},
		{"members/bare", http.MethodGet, "/api/orgs/" + f.orgA + "/members", "", f.bareToken, http.StatusForbidden},
		{"members/member-A", http.MethodGet, "/api/orgs/" + f.orgA + "/members", "", f.memberAToken, http.StatusForbidden},
		{"members/member-B", http.MethodGet, "/api/orgs/" + f.orgA + "/members", "", f.orgAdminBToken, http.StatusForbidden},
		{"members/org_admin-A", http.MethodGet, "/api/orgs/" + f.orgA + "/members", "", f.orgAdminAToken, http.StatusOK},
		// tool-settings write (PUT)
		{"toolsetting-put/unauth", http.MethodPut, "/api/admin/orgs/" + f.orgA + "/tool-settings/other", toolBody, "", http.StatusUnauthorized},
		{"toolsetting-put/member-A", http.MethodPut, "/api/admin/orgs/" + f.orgA + "/tool-settings/other", toolBody, f.memberAToken, http.StatusForbidden},
		{"toolsetting-put/member-B", http.MethodPut, "/api/admin/orgs/" + f.orgA + "/tool-settings/other", toolBody, f.orgAdminBToken, http.StatusForbidden},
		{"toolsetting-put/org_admin-A", http.MethodPut, "/api/admin/orgs/" + f.orgA + "/tool-settings/other", toolBody, f.orgAdminAToken, http.StatusOK},
		// tool-settings delete (DELETE)
		{"toolsetting-delete/member-A", http.MethodDelete, "/api/admin/orgs/" + f.orgA + "/tool-settings/web", "", f.memberAToken, http.StatusForbidden},
		{"toolsetting-delete/member-B", http.MethodDelete, "/api/admin/orgs/" + f.orgA + "/tool-settings/web", "", f.orgAdminBToken, http.StatusForbidden},
		{"toolsetting-delete/org_admin-A", http.MethodDelete, "/api/admin/orgs/" + f.orgA + "/tool-settings/web", "", f.orgAdminAToken, http.StatusOK},
	}

	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(e, tc.method, tc.path, tc.token)
			if tc.body != "" {
				rec = doJSON(e, tc.method, tc.path, tc.token, tc.body)
			}
			if rec.Code != tc.want {
				t.Errorf("%s %s (token=%q) = %d, want %d: %s", tc.method, tc.path, tc.token, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestOrgDeleteAuthorityMatrix pins the destructive delete-org bar: org_admin of
// the addressed org succeeds; a plain member, a foreign org_admin, a bare user,
// and an unauthenticated caller are all refused.
func TestOrgDeleteAuthorityMatrix(t *testing.T) {
	testDB, e := newAuthzServer(t)
	f := seedAuthzFixture(t, testDB.DB)

	delPath := "/api/orgs/" + f.orgDel
	checks := []struct {
		name  string
		token string
		want  int
	}{
		{"unauth", "", http.StatusUnauthorized},
		{"bare", f.bareToken, http.StatusForbidden},
		{"member-A", f.memberAToken, http.StatusForbidden},
		{"member-B", f.orgAdminBToken, http.StatusForbidden},
		{"superadmin-not-org-admin", f.superadminToken, http.StatusForbidden},
		{"org_admin-A", f.orgAdminAToken, http.StatusOK},
	}

	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(e, http.MethodDelete, delPath, tc.token)
			if rec.Code != tc.want {
				t.Errorf("DELETE %s (token=%q) = %d, want %d: %s", delPath, tc.token, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestOrgDecisionIgnoresSpoofedOrgHeader is the issue #812 Q10 / #1162 guard: a
// client-supplied X-Org-ID must not influence an org-scoped decision. Org context
// is route/server-derived only, so a caller cannot self-grant by naming an org in
// the header, and a foreign org_admin cannot widen into the target org by
// claiming it in the header.
func TestOrgDecisionIgnoresSpoofedOrgHeader(t *testing.T) {
	testDB, e := newAuthzServer(t)
	f := seedAuthzFixture(t, testDB.DB)

	do := func(method, path, token, orgHeader string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(`{"name":"renamed"}`))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if orgHeader != "" {
			req.Header.Set("X-Org-ID", orgHeader)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	t.Run("foreign org_admin claiming the target org in the header is refused", func(t *testing.T) {
		rec := do(http.MethodPatch, "/api/orgs/"+f.orgA, f.orgAdminBToken, f.orgA)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("PATCH orgA as foreign org_admin with X-Org-ID=orgA = %d, want 403: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("bare caller cannot self-grant an org via the header", func(t *testing.T) {
		rec := do(http.MethodPatch, "/api/orgs/"+f.orgB, f.bareToken, f.orgB)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("PATCH orgB as bare caller with X-Org-ID=orgB = %d, want 403: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("plain member cannot self-grant org-admin via the header", func(t *testing.T) {
		rec := do(http.MethodDelete, "/api/orgs/"+f.orgDel, f.memberAToken, f.orgDel)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("DELETE orgDel as plain member with X-Org-ID=orgDel = %d, want 403: %s", rec.Code, rec.Body.String())
		}
	})
}

// TestOrgMemberWriteAuthorityMatrix pins the org-admin bar for the organization
// member add/role/remove routes. Wrong-authority callers are refused before any
// write; an org_admin of the addressed org succeeds.
func TestOrgMemberWriteAuthorityMatrix(t *testing.T) {
	testDB, e := newAuthzServer(t)
	f := seedAuthzFixture(t, testDB.DB)
	_ = f

	orgID := f.orgDel
	base := "/api/orgs/" + orgID + "/members"

	// Direct add.
	addBody := `{"email":"` + f.bareToken + `@example.com","role":"member"}`
	for _, tc := range []struct {
		name  string
		token string
		want  int
	}{
		{"add/unauth", "", http.StatusUnauthorized},
		{"add/bare", f.bareToken, http.StatusForbidden},
		{"add/member-A", f.memberAToken, http.StatusForbidden},
		{"add/member-B", f.orgAdminBToken, http.StatusForbidden},
		{"add/org_admin-A", f.orgAdminAToken, http.StatusCreated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(e, http.MethodPost, base, tc.token, addBody)
			if rec.Code != tc.want {
				t.Errorf("POST %s (token=%q) = %d, want %d: %s", base, tc.token, rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	// Role change: promote the plain member to org_admin.
	// The member's id is not exposed by the fixture; resolve it from the DB.
	var memberID string
	if err := testDB.DB.NewRaw(
		`SELECT user_id FROM kb.organization_memberships WHERE organization_id = ? AND role = 'member' LIMIT 1`,
		orgID,
	).Scan(context.Background(), &memberID); err != nil {
		t.Fatalf("resolve member id: %v", err)
	}
	rolePath := base + "/" + memberID
	for _, tc := range []struct {
		name  string
		token string
		want  int
	}{
		{"role/unauth", "", http.StatusUnauthorized},
		{"role/bare", f.bareToken, http.StatusForbidden},
		{"role/member-A", f.memberAToken, http.StatusForbidden},
		{"role/member-B", f.orgAdminBToken, http.StatusForbidden},
		{"role/org_admin-A", f.orgAdminAToken, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(e, http.MethodPatch, rolePath, tc.token, `{"role":"org_admin"}`)
			if rec.Code != tc.want {
				t.Errorf("PATCH %s (token=%q) = %d, want %d: %s", rolePath, tc.token, rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	// Demote the member back to plain member so the removal matrix exercises a
	// non-admin caller and a non-admin target.
	demote := doJSON(e, http.MethodPatch, rolePath, f.orgAdminAToken, `{"role":"member"}`)
	if demote.Code != http.StatusOK {
		t.Fatalf("demote member before removal = %d: %s", demote.Code, demote.Body.String())
	}

	// Remove the member (orgDel still has orgAdminA as admin).
	removePath := base + "/" + memberID
	for _, tc := range []struct {
		name  string
		token string
		want  int
	}{
		{"remove/unauth", "", http.StatusUnauthorized},
		{"remove/bare", f.bareToken, http.StatusForbidden},
		{"remove/member-A", f.memberAToken, http.StatusForbidden},
		{"remove/member-B", f.orgAdminBToken, http.StatusForbidden},
		{"remove/org_admin-A", f.orgAdminAToken, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(e, http.MethodDelete, removePath, tc.token)
			if rec.Code != tc.want {
				t.Errorf("DELETE %s (token=%q) = %d, want %d: %s", removePath, tc.token, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
