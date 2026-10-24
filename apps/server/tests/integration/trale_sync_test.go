package integration

// TraleSyncSuite exercises the full trale data-source sync path end to end
// against a fake MCP server and a real Postgres test database (no network, no
// browser). It proves that a trale source: enumerates a tag-scoped meeting set,
// writes one kb.documents row per completed meeting with the expected Markdown,
// is idempotent on re-sync, creates a new version when a summary changes, and
// marks a meeting that falls out of tag scope as deleted_from_source without
// destroying history — plus that a failing enumeration surfaces as a failed job
// rather than an empty success.

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/sources"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

func TestTraleSyncSuite(t *testing.T) {
	suite.Run(t, new(TraleSyncSuite))
}

type TraleSyncSuite struct {
	testutil.BaseSuite
	testKey []byte
	log     *slog.Logger
}

func (s *TraleSyncSuite) SetupSuite() {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 9)
	}
	s.testKey = key
	// Set before the server/config is built so the sources service derives the
	// same config-sourced credential-encryption key as production.
	s.T().Setenv("LLM_ENCRYPTION_KEY", hex.EncodeToString(key))
	s.SetDBSuffix("tralesync")
	s.BaseSuite.SetupSuite()
	s.log = slog.New(slog.NewTextHandler(io.Discard, nil))
}

func (s *TraleSyncSuite) projectUUID() uuid.UUID {
	return uuid.MustParse(s.ProjectID)
}

// newServiceWith builds a sources.Service on the test transaction with a real
// key and the given provider registry.
func (s *TraleSyncSuite) newServiceWith(registry *sources.Registry) *sources.Service {
	cfg := &config.Config{}
	cfg.LLMProvider.EncryptionKey = hex.EncodeToString(s.testKey)
	return sources.NewService(sources.NewRepository(s.DB(), s.log), cfg, s.log, registry)
}

func (s *TraleSyncSuite) newDocsService() *documents.Service {
	return documents.NewService(documents.NewRepository(s.DB(), s.log), s.log)
}

// newWorker builds a sync worker wired to a trale provider whose MCP transport
// uses the fake server's client, bypassing the SSRF guard exactly like the
// domain tests do.
func (s *TraleSyncSuite) newWorker(srv *httptest.Server) *sources.SyncWorker {
	svc := s.newServiceWith(sources.NewRegistry(sources.NewTraleProvider(srv.Client())))
	return sources.NewSyncWorker(svc, s.newDocsService(), nil, nil, s.log)
}

func (s *TraleSyncSuite) createSource(svc *sources.Service, resourceURL string) string {
	s.T().Helper()
	created, err := svc.Create(s.Ctx, s.projectUUID(), &sources.CreateIntegrationRequest{
		Name:         "Trale",
		ProviderType: sources.ProviderTypeTrale,
		Config: map[string]any{
			"tags":               []string{"standup"},
			"include_transcript": true,
			"backfill_days":      30,
			"access_token":       "test-token",
			"resource":           resourceURL,
		},
	})
	s.Require().NoError(err)
	return created.ID
}

// createSourceWithExpiredToken stores an already-expired OAuth token set plus a
// refresh token, so the next connector build has to refresh.
func (s *TraleSyncSuite) createSourceWithExpiredToken(svc *sources.Service, resourceURL string) string {
	s.T().Helper()
	created, err := svc.Create(s.Ctx, s.projectUUID(), &sources.CreateIntegrationRequest{
		Name:         "Trale rotating",
		ProviderType: sources.ProviderTypeTrale,
		Config: map[string]any{
			"tags":           []string{"standup"},
			"access_token":   "at-expired",
			"refresh_token":  "rt1",
			"expires_at":     time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
			"client_id":      "client-1",
			"token_endpoint": resourceURL + "/token",
			"resource":       resourceURL,
		},
	})
	s.Require().NoError(err)
	return created.ID
}

// triggerAndSync triggers a manual sync and drives it synchronously with a
// single worker batch (RunOnce). It returns the resulting sync-job DTO.
func (s *TraleSyncSuite) triggerAndSync(svc *sources.Service, worker *sources.SyncWorker, sourceID string) *sources.SyncJobDTO {
	s.T().Helper()
	resp, err := svc.TriggerSync(s.Ctx, s.projectUUID(), uuid.MustParse(sourceID))
	s.Require().NoError(err)
	s.Require().True(resp.Success, "trigger must succeed")
	s.Require().NotNil(resp.JobID)
	s.Require().NoError(worker.RunOnce(s.Ctx))

	dto, err := svc.GetSyncJob(s.Ctx, s.projectUUID(), uuid.MustParse(sourceID), uuid.MustParse(*resp.JobID))
	s.Require().NoError(err)
	return dto
}

// serveToken is the OAuth token endpoint. It rotates the refresh token on every
// use and rejects a spent one, which is what Supabase (Trale's authorization
// server) does. The lifetime is 1s — inside mcpauth.ExpirySkew — so every sync
// job has to refresh, which is what makes the persistence observable.
func (f *fakeTraleMCP) serveToken(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	presented := r.PostForm.Get("refresh_token")

	f.tokenMu.Lock()
	if f.spentTokens == nil {
		f.spentTokens = map[string]bool{}
	}
	if f.spentTokens[presented] {
		f.tokenMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"refresh token reuse detected"}`))
		return
	}
	f.spentTokens[presented] = true
	f.refreshSeen = append(f.refreshSeen, presented)
	f.tokenSeq++
	next := fmt.Sprintf("rt%d", f.tokenSeq)
	f.tokenMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  "at-" + next,
		"refresh_token": next,
		"token_type":    "Bearer",
		"expires_in":    1,
	})
}

// refreshTokensSeen returns the refresh tokens presented to the endpoint, in
// order.
func (f *fakeTraleMCP) refreshTokensSeen() []string {
	f.tokenMu.Lock()
	defer f.tokenMu.Unlock()
	return append([]string(nil), f.refreshSeen...)
}

// ---------------------------------------------------------------------------
// Document assertions (raw SQL over kb.documents, matching the existing sync
// suite's style).
// ---------------------------------------------------------------------------

type traleHeadDoc struct {
	ID               string `bun:"id"`
	Content          string `bun:"content"`
	SourceExternalID string `bun:"source_external_id"`
	SourceStatus     string `bun:"source_status"`
	CanonicalID      string `bun:"canonical_id"`
	Version          int    `bun:"version"`
}

func (s *TraleSyncSuite) headDocuments(sourceID string) []traleHeadDoc {
	s.T().Helper()
	var rows []traleHeadDoc
	err := s.DB().NewRaw(`
		SELECT id, COALESCE(content, '') AS content, source_external_id, source_status,
		       canonical_id, version
		FROM kb.documents
		WHERE source_id = ? AND supersedes_id IS NULL
		ORDER BY source_external_id
	`, sourceID).Scan(s.Ctx, &rows)
	s.Require().NoError(err)
	return rows
}

func (s *TraleSyncSuite) versionCount(sourceID, externalID string) int {
	s.T().Helper()
	var n int
	err := s.DB().NewRaw(`
		SELECT count(*)
		FROM kb.documents
		WHERE source_id = ? AND source_external_id = ?
	`, sourceID, externalID).Scan(s.Ctx, &n)
	s.Require().NoError(err)
	return n
}

// ---------------------------------------------------------------------------
// Fake Trale MCP server (streamable HTTP, JSON responses).
// ---------------------------------------------------------------------------

type fakeMeeting struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Summary          string `json:"summary"`
	Date             string `json:"date"`
	Status           string `json:"status"`
	ParticipantCount int    `json:"participant_count"`
	Tags             []any  `json:"tags"`
}

type fakeMeetingDetail struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	Date              string `json:"date"`
	Platform          string `json:"platform"`
	Status            string `json:"status"`
	Summary           string `json:"summary"`
	StructuredSummary string `json:"structured_summary"`
	Content           string `json:"content"`
	Participants      []any  `json:"participants"`
	Tags              []any  `json:"tags"`
}

type fakeTraleMCP struct {
	mu sync.Mutex

	// OAuth token endpoint state: every refresh rotates the token (#2091).
	tokenMu     sync.Mutex
	tokenSeq    int
	refreshSeen []string
	spentTokens map[string]bool

	meetings []fakeMeeting
	detail   map[string]fakeMeetingDetail
	failList bool

	listArgSets []map[string]any
	fetchIDs    []string
}

func newFakeTraleMCP(t *testing.T) (*fakeTraleMCP, *httptest.Server) {
	t.Helper()
	f := &fakeTraleMCP{detail: map[string]fakeMeetingDetail{}, tokenSeq: 1}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeTraleMCP) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/token" {
		f.serveToken(w, r)
		return
	}
	f.mu.Lock()
	session := r.Header.Get("Mcp-Session-Id")
	f.mu.Unlock()

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
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		writeResult(w, msg.ID, map[string]any{
			"tools": []any{
				map[string]any{"name": "list_transcriptions"},
				map[string]any{"name": "fetch"},
				map[string]any{"name": "list_tags"},
			},
		})
	case "tools/call":
		f.handleToolCall(w, msg.ID, msg.Params.Name, msg.Params.Arguments)
	default:
		http.Error(w, "unknown method: "+msg.Method, http.StatusBadRequest)
	}
}

func (f *fakeTraleMCP) handleToolCall(w http.ResponseWriter, id any, name string, args map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch name {
	case "list_transcriptions":
		f.listArgSets = append(f.listArgSets, args)
		if f.failList {
			writeErrorResult(w, id, "list_transcriptions unavailable")
			return
		}
		tags := extractStrings(args["tags"])
		writeToolResult(w, id, map[string]any{
			"meetings":   filterMeetings(f.meetings, tags),
			"pagination": map[string]any{"page": 1, "limit": 25, "has_next": false},
		})
	case "fetch":
		meetingID, _ := args["id"].(string)
		f.fetchIDs = append(f.fetchIDs, meetingID)
		detail, ok := f.detail[meetingID]
		if !ok {
			writeErrorResult(w, id, "not found")
			return
		}
		writeToolResult(w, id, detail)
	case "list_tags":
		writeToolResult(w, id, map[string]any{"tags": []any{}})
	default:
		writeErrorResult(w, id, "unknown tool")
	}
}

// setSummary mutates a meeting's summary in both the list item (driving the
// revision) and the fetched detail (driving the rendered content), mirroring a
// real edit where both surfaces change together.
func (f *fakeTraleMCP) setSummary(id, summary string) {
	for i := range f.meetings {
		if f.meetings[i].ID == id {
			f.meetings[i].Summary = summary
		}
	}
	if d, ok := f.detail[id]; ok {
		d.Summary = summary
		f.detail[id] = d
	}
}

func writeToolResult(w http.ResponseWriter, id any, structuredContent any) {
	encoded, _ := json.Marshal(structuredContent)
	writeResult(w, id, map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": string(encoded)}},
		"structuredContent": structuredContent,
	})
}

func writeErrorResult(w http.ResponseWriter, id any, msg string) {
	writeResult(w, id, map[string]any{
		"content": []any{map[string]any{"type": "text", "text": msg}},
		"isError": true,
	})
}

func writeResult(w http.ResponseWriter, id any, result map[string]any) {
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload)
}

// extractStrings tolerates the shapes a JSON round-trip produces for a string
// array argument.
func extractStrings(v any) []string {
	switch vals := v.(type) {
	case []any:
		out := make([]string, 0, len(vals))
		for _, item := range vals {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return vals
	default:
		return nil
	}
}

// filterMeetings returns the meetings whose tags match any of the requested tag
// slugs. An empty tag set means "no filter" (return everything), which is not
// what the sync path produces — the connector always forwards a non-empty tag
// scope.
func filterMeetings(meetings []fakeMeeting, tags []string) []fakeMeeting {
	if len(tags) == 0 {
		return meetings
	}
	want := make(map[string]bool, len(tags))
	for _, t := range tags {
		want[t] = true
	}
	out := make([]fakeMeeting, 0, len(meetings))
	for _, m := range meetings {
		for _, t := range m.Tags {
			switch v := t.(type) {
			case string:
				if want[v] {
					out = append(out, m)
					goto next
				}
			case map[string]any:
				if s, ok := v["slug"].(string); ok && want[s] {
					out = append(out, m)
					goto next
				}
				if s, ok := v["name"].(string); ok && want[s] {
					out = append(out, m)
					goto next
				}
			}
		}
	next:
	}
	return out
}

func tagObj(slug string) map[string]any { return map[string]any{"slug": slug, "name": slug} }

// defaultMeetings is the canonical fixture: two completed meetings tagged
// "standup" (m1, m2), one processing meeting tagged "standup" (m3 — skipped),
// and one completed meeting tagged "roadmap" (m4 — out of tag scope).
func defaultMeetings() []fakeMeeting {
	return []fakeMeeting{
		{ID: "m1", Title: "Standup 2026-10-22", Summary: "Team sync.", Date: "2026-10-22T09:00:00Z", Status: "completed", ParticipantCount: 3, Tags: []any{tagObj("standup"), tagObj("eng")}},
		{ID: "m2", Title: "Sprint planning 2026-10-21", Summary: "Planned the sprint.", Date: "2026-10-21T10:00:00Z", Status: "completed", ParticipantCount: 5, Tags: []any{tagObj("standup")}},
		{ID: "m3", Title: "Still processing", Summary: "not ready", Date: "2026-10-20T10:00:00Z", Status: "processing", ParticipantCount: 2, Tags: []any{tagObj("standup")}},
		{ID: "m4", Title: "Roadmap review", Summary: "Quarterly roadmap.", Date: "2026-10-19T10:00:00Z", Status: "completed", ParticipantCount: 4, Tags: []any{tagObj("roadmap")}},
	}
}

func defaultDetail(id, title, summary string) fakeMeetingDetail {
	return fakeMeetingDetail{
		ID:                id,
		Title:             title,
		Date:              "2026-10-22T09:00:00Z",
		Platform:          "Zoom",
		Status:            "completed",
		Summary:           summary,
		StructuredSummary: `{"agenda":"sync","next_steps":["ship"]}`,
		Content:           "Alice: hello\nBob: hi",
		Participants:      []any{map[string]any{"display_name": "Alice", "email": "alice@x.com", "org": "Acme"}},
		Tags:              []any{tagObj("standup")},
	}
}

// seed populates a fake with the standard four meetings and details for the two
// completed in-scope meetings.
func (f *fakeTraleMCP) seed() {
	f.meetings = defaultMeetings()
	f.detail["m1"] = defaultDetail("m1", "Standup 2026-10-22", "Team sync.")
	f.detail["m2"] = defaultDetail("m2", "Sprint planning 2026-10-21", "Planned the sprint.")
}

// ---------------------------------------------------------------------------
// Test cases
// ---------------------------------------------------------------------------

// TestSyncCreatesDocumentsForCompletedMeetings covers create + sync: exactly two
// documents (one per completed, in-scope meeting) with the expected Markdown;
// the processing and out-of-tag meetings produce nothing.
func (s *TraleSyncSuite) TestSyncCreatesDocumentsForCompletedMeetings() {
	f, srv := newFakeTraleMCP(s.T())
	f.seed()

	svc := s.newServiceWith(sources.NewRegistry(sources.NewTraleProvider(srv.Client())))
	sourceID := s.createSource(svc, srv.URL)
	dto := s.triggerAndSync(svc, s.newWorker(srv), sourceID)

	s.Require().Equal(sources.SyncStatusCompleted, dto.Status, "sync must complete")

	docs := s.headDocuments(sourceID)
	s.Require().Len(docs, 2, "exactly two completed in-scope meetings must produce documents")
	s.Require().Equal(2, dto.SuccessfulItems)
	s.Require().Equal(0, dto.FailedItems)

	byExt := map[string]traleHeadDoc{}
	for _, d := range docs {
		byExt[d.SourceExternalID] = d
	}

	m1, ok := byExt["m1"]
	s.Require().True(ok, "m1 must be ingested")
	for _, want := range []string{
		"# Standup 2026-10-22",
		"tags: standup", // metadata line
		"## Participants",
		"- Alice / alice@x.com / Acme",
		"## Summary",
		"Team sync.",
		"## Structured summary",
		`"agenda": "sync"`,
		"## Transcript",
		"Alice: hello",
	} {
		s.Require().Contains(m1.Content, want, "m1 markdown missing %q:\n%s", want, m1.Content)
	}

	// m2 also produced a document; m3 (processing) and m4 (out of tag) did not.
	s.Require().Contains(byExt, "m2", "m2 must be ingested")
	s.Require().NotContains(byExt, "m3", "processing meeting must not produce a document")
	s.Require().NotContains(byExt, "m4", "out-of-tag meeting must not produce a document")
}

// TestSyncScopeIsTagScopedAndDateBounded asserts the enumeration request was
// tag-scoped and date-bounded to the configured backfill window.
func (s *TraleSyncSuite) TestSyncScopeIsTagScopedAndDateBounded() {
	f, srv := newFakeTraleMCP(s.T())
	f.seed()

	svc := s.newServiceWith(sources.NewRegistry(sources.NewTraleProvider(srv.Client())))
	sourceID := s.createSource(svc, srv.URL)

	now := time.Now().UTC()
	s.triggerAndSync(svc, s.newWorker(srv), sourceID)

	f.mu.Lock()
	s.Require().Len(f.listArgSets, 1, "list_transcriptions called exactly once")
	args := f.listArgSets[0]
	f.mu.Unlock()

	tags, ok := args["tags"].([]any)
	s.Require().True(ok, "tags argument missing or wrong type: %#v", args)
	s.Require().Len(tags, 1)
	s.Require().Equal("standup", tags[0], "enumeration must be tag-scoped to [standup]")

	dateFromStr, ok := args["dateFrom"].(string)
	s.Require().True(ok, "dateFrom argument missing or wrong type: %#v", args)
	dateFrom, err := time.Parse(time.RFC3339, dateFromStr)
	s.Require().NoError(err, "dateFrom must be RFC3339")
	s.Require().WithinDuration(now.Add(-30*24*time.Hour), dateFrom, 2*time.Minute,
		"dateFrom must be the 30-day backfill window")

	dateToStr, ok := args["dateTo"].(string)
	s.Require().True(ok, "dateTo argument missing or wrong type: %#v", args)
	_, err = time.Parse(time.RFC3339, dateToStr)
	s.Require().NoError(err, "dateTo must be RFC3339")
}

// TestSyncIdempotentReRun asserts a second sync with unchanged data creates no
// new documents and no new version rows.
func (s *TraleSyncSuite) TestSyncIdempotentReRun() {
	f, srv := newFakeTraleMCP(s.T())
	f.seed()

	svc := s.newServiceWith(sources.NewRegistry(sources.NewTraleProvider(srv.Client())))
	sourceID := s.createSource(svc, srv.URL)
	worker := s.newWorker(srv)

	s.triggerAndSync(svc, worker, sourceID)
	s.Require().Len(s.headDocuments(sourceID), 2)

	s.triggerAndSync(svc, worker, sourceID)

	s.Require().Len(s.headDocuments(sourceID), 2, "re-sync must not create new documents")
	s.Require().Equal(1, s.versionCount(sourceID, "m1"), "unchanged meeting must not append a version")
	s.Require().Equal(1, s.versionCount(sourceID, "m2"), "unchanged meeting must not append a version")
}

// TestSyncChangeDetectedCreatesVersion asserts a changed summary creates a new
// version for that meeting only, leaving the other document untouched.
func (s *TraleSyncSuite) TestSyncChangeDetectedCreatesVersion() {
	f, srv := newFakeTraleMCP(s.T())
	f.seed()

	svc := s.newServiceWith(sources.NewRegistry(sources.NewTraleProvider(srv.Client())))
	sourceID := s.createSource(svc, srv.URL)
	worker := s.newWorker(srv)

	s.triggerAndSync(svc, worker, sourceID)
	s.Require().Len(s.headDocuments(sourceID), 2)

	// Mutate m1's summary (list + detail) so both the revision and the rendered
	// content change.
	f.setSummary("m1", "Team sync — revised.")
	s.triggerAndSync(svc, worker, sourceID)

	s.Require().Equal(2, s.versionCount(sourceID, "m1"), "changed summary must append a version")
	s.Require().Equal(1, s.versionCount(sourceID, "m2"), "unchanged meeting must keep a single version")

	docs := s.headDocuments(sourceID)
	s.Require().Len(docs, 2, "still two documents after change")
	for _, d := range docs {
		if d.SourceExternalID == "m1" {
			s.Require().Contains(d.Content, "Team sync — revised.", "m1 HEAD must carry the new summary")
			s.Require().Equal(2, d.Version, "m1 HEAD version must be 2")
		}
	}
}

// TestSyncRemovedMeetingMarkedDeleted asserts a meeting that falls out of tag
// scope is marked deleted_from_source, retained with its history, and excluded
// from a normal listing.
func (s *TraleSyncSuite) TestSyncRemovedMeetingMarkedDeleted() {
	f, srv := newFakeTraleMCP(s.T())
	f.seed()

	svc := s.newServiceWith(sources.NewRegistry(sources.NewTraleProvider(srv.Client())))
	sourceID := s.createSource(svc, srv.URL)
	worker := s.newWorker(srv)

	s.triggerAndSync(svc, worker, sourceID)
	s.Require().Len(s.headDocuments(sourceID), 2)

	// Drop m2 out of the standup tag so the next enumeration no longer returns it.
	for i := range f.meetings {
		if f.meetings[i].ID == "m2" {
			f.meetings[i].Tags = []any{tagObj("roadmap")}
		}
	}
	s.triggerAndSync(svc, worker, sourceID)

	// The m2 HEAD is marked deleted_from_source and retained (still one row).
	var status string
	err := s.DB().NewRaw(`
		SELECT source_status FROM kb.documents
		WHERE source_id = ? AND source_external_id = ? AND supersedes_id IS NULL
	`, sourceID, "m2").Scan(s.Ctx, &status)
	s.Require().NoError(err)
	s.Require().Equal(documents.SourceStatusDeletedFromSource, status, "out-of-scope meeting must be marked deleted-from-source")
	s.Require().Equal(1, s.versionCount(sourceID, "m2"), "deleted meeting must retain its history")

	// A normal listing excludes the deleted document.
	list, err := s.newDocsService().List(s.Ctx, documents.ListParams{ProjectID: s.ProjectID})
	s.Require().NoError(err)
	s.Require().Len(list.Documents, 1, "normal listing must exclude the deleted-from-source document")
	s.Require().Equal("m1", *list.Documents[0].SourceExternalID)
}

// TestSyncFailureSurfacesError asserts a failing enumeration fails the job with
// a non-empty error message rather than reporting an empty successful sync.
func (s *TraleSyncSuite) TestSyncFailureSurfacesError() {
	f, srv := newFakeTraleMCP(s.T())
	f.seed()
	f.mu.Lock()
	f.failList = true
	f.mu.Unlock()

	svc := s.newServiceWith(sources.NewRegistry(sources.NewTraleProvider(srv.Client())))
	sourceID := s.createSource(svc, srv.URL)
	dto := s.triggerAndSync(svc, s.newWorker(srv), sourceID)

	s.Require().Equal(sources.SyncStatusFailed, dto.Status, "enumeration failure must fail the job")
	s.Require().NotNil(dto.ErrorMessage, "failed job must carry an error message")
	s.Require().NotEmpty(*dto.ErrorMessage)
	s.Require().Empty(s.headDocuments(sourceID), "no documents may be written on a failed enumeration")
}

// TestRotationPersistsAcrossJobs is the #2091 regression at the storage layer:
// the worker writes the rotated token set back to the encrypted credential blob,
// so the second job refreshes with the token the first job persisted. Without the
// write-back it presents the spent rt1, the endpoint answers invalid_grant, and
// the job fails with the reconnect error instead of syncing.
func (s *TraleSyncSuite) TestRotationPersistsAcrossJobs() {
	f, srv := newFakeTraleMCP(s.T())
	f.seed()

	svc := s.newServiceWith(sources.NewRegistry(sources.NewTraleProvider(srv.Client())))
	sourceID := s.createSourceWithExpiredToken(svc, srv.URL)
	worker := s.newWorker(srv)

	first := s.triggerAndSync(svc, worker, sourceID)
	s.Require().Equal(sources.SyncStatusCompleted, first.Status, "the first job must refresh and sync")

	second := s.triggerAndSync(svc, worker, sourceID)
	s.Require().Equal(sources.SyncStatusCompleted, second.Status,
		"the second job must refresh with the token the first job persisted")

	s.Require().Equal([]string{"rt1", "rt2"}, f.refreshTokensSeen(),
		"one refresh per job, the second using the persisted rotation")
}

// TestListingAndConnectionTestPersistRotatedTokens covers the read-only paths
// that still refresh the token: the tag picker listing and the "test connection"
// button. Both rotate the stored token server-side, so both must write it back —
// an unpersisted rotation there strands the next sync just as surely as a job
// failing to persist its own (#2091 review).
func (s *TraleSyncSuite) TestListingAndConnectionTestPersistRotatedTokens() {
	f, srv := newFakeTraleMCP(s.T())
	f.seed()

	svc := s.newServiceWith(sources.NewRegistry(sources.NewTraleProvider(srv.Client())))
	sourceID := s.createSourceWithExpiredToken(svc, srv.URL)
	worker := s.newWorker(srv)
	id := uuid.MustParse(sourceID)

	// The tag picker refreshes rt1 -> rt2.
	_, _, err := svc.ListResources(s.Ctx, s.projectUUID(), id, "")
	s.Require().NoError(err, "the tag listing must survive a refresh")

	// The next job has to present rt2, not the spent rt1.
	dto := s.triggerAndSync(svc, worker, sourceID)
	s.Require().Equal(sources.SyncStatusCompleted, dto.Status,
		"the sync after a listing must use the persisted rotation")

	// The connection test refreshes again; the next job must follow it.
	resp, err := svc.TestConnection(s.Ctx, s.projectUUID(), id)
	s.Require().NoError(err)
	s.Require().True(resp.Success, resp.Message)

	second := s.triggerAndSync(svc, worker, sourceID)
	s.Require().Equal(sources.SyncStatusCompleted, second.Status,
		"the sync after a connection test must use the persisted rotation")

	seen := f.refreshTokensSeen()
	s.Require().Equal([]string{"rt1", "rt2", "rt3", "rt4"}, seen,
		"listing, sync, connection test and second sync must each refresh with the current token")
}
