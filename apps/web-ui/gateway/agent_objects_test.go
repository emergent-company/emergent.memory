package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// newAgentObjectsEcho registers the device object-browser route against a fake
// backend, mirroring newClientEcho's minimal router.
func newAgentObjectsEcho(f MemoryBackend) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/api/agents/:id/objects", s.listAgentObjects)
	return s, e
}

// agentObjectsFixture has one agent, two objects, and a second-page cursor.
func agentObjectsFixture() *fakeMemory {
	return &fakeMemory{
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		pageObjects: []GraphObject{
			{ID: "o1", Type: "person", Key: "sam"},
			{ID: "o2", Type: "note", Key: "n1"},
		},
		nextPageCursor: "cur2",
	}
}

func TestListAgentObjectsDefaults(t *testing.T) {
	f := agentObjectsFixture()
	_, e := newAgentObjectsEcho(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents/a1/objects", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Items      []GraphObject `json:"items"`
		NextCursor string        `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 || got.Items[0].ID != "o1" || got.Items[1].ID != "o2" {
		t.Fatalf("items = %+v, want [o1 o2]", got.Items)
	}
	if got.NextCursor != "cur2" {
		t.Errorf("next_cursor = %q, want cur2", got.NextCursor)
	}
	// The filter is pinned to the (agent, :id) pair, provenance defaults to
	// any, and the page size defaults to 25.
	if f.lastPageActorType != "agent" || f.lastPageActorID != "a1" {
		t.Errorf("actor pair = (%q, %q), want (agent, a1)", f.lastPageActorType, f.lastPageActorID)
	}
	if f.lastPageProvenance != "any" {
		t.Errorf("provenance = %q, want any", f.lastPageProvenance)
	}
	if f.lastPageLimit != agentObjectsDefaultLimit {
		t.Errorf("limit = %d, want %d", f.lastPageLimit, agentObjectsDefaultLimit)
	}
}

func TestListAgentObjectsForwardsParams(t *testing.T) {
	f := agentObjectsFixture()
	_, e := newAgentObjectsEcho(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents/a1/objects?provenance=created&cursor=cur1&limit=10&branch=b1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.lastPageProvenance != "created" {
		t.Errorf("provenance = %q, want created", f.lastPageProvenance)
	}
	if f.lastPageCursor != "cur1" {
		t.Errorf("cursor = %q, want cur1", f.lastPageCursor)
	}
	if f.lastPageLimit != 10 {
		t.Errorf("limit = %d, want 10", f.lastPageLimit)
	}
	if f.lastPageBranch != "b1" {
		t.Errorf("branch = %q, want b1", f.lastPageBranch)
	}
}

func TestListAgentObjectsUpdatedMode(t *testing.T) {
	f := agentObjectsFixture()
	_, e := newAgentObjectsEcho(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents/a1/objects?provenance=updated", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.lastPageProvenance != "updated" {
		t.Errorf("provenance = %q, want updated", f.lastPageProvenance)
	}
}

func TestListAgentObjectsLimitCapped(t *testing.T) {
	f := agentObjectsFixture()
	_, e := newAgentObjectsEcho(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents/a1/objects?limit=1000", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.lastPageLimit != agentObjectsMaxLimit {
		t.Errorf("limit = %d, want cap %d", f.lastPageLimit, agentObjectsMaxLimit)
	}
}

func TestListAgentObjectsRejectsBadParams(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"bad provenance", "/api/agents/a1/objects?provenance=bogus"},
		{"zero limit", "/api/agents/a1/objects?limit=0"},
		{"negative limit", "/api/agents/a1/objects?limit=-3"},
		{"non-integer limit", "/api/agents/a1/objects?limit=lots"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := agentObjectsFixture()
			_, e := newAgentObjectsEcho(f)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.url, nil))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			// A rejected request must not touch the backend.
			if f.lastPageActorType != "" || f.lastPageLimit != 0 {
				t.Errorf("backend called for rejected params: %+v", f)
			}
		})
	}
}

func TestListAgentObjectsUnknownAgent(t *testing.T) {
	_, e := newAgentObjectsEcho(&fakeMemory{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents/ghost/objects", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown agent") {
		t.Errorf("body = %s, want unknown agent", rec.Body.String())
	}
}

func TestListAgentObjectsBackendDown(t *testing.T) {
	f := agentObjectsFixture()
	f.pageErr = fmt.Errorf("memory 503: graph down")
	_, e := newAgentObjectsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents/a1/objects", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "memory service unavailable") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestListAgentObjectsEmptyIsArray(t *testing.T) {
	f := &fakeMemory{agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}}}
	_, e := newAgentObjectsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents/a1/objects", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"items":[]`) {
		t.Errorf("body = %s, want empty items array (not null)", body)
	}
	if !strings.Contains(body, `"next_cursor":""`) {
		t.Errorf("body = %s, want empty next_cursor", body)
	}
}
