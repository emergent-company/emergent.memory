// Package api_test — orgs_test.go
//
// Tests for the organizations API (/api/orgs).
// Ported from emergent.memory/apps/server/tests/e2e/orgs_test.go
package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestOrgs_ListRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	rl.Section("GET /api/orgs without auth")
	resp := doAPILogged(t, rl, "GET", "/api/orgs", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestOrgs_ListReturnsUserOrgs(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	orgID := createOrg(t, uniqueName("e2e-list-org"))

	rl.Section("GET /api/orgs — see created org")
	resp := doAPILogged(t, rl, "GET", "/api/orgs", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var orgs []map[string]any
	parseBodyJSON(t, body, &orgs)
	if len(orgs) < 1 {
		t.Fatal("expected at least one org in list")
	}
	found := false
	for _, o := range orgs {
		if o["id"] == orgID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("created org %s not found in list", orgID)
	}
	rl.Printf("found org %s in list of %d orgs", orgID, len(orgs))
}

func TestOrgs_GetRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/orgs/some-id", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

// TestOrgs_GetNonexistentForbidden covers the org-scoped membership contract
// (issue #851): an authenticated caller who is not a member of the addressed
// org receives 403 uniformly, whether the org exists or not, so the response
// cannot disclose an org's existence.
func TestOrgs_GetNonexistentForbidden(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/orgs/00000000-0000-0000-0000-000000000000", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusForbidden)
}

func TestOrgs_GetSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	orgID := createOrg(t, uniqueName("e2e-get-org"))

	rl.Section("GET /api/orgs/:id")
	resp := doAPILogged(t, rl, "GET", "/api/orgs/"+orgID, e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var org map[string]any
	parseBodyJSON(t, body, &org)
	if org["id"] != orgID {
		t.Errorf("expected org id=%s, got %v", orgID, org["id"])
	}
	if org["name"] == "" || org["name"] == nil {
		t.Error("expected non-empty org name")
	}
	rl.Printf("org %s fetched OK", orgID)
}

func TestOrgs_CreateRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/orgs", "", "", jsonBody(map[string]any{"name": "New Org"}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestOrgs_CreateSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	name := uniqueName("e2e-create-org")
	rl.Section("POST /api/orgs")
	resp := doAPILogged(t, rl, "POST", "/api/orgs", e2eTestToken(), "", jsonBody(map[string]any{"name": name}))
	body := mustStatus(t, resp, http.StatusCreated)

	var org map[string]any
	parseBodyJSON(t, body, &org)
	orgID, _ := org["id"].(string)
	if orgID == "" {
		t.Fatal("expected non-empty org id in response")
	}
	if org["name"] != name {
		t.Errorf("expected name=%s, got %v", name, org["name"])
	}
	t.Cleanup(func() { deleteOrg(t, orgID) })

	// Verify in list
	listResp := doAPILogged(t, rl, "GET", "/api/orgs", e2eTestToken(), "", nil)
	listBody := mustStatus(t, listResp, http.StatusOK)
	var orgs []map[string]any
	parseBodyJSON(t, listBody, &orgs)
	found := false
	for _, o := range orgs {
		if o["id"] == orgID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("created org %s not found in list", orgID)
	}
	rl.Printf("created org %s", orgID)
}

func TestOrgs_CreateTrimsWhitespace(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/orgs", e2eTestToken(), "", jsonBody(map[string]any{"name": "  Trimmed Name  "}))
	body := mustStatus(t, resp, http.StatusCreated)

	var org map[string]any
	parseBodyJSON(t, body, &org)
	orgID, _ := org["id"].(string)
	t.Cleanup(func() { deleteOrg(t, orgID) })

	if org["name"] != "Trimmed Name" {
		t.Errorf("expected trimmed name, got %v", org["name"])
	}
	rl.Printf("org name trimmed correctly")
}

func TestOrgs_CreateEmptyNameFails(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/orgs", e2eTestToken(), "", jsonBody(map[string]any{"name": ""}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestOrgs_CreateWhitespaceOnlyNameFails(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/orgs", e2eTestToken(), "", jsonBody(map[string]any{"name": "   "}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestOrgs_CreateNameTooLongFails(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/orgs", e2eTestToken(), "", jsonBody(map[string]any{
		"name": strings.Repeat("a", 121),
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestOrgs_CreateInvalidJSONFails(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/orgs", e2eTestToken(), "", []byte(`{invalid json`))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestOrgs_CreateMissingNameFails(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/orgs", e2eTestToken(), "", jsonBody(map[string]any{}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestOrgs_CreatorSeesOrg(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/orgs", e2eTestToken(), "", jsonBody(map[string]any{"name": uniqueName("e2e-creator-org")}))
	body := mustStatus(t, resp, http.StatusCreated)

	var org map[string]any
	parseBodyJSON(t, body, &org)
	orgID, _ := org["id"].(string)
	t.Cleanup(func() { deleteOrg(t, orgID) })

	// Creator can get the org
	getResp := doAPILogged(t, rl, "GET", "/api/orgs/"+orgID, e2eTestToken(), "", nil)
	mustStatus(t, getResp, http.StatusOK)

	// Creator sees org in list
	listResp := doAPILogged(t, rl, "GET", "/api/orgs", e2eTestToken(), "", nil)
	listBody := mustStatus(t, listResp, http.StatusOK)
	var orgs []map[string]any
	parseBodyJSON(t, listBody, &orgs)
	found := false
	for _, o := range orgs {
		if o["id"] == orgID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("creator should see org %s in their list", orgID)
	}
	rl.Printf("creator sees their org in list")
}

func TestOrgs_DeleteRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/orgs/some-id", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

// TestOrgs_DeleteNonexistentForbidden covers the org-scoped membership contract
// (issue #851): a non-member cannot delete an org, and the uniform 403 does not
// disclose whether the addressed org exists.
func TestOrgs_DeleteNonexistentForbidden(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/orgs/00000000-0000-0000-0000-000000000000", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusForbidden)
}

func TestOrgs_DeleteSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// Create org without registering cleanup (we'll delete it manually)
	resp := doAPILogged(t, rl, "POST", "/api/orgs", e2eTestToken(), "", jsonBody(map[string]any{"name": uniqueName("e2e-delete-org")}))
	body := mustStatus(t, resp, http.StatusCreated)
	orgID := parseIDFromBody(t, body)

	rl.Section("DELETE /api/orgs/:id")
	delResp := doAPILogged(t, rl, "DELETE", "/api/orgs/"+orgID, e2eTestToken(), "", nil)
	delBody := mustStatus(t, delResp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, delBody, &result)
	if result["status"] != "deleted" {
		t.Errorf("expected status=deleted, got %v", result["status"])
	}

	// Verify gone: after deleting the org, the caller's membership is also gone,
	// so a follow-up GET is rejected 403 (uniform non-member) rather than 404.
	getResp := doAPILogged(t, rl, "GET", "/api/orgs/"+orgID, e2eTestToken(), "", nil)
	mustStatus(t, getResp, http.StatusForbidden)
	rl.Printf("org %s deleted and confirmed gone", orgID)
}

func TestOrgs_ListMultipleOrgs(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	for i := 1; i <= 2; i++ {
		createOrg(t, fmt.Sprintf("e2e-multi-org-%d-%s", i, uniqueName("x")))
	}

	resp := doAPILogged(t, rl, "GET", "/api/orgs", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var orgs []map[string]any
	parseBodyJSON(t, body, &orgs)
	if len(orgs) < 2 {
		t.Errorf("expected at least 2 orgs, got %d", len(orgs))
	}
	rl.Printf("list returned %d orgs", len(orgs))
}

func TestOrgs_GetResponseStructure(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	orgID := createOrg(t, uniqueName("e2e-struct-org"))

	resp := doAPILogged(t, rl, "GET", "/api/orgs/"+orgID, e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var org map[string]any
	parseBodyJSON(t, body, &org)
	for _, field := range []string{"id", "name"} {
		if _, ok := org[field]; !ok {
			t.Errorf("org response missing field %q", field)
		}
	}
	rl.Printf("org response structure OK")
}
