package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMemoryFromParts covers the entity→Memory normalization (parity with
// admin.py _memory_item + _memory_content).
func TestMemoryFromParts(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		objType string
		props   map[string]any
		wantMem Memory
	}{
		{
			name:    "note keeps its own category and confidence",
			id:      "e1",
			objType: "Note",
			props:   map[string]any{"content": "likes dark roast", "category": "preference", "confidence": 0.92},
			wantMem: Memory{ID: "e1", Content: "likes dark roast", Category: "preference", Confidence: 0.92},
		},
		{
			name:    "person derives name + relationship",
			id:      "e2",
			objType: "person",
			props:   map[string]any{"first_name": "Sam", "last_name": "Lee", "relationship": "colleague"},
			wantMem: Memory{ID: "e2", Content: "Sam Lee (colleague)", Category: "person", Confidence: 1},
		},
		{
			name:    "task uses title",
			id:      "e3",
			objType: "task",
			props:   map[string]any{"title": "water the plants"},
			wantMem: Memory{ID: "e3", Content: "water the plants", Category: "task", Confidence: 1},
		},
		{
			name:    "note confidence may arrive as int",
			id:      "e4",
			objType: "Note",
			props:   map[string]any{"content": "x", "category": "fact", "confidence": 1},
			wantMem: Memory{ID: "e4", Content: "x", Category: "fact", Confidence: 1},
		},
		{
			name:    "unknown type falls back to a known property then the type",
			id:      "e5",
			objType: "widget",
			props:   map[string]any{"summary": "a widget"},
			wantMem: Memory{ID: "e5", Content: "a widget", Category: "widget", Confidence: 1},
		},
		{
			name:    "typed entity with no props returns empty content (parity with admin.py)",
			id:      "e6",
			objType: "place",
			props:   map[string]any{},
			wantMem: Memory{ID: "e6", Category: "place", Confidence: 1},
		},
		{
			name:    "note with missing fields degrades to zero values",
			id:      "e7",
			objType: "Note",
			props:   map[string]any{},
			wantMem: Memory{ID: "e7"},
		},
	}
	for _, c := range cases {
		got := memoryFromParts(c.id, c.objType, c.props)
		if got != c.wantMem {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.wantMem)
		}
	}
}

// TestSearchMemories exercises POST /api/search/unified end-to-end:
// request shape, response parsing, score capture, and normalization of graph
// results (Note, typed) plus a text-chunk result with no graph fields.
// Results below minSearchScore (0.08) are filtered out.
func TestSearchMemories(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search/unified" {
			t.Errorf("path = %q, want /api/search/unified", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[
			{"type":"graph","id":"oid1","object_id":"oid1","canonical_id":"m1","object_type":"Note","key":"m1","score":0.168,"fields":{"content":"likes dark roast","category":"preference"}},
			{"type":"graph","id":"oid2","object_id":"oid2","canonical_id":"m2","object_type":"person","key":"sam-person","score":0.31,"fields":{"first_name":"Sam","relationship":"colleague"}},
			{"type":"graph","id":"oid3","object_id":"oid3","canonical_id":"m3","object_type":"task","key":"t3","score":0.05,"fields":{"title":"water the plants"}},
			{"type":"text","id":"chunk1","snippet":"a chunk of text","score":0.22},
			{"type":"relationship","id":"rel1","score":0.03}
		],"metadata":{}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok123", "proj")
	mems, err := m.SearchMemories(context.Background(), "dark roast")
	if err != nil {
		t.Fatal(err)
	}
	if gotBody["query"] != "dark roast" {
		t.Errorf("query = %v, want dark roast", gotBody["query"])
	}
	if gotBody["limit"] != float64(50) {
		t.Errorf("limit = %v, want 50", gotBody["limit"])
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("auth = %q", gotAuth)
	}
	// m3 (0.05) and rel1 (0.03) are below minSearchScore and must be dropped.
	want := []Memory{
		{ID: "m1", Content: "likes dark roast", Category: "preference", Score: 0.168},
		{ID: "m2", Content: "Sam (colleague)", Category: "person", Confidence: 1, Score: 0.31},
		// text chunk: no graph fields, falls back to its snippet
		{ID: "chunk1", Content: "a chunk of text", Confidence: 1, Score: 0.22},
	}
	if len(mems) != len(want) {
		t.Fatalf("got %d memories, want %d: %+v", len(mems), len(want), mems)
	}
	for i := range want {
		if mems[i] != want[i] {
			t.Errorf("memory %d = %+v, want %+v", i, mems[i], want[i])
		}
	}
}

// TestSearchMemoriesError surfaces non-2xx responses as errors.
func TestSearchMemoriesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":{"code":"upstream","message":"memory down"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, err := m.SearchMemories(context.Background(), "x"); err == nil {
		t.Fatal("want error, got nil")
	}
}

// TestListMemories exercises the REST memory list: GET /api/graph/objects/search
// (no type filter) normalized to []Memory — replaces the old MCP entity-query flow.
func TestListMemories(t *testing.T) {
	var sawPath, sawMethod, sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		sawMethod = r.Method
		sawAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/api/graph/objects/search" {
			t.Errorf("path = %q, want /api/graph/objects/search", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[
			{"id":"e1","type":"Note","properties":{"content":"prefers dark roast","category":"preference","confidence":0.9}},
			{"id":"e2","type":"person","properties":{"first_name":"Sam","relationship":"colleague"}}
		],"total":2}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok123", "proj")
	mems, err := m.ListMemories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sawPath != "/api/graph/objects/search" || sawMethod != http.MethodGet {
		t.Errorf("request = %s %s", sawMethod, sawPath)
	}
	if sawAuth != "Bearer tok123" {
		t.Errorf("auth = %q", sawAuth)
	}
	want := []Memory{
		{ID: "e1", Content: "prefers dark roast", Category: "preference", Confidence: 0.9},
		{ID: "e2", Content: "Sam (colleague)", Category: "person", Confidence: 1},
	}
	if len(mems) != len(want) {
		t.Fatalf("got %d memories, want %d: %+v", len(mems), len(want), mems)
	}
	for i := range want {
		if mems[i] != want[i] {
			t.Errorf("memory %d = %+v, want %+v", i, mems[i], want[i])
		}
	}
}

// --- blueprint/schema client methods ---

func TestGetCompiledTypes(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"objectTypes":[{"name":"person","label":"Person","description":"a person","schemaName":"personal-memory","schemaVersion":"1.0.0","shadowed":false}],
			"relationshipTypes":[{"name":"assigned_to","label":"Assigned To","sourceType":"task","targetType":"person","schemaName":"personal-memory"}]
		}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	out, err := m.GetCompiledTypes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/schemas/projects/proj-1/compiled-types" {
		t.Errorf("path = %q", gotPath)
	}
	if len(out.ObjectTypes) != 1 || out.ObjectTypes[0].Name != "person" || out.ObjectTypes[0].SchemaName != "personal-memory" {
		t.Errorf("objectTypes = %+v", out.ObjectTypes)
	}
	if len(out.RelationshipTypes) != 1 || out.RelationshipTypes[0].SourceType != "task" || out.RelationshipTypes[0].TargetType != "person" {
		t.Errorf("relationshipTypes = %+v", out.RelationshipTypes)
	}
}

// TestListAllSchemas exercises the REST schema-catalog endpoint
// (GET /api/schemas/projects/:projectId — the REST mirror of the MCP
// schema-list tool), parsing the schemas array out of the JSON envelope.
func TestListAllSchemas(t *testing.T) {
	var sawPath, sawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		sawQuery = r.URL.RawQuery
		if r.URL.Path != "/api/schemas/projects/proj" {
			t.Errorf("path = %q, want /api/schemas/projects/proj", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		_, _ = io.WriteString(w, `{"project_id":"proj","schemas":[{"id":"s1","name":"multi-agent","version":"1.0.0","description":"","project_id":"","source":"system"},{"id":"s2","name":"agent-notes","version":"1.0.0","description":"","project_id":"proj","source":"manual"}],"total":2,"limit":100,"offset":0}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	out, err := m.ListAllSchemas(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sawPath != "/api/schemas/projects/proj" {
		t.Errorf("path = %q", sawPath)
	}
	if !strings.Contains(sawQuery, "limit=100") {
		t.Errorf("query %q missing limit=100", sawQuery)
	}
	if len(out) != 2 {
		t.Fatalf("got %d schemas, want 2: %+v", len(out), out)
	}
	if out[0].Name != "multi-agent" || out[0].ProjectID != "" {
		t.Errorf("schema0 = %+v", out[0])
	}
	if out[1].Name != "agent-notes" || out[1].ProjectID != "proj" {
		t.Errorf("schema1 = %+v", out[1])
	}
}

func TestGetSchema(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"s1","name":"multi-agent","version":"1.0.0","description":"multi","author":"emergent","objectTypeSchemas":[{"name":"Task","label":"Task"}],"relationshipTypeSchemas":[{"name":"has_subtask","sourceType":"Task","targetType":"Task"}],"migrations":{"from_version":"0.9.0","type_renames":[{"from":"Job","to":"Task"}],"property_renames":[{"type_name":"Task","from":"jobId","to":"taskId"}]}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	s, err := m.GetSchema(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/schemas/s1" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("auth = %q", gotAuth)
	}
	if s.Name != "multi-agent" || s.Author != "emergent" {
		t.Errorf("schema = %+v", s)
	}
	if len(s.ObjectTypeSchemas) == 0 || len(s.RelationshipTypeSchemas) == 0 {
		t.Errorf("raw schemas missing: %+v", s)
	}
	if s.Migrations == nil || s.Migrations.FromVersion != "0.9.0" || len(s.Migrations.TypeRenames) != 1 || s.Migrations.TypeRenames[0].To != "Task" {
		t.Errorf("migrations = %+v", s.Migrations)
	}
}

// --- migration client methods ---

func TestGetSchemaHistory(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"a1","schemaId":"s1","name":"agent-notes","version":"1.0.0","active":true,"installedAt":"2026-08-26T10:00:00Z","removedAt":""}]`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	out, err := m.GetSchemaHistory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/schemas/projects/proj-1/history" {
		t.Errorf("path = %q", gotPath)
	}
	if len(out) != 1 || out[0].Name != "agent-notes" || !out[0].Active {
		t.Errorf("history = %+v", out)
	}
}

func TestPreviewMigration(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"project_id":"proj-1","from_schema_id":"s1","to_schema_id":"s2","overall_risk_level":"risky","can_proceed":false,"block_reason":"breaking changes","total_objects":12,"plan":{"type_renames":[{"from":"Contract","to":"Agreement"}],"property_renames":[{"type_name":"Agreement","from":"signedDate","to":"signed_at"}],"removed_properties":[{"type_name":"Agreement","name":"legacyId"}],"added_types":["Clause"],"added_properties":[{"type_name":"Agreement","name":"jurisdiction"}]},"per_type_results":[{"type_name":"Agreement","object_count":12,"risk_level":"risky","can_proceed":false,"block_reason":"breaking changes","migrated_props":["title"],"dropped_props":["legacyId"],"added_props":["jurisdiction"],"coerced_props":["signedDate"]}]}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	res, err := m.PreviewMigration(context.Background(), "s1", "s2")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/schemas/projects/proj-1/migrate/preview" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["from_schema_id"] != "s1" || gotBody["to_schema_id"] != "s2" {
		t.Errorf("body = %v", gotBody)
	}
	if res.OverallRiskLevel != "risky" || res.CanProceed || res.BlockReason != "breaking changes" || res.TotalObjects != 12 {
		t.Errorf("res = %+v", res)
	}
	if res.Plan == nil {
		t.Fatal("plan not decoded")
	}
	if len(res.Plan.TypeRenames) != 1 || res.Plan.TypeRenames[0].From != "Contract" || res.Plan.TypeRenames[0].To != "Agreement" {
		t.Errorf("plan.type_renames = %+v", res.Plan.TypeRenames)
	}
	if len(res.Plan.PropertyRenames) != 1 || res.Plan.PropertyRenames[0].TypeName != "Agreement" || res.Plan.PropertyRenames[0].From != "signedDate" || res.Plan.PropertyRenames[0].To != "signed_at" {
		t.Errorf("plan.property_renames = %+v", res.Plan.PropertyRenames)
	}
	if len(res.Plan.RemovedProperties) != 1 || res.Plan.RemovedProperties[0].TypeName != "Agreement" || res.Plan.RemovedProperties[0].Name != "legacyId" {
		t.Errorf("plan.removed_properties = %+v", res.Plan.RemovedProperties)
	}
	if len(res.Plan.AddedTypes) != 1 || res.Plan.AddedTypes[0] != "Clause" {
		t.Errorf("plan.added_types = %+v", res.Plan.AddedTypes)
	}
	if len(res.Plan.AddedProperties) != 1 || res.Plan.AddedProperties[0].TypeName != "Agreement" || res.Plan.AddedProperties[0].Name != "jurisdiction" {
		t.Errorf("plan.added_properties = %+v", res.Plan.AddedProperties)
	}
	if len(res.PerTypeResults) != 1 {
		t.Fatalf("per_type_results = %+v", res.PerTypeResults)
	}
	ptr := res.PerTypeResults[0]
	if ptr.TypeName != "Agreement" || ptr.ObjectCount != 12 || ptr.RiskLevel != "risky" || ptr.CanProceed || ptr.BlockReason != "breaking changes" {
		t.Errorf("per_type_results[0] = %+v", ptr)
	}
	if len(ptr.MigratedProps) != 1 || ptr.MigratedProps[0] != "title" {
		t.Errorf("migrated_props = %v", ptr.MigratedProps)
	}
	if len(ptr.DroppedProps) != 1 || ptr.DroppedProps[0] != "legacyId" {
		t.Errorf("dropped_props = %v", ptr.DroppedProps)
	}
	if len(ptr.AddedProps) != 1 || ptr.AddedProps[0] != "jurisdiction" {
		t.Errorf("added_props = %v", ptr.AddedProps)
	}
	if len(ptr.CoercedProps) != 1 || ptr.CoercedProps[0] != "signedDate" {
		t.Errorf("coerced_props = %v", ptr.CoercedProps)
	}
}

func TestExecuteMigration(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"project_id":"proj-1","objects_migrated":12,"objects_failed":0,"risk_level":"safe"}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	res, err := m.ExecuteMigration(context.Background(), "s1", "s2", true)
	if err != nil {
		t.Fatal(err)
	}
	if gotBody["from_schema_id"] != "s1" || gotBody["to_schema_id"] != "s2" || gotBody["force"] != true {
		t.Errorf("body = %v", gotBody)
	}
	if res.ObjectsMigrated != 12 {
		t.Errorf("res = %+v", res)
	}
}

func TestRollbackMigration(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"project_id":"proj-1","to_version":"1.0.0","objects_restored":5,"objects_failed":0}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	res, err := m.RollbackMigration(context.Background(), "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if gotBody["to_version"] != "1.0.0" {
		t.Errorf("body = %v", gotBody)
	}
	if res.ObjectsRestored != 5 {
		t.Errorf("res = %+v", res)
	}
}

func TestGetMigrationJobStatus(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"job1","status":"completed","objects_migrated":12,"objects_failed":0}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	job, err := m.GetMigrationJobStatus(context.Background(), "job1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/schemas/projects/proj-1/migration-jobs/job1" {
		t.Errorf("path = %q", gotPath)
	}
	if job.Status != "completed" {
		t.Errorf("job = %+v", job)
	}
}

func TestValidateSchemas(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"project_id":"proj-1","total_objects":100,"stale_objects":3}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	res, err := m.ValidateSchemas(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/schemas/projects/proj-1/validate" {
		t.Errorf("path = %q", gotPath)
	}
	if res.StaleObjects != 3 {
		t.Errorf("res = %+v", res)
	}
}

// TestSessionContextCredentialResolution asserts MemoryClient resolves the
// bearer token and active project per request: static credentials when no
// session context is attached (API-key / programmatic path) and the session's
// credentials when withSessionContext is used (design D3).
func TestSessionContextCredentialResolution(t *testing.T) {
	type seen struct {
		auth string
		proj string
		path string
	}
	var got seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = seen{
			auth: r.Header.Get("Authorization"),
			proj: r.Header.Get("X-Project-ID"),
			path: r.URL.Path,
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"documents":[],"next_cursor":""}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "static-token", "static-project")

	// (a) No session context: falls back to the static server credentials.
	if _, _, err := m.ListDocuments(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if got.auth != "Bearer static-token" {
		t.Errorf("fallback auth = %q, want Bearer static-token", got.auth)
	}
	if got.proj != "static-project" {
		t.Errorf("fallback X-Project-ID = %q, want static-project", got.proj)
	}

	// (b) Session context: per-request token + active project win.
	sessCtx := withSessionContext(context.Background(), &sessionContext{
		Token:     "sess-token",
		ProjectID: "sess-project",
	})
	if _, _, err := m.ListDocuments(sessCtx, ""); err != nil {
		t.Fatal(err)
	}
	if got.auth != "Bearer sess-token" {
		t.Errorf("session auth = %q, want Bearer sess-token", got.auth)
	}
	if got.proj != "sess-project" {
		t.Errorf("session X-Project-ID = %q, want sess-project", got.proj)
	}

	// The active project also threads through path-embedded routes.
	if _, err := m.ListAgentDefinitions(sessCtx); err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/projects/sess-project/agent-definitions" {
		t.Errorf("session path = %q, want /api/projects/sess-project/agent-definitions", got.path)
	}

	// (c) Partial session context: an empty field still falls back.
	partialCtx := withSessionContext(context.Background(), &sessionContext{ProjectID: "only-proj"})
	if _, _, err := m.ListDocuments(partialCtx, ""); err != nil {
		t.Fatal(err)
	}
	if got.auth != "Bearer static-token" {
		t.Errorf("partial auth = %q, want static-token fallback", got.auth)
	}
	if got.proj != "only-proj" {
		t.Errorf("partial X-Project-ID = %q, want only-proj", got.proj)
	}
}

// TestSessionContextFromCoversAbsence asserts sessionContextFrom reports
// absence on a plain context and round-trips the attached value.
func TestSessionContextFromCoversAbsence(t *testing.T) {
	if sc, ok := sessionContextFrom(context.Background()); ok || sc != nil {
		t.Errorf("plain context: got (%+v, %v), want (nil, false)", sc, ok)
	}
	want := &sessionContext{Token: "t", ProjectID: "p", OrgID: "o"}
	ctx := withSessionContext(context.Background(), want)
	got, ok := sessionContextFrom(ctx)
	if !ok {
		t.Fatal("session context not found after withSessionContext")
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// --- orgs & projects client methods ---

// TestListOrgs exercises GET /api/orgs decoding the bare JSON array.
func TestListOrgs(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"o1","name":"Acme"},{"id":"o2","name":"Globex"}]`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	orgs, err := m.ListOrgs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/orgs" {
		t.Errorf("path = %q, want /api/orgs", gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("auth = %q", gotAuth)
	}
	if len(orgs) != 2 || orgs[0].Name != "Acme" || orgs[1].ID != "o2" {
		t.Errorf("orgs = %+v", orgs)
	}
}

// TestListProjects exercises GET /api/projects decoding the bare JSON array.
func TestListProjects(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"p1","name":"Main","orgId":"o1"},{"id":"p2","name":"Lab","orgId":"o2"}]`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	projects, err := m.ListProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects" {
		t.Errorf("path = %q, want /api/projects", gotPath)
	}
	if len(projects) != 2 || projects[0].OrgID != "o1" || projects[1].Name != "Lab" {
		t.Errorf("projects = %+v", projects)
	}
}

// TestCreateProject exercises POST /api/projects: body shape {name, orgId} and
// the 201 bare-project response.
func TestCreateProject(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"p-new","name":"New","orgId":"o1"}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	p, err := m.CreateProject(context.Background(), "New", "o1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/projects", gotMethod, gotPath)
	}
	if gotBody["name"] != "New" || gotBody["orgId"] != "o1" {
		t.Errorf("body = %v", gotBody)
	}
	if p.ID != "p-new" || p.Name != "New" || p.OrgID != "o1" {
		t.Errorf("project = %+v", p)
	}
}

// TestListProjectsIncludingPending exercises GET
// /api/projects?include_pending=true decoding the bare JSON array including
// soft-deleted projects.
func TestListProjectsIncludingPending(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"p1","name":"Main","orgId":"o1"},{"id":"p2","name":"Gone","orgId":"o1","deletionStatus":"pending_deletion","deletionScheduledFor":"2026-09-20T00:00:00Z"}]`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	projects, err := m.ListProjectsIncludingPending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotURI != "/api/projects?include_pending=true" {
		t.Errorf("uri = %q, want /api/projects?include_pending=true", gotURI)
	}
	if len(projects) != 2 {
		t.Fatalf("projects = %+v, want 2", projects)
	}
	if projects[0].PendingDeletion() {
		t.Errorf("projects[0].PendingDeletion() = true, want false")
	}
	if !projects[1].PendingDeletion() {
		t.Errorf("projects[1].PendingDeletion() = false, want true")
	}
}

// TestRestoreProject exercises POST /api/projects/{id}/restore.
func TestRestoreProject(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.RestoreProject(context.Background(), "p1"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/projects/p1/restore" {
		t.Errorf("request = %s %s, want POST /api/projects/p1/restore", gotMethod, gotPath)
	}
}

// TestSessionHeadersOnOutbound asserts X-Project-ID/X-Org-ID are added to
// outbound Memory requests only when a session context is present (design D6).
func TestSessionHeadersOnOutbound(t *testing.T) {
	var gotProj, gotOrg string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProj = r.Header.Get("X-Project-ID")
		gotOrg = r.Header.Get("X-Org-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":[]}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "static-token", "static-project")

	// No session context (API-key path): no scoping headers on path-embedded
	// routes — the token is already project-bound.
	if _, err := m.ListAgentDefinitions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotProj != "" {
		t.Errorf("X-Project-ID = %q, want empty without a session", gotProj)
	}

	// Session context: X-Project-ID + X-Org-ID are threaded.
	ctx := withSessionContext(context.Background(), &sessionContext{
		Token:     "sess-token",
		ProjectID: "sess-project",
		OrgID:     "sess-org",
	})
	if _, err := m.ListAgentDefinitions(ctx); err != nil {
		t.Fatal(err)
	}
	if gotProj != "sess-project" {
		t.Errorf("X-Project-ID = %q, want sess-project", gotProj)
	}
	if gotOrg != "sess-org" {
		t.Errorf("X-Org-ID = %q, want sess-org", gotOrg)
	}
}

// TestCreateOrg exercises POST /api/orgs: body {name} and the 201 bare-org
// response.
func TestCreateOrg(t *testing.T) {
	var gotPath, gotMethod, gotAuth string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"o-new","name":"Acme"}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	o, err := m.CreateOrg(context.Background(), "Acme")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/orgs" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/orgs", gotMethod, gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotBody["name"] != "Acme" {
		t.Errorf("body = %v", gotBody)
	}
	if o.ID != "o-new" || o.Name != "Acme" {
		t.Errorf("org = %+v", o)
	}
}

// TestGetOrgsAndProjects exercises GET /api/user/orgs-and-projects decoding the
// org→project access tree with per-level roles.
func TestGetOrgsAndProjects(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"id":"o1","name":"Acme","role":"org_admin","projects":[
				{"id":"p1","name":"Main","orgId":"o1","role":"project_admin"},
				{"id":"p2","name":"Lab","orgId":"o1","role":"project_user"}
			]},
			{"id":"o2","name":"Globex","role":"org_member","projects":[]}
		]`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	tree, err := m.GetOrgsAndProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/user/orgs-and-projects" {
		t.Errorf("path = %q, want /api/user/orgs-and-projects", gotPath)
	}
	if len(tree) != 2 || tree[0].Name != "Acme" || tree[0].Role != "org_admin" {
		t.Fatalf("tree = %+v", tree)
	}
	if len(tree[0].Projects) != 2 {
		t.Fatalf("projects = %+v", tree[0].Projects)
	}
	if tree[0].Projects[0].ID != "p1" || tree[0].Projects[0].Role != "project_admin" {
		t.Errorf("first project = %+v", tree[0].Projects[0])
	}
	if tree[0].Projects[1].Role != "project_user" || tree[0].Projects[1].OrgID != "o1" {
		t.Errorf("second project = %+v", tree[0].Projects[1])
	}
	if tree[1].Role != "org_member" || len(tree[1].Projects) != 0 {
		t.Errorf("second org = %+v", tree[1])
	}
}

// TestTransferProject exercises POST /api/projects/{id}/transfer: the JSON
// body {"orgId": destinationOrgID}, the bearer token, and the session scoping
// headers on an outbound request.
func TestTransferProject(t *testing.T) {
	var gotPath, gotMethod, gotAuth, gotProj, gotOrg string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotProj = r.Header.Get("X-Project-ID")
		gotOrg = r.Header.Get("X-Org-ID")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	ctx := withSessionContext(context.Background(), &sessionContext{
		Token:     "sess-token",
		ProjectID: "sess-project",
		OrgID:     "sess-org",
	})
	if err := m.TransferProject(ctx, "p1", "o2"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/projects/p1/transfer" {
		t.Errorf("request = %s %s, want POST /api/projects/p1/transfer", gotMethod, gotPath)
	}
	if gotBody["orgId"] != "o2" {
		t.Errorf("body = %v, want orgId o2", gotBody)
	}
	if gotAuth != "Bearer sess-token" {
		t.Errorf("auth = %q, want Bearer sess-token", gotAuth)
	}
	if gotProj != "sess-project" {
		t.Errorf("X-Project-ID = %q, want sess-project", gotProj)
	}
	if gotOrg != "sess-org" {
		t.Errorf("X-Org-ID = %q, want sess-org", gotOrg)
	}
}

// TestListMembers exercises GET /api/projects/{id}/members against the active
// project ("proj").
func TestListMembers(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"id":"u1","email":"a@example.com","displayName":"Ada","firstName":"Ada","lastName":"Lovelace","avatarUrl":"http://a","role":"project_admin","joinedAt":"2024-01-01T00:00:00Z"},
			{"id":"u2","email":"b@example.com","displayName":"Bob","role":"project_user","joinedAt":"2024-02-01T00:00:00Z"}
		]`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	members, err := m.ListMembers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/proj/members" {
		t.Errorf("path = %q, want /api/projects/proj/members", gotPath)
	}
	if len(members) != 2 {
		t.Fatalf("members = %+v", members)
	}
	if members[0].ID != "u1" || members[0].Email != "a@example.com" || members[0].DisplayName != "Ada" || members[0].Role != "project_admin" || members[0].FirstName != "Ada" || members[0].LastName != "Lovelace" || members[0].AvatarURL != "http://a" || members[0].JoinedAt == "" {
		t.Errorf("first member = %+v", members[0])
	}
	if members[1].Role != "project_user" || members[1].Email != "b@example.com" {
		t.Errorf("second member = %+v", members[1])
	}
}

// TestRemoveMember exercises DELETE /api/projects/{id}/members/{userId}.
func TestRemoveMember(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var gotPath, gotMethod string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotMethod = r.Method
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()
		m := NewMemoryClient(srv.URL, "tok", "proj")
		if err := m.RemoveMember(context.Background(), "u1"); err != nil {
			t.Fatal(err)
		}
		if gotPath != "/api/projects/proj/members/u1" || gotMethod != http.MethodDelete {
			t.Errorf("request = %s %s, want DELETE /api/projects/proj/members/u1", gotMethod, gotPath)
		}
	})

	t.Run("last-admin 403 surfaces as error", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":{"code":"last-admin","message":"cannot remove the sole admin"}}`)
		}))
		defer srv.Close()
		m := NewMemoryClient(srv.URL, "tok", "proj")
		err := m.RemoveMember(context.Background(), "u1")
		if err == nil {
			t.Fatal("expected error for 403 last-admin, got nil")
		}
		if gotPath != "/api/projects/proj/members/u1" {
			t.Errorf("path = %q, want /api/projects/proj/members/u1", gotPath)
		}
	})
}

// TestListInvites exercises GET /api/projects/{id}/invites against the active
// project ("proj").
func TestListInvites(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"id":"i1","email":"a@example.com","role":"project_admin","status":"pending","createdAt":"2024-01-01T00:00:00Z"},
			{"id":"i2","email":"b@example.com","role":"project_user","status":"accepted","createdAt":"2024-01-02T00:00:00Z"}
		]`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	invites, err := m.ListInvites(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/proj/invites" {
		t.Errorf("path = %q, want /api/projects/proj/invites", gotPath)
	}
	if len(invites) != 2 || invites[0].ID != "i1" || invites[0].Email != "a@example.com" || invites[0].Role != "project_admin" || invites[0].Status != "pending" {
		t.Errorf("invites = %+v", invites)
	}
}

// TestCreateInvite exercises POST /api/invites: the request DTO carries the
// exact role strings and the 201 response includes the token.
func TestCreateInvite(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody CreateInviteDto
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"i-new","organizationId":"o1","projectId":"proj","email":"a@example.com","role":"project_admin","token":"inv-tok","status":"pending","createdAt":"2024-01-01T00:00:00Z"}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	inv, err := m.CreateInvite(context.Background(), CreateInviteDto{
		OrgID:     "o1",
		ProjectID: "proj",
		Email:     "a@example.com",
		Role:      "project_admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/invites" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/invites", gotMethod, gotPath)
	}
	if gotBody.OrgID != "o1" || gotBody.ProjectID != "proj" || gotBody.Email != "a@example.com" || gotBody.Role != "project_admin" {
		t.Errorf("body = %+v", gotBody)
	}
	if inv.ID != "i-new" || inv.OrganizationID != "o1" || inv.ProjectID != "proj" || inv.Role != "project_admin" || inv.Token != "inv-tok" || inv.Status != "pending" {
		t.Errorf("invite = %+v", inv)
	}
}

// TestAcceptInvite exercises POST /api/invites/accept with the token body.
func TestAcceptInvite(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"accepted"}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.AcceptInvite(context.Background(), "inv-tok"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/invites/accept" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/invites/accept", gotMethod, gotPath)
	}
	if gotBody["token"] != "inv-tok" {
		t.Errorf("body = %v", gotBody)
	}
}

// TestListPendingInvites exercises GET /api/invites/pending decoding the token
// field.
func TestListPendingInvites(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"id":"i1","projectId":"proj","projectName":"Main","organizationId":"o1","organizationName":"Acme","role":"project_admin","token":"inv-tok","createdAt":"2024-01-01T00:00:00Z"}
		]`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	invites, err := m.ListPendingInvites(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/invites/pending" {
		t.Errorf("path = %q, want /api/invites/pending", gotPath)
	}
	if len(invites) != 1 || invites[0].ID != "i1" || invites[0].ProjectName != "Main" || invites[0].OrganizationName != "Acme" || invites[0].Role != "project_admin" || invites[0].Token != "inv-tok" {
		t.Errorf("invites = %+v", invites)
	}
}

// TestDeclineInvite exercises POST /api/invites/{id}/decline.
func TestDeclineInvite(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.DeclineInvite(context.Background(), "i1"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/invites/i1/decline" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/invites/i1/decline", gotMethod, gotPath)
	}
}

// TestCancelInvite exercises DELETE /api/invites/{id}.
func TestCancelInvite(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.CancelInvite(context.Background(), "i1"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/invites/i1" || gotMethod != http.MethodDelete {
		t.Errorf("request = %s %s, want DELETE /api/invites/i1", gotMethod, gotPath)
	}
}

// TestSearchUsers exercises GET /api/users/search?email= with the query
// escaped, decoding the wrapped users array.
func TestSearchUsers(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"users":[
			{"id":"u1","email":"alice@example.com","displayName":"Alice","firstName":"Alice","lastName":"Smith"}
		]}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	users, err := m.SearchUsers(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if gotURI != "/api/users/search?email=alice%40example.com" {
		t.Errorf("uri = %q, want /api/users/search?email=alice%%40example.com", gotURI)
	}
	if len(users) != 1 || users[0].ID != "u1" || users[0].Email != "alice@example.com" || users[0].DisplayName != "Alice" || users[0].FirstName != "Alice" || users[0].LastName != "Smith" {
		t.Errorf("users = %+v", users)
	}
}

// TestGetProfile exercises GET /api/user/profile decoding the profile fields.
func TestGetProfile(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"u1","subjectId":"sub-1","firstName":"Ada","lastName":"Lovelace","displayName":"Ada Lovelace","email":"ada@example.com","phoneE164":"+1234567890"}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	p, err := m.GetProfile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/user/profile" || gotMethod != http.MethodGet {
		t.Errorf("request = %s %s, want GET /api/user/profile", gotMethod, gotPath)
	}
	if p.ID != "u1" || p.SubjectID != "sub-1" || p.FirstName != "Ada" || p.LastName != "Lovelace" || p.DisplayName != "Ada Lovelace" || p.Email != "ada@example.com" || p.PhoneE164 != "+1234567890" {
		t.Errorf("profile = %+v", p)
	}
}

// TestUpdateProfile exercises PUT /api/user/profile with the update fields in
// the body.
func TestUpdateProfile(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody UpdateUserProfileDto
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"u1","firstName":"Grace","lastName":"Hopper","displayName":"Grace Hopper","phoneE164":"+0987654321"}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	p, err := m.UpdateProfile(context.Background(), UpdateUserProfileDto{
		FirstName:   "Grace",
		LastName:    "Hopper",
		DisplayName: "Grace Hopper",
		PhoneE164:   "+0987654321",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/user/profile" || gotMethod != http.MethodPut {
		t.Errorf("request = %s %s, want PUT /api/user/profile", gotMethod, gotPath)
	}
	if gotBody.FirstName != "Grace" || gotBody.LastName != "Hopper" || gotBody.DisplayName != "Grace Hopper" || gotBody.PhoneE164 != "+0987654321" {
		t.Errorf("body = %+v", gotBody)
	}
	if p.ID != "u1" || p.FirstName != "Grace" || p.LastName != "Hopper" || p.DisplayName != "Grace Hopper" || p.PhoneE164 != "+0987654321" {
		t.Errorf("profile = %+v", p)
	}
}

// TestGetOrg exercises GET /api/orgs/{id} decoding the bare org.
func TestGetOrg(t *testing.T) {
	var gotPath, gotMethod, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"o1","name":"Acme"}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	o, err := m.GetOrg(context.Background(), "o1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/orgs/o1" || gotMethod != http.MethodGet {
		t.Errorf("request = %s %s, want GET /api/orgs/o1", gotMethod, gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("auth = %q", gotAuth)
	}
	if o.ID != "o1" || o.Name != "Acme" {
		t.Errorf("org = %+v", o)
	}
}

// TestListOrgMembers exercises GET /api/orgs/{id}/members: member identity +
// role, with the displayName/firstName/lastName pointers nil when the backend
// omits them.
func TestListOrgMembers(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"id":"u1","email":"a@example.com","displayName":"Ada","firstName":"Ada","lastName":"Lovelace","role":"org_admin","joinedAt":"2024-01-01T00:00:00Z"},
			{"id":"u2","email":"b@example.com","role":"org_member","joinedAt":"2024-02-01T00:00:00Z"}
		]`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	members, err := m.ListOrgMembers(context.Background(), "o1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/orgs/o1/members" {
		t.Errorf("path = %q, want /api/orgs/o1/members", gotPath)
	}
	if len(members) != 2 {
		t.Fatalf("members = %+v", members)
	}
	first := members[0]
	if first.ID != "u1" || first.Email != "a@example.com" || first.Role != "org_admin" || first.JoinedAt == "" {
		t.Errorf("first member = %+v", first)
	}
	if first.DisplayName == nil || *first.DisplayName != "Ada" || first.FirstName == nil || *first.FirstName != "Ada" || first.LastName == nil || *first.LastName != "Lovelace" {
		t.Errorf("first member pointers = %+v", first)
	}
	second := members[1]
	if second.Role != "org_member" || second.Email != "b@example.com" {
		t.Errorf("second member = %+v", second)
	}
	if second.DisplayName != nil || second.FirstName != nil || second.LastName != nil {
		t.Errorf("second member pointers = %+v, want nil", second)
	}
}

// TestDeleteOrg exercises DELETE /api/orgs/{id}.
func TestDeleteOrg(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.DeleteOrg(context.Background(), "o1"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/orgs/o1" || gotMethod != http.MethodDelete {
		t.Errorf("request = %s %s, want DELETE /api/orgs/o1", gotMethod, gotPath)
	}
}

// TestListOrgToolSettings exercises GET /api/admin/orgs/{orgId}/tool-settings
// decoding the bare array of settings.
func TestListOrgToolSettings(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"id":"ts1","orgId":"o1","toolName":"web_search","enabled":true,"config":{"maxResults":5},"createdAt":"2024-01-01T00:00:00Z","updatedAt":"2024-01-02T00:00:00Z"},
			{"id":"ts2","orgId":"o1","toolName":"code","enabled":false,"createdAt":"2024-01-01T00:00:00Z","updatedAt":"2024-01-01T00:00:00Z"}
		]`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	settings, err := m.ListOrgToolSettings(context.Background(), "o1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/admin/orgs/o1/tool-settings" || gotMethod != http.MethodGet {
		t.Errorf("request = %s %s, want GET /api/admin/orgs/o1/tool-settings", gotMethod, gotPath)
	}
	if len(settings) != 2 {
		t.Fatalf("settings = %+v", settings)
	}
	if settings[0].ID != "ts1" || settings[0].OrgID != "o1" || settings[0].ToolName != "web_search" || !settings[0].Enabled || settings[0].Config["maxResults"] != float64(5) || settings[0].UpdatedAt == "" {
		t.Errorf("first setting = %+v", settings[0])
	}
	if settings[1].ToolName != "code" || settings[1].Enabled || settings[1].Config != nil {
		t.Errorf("second setting = %+v", settings[1])
	}
}

// TestUpsertOrgToolSetting exercises PUT /api/admin/orgs/{orgId}/tool-settings/
// {toolName}: request {enabled, config} and the decoded setting response.
func TestUpsertOrgToolSetting(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody UpsertOrgToolSettingInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"ts1","orgId":"o1","toolName":"web_search","enabled":true,"config":{"maxResults":5},"createdAt":"2024-01-01T00:00:00Z","updatedAt":"2024-01-02T00:00:00Z"}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	in := UpsertOrgToolSettingInput{
		Enabled: true,
		Config:  map[string]any{"maxResults": 5},
	}
	s, err := m.UpsertOrgToolSetting(context.Background(), "o1", "web_search", in)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/admin/orgs/o1/tool-settings/web_search" || gotMethod != http.MethodPut {
		t.Errorf("request = %s %s, want PUT /api/admin/orgs/o1/tool-settings/web_search", gotMethod, gotPath)
	}
	if !gotBody.Enabled || gotBody.Config["maxResults"] != float64(5) {
		t.Errorf("body = %+v", gotBody)
	}
	if s.ID != "ts1" || s.OrgID != "o1" || s.ToolName != "web_search" || !s.Enabled || s.Config["maxResults"] != float64(5) {
		t.Errorf("setting = %+v", s)
	}
}

// TestDeleteOrgToolSetting exercises DELETE /api/admin/orgs/{orgId}/tool-settings/
// {toolName}.
func TestDeleteOrgToolSetting(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.DeleteOrgToolSetting(context.Background(), "o1", "web_search"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/admin/orgs/o1/tool-settings/web_search" || gotMethod != http.MethodDelete {
		t.Errorf("request = %s %s, want DELETE /api/admin/orgs/o1/tool-settings/web_search", gotMethod, gotPath)
	}
}

// --- graph object update / relationship create / FTS search client methods ---

// TestUpdateObject exercises PATCH /api/graph/objects/{id}: delta-merge body
// (key/status/labels/properties) and decoding the returned versioned object
// (including its labels array).
func TestUpdateObject(t *testing.T) {
	key := "sam-lee"
	status := "confirmed"
	branchID := "b1"
	var gotPath, gotMethod, gotAuth, gotProj string
	var gotBody UpdateObjectRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotProj = r.Header.Get("X-Project-ID")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"obj-v2","canonical_id":"c1","type":"person","key":"sam-lee","status":"confirmed","labels":["contact","colleague"],"properties":{"first_name":"Sam"},"created_at":"2026-09-04T10:00:00Z"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok123", "proj")
	obj, err := m.UpdateObject(context.Background(), "obj-v1", &UpdateObjectRequest{
		Key:        &key,
		Status:     &status,
		Labels:     []string{"contact", "colleague"},
		Properties: map[string]any{"first_name": "Sam"},
		BranchID:   &branchID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/graph/objects/obj-v1" || gotMethod != http.MethodPatch {
		t.Errorf("request = %s %s, want PATCH /api/graph/objects/obj-v1", gotMethod, gotPath)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotProj != "proj" {
		t.Errorf("X-Project-ID = %q, want proj", gotProj)
	}
	if gotBody.Key == nil || *gotBody.Key != key {
		t.Errorf("body.key = %v, want %q", gotBody.Key, key)
	}
	if gotBody.Status == nil || *gotBody.Status != status {
		t.Errorf("body.status = %v, want %q", gotBody.Status, status)
	}
	if len(gotBody.Labels) != 2 || gotBody.Labels[0] != "contact" || gotBody.Labels[1] != "colleague" {
		t.Errorf("body.labels = %v", gotBody.Labels)
	}
	if gotBody.Properties["first_name"] != "Sam" {
		t.Errorf("body.properties = %v", gotBody.Properties)
	}
	if gotBody.BranchID == nil || *gotBody.BranchID != branchID {
		t.Errorf("body.branch_id = %v, want %q", gotBody.BranchID, branchID)
	}
	if obj == nil || obj.ID != "obj-v2" {
		t.Fatalf("object = %+v, want new id obj-v2", obj)
	}
	if len(obj.Labels) != 2 || obj.Labels[0] != "contact" {
		t.Errorf("labels = %v", obj.Labels)
	}
}

// TestUpdateObjectError surfaces non-2xx responses as errors.
func TestUpdateObjectError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"bad_request","message":"nope"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, err := m.UpdateObject(context.Background(), "o1", &UpdateObjectRequest{}); err == nil {
		t.Fatal("want error, got nil")
	}
}

// TestCreateRelationship exercises POST /api/graph/relationships with
// type/src_id/dst_id in the body.
func TestCreateRelationship(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody CreateRelationshipRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	err := m.CreateRelationship(context.Background(), &CreateRelationshipRequest{
		Type:       "assigned_to",
		SrcID:      "obj-1",
		DstID:      "obj-2",
		Properties: map[string]any{"since": "2026"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/graph/relationships" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/graph/relationships", gotMethod, gotPath)
	}
	if gotBody.Type != "assigned_to" || gotBody.SrcID != "obj-1" || gotBody.DstID != "obj-2" {
		t.Errorf("body = %+v", gotBody)
	}
	if gotBody.Properties["since"] != "2026" {
		t.Errorf("body.properties = %v", gotBody.Properties)
	}
}

// TestCreateRelationshipError surfaces non-2xx responses as errors.
func TestCreateRelationshipError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":{"code":"conflict","message":"exists"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.CreateRelationship(context.Background(), &CreateRelationshipRequest{Type: "t", SrcID: "a", DstID: "b"}); err == nil {
		t.Fatal("want error, got nil")
	}
}

// TestCreateObject exercises POST /api/graph/objects: type/key/status/labels/
// properties in the body and decoding the 201 GraphObject response.
func TestCreateObject(t *testing.T) {
	key := "sam-lee"
	status := "confirmed"
	var gotPath, gotMethod, gotAuth, gotProj string
	var gotBody CreateObjectRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotProj = r.Header.Get("X-Project-ID")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"obj-new","canonical_id":"c1","type":"person","key":"sam-lee","status":"confirmed","labels":["contact"],"properties":{"first_name":"Sam"},"created_at":"2026-09-04T10:00:00Z"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok123", "proj")
	obj, err := m.CreateObject(context.Background(), &CreateObjectRequest{
		Type:       "person",
		Key:        &key,
		Status:     &status,
		Labels:     []string{"contact"},
		Properties: map[string]any{"first_name": "Sam"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/graph/objects" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/graph/objects", gotMethod, gotPath)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotProj != "proj" {
		t.Errorf("X-Project-ID = %q, want proj", gotProj)
	}
	if gotBody.Type != "person" {
		t.Errorf("body.type = %q, want person", gotBody.Type)
	}
	if gotBody.Key == nil || *gotBody.Key != key {
		t.Errorf("body.key = %v, want %q", gotBody.Key, key)
	}
	if gotBody.Status == nil || *gotBody.Status != status {
		t.Errorf("body.status = %v, want %q", gotBody.Status, status)
	}
	if len(gotBody.Labels) != 1 || gotBody.Labels[0] != "contact" {
		t.Errorf("body.labels = %v", gotBody.Labels)
	}
	if gotBody.Properties["first_name"] != "Sam" {
		t.Errorf("body.properties = %v", gotBody.Properties)
	}
	if obj == nil || obj.ID != "obj-new" {
		t.Fatalf("object = %+v, want new id obj-new", obj)
	}
	if len(obj.Labels) != 1 || obj.Labels[0] != "contact" {
		t.Errorf("labels = %v", obj.Labels)
	}
}

// TestCreateObjectError surfaces non-2xx responses as errors.
func TestCreateObjectError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"bad_request","message":"nope"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, err := m.CreateObject(context.Background(), &CreateObjectRequest{Type: "person"}); err == nil {
		t.Fatal("want error, got nil")
	}
}

// TestSearchObjectsFTS exercises GET /api/graph/objects/fts: q and (when set)
// types query params, and parsing the {"data":[{"object":{...}}]} envelope.
func TestSearchObjectsFTS(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[
			{"object":{"id":"o1","canonical_id":"c1","type":"person","key":"sam-lee","status":"confirmed","labels":["contact"],"properties":{"first_name":"Sam"},"created_at":"2026-09-04T10:00:00Z"}},
			{"object":{"id":"o2","canonical_id":"c2","type":"task","key":"call dentist","status":"suggested"}}
		]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	out, err := m.SearchObjectsFTS(context.Background(), "sam lee", "person")
	if err != nil {
		t.Fatal(err)
	}
	if gotURI != "/api/graph/objects/fts?limit=50&q=sam+lee&types=person" {
		t.Errorf("uri = %q", gotURI)
	}
	if len(out) != 2 {
		t.Fatalf("got %d objects, want 2: %+v", len(out), out)
	}
	if out[0].ID != "o1" || out[0].Type != "person" || out[0].Key != "sam-lee" {
		t.Errorf("object0 = %+v", out[0])
	}
	if len(out[0].Labels) != 1 || out[0].Labels[0] != "contact" {
		t.Errorf("object0 labels = %v", out[0].Labels)
	}
	if out[1].ID != "o2" || out[1].Key != "call dentist" {
		t.Errorf("object1 = %+v", out[1])
	}
}

// TestSearchObjectsFTSNoTypeFilter omits the types param when typeFilter is
// empty.
func TestSearchObjectsFTSNoTypeFilter(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	out, err := m.SearchObjectsFTS(context.Background(), "sam", "")
	if err != nil {
		t.Fatal(err)
	}
	if gotURI != "/api/graph/objects/fts?limit=50&q=sam" {
		t.Errorf("uri = %q", gotURI)
	}
	if len(out) != 0 {
		t.Errorf("objects = %+v, want empty", out)
	}
}

// TestGraphObjectParsesLabels asserts the labels array decodes from JSON.
func TestGraphObjectParsesLabels(t *testing.T) {
	var obj GraphObject
	if err := json.Unmarshal([]byte(`{"id":"o1","type":"person","labels":["contact","colleague"],"properties":{"first_name":"Sam"}}`), &obj); err != nil {
		t.Fatal(err)
	}
	if obj.ID != "o1" || obj.Type != "person" {
		t.Errorf("object = %+v", obj)
	}
	if len(obj.Labels) != 2 || obj.Labels[0] != "contact" || obj.Labels[1] != "colleague" {
		t.Errorf("labels = %v", obj.Labels)
	}
}

// TestChatRequestMarshalsCanonicalID asserts canonicalId is emitted in the
// chat request body.
func TestChatRequestMarshalsCanonicalID(t *testing.T) {
	b, err := json.Marshal(ChatRequest{Message: "hi", CanonicalID: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"canonicalId":"c1"`) {
		t.Errorf("body = %s, want canonicalId present", b)
	}
}

// TestCreateObjectConversation exercises POST /api/chat/conversations: the
// body carries title/message/canonicalId and the response id is returned.
func TestCreateObjectConversation(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"conv-42"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	id, err := m.CreateObjectConversation(context.Background(), "canon-1", "sam-lee", "Help me work with this object")
	if err != nil {
		t.Fatal(err)
	}
	if id != "conv-42" {
		t.Errorf("id = %q, want conv-42", id)
	}
	if gotPath != "/api/chat/conversations" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/chat/conversations", gotMethod, gotPath)
	}
	if gotBody["title"] != "sam-lee" || gotBody["message"] != "Help me work with this object" || gotBody["canonicalId"] != "canon-1" {
		t.Errorf("body = %v, want title/message/canonicalId", gotBody)
	}
}

// TestCreateObjectConversationError surfaces a non-2xx response as an error.
func TestCreateObjectConversationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":"boom","message":"nope"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, err := m.CreateObjectConversation(context.Background(), "canon-1", "sam-lee", "hi"); err == nil {
		t.Fatal("expected error, got nil")
	}
}
