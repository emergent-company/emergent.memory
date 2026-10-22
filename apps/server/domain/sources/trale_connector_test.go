package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeMCP is a fake Trale MCP server over streamable HTTP. It speaks the
// initialize / notifications / tools/list / tools/call methods and can answer
// with application/json or text/event-stream bodies.
type fakeMCP struct {
	mu       sync.Mutex
	meetings []traleMeeting
	detail   map[string]traleMeetingDetail
	tags     []traleTag
	tools    []string

	sseList  bool
	sseFetch bool

	listArgSets []map[string]any
	fetchIDs    []string
	authHeaders []string
}

func newFakeMCP(t *testing.T) (*fakeMCP, *httptest.Server) {
	t.Helper()
	f := &fakeMCP{
		detail: map[string]traleMeetingDetail{},
		tools:  []string{"list_transcriptions", "fetch", "list_tags"},
	}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeMCP) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.authHeaders = append(f.authHeaders, r.Header.Get("Authorization"))
	session := r.Header.Get("Mcp-Session-Id")
	f.mu.Unlock()

	// Echo the session id so the client learns and replays it.
	if session != "" {
		w.Header().Set("Mcp-Session-Id", session)
	} else {
		w.Header().Set("Mcp-Session-Id", "sess-1")
	}

	var msg struct {
		JSONRPC string `json:"jsonrpc"`
		ID      any    `json:"id"`
		Method  string `json:"method"`
		Params  struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	switch msg.Method {
	case "initialize":
		writeResult(w, msg.ID, map[string]any{
			"protocolVersion": "2025-11-25",
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "trale", "version": "1.0"},
		}, false)
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		tools := make([]any, 0, len(f.tools))
		for _, name := range f.tools {
			tools = append(tools, map[string]any{"name": name})
		}
		writeResult(w, msg.ID, map[string]any{"tools": tools}, false)
	case "tools/call":
		f.handleToolCall(w, msg.ID, msg.Params.Name, msg.Params.Arguments)
	default:
		http.Error(w, "unknown method: "+msg.Method, http.StatusBadRequest)
	}
}

func (f *fakeMCP) handleToolCall(w http.ResponseWriter, id any, name string, args map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch name {
	case "list_transcriptions":
		f.listArgSets = append(f.listArgSets, args)
		// Pagination is driven by the last set's has_next; here we just return
		// the whole configured list with has_next=false. Paged tests use a
		// dedicated server.
		writeToolResult(w, id, map[string]any{
			"meetings":   f.meetings,
			"pagination": map[string]any{"page": 1, "limit": 25, "has_next": false},
		}, f.sseList)
	case "fetch":
		meetingID, _ := args["id"].(string)
		f.fetchIDs = append(f.fetchIDs, meetingID)
		detail, ok := f.detail[meetingID]
		if !ok {
			writeErrorResult(w, id, "not found", f.sseFetch)
			return
		}
		writeToolResult(w, id, detail, f.sseFetch)
	case "list_tags":
		writeToolResult(w, id, map[string]any{"tags": f.tags}, false)
	default:
		writeErrorResult(w, id, "unknown tool", false)
	}
}

// writeToolResult wraps a structuredContent object in a full CallToolResult
// (content array + structuredContent), as required by the MCP client parser.
func writeToolResult(w http.ResponseWriter, id any, structuredContent any, sse bool) {
	encoded, _ := json.Marshal(structuredContent)
	writeResult(w, id, map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": string(encoded)}},
		"structuredContent": structuredContent,
	}, sse)
}

func writeErrorResult(w http.ResponseWriter, id any, msg string, sse bool) {
	writeResult(w, id, map[string]any{
		"content": []any{map[string]any{"type": "text", "text": msg}},
		"isError": true,
	}, sse)
}

// writeResult writes a JSON-RPC result as application/json or SSE.
func writeResult(w http.ResponseWriter, id any, result map[string]any, sse bool) {
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	if sse {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload)
}

func newTestConnector(f *fakeMCP, srv *httptest.Server, cfg traleSyncConfig) *traleConnector {
	c := newTraleConnector(traleCredentials{
		accessToken:   "at",
		refreshToken:  "rt",
		clientID:      "client-123",
		tokenEndpoint: srv.URL + "/token",
		resource:      srv.URL,
	}, srv.Client())
	c.config = cfg
	return c
}

func tagObj(slug string) map[string]any { return map[string]any{"slug": slug, "name": slug} }

func TestTraleConnector_Enumerate_PaginationAndFilters(t *testing.T) {
	f, srv := newFakeMCP(t)
	f.meetings = []traleMeeting{
		{ID: "m1", Title: "Standup 2026-10-22", Summary: "s1", Date: "2026-10-22T09:00:00Z", Status: "completed", ParticipantCount: 3, Tags: []any{tagObj("standup"), tagObj("eng")}},
		{ID: "m2", Title: "Roadmap review", Summary: "s2", Date: "2026-10-21T10:00:00Z", Status: "completed", ParticipantCount: 5, Tags: []any{tagObj("roadmap")}},
		{ID: "m3", Title: "Still processing", Summary: "s3", Date: "2026-10-20T10:00:00Z", Status: "processing", ParticipantCount: 2, Tags: []any{tagObj("standup")}},
	}

	conn := newTestConnector(f, srv, traleSyncConfig{tags: []string{"standup"}, backfillDays: 90})
	items, err := conn.Enumerate(context.Background(), map[string]any{
		"tags":          []string{"standup"},
		"backfill_days": 90,
	})
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}

	// m3 is "processing" → skipped. m2 lacks the "standup" tag? No — tags filter
	// is server-side passthrough; here the server returned all meetings, so the
	// client only skips non-completed and applies name_pattern. So we expect m1
	// and m2 (both completed), m3 skipped.
	if len(items) != 2 {
		t.Fatalf("Enumerate() returned %d items, want 2 (processing skipped): %+v", len(items), items)
	}
	if items[0].ID != "m1" || items[1].ID != "m2" {
		t.Errorf("Enumerate() ids = %v, want [m1 m2]", []string{items[0].ID, items[1].ID})
	}

	// Tag passthrough: the tags config is forwarded verbatim to the tool call.
	if len(f.listArgSets) != 1 {
		t.Fatalf("list_transcriptions called %d times, want 1", len(f.listArgSets))
	}
	args := f.listArgSets[0]
	if args["limit"] != float64(25) {
		t.Errorf("limit = %v, want 25", args["limit"])
	}
	if tags, ok := args["tags"].([]any); !ok || len(tags) != 1 || tags[0] != "standup" {
		t.Errorf("tags = %v, want [standup] passthrough", args["tags"])
	}
	if _, ok := args["dateFrom"].(string); !ok {
		t.Errorf("dateFrom missing or wrong type: %v", args["dateFrom"])
	}
	if _, ok := args["dateTo"].(string); !ok {
		t.Errorf("dateTo missing or wrong type: %v", args["dateTo"])
	}

	// Revision stability + determinism.
	r1 := traleRevisionID(f.meetings[0])
	r2 := traleRevisionID(f.meetings[0])
	if r1 != r2 {
		t.Errorf("traleRevisionID(m1) not stable: %q vs %q", r1, r2)
	}
	if traleRevisionID(f.meetings[1]) == r1 {
		t.Error("traleRevisionID(m1) == traleRevisionID(m2), want distinct")
	}
}

func TestTraleConnector_Enumerate_PagesUntilHasNext(t *testing.T) {
	// A server that serves two pages via list_transcriptions (has_next true then
	// false) drives the client's pagination loop to completion.
	mux := newPagedMCP(t)
	conn := newTraleConnector(traleCredentials{accessToken: "at", resource: mux.URL}, mux.Client())
	conn.config = traleSyncConfig{tags: []string{"standup"}, backfillDays: 90}
	items, err := conn.Enumerate(context.Background(), map[string]any{"tags": []string{"standup"}})
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("Enumerate() returned %d items, want 3 (2 pages)", len(items))
	}
}

// newPagedMCP serves two pages of list_transcriptions (has_next true then false)
// and a no-op initialize/notifications/tools-list surface.
func newPagedMCP(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	page := 0
	pages := [][]traleMeeting{
		{
			{ID: "p1a", Title: "A", Status: "completed", Date: "2026-10-02T09:00:00Z"},
			{ID: "p1b", Title: "B", Status: "completed", Date: "2026-10-02T10:00:00Z"},
		},
		{
			{ID: "p2a", Title: "C", Status: "completed", Date: "2026-10-01T09:00:00Z"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg struct {
			JSONRPC string `json:"jsonrpc"`
			ID      any    `json:"id"`
			Method  string `json:"method"`
			Params  struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&msg)
		switch msg.Method {
		case "initialize":
			writeResult(w, msg.ID, map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "trale"}}, false)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			if msg.Params.Name != "list_transcriptions" {
				writeErrorResult(w, msg.ID, "unknown", false)
				return
			}
			mu.Lock()
			idx := page
			page++
			mu.Unlock()
			hasNext := idx < len(pages)-1
			writeToolResult(w, msg.ID, map[string]any{
				"meetings":   pages[idx],
				"pagination": map[string]any{"page": idx + 1, "limit": 25, "has_next": hasNext},
			}, false)
		default:
			writeResult(w, msg.ID, map[string]any{}, false)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTraleConnector_Enumerate_NamePattern(t *testing.T) {
	f, srv := newFakeMCP(t)
	f.meetings = []traleMeeting{
		{ID: "m1", Title: "Standup 2026-10-22", Status: "completed", Date: "2026-10-22T09:00:00Z"},
		{ID: "m2", Title: "Roadmap review", Status: "completed", Date: "2026-10-21T09:00:00Z"},
	}

	re := regexp.MustCompile(`(?i)standup`)
	conn := newTestConnector(f, srv, traleSyncConfig{tags: []string{"standup"}, backfillDays: 90, nameRegex: re})
	items, err := conn.Enumerate(context.Background(), map[string]any{
		"tags":         []string{"standup"},
		"name_pattern": "(?i)standup",
	})
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != "m1" {
		t.Fatalf("Enumerate() = %+v, want only [m1]", items)
	}
}

func TestTraleConnector_Fetch_JSON(t *testing.T) {
	f, srv := newFakeMCP(t)
	f.sseFetch = false
	f.detail["m1"] = traleMeetingDetail{
		ID:                "m1",
		Title:             "Standup 2026-10-22",
		Date:              "2026-10-22",
		Platform:          "Zoom",
		Status:            "completed",
		Summary:           "Team sync.",
		StructuredSummary: `{"agenda":"sync","next_steps":["ship"]}`,
		Content:           "Alice: hello\nBob: hi",
		Participants:      []any{map[string]any{"display_name": "Alice", "email": "alice@x.com", "org": "Acme"}, "Bob"},
		Tags:              []any{tagObj("standup")},
	}

	conn := newTestConnector(f, srv, traleSyncConfig{includeTranscript: true})
	content, mime, err := conn.Fetch(context.Background(), SourceItem{ID: "m1"})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if mime != traleMarkdownMimeType {
		t.Errorf("Fetch() mime = %q, want %q", mime, traleMarkdownMimeType)
	}
	for _, want := range []string{
		"# Standup 2026-10-22",
		"Team sync.",
		"```json",
		`"agenda": "sync"`,
		"## Transcript",
		"Alice: hello",
		"- Alice / alice@x.com / Acme",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("Fetch() markdown missing %q:\n%s", want, content)
		}
	}
}

func TestTraleConnector_Fetch_FenceEscapesBackticks(t *testing.T) {
	f, srv := newFakeMCP(t)
	f.detail["m1"] = traleMeetingDetail{
		ID:     "m1",
		Title:  "Fence",
		Status: "completed",
		// A structured summary whose content contains a fenced block: the outer
		// fence must be lengthened so the inner one cannot break out.
		StructuredSummary: "{\"note\":\"```json\\ninjected\\n```\"}",
	}

	conn := newTestConnector(f, srv, traleSyncConfig{})
	content, _, err := conn.Fetch(context.Background(), SourceItem{ID: "m1"})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if !strings.Contains(content, "````json") {
		t.Errorf("Fetch() did not lengthen the fence:\n%s", content)
	}
	if strings.Contains(content, "\n```json\n{\n") {
		t.Errorf("Fetch() left a short fence that the content can close:\n%s", content)
	}
}

func TestTraleConnector_Fetch_SSE(t *testing.T) {
	f, srv := newFakeMCP(t)
	f.sseFetch = true
	f.detail["m1"] = traleMeetingDetail{
		ID:      "m1",
		Title:   "Standup (SSE)",
		Summary: "Over SSE.",
		Content: "Alice: hi",
	}

	conn := newTestConnector(f, srv, traleSyncConfig{includeTranscript: true})
	content, _, err := conn.Fetch(context.Background(), SourceItem{ID: "m1"})
	if err != nil {
		t.Fatalf("Fetch(SSE) error = %v", err)
	}
	if !strings.Contains(content, "# Standup (SSE)") {
		t.Errorf("Fetch(SSE) markdown = %q, want title", content)
	}
	if !strings.Contains(content, "## Transcript") {
		t.Errorf("Fetch(SSE) missing transcript:\n%s", content)
	}
}

func TestTraleConnector_Fetch_TranscriptOnOff(t *testing.T) {
	f, srv := newFakeMCP(t)
	f.detail["m1"] = traleMeetingDetail{ID: "m1", Title: "T", Summary: "s", Content: "transcript text"}

	off := newTestConnector(f, srv, traleSyncConfig{includeTranscript: false})
	content, _, err := off.Fetch(context.Background(), SourceItem{ID: "m1"})
	if err != nil {
		t.Fatalf("Fetch(off) error = %v", err)
	}
	if strings.Contains(content, "## Transcript") || strings.Contains(content, "transcript text") {
		t.Errorf("Fetch(include_transcript=false) leaked transcript:\n%s", content)
	}

	on := newTestConnector(f, srv, traleSyncConfig{includeTranscript: true})
	content, _, err = on.Fetch(context.Background(), SourceItem{ID: "m1"})
	if err != nil {
		t.Fatalf("Fetch(on) error = %v", err)
	}
	if !strings.Contains(content, "## Transcript") || !strings.Contains(content, "transcript text") {
		t.Errorf("Fetch(include_transcript=true) missing transcript:\n%s", content)
	}
}

func TestTraleConnector_TestConnection(t *testing.T) {
	f, srv := newFakeMCP(t)
	f.tools = []string{"list_transcriptions", "fetch", "list_tags"}
	conn := newTestConnector(f, srv, traleSyncConfig{tags: []string{"standup"}})
	if err := conn.TestConnection(context.Background(), map[string]any{"tags": []string{"standup"}}); err != nil {
		t.Fatalf("TestConnection() error = %v, want nil", err)
	}

	missing, missingSrv := newFakeMCP(t)
	missing.tools = []string{"list_transcriptions"} // no fetch
	conn2 := newTestConnector(missing, missingSrv, traleSyncConfig{tags: []string{"standup"}})
	err := conn2.TestConnection(context.Background(), map[string]any{"tags": []string{"standup"}})
	if err == nil || !strings.Contains(err.Error(), "fetch") {
		t.Errorf("TestConnection(missing fetch) error = %v, want fetch named", err)
	}
}

func TestTraleProvider_ListResources_Tags(t *testing.T) {
	f, srv := newFakeMCP(t)
	f.tags = []traleTag{{Slug: "standup", Name: "Standup"}, {Slug: "roadmap", Name: "Roadmap"}}

	p := NewTraleProvider(srv.Client()).(*traleProvider)
	creds := Credentials{"access_token": "at", "resource": srv.URL}
	resources, _, err := p.ListResources(context.Background(), creds, nil, "")
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("ListResources() = %d resources, want 2", len(resources))
	}
	if resources[0].ID != "standup" || resources[0].Name != "Standup" {
		t.Errorf("ListResources()[0] = %+v, want standup/Standup", resources[0])
	}
}

func TestTraleConnector_ReconnectError(t *testing.T) {
	// A connector with an expired token and no refresh token surfaces the
	// actionable reconnect error before any MCP call.
	c := newTraleConnector(traleCredentials{
		accessToken:   "at",
		refreshToken:  "",
		expiresAt:     time.Now().Add(-time.Hour),
		clientID:      "client-123",
		tokenEndpoint: "https://tok",
		resource:      "https://res",
	}, nil)
	_, err := c.Enumerate(context.Background(), map[string]any{"tags": []string{"standup"}})
	if err == nil {
		t.Fatal("Enumerate(expired, no refresh) = nil, want reconnect error")
	}
	if !strings.Contains(err.Error(), "reconnect the source") {
		t.Errorf("Enumerate(expired) error = %v, want actionable reconnect message", err)
	}
}
