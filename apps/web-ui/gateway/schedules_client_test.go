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

// --- ScheduledAgent DTO round-trips ---

func TestScheduledAgentJSONRoundTrip(t *testing.T) {
	prompt := "Summarize yesterday's notes"
	desc := "daily digest"
	defID := "def-1"
	lastRun := "2026-08-26T08:00:00Z"
	status := "completed"
	in := ScheduledAgent{
		ID:                  "a1",
		ProjectID:           "p1",
		Name:                "Daily briefing",
		StrategyType:        "definition",
		Prompt:              &prompt,
		CronSchedule:        "0 0 8 * * *",
		Enabled:             true,
		TriggerType:         "schedule",
		Description:         &desc,
		LastRunAt:           &lastRun,
		LastRunStatus:       &status,
		ConsecutiveFailures: 2,
		AgentDefinitionID:   &defID,
		CreatedAt:           "2026-08-25T10:00:00Z",
		UpdatedAt:           "2026-08-26T07:00:00Z",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out ScheduledAgent
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	// Pointer fields must round-trip by pointee (addresses differ across
	// marshal/unmarshal).
	if out.ID != in.ID || out.ProjectID != in.ProjectID || out.Name != in.Name || out.StrategyType != in.StrategyType {
		t.Errorf("scalar mismatch: got %+v, want %+v", out, in)
	}
	if out.CronSchedule != in.CronSchedule || out.Enabled != in.Enabled || out.TriggerType != in.TriggerType {
		t.Errorf("scalar mismatch: got %+v, want %+v", out, in)
	}
	if out.ConsecutiveFailures != in.ConsecutiveFailures || out.CreatedAt != in.CreatedAt || out.UpdatedAt != in.UpdatedAt {
		t.Errorf("scalar mismatch: got %+v, want %+v", out, in)
	}
	strPtr := func(name string, got, want *string) {
		t.Helper()
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	strPtr("prompt", out.Prompt, in.Prompt)
	strPtr("description", out.Description, in.Description)
	strPtr("lastRunAt", out.LastRunAt, in.LastRunAt)
	strPtr("lastRunStatus", out.LastRunStatus, in.LastRunStatus)
	strPtr("agentDefinitionId", out.AgentDefinitionID, in.AgentDefinitionID)
	// camelCase wire names (spot check)
	wire := string(b)
	for _, want := range []string{`"strategyType":"definition"`, `"cronSchedule":"0 0 8 * * *"`, `"agentDefinitionId":"def-1"`, `"consecutiveFailures":2`} {
		if !strings.Contains(wire, want) {
			t.Errorf("wire JSON missing %q: %s", want, wire)
		}
	}
}

func TestScheduledAgentRunJSONRoundTrip(t *testing.T) {
	completed := "2026-08-26T08:02:00Z"
	msg := "boom"
	src := "schedule"
	in := ScheduledAgentRun{
		ID:            "r1",
		AgentID:       "a1",
		Status:        "failed",
		StartedAt:     "2026-08-26T08:00:00Z",
		CompletedAt:   &completed,
		Summary:       map[string]any{"summary": "digest ready"},
		ErrorMessage:  &msg,
		TriggerSource: &src,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out ScheduledAgentRun
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != in.ID || out.AgentID != in.AgentID || out.Status != in.Status || out.StartedAt != in.StartedAt {
		t.Errorf("round-trip mismatch: got %+v, want %+v", out, in)
	}
	if out.CompletedAt == nil || *out.CompletedAt != completed {
		t.Errorf("completedAt = %v", out.CompletedAt)
	}
	if out.ErrorMessage == nil || *out.ErrorMessage != msg {
		t.Errorf("errorMessage = %v", out.ErrorMessage)
	}
	if out.TriggerSource == nil || *out.TriggerSource != src {
		t.Errorf("triggerSource = %v", out.TriggerSource)
	}
	if out.Summary["summary"] != "digest ready" {
		t.Errorf("summary = %v", out.Summary)
	}
}

func TestTriggerResultJSONRoundTrip(t *testing.T) {
	runID := "run-1"
	in := TriggerResult{Success: true, RunID: &runID}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out TriggerResult
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Success || out.RunID == nil || *out.RunID != "run-1" {
		t.Errorf("trigger round-trip = %+v", out)
	}
}

// --- MemoryClient scheduled-agent methods ---

// schedTestServer spins up an httptest server asserting the expected
// method+path per request and serving the canned body. Returns the client.
func schedTestServer(t *testing.T, method, path, body string) (*MemoryClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			t.Errorf("method = %s, want %s", r.Method, method)
		}
		if r.URL.Path != path {
			t.Errorf("path = %s, want %s", r.URL.Path, path)
		}
		if r.Header.Get("Authorization") != "Bearer tok123" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return NewMemoryClient(srv.URL, "tok123", "proj"), srv
}

func TestListScheduledAgentsClient(t *testing.T) {
	m, _ := schedTestServer(t, http.MethodGet, "/api/projects/proj/agents",
		`{"success":true,"data":[{"id":"a1","name":"Daily briefing","strategyType":"definition","cronSchedule":"0 0 8 * * *","enabled":true,"triggerType":"schedule"}]}`)
	agents, err := m.ListScheduledAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].ID != "a1" || agents[0].Name != "Daily briefing" || !agents[0].Enabled {
		t.Errorf("agents = %+v", agents)
	}
}

func TestGetScheduledAgentClient(t *testing.T) {
	m, _ := schedTestServer(t, http.MethodGet, "/api/projects/proj/agents/a1",
		`{"success":true,"data":{"id":"a1","name":"Daily briefing","enabled":false,"cronSchedule":"0 0 8 * * *"}}`)
	a, err := m.GetScheduledAgent(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if a == nil || a.ID != "a1" || a.Enabled {
		t.Errorf("agent = %+v", a)
	}
}

func TestCreateScheduledAgentClient(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/projects/proj/agents" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"id":"a9","name":"Daily briefing"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok123", "proj")

	prompt := "run it"
	in := &ScheduledAgent{Name: "Daily briefing", StrategyType: "definition", Prompt: &prompt, CronSchedule: "0 0 8 * * *", Enabled: true, TriggerType: "schedule"}
	a, err := m.CreateScheduledAgent(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if a == nil || a.ID != "a9" {
		t.Errorf("agent = %+v", a)
	}
	// projectId is forced by the client; the other fields pass through.
	if gotBody["projectId"] != "proj" {
		t.Errorf("body projectId = %v, want proj", gotBody["projectId"])
	}
	if gotBody["name"] != "Daily briefing" || gotBody["cronSchedule"] != "0 0 8 * * *" || gotBody["strategyType"] != "definition" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestUpdateScheduledAgentClient(t *testing.T) {
	m, _ := schedTestServer(t, http.MethodPatch, "/api/projects/proj/agents/a1",
		`{"success":true,"data":{"id":"a1","name":"Renamed"}}`)
	in := &ScheduledAgent{Name: "Renamed", CronSchedule: "0 0 8 * * *"}
	a, err := m.UpdateScheduledAgent(context.Background(), "a1", in)
	if err != nil {
		t.Fatal(err)
	}
	if a == nil || a.Name != "Renamed" {
		t.Errorf("agent = %+v", a)
	}
}

func TestEnableScheduledAgentClient(t *testing.T) {
	m, _ := schedTestServer(t, http.MethodPost, "/api/projects/proj/agents/a1/enable",
		`{"success":true,"data":{"id":"a1","name":"Daily briefing","enabled":true}}`)
	a, err := m.EnableScheduledAgent(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if a == nil || !a.Enabled {
		t.Errorf("agent = %+v", a)
	}
}

func TestSetScheduledAgentEnabledClient(t *testing.T) {
	m, _ := schedTestServer(t, http.MethodPatch, "/api/projects/proj/agents/a1",
		`{"success":true,"data":{"id":"a1","name":"Daily briefing","enabled":false}}`)
	a, err := m.SetScheduledAgentEnabled(context.Background(), "a1", false)
	if err != nil {
		t.Fatal(err)
	}
	if a == nil || a.Enabled {
		t.Errorf("agent = %+v", a)
	}
}

func TestDeleteScheduledAgentClient(t *testing.T) {
	m, _ := schedTestServer(t, http.MethodDelete, "/api/projects/proj/agents/a1", `{"success":true}`)
	if err := m.DeleteScheduledAgent(context.Background(), "a1"); err != nil {
		t.Fatal(err)
	}
}

func TestTriggerScheduledAgentClient(t *testing.T) {
	m, _ := schedTestServer(t, http.MethodPost, "/api/projects/proj/agents/a1/trigger",
		`{"success":true,"runId":"run-42","message":"queued"}`)
	res, err := m.TriggerScheduledAgent(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.Success || res.RunID == nil || *res.RunID != "run-42" {
		t.Errorf("trigger = %+v", res)
	}
}

func TestTriggerScheduledAgentFailure(t *testing.T) {
	m, _ := schedTestServer(t, http.MethodPost, "/api/projects/proj/agents/a1/trigger",
		`{"success":false,"error":"agent disabled"}`)
	res, err := m.TriggerScheduledAgent(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || res.Error == nil || *res.Error != "agent disabled" {
		t.Errorf("trigger = %+v", res)
	}
}

// TestGetRunQuestionsClient verifies GetRunQuestions hits the project-scoped
// run questions endpoint and parses the {success,data} envelope, including
// option lists and the nil-vs-set response distinction.
func TestGetRunQuestionsClient(t *testing.T) {
	m, _ := schedTestServer(t, http.MethodGet, "/api/projects/proj/agent-runs/run-42/questions",
		`{"success":true,"data":[
			{"id":"q1","runId":"run-42","agentId":"a1","projectId":"proj","question":"Approve the deploy?","options":[{"label":"Yes","value":"yes","description":"go ahead"},{"label":"No","value":"no"}],"interactionType":"buttons","status":"pending","createdAt":"2026-08-26T08:00:01Z"},
			{"id":"q2","runId":"run-42","agentId":"a1","projectId":"proj","question":"Confirm details","options":[],"interactionType":"text","response":"ok","status":"answered","resumeRunId":"run-43","createdAt":"2026-08-26T08:00:02Z"}
		]}`)
	qs, err := m.GetRunQuestions(context.Background(), "run-42")
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 2 {
		t.Fatalf("questions = %d, want 2", len(qs))
	}
	q := qs[0]
	if q.ID != "q1" || q.RunID != "run-42" || q.Question != "Approve the deploy?" || q.Status != "pending" {
		t.Errorf("question[0] = %+v", q)
	}
	if q.Response != nil {
		t.Errorf("question[0].response = %v, want nil (pending)", q.Response)
	}
	if len(q.Options) != 2 || q.Options[0].Label != "Yes" || q.Options[0].Value != "yes" || q.Options[0].Description != "go ahead" || q.Options[1].Label != "No" {
		t.Errorf("question[0].options = %+v", q.Options)
	}
	q2 := qs[1]
	if q2.Response == nil || *q2.Response != "ok" {
		t.Errorf("question[1].response = %v, want ok", q2.Response)
	}
	if q2.ResumeRunID == nil || *q2.ResumeRunID != "run-43" {
		t.Errorf("question[1].resumeRunId = %v", q2.ResumeRunID)
	}
}

func TestListScheduledAgentRunsClient(t *testing.T) {
	m, _ := schedTestServer(t, http.MethodGet, "/api/projects/proj/agents/a1/runs",
		`{"success":true,"data":[{"id":"r1","agentId":"a1","status":"completed","startedAt":"2026-08-26T08:00:00Z","summary":{"summary":"digest"}}]}`)
	runs, err := m.ListScheduledAgentRuns(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != "r1" || runs[0].Status != "completed" {
		t.Errorf("runs = %+v", runs)
	}
	if runs[0].Summary["summary"] != "digest" {
		t.Errorf("run summary = %v", runs[0].Summary)
	}
}

// TestGetRunFullClient verifies GetRunFull hits the project-scoped run
// transcript endpoint and parses the {success,data} envelope.
func TestGetRunFullClient(t *testing.T) {
	completed := "2026-08-26T08:02:00Z"
	m, _ := schedTestServer(t, http.MethodGet, "/api/projects/proj/agent-runs/run-42/full",
		`{"success":true,"data":{
			"run":{"id":"run-42","agentId":"a1","agentName":"Daily briefing","status":"failed","startedAt":"2026-08-26T08:00:00Z","completedAt":"2026-08-26T08:02:00Z","summary":{"summary":"digest"},"errorMessage":"boom"},
			"messages":[
				{"id":"m1","runId":"run-42","role":"user","content":{"text":"hello"},"stepNumber":1,"createdAt":"2026-08-26T08:00:01Z"},
				{"id":"m2","runId":"run-42","role":"assistant","content":{"text":"hi there","function_calls":[{"name":"web_search"}]},"stepNumber":1,"createdAt":"2026-08-26T08:00:02Z"}
			],
			"toolCalls":[
				{"id":"t1","runId":"run-42","toolName":"web_search","input":{"q":"x"},"output":{"result":"hits"},"status":"completed","stepNumber":1,"createdAt":"2026-08-26T08:00:03Z"}
			]
		}}`)
	full, err := m.GetRunFull(context.Background(), "run-42")
	if err != nil {
		t.Fatal(err)
	}
	if full == nil || full.Run == nil {
		t.Fatal("run = nil")
	}
	r := full.Run
	if r.ID != "run-42" || r.AgentName != "Daily briefing" || r.Status != "failed" {
		t.Errorf("run = %+v", r)
	}
	if r.ErrorMessage == nil || *r.ErrorMessage != "boom" {
		t.Errorf("errorMessage = %v", r.ErrorMessage)
	}
	if r.CompletedAt == nil || *r.CompletedAt != completed {
		t.Errorf("completedAt = %v", r.CompletedAt)
	}
	if len(full.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(full.Messages))
	}
	if full.Messages[0].Role != "user" || full.Messages[0].Content["text"] != "hello" {
		t.Errorf("message[0] = %+v", full.Messages[0])
	}
	if len(full.ToolCalls) != 1 || full.ToolCalls[0].ToolName != "web_search" || full.ToolCalls[0].Status != "completed" {
		t.Errorf("toolCalls = %+v", full.ToolCalls)
	}
	if full.ToolCalls[0].Output["result"] != "hits" {
		t.Errorf("tool output = %v", full.ToolCalls[0].Output)
	}
}
