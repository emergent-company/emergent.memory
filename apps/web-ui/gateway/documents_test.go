package main

import (
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- MemoryClient document methods ---

func TestListDocuments(t *testing.T) {
	var gotPath, gotLimit, gotCursor, gotProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotLimit = r.URL.Query().Get("limit")
		gotCursor = r.URL.Query().Get("cursor")
		gotProject = r.Header.Get("X-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"documents":[
			{"id":"d1","filename":"Spec.md","mimeType":"text/markdown","chunks":12,"embeddedChunks":10,"extractionStatus":"completed","objectsCreated":15,"createdAt":"2026-08-26T10:00:00Z"},
			{"id":"d2","filename":"notes.txt","chunks":3,"extractionStatus":"pending","createdAt":"2026-08-26T11:00:00Z"}
		],"total":2,"next_cursor":"abc123"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	docs, cursor, err := m.ListDocuments(context.Background(), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/documents" {
		t.Errorf("path = %q", gotPath)
	}
	if gotLimit != "100" {
		t.Errorf("limit = %q, want 100", gotLimit)
	}
	if gotCursor != "abc123" {
		t.Errorf("cursor = %q, want abc123", gotCursor)
	}
	if gotProject != "proj-1" {
		t.Errorf("X-Project-ID = %q, want proj-1", gotProject)
	}
	if cursor != "abc123" {
		t.Errorf("next cursor = %q, want abc123", cursor)
	}
	if len(docs) != 2 {
		t.Fatalf("got %d docs, want 2", len(docs))
	}
	if docs[0].Filename != "Spec.md" || docs[0].Chunks != 12 || docs[0].ExtractionStatus != "completed" || docs[0].ObjectsCreated != 15 {
		t.Errorf("doc0 = %+v", docs[0])
	}
}

func TestListDocumentsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":{"code":"upstream","message":"down"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, _, err := m.ListDocuments(context.Background(), ""); err == nil {
		t.Fatal("want error, got nil")
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

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
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

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
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
	m := NewMemoryClient(srv.URL, "tok", "proj")
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

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
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

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
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

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
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
	m := NewMemoryClient(srv.URL, "tok", "proj")
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

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
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
	if apiKey == "" {
		// Test convenience: register a device key and inject it when the
		// request omits X-API-Key, so the handler tests exercise the real
		// requireClientKey middleware without per-request headers.
		key := randomHex(32)
		_ = f.SetProjectSetting(context.Background(), "ios_device_keys", "registry", map[string]any{
			"devices": map[string]any{key: map[string]any{"createdAt": "2026-08-30T00:00:00Z"}},
		})
		e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				if c.Request().Header.Get("X-API-Key") == "" {
					c.Request().Header.Set("X-API-Key", key)
				}
				return next(c)
			}
		})
	}
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

func TestRenderDocumentsPage(t *testing.T) {
	docs := []Document{
		{ID: "d1", Filename: "Spec.md", Chunks: 12, ExtractionStatus: "completed", CreatedAt: "2026-08-26T10:00:00Z"},
		{ID: "d2", Filename: "notes.txt", Chunks: 3, ExtractionStatus: "pending", CreatedAt: "2026-08-26T11:00:00Z"},
	}
	html := renderHTML(t, DocumentsPage(docs, "", nil, "", nil))
	for _, want := range []string{
		"Documents", "Spec.md", "notes.txt", "12 chunks", "3 chunks",
		"completed", "pending", `href="/documents/d1"`, `name="file"`, `enctype="multipart/form-data"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("documents page missing %q", want)
		}
	}
	// per-row delete affordance on every row, with the confirm copy
	for _, want := range []string{
		`action="/documents/d1/delete"`, `action="/documents/d2/delete"`,
		"Delete this document? This cannot be undone.", "lucide--trash-2",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("documents page missing %q", want)
		}
	}

	htmlDeleted := renderHTML(t, DocumentsPage(nil, "", nil, "Document deleted.", nil))
	if !strings.Contains(htmlDeleted, "Document deleted.") {
		t.Error("delete flash missing")
	}

	htmlEmpty := renderHTML(t, DocumentsPage(nil, "", nil, "", nil))
	if !strings.Contains(htmlEmpty, "No documents yet") {
		t.Error("empty state missing")
	}

	htmlErr := renderHTML(t, DocumentsPage(nil, "", errTest, "", nil))
	if !strings.Contains(htmlErr, "Failed to load documents") {
		t.Error("error state missing")
	}

	htmlFlash := renderHTML(t, DocumentsPage(nil, "", nil, "", errTest))
	if !strings.Contains(htmlFlash, "backend unreachable") {
		t.Error("upload flash should render the real error, missing")
	}

	htmlUploaded := renderHTML(t, DocumentsPage(nil, "", nil, "Document uploaded.", nil))
	if !strings.Contains(htmlUploaded, "Document uploaded.") {
		t.Error("upload success flash missing")
	}
}

func TestRenderDocumentDetailPage(t *testing.T) {
	doc := &Document{ID: "d1", Filename: "Spec.md", Chunks: 2, ExtractionStatus: "completed", ObjectsCreated: 15}
	chunks := []Chunk{
		{ID: "c1", Index: 0, Size: 5, HasEmbedding: true, Text: "hello"},
		{ID: "c2", Index: 1, Size: 5, HasEmbedding: false, Text: "world"},
	}
	html := renderHTML(t, DocumentDetailPage(doc, chunks, extractionResults{}, nil, "", nil))
	for _, want := range []string{
		"Spec.md", "2 chunks", "completed", "15 objects extracted",
		"hello", "world", "embedded", "chunk 0", "chunk 1",
		`action="/documents/d1/extract"`, `href="/documents"`,
		`action="/documents/d1/delete"`, "Delete this document? This cannot be undone.", "lucide--trash-2", ">Delete<",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("detail page missing %q", want)
		}
	}

	htmlNoChunks := renderHTML(t, DocumentDetailPage(doc, nil, extractionResults{}, nil, "", nil))
	if !strings.Contains(htmlNoChunks, "No chunks yet") {
		t.Error("no-chunks state missing")
	}

	htmlErr := renderHTML(t, DocumentDetailPage(nil, nil, extractionResults{}, errTest, "", nil))
	if !strings.Contains(htmlErr, "Document unavailable") {
		t.Error("load error state missing")
	}

	htmlFlash := renderHTML(t, DocumentDetailPage(doc, nil, extractionResults{}, nil, "Extraction triggered.", nil))
	if !strings.Contains(htmlFlash, "Extraction triggered.") {
		t.Error("extraction flash missing")
	}
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

// TestRenderExtractionResults covers the extraction results section: extracted
// objects (type + label) and relationships (type + resolved src/dst labels).
func TestRenderExtractionResults(t *testing.T) {
	results := extractionResults{
		Summary: &ExtractionSummary{JobID: "job1", ObjectsCreated: 2, RelationshipsCreated: 1},
		Objects: []GraphObject{
			{ID: "o1", CanonicalID: "c1", Type: "person", Key: "sam", Properties: map[string]any{"first_name": "Sam"}},
			{ID: "o2", CanonicalID: "c2", Type: "task", Key: "call dentist", Properties: map[string]any{"title": "call dentist"}},
		},
		Relationships: []GraphRelationship{
			{ID: "r1", Type: "assigned_to", SrcID: "c2", DstID: "c1"},
		},
	}
	doc := &Document{ID: "d1", Filename: "Spec.md"}
	html := renderHTML(t, DocumentDetailPage(doc, nil, results, nil, "", nil))
	for _, want := range []string{
		"Extraction results", "2 objects", "1 relationship",
		"person", "sam", "task", "call dentist", "assigned to",
		`href="/objects/o1"`, `href="/objects/o2"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("extraction results missing %q", want)
		}
	}
}

// TestLoadExtractionResults exercises the assembly: a document with no
// extraction yields empty results; with an extraction it gathers objects and
// the relationships touching them (deduped).
func TestLoadExtractionResults(t *testing.T) {
	t.Run("no summary", func(t *testing.T) {
		s := &Server{memory: &fakeMemory{}}
		res := s.loadExtractionResults(context.Background(), "d1")
		if res.Summary != nil || len(res.Objects) != 0 || len(res.Relationships) != 0 {
			t.Fatalf("want empty results, got %+v", res)
		}
	})

	t.Run("with extraction", func(t *testing.T) {
		f := &fakeMemory{
			summary: &ExtractionSummary{JobID: "job1", ObjectsCreated: 2, RelationshipsCreated: 1, ObjectIDs: []string{"c1", "c2"}},
			objects: []GraphObject{
				{ID: "o1", CanonicalID: "c1", Type: "person", Key: "sam"},
				{ID: "o2", CanonicalID: "c2", Type: "task", Key: "call dentist"},
			},
			relns: []GraphRelationship{
				{ID: "r1", Type: "assigned_to", SrcID: "c2", DstID: "c1"},
				{ID: "r2", Type: "unrelated", SrcID: "c9", DstID: "c8"},
			},
		}
		s := &Server{memory: f}
		res := s.loadExtractionResults(context.Background(), "d1")
		if res.Summary == nil || res.Summary.JobID != "job1" {
			t.Fatalf("summary = %+v", res.Summary)
		}
		if len(res.Objects) != 2 {
			t.Fatalf("objects = %d, want 2", len(res.Objects))
		}
		// only the relationship touching an extracted object is kept
		if len(res.Relationships) != 1 || res.Relationships[0].Type != "assigned_to" {
			t.Fatalf("relationships = %+v", res.Relationships)
		}
	})
}
