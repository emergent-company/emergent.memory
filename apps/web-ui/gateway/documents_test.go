package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"golang.org/x/net/html"
)

// --- MemoryClient document methods ---

func TestListDocuments(t *testing.T) {
	var gotPath, gotLimit, gotCursor, gotProject string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		gotLimit = r.URL.Query().Get("limit")
		gotCursor = r.URL.Query().Get("cursor")
		gotProject = r.Header.Get("X-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"documents":[
			{"id":"d1","filename":"Spec.md","mimeType":"text/markdown","chunks":12,"embeddedChunks":10,"extractionStatus":"completed","processingStatus":"completed","version":2,"objectsCreated":15,"createdAt":"2026-08-26T10:00:00Z","updatedAt":"2026-08-27T10:00:00Z"},
			{"id":"d2","filename":"notes.txt","chunks":3,"extractionStatus":"pending","createdAt":"2026-08-26T11:00:00Z"}
		],"total":37,"statusCounts":{"all":37,"processing":4,"ready":20,"extracted":10,"failed":3},"next_cursor":"abc123"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	page, err := m.ListDocuments(context.Background(), DocumentListParams{
		Query:    "spec",
		Status:   "extracted",
		SourceID: "none",
		Sort:     "updated",
		Order:    "asc",
		Cursor:   "abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/documents" {
		t.Errorf("path = %q", gotPath)
	}
	if gotLimit != "50" {
		t.Errorf("limit = %q, want the default page size 50", gotLimit)
	}
	if gotCursor != "abc123" {
		t.Errorf("cursor = %q, want abc123", gotCursor)
	}
	if gotQuery.Get("q") != "spec" || gotQuery.Get("status") != "extracted" || gotQuery.Get("sourceId") != "none" {
		t.Errorf("filter params = %v", gotQuery)
	}
	if gotQuery.Get("sort") != "updated" || gotQuery.Get("order") != "asc" {
		t.Errorf("sort params = %v", gotQuery)
	}
	if gotProject != "proj-1" {
		t.Errorf("X-Project-ID = %q, want proj-1", gotProject)
	}
	if page.NextCursor != "abc123" {
		t.Errorf("next cursor = %q, want abc123", page.NextCursor)
	}
	if page.Total != 37 {
		t.Errorf("total = %d, want 37", page.Total)
	}
	if page.StatusCounts != (DocumentStatusCounts{All: 37, Processing: 4, Ready: 20, Extracted: 10, Failed: 3}) {
		t.Errorf("status counts = %+v", page.StatusCounts)
	}
	if len(page.Documents) != 2 {
		t.Fatalf("got %d docs, want 2", len(page.Documents))
	}
	if page.Documents[0].Filename != "Spec.md" || page.Documents[0].Chunks != 12 || page.Documents[0].ExtractionStatus != "completed" || page.Documents[0].ObjectsCreated != 15 {
		t.Errorf("doc0 = %+v", page.Documents[0])
	}
	if page.Documents[0].Version != 2 || page.Documents[0].ProcessingStatus != "completed" {
		t.Errorf("doc0 version/status = %d/%q", page.Documents[0].Version, page.Documents[0].ProcessingStatus)
	}
}

func TestListDocumentsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":{"code":"upstream","message":"down"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "proj")
	if _, err := m.ListDocuments(context.Background(), DocumentListParams{}); err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestListDocumentSourceCounts(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"sources":[{"sourceType":"upload","count":12},{"sourceId":"ds-1","sourceType":"drive","count":3}]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	sources, err := m.ListDocumentSourceCounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/documents/source-counts" {
		t.Errorf("path = %q", gotPath)
	}
	if len(sources) != 2 || sources[0].SourceType != "upload" || sources[0].Count != 12 || sources[1].SourceID != "ds-1" || sources[1].Count != 3 {
		t.Errorf("source counts = %+v", sources)
	}
}

func TestGetDocument(t *testing.T) {
	var gotPath, gotProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotProject = r.Header.Get("X-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"d1","filename":"Spec.md","mimeType":"text/markdown","chunks":12,"extractionStatus":"completed"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	doc, err := m.GetDocument(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/documents/d1" {
		t.Errorf("path = %q", gotPath)
	}
	if gotProject != "proj-1" {
		t.Errorf("X-Project-ID = %q", gotProject)
	}
	if doc.Filename != "Spec.md" || doc.Chunks != 12 {
		t.Errorf("doc = %+v", doc)
	}
}

func TestDeleteDocument(t *testing.T) {
	var gotMethod, gotPath, gotProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotProject = r.Header.Get("X-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"deleted","summary":{"chunks":2,"extractionJobs":1}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	if err := m.DeleteDocument(context.Background(), "d1"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/api/documents/d1" {
		t.Errorf("path = %q", gotPath)
	}
	if gotProject != "proj-1" {
		t.Errorf("X-Project-ID = %q, want proj-1", gotProject)
	}
}

func TestDeleteDocumentError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":"not_found","message":"document d1 not found"}}`, http.StatusNotFound)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "proj")
	if err := m.DeleteDocument(context.Background(), "d1"); err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestListChunks(t *testing.T) {
	var gotQuery, gotProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("documentId")
		gotProject = r.Header.Get("X-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[
			{"id":"c1","documentId":"d1","documentTitle":"Spec.md","index":0,"size":512,"hasEmbedding":true,"text":"first chunk"},
			{"id":"c2","documentId":"d1","documentTitle":"Spec.md","index":1,"size":300,"hasEmbedding":false,"text":"second chunk"}
		],"totalCount":2}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	chunks, err := m.ListChunks(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "d1" {
		t.Errorf("documentId = %q, want d1", gotQuery)
	}
	if gotProject != "proj-1" {
		t.Errorf("X-Project-ID = %q", gotProject)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if chunks[0].Index != 0 || chunks[0].Text != "first chunk" || !chunks[0].HasEmbedding {
		t.Errorf("chunk0 = %+v", chunks[0])
	}
	if chunks[1].HasEmbedding {
		t.Errorf("chunk1 should not be embedded")
	}
}

func TestUploadDocument(t *testing.T) {
	var gotFieldName, gotFilename, gotProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProject = r.Header.Get("X-Project-ID")
		f, fh, err := r.FormFile("file")
		if err != nil {
			t.Errorf("FormFile: %v", err)
			http.Error(w, "file required", http.StatusBadRequest)
			return
		}
		defer func() { _ = f.Close() }()
		gotFieldName = "file"
		gotFilename = fh.Filename
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"document":{"id":"newdoc"},"isDuplicate":false}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	res, err := m.UploadDocument(context.Background(), "hello.txt", strings.NewReader("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	if gotProject != "proj-1" {
		t.Errorf("X-Project-ID = %q", gotProject)
	}
	if gotFieldName != "file" {
		t.Errorf("form field = %q, want file", gotFieldName)
	}
	if gotFilename != "hello.txt" {
		t.Errorf("filename = %q, want hello.txt", gotFilename)
	}
	if res.DocumentID != "newdoc" {
		t.Errorf("id = %q, want newdoc", res.DocumentID)
	}
	if res.IsDuplicate {
		t.Errorf("isDuplicate = true, want false")
	}
	if res.ExistingDocumentID != "" {
		t.Errorf("existingDocumentId = %q, want empty", res.ExistingDocumentID)
	}
}

func TestUploadDocumentDuplicate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"document":{"id":"olddoc"},"isDuplicate":true,"existingDocumentId":"olddoc"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	res, err := m.UploadDocument(context.Background(), "hello.txt", strings.NewReader("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	if res.DocumentID != "olddoc" {
		t.Errorf("id = %q, want olddoc", res.DocumentID)
	}
	if !res.IsDuplicate {
		t.Errorf("isDuplicate = false, want true")
	}
	if res.ExistingDocumentID != "olddoc" {
		t.Errorf("existingDocumentId = %q, want olddoc", res.ExistingDocumentID)
	}
}

func TestUploadDocumentError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":"validation-failed","message":"file is required"}}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "proj")
	if _, err := m.UploadDocument(context.Background(), "x.txt", strings.NewReader("x")); err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestCreateExtractionJob(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"id":"job1","status":"queued"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	jobID, err := m.CreateExtractionJob(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if jobID != "job1" {
		t.Errorf("job id = %q, want job1", jobID)
	}
	if gotBody["project_id"] != "proj-1" {
		t.Errorf("project_id = %v", gotBody["project_id"])
	}
	if gotBody["source_type"] != "document" {
		t.Errorf("source_type = %v", gotBody["source_type"])
	}
	if gotBody["source_id"] != "d1" {
		t.Errorf("source_id = %v", gotBody["source_id"])
	}
	if _, ok := gotBody["extraction_config"]; !ok {
		t.Errorf("extraction_config missing: %v", gotBody)
	}
}

// --- handlers ---

func newDocAPIServer(f *fakeMemory, apiKey string) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{DefaultAgent: "memory", ClientAPIKey: apiKey}, memory: f}
	e := echo.New()
	// When apiKey is empty, requireClientKey is open (dev mode); when set, the
	// admin key gates /api. The retired registry-key path is no longer needed
	// here.
	api := e.Group("/api", s.requireClientKey)
	api.GET("/documents", s.listDocuments)
	api.GET("/documents/:id", s.getDocument)
	api.GET("/documents/:id/chunks", s.listDocumentChunks)
	api.POST("/documents", s.uploadDocument)
	api.POST("/documents/:id/extract", s.triggerExtraction)
	return s, e
}

func TestListDocumentsHandler(t *testing.T) {
	f := &fakeMemory{documents: []Document{{ID: "d1", Filename: "a.md", Chunks: 4, ExtractionStatus: "completed"}}}
	_, e := newDocAPIServer(f, "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/documents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Documents  []documentItem `json:"documents"`
		NextCursor string         `json:"nextCursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Documents) != 1 || got.Documents[0].Name != "a.md" {
		t.Fatalf("documents = %+v", got.Documents)
	}
}

func TestListDocumentsHandlerEmpty(t *testing.T) {
	_, e := newDocAPIServer(&fakeMemory{}, "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/documents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"documents":[]`) {
		t.Errorf("want empty documents array, got %s", rec.Body.String())
	}
}

func TestGetDocumentUnknown(t *testing.T) {
	_, e := newDocAPIServer(&fakeMemory{}, "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/documents/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

func TestUploadDocumentHandlerValidation(t *testing.T) {
	_, e := newDocAPIServer(&fakeMemory{}, "")
	// No multipart body → missing file → 400.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/documents", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/octet-stream")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestUploadDocumentHandler(t *testing.T) {
	f := &fakeMemory{}
	_, e := newDocAPIServer(f, "")

	var buf strings.Builder
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", "a.md")
	_, _ = fw.Write([]byte("hello"))
	_ = w.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/documents", strings.NewReader(buf.String()))
	req.Header.Set("Content-Type", w.FormDataContentType())
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(f.uploaded) != 1 || f.uploaded[0] != "a.md" {
		t.Fatalf("uploaded = %v", f.uploaded)
	}
}

func TestUploadDocumentHandlerDuplicate(t *testing.T) {
	f := &fakeMemory{uploadResult: &UploadResult{DocumentID: "old-doc", IsDuplicate: true, ExistingDocumentID: "old-doc"}}
	_, e := newDocAPIServer(f, "")

	var buf strings.Builder
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", "a.md")
	_, _ = fw.Write([]byte("hello"))
	_ = w.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/documents", strings.NewReader(buf.String()))
	req.Header.Set("Content-Type", w.FormDataContentType())
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != "old-doc" {
		t.Errorf("id = %v, want old-doc", got["id"])
	}
	if got["isDuplicate"] != true {
		t.Errorf("isDuplicate = %v, want true", got["isDuplicate"])
	}
	if got["existingDocumentId"] != "old-doc" {
		t.Errorf("existingDocumentId = %v, want old-doc", got["existingDocumentId"])
	}
}

func TestTriggerExtractionHandler(t *testing.T) {
	f := &fakeMemory{}
	_, e := newDocAPIServer(f, "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/documents/d1/extract", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(f.extracted) != 1 || f.extracted[0] != "d1" {
		t.Fatalf("extracted = %v", f.extracted)
	}
}

func TestTriggerExtractionHandlerBlocked(t *testing.T) {
	f := &fakeMemory{
		documents:      []Document{{ID: "d1", Filename: "Spec.md", Chunks: 2}},
		effectiveModel: &EffectiveModelConfig{},
	}
	_, e := newDocAPIServer(f, "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/documents/d1/extract", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No generative model is configured") {
		t.Errorf("body = %s, want no-generative-model error", rec.Body.String())
	}
	if len(f.extracted) != 0 {
		t.Fatalf("extracted = %v, want none (job must not be enqueued)", f.extracted)
	}
}

func TestTriggerExtractionHandlerBlockedUnknownDocument(t *testing.T) {
	// No model + unknown document id must still return 404 (not 409), preserving
	// the not-found contract even when the project has no generative model.
	f := &fakeMemory{effectiveModel: &EffectiveModelConfig{}}
	_, e := newDocAPIServer(f, "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/documents/nope/extract", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if len(f.extracted) != 0 {
		t.Fatalf("extracted = %v, want none (job must not be enqueued)", f.extracted)
	}
}

func TestTriggerExtractionHandlerFailOpenOnConfigError(t *testing.T) {
	// A model-config fetch error must fail open: extraction proceeds (200, job
	// enqueued), never 409.
	f := &fakeMemory{effectiveModelErr: errTest}
	_, e := newDocAPIServer(f, "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/documents/d1/extract", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(f.extracted) != 1 || f.extracted[0] != "d1" {
		t.Fatalf("extracted = %v, want [d1]", f.extracted)
	}
}

func TestDocumentsRequireAPIKey(t *testing.T) {
	_, e := newDocAPIServer(&fakeMemory{}, "secret")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/documents", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}

	// With the correct key it passes.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/documents", nil)
	req.Header.Set("X-API-Key", "secret")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 with key, got %d", rec.Code)
	}
}

// --- UI render ---

// documentsTestPage is a two-row page with one document of each provenance,
// real counts, and a further page behind a cursor.
func documentsTestPage() documentPageData {
	return documentPageData{
		Sort:       documentSortCreated,
		Order:      documentOrderDesc,
		Total:      37,
		Counts:     DocumentStatusCounts{All: 37, Processing: 4, Ready: 20, Extracted: 10, Failed: 3},
		NextCursor: "abc123",
		HasMore:    true,
		Sources: []documentSourceOption{
			{Value: documentSourceNone, Label: "Uploaded", Count: 34},
			{Value: "ds-2", Label: "Product Drive", Count: 3},
		},
		Rows: []documentRow{
			{ID: "d1", Name: "Spec.md", Chunks: 12, Status: "completed", Versions: 2, Source: "Uploaded", CreatedAt: "2026-08-26T10:00:00Z", UpdatedAt: "2026-08-27T10:00:00Z"},
			{ID: "d2", Name: "notes.txt", Chunks: 3, Status: "ready_for_extraction", Versions: 1, Source: "Product Drive", SourceID: "ds-2", SourceType: "drive", CreatedAt: "2026-08-26T11:00:00Z", UpdatedAt: "2026-08-26T11:00:00Z"},
		},
	}
}

func TestRenderDocumentsPage(t *testing.T) {
	data := documentsTestPage()
	html := renderHTML(t, DocumentsPage(data))
	for _, want := range []string{
		"Documents", "Spec.md", "notes.txt", "12 chunks", "3 chunks",
		// status labels are the user-facing names of the derived states
		"Extracted", "Ready",
		`href="/documents/d1"`, `name="file"`, `enctype="multipart/form-data"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("documents page missing %q", want)
		}
	}
	// per-row delete affordance on every row, with the confirm copy naming the
	// document (templ escapes the inner quotes to &#34;).
	for _, want := range []string{
		`action="/documents/d1/delete"`, `action="/documents/d2/delete"`,
		"Delete &#34;Spec.md&#34;? This cannot be undone.", "Delete &#34;notes.txt&#34;? This cannot be undone.", "lucide--trash-2",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("documents page missing %q", want)
		}
	}
	// #57: the search-form submit button uses the default (md) size, not btn-sm.
	searchAt := strings.Index(html, `data-testid="documents-search-form"`)
	if searchAt < 0 {
		t.Fatal("search form missing")
	}
	searchForm := html[searchAt:]
	if end := strings.Index(searchForm, "</form>"); end >= 0 {
		searchForm = searchForm[:end]
	}
	if strings.Contains(searchForm, "btn-sm") {
		t.Error("documents search button should not be btn-sm (it uses ui.ButtonMD)")
	}
}

// TestRenderDocumentsBrowser pins the browser's contract: a truthful header
// count, the stats strip, the status filter with counts, the table and the mobile
// card list, the columns the API already returns, and a load-more control that
// carries the active view.
func TestRenderDocumentsBrowser(t *testing.T) {
	data := documentsTestPage()
	html := renderHTML(t, DocumentsPage(data))

	for _, want := range []string{
		// header count is the project total, not the page's row count
		`data-testid="documents-count"`,
		"37 documents",
		`data-testid="documents-stats"`,
		`data-testid="documents-status-filter"`,
		`data-testid="documents-table"`,
		`id="documents-list"`,
		`id="documents-mobile-list"`,
		// columns from the API that the old row hid
		"Versions", "Source", "Updated",
		// the filter names the connected source ...
		`<option value="ds-2">Product Drive (3)</option>`,
		// ... and a row from it names it and links to it
		`title="From Product Drive"`,
		`href="/data-sources/ds-2"`,
		`data-testid="documents-source-link"`,
		`data-status="completed"`,
		// status filter is a select owning the same state
		`name="status"`,
		`value="failed"`,
		// sort headers: created is active (desc), updated links to it
		`aria-sort="descending"`,
		`href="/documents?sort=updated"`,
		// load more carries the cursor and the active view
		`data-testid="documents-load-more"`,
		`hx-target="#documents-list"`,
		`hx-swap="beforeend"`,
		`/documents/partial?cursor=abc123`,
		"Newest first",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("documents browser missing %q", want)
		}
	}

	// Only the sourced row links: one anchor in the table and one in the card
	// list, and nothing for the uploaded row (there is no data source to open).
	if got := strings.Count(html, `href="/data-sources/ds-2"`); got != 2 {
		t.Errorf("source links to ds-2 = %d, want 2 (table row + card)", got)
	}
	if strings.Contains(html, `href="/data-sources/ds-1"`) {
		t.Error("a row without a source id must not link to a data source")
	}
	if got := strings.Count(html, `data-testid="documents-source-link"`); got != 1 {
		t.Errorf("table source links = %d, want 1", got)
	}
	if got := strings.Count(html, `data-testid="documents-source-link-card"`); got != 1 {
		t.Errorf("card source links = %d, want 1", got)
	}
	// The card used to be one big <a>; the source link is its own anchor, so an
	// anchor inside an anchor would silently split the card's DOM.
	assertNoNestedAnchors(t, html)

	// No further page: no load-more trigger.
	data.HasMore = false
	data.NextCursor = ""
	lastPage := renderHTML(t, DocumentsPage(data))
	if strings.Contains(lastPage, `data-testid="documents-load-more"`) {
		t.Error("load more rendered on the last page")
	}
}

// TestRenderDocumentsFilterState pins that the controls echo the active state:
// an active search, status, source and sort survive in the form's hidden inputs,
// selects and sort links, so narrowing never silently drops a filter.
func TestRenderDocumentsFilterState(t *testing.T) {
	data := documentsTestPage()
	data.Query = "spec"
	data.Status = documentStatusFailed
	data.SourceID = "ds-2"
	data.Sort = documentSortUpdated
	data.Order = documentOrderAsc

	html := renderHTML(t, DocumentsPage(data))
	for _, want := range []string{
		`name="q" value="spec"`,
		`<option value="failed" selected`,
		`aria-sort="ascending"`,
		"Least recently updated first",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("filter state missing %q", want)
		}
	}
	// The source filter's options come from the project's connected sources.
	if !strings.Contains(html, `value="ds-2" selected`) {
		t.Error("active source not selected in the source filter")
	}
}

func TestRenderDocumentsEmptyStates(t *testing.T) {
	empty := renderHTML(t, DocumentsPage(documentPageData{Sort: documentSortCreated, Order: documentOrderDesc}))
	if !strings.Contains(empty, "No documents yet") {
		t.Error("empty state missing")
	}
	if strings.Contains(empty, "No documents match") {
		t.Error("unfiltered empty state should not claim a filter matched nothing")
	}

	filtered := renderHTML(t, DocumentsPage(documentPageData{Query: "zzz", Sort: documentSortCreated, Order: documentOrderDesc}))
	if !strings.Contains(filtered, "No documents match") {
		t.Error("filtered empty state missing")
	}
	if strings.Contains(filtered, "No documents yet") {
		t.Error("filtered empty state must not claim the project has no documents")
	}
	// #2312: the standalone "Clear" button was removed — the search field's own
	// clear affordance and the status filter cover it, and two ways back read as
	// clutter. The search field must still be there, or the empty state would
	// have no way out at all.
	if strings.Contains(filtered, `data-testid="documents-clear-filters"`) {
		t.Error("the redundant Clear button was removed in #2312; it must not come back")
	}
	if !strings.Contains(filtered, `name="q"`) {
		t.Error("the filtered empty state must keep the search field reachable")
	}
	if !strings.Contains(filtered, `data-document-upload`) {
		t.Error("upload form should stay usable in the filtered empty state")
	}
}

func TestRenderDocumentsLoadError(t *testing.T) {
	html := renderHTML(t, DocumentsPage(documentPageData{Sort: documentSortCreated, Order: documentOrderDesc, LoadErr: errTest}))
	if !strings.Contains(html, "Failed to load documents") {
		t.Error("error state missing")
	}
	if strings.Contains(html, `data-testid="documents-stats"`) {
		t.Error("a failed fetch must not render zeroed statistics")
	}
}

func TestRenderDocumentsFlashes(t *testing.T) {
	data := documentsTestPage()
	data.FlashMsg = "Document uploaded."
	if html := renderHTML(t, DocumentsPage(data)); !strings.Contains(html, "Document uploaded.") {
		t.Error("upload success flash missing")
	}

	data = documentsTestPage()
	data.FlashErr = errTest
	if html := renderHTML(t, DocumentsPage(data)); !strings.Contains(html, "backend unreachable") {
		t.Error("upload flash should render the real error, missing")
	}
}

// TestRenderDocumentsUploadDialog pins the upload affordance: the page keeps a
// single header button, and the form itself lives in a closed modal so the
// listing is not pushed down by an always-open drop zone.
func TestRenderDocumentsUploadDialog(t *testing.T) {
	html := renderHTML(t, DocumentsPage(documentsTestPage()))

	for _, want := range []string{
		`data-testid="documents-upload-open"`,
		`data-dialog-open="document-upload-modal"`,
		`<dialog id="document-upload-modal" class="modal"`,
		`data-testid="documents-upload-modal"`,
		`aria-labelledby="document-upload-title"`,
		`data-testid="documents-upload-input"`,
		`data-testid="documents-upload-submit"`,
		`data-dialog-close="document-upload-modal"`,
		`data-document-upload`,
		`enctype="multipart/form-data"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("upload dialog missing %q", want)
		}
	}

	// One file input, and it is inside the dialog rather than loose on the page.
	if got := strings.Count(html, `name="file"`); got != 1 {
		t.Errorf("file inputs = %d, want 1", got)
	}
	dialogAt := strings.Index(html, `<dialog id="document-upload-modal"`)
	inputAt := strings.Index(html, `data-testid="documents-upload-input"`)
	closeAt := strings.Index(html, `</dialog>`)
	if dialogAt < 0 || inputAt <= dialogAt || closeAt <= inputAt {
		t.Errorf("file input must sit inside the upload dialog (dialog=%d input=%d close=%d)", dialogAt, inputAt, closeAt)
	}

	// An empty project points at the same dialog from its own call to action.
	empty := renderHTML(t, DocumentsPage(documentPageData{Sort: documentSortCreated, Order: documentOrderDesc}))
	ctaAt := strings.Index(empty, `data-testid="documents-empty-upload"`)
	if ctaAt < 0 {
		t.Fatal("empty state should offer an upload button")
	}
	// The attributes render before the test id, so take the whole opening tag.
	tagStart := strings.LastIndex(empty[:ctaAt], "<button")
	tagEnd := strings.Index(empty[ctaAt:], ">")
	if tagStart < 0 || tagEnd < 0 {
		t.Fatal("empty state upload button is not a <button> tag")
	}
	if cta := empty[tagStart : ctaAt+tagEnd]; !strings.Contains(cta, `data-dialog-open="document-upload-modal"`) {
		t.Error("the empty state's upload button should open the dialog")
	}
}

// TestRenderDocumentsRowsPartial pins the pagination fragment: rows appended
// into #documents-list, the same rows OOB-appended to the mobile list, and a
// replacement load-more slot that disappears on the final page.
func TestRenderDocumentsRowsPartial(t *testing.T) {
	data := documentsTestPage()
	html := renderHTML(t, documentsRowsPartial(data))
	for _, want := range []string{
		`data-document-id="d1"`,
		`data-document-id="d2"`,
		`id="documents-mobile-list"`,
		`hx-swap-oob="beforeend"`,
		`id="documents-load-more"`,
		`hx-swap-oob="true"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rows partial missing %q", want)
		}
	}

	data.HasMore = false
	data.NextCursor = ""
	html = renderHTML(t, documentsRowsPartial(data))
	if strings.Contains(html, `data-testid="documents-load-more"`) {
		t.Error("rows partial should drop the load-more trigger on the last page")
	}
}

func TestRenderDocumentDetailPage(t *testing.T) {
	doc := &Document{ID: "d1", Filename: "Spec.md", MimeType: "text/markdown", Chunks: 2, ExtractionStatus: "completed", ObjectsCreated: 15}
	preview := documentPreview{Mode: previewModeText, Text: "hello preview"}
	html := renderHTML(t, DocumentDetailPage(doc, preview, nil, "", nil, false))
	for _, want := range []string{
		"Spec.md", "completed", "15 objects extracted",
		`action="/documents/d1/extract"`, `href="/documents"`,
		`action="/documents/d1/delete"`, "Delete &#34;Spec.md&#34;? This cannot be undone.", "lucide--trash-2", ">Delete<",
		// sub-navigation rail (the chunk count is a badge on its Chunks entry)
		`aria-label="Document sections"`, "Overview", "Chunks", "Extraction history",
		`data-testid="document-chunks-count"`,
		`href="/documents/d1/chunks"`, `href="/documents/d1/extractions"`,
		// text preview
		`data-testid="document-preview-text"`, "hello preview",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("detail page missing %q", want)
		}
	}
	// Chunks and extraction results now live on their own sub-pages.
	if strings.Contains(html, "No chunks yet") {
		t.Error("overview should not render the chunks section")
	}
	if strings.Contains(html, "Extraction results") {
		t.Error("overview should not render the inline extraction results section")
	}

	htmlErr := renderHTML(t, DocumentDetailPage(nil, documentPreview{}, errTest, "", nil, false))
	if !strings.Contains(htmlErr, "Document unavailable") {
		t.Error("load error state missing")
	}

	htmlFlash := renderHTML(t, DocumentDetailPage(doc, preview, nil, "Extraction triggered.", nil, false))
	if !strings.Contains(htmlFlash, "Extraction triggered.") {
		t.Error("extraction flash missing")
	}

	// The enabled render must not carry a disabled extract button.
	if strings.Contains(html, "disabled") {
		t.Error("enabled render must not disable the extract button")
	}

	// A blocked render disables the extract button, swaps the description, and
	// surfaces the warning + providers-settings link.
	htmlBlocked := renderHTML(t, DocumentDetailPage(doc, preview, nil, "", nil, true))
	for _, want := range []string{
		"disabled",
		"Extraction needs a generative model.",
		"No generative model is configured. Add a provider and set a default model before running extraction.",
		`href="/settings/providers"`,
	} {
		if !strings.Contains(htmlBlocked, want) {
			t.Errorf("blocked detail page missing %q", want)
		}
	}
}

// TestDocumentPreviewEscapesHostileContent pins that stored document text is
// HTML-escaped in the preview: templ renders the string as text, so a document
// containing script/HTML tags cannot execute in the gateway origin.
func TestDocumentPreviewEscapesHostileContent(t *testing.T) {
	doc := &Document{ID: "d1", Filename: "evil.md", MimeType: "text/markdown"}
	hostile := `<script>alert(1)</script><img src=x onerror=alert(2)></pre><svg onload=alert(3)>`
	html := renderHTML(t, DocumentDetailPage(doc, documentPreview{Mode: previewModeText, Text: hostile}, nil, "", nil, false))
	for _, raw := range []string{"<script>alert", "<img src=x", "<svg onload", "</pre><svg"} {
		if strings.Contains(html, raw) {
			t.Errorf("hostile preview content rendered unescaped: %q", raw)
		}
	}
	for _, esc := range []string{"&lt;script&gt;alert", "&lt;img src=x", "&lt;svg onload"} {
		if !strings.Contains(html, esc) {
			t.Errorf("expected escaped preview content %q", esc)
		}
	}
}

// TestDocumentSubNavActive asserts the shared document sub-nav marks exactly the
// active view current.
func TestDocumentSubNavActive(t *testing.T) {
	html := renderHTML(t, documentSubNav(&Document{ID: "d1", Chunks: 2}, "chunks"))
	if !strings.Contains(html, `aria-label="Document sections"`) {
		t.Error("sub-nav label missing")
	}
	if !linkMarkedCurrent(html, "/documents/d1/chunks") {
		t.Error("chunks link should be marked current")
	}
	if linkMarkedCurrent(html, "/documents/d1") || linkMarkedCurrent(html, "/documents/d1/extractions") {
		t.Error("only the active sub-nav link should be current")
	}
	// The chunk count rides on the Chunks entry, not the page header.
	if !strings.Contains(html, `data-testid="document-chunks-count"`) {
		t.Error("chunks rail item should carry the chunk-count badge")
	}

	overview := renderHTML(t, documentSubNav(&Document{ID: "d1", Chunks: 2}, "overview"))
	if !linkMarkedCurrent(overview, "/documents/d1") {
		t.Error("overview link should be current on the overview view")
	}
	if !strings.Contains(overview, `data-testid="document-chunks-count"`) {
		t.Error("chunk count should stay visible on the overview view")
	}

	empty := renderHTML(t, documentSubNav(&Document{ID: "d1"}, "overview"))
	if strings.Contains(empty, `data-testid="document-chunks-count"`) {
		t.Error("a zero-chunk document should not render an empty count badge")
	}
}

// linkMarkedCurrent reports whether the anchor with exactly href carries
// aria-current="page".
func linkMarkedCurrent(html, href string) bool {
	marker := `href="` + href + `"`
	for i := strings.Index(html, marker); i >= 0; {
		rest := html[i:]
		end := strings.Index(rest, ">")
		if end < 0 {
			return false
		}
		if strings.Contains(rest[:end], `aria-current="page"`) {
			return true
		}
		next := strings.Index(rest[len(marker):], marker)
		if next < 0 {
			return false
		}
		i += len(marker) + next
	}
	return false
}

// TestDocumentPreviewMode covers the preview strategy chosen from the document's
// media type.
func TestDocumentPreviewMode(t *testing.T) {
	cases := []struct {
		name string
		doc  *Document
		want string
	}{
		{"pdf", &Document{ID: "d1", Filename: "a.pdf", MimeType: "application/pdf"}, `data-testid="document-preview-pdf"`},
		{"image", &Document{ID: "d1", Filename: "a.png", MimeType: "image/png"}, `data-testid="document-preview-image"`},
		{"markdown-mime", &Document{ID: "d1", Filename: "a.md", MimeType: "text/markdown"}, `data-testid="document-preview-text"`},
		{"markdown-filename", &Document{ID: "d1", Filename: "notes.md", MimeType: ""}, `data-testid="document-preview-text"`},
		{"plaintext", &Document{ID: "d1", Filename: "a.txt", MimeType: "text/plain"}, `data-testid="document-preview-text"`},
		{"download", &Document{ID: "d1", Filename: "a.bin", MimeType: "application/octet-stream"}, `data-testid="document-preview-download"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			preview := documentPreview{Mode: previewModeFor(tc.doc)}
			html := renderHTML(t, DocumentDetailPage(tc.doc, preview, nil, "", nil, false))
			if !strings.Contains(html, tc.want) {
				t.Errorf("preview mode %s missing %q", tc.name, tc.want)
			}
			if !strings.Contains(html, `href="/documents/d1/file"`) {
				t.Errorf("preview mode %s missing download link", tc.name)
			}
		})
	}
}

// TestDocumentPreviewSrcScoped pins that the PDF/image preview embed carries the
// page's scope base on a scoped request, and stays unchanged when unscoped. The
// sub-resource URLs otherwise 404 under the session-mode hard cut (the embed
// src is rendered by rewriteScopedHTML at render time, not by linkURL).
func TestDocumentPreviewSrcScoped(t *testing.T) {
	const base = "/o/o1/p/p1"
	for _, tc := range []struct {
		name   string
		doc    *Document
		marker string
	}{
		{"pdf", &Document{ID: "d1", Filename: "a.pdf", MimeType: "application/pdf"}, `data-testid="document-preview-pdf"`},
		{"image", &Document{ID: "d1", Filename: "a.png", MimeType: "image/png"}, `data-testid="document-preview-image"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			preview := documentPreview{Mode: previewModeFor(tc.doc)}
			html := renderHTML(t, DocumentDetailPage(tc.doc, preview, nil, "", nil, false))
			if !strings.Contains(html, tc.marker) {
				t.Fatalf("preview mode missing %q", tc.marker)
			}
			scoped := rewriteScopedHTML(html, base)
			if !strings.Contains(scoped, `src="/o/o1/p/p1/documents/d1/file"`) {
				t.Errorf("scoped preview missing scoped src: %s", scoped)
			}
			if strings.Contains(scoped, `src="/documents/d1/file"`) {
				t.Errorf("scoped preview retained unscoped src: %s", scoped)
			}
			if got := rewriteScopedHTML(html, ""); !strings.Contains(got, `src="/documents/d1/file"`) {
				t.Errorf("unscoped preview changed src: %s", got)
			}
		})
	}
}

// TestDocumentPreviewUnavailable asserts a failed server-side text fetch
// degrades to a note with a Chunks link rather than an error.
func TestDocumentPreviewUnavailable(t *testing.T) {
	doc := &Document{ID: "d1", Filename: "a.md", MimeType: "text/markdown"}
	html := renderHTML(t, DocumentDetailPage(doc, documentPreview{Mode: previewModeText, Err: errTest}, nil, "", nil, false))
	if !strings.Contains(html, `data-testid="document-preview-unavailable"`) {
		t.Error("failed text preview should degrade to the unavailable card")
	}
	if !strings.Contains(html, `href="/documents/d1/chunks"`) {
		t.Error("unavailable preview should link to the Chunks view")
	}
}

// TestLoadDocumentPreview covers the overview preview resolver: text comes from
// the stored-content endpoint (so raw-content documents with no original file
// still preview), empty/error content degrades, long content is capped, and
// non-text media types skip the content read.
func TestLoadDocumentPreview(t *testing.T) {
	textDoc := &Document{ID: "d1", Filename: "a.md", MimeType: "text/markdown"}

	t.Run("content present without original file", func(t *testing.T) {
		content := "# Title\n\nBody"
		// DownloadDocument fails/absent; the preview must still come from content.
		f := &fakeMemory{docContent: &content, downloadDocErr: errTest}
		s := &Server{memory: f}
		p := s.loadDocumentPreview(context.Background(), textDoc)
		if p.Err != nil || p.Text != content || p.Truncated {
			t.Fatalf("preview = %+v", p)
		}
		html := renderHTML(t, DocumentDetailPage(textDoc, p, nil, "", nil, false))
		if !strings.Contains(html, `data-testid="document-preview-text"`) || !strings.Contains(html, content) {
			t.Fatalf("expected text preview, got %s", html)
		}
		if strings.Contains(html, `data-testid="document-preview-unavailable"`) {
			t.Fatal("should not degrade when content is available")
		}
	})

	t.Run("empty content degrades", func(t *testing.T) {
		empty := ""
		s := &Server{memory: &fakeMemory{docContent: &empty}}
		p := s.loadDocumentPreview(context.Background(), textDoc)
		if p.Err == nil {
			t.Fatalf("empty content should degrade, got %+v", p)
		}
		html := renderHTML(t, DocumentDetailPage(textDoc, p, nil, "", nil, false))
		if !strings.Contains(html, `data-testid="document-preview-unavailable"`) {
			t.Error("empty content should render the unavailable card, not a blank box")
		}
	})

	t.Run("content read error degrades", func(t *testing.T) {
		s := &Server{memory: &fakeMemory{docErr: errTest}}
		p := s.loadDocumentPreview(context.Background(), textDoc)
		if p.Err == nil {
			t.Fatalf("read error should degrade, got %+v", p)
		}
	})

	t.Run("caps long content", func(t *testing.T) {
		long := strings.Repeat("a", maxTextPreviewChars+10)
		s := &Server{memory: &fakeMemory{docContent: &long}}
		p := s.loadDocumentPreview(context.Background(), textDoc)
		if !p.Truncated || len([]rune(p.Text)) != maxTextPreviewChars {
			t.Fatalf("truncation = %v, len = %d", p.Truncated, len([]rune(p.Text)))
		}
	})

	t.Run("non-text mode skips content", func(t *testing.T) {
		s := &Server{memory: &fakeMemory{docErr: errTest}}
		p := s.loadDocumentPreview(context.Background(), &Document{ID: "d1", Filename: "a.pdf", MimeType: "application/pdf"})
		if p.Mode != previewModePDF || p.Text != "" || p.Err != nil {
			t.Fatalf("pdf preview = %+v", p)
		}
	})
}

// TestRenderExtractionHistoryEmpty covers the no-extractions state.
func TestRenderExtractionHistoryEmpty(t *testing.T) {
	doc := &Document{ID: "d1", Filename: "Spec.md"}
	html := renderHTML(t, DocumentExtractionsPage(doc, nil, nil, "", "", nil, nil))
	if !strings.Contains(html, "No extractions yet") {
		t.Error("empty extraction history state missing")
	}
	if !strings.Contains(html, `href="/documents/d1/extractions"`) {
		t.Error("extraction history sub-nav missing")
	}
}

// TestRenderExtractionHistoryRows covers the per-job rows and their lazy detail
// targets.
func TestRenderExtractionHistoryRows(t *testing.T) {
	doc := &Document{ID: "d1", Filename: "Spec.md"}
	jobs := []ExtractionJob{
		{ID: "job1", Status: "completed", CreatedAt: "2026-08-26T10:00:00Z", CreatedObjects: []string{"o1"}, DiscoveredTypes: []string{"person"}},
		{ID: "job2", Status: "failed", CreatedAt: "2026-08-25T10:00:00Z"},
	}
	html := renderHTML(t, DocumentExtractionsPage(doc, jobs, nil, "", "", nil, nil))
	for _, want := range []string{
		`data-testid="extraction-history"`,
		`id="extraction-detail-job1"`,
		`hx-get="/documents/d1/extractions/job1/detail"`,
		`hx-trigger="load"`,
		`hx-target="this"`,
		`hx-swap="outerHTML"`,
		`data-testid="extraction-job-row"`,
		"1 object", "person", "failed",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("extraction history missing %q", want)
		}
	}
}

// TestSortExtractionJobsNewestFirst pins the newest-first order the view relies
// on.
func TestSortExtractionJobsNewestFirst(t *testing.T) {
	jobs := []ExtractionJob{
		{ID: "old", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "new", CreatedAt: "2026-03-01T00:00:00Z"},
	}
	sortExtractionJobsNewestFirst(jobs)
	if jobs[0].ID != "new" || jobs[1].ID != "old" {
		t.Fatalf("order = %s,%s", jobs[0].ID, jobs[1].ID)
	}
}

// TestRenderExtractionJobDetailMergeForm covers the merge-action visibility
// rule: shown only for an unmerged, resolvable branch.
func TestRenderExtractionJobDetailMergeForm(t *testing.T) {
	base := extractionJobDetail{
		Job:     ExtractionJob{ID: "job1", Status: "completed"},
		Objects: []GraphObject{{ID: "o1", CanonicalID: "c1", Type: "person", Key: "sam"}},
		Relationships: []GraphRelationship{
			{ID: "r1", Type: "assigned_to", SrcID: "c1", DstID: "c1"},
		},
	}

	t.Run("unmerged branch shows merge form", func(t *testing.T) {
		d := base
		d.BranchID = "b1"
		d.Branch = &Branch{ID: "b1", Name: "extract-1"}
		d.Readiness = &MergeReadiness{Ready: true, TotalObjects: 1}
		html := renderHTML(t, ExtractionJobDetail("d1", d))
		for _, want := range []string{
			`data-testid="extraction-merge"`,
			`hx-get="/documents/d1/extractions/job1/merge-preview"`,
			"extract-1", "Ready to merge", "assigned to", "sam",
		} {
			if !strings.Contains(html, want) {
				t.Errorf("unmerged detail missing %q", want)
			}
		}
	})

	t.Run("merged branch hides merge form", func(t *testing.T) {
		d := base
		d.Branch = &Branch{ID: "b1", Name: "extract-1", MergedAt: "2026-08-27T00:00:00Z"}
		html := renderHTML(t, ExtractionJobDetail("d1", d))
		if strings.Contains(html, `data-testid="extraction-merge"`) {
			t.Error("merged branch must not render the merge form")
		}
		if !strings.Contains(html, "Merged into main") {
			t.Error("merged branch should report it is merged")
		}
	})

	t.Run("no branch hides merge form", func(t *testing.T) {
		d := base
		d.NoBranch = true
		html := renderHTML(t, ExtractionJobDetail("d1", d))
		if strings.Contains(html, `data-testid="extraction-merge"`) {
			t.Error("branchless job must not render the merge form")
		}
		if !strings.Contains(html, `data-testid="extraction-detail-nobranch"`) {
			t.Error("branchless job should show the no-branch note")
		}
	})
}

// TestDocumentRoutes exercises the page routes against the fake backend,
// including the upload and extraction PRG redirects.
func TestDocumentRoutes(t *testing.T) {
	f := &fakeMemory{documents: []Document{{ID: "d1", Filename: "Spec.md", Chunks: 2, ExtractionStatus: "completed"}}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/documents", s.uiDocuments)
	e.GET("/documents/:id", s.uiDocument)
	e.POST("/documents", s.uiUploadDocument)
	e.POST("/documents/:id/extract", s.uiTriggerExtraction)
	e.POST("/documents/:id/delete", s.uiDeleteDocument)

	// list page
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Spec.md") {
		t.Fatalf("list page: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// detail page
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents/d1", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Extraction") {
		t.Fatalf("detail page: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// upload redirect
	var buf strings.Builder
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", "a.md")
	_, _ = fw.Write([]byte("hi"))
	_ = w.Close()
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/documents", strings.NewReader(buf.String()))
	req.Header.Set("Content-Type", w.FormDataContentType())
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upload redirect status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/documents?uploaded=1" {
		t.Errorf("upload redirect location = %q", loc)
	}

	// extraction redirect
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/documents/d1/extract", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("extract redirect status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/documents/d1?extracted=1" {
		t.Errorf("extract redirect location = %q", loc)
	}
	if len(f.extracted) != 1 || f.extracted[0] != "d1" {
		t.Fatalf("extracted = %v", f.extracted)
	}

	// delete redirect (plain form POST → 303 to the list with the deleted flash)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/documents/d1/delete", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete redirect status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/documents?deleted=1" {
		t.Errorf("delete redirect location = %q", loc)
	}
	if len(f.deletedDocuments) != 1 || f.deletedDocuments[0] != "d1" {
		t.Fatalf("deletedDocuments = %v", f.deletedDocuments)
	}
	if len(f.documents) != 0 {
		t.Fatalf("documents after delete = %v, want none", f.documents)
	}

	// deleted flash renders on the list page
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents?deleted=1", nil))
	if !strings.Contains(rec.Body.String(), "Document deleted.") {
		t.Error("deleted flash missing on list page")
	}
}

// TestDocumentRoutesExtractionBlocked pins that the web extraction trigger is
// refused (redirect with ?err=, no job enqueued) when the project has no
// resolvable generative model.
func TestDocumentRoutesExtractionBlocked(t *testing.T) {
	f := &fakeMemory{
		documents:      []Document{{ID: "d1", Filename: "Spec.md", Chunks: 2, ExtractionStatus: "completed"}},
		effectiveModel: &EffectiveModelConfig{},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/documents/:id/extract", s.uiTriggerExtraction)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/documents/d1/extract", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("blocked extract status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "?err=") {
		t.Errorf("blocked extract location = %q, want ?err=", loc)
	}
	if len(f.extracted) != 0 {
		t.Fatalf("extracted = %v, want none (job must not be enqueued)", f.extracted)
	}
}

// TestDocumentRoutesExtractionFailOpen pins that the web extraction trigger
// fails open on a model-config fetch error: it proceeds to enqueue (redirect to
// ?extracted=1), never the blocked ?err= redirect.
func TestDocumentRoutesExtractionFailOpen(t *testing.T) {
	f := &fakeMemory{effectiveModelErr: errTest}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/documents/:id/extract", s.uiTriggerExtraction)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/documents/d1/extract", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("fail-open extract status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/documents/d1?extracted=1" {
		t.Errorf("fail-open extract location = %q, want /documents/d1?extracted=1", loc)
	}
	if len(f.extracted) != 1 || f.extracted[0] != "d1" {
		t.Fatalf("extracted = %v, want [d1]", f.extracted)
	}
}

// TestUIDeleteDocument covers the delete-document flow shapes: HTMX submit →
// 200 + HX-Redirect to the list, plain POST → 303, the empty-id guard, and the
// memory-error flash path.
func TestUIDeleteDocument(t *testing.T) {
	newSrv := func(f *fakeMemory) *echo.Echo {
		s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
		e := echo.New()
		e.POST("/documents/:id/delete", s.uiDeleteDocument)
		return e
	}

	t.Run("HTMX delete", func(t *testing.T) {
		f := &fakeMemory{documents: []Document{{ID: "d1", Filename: "Spec.md"}}}
		e := newSrv(f)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/documents/d1/delete", nil)
		req.Header.Set("HX-Request", "true")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "/documents?deleted=1" {
			t.Errorf("HTMX delete = %d HX-Redirect %q, want 200 /documents?deleted=1", rec.Code, rec.Header().Get("HX-Redirect"))
		}
		if len(f.deletedDocuments) != 1 || f.deletedDocuments[0] != "d1" {
			t.Errorf("deletedDocuments = %v, want [d1]", f.deletedDocuments)
		}
	})

	t.Run("empty id guard", func(t *testing.T) {
		e := newSrv(&fakeMemory{})
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/documents//delete", nil))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("empty id = %d, want 303", rec.Code)
		}
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "?err=") {
			t.Errorf("empty id Location = %q, want ?err= flash", loc)
		}
	})

	t.Run("memory error flashes", func(t *testing.T) {
		f := &fakeMemory{deleteDocErr: errTest}
		e := newSrv(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/documents/d1/delete", nil))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("error path = %d, want 303", rec.Code)
		}
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "?err=") || !strings.Contains(loc, "backend+unreachable") {
			t.Errorf("error Location = %q, want ?err= with the memory error", loc)
		}
		if len(f.deletedDocuments) != 0 {
			t.Errorf("DeleteDocument called with %v, want none", f.deletedDocuments)
		}
	})
}

// TestDocumentStatusIntent covers the badge mapping for known and unknown
// extraction statuses.
func TestDocumentStatusIntent(t *testing.T) {
	if documentStatusIntent("completed") == documentStatusIntent("failed") {
		t.Error("completed and failed must map to different intents")
	}
	if documentStatusIntent("mystery") == documentStatusIntent("completed") {
		t.Error("unknown status should not map to completed")
	}
	if documentStatusIntent("") == documentStatusIntent("completed") {
		t.Error("empty status should not map to completed")
	}
}

// TestRenderExtractionJobDetailObjects covers the per-job detail's extracted
// object and relationship rendering (type + resolved endpoint labels).
func TestRenderExtractionJobDetailObjects(t *testing.T) {
	d := extractionJobDetail{
		Job: ExtractionJob{ID: "job1", Status: "completed"},
		Objects: []GraphObject{
			{ID: "o1", CanonicalID: "c1", Type: "person", Key: "sam"},
			{ID: "o2", CanonicalID: "c2", Type: "task", Key: "call dentist"},
		},
		Relationships: []GraphRelationship{
			{ID: "r1", Type: "assigned_to", SrcID: "c2", DstID: "c1"},
		},
	}
	html := renderHTML(t, ExtractionJobDetail("d1", d))
	for _, want := range []string{
		"2 objects", "1 relationship",
		"person", "sam", "task", "call dentist", "assigned to",
		`href="/objects/o1?from=%2Fdocuments%2Fd1%2Fextractions%2Fjob1"`,
		`href="/objects/o2?from=%2Fdocuments%2Fd1%2Fextractions%2Fjob1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("extraction job detail missing %q", want)
		}
	}
}

// TestRenderExtractionJobDetailOpenBrowser asserts the "Open in object
// browser" affordance renders only when the staging branch is resolved, and
// links to /objects?branch=<BranchID>.
func TestRenderExtractionJobDetailOpenBrowser(t *testing.T) {
	withBranch := extractionJobDetail{
		Job:      ExtractionJob{ID: "job1", Status: "completed"},
		BranchID: "br1",
		Branch:   &Branch{ID: "br1", Name: "extract-1"},
		Objects:  []GraphObject{{ID: "o1", CanonicalID: "c1", Type: "person", Key: "sam"}},
	}
	html := renderHTML(t, ExtractionJobDetail("d1", withBranch))
	if !strings.Contains(html, `data-testid="extraction-open-browser"`) {
		t.Error("resolved branch must render the object-browser link")
	}
	if !strings.Contains(html, `href="/objects?branch=br1"`) {
		t.Errorf("object-browser link must target /objects?branch=<BranchID>:\n%s", html)
	}

	branchless := extractionJobDetail{
		Job:      ExtractionJob{ID: "job1", Status: "completed"},
		NoBranch: true,
		Objects:  []GraphObject{{ID: "o1", CanonicalID: "c1", Type: "person", Key: "sam"}},
	}
	if html := renderHTML(t, ExtractionJobDetail("d1", branchless)); strings.Contains(html, `data-testid="extraction-open-browser"`) {
		t.Error("branchless job must not render the object-browser link")
	}
}

// TestRenderExtractionJobDetailNoBranchFallback asserts a merged (branchless)
// job still lists its objects — resolved on main — instead of collapsing to 0.
func TestRenderExtractionJobDetailNoBranchFallback(t *testing.T) {
	d := extractionJobDetail{
		Job:      ExtractionJob{ID: "job1", Status: "completed"},
		NoBranch: true,
		Objects: []GraphObject{
			{ID: "o1", CanonicalID: "c1", Type: "person", Key: "sam"},
			{ID: "o2", CanonicalID: "c2", Type: "task", Key: "call dentist"},
		},
		Relationships: []GraphRelationship{
			{ID: "r1", Type: "assigned_to", SrcID: "c2", DstID: "c1"},
		},
	}
	html := renderHTML(t, ExtractionJobDetail("d1", d))
	for _, want := range []string{
		"2 objects", "1 relationship",
		`data-testid="extraction-detail-nobranch"`,
		"showing the objects on the main graph",
		`href="/objects/o1?from=%2Fdocuments%2Fd1%2Fextractions%2Fjob1"`,
		`href="/objects/o2?from=%2Fdocuments%2Fd1%2Fextractions%2Fjob1"`,
		"assigned to",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("branchless detail missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, `data-testid="extraction-merge"`) {
		t.Error("branchless job must not render the merge form")
	}
}

// TestDocumentRoutesRegister asserts the new document sub-routes coexist with
// the existing document routes without a router conflict (Echo panics on a
// conflicting registration).
func TestDocumentRoutesRegister(t *testing.T) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: &fakeMemory{}}
	e := echo.New()
	e.GET("/documents", s.uiDocuments)
	e.GET("/documents/:id", s.uiDocument)
	e.GET("/documents/:id/chunks", s.uiDocumentChunks)
	e.GET("/documents/:id/extractions", s.uiDocumentExtractions)
	e.GET("/documents/:id/extractions/:jobId", s.uiDocumentExtractionPage)
	e.GET("/documents/:id/extractions/:jobId/detail", s.uiDocumentExtractionDetail)
	e.GET("/documents/:id/extractions/:jobId/trace", s.uiDocumentExtractionTrace)
	e.GET("/documents/:id/file", s.uiDocumentFile)
	e.POST("/documents", s.uiUploadDocument)
	e.POST("/documents/:id/extract", s.uiTriggerExtraction)
	e.POST("/documents/:id/extractions/:jobId/merge", s.uiDocumentMerge)
	e.POST("/documents/:id/delete", s.uiDeleteDocument)
}

// TestLoadExtractionJobDetail exercises per-job assembly: branch resolution
// from the job's first created object, branch-scoped objects, relationship
// filtering, readiness, and graceful degradation.
func TestLoadExtractionJobDetail(t *testing.T) {
	t.Run("no created objects", func(t *testing.T) {
		f := &fakeMemory{extractionJobs: []ExtractionJob{{ID: "job1", Status: "completed"}}}
		s := &Server{memory: f}
		d := s.loadExtractionJobDetail(context.Background(), "d1", "job1")
		if d.Err != nil || !d.NoBranch {
			t.Fatalf("want branchless detail, got %+v", d)
		}
	})

	t.Run("with branch", func(t *testing.T) {
		f := &fakeMemory{
			extractionJobs: []ExtractionJob{{ID: "job1", Status: "completed", CreatedObjectIDs: []string{"o1", "o2"}, StagingBranchID: "br1"}},
			objects: []GraphObject{
				{ID: "o1", CanonicalID: "c1", BranchID: "br1", Type: "person", Key: "sam"},
				{ID: "o2", CanonicalID: "c2", BranchID: "br1", Type: "task", Key: "call dentist"},
			},
			relns: []GraphRelationship{
				{ID: "r1", Type: "assigned_to", SrcID: "c2", DstID: "c1"},
				{ID: "r2", Type: "unrelated", SrcID: "x", DstID: "y"},
			},
			branches:       []Branch{{ID: "br1", Name: "extract-1"}},
			mergeReadiness: &MergeReadiness{Ready: true, TotalObjects: 2},
		}
		s := &Server{memory: f}
		d := s.loadExtractionJobDetail(context.Background(), "d1", "job1")
		if d.Err != nil {
			t.Fatalf("err = %v", d.Err)
		}
		if d.NoBranch || d.BranchID != "br1" || d.Branch == nil || d.Branch.Name != "extract-1" {
			t.Fatalf("branch resolution = %+v", d)
		}
		if len(d.Objects) != 2 {
			t.Fatalf("objects = %d, want 2", len(d.Objects))
		}
		// only the relationship touching an extracted object is kept
		if len(d.Relationships) != 1 || d.Relationships[0].Type != "assigned_to" {
			t.Fatalf("relationships = %+v", d.Relationships)
		}
		if d.Readiness == nil || !d.Readiness.Ready {
			t.Fatalf("readiness = %+v", d.Readiness)
		}
	})

	t.Run("merged branch falls back to main", func(t *testing.T) {
		// The staging branch was deleted on merge: the first created object no
		// longer carries a BranchID, so the job's objects resolve on main and
		// the counts survive instead of collapsing to 0.
		f := &fakeMemory{
			extractionJobs: []ExtractionJob{{ID: "job1", Status: "completed", CreatedObjects: []string{"o1", "o2"}}},
			objects: []GraphObject{
				{ID: "o1", CanonicalID: "c1", BranchID: "", Type: "person", Key: "sam"},
				{ID: "o2", CanonicalID: "c2", BranchID: "", Type: "task", Key: "call dentist"},
			},
			relns: []GraphRelationship{
				{ID: "r1", Type: "assigned_to", SrcID: "c2", DstID: "c1"},
			},
		}
		s := &Server{memory: f}
		d := s.loadExtractionJobDetail(context.Background(), "d1", "job1")
		if d.Err != nil {
			t.Fatalf("err = %v", d.Err)
		}
		if !d.NoBranch {
			t.Fatalf("want NoBranch=true, got %+v", d)
		}
		if d.BranchID != "" {
			t.Fatalf("branchID = %q, want empty", d.BranchID)
		}
		if len(d.Objects) != 2 {
			t.Fatalf("objects = %d, want 2 (fallen back to main)", len(d.Objects))
		}
		if len(d.Relationships) != 1 || d.Relationships[0].Type != "assigned_to" {
			t.Fatalf("relationships = %+v", d.Relationships)
		}
	})

	t.Run("unknown job", func(t *testing.T) {
		s := &Server{memory: &fakeMemory{}}
		d := s.loadExtractionJobDetail(context.Background(), "d1", "nope")
		if d.Err == nil {
			t.Fatal("expected an error for an unknown job")
		}
	})
}

// TestLoadExtractionJobDetailStagingBranch pins the fix for the "0 objects"
// regression: a modern job carries only created_object_ids / staging_branch_id
// (created_objects jsonb is NULL), so the detail must resolve its staging
// branch by id and list the id-backed objects — not collapse to "no staging
// branch".
func TestLoadExtractionJobDetailStagingBranch(t *testing.T) {
	f := &fakeMemory{
		extractionJobs: []ExtractionJob{{ID: "job1", Status: "completed", CreatedObjectIDs: []string{"o1", "o2"}, ObjectsCreated: 2, StagingBranchID: "br1"}},
		objects: []GraphObject{
			{ID: "o1", CanonicalID: "c1", BranchID: "br1", Type: "person", Key: "sam"},
			{ID: "o2", CanonicalID: "c2", BranchID: "br1", Type: "task", Key: "call dentist"},
		},
		relns:          []GraphRelationship{{ID: "r1", Type: "assigned_to", SrcID: "c2", DstID: "c1"}},
		branches:       []Branch{{ID: "br1", Name: "extract-1"}},
		mergeReadiness: &MergeReadiness{Ready: true, TotalObjects: 2},
	}
	s := &Server{memory: f}
	d := s.loadExtractionJobDetail(context.Background(), "d1", "job1")
	if d.Err != nil {
		t.Fatalf("err = %v", d.Err)
	}
	if d.NoBranch || d.BranchID != "br1" || d.Branch == nil || d.Branch.Name != "extract-1" {
		t.Fatalf("staging branch resolution = %+v", d)
	}
	if len(d.Objects) != 2 {
		t.Fatalf("objects = %d, want 2", len(d.Objects))
	}
	html := renderHTML(t, ExtractionJobDetail("d1", d))
	for _, want := range []string{
		"2 objects",
		`data-testid="extraction-merge"`,
		`data-testid="extraction-open-browser"`,
		`href="/objects?branch=br1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("staging-branch detail missing %q:\n%s", want, html)
		}
	}
}

// TestLoadExtractionJobDetailStagingBranchMerged covers a job whose staging
// branch was deleted on merge: ListBranches no longer contains it, its objects
// now live on main, so the detail degrades to the main-graph fallback without a
// merge form.
func TestLoadExtractionJobDetailStagingBranchMerged(t *testing.T) {
	f := &fakeMemory{
		extractionJobs: []ExtractionJob{{ID: "job1", Status: "completed", CreatedObjectIDs: []string{"o1", "o2"}, StagingBranchID: "br-gone"}},
		objects: []GraphObject{
			{ID: "o1", CanonicalID: "c1", BranchID: "", Type: "person", Key: "sam"},
			{ID: "o2", CanonicalID: "c2", BranchID: "", Type: "task", Key: "call dentist"},
		},
		relns:    []GraphRelationship{{ID: "r1", Type: "assigned_to", SrcID: "c2", DstID: "c1"}},
		branches: []Branch{{ID: "br-other", Name: "extract-9"}},
	}
	s := &Server{memory: f}
	d := s.loadExtractionJobDetail(context.Background(), "d1", "job1")
	if d.Err != nil {
		t.Fatalf("err = %v", d.Err)
	}
	if !d.NoBranch || d.BranchID != "" {
		t.Fatalf("want NoBranch=true with empty branch id, got %+v", d)
	}
	if len(d.Objects) != 2 {
		t.Fatalf("objects = %d, want 2 (fallen back to main)", len(d.Objects))
	}
	html := renderHTML(t, ExtractionJobDetail("d1", d))
	if strings.Contains(html, `data-testid="extraction-merge"`) {
		t.Error("merged staging branch must not render the merge form")
	}
	if !strings.Contains(html, `data-testid="extraction-detail-nobranch"`) {
		t.Errorf("merged staging branch must render the no-branch note:\n%s", html)
	}
}

// TestLoadExtractionJobDetailEmptyIDsNeverListsBranch asserts a job that has a
// live staging branch but no recorded object ids does not fall through to an
// unfiltered branch listing (which would show objects the job never created).
func TestLoadExtractionJobDetailEmptyIDsNeverListsBranch(t *testing.T) {
	f := &fakeMemory{
		extractionJobs: []ExtractionJob{{ID: "job1", Status: "completed", StagingBranchID: "br1"}},
		objects:        []GraphObject{{ID: "o1", CanonicalID: "c1", BranchID: "br1", Type: "person", Key: "other job's object"}},
		branches:       []Branch{{ID: "br1", Name: "extract-1"}},
	}
	s := &Server{memory: f}
	d := s.loadExtractionJobDetail(context.Background(), "d1", "job1")
	if d.Err != nil {
		t.Fatalf("err = %v", d.Err)
	}
	if d.NoBranch || d.BranchID != "br1" {
		t.Fatalf("branch resolution = %+v", d)
	}
	if len(d.Objects) != 0 {
		t.Fatalf("objects = %d, want 0: an empty id set must not list the branch", len(d.Objects))
	}
}

// TestLoadExtractionJobDetailLegacyCreatedObjects guards the legacy fallback: a
// pre-migration job with only created_objects (no staging_branch_id /
// created_object_ids) still resolves its branch from the first object.
func TestLoadExtractionJobDetailLegacyCreatedObjects(t *testing.T) {
	f := &fakeMemory{
		extractionJobs: []ExtractionJob{{ID: "job1", Status: "completed", CreatedObjects: []string{"o1", "o2"}}},
		objects: []GraphObject{
			{ID: "o1", CanonicalID: "c1", BranchID: "br1", Type: "person", Key: "sam"},
			{ID: "o2", CanonicalID: "c2", BranchID: "br1", Type: "task", Key: "call dentist"},
		},
		relns:    []GraphRelationship{{ID: "r1", Type: "assigned_to", SrcID: "c2", DstID: "c1"}},
		branches: []Branch{{ID: "br1", Name: "extract-1"}},
	}
	s := &Server{memory: f}
	d := s.loadExtractionJobDetail(context.Background(), "d1", "job1")
	if d.Err != nil {
		t.Fatalf("err = %v", d.Err)
	}
	if d.NoBranch || d.BranchID != "br1" {
		t.Fatalf("legacy fallback resolution = %+v", d)
	}
	if len(d.Objects) != 2 {
		t.Fatalf("objects = %d, want 2", len(d.Objects))
	}
	// The legacy path resolves the same branch record, so the merge action and
	// branch-name badge still render for pre-migration jobs.
	if d.Branch == nil || d.Branch.Name != "extract-1" {
		t.Fatalf("legacy branch record = %+v, want extract-1", d.Branch)
	}
	html := renderHTML(t, ExtractionJobDetail("d1", d))
	if !strings.Contains(html, `data-testid="extraction-merge"`) {
		t.Errorf("legacy job with a live branch must render the merge form:\n%s", html)
	}
}

// TestExtractionJobObjectIDs pins the id preference: modern created_object_ids
// wins over the legacy created_objects jsonb column.
func TestExtractionJobObjectIDs(t *testing.T) {
	j := ExtractionJob{
		CreatedObjectIDs: []string{"a", "b"},
		CreatedObjects:   []string{"legacy"},
	}
	if ids := j.ObjectIDs(); !slices.Equal(ids, []string{"a", "b"}) {
		t.Fatalf("ObjectIDs() = %v, want [a b]", ids)
	}
	legacy := ExtractionJob{CreatedObjects: []string{"x"}}
	if ids := legacy.ObjectIDs(); !slices.Equal(ids, []string{"x"}) {
		t.Fatalf("legacy ObjectIDs() = %v, want [x]", ids)
	}
	if j.ObjectCount() != 2 {
		t.Fatalf("ObjectCount() = %d, want 2", j.ObjectCount())
	}
	if count := (ExtractionJob{ObjectsCreated: 31}).ObjectCount(); count != 31 {
		t.Fatalf("ObjectCount() fallback = %d, want 31", count)
	}
}

// TestRenderExtractionJobRowBadgeCount asserts the history-row badge reports the
// id-backed object count, not the stale created_objects column.
func TestRenderExtractionJobRowBadgeCount(t *testing.T) {
	job := ExtractionJob{
		ID:               "job1",
		Status:           "completed",
		CreatedAt:        "2026-08-26T10:00:00Z",
		CreatedObjectIDs: []string{"o1", "o2", "o3"},
		ObjectsCreated:   31,
	}
	html := renderHTML(t, extractionJobRow("d1", job, nil))
	if !strings.Contains(html, "3 objects") {
		t.Errorf("row badge missing id count:\n%s", html)
	}
	if strings.Contains(html, "31 objects") {
		t.Errorf("row badge fell back to objects_created instead of ids:\n%s", html)
	}
}

// TestDocumentSubPageRoutes exercises the chunks/extractions sub-pages, the
// lazy per-job detail partial, the binary file proxy, and the merge PRG flow
// against the fake backend.
func TestDocumentSubPageRoutes(t *testing.T) {
	f := &fakeMemory{
		documents: []Document{{ID: "d1", Filename: "Spec.md", MimeType: "text/markdown", Chunks: 1}},
		chunks:    []Chunk{{ID: "c1", Index: 0, Size: 5, Text: "hello chunk"}},
		extractionJobs: []ExtractionJob{
			{ID: "job1", Status: "completed", CreatedAt: "2026-08-26T10:00:00Z", CreatedObjectIDs: []string{"o1"}, StagingBranchID: "br1"},
		},
		objects:        []GraphObject{{ID: "o1", CanonicalID: "c1", BranchID: "br1", Type: "person", Key: "sam"}},
		relns:          []GraphRelationship{{ID: "r1", Type: "assigned_to", SrcID: "c1", DstID: "c1"}},
		branches:       []Branch{{ID: "br1", Name: "extract-1"}},
		mergeReadiness: &MergeReadiness{Ready: true, TotalObjects: 1},
		mergeResult:    &BranchMergeResult{AddedCount: 1},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/documents/:id/chunks", s.uiDocumentChunks)
	e.GET("/documents/:id/extractions", s.uiDocumentExtractions)
	e.GET("/documents/:id/extractions/:jobId", s.uiDocumentExtractionPage)
	e.GET("/documents/:id/extractions/:jobId/detail", s.uiDocumentExtractionDetail)
	e.GET("/documents/:id/extractions/:jobId/trace", s.uiDocumentExtractionTrace)
	e.GET("/documents/:id/file", s.uiDocumentFile)
	e.POST("/documents/:id/extractions/:jobId/merge", s.uiDocumentMerge)

	t.Run("chunks page", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents/d1/chunks", nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "hello chunk") {
			t.Fatalf("chunks page: %d %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `aria-current="page"`) {
			t.Error("chunks sub-nav active state missing")
		}
	})

	t.Run("extractions page", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents/d1/extractions", nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `hx-get="/documents/d1/extractions/job1/detail"`) {
			t.Fatalf("extractions page: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("job detail partial", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/documents/d1/extractions/job1/detail", nil)
		req.Header.Set("HX-Request-Type", "partial")
		e.ServeHTTP(rec, req)
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("job detail status = %d", rec.Code)
		}
		for _, want := range []string{
			`data-testid="extraction-job-detail"`,
			`data-testid="extraction-merge"`,
			`hx-get="/documents/d1/extractions/job1/merge-preview"`,
			"extract-1", "assigned to",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("job detail missing %q", want)
			}
		}
	})

	t.Run("missing job detail degrades", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents/d1/extractions/nope/detail", nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Failed to load this extraction") {
			t.Fatalf("missing job detail: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("file proxy serves safe types inline and forces others to download", func(t *testing.T) {
		f.downloadDocBody = []byte("raw bytes")
		cases := []struct {
			name     string
			ct       string
			wantCT   string
			wantDisp string
		}{
			{"pdf", "application/pdf", "application/pdf", "inline"},
			{"png", "image/png", "image/png", "inline"},
			{"html", "text/html", "application/octet-stream", "attachment"},
			{"svg", "image/svg+xml", "application/octet-stream", "attachment"},
			{"unknown", "application/octet-stream", "application/octet-stream", "attachment"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				f.downloadDocCT = tc.ct
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents/d1/file", nil))
				if rec.Code != http.StatusOK || rec.Body.String() != "raw bytes" {
					t.Fatalf("file proxy: %d %q", rec.Code, rec.Body.String())
				}
				if ct := rec.Header().Get("Content-Type"); ct != tc.wantCT {
					t.Errorf("content type = %q, want %q", ct, tc.wantCT)
				}
				if cd := rec.Header().Get("Content-Disposition"); cd != tc.wantDisp {
					t.Errorf("content disposition = %q, want %q", cd, tc.wantDisp)
				}
				if nosniff := rec.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
					t.Errorf("X-Content-Type-Options = %q, want nosniff", nosniff)
				}
			})
		}
	})

	t.Run("missing file is 404", func(t *testing.T) {
		f.downloadDocBody = nil
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents/d1/file", nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("missing file = %d, want 404", rec.Code)
		}
	})

	t.Run("merge redirects with partial policy", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/documents/d1/extractions/job1/merge", nil))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("merge status = %d", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/documents/d1/extractions?merged=1" {
			t.Errorf("merge location = %q", loc)
		}
		if f.lastMergeBranchID != "br1" || f.lastMergePolicy != "partial" {
			t.Errorf("merge called with (%q,%q)", f.lastMergeBranchID, f.lastMergePolicy)
		}
	})

	t.Run("merge without branch flashes error", func(t *testing.T) {
		f2 := &fakeMemory{
			documents:      []Document{{ID: "d2", Filename: "Spec.md"}},
			extractionJobs: []ExtractionJob{{ID: "job1", Status: "completed"}},
		}
		s2 := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f2}
		e2 := echo.New()
		e2.POST("/documents/:id/extractions/:jobId/merge", s2.uiDocumentMerge)
		rec := httptest.NewRecorder()
		e2.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/documents/d2/extractions/job1/merge", nil))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("branchless merge status = %d", rec.Code)
		}
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "?err=") {
			t.Errorf("branchless merge location = %q, want ?err=", loc)
		}
		if f2.lastMergeBranchID != "" {
			t.Errorf("MergeBranch should not be called, got branch %q", f2.lastMergeBranchID)
		}
	})
}

// mergePreviewRowPattern extracts a summary row's (label, value) pair: the
// label span carries the "grow" class and the value span "tabular-nums".
var mergePreviewRowPattern = regexp.MustCompile(
	`<span class="[^"]*\bgrow\b[^"]*">([^<]+)</span>\s*<span class="[^"]*\btabular-nums\b[^"]*">([^<]+)</span>`)

// mergePreviewRowValues maps each rendered summary row label to its value.
func mergePreviewRowValues(t *testing.T, html string) map[string]string {
	t.Helper()
	matches := mergePreviewRowPattern.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		t.Fatalf("no merge preview rows found in:\n%s", html)
	}
	rows := make(map[string]string, len(matches))
	for _, m := range matches {
		rows[strings.TrimSpace(m[1])] = strings.TrimSpace(m[2])
	}
	return rows
}

// TestRenderMergePreviewSummary asserts the confirm-before-merge breakdown
// counts only what the "partial" merge actually applies.
//
// Fixture — 10 objects on the branch:
//
//	3 added        → applied as new objects
//	2 fast-forward → applied as updates to existing objects (the only existing
//	                 objects "partial" writes; FastForwardCount already includes
//	                 deleted→fast-forward objects)
//	4 similar      → NOT applied: "partial" resolves similarity to "suggest", so
//	                 they stay on the branch
//	1 conflict     → NOT applied: "partial" preserves the target
//	→ left for review = 4 similar + 1 conflict = 5
//
// This is the regression guard for #2084: mergePreviewUpdated used to return
// SimilarCount+FastForwardCount (4+2=6), so the same four similar objects were
// listed as both "Existing objects updated" (6) and "Left for review" (5),
// while the confirm label counted 3+2=5 applied objects.
func TestRenderMergePreviewSummary(t *testing.T) {
	res := &BranchMergeResult{
		TotalObjects:       10,
		AddedCount:         3,
		FastForwardCount:   2,
		SimilarCount:       4,
		ConflictCount:      1,
		LeftForReviewCount: 5, // 4 similar + 1 conflict, as the server computes it
		MergedCount:        0, // first merge: the ledger is empty
	}
	html := renderHTML(t, mergePreviewSummary(res))

	rows := mergePreviewRowValues(t, html)
	want := map[string]string{
		"New objects":              "3",
		"Existing objects updated": "2", // fast-forwards only — NOT 2+4 similar = 6
		"Conflicts":                "1",
		"Left for review":          "5", // 4 similar + 1 conflict
		"Total on branch":          "10",
	}
	for label, value := range want {
		if got := rows[label]; got != value {
			t.Errorf("row %q = %q, want %q (rows: %v)", label, got, value, rows)
		}
	}
	if len(rows) != len(want) {
		t.Errorf("rendered %d summary rows, want %d: %v", len(rows), len(want), rows)
	}

	// The bug in one assertion: the similarity matches must appear only under
	// "Left for review", never as updates — so the two rows cannot be equal.
	if rows["Existing objects updated"] == rows["Left for review"] {
		t.Errorf("updated (%s) and left-for-review (%s) rows show the same count; "+
			"similar objects are double-counted", rows["Existing objects updated"], rows["Left for review"])
	}
	// The updated row must equal the applied set minus the new-object row.
	if got, wantUpdated := mergePreviewUpdated(res), mergeAppliedObjects(res)-mergePreviewAdded(res); got != wantUpdated {
		t.Errorf("updated count = %d, want %d (applied %d − added %d)",
			got, wantUpdated, mergeAppliedObjects(res), mergePreviewAdded(res))
	}
	if !strings.Contains(html, "text-warning") {
		t.Errorf("summary with conflicts/leftover must keep warning styling:\n%s", html)
	}

	// A nil result still renders the full breakdown with zero counts and no
	// warning styling (nothing to flag).
	htmlNil := renderHTML(t, mergePreviewSummary(nil))
	rowsNil := mergePreviewRowValues(t, htmlNil)
	for _, label := range []string{"New objects", "Existing objects updated", "Conflicts", "Left for review", "Total on branch"} {
		if got, ok := rowsNil[label]; !ok || got != "0" {
			t.Errorf("nil merge preview row %q = %q (present=%v), want \"0\"", label, got, ok)
		}
	}
	if strings.Contains(htmlNil, "text-warning") {
		t.Errorf("nil merge preview summary should not warn:\n%s", htmlNil)
	}
}

// TestRenderMergePreviewModalAutoOpens pins the contract the client relies on:
// the lazily-fetched modal carries data-dialog-autoopen, so app.js calls
// showModal() once the htmx swap inserts it, plus the confirm/cancel wiring and
// the truthful confirm label. The error state must still auto-open with its
// explanation, but must not offer a confirm action.
func TestRenderMergePreviewModalAutoOpens(t *testing.T) {
	const actionURL = "/documents/d1/extractions/job1/merge"
	html := renderHTML(t, mergePreviewModal(actionURL, &BranchMergeResult{
		TotalObjects: 3,
		AddedCount:   2,
		SimilarCount: 1,
	}, nil))
	for _, want := range []string{
		`data-dialog-autoopen="true"`,
		`data-testid="merge-preview-modal"`,
		`action="` + actionURL + `"`,
		`data-testid="merge-preview-confirm"`,
		"Merge 2 objects into main",
		"New objects", "Total on branch",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("merge preview modal missing %q:\n%s", want, html)
		}
	}

	errHTML := renderHTML(t, mergePreviewModal(actionURL, nil, errors.New("preview unavailable")))
	for _, want := range []string{
		`data-dialog-autoopen="true"`,
		`data-testid="merge-preview-modal"`,
		"preview unavailable",
	} {
		if !strings.Contains(errHTML, want) {
			t.Errorf("error merge preview modal missing %q:\n%s", want, errHTML)
		}
	}
	if strings.Contains(errHTML, `data-testid="merge-preview-confirm"`) {
		t.Error("error merge preview modal must not offer a confirm action")
	}
}

// TestMergeAppliedObjects pins the count the confirm gate and the post-merge
// PRG flash use: the objects the "partial" policy actually applies (added +
// fast-forward), never MergedCount. MergedCount is the merge-ledger count —
// objects cloned by a PRIOR merge — so on a first merge it is 0, which disabled
// the confirm button permanently and flashed "Merged 0 object(s) into main"
// after a successful merge.
func TestMergeAppliedObjects(t *testing.T) {
	first := &BranchMergeResult{TotalObjects: 3, AddedCount: 2, FastForwardCount: 1}
	if got := mergeAppliedObjects(first); got != 3 {
		t.Errorf("first preview clean count = %d, want 3", got)
	}
	if mergePreviewDisabled(first) {
		t.Error("a first merge with clean additions must be confirmable")
	}
	if got := mergePreviewConfirmLabel(first); got != "Merge 3 objects into main" {
		t.Errorf("first preview confirm label = %q", got)
	}
	// The PRG query after an actual first merge must report the same count, not
	// the (zero) merge-ledger MergedCount.
	if got := mergeResultQuery(first, "job1"); got != "?merged=3" {
		t.Errorf("first merge result query = %q, want ?merged=3", got)
	}

	// Everything already cloned by a prior merge: nothing clean left to apply,
	// so the confirm button is correctly disabled.
	prior := &BranchMergeResult{TotalObjects: 2, MergedCount: 2}
	if got := mergeAppliedObjects(prior); got != 0 {
		t.Errorf("prior-merge preview clean count = %d, want 0", got)
	}
	if !mergePreviewDisabled(prior) {
		t.Error("a preview with nothing clean to merge must disable confirm")
	}

	// Conflicts and similarity matches stay on the branch under "partial", so
	// they never count as clean merges.
	review := &BranchMergeResult{TotalObjects: 5, ConflictCount: 2, SimilarCount: 3}
	if got := mergeAppliedObjects(review); got != 0 {
		t.Errorf("review-only preview clean count = %d, want 0", got)
	}
}

// TestRenderExtractionSummaryStrip asserts the summary strip badges: start/
// finish/duration, progress counters, objects/relationships, model, token
// counts, estimated cost, and retry count.
func TestRenderExtractionSummaryStrip(t *testing.T) {
	cost := 0.0123
	d := extractionJobDetail{
		Job: ExtractionJob{
			ID:             "job1",
			Status:         "completed",
			StartedAt:      "2026-08-26T10:00:00Z",
			CompletedAt:    "2026-08-26T10:01:30Z",
			TotalItems:     10,
			ProcessedItems: 10,
			FailedItems:    1,
			Spans: []TraceSpan{
				{Name: "extract", SpanID: "s1"},
				{Name: "call_llm", SpanID: "s2", ParentSpanID: "s1", Model: "deepseek-v4-flash", InputTokens: 123, OutputTokens: 456},
			},
			TraceCostUSD: &cost,
			RetryCount:   2,
		},
		Objects:       []GraphObject{{ID: "o1"}},
		Relationships: []GraphRelationship{{ID: "r1"}},
	}
	html := renderHTML(t, extractionSummaryStrip(d))
	for _, want := range []string{
		`data-testid="extraction-summary-strip"`,
		"started ", "finished ", "1m", // duration 90s → "1m"
		"10/10 processed", "1 failed",
		"1 object", "1 relationship",
		"deepseek-v4-flash", "123 in · 456 out",
		"$0.0123", "2 retries",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("summary strip missing %q:\n%s", want, html)
		}
	}
}

// TestExtractionTracePanelPolling pins the polling-attribute lifecycle: the
// run-body root (summary strip + trace panel) emits hx-get/hx-trigger/hx-swap
// while the job is active and omits them once terminal, so the interval
// self-terminates and the final numbers land inside the swapped element.
func TestExtractionTracePanelPolling(t *testing.T) {
	t.Run("processing renders polling attributes", func(t *testing.T) {
		d := extractionJobDetail{Job: ExtractionJob{ID: "job1", Status: "processing"}}
		html := renderHTML(t, extractionRunBody("d1", "job1", d, extractionJobActive(d.Job.Status), false))
		for _, want := range []string{
			`id="extraction-run-body"`,
			`data-testid="extraction-run-body"`,
			`data-testid="extraction-summary-strip"`,
			`id="extraction-trace"`,
			`data-testid="extraction-trace"`,
			`hx-get="/documents/d1/extractions/job1/trace"`,
			`hx-trigger="every 3s"`,
			`hx-swap="outerHTML"`,
			"Waiting for the first spans…",
		} {
			if !strings.Contains(html, want) {
				t.Errorf("processing run body missing %q:\n%s", want, html)
			}
		}
	})

	t.Run("completed omits polling attributes", func(t *testing.T) {
		d := extractionJobDetail{Job: ExtractionJob{ID: "job1", Status: "completed"}}
		html := renderHTML(t, extractionRunBody("d1", "job1", d, extractionJobActive(d.Job.Status), false))
		for _, unwanted := range []string{`hx-get`, `hx-trigger`, "every 3s"} {
			if strings.Contains(html, unwanted) {
				t.Errorf("completed run body must not emit %q:\n%s", unwanted, html)
			}
		}
		if !strings.Contains(html, "No trace recorded for this run.") {
			t.Errorf("completed run body missing no-trace note:\n%s", html)
		}
		if !strings.Contains(html, "Tracing is recorded only when the server has OTEL enabled.") {
			t.Errorf("completed run body missing tracing-off hint:\n%s", html)
		}
	})

	t.Run("completed with spans renders waterfall without polling", func(t *testing.T) {
		d := extractionJobDetail{Job: ExtractionJob{
			ID:     "job1",
			Status: "completed",
			Spans:  []TraceSpan{{SpanID: "s1", Name: "extract", StartUnixNano: 1700000000000000000, EndUnixNano: 1700000001230000000}},
		}}
		html := renderHTML(t, extractionRunBody("d1", "job1", d, extractionJobActive(d.Job.Status), false))
		if strings.Contains(html, `hx-get`) {
			t.Errorf("completed run body with spans must not poll:\n%s", html)
		}
		for _, want := range []string{"Trace", "1 span", "extract"} {
			if !strings.Contains(html, want) {
				t.Errorf("completed run body with spans missing %q:\n%s", want, html)
			}
		}
	})

	t.Run("oob status refreshes the header badge", func(t *testing.T) {
		d := extractionJobDetail{Job: ExtractionJob{ID: "job1", Status: "completed"}}
		html := renderHTML(t, extractionRunBody("d1", "job1", d, false, true))
		if !strings.Contains(html, `id="extraction-run-status"`) || !strings.Contains(html, `hx-swap-oob="true"`) {
			t.Errorf("oob run body missing out-of-band status group:\n%s", html)
		}
		if strings.Contains(html, `data-testid="extraction-live"`) {
			t.Errorf("terminal status must not render the Live badge:\n%s", html)
		}
	})
}

// TestDocumentExtractionPageRoutes exercises the three extraction-run routes
// against the fake backend: the full page, the moved /detail partial, and the
// polling /trace fragment.
func TestDocumentExtractionPageRoutes(t *testing.T) {
	cost := 0.0123
	msg := "extraction failed upstream"
	f := &fakeMemory{
		documents: []Document{{ID: "d1", Filename: "Spec.md", MimeType: "text/markdown"}},
		extractionJobs: []ExtractionJob{{
			ID:               "job1",
			Status:           "processing",
			CreatedAt:        "2026-08-26T10:00:00Z",
			StartedAt:        "2026-08-26T10:00:00Z",
			CreatedObjectIDs: []string{"o1"},
			StagingBranchID:  "br1",
		}},
		extractionJob: &ExtractionJob{
			ID:               "job1",
			Status:           "processing",
			CreatedAt:        "2026-08-26T10:00:00Z",
			StartedAt:        "2026-08-26T10:00:00Z",
			TotalItems:       10,
			ProcessedItems:   4,
			FailedItems:      1,
			TraceID:          "tr_1",
			TraceCostUSD:     &cost,
			RetryCount:       1,
			CreatedObjectIDs: []string{"o1"},
			StagingBranchID:  "br1",
			ErrorMessage:     &msg,
			ErrorDetails:     map[string]any{"code": "upstream"},
			Spans: []TraceSpan{
				{SpanID: "sp_root", Name: "extract", StartUnixNano: 1700000000000000000, EndUnixNano: 1700000001230000000},
				{SpanID: "sp_llm", ParentSpanID: "sp_root", Name: "call_llm", StartUnixNano: 1700000001000000000, EndUnixNano: 1700000001045000000, InputTokens: 123, OutputTokens: 456, Model: "deepseek-v4-flash"},
			},
		},
		objects:        []GraphObject{{ID: "o1", CanonicalID: "c1", BranchID: "br1", Type: "person", Key: "sam"}},
		branches:       []Branch{{ID: "br1", Name: "extract-1"}},
		mergeReadiness: &MergeReadiness{Ready: true, TotalObjects: 1},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/documents/:id/extractions/:jobId", s.uiDocumentExtractionPage)
	e.GET("/documents/:id/extractions/:jobId/detail", s.uiDocumentExtractionDetail)
	e.GET("/documents/:id/extractions/:jobId/trace", s.uiDocumentExtractionTrace)

	t.Run("page renders summary, trace, error, and objects", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents/d1/extractions/job1", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("page status = %d, body=%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, want := range []string{
			"All extractions",
			`id="extraction-run-body"`,
			`data-testid="extraction-run-body"`,
			`id="extraction-run-status"`,
			`data-testid="extraction-summary-strip"`,
			`data-testid="extraction-trace"`,
			`hx-get="/documents/d1/extractions/job1/trace"`,
			`hx-trigger="every 3s"`,
			"deepseek-v4-flash", "123 in · 456 out", "$0.0123",
			"extraction failed upstream", "Error details", "upstream",
			"extract-1", "sam", "Ready to merge",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("page missing %q", want)
			}
		}
		region := breadcrumbRegion(body)
		for _, want := range []string{"Documents", "Spec.md", "Extraction history"} {
			if !strings.Contains(region, ">"+want+"<") {
				t.Errorf("run page breadcrumb missing %q: %s", want, region)
			}
		}
		if !strings.Contains(region, ">Extraction history</span>") {
			t.Errorf("run page section crumb must be the current, unlinked item: %s", region)
		}
		if strings.Contains(region, "job1") || strings.Contains(region, "Run ") {
			t.Errorf("run page breadcrumb must not repeat the run short id: %s", region)
		}
		if strings.Contains(body, `hx-swap-oob`) {
			t.Errorf("full page must not emit an out-of-band status swap:\n%s", body)
		}
	})

	t.Run("detail partial still serves on /detail", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/documents/d1/extractions/job1/detail", nil)
		req.Header.Set("HX-Request-Type", "partial")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("detail status = %d, body=%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, `data-testid="extraction-job-detail"`) {
			t.Errorf("detail partial missing job-detail testid:\n%s", body)
		}
		if !strings.Contains(body, "extract-1") {
			t.Errorf("detail partial missing branch name:\n%s", body)
		}
	})

	t.Run("unknown job degrades to a page error", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents/d1/extractions/nope", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("unknown job status = %d, body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "Extraction run unavailable") {
			t.Errorf("unknown job page missing error heading:\n%s", rec.Body.String())
		}
	})

	t.Run("trace fragment returns the polling body and oob status", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/documents/d1/extractions/job1/trace", nil)
		req.Header.Set("HX-Request-Type", "partial")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("trace status = %d, body=%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, want := range []string{
			`data-testid="extraction-run-body"`,
			`data-testid="extraction-summary-strip"`,
			`data-testid="extraction-trace"`,
			`hx-trigger="every 3s"`,
			`id="extraction-run-status"`,
			`hx-swap-oob="true"`,
			"Trace", "2 span",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("trace fragment missing %q:\n%s", want, body)
			}
		}
	})

	t.Run("terminal trace fragment stops polling and shows final numbers", func(t *testing.T) {
		original := f.extractionJob
		completed := *original
		completed.Status = "completed"
		completed.CompletedAt = "2026-08-26T10:01:30Z"
		f.extractionJob = &completed
		defer func() { f.extractionJob = original }()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/documents/d1/extractions/job1/trace", nil)
		req.Header.Set("HX-Request-Type", "partial")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("terminal trace status = %d, body=%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, unwanted := range []string{`hx-trigger`, "every 3s", `data-testid="extraction-live"`} {
			if strings.Contains(body, unwanted) {
				t.Errorf("terminal fragment must not emit %q:\n%s", unwanted, body)
			}
		}
		for _, want := range []string{
			`data-testid="extraction-summary-strip"`,
			"finished ", "deepseek-v4-flash", "123 in · 456 out", "$0.0123",
			`hx-swap-oob="true"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("terminal fragment missing final %q:\n%s", want, body)
			}
		}
	})

	t.Run("trace fragment error leaves the last summary intact", func(t *testing.T) {
		original := f.docErr
		f.docErr = errTest
		defer func() { f.docErr = original }()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/documents/d1/extractions/job1/trace", nil)
		req.Header.Set("HX-Request-Type", "partial")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("error trace status = %d, body=%s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("HX-Reswap"); got != "none" {
			t.Errorf("error trace HX-Reswap = %q, want %q", got, "none")
		}
		body := rec.Body.String()
		for _, unwanted := range []string{
			`data-testid="extraction-run-body"`,
			`data-testid="extraction-summary-strip"`,
			`id="extraction-run-status"`,
			`hx-swap-oob`,
			"0 object",
			"0 relationship",
		} {
			if strings.Contains(body, unwanted) {
				t.Errorf("error trace must not emit %q:\n%s", unwanted, body)
			}
		}
	})
}

// --- breadcrumbs ---

// breadcrumbRegion returns the rendered breadcrumb trail, or "" when the page
// renders none. Scoping assertions to the trail matters because the same labels
// also appear in the sub-navigation and the page title.
func breadcrumbRegion(html string) string {
	start := strings.Index(html, `class="breadcrumbs`)
	if start < 0 {
		return ""
	}
	end := strings.Index(html[start:], "</ul></div>")
	if end < 0 {
		return ""
	}
	end += start + len("</ul></div>")
	return html[start:end]
}

// TestDocumentSectionBreadcrumbs pins the trail on every document view: the
// documents list, the document (linking back to its overview), and the section
// on the three sub-pages. The overview stops at the document.
func TestDocumentSectionBreadcrumbs(t *testing.T) {
	doc := &Document{ID: "d1", Filename: "Spec.md"}
	cases := []struct {
		section string
		trail   []string
	}{
		{"overview", []string{"Documents", "Spec.md"}},
		{"chunks", []string{"Documents", "Spec.md", "Chunks"}},
		{"extractions", []string{"Documents", "Spec.md", "Extraction history"}},
		{"versions", []string{"Documents", "Spec.md", "Versions"}},
	}
	for _, tc := range cases {
		t.Run(tc.section, func(t *testing.T) {
			region := breadcrumbRegion(renderHTML(t, documentDetailHeader(doc, tc.section)))
			if region == "" {
				t.Fatal("no breadcrumb trail rendered")
			}
			last := -1
			for _, label := range tc.trail {
				i := strings.Index(region, ">"+label+"<")
				if i < 0 {
					t.Fatalf("missing crumb %q in %s", label, region)
				}
				if i <= last {
					t.Fatalf("crumb %q out of order in %s", label, region)
				}
				last = i
			}
			if !strings.Contains(region, `href="/documents"`) {
				t.Errorf("documents crumb must link to the list: %s", region)
			}
			if tc.section == "overview" {
				// The document is the current page on the overview, so it is not
				// a link there; on a sub-page it links back to the overview.
				if strings.Contains(region, `href="/documents/d1"`) {
					t.Errorf("overview document crumb must not link to itself: %s", region)
				}
			} else if !strings.Contains(region, `href="/documents/d1"`) {
				t.Errorf("document crumb must link to the overview: %s", region)
			}
			if !strings.Contains(region, `class="font-medium text-base-content"`) {
				t.Errorf("trailing crumb must be marked active: %s", region)
			}
			if tc.section != "overview" && !strings.Contains(region, ">"+tc.trail[len(tc.trail)-1]+"<") {
				t.Errorf("section crumb missing: %s", region)
			}
		})
	}
}

// TestRenderDocumentVersionsBreadcrumb pins that the shared header is told the
// versions section on the versions page, so the trail is not a bare
// Documents > <document>.
func TestRenderDocumentVersionsBreadcrumb(t *testing.T) {
	doc := &Document{ID: "d1", Filename: "Spec.md"}
	html := renderHTML(t, DocumentVersionsPage(doc, nil, nil))
	region := breadcrumbRegion(html)
	for _, want := range []string{"Documents", "Spec.md", "Versions"} {
		if !strings.Contains(region, ">"+want+"<") {
			t.Errorf("versions breadcrumb missing %q: %s", want, region)
		}
	}
}

// TestRenderExtractionObjectsCarryOrigin pins that an object opened from a
// run's object list links back with the origin trail, so the object page can
// render Documents > <doc> > Extraction history rather than a bare Objects
// trail.
func TestRenderExtractionObjectsCarryOrigin(t *testing.T) {
	d := extractionJobDetail{
		Job:     ExtractionJob{ID: "job1", Status: "completed"},
		Objects: []GraphObject{{ID: "o1", Type: "person", Key: "sam"}},
	}
	html := renderHTML(t, ExtractionJobDetail("d1", d))
	want := `href="/objects/o1?from=%2Fdocuments%2Fd1%2Fextractions%2Fjob1"`
	if !strings.Contains(html, want) {
		t.Fatalf("extracted object link missing origin trail %q:\n%s", want, html)
	}
}

// anchorNesting reports the deepest <a> nesting in markup.
//
// It tokenizes instead of building a tree on purpose: html.Parse runs HTML5 tree
// construction, which repairs nested anchors (the outer one is closed), so a
// tree walk can never see the nesting it is meant to catch. The tokenizer sees
// the tags as written.
func anchorNesting(markup string) (int, error) {
	z := html.NewTokenizer(strings.NewReader(markup))
	depth, deepest := 0, 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if err := z.Err(); err != io.EOF {
				return deepest, err
			}
			return deepest, nil
		}
		if tt != html.StartTagToken && tt != html.EndTagToken {
			continue
		}
		name, _ := z.TagName()
		if string(name) != "a" {
			continue
		}
		if tt == html.EndTagToken {
			if depth > 0 {
				depth--
			}
			continue
		}
		depth++
		if depth > deepest {
			deepest = depth
		}
	}
}

// assertNoNestedAnchors fails when an <a> opens inside another <a>. Browsers
// repair that by closing the outer anchor, splitting whatever was inside it into
// stray focusable fragments.
func assertNoNestedAnchors(t *testing.T, markup string) {
	t.Helper()
	deepest, err := anchorNesting(markup)
	if err != nil {
		t.Fatalf("tokenize rendered page: %v", err)
	}
	if deepest > 1 {
		t.Errorf("anchors nest %d deep; an <a> must never contain another <a>", deepest)
	}
}

// TestAnchorNestingSeesNestedAnchors proves the guard can actually fail: the
// fixture is the card markup from before the fix, where the source link sat
// inside the card's anchor.
func TestAnchorNestingSeesNestedAnchors(t *testing.T) {
	preFix := `<div data-document-id="d1"><a class="card" href="/documents/d1"><p>spec.md</p>` +
		`<a class="link" href="/data-sources/ds-1">Product Drive</a></a></div>`
	deepest, err := anchorNesting(preFix)
	if err != nil {
		t.Fatalf("tokenize fixture: %v", err)
	}
	if deepest != 2 {
		t.Errorf("nesting in the pre-fix markup = %d, want 2 (the guard would not have caught it)", deepest)
	}
	if deepest, err := anchorNesting(`<div><a href="/a">a</a><a href="/b">b</a></div>`); err != nil || deepest != 1 {
		t.Errorf("sibling anchors = %d (err %v), want 1", deepest, err)
	}
}

// TestRenderExtractionMergeRowLayout pins #2088, a recorded live style change:
// the merge row stays on one line (explicit row direction, no wrapping) and the
// explanatory note is right-aligned inside it.
func TestRenderExtractionMergeRowLayout(t *testing.T) {
	html := renderHTML(t, extractionMergeAction("doc-1", "job-1", "branch-1", 0))
	// One row from sm up; below it the row wraps, because a non-wrapping row at
	// 320 px squeezed the note into a tall stack of ragged lines (#2088 review).
	if !strings.Contains(html, "flex flex-row flex-wrap items-center justify-between gap-3") {
		t.Error("the merge row should be an explicit row that wraps on narrow screens")
	}
	if !strings.Contains(html, "sm:flex-nowrap") {
		t.Error("the merge row should stop wrapping from sm up")
	}
	if !strings.Contains(html, "text-right") {
		t.Error("the merge note should be right-aligned")
	}
	if !strings.Contains(html, `data-testid="extraction-merge-button"`) {
		t.Error("the merge action must survive the layout change")
	}
}

// TestRenderExtractionMergeLeftLinksToBrowser pins #242: the merge row showed
// "N left for review" but offered no way to see the objects. The badge is now a
// link into the branch-filtered object browser, and it is absent at zero.
func TestRenderExtractionMergeLeftLinksToBrowser(t *testing.T) {
	html := renderHTML(t, extractionMergeAction("doc-1", "job-1", "branch-7", 3))
	for _, want := range []string{
		`href="/objects?branch=branch-7"`,
		"3 left for review",
		`data-testid="extraction-merge-left"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("left-for-review link missing %q:\n%s", want, html)
		}
	}

	zero := renderHTML(t, extractionMergeAction("doc-1", "job-1", "branch-7", 0))
	if strings.Contains(zero, "/objects?branch=") {
		t.Errorf("no review badge should render at zero leftovers:\n%s", zero)
	}
}

// TestRenderExtractionDetailReadinessOnlyWhileOpen pins #242: after a merge the
// run still advertised "Ready to merge" next to "Merged into main". Readiness
// answers "can this merge now?", so it must disappear once merged, and the
// branch badge must name the source document rather than the opaque
// extraction/<doc>/<job> name.
func TestRenderExtractionDetailReadinessOnlyWhileOpen(t *testing.T) {
	branch := Branch{
		ID:          "b1",
		Name:        "extraction/aaaa/bbbb",
		MergedAt:    "2026-11-06T10:00:00Z",
		Origin:      "extraction",
		OriginLabel: "Invoice.pdf",
	}
	detail := extractionJobDetail{
		Job:       ExtractionJob{ID: "job1"},
		Branch:    &branch,
		BranchID:  "b1",
		Readiness: &MergeReadiness{Ready: true, TotalObjects: 3},
	}

	merged := renderHTML(t, ExtractionJobDetail("doc-1", detail))
	if strings.Contains(merged, "Ready to merge") {
		t.Errorf("a merged branch must not advertise merge readiness (#242):\n%s", merged)
	}
	if !strings.Contains(merged, "Merged into main") {
		t.Error("a merged branch should show its merged state")
	}
	if !strings.Contains(merged, "Extraction · Invoice.pdf") {
		t.Errorf("branch badge should name the source document, not the branch name:\n%s", merged)
	}

	branch.MergedAt = ""
	detail.Branch = &branch
	open := renderHTML(t, ExtractionJobDetail("doc-1", detail))
	if !strings.Contains(open, "Ready to merge") {
		t.Errorf("an open branch must still show readiness:\n%s", open)
	}
}

// TestAssembleExtractionJobDetailSkipsReadinessWhenMerged pins the fetch half of
// the #242 fix: a merged branch must not issue the merge-readiness request at
// all, not merely hide its result. Rendering is covered separately.
func TestAssembleExtractionJobDetailSkipsReadinessWhenMerged(t *testing.T) {
	cases := []struct {
		name               string
		mergedAt           string
		wantReadinessCalls int
	}{
		{name: "merged branch skips the request", mergedAt: "2026-11-06T10:00:00Z", wantReadinessCalls: 0},
		{name: "open branch still asks", mergedAt: "", wantReadinessCalls: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var readinessCalls int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/merge-readiness"):
					readinessCalls++
					_, _ = io.WriteString(w, `{"ready":true,"total_objects":3,"pending_embeddings":0}`)
				case r.URL.Path == "/api/graph/branches":
					_, _ = io.WriteString(w, `[{"id":"b1","name":"extraction/aaaa/bbbb","origin":"extraction","origin_label":"Invoice.pdf","merged_at":"`+tc.mergedAt+`"}]`)
				case strings.HasPrefix(r.URL.Path, "/api/graph/objects/search"):
					_, _ = io.WriteString(w, `{"items":[],"total":0}`)
				case strings.HasPrefix(r.URL.Path, "/api/graph/relationships/search"):
					_, _ = io.WriteString(w, `{"items":[],"total":0}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()

			s := &Server{
				cfg:    Config{MemoryURL: srv.URL, MemoryProjectID: "proj-1"},
				memory: NewMemoryClient(srv.URL, "proj-1"),
			}
			job := ExtractionJob{
				ID:               "job1",
				StagingBranchID:  "b1",
				CreatedObjectIDs: []string{"obj-1"},
			}

			d := s.assembleExtractionJobDetail(context.Background(), job)
			if d.Branch == nil {
				t.Fatalf("branch did not resolve: %+v", d)
			}
			if readinessCalls != tc.wantReadinessCalls {
				t.Errorf("merge-readiness requests = %d, want %d", readinessCalls, tc.wantReadinessCalls)
			}
			if tc.wantReadinessCalls == 0 && d.Readiness != nil {
				t.Error("a merged branch must not carry readiness")
			}
			if tc.wantReadinessCalls == 1 && d.Readiness == nil {
				t.Error("an open branch should still carry readiness")
			}
		})
	}
}
