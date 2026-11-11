package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

const cardObjectID = "11111111-1111-1111-1111-111111111111"

// newObjectCardEcho registers the device object-card routes against a fake
// backend, mirroring newAgentObjectsEcho.
func newObjectCardEcho(f MemoryBackend) *echo.Echo {
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/api/objects/:id/card", s.getObjectCard)
	e.GET("/api/objects/:id/history", s.getObjectCardHistory)
	return e
}

func cardFixture() *fakeMemory {
	return &fakeMemory{
		objects: []GraphObject{{
			ID:          cardObjectID,
			CanonicalID: "22222222-2222-2222-2222-222222222222",
			Type:        "person",
			Key:         "sam",
			Status:      "active",
			Version:     3,
			Labels:      []string{"colleague"},
			Properties: map[string]any{
				"first_name": "Sam",
				"nickname":   "",
				"_internal":  "hidden",
			},
		}},
		compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{{
			Name:       "person",
			Label:      "Person",
			UI:         json.RawMessage(`{"icon":"user","color":"#3b82f6"}`),
			Properties: json.RawMessage(`{"first_name":{"type":"string","title":"First name","description":"Given name."}}`),
		}}},
		relns: []GraphRelationship{{ID: "r1", Type: "works_at", SrcID: cardObjectID, DstID: "x"}},
	}
}

func getCard(t *testing.T, e *echo.Echo, path string) (int, objectCard) {
	t.Helper()
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var card objectCard
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &card); err != nil {
			t.Fatalf("decode: %v (%s)", err, rec.Body.String())
		}
	}
	return rec.Code, card
}

func TestObjectCardMirrorsPreviewDrawer(t *testing.T) {
	e := newObjectCardEcho(cardFixture())
	code, card := getCard(t, e, "/api/objects/"+cardObjectID+"/card?rel=r1")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}

	if card.Title != "sam" || card.CopyID != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("title/copy = %q/%q", card.Title, card.CopyID)
	}
	if card.Type != (objectCardType{Name: "person", Label: "Person", Icon: "user", Color: "#3b82f6"}) {
		t.Errorf("type = %+v", card.Type)
	}
	// A type without a workflow keeps the status as the header badge.
	if card.Workflow != nil || card.Status == nil || card.Status.Label != "Active" || card.Status.Tone != "neutral" {
		t.Errorf("status = %+v workflow = %+v", card.Status, card.Workflow)
	}
	if card.ReferencedVia != "works at" {
		t.Errorf("referenced_via = %q", card.ReferencedVia)
	}
	if len(card.Labels) != 1 || card.Labels[0] != "colleague" {
		t.Errorf("labels = %v", card.Labels)
	}

	// Sections are the drawer's: platform fields, then "<Type> Properties"
	// with the schema title and description, then undeclared stored keys.
	if len(card.Sections) != 2 || card.Sections[0].Title != "Properties" || card.Sections[1].Title != "Person Properties" {
		t.Fatalf("sections = %+v", card.Sections)
	}
	var builtinLabels []string
	for _, f := range card.Sections[0].Fields {
		builtinLabels = append(builtinLabels, f.Label)
	}
	if want := []string{"Key", "Version", "Created", "ID"}; !equalStrings(builtinLabels, want) {
		t.Errorf("builtin labels = %v, want %v", builtinLabels, want)
	}
	typed := card.Sections[1].Fields
	if len(typed) != 2 {
		t.Fatalf("type fields = %+v", typed)
	}
	if typed[0] != (objectCardField{Label: "First name", Name: "first_name", Description: "Given name.", Value: "Sam"}) {
		t.Errorf("first field = %+v", typed[0])
	}
	if typed[1].Label != "Nickname" || !typed[1].Empty {
		t.Errorf("undeclared empty field = %+v", typed[1])
	}
	for _, f := range typed {
		if f.Name == "_internal" {
			t.Errorf("internal property leaked: %+v", f)
		}
	}
}

func TestObjectCardWorkflowTypeMovesStatus(t *testing.T) {
	f := cardFixture()
	f.objects[0].Status = "in_progress"
	f.objects[0].Assignee = "diane"
	f.compiled.ObjectTypes[0].BoardEnabled = true
	_, card := getCard(t, newObjectCardEcho(f), "/api/objects/"+cardObjectID+"/card")

	if card.Status != nil {
		t.Errorf("header status = %+v, want nil for a workflow type", card.Status)
	}
	if card.Workflow == nil || card.Workflow.Assignee != "diane" ||
		card.Workflow.Status == nil || card.Workflow.Status.Label != "In progress" || card.Workflow.Status.Tone != "info" {
		t.Errorf("workflow = %+v", card.Workflow)
	}
}

func TestObjectCardResolvesKeyRefs(t *testing.T) {
	f := cardFixture()
	f.lookupObjects = []GraphObject{f.objects[0]}
	code, card := getCard(t, newObjectCardEcho(f), "/api/objects/sam/card")
	if code != http.StatusOK || card.ID != cardObjectID {
		t.Fatalf("status = %d card = %+v", code, card)
	}
	if f.lastLookupKey != "sam" || f.lastLookupType != "" {
		t.Errorf("lookup = (%q, %q)", f.lastLookupType, f.lastLookupKey)
	}
}

func TestObjectCardDegradesWithoutSchema(t *testing.T) {
	f := cardFixture()
	f.blueprintErr = errors.New("schema down")
	code, card := getCard(t, newObjectCardEcho(f), "/api/objects/"+cardObjectID+"/card")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if card.Type != (objectCardType{Name: "person", Label: "person", Icon: "box"}) {
		t.Errorf("type = %+v", card.Type)
	}
	if len(card.Sections) != 2 || card.Sections[1].Title != "person Properties" {
		t.Errorf("sections = %+v", card.Sections)
	}
}

func TestObjectCardErrors(t *testing.T) {
	missing := cardFixture()
	missing.objects = nil
	if code, _ := getCard(t, newObjectCardEcho(missing), "/api/objects/"+cardObjectID+"/card"); code != http.StatusNotFound {
		t.Errorf("missing uuid: status = %d, want 404", code)
	}
	if code, _ := getCard(t, newObjectCardEcho(missing), "/api/objects/nobody/card"); code != http.StatusNotFound {
		t.Errorf("missing key: status = %d, want 404", code)
	}

	down := cardFixture()
	down.docErr = errors.New("connection refused")
	if code, _ := getCard(t, newObjectCardEcho(down), "/api/objects/"+cardObjectID+"/card"); code != http.StatusBadGateway {
		t.Errorf("backend down: status = %d, want 502", code)
	}
}

func TestObjectCardHistory(t *testing.T) {
	f := cardFixture()
	f.objectHistory = []GraphObject{
		{ID: cardObjectID, Version: 2, Status: "done", Key: "sam", CreatedAt: "2026-09-02T10:00:00Z",
			ChangeSummary: map[string]any{"updated": map[string]any{"first_name": map[string]any{"from": "S", "to": "Sam"}}}},
		{ID: cardObjectID, Version: 1, Key: "sam"},
	}
	rec := httptest.NewRecorder()
	newObjectCardEcho(f).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/objects/"+cardObjectID+"/history", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Versions []objectCardVersion `json:"versions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Versions) != 2 {
		t.Fatalf("versions = %+v", got.Versions)
	}
	v2 := got.Versions[0]
	if v2.Label != "v2" || v2.Status == nil || v2.Status.Tone != "success" {
		t.Errorf("v2 = %+v", v2)
	}
	if len(v2.Changes) != 1 || v2.Changes[0] != (objectCardChange{Kind: "update", Label: "first_name", From: "S", To: "Sam"}) {
		t.Errorf("v2 changes = %+v", v2.Changes)
	}
	// The oldest version has nothing to diff against: the drawer's note.
	if c := got.Versions[1].Changes; len(c) != 1 || c[0].Kind != "note" {
		t.Errorf("v1 changes = %+v", c)
	}

	f.objectHistoryErr = errors.New("boom")
	rec = httptest.NewRecorder()
	newObjectCardEcho(f).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/objects/"+cardObjectID+"/history", nil))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("history down: status = %d, want 502", rec.Code)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestObjectCardWorkflowCarriesPickerOptions pins the contract the iOS pickers
// read (issue #454): allowed_statuses in the type's declared board order, plus
// the assignee candidates for a work item (agents, then members). A keyless
// workflow object keeps the status options but offers no assignee candidates:
// the write routes require a key, so the strip stays read-only there.
func TestObjectCardWorkflowCarriesPickerOptions(t *testing.T) {
	t.Run("work item", func(t *testing.T) {
		f := cardFixture()
		f.objects[0].Status = "review"
		f.compiled.ObjectTypes[0].AllowedStatuses = []string{"ready", "review", "done"}
		f.scheduledAgents = []ScheduledAgent{{Name: "agent-a"}}
		f.members = []ProjectMemberDto{{DisplayName: "Alice"}}
		_, card := getCard(t, newObjectCardEcho(f), "/api/objects/"+cardObjectID+"/card")

		if card.Workflow == nil {
			t.Fatal("workflow = nil, want the work strip")
		}
		want := []string{"ready", "review", "done"}
		if len(card.Workflow.AllowedStatuses) != len(want) {
			t.Fatalf("allowed_statuses = %+v, want %v", card.Workflow.AllowedStatuses, want)
		}
		for i, v := range want {
			if card.Workflow.AllowedStatuses[i].Value != v {
				t.Errorf("allowed_statuses[%d] = %q, want %q", i, card.Workflow.AllowedStatuses[i].Value, v)
			}
		}
		if got := card.Workflow.AllowedStatuses[1]; got.Label != "Review" || got.Tone != "warning" {
			t.Errorf("review option = %+v, want the board label and tone", got)
		}
		if len(card.Workflow.Assignees) != 2 ||
			card.Workflow.Assignees[0].Value != "agent-a" ||
			card.Workflow.Assignees[1].Value != "Alice" {
			t.Errorf("assignees = %+v, want agent-a then Alice", card.Workflow.Assignees)
		}
	})

	t.Run("keyless workflow object", func(t *testing.T) {
		f := cardFixture()
		f.objects[0].Key = ""
		f.compiled.ObjectTypes[0].AllowedStatuses = []string{"ready", "done"}
		f.scheduledAgents = []ScheduledAgent{{Name: "agent-a"}}
		_, card := getCard(t, newObjectCardEcho(f), "/api/objects/"+cardObjectID+"/card")

		if card.Workflow == nil {
			t.Fatal("workflow = nil, want the work strip")
		}
		if len(card.Workflow.AllowedStatuses) != 2 {
			t.Errorf("allowed_statuses = %+v, want the type's two statuses", card.Workflow.AllowedStatuses)
		}
		if len(card.Workflow.Assignees) != 0 {
			t.Errorf("assignees = %+v, want none for a keyless object", card.Workflow.Assignees)
		}
	})
}
