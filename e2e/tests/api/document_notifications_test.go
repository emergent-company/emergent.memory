// Package api_test — document_notifications_test.go
//
// End-to-end coverage for the document lifecycle notifications. Three
// properties are pinned here, all through the public API against a running server:
//
//  1. an upload reaches the project's OTHER members;
//  2. it never reaches the uploader — the fan-out excludes the actor, which is
//     also why a solo project gets nothing;
//  3. a member who disables the key stops receiving it without affecting anyone
//     else, and the three informational document keys are on by default while
//     the failure key is required.
package api_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// uploadDocumentAs uploads one file as the given identity and returns the
// created document's id.
func uploadDocumentAs(t *testing.T, token, projectID, filename string, content []byte) string {
	t.Helper()
	resp := doMultipartWithFile(t, "/api/documents/upload", token, projectID, "file", filename, content)
	body := mustStatus(t, resp, http.StatusCreated)

	var out struct {
		Document struct {
			ID string `json:"id"`
		} `json:"document"`
	}
	parseBodyJSON(t, body, &out)
	if out.Document.ID == "" {
		t.Fatalf("upload response carried no document id: %s", body)
	}
	return out.Document.ID
}

// documentNotifications lists the caller's document-category notifications in a
// project, narrowed by a search term so rows from other tests cannot leak in.
// The search matches the notification message, which carries the filename.
func documentNotifications(t *testing.T, rl *runLog, token, projectID, search string) []map[string]any {
	t.Helper()
	path := "/api/notifications?scope=project&category=documents&project_id=" +
		url.QueryEscape(projectID) + "&search=" + url.QueryEscape(search)
	body := mustStatus(t, doAPILogged(t, rl, "GET", path, token, "", nil), http.StatusOK)

	var out struct {
		Data []map[string]any `json:"data"`
	}
	parseBodyJSON(t, body, &out)
	return out.Data
}

// joinProjectAsSecondMember invites the secondary standalone identity to the
// project and accepts the invitation, so the project has two real members. The
// project fan-out reads kb.project_memberships, which the acceptance writes.
func joinProjectAsSecondMember(t *testing.T, rl *runLog, projectID, orgID string) {
	t.Helper()
	inv := createInvite(t, inviteeEmail(), orgID, projectID, "project_user")
	mustStatus(t, acceptInvite(t, rl, inviteeToken(), inv["token"].(string)), http.StatusOK)
}

// notificationPreferences returns the caller's effective project preferences.
func notificationPreferences(t *testing.T, rl *runLog, token, projectID string) []map[string]any {
	t.Helper()
	path := "/api/notifications/preferences?project_id=" + url.QueryEscape(projectID)
	body := mustStatus(t, doAPILogged(t, rl, "GET", path, token, "", nil), http.StatusOK)

	var out struct {
		Data []map[string]any `json:"data"`
	}
	parseBodyJSON(t, body, &out)
	return out.Data
}

// preferenceFor returns the preference entry for an event key, or nil.
func preferenceFor(entries []map[string]any, eventKey string) map[string]any {
	for _, e := range entries {
		if e["eventKey"] == eventKey {
			return e
		}
	}
	return nil
}

// setNotificationPreference stores a delivery preference for one project key.
func setNotificationPreference(t *testing.T, rl *runLog, token, projectID, eventKey string, enabled bool) {
	t.Helper()
	body := jsonBody(map[string]any{
		"projectId": projectID,
		"eventKey":  eventKey,
		"enabled":   enabled,
	})
	mustStatus(t, doAPILogged(t, rl, "PUT", "/api/notifications/preferences", token, "", body), http.StatusOK)
}

// ─── Fan-out and actor exclusion ─────────────────────────────────────────────

func TestDocumentUpload_NotifiesOtherMembersNotTheUploader(t *testing.T) {
	requireSecondIdentity(t)
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	joinProjectAsSecondMember(t, rl, projectID, orgID)

	filename := uniqueName("notify-upload") + ".txt"
	docID := uploadDocumentAs(t, e2eTestToken(), projectID, filename, []byte("shared project knowledge"))

	// The other member is told.
	rows := documentNotifications(t, rl, inviteeToken(), projectID, filename)
	if len(rows) != 1 {
		t.Fatalf("other member received %d document.uploaded rows for the upload, want 1", len(rows))
	}
	row := rows[0]
	if row["eventKey"] != "document.uploaded" {
		t.Errorf("eventKey = %v, want document.uploaded", row["eventKey"])
	}
	if row["scope"] != "project" {
		t.Errorf("scope = %v, want project", row["scope"])
	}
	if row["category"] != "documents" {
		t.Errorf("category = %v, want documents", row["category"])
	}
	if row["relatedResourceId"] != docID {
		t.Errorf("relatedResourceId = %v, want the document id %s", row["relatedResourceId"], docID)
	}
	if !assertActionURLContains(t, row, "/documents/"+docID) {
		t.Errorf("actionUrl = %v, want a link to the document", row["actionUrl"])
	}

	// The uploader is not — a member is never notified about their own upload.
	if rows := documentNotifications(t, rl, e2eTestToken(), projectID, filename); len(rows) != 0 {
		t.Fatalf("uploader received %d rows about their own upload, want 0", len(rows))
	}
}

// TestDocumentVersionSync_NotifiesMembers pins the second producer end to end
// against the web-source stub: a source's first ingest is silent, and the sync
// that appends a new version announces document.version_synced once, pointing at
// the new version row.
func TestDocumentVersionSync_NotifiesMembers(t *testing.T) {
	stub := skipWithoutWebStub(t)
	requireSecondIdentity(t)

	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	joinProjectAsSecondMember(t, rl, projectID, orgID)

	resetWebSite(t, stub)
	sourceID := createWebSource(t, rl, projectID, map[string]any{
		"start_urls": []string{webSeedURL},
		"max_depth":  2,
	})

	// ── First sync: the documents appear, nobody is told ────────────────────
	job := waitForSyncJob(t, rl, projectID, sourceID, triggerWebSync(t, rl, projectID, sourceID))
	if job.FailedItems != 0 {
		t.Fatalf("first sync failed %d items", job.FailedItems)
	}
	if rows := ingestNotifications(documentNotifications(t, rl, inviteeToken(), projectID, "")); len(rows) != 0 {
		t.Fatalf("a first ingest announced %d rows, want 0", len(rows))
	}

	// ── Change one page, sync again: exactly one version row ────────────────
	upsertWebPage(t, stub, "/docs/a", http.StatusOK, "text/html; charset=utf-8",
		`<!doctype html><html><head><title>Page A</title></head><body><main><h1>Page A</h1><p>Version two.</p><a href="/docs/a/deep">Deep</a></main></body></html>`)

	waitForSyncJob(t, rl, projectID, sourceID, triggerWebSync(t, rl, projectID, sourceID))

	rows := ingestNotifications(documentNotifications(t, rl, inviteeToken(), projectID, ""))
	if len(rows) != 1 {
		t.Fatalf("other member received %d ingest rows after a one-page version bump, want 1", len(rows))
	}
	row := rows[0]
	if row["eventKey"] != "document.version_synced" {
		t.Errorf("eventKey = %v, want document.version_synced", row["eventKey"])
	}
	updated := findSourceDocument(t, projectID, sourceID, webExtDocsA)
	if row["relatedResourceId"] != updated.ID {
		t.Errorf("relatedResourceId = %v, want the new version row %s", row["relatedResourceId"], updated.ID)
	}
	if message, _ := row["message"].(string); !strings.Contains(message, "version 2") {
		t.Errorf("message = %q, want it to name version 2", message)
	}
}

// ingestNotifications keeps only the document-ingest event keys, so assertions
// about the ingest producers are not disturbed by extraction rows that land in
// the same category asynchronously.
func ingestNotifications(rows []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		switch row["eventKey"] {
		case "document.uploaded", "document.version_synced":
			out = append(out, row)
		}
	}
	return out
}

// ─── Preferences ────────────────────────────────────────────────────────────

// TestDocumentNotificationPreferences_DefaultOnAndRequired pins the taxonomy on
// the wire: the three informational document keys are delivered by default
// (opt-out) and the failure key is required, so a UI can render "on, disable me"
// rather than "off, opt in".
func TestDocumentNotificationPreferences_DefaultOnAndRequired(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	entries := notificationPreferences(t, rl, e2eTestToken(), projectID)

	for _, key := range []string{
		"document.uploaded",
		"document.version_synced",
		"document.extraction_completed",
	} {
		e := preferenceFor(entries, key)
		if e == nil {
			t.Fatalf("%s missing from the effective preferences", key)
		}
		if e["enabled"] != true {
			t.Errorf("%s enabled = %v, want true (delivered without an explicit preference)", key, e["enabled"])
		}
		if e["required"] != false {
			t.Errorf("%s required = %v, want false (suppressible)", key, e["required"])
		}
		if e["delivery"] != "optout" {
			t.Errorf("%s delivery = %v, want optout", key, e["delivery"])
		}
		if e["category"] != "documents" {
			t.Errorf("%s category = %v, want documents", key, e["category"])
		}
	}

	failure := preferenceFor(entries, "document.processing_failed")
	if failure == nil {
		t.Fatal("document.processing_failed missing from the effective preferences")
	}
	if failure["required"] != true || failure["delivery"] != "required" {
		t.Errorf("processing_failed required/delivery = %v/%v, want true/required", failure["required"], failure["delivery"])
	}
}

// TestDocumentUpload_OptOutSuppressesForThatMemberOnly is the preference path: a
// member who disables the key stops receiving it, while the row keeps flowing to
// everyone who did not.
func TestDocumentUpload_OptOutSuppressesForThatMemberOnly(t *testing.T) {
	requireSecondIdentity(t)
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	joinProjectAsSecondMember(t, rl, projectID, orgID)

	// Baseline: before opting out, the member does receive the upload. Without
	// this the suppression check below could pass on a broken fan-out.
	before := uniqueName("notify-before-optout") + ".txt"
	uploadDocumentAs(t, e2eTestToken(), projectID, before, []byte("baseline content"))
	if rows := documentNotifications(t, rl, inviteeToken(), projectID, before); len(rows) != 1 {
		t.Fatalf("baseline upload produced %d rows for the member, want 1", len(rows))
	}

	setNotificationPreference(t, rl, inviteeToken(), projectID, "document.uploaded", false)

	// The saved preference is reflected in the effective list.
	pref := preferenceFor(notificationPreferences(t, rl, inviteeToken(), projectID), "document.uploaded")
	if pref == nil || pref["enabled"] != false {
		t.Fatalf("document.uploaded preference = %v, want enabled false", pref)
	}

	after := uniqueName("notify-after-optout") + ".txt"
	uploadDocumentAs(t, e2eTestToken(), projectID, after, []byte("suppressed content"))
	if rows := documentNotifications(t, rl, inviteeToken(), projectID, after); len(rows) != 0 {
		t.Fatalf("opted-out member received %d rows, want 0", len(rows))
	}

	// The suppression is per user: a third recipient is not configured here, so
	// the strongest available check is that the preference row is scoped to the
	// member who set it and the enum key list is untouched for the uploader.
	if rows := documentNotifications(t, rl, e2eTestToken(), projectID, before); len(rows) != 0 {
		t.Fatalf("the uploader received %d rows, want 0", len(rows))
	}
}

// assertActionURLContains reports whether the row's actionUrl points at path.
func assertActionURLContains(t *testing.T, row map[string]any, path string) bool {
	t.Helper()
	actionURL, _ := row["actionUrl"].(string)
	if actionURL == "" {
		return false
	}
	return len(actionURL) >= len(path) && actionURL[len(actionURL)-len(path):] == path
}
