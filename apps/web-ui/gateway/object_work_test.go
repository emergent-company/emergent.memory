package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// testObjectUUID is an object-id-shaped reference: the card resolves a UUID
// straight by id, while a non-UUID ref goes through a key lookup.
const testObjectUUID = "11111111-1111-1111-1111-111111111111"

// objectWorkEcho wires the two object-card write routes behind the real
// session-or-device gate, so the device boundary is exercised end to end.
func objectWorkEcho(f *fakeMemory) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}, memory: f}
	e := echo.New()
	e.Use(s.requireSessionOrKey)
	e.POST("/api/objects/:id/status", s.postObjectStatus)
	e.POST("/api/objects/:id/assignee", s.postObjectAssignee)
	return s, e
}

// objectWorkEchoOpen wires the same handlers without auth, for the behaviour
// that is shared with non-device callers.
func objectWorkEchoOpen(f *fakeMemory) *echo.Echo {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/api/objects/:id/status", s.postObjectStatus)
	e.POST("/api/objects/:id/assignee", s.postObjectAssignee)
	return e
}

func objectWorkPost(t *testing.T, e *echo.Echo, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func objectWorkCompiledTypes() *CompiledSchemaTypes {
	return &CompiledSchemaTypes{ObjectTypes: []CompiledType{{
		Name:            "Task",
		Label:           "Task",
		BoardEnabled:    true,
		AllowedStatuses: []string{"ready", "review", "done"},
	}}}
}

// TestObjectWorkWritesHandler pins the handler behaviour both surfaces share:
// a keyed object is written through the work-item routes, a keyless object
// falls back to the generic graph update, and both answer with the refreshed
// card (including the picker options).
func TestObjectWorkWritesHandler(t *testing.T) {
	t.Run("work item status returns the refreshed card", func(t *testing.T) {
		f := &fakeMemory{
			compiled:        objectWorkCompiledTypes(),
			objects:         []GraphObject{{ID: testObjectUUID, CanonicalID: "c1", Type: "Task", Key: "t1", Status: "done"}},
			scheduledAgents: []ScheduledAgent{{Name: "agent-a"}},
			members:         []ProjectMemberDto{{DisplayName: "Alice"}},
		}
		e := objectWorkEchoOpen(f)

		rec := objectWorkPost(t, e, "/api/objects/"+testObjectUUID+"/status", `{"status":"done"}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if f.lastWorkItemID != "c1" || f.lastStatusTo != "done" {
			t.Errorf("SetWorkItemStatus got id=%q status=%q, want c1/done", f.lastWorkItemID, f.lastStatusTo)
		}

		var card objectCard
		if err := json.Unmarshal(rec.Body.Bytes(), &card); err != nil {
			t.Fatalf("decode card: %v", err)
		}
		if card.Workflow == nil || card.Workflow.Status == nil || card.Workflow.Status.Value != "done" {
			t.Fatalf("card workflow = %+v, want the refreshed done status", card.Workflow)
		}
		want := []string{"ready", "review", "done"}
		if len(card.Workflow.AllowedStatuses) != len(want) {
			t.Fatalf("allowed_statuses = %+v, want %v", card.Workflow.AllowedStatuses, want)
		}
		for i, v := range want {
			if card.Workflow.AllowedStatuses[i].Value != v {
				t.Errorf("allowed_statuses[%d] = %q, want %q (board order)", i, card.Workflow.AllowedStatuses[i].Value, v)
			}
		}
		if got := card.Workflow.AllowedStatuses[1].Label; got == "" {
			t.Errorf("allowed_statuses[1].label is empty; the picker needs a display label")
		}
		if got := card.Workflow.AllowedStatuses[1].Tone; got == "" {
			t.Errorf("allowed_statuses[1].tone is empty; the picker needs a tone")
		}
		if len(card.Workflow.Assignees) != 2 {
			t.Fatalf("assignees = %+v, want the agent and the member", card.Workflow.Assignees)
		}
		if card.Workflow.Assignees[0].Value != "agent-a" || card.Workflow.Assignees[1].Value != "Alice" {
			t.Errorf("assignees = %+v, want agent-a then Alice", card.Workflow.Assignees)
		}
	})

	t.Run("empty assignee clears through the work-item route", func(t *testing.T) {
		f := &fakeMemory{
			compiled: objectWorkCompiledTypes(),
			objects:  []GraphObject{{ID: testObjectUUID, CanonicalID: "c1", Type: "Task", Key: "t1", Status: "ready"}},
		}
		e := objectWorkEchoOpen(f)

		rec := objectWorkPost(t, e, "/api/objects/"+testObjectUUID+"/assignee", `{"assignee":""}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if f.lastWorkItemID != "c1" || f.lastReassignTo != "" {
			t.Errorf("ReassignWorkItem got id=%q assignee=%q, want c1/empty", f.lastWorkItemID, f.lastReassignTo)
		}
	})

	t.Run("object without a key falls back to the generic update", func(t *testing.T) {
		f := &fakeMemory{
			compiled: objectWorkCompiledTypes(),
			objects:  []GraphObject{{ID: testObjectUUID, Type: "Note"}},
		}
		e := objectWorkEchoOpen(f)

		rec := objectWorkPost(t, e, "/api/objects/"+testObjectUUID+"/status", `{"status":"done"}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if f.lastStatusTo != "" {
			t.Errorf("SetWorkItemStatus called for a keyless object: %q", f.lastStatusTo)
		}
		if f.lastUpdateReq == nil || f.lastUpdateReq.Status == nil || *f.lastUpdateReq.Status != "done" {
			t.Errorf("UpdateObject status = %+v, want done", f.lastUpdateReq)
		}

		rec = objectWorkPost(t, e, "/api/objects/"+testObjectUUID+"/assignee", `{"assignee":"Alice"}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if f.lastReassignTo != "" {
			t.Errorf("ReassignWorkItem called for a keyless object: %q", f.lastReassignTo)
		}
		if f.lastUpdateReq == nil || f.lastUpdateReq.Assignee == nil || *f.lastUpdateReq.Assignee != "Alice" {
			t.Errorf("UpdateObject assignee = %+v, want Alice", f.lastUpdateReq)
		}
	})

	t.Run("empty status is refused before any write", func(t *testing.T) {
		f := &fakeMemory{compiled: objectWorkCompiledTypes(), objects: []GraphObject{{ID: testObjectUUID, CanonicalID: "c1", Type: "Task", Key: "t1"}}}
		e := objectWorkEchoOpen(f)

		rec := objectWorkPost(t, e, "/api/objects/"+testObjectUUID+"/status", `{"status":"  "}`, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
		}
		if f.lastStatusTo != "" {
			t.Errorf("a write happened for an empty status: %q", f.lastStatusTo)
		}
	})

	t.Run("unknown object is 404", func(t *testing.T) {
		e := objectWorkEchoOpen(&fakeMemory{compiled: objectWorkCompiledTypes()})
		rec := objectWorkPost(t, e, "/api/objects/nope/status", `{"status":"done"}`, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
		}
	})
}

// TestObjectWorkWritesDeviceCredential is the positive half of the device
// boundary: a device credential may POST both routes and receives the refreshed
// card. A memory refusal (a disallowed status, or a project the credential is
// not bound to) reaches the client as its own status, never as a 502.
func TestObjectWorkWritesDeviceCredential(t *testing.T) {
	newDeviceMemory := func() *fakeMemory {
		return &fakeMemory{
			introspectInfo:  testDeviceTokenInfo(),
			compiled:        objectWorkCompiledTypes(),
			objects:         []GraphObject{{ID: testObjectUUID, CanonicalID: "c1", Type: "Task", Key: "t1", Status: "review"}},
			scheduledAgents: []ScheduledAgent{{Name: "agent-a"}},
		}
	}
	deviceHeaders := map[string]string{"X-API-Key": "emt_devicecredential"}

	t.Run("status", func(t *testing.T) {
		f := newDeviceMemory()
		_, e := objectWorkEcho(f)
		rec := objectWorkPost(t, e, "/api/objects/"+testObjectUUID+"/status", `{"status":"review"}`, deviceHeaders)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if f.lastWorkItemID != "c1" || f.lastStatusTo != "review" {
			t.Errorf("SetWorkItemStatus got id=%q status=%q, want c1/review", f.lastWorkItemID, f.lastStatusTo)
		}
		var card objectCard
		if err := json.Unmarshal(rec.Body.Bytes(), &card); err != nil {
			t.Fatalf("decode card: %v", err)
		}
		if card.Workflow == nil || card.Workflow.Status == nil || card.Workflow.Status.Value != "review" {
			t.Fatalf("card workflow = %+v, want the refreshed review status", card.Workflow)
		}
	})

	t.Run("assignee", func(t *testing.T) {
		f := newDeviceMemory()
		_, e := objectWorkEcho(f)
		rec := objectWorkPost(t, e, "/api/objects/"+testObjectUUID+"/assignee", `{"assignee":"agent-a"}`, deviceHeaders)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if f.lastReassignTo != "agent-a" {
			t.Errorf("ReassignWorkItem got assignee=%q, want agent-a", f.lastReassignTo)
		}
	})

	t.Run("memory refusal is passed through, not masked as 502", func(t *testing.T) {
		f := newDeviceMemory()
		f.workActionErr = &memoryHTTPError{
			Status:  http.StatusForbidden,
			Code:    "forbidden",
			Message: "API token is scoped to a different project",
		}
		_, e := objectWorkEcho(f)
		rec := objectWorkPost(t, e, "/api/objects/"+testObjectUUID+"/status", `{"status":"done"}`, deviceHeaders)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "different project") {
			t.Errorf("body = %s, want the backend's reason preserved", rec.Body.String())
		}
	})

	t.Run("disallowed status is refused by the handler", func(t *testing.T) {
		f := newDeviceMemory()
		f.workActionErr = &memoryHTTPError{
			Status:  http.StatusBadRequest,
			Code:    "work_status_not_allowed",
			Message: "status is not in the type's allowed work-status set: shipped",
		}
		_, e := objectWorkEcho(f)
		rec := objectWorkPost(t, e, "/api/objects/"+testObjectUUID+"/status", `{"status":"shipped"}`, deviceHeaders)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("keyless object write is refused upstream", func(t *testing.T) {
		// The generic graph update is not on the device surface, so memory
		// refuses it. The gateway still routes the request the way the drawer
		// does and returns memory's verdict — the card never offers the pickers
		// for a keyless object, so this path is defence in depth.
		f := newDeviceMemory()
		f.objects = []GraphObject{{ID: testObjectUUID, Type: "Note"}}
		f.updateObjectErr = &memoryHTTPError{
			Status:  http.StatusForbidden,
			Code:    "forbidden",
			Message: "device:api credentials are only valid on the device surface",
		}
		_, e := objectWorkEcho(f)
		rec := objectWorkPost(t, e, "/api/objects/"+testObjectUUID+"/status", `{"status":"done"}`, deviceHeaders)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
		}
		if f.lastStatusTo != "" {
			t.Errorf("SetWorkItemStatus called for a keyless object: %q", f.lastStatusTo)
		}
	})
}

// TestObjectWorkWritesNonDeviceUnaffected proves the new routes are not
// device-only: a browser/session caller reaches the same handlers (dev mode
// here, where /api has no session requirement).
func TestObjectWorkWritesNonDeviceUnaffected(t *testing.T) {
	f := &fakeMemory{
		compiled: objectWorkCompiledTypes(),
		objects:  []GraphObject{{ID: testObjectUUID, CanonicalID: "c1", Type: "Task", Key: "t1", Status: "done"}},
	}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.Use(s.authDispatch)
	e.POST("/api/objects/:id/status", s.postObjectStatus)

	rec := objectWorkPost(t, e, "/api/objects/"+testObjectUUID+"/status", `{"status":"done"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if f.lastStatusTo != "done" {
		t.Errorf("SetWorkItemStatus got %q, want done", f.lastStatusTo)
	}
}
