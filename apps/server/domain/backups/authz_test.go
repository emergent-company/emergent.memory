package backups

import (
	"net/http"
	"testing"
)

// TestBackupsAuthzPlatformTier pins the platform tier: /api/superadmin/database-backups
// admits only an active superadmin_full principal. A bare authenticated user or
// an org_admin must be refused.
func TestBackupsAuthzPlatformTier(t *testing.T) {
	testDB, e := newAuthzServer(t)
	f := seedAuthzFixture(t, testDB.DB)

	probes := []struct {
		name  string
		token string
		path  string
		want  int
	}{
		{"unauthenticated list", "", "/api/superadmin/database-backups", http.StatusUnauthorized},
		{"bare list", f.bareToken, "/api/superadmin/database-backups", http.StatusForbidden},
		{"org_admin list", f.orgAdminAToken, "/api/superadmin/database-backups", http.StatusForbidden},
		{"superadmin list", f.superadminToken, "/api/superadmin/database-backups", http.StatusOK},
		{"bare download", f.bareToken, "/api/superadmin/database-backups/" + f.dbBackup + "/download", http.StatusForbidden},
		{"org_admin download", f.orgAdminAToken, "/api/superadmin/database-backups/" + f.dbBackup + "/download", http.StatusForbidden},
		{"superadmin download", f.superadminToken, "/api/superadmin/database-backups/" + f.dbBackup + "/download", http.StatusFound},
	}

	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			if got := do(e, http.MethodGet, p.path, p.token).Code; got != p.want {
				t.Errorf("got %d, want %d", got, p.want)
			}
		})
	}
}

// TestBackupsAuthzOrgTier pins the org tier: every org-scoped backup route
// requires org_admin of the addressed org. A plain member, a foreign org_admin,
// or a bare user must be refused.
func TestBackupsAuthzOrgTier(t *testing.T) {
	testDB, e := newAuthzServer(t)
	f := seedAuthzFixture(t, testDB.DB)

	listPath := "/api/v1/organizations/" + f.orgA + "/backups"
	getPath := "/api/v1/organizations/" + f.orgA + "/backups/" + f.backupA
	delPath := "/api/v1/organizations/" + f.orgA + "/backups/" + f.backupA
	clonePath := "/api/v1/organizations/" + f.orgA + "/restore"
	cloneBody := `{"backupId":"` + f.backupA + `"}`

	probes := []struct {
		name   string
		method string
		path   string
		body   string
		token  string
		want   int
	}{
		{"unauthenticated list", http.MethodGet, listPath, "", "", http.StatusUnauthorized},
		{"bare list", http.MethodGet, listPath, "", f.bareToken, http.StatusForbidden},
		{"member list", http.MethodGet, listPath, "", f.orgMemberAToken, http.StatusForbidden},
		{"foreign org_admin list", http.MethodGet, listPath, "", f.orgAdminBToken, http.StatusForbidden},
		{"org_admin list", http.MethodGet, listPath, "", f.orgAdminAToken, http.StatusOK},

		{"bare get", http.MethodGet, getPath, "", f.bareToken, http.StatusForbidden},
		{"member get", http.MethodGet, getPath, "", f.orgMemberAToken, http.StatusForbidden},
		{"org_admin get", http.MethodGet, getPath, "", f.orgAdminAToken, http.StatusOK},

		{"member clone restore", http.MethodPost, clonePath, cloneBody, f.orgMemberAToken, http.StatusForbidden},
		{"foreign org_admin clone restore", http.MethodPost, clonePath, cloneBody, f.orgAdminBToken, http.StatusForbidden},
		{"org_admin clone restore", http.MethodPost, clonePath, cloneBody, f.orgAdminAToken, http.StatusAccepted},

		{"member delete", http.MethodDelete, delPath, "", f.orgMemberAToken, http.StatusForbidden},
		{"org_admin delete", http.MethodDelete, delPath, "", f.orgAdminAToken, http.StatusNoContent},
	}

	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			var rec = doJSON(e, p.method, p.path, p.token, p.body)
			if p.body == "" {
				rec = do(e, p.method, p.path, p.token)
			}
			if rec.Code != p.want {
				t.Errorf("got %d, want %d (body: %s)", rec.Code, p.want, rec.Body.String())
			}
		})
	}
}

// TestBackupsAuthzProjectTier pins the project tier: project backup create and
// overwrite-restore require project_admin of the addressed project OR org_admin
// of its owning org. A bare user, a plain org member, or a project_user is
// refused.
func TestBackupsAuthzProjectTier(t *testing.T) {
	testDB, e := newAuthzServer(t)
	f := seedAuthzFixture(t, testDB.DB)

	createPath := "/api/v1/projects/" + f.projectA + "/backups"
	overwritePath := "/api/v1/projects/" + f.projectA + "/restore"
	overwriteBody := `{"backupId":"` + f.backupA + `"}`

	probes := []struct {
		name   string
		method string
		path   string
		body   string
		token  string
		want   int
	}{
		{"unauthenticated create", http.MethodPost, createPath, "", "", http.StatusUnauthorized},
		{"bare create", http.MethodPost, createPath, "", f.bareToken, http.StatusForbidden},
		{"member create", http.MethodPost, createPath, "", f.orgMemberAToken, http.StatusForbidden},
		{"project_admin create", http.MethodPost, createPath, "", f.projectAdminAToken, http.StatusAccepted},
		{"org_admin create", http.MethodPost, createPath, "", f.orgAdminAToken, http.StatusAccepted},

		{"bare overwrite restore", http.MethodPost, overwritePath, overwriteBody, f.bareToken, http.StatusForbidden},
		{"member overwrite restore", http.MethodPost, overwritePath, overwriteBody, f.orgMemberAToken, http.StatusForbidden},
		{"project_admin overwrite restore", http.MethodPost, overwritePath, overwriteBody, f.projectAdminAToken, http.StatusAccepted},
	}

	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			var rec = doJSON(e, p.method, p.path, p.token, p.body)
			if p.body == "" {
				rec = do(e, p.method, p.path, p.token)
			}
			if rec.Code != p.want {
				t.Errorf("got %d, want %d (body: %s)", rec.Code, p.want, rec.Body.String())
			}
		})
	}
}

// TestBackupsAuthzRestoreChildTier pins the child-resource tier: reading a
// restore requires org_admin of the restore's owning org, and a foreign or
// missing id is a 404 (not-found-on-foreign), not a 403.
func TestBackupsAuthzRestoreChildTier(t *testing.T) {
	testDB, e := newAuthzServer(t)
	f := seedAuthzFixture(t, testDB.DB)

	path := "/api/v1/restores/" + f.restoreA

	probes := []struct {
		name  string
		token string
		want  int
	}{
		{"unauthenticated", "", http.StatusUnauthorized},
		{"bare (foreign)", f.bareToken, http.StatusNotFound},
		{"member (foreign)", f.orgMemberAToken, http.StatusNotFound},
		{"foreign org_admin", f.orgAdminBToken, http.StatusNotFound},
		{"owning org_admin", f.orgAdminAToken, http.StatusOK},
	}

	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			if got := do(e, http.MethodGet, path, p.token).Code; got != p.want {
				t.Errorf("got %d, want %d", got, p.want)
			}
		})
	}
}
