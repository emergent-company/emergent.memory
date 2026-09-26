package invites_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// TestInviteRevokeAuthorityMatrix pins the tier-correct authority for
// DELETE /api/invites/:id (revoke): org_admin of the invite's org (and an
// active superadmin_full) succeed, while a plain member of the same org (who
// could previously revoke), a foreign org_admin, a bare user, and an
// unauthenticated caller are refused fail-closed. A non-existent invite is 404.
func TestInviteRevokeAuthorityMatrix(t *testing.T) {
	testDB, e := newAuthzServer(t)
	f := seedAuthzFixture(t, testDB.DB)

	// The 401/403/404 cases never mutate the invite, so they may target inviteA
	// in any order. The two 204 cases mutate, so they use distinct invites and
	// run last.
	checks := []struct {
		name  string
		path  string
		token string
		want  int
	}{
		{"unauth", "/api/invites/" + f.inviteA, "", http.StatusUnauthorized},
		{"bare", "/api/invites/" + f.inviteA, f.bareToken, http.StatusForbidden},
		{"member-A", "/api/invites/" + f.inviteA, f.memberAToken, http.StatusForbidden},
		{"member-B", "/api/invites/" + f.inviteA, f.orgAdminBToken, http.StatusForbidden},
		{"unknown-invite", "/api/invites/" + uuid.NewString(), f.orgAdminAToken, http.StatusNotFound},
		{"org_admin-A", "/api/invites/" + f.inviteA, f.orgAdminAToken, http.StatusNoContent},
		{"superadmin", "/api/invites/" + f.inviteSuper, f.superadminToken, http.StatusNoContent},
	}

	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(e, http.MethodDelete, tc.path, tc.token)
			if rec.Code != tc.want {
				t.Errorf("DELETE %s (token=%q) = %d, want %d: %s", tc.path, tc.token, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
