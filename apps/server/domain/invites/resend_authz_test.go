package invites_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// TestInviteResendAuthorityMatrix pins the tier-correct authority for
// POST /api/invites/:id/resend: identical to revoke — org_admin of the invite's
// org (and an active superadmin_full) succeed, while a plain member of the same
// org, a foreign org_admin, a bare user, and an unauthenticated caller are
// refused fail-closed. A non-existent invite is 404.
func TestInviteResendAuthorityMatrix(t *testing.T) {
	testDB, e := newAuthzServer(t)
	f := seedAuthzFixture(t, testDB.DB)

	checks := []struct {
		name  string
		path  string
		token string
		want  int
	}{
		{"unauth", "/api/invites/" + f.inviteA + "/resend", "", http.StatusUnauthorized},
		{"bare", "/api/invites/" + f.inviteA + "/resend", f.bareToken, http.StatusForbidden},
		{"member-A", "/api/invites/" + f.inviteA + "/resend", f.memberAToken, http.StatusForbidden},
		{"member-B", "/api/invites/" + f.inviteA + "/resend", f.orgAdminBToken, http.StatusForbidden},
		{"unknown-invite", "/api/invites/" + uuid.NewString() + "/resend", f.orgAdminAToken, http.StatusNotFound},
		{"org_admin-A", "/api/invites/" + f.inviteA + "/resend", f.orgAdminAToken, http.StatusOK},
		{"superadmin", "/api/invites/" + f.inviteSuper + "/resend", f.superadminToken, http.StatusOK},
	}

	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(e, http.MethodPost, tc.path, tc.token)
			if rec.Code != tc.want {
				t.Errorf("POST %s (token=%q) = %d, want %d: %s", tc.path, tc.token, rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == http.StatusOK {
				// The success body must carry the invitation AND the truthful
				// outcome field, so a caller can tell a real send from a no-op
				// (issue #1327).
				var body struct {
					ID      string `json:"id"`
					Outcome string `json:"outcome"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode resend response: %v: %s", err, rec.Body.String())
				}
				if body.ID == "" || body.Outcome == "" {
					t.Errorf("resend response missing id/outcome: %s", rec.Body.String())
				}
			}
		})
	}
}
