package orgs_test

import (
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
