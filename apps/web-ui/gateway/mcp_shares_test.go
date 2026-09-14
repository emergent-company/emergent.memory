package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMCPShareInstanceJSONRoundTrip verifies the instance model carries every
// field the UI needs, including the nullable tools/agents arrays and the
// nullable lastUsedAt.
func TestMCPShareInstanceJSONRoundTrip(t *testing.T) {
	raw := `{
		"id": "s1",
		"name": "research",
		"description": "read-only research access",
		"tools": ["search_memory", "get_object"],
		"agents": ["a1"],
		"status": "active",
		"isLegacy": false,
		"createdAt": "2026-01-02T03:04:05Z",
		"lastUsedAt": "2026-01-03T00:00:00Z",
		"toolCount": 2,
		"agentCount": 1
	}`
	var inst MCPShareInstance
	if err := json.Unmarshal([]byte(raw), &inst); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if inst.ID != "s1" || inst.Name != "research" || inst.Description != "read-only research access" {
		t.Errorf("identity = %+v", inst)
	}
	if len(inst.Tools) != 2 || inst.Tools[0] != "search_memory" {
		t.Errorf("tools = %v", inst.Tools)
	}
	if len(inst.Agents) != 1 || inst.Agents[0] != "a1" {
		t.Errorf("agents = %v", inst.Agents)
	}
	if inst.Status != "active" || inst.IsLegacy || inst.ToolCount != 2 || inst.AgentCount != 1 {
		t.Errorf("flags/counts = %+v", inst)
	}
	if inst.CreatedAt != "2026-01-02T03:04:05Z" || inst.LastUsedAt == nil || *inst.LastUsedAt != "2026-01-03T00:00:00Z" {
		t.Errorf("timestamps = %+v", inst)
	}

	// nullable arrays + lastUsedAt survive a null round-trip
	var nullish MCPShareInstance
	if err := json.Unmarshal([]byte(`{"id":"s2","tools":null,"agents":null,"lastUsedAt":null}`), &nullish); err != nil {
		t.Fatalf("null unmarshal: %v", err)
	}
	if nullish.Tools != nil || nullish.Agents != nil || nullish.LastUsedAt != nil {
		t.Errorf("null fields should decode to nil, got %+v", nullish)
	}

	out, err := json.Marshal(inst)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"toolCount":2`) || !strings.Contains(string(out), `"isLegacy":false`) {
		t.Errorf("marshal missing fields: %s", out)
	}
}

// TestMemoryClientListMCPShareInstances asserts the path, method, auth header
// and envelope decoding.
func TestMemoryClientListMCPShareInstances(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":[{"id":"s1","name":"alpha","tools":["a"],"agents":null,"toolCount":1}]}`)
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "tok", "proj-1")
	shares, err := m.ListMCPShareInstances(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/projects/proj-1/mcp/shares" {
		t.Errorf("request = %s %s, want GET /api/projects/proj-1/mcp/shares", gotMethod, gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("auth = %q, want Bearer tok", gotAuth)
	}
	if len(shares) != 1 || shares[0].Name != "alpha" || len(shares[0].Tools) != 1 {
		t.Errorf("shares = %+v", shares)
	}
}

// TestMemoryClientMCPShareDecodeTolerance covers plain JSON (no envelope) and
// the {"shares":[...]} wrapper.
func TestMemoryClientMCPShareDecodeTolerance(t *testing.T) {
	for name, body := range map[string]string{
		"plain-array": `[{"id":"s1","name":"alpha"}]`,
		"shares-key":  `{"shares":[{"id":"s1","name":"alpha"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, body)
			}))
			defer ts.Close()
			m := NewMemoryClient(ts.URL, "tok", "proj-1")
			shares, err := m.ListMCPShareInstances(t.Context())
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(shares) != 1 || shares[0].Name != "alpha" {
				t.Errorf("shares = %+v", shares)
			}
		})
	}
}

// TestMemoryClientCreateMCPShareInstance asserts the POST path/body and that
// the one-time secret decodes.
func TestMemoryClientCreateMCPShareInstance(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, `{"success":true,"data":{"id":"s9","name":"alpha","tools":["a","b"],"token":"emt_secret","mcpUrl":"https://mem.example/mcp","snippets":{"cursor":"x"}}}`)
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "tok", "proj-1")
	created, err := m.CreateMCPShareInstance(t.Context(), &MCPShareInput{
		Name: "alpha", Description: "d", Tools: []string{"a", "b"}, Agents: []string{"a1"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/projects/proj-1/mcp/shares" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if gotBody["name"] != "alpha" || gotBody["description"] != "d" {
		t.Errorf("body = %+v", gotBody)
	}
	if tools, _ := gotBody["tools"].([]any); len(tools) != 2 {
		t.Errorf("body tools = %+v", gotBody["tools"])
	}
	if created.Token != "emt_secret" || created.MCPURL != "https://mem.example/mcp" {
		t.Errorf("created = %+v", created)
	}
	if created.Name != "alpha" || created.ID != "s9" {
		t.Errorf("created instance = %+v", created.MCPShareInstance)
	}
}

// TestMemoryClientCreateMCPShareNestedInstance accepts an {"instance":{...}}
// payload shape too.
func TestMemoryClientCreateMCPShareNestedInstance(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"instance":{"id":"s9","name":"alpha"},"token":"emt_secret","mcpUrl":"https://mem.example/mcp"}`)
	}))
	defer ts.Close()
	m := NewMemoryClient(ts.URL, "tok", "proj-1")
	created, err := m.CreateMCPShareInstance(t.Context(), &MCPShareInput{Name: "alpha", Tools: []string{"a"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID != "s9" || created.Name != "alpha" || created.Token != "emt_secret" {
		t.Errorf("created = %+v", created)
	}
}

// TestMemoryClientMCPSharePaths asserts get/update/revoke/rotate/tools paths
// and methods.
func TestMemoryClientMCPSharePaths(t *testing.T) {
	type call struct {
		method string
		path   string
		body   string
	}
	var calls []call
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, call{r.Method, r.URL.Path, string(b)})
		_, _ = io.WriteString(w, `{"id":"s1","name":"alpha","token":"t","mcpUrl":"u"}`)
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "tok", "proj-1")
	ctx := t.Context()
	if _, err := m.GetMCPShareInstance(ctx, "s1"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, err := m.UpdateMCPShareInstance(ctx, "s1", &MCPShareInput{Name: "beta", Tools: []string{"a"}}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := m.RevokeMCPShareInstance(ctx, "s1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := m.RotateMCPShareInstance(ctx, "s1"); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if _, err := m.ListMCPShareTools(ctx); err != nil {
		t.Fatalf("tools: %v", err)
	}

	want := []call{
		{"GET", "/api/projects/proj-1/mcp/shares/s1", ""},
		{"PATCH", "/api/projects/proj-1/mcp/shares/s1", `{"name":"beta","tools":["a"],"agents":null}`},
		{"DELETE", "/api/projects/proj-1/mcp/shares/s1", ""},
		{"POST", "/api/projects/proj-1/mcp/shares/s1/rotate", ""},
		{"GET", "/api/projects/proj-1/mcp/tools", ""},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %+v", calls)
	}
	for i, w := range want {
		if calls[i].method != w.method || calls[i].path != w.path {
			t.Errorf("call %d = %s %s, want %s %s", i, calls[i].method, calls[i].path, w.method, w.path)
		}
	}
}

// TestMemoryClientListMCPShareTools decodes the catalog.
func TestMemoryClientListMCPShareTools(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"success":true,"data":[{"name":"search_memory","description":"d","requiredScope":"memory:read","category":"Memory"}]}`)
	}))
	defer ts.Close()
	m := NewMemoryClient(ts.URL, "tok", "proj-1")
	tools, err := m.ListMCPShareTools(t.Context())
	if err != nil {
		t.Fatalf("tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "search_memory" || tools[0].Category != "Memory" || tools[0].RequiredScope != "memory:read" {
		t.Errorf("tools = %+v", tools)
	}
}

// TestMemoryClientListMCPShareInstancesError surfaces a non-2xx as an error.
func TestMemoryClientListMCPShareInstancesError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":{"code":"conflict","message":"name taken"}}`)
	}))
	defer ts.Close()
	m := NewMemoryClient(ts.URL, "tok", "proj-1")
	if _, err := m.ListMCPShareInstances(t.Context()); err == nil {
		t.Fatal("expected an error for a 409 response")
	}
}
