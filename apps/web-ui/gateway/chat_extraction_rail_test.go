package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestChatRailRendersExtractionOrigin pins the rail contract for extraction runs:
// a job the worker recorded as an agent run opens that run's transcript exactly
// like a board or scheduled run (data-origin="extraction" +
// data-action="open-run"), and a job without a recorded run still renders — as a
// link to its extraction history page, carrying the same origin so the type
// filter can isolate it rather than leaking into every other filter's view.
func TestChatRailRendersExtractionOrigin(t *testing.T) {
	rows := []workRunRow{
		{
			ID: "board-run-1", RunID: "board-run-1", Origin: "board",
			Title: "ACME-42", AgentName: "Kanban agent",
			Status: "completed", StartedAt: "2026-11-03T10:00:00Z",
		},
		{
			ID: "job-1", RunID: "run-1", Origin: "extraction",
			Title: "handbook.pdf", AgentName: "Extraction",
			Status: "completed", StartedAt: "2026-11-03T09:00:00Z",
		},
		{
			ID: "job-2", Origin: "extraction",
			Href:  "/documents/doc-2/extractions/job-2",
			Title: "Extraction job-2", AgentName: "Extraction",
			Status: "failed", StartedAt: "2026-11-03T08:00:00Z",
		},
	}

	html := renderHTML(t, chatRailList(&ConversationList{}, nil, nil, nil, rows, ""))

	for _, want := range []string{
		`data-origin="board"`,
		`data-run-id="board-run-1"`,
		`data-origin="extraction"`,
		`data-action="open-run"`,
		`data-run-id="run-1"`,
		`href="/documents/doc-2/extractions/job-2"`,
		`handbook.pdf`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("extraction rail missing %q\n%s", want, html)
		}
	}
	if strings.Contains(html, `data-run-id="job-2"`) {
		t.Error("a run-less extraction job must link to its history page, not open a run")
	}
	if strings.Contains(html, `data-run-id=""`) {
		t.Error("every run row must carry a non-empty run id — an empty one makes the row inert")
	}
}

// TestListSessionsExtractionOrigin proves the JSON sessions surface agrees with
// the rail: recorded extraction jobs are sessions with origin "extraction" keyed
// by their run id, run-less jobs are omitted (they are not resumable), and
// ?origin=extraction isolates them.
func TestListSessionsExtractionOrigin(t *testing.T) {
	f := &fakeMemory{
		documents: []Document{{ID: "doc-1", Filename: "handbook.pdf"}},
		extractionJobs: []ExtractionJob{
			{
				ID: "job-1", SourceType: "document", SourceID: "doc-1", Status: "completed",
				RunID:     "run-1",
				StartedAt: "2026-11-03T09:00:00Z", CompletedAt: "2026-11-03T09:01:00Z",
			},
			{ID: "job-2", SourceType: "document", SourceID: "doc-2", Status: "failed"},
		},
	}

	type session struct {
		Room    string  `json:"room"`
		Origin  string  `json:"origin"`
		Preview *string `json:"preview"`
	}
	decode := func(rec *httptest.ResponseRecorder) []session {
		t.Helper()
		var got struct {
			Sessions []session `json:"sessions"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v (body=%s)", err, rec.Body.String())
		}
		return got.Sessions
	}

	_, e := newClientEcho(f, Config{})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions?origin=extraction", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	sessions := decode(rec)
	if len(sessions) != 1 {
		t.Fatalf("want 1 extraction session (the run-less job is not one), got %d: %+v", len(sessions), sessions)
	}
	if sessions[0].Room != "run-1" || sessions[0].Origin != "extraction" {
		t.Errorf("session = %q/%q, want run-1/extraction", sessions[0].Room, sessions[0].Origin)
	}
	if sessions[0].Preview == nil || *sessions[0].Preview != "handbook.pdf" {
		t.Errorf("preview = %v, want the source document's filename", sessions[0].Preview)
	}
}

// TestChatRailBoardRowKeepsRunID drives the real rail route with a board run.
// The template keys the transcript target off RunID, so a producer that sets only
// ID renders data-run-id="" and chat.js (which guards `if (runId)`) makes the row
// inert. A hand-built workRunRow cannot catch that, because the bug lived in the
// data the producer supplies — so this asserts on the route output.
func TestChatRailBoardRowKeepsRunID(t *testing.T) {
	f := &fakeMemory{boardRuns: []BoardRun{{
		ID:        "run-board-1",
		AgentID:   "agent-1",
		AgentName: "Kanban agent",
		Status:    "completed",
		StartedAt: "2026-11-03T10:00:00Z",
	}}}
	_, e := newChatRailEcho(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/partial/chat-rail", nil))
	body := rec.Body.String()

	if !strings.Contains(body, `data-run-id="run-board-1"`) {
		t.Fatalf("board row must open its run: want data-run-id=\"run-board-1\"\n%s", body)
	}
	if strings.Contains(body, `data-run-id=""`) {
		t.Errorf("a row with an empty run id is inert (chat.js guards `if (runId)`)\n%s", body)
	}
}
