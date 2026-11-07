// Package api_test — graph_changeset_provenance_test.go
//
// Over-HTTP proof of the source write path (slice 3 of #2376). The domain tests
// assert the apply and the contribution table directly; this asserts what a
// client actually receives on the wire: a changeset pushed with a source's own
// external ids lands as source-owned data, the object read reports the source
// backing that version, a revision moves the contribution onto the new version
// while the superseded one keeps its rows, and a withdrawal is reported as a
// state rather than a delete.
//
// It also covers task 13.2's list/count/total agreement: the rows the push
// created appear in the object list, the list's own `total`, and the count
// endpoint, with the same number in all three.
package api_test

import (
	"fmt"
	"net/http"
	"testing"
)

const (
	changesetE2EObjectType = "ChangesetE2EObject"
	// Relationship types are lower_snake_case: every ordinary write path
	// normalises them (normalizeRelationType) and the search filter lowercases
	// unconditionally. The changeset apply does not normalise yet, so this
	// fixture is written already-normalised — pushing a mixed-case type and
	// filtering for it is the bug filed as #140, not something this test should
	// paper over by asserting the unnormalised row.
	changesetE2ERelType = "changeset_e2e_links"
)

// changesetPushResult mirrors the subset of POST /api/graph/changesets this
// test reads. Keeping it a struct (rather than map[string]any) means a renamed
// or dropped field fails the test instead of silently asserting nothing.
type changesetPushResult struct {
	Generation int64                  `json:"generation"`
	Results    []changesetItemOutcome `json:"results"`
}

type changesetItemOutcome struct {
	Kind        string `json:"kind"`
	ExternalID  string `json:"external_id"`
	Outcome     string `json:"outcome"`
	SubjectID   string `json:"subject_id"`
	CanonicalID string `json:"canonical_id"`
	Version     int    `json:"version"`
}

// contributionJSON mirrors the contribution entry a response carries.
type contributionJSON struct {
	SubjectKind         string  `json:"subject_kind"`
	SubjectVersionID    string  `json:"subject_version_id"`
	SourceID            string  `json:"source_id"`
	ExternalID          string  `json:"external_id"`
	ExternalRevision    string  `json:"external_revision"`
	ExternalRevisionSeq float64 `json:"external_revision_seq"`
	State               string  `json:"state"`
	WithdrawnAt         *string `json:"withdrawn_at"`
}

// objectJSON is the subset of an object response the assertions read.
type objectJSON struct {
	ID            string             `json:"id"`
	CanonicalID   string             `json:"canonical_id"`
	Version       int                `json:"version"`
	DataClass     string             `json:"data_class"`
	Properties    map[string]any     `json:"properties"`
	OwnerSourceID string             `json:"owner_source_id"`
	Contributions []contributionJSON `json:"contributions"`
}

func outcomeFor(t *testing.T, resp *changesetPushResult, externalID string) changesetItemOutcome {
	t.Helper()
	for _, r := range resp.Results {
		if r.ExternalID == externalID {
			return r
		}
	}
	t.Fatalf("no result for external_id %q in %+v", externalID, resp.Results)
	return changesetItemOutcome{}
}

// pushChangeset posts an apply and requires the documented 201.
func pushChangeset(t *testing.T, rl *runLog, projectID string, body map[string]any) *changesetPushResult {
	t.Helper()
	return pushChangesetAs(t, rl, projectID, e2eTestToken(), body)
}

// pushChangesetAs posts an apply with an explicit bearer token. The provenance
// e2e pushes with the same project token that created the source, so the push
// succeeds through the exact source grant the server recorded at creation rather
// than through a backfill wildcard.
func pushChangesetAs(t *testing.T, rl *runLog, projectID, token string, body map[string]any) *changesetPushResult {
	t.Helper()
	resp := doAPILogged(t, rl, "POST", "/api/graph/changesets", token, projectID, jsonBody(body))
	raw := mustStatus(t, resp, http.StatusCreated)
	var out changesetPushResult
	parseBodyJSON(t, raw, &out)
	return &out
}

// getObject reads an object and requires the documented 200.
func getObject(t *testing.T, rl *runLog, projectID, id string) objectJSON {
	t.Helper()
	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/"+id, e2eTestToken(), projectID, nil)
	raw := mustStatus(t, resp, http.StatusOK)
	var out objectJSON
	parseBodyJSON(t, raw, &out)
	return out
}

// TestGraphChangesetProvenance_HTTP is the end-to-end proof of 13.2.
func TestGraphChangesetProvenance_HTTP(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// The source is created and pushed with the SAME project token. A new source
	// carries no backfill wildcard, so the server records an exact grant for the
	// creating credential; the push below then succeeds through that grant rather
	// than through a wildcard. This states the slice-2 contract on the wire: a
	// session-created source cannot be pushed over HTTP at all.
	tokenID, rawToken := createToken(t, projectID, "e2e-changeset-source-token", []string{"documents:read", "documents:write"})
	if tokenID == "" || rawToken == "" {
		t.Fatalf("createToken returned an incomplete token: id=%q token=%q", tokenID, rawToken)
	}
	rl.Printf("project token credential id: %s", tokenID)

	// ── 1. A source to push as ────────────────────────────────────────────────
	// A changeset names a kb.sources row. The web provider is config-only: it
	// needs no OAuth consent and does no network work on create, so it is the
	// one a hermetic test can register. createWebSourceAs (datasources_web_test.go)
	// pins syncMode=manual so the admit sweep cannot run a sync mid-test.
	rl.Section("Register the pushing source")
	sourceID := createWebSourceAs(t, rl, projectID, rawToken, map[string]any{"start_urls": []string{"https://example.com"}})
	// Cleanup runs after the test returns, i.e. after rl.Close(), so it must not
	// write to the runlog (doAPI, not doAPILogged).
	t.Cleanup(func() {
		resp := doAPI(t, "DELETE", "/api/data-source-integrations/"+sourceID, e2eTestToken(), projectID, nil)
		if resp == nil {
			return
		}
		if resp.Body != nil {
			resp.Body.Close()
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			t.Logf("source cleanup returned %d", resp.StatusCode)
		}
	})
	rl.Printf("source id: %s", sourceID)

	// ── 2. First push: two objects and the relationship between them ─────────
	rl.Section("Push a changeset: two objects and a relationship")
	pushed := pushChangesetAs(t, rl, projectID, rawToken, map[string]any{
		"source_id": sourceID,
		"objects": []map[string]any{
			{
				"_ref":                  "ada",
				"type":                  changesetE2EObjectType,
				"properties":            map[string]any{"name": "Ada"},
				"external_id":           "e2e-ada",
				"external_revision":     "rev-1",
				"external_revision_seq": 1,
			},
			{
				"_ref":                  "bob",
				"type":                  changesetE2EObjectType,
				"properties":            map[string]any{"name": "Bob"},
				"external_id":           "e2e-bob",
				"external_revision":     "rev-1",
				"external_revision_seq": 1,
			},
		},
		"relationships": []map[string]any{{
			"type":                  changesetE2ERelType,
			"src_ref":               "ada",
			"dst_ref":               "bob",
			"external_id":           "e2e-knows",
			"external_revision_seq": 1,
		}},
	})
	if len(pushed.Results) != 3 {
		t.Fatalf("expected one result per item (3), got %d: %+v", len(pushed.Results), pushed.Results)
	}
	for _, externalID := range []string{"e2e-ada", "e2e-bob", "e2e-knows"} {
		if got := outcomeFor(t, pushed, externalID).Outcome; got != "created" {
			t.Fatalf("expected outcome=created for %s, got %q", externalID, got)
		}
	}
	ada := outcomeFor(t, pushed, "e2e-ada")
	bob := outcomeFor(t, pushed, "e2e-bob")
	knows := outcomeFor(t, pushed, "e2e-knows")
	// Cleanup runs after rl.Close(), so it uses the unlogged helpers. Delete by
	// canonical id: the id the push reported is the version that later becomes
	// superseded, while the canonical id resolves to the head of the chain.
	// Relationships are deleted through the object delete cascade only when their
	// endpoints go, so remove the relationship explicitly first.
	t.Cleanup(func() {
		deleteRelationship(t, projectID, knows.SubjectID)
		deleteGraphObject(t, projectID, ada.CanonicalID)
		deleteGraphObject(t, projectID, bob.CanonicalID)
	})

	// ── 3. The object read reports the source that owns it ───────────────────
	rl.Section("Read the pushed object back")
	read := getObject(t, rl, projectID, ada.SubjectID)
	if read.DataClass != "source" {
		t.Errorf("expected data_class=source on a source-pushed object, got %q", read.DataClass)
	}
	if read.OwnerSourceID != sourceID {
		t.Errorf("expected owner_source_id=%s, got %q", sourceID, read.OwnerSourceID)
	}
	if len(read.Contributions) != 1 {
		t.Fatalf("expected exactly 1 contribution on the pushed version, got %d: %+v", len(read.Contributions), read.Contributions)
	}
	contrib := read.Contributions[0]
	if contrib.SourceID != sourceID {
		t.Errorf("expected contribution source_id=%s, got %q", sourceID, contrib.SourceID)
	}
	if contrib.ExternalID != "e2e-ada" {
		t.Errorf("expected contribution external_id=e2e-ada, got %q", contrib.ExternalID)
	}
	if contrib.ExternalRevision != "rev-1" {
		t.Errorf("expected contribution external_revision=rev-1, got %q", contrib.ExternalRevision)
	}
	if contrib.ExternalRevisionSeq != 1 {
		t.Errorf("expected contribution external_revision_seq=1, got %v", contrib.ExternalRevisionSeq)
	}
	if contrib.State != "present" {
		t.Errorf("expected contribution state=present, got %q", contrib.State)
	}
	if contrib.SubjectKind != "object" {
		t.Errorf("expected contribution subject_kind=object, got %q", contrib.SubjectKind)
	}
	if contrib.SubjectVersionID != read.ID {
		t.Errorf("expected the contribution to describe the version read (%s), got %s", read.ID, contrib.SubjectVersionID)
	}

	// ── 4. A revision moves the contribution onto the new version ────────────
	rl.Section("Push a revision of the object")
	revised := pushChangesetAs(t, rl, projectID, rawToken, map[string]any{
		"source_id": sourceID,
		"objects": []map[string]any{{
			"type":                  changesetE2EObjectType,
			"properties":            map[string]any{"name": "Ada Lovelace"},
			"external_id":           "e2e-ada",
			"external_revision":     "rev-2",
			"external_revision_seq": 2,
		}},
	})
	updated := outcomeFor(t, revised, "e2e-ada")
	if updated.Outcome != "updated" {
		t.Fatalf("expected a new revision to update the object, got %q", updated.Outcome)
	}
	if updated.SubjectID == ada.SubjectID {
		t.Fatalf("a revision must mint a new version row; both pushes named %s", ada.SubjectID)
	}
	if updated.Version != 2 {
		t.Errorf("expected the revised object at version 2, got %d", updated.Version)
	}

	head := getObject(t, rl, projectID, updated.SubjectID)
	if len(head.Contributions) != 1 {
		t.Fatalf("expected 1 contribution on the revised version, got %d", len(head.Contributions))
	}
	if head.Contributions[0].ExternalRevisionSeq != 2 {
		t.Errorf("expected the revised contribution at revision 2, got %v", head.Contributions[0].ExternalRevisionSeq)
	}
	if head.Contributions[0].State != "present" {
		t.Errorf("expected the revised contribution present, got %q", head.Contributions[0].State)
	}

	rl.Section("The superseded version keeps its own contribution as history")
	histResp := doAPILogged(t, rl, "GET", "/api/graph/objects/"+updated.SubjectID+"/history", e2eTestToken(), projectID, nil)
	histRaw := mustStatus(t, histResp, http.StatusOK)
	var history struct {
		Versions []objectJSON `json:"versions"`
	}
	parseBodyJSON(t, histRaw, &history)
	if len(history.Versions) != 2 {
		t.Fatalf("expected 2 versions in the history, got %d: %s", len(history.Versions), histRaw)
	}
	presentOn := ""
	for _, version := range history.Versions {
		if len(version.Contributions) != 1 {
			t.Fatalf("version %d lost its contribution: %+v", version.Version, version)
		}
		row := version.Contributions[0]
		if row.SubjectVersionID != version.ID {
			t.Errorf("version %s reported a contribution of version %s", version.ID, row.SubjectVersionID)
		}
		switch row.State {
		case "present":
			if presentOn != "" {
				t.Errorf("two versions report a present contribution (%s and %s)", presentOn, version.ID)
			}
			presentOn = version.ID
		case "superseded":
			// expected for the version the revision replaced
		default:
			t.Errorf("unexpected contribution state %q on version %s", row.State, version.ID)
		}
	}
	if presentOn != updated.SubjectID {
		t.Errorf("expected the present contribution on the HEAD version %s, got %q", updated.SubjectID, presentOn)
	}

	// ── 5. A stale re-push applies nothing ───────────────────────────────────
	rl.Section("Re-push a stale revision")
	stale := pushChangesetAs(t, rl, projectID, rawToken, map[string]any{
		"source_id": sourceID,
		"objects": []map[string]any{{
			"type":                  changesetE2EObjectType,
			"properties":            map[string]any{"name": "Ada (stale)"},
			"external_id":           "e2e-ada",
			"external_revision_seq": 1,
		}},
	})
	if got := outcomeFor(t, stale, "e2e-ada").Outcome; got != "skipped" {
		t.Fatalf("expected a stale revision to be skipped, got %q", got)
	}
	stillHead := getObject(t, rl, projectID, updated.SubjectID)
	if stillHead.Properties["name"] != "Ada Lovelace" {
		t.Errorf("a skipped revision must not write; properties are %v", stillHead.Properties)
	}

	// ── 6. Withdrawal is a contribution state, not a delete ──────────────────
	rl.Section("Withdraw one source record")
	withdrawn := pushChangesetAs(t, rl, projectID, rawToken, map[string]any{
		"source_id":   sourceID,
		"withdrawals": []map[string]any{{"external_id": "e2e-bob"}},
	})
	if got := outcomeFor(t, withdrawn, "e2e-bob").Outcome; got != "withdrawn" {
		t.Fatalf("expected outcome=withdrawn for e2e-bob, got %q", got)
	}

	bobRead := getObject(t, rl, projectID, bob.SubjectID)
	if len(bobRead.Contributions) != 1 {
		t.Fatalf("withdrawal must keep the row: expected 1 contribution, got %d", len(bobRead.Contributions))
	}
	if got := bobRead.Contributions[0].State; got != "withdrawn" {
		t.Errorf("expected the contribution state=withdrawn, got %q", got)
	}
	if bobRead.Contributions[0].WithdrawnAt == nil {
		t.Errorf("a withdrawn contribution must record when: %+v", bobRead.Contributions[0])
	}

	// ── 7. The relationship read reports its own kind ────────────────────────
	rl.Section("Read the pushed relationship back")
	relResp := doAPILogged(t, rl, "GET", "/api/graph/relationships/"+knows.SubjectID, e2eTestToken(), projectID, nil)
	relRaw := mustStatus(t, relResp, http.StatusOK)
	var rel struct {
		ID            string             `json:"id"`
		Contributions []contributionJSON `json:"contributions"`
	}
	parseBodyJSON(t, relRaw, &rel)
	if len(rel.Contributions) != 1 {
		t.Fatalf("expected 1 contribution on the pushed relationship, got %d: %s", len(rel.Contributions), relRaw)
	}
	if rel.Contributions[0].SubjectKind != "relationship" {
		t.Errorf("expected subject_kind=relationship, got %q", rel.Contributions[0].SubjectKind)
	}
	if rel.Contributions[0].ExternalID != "e2e-knows" {
		t.Errorf("expected the relationship's external id, got %q", rel.Contributions[0].ExternalID)
	}
	if rel.Contributions[0].SubjectVersionID != rel.ID {
		t.Errorf("expected the contribution to describe the relationship version read (%s), got %s", rel.ID, rel.Contributions[0].SubjectVersionID)
	}

	relListResp := doAPILogged(t, rl, "GET", "/api/graph/relationships/search?type="+changesetE2ERelType, e2eTestToken(), projectID, nil)
	relListRaw := mustStatus(t, relListResp, http.StatusOK)
	var relList struct {
		Items []struct {
			ID            string             `json:"id"`
			Contributions []contributionJSON `json:"contributions"`
		} `json:"items"`
		Total int `json:"total"`
	}
	parseBodyJSON(t, relListRaw, &relList)
	if len(relList.Items) != 1 || relList.Total != 1 {
		t.Fatalf("expected the relationship list and its total to agree at 1, got items=%d total=%d", len(relList.Items), relList.Total)
	}
	if len(relList.Items[0].Contributions) != 1 {
		t.Errorf("the listed relationship lost its contributor: %s", relListRaw)
	}

	// ── 8. The pushed rows appear in list, total and count alike ─────────────
	rl.Section("List, total and count agree on the pushed rows")
	listResp := doAPILogged(t, rl, "GET",
		"/api/graph/objects/search?type="+changesetE2EObjectType, e2eTestToken(), projectID, nil)
	listRaw := mustStatus(t, listResp, http.StatusOK)
	var listed struct {
		Items []struct {
			ID            string             `json:"id"`
			DataClass     string             `json:"data_class"`
			Contributions []contributionJSON `json:"contributions"`
		} `json:"items"`
		Total int `json:"total"`
	}
	parseBodyJSON(t, listRaw, &listed)
	if len(listed.Items) != 2 || listed.Total != 2 {
		t.Fatalf("expected the two pushed objects in list and total, got items=%d total=%d: %s",
			len(listed.Items), listed.Total, listRaw)
	}
	for _, item := range listed.Items {
		if item.DataClass != "source" {
			t.Errorf("expected every listed row to disclose data_class=source, got %q for %s", item.DataClass, item.ID)
		}
		// #135: a listed row reports its sources without a per-row detail call.
		if len(item.Contributions) != 1 {
			t.Errorf("the listed object %s lost its contributor: %s", item.ID, listRaw)
			continue
		}
		if item.Contributions[0].SubjectKind != "object" {
			t.Errorf("expected subject_kind=object on the listed object %s, got %q", item.ID, item.Contributions[0].SubjectKind)
		}
		if item.Contributions[0].SubjectVersionID != item.ID {
			t.Errorf("expected the listed object's contribution to describe its version (%s), got %s",
				item.ID, item.Contributions[0].SubjectVersionID)
		}
	}

	countResp := doAPILogged(t, rl, "GET",
		fmt.Sprintf("/api/graph/objects/count?type=%s", changesetE2EObjectType), e2eTestToken(), projectID, nil)
	countRaw := mustStatus(t, countResp, http.StatusOK)
	var counted struct {
		Count int `json:"count"`
	}
	parseBodyJSON(t, countRaw, &counted)
	if counted.Count != listed.Total {
		t.Errorf("list total (%d) and count (%d) disagree for the pushed rows", listed.Total, counted.Count)
	}
}
