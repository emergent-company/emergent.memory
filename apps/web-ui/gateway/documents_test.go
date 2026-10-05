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

	m := NewMemoryClient(srv.URL, "proj-1")
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
	m := NewMemoryClient(srv.URL, "proj")
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

// TestRenderDocumentsUploadPicker pins the upload-form contract: the form is
// tagged for the picker wiring, and the page ships the script that opens the
// file dialog when Upload is pressed with no file chosen. The listener must be
// registered in the capture phase (`, true`) and stop propagation — otherwise
// htmx's boosted-form submit handler, attached to the form, fires first and the
// empty multipart request still reaches the API.
func TestRenderDocumentsUploadPicker(t *testing.T) {
	html := renderHTML(t, DocumentsPage(nil, "", nil, "", nil))
	for _, want := range []string{
		`data-document-upload`, `enctype="multipart/form-data"`, `name="file"`,
		`__documentsUploadInit`, `form.matches('[data-document-upload]')`,
		`event.preventDefault()`, `event.stopPropagation()`, `input.click()`, `}, true);`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("upload picker wiring missing %q", want)
		}
	}
}

func TestRenderDocumentDetailPage(t *testing.T) {
	doc := &Document{ID: "d1", Filename: "Spec.md", MimeType: "text/markdown", Chunks: 2, ExtractionStatus: "completed", ObjectsCreated: 15}
	preview := documentPreview{Mode: previewModeText, Text: "hello preview"}
	html := renderHTML(t, DocumentDetailPage(doc, preview, nil, "", nil))
	for _, want := range []string{
		"Spec.md", "2 chunks", "completed", "15 objects extracted",
		`action="/documents/d1/extract"`, `href="/documents"`,
		`action="/documents/d1/delete"`, "Delete this document? This cannot be undone.", "lucide--trash-2", ">Delete<",
		// sub-navigation rail
		`aria-label="Document sections"`, "Overview", "Chunks", "Extraction history",
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

	htmlErr := renderHTML(t, DocumentDetailPage(nil, documentPreview{}, errTest, "", nil))
	if !strings.Contains(htmlErr, "Document unavailable") {
		t.Error("load error state missing")
	}

	htmlFlash := renderHTML(t, DocumentDetailPage(doc, preview, nil, "Extraction triggered.", nil))
	if !strings.Contains(htmlFlash, "Extraction triggered.") {
		t.Error("extraction flash missing")
	}
}

// TestDocumentPreviewEscapesHostileContent pins that stored document text is
// HTML-escaped in the preview: templ renders the string as text, so a document
// containing script/HTML tags cannot execute in the gateway origin.
func TestDocumentPreviewEscapesHostileContent(t *testing.T) {
	doc := &Document{ID: "d1", Filename: "evil.md", MimeType: "text/markdown"}
	hostile := `<script>alert(1)</script><img src=x onerror=alert(2)></pre><svg onload=alert(3)>`
	html := renderHTML(t, DocumentDetailPage(doc, documentPreview{Mode: previewModeText, Text: hostile}, nil, "", nil))
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
	html := renderHTML(t, documentSubNav("d1", "chunks"))
	if !strings.Contains(html, `aria-label="Document sections"`) {
		t.Error("sub-nav label missing")
	}
	if !linkMarkedCurrent(html, "/documents/d1/chunks") {
		t.Error("chunks link should be marked current")
	}
	if linkMarkedCurrent(html, "/documents/d1") || linkMarkedCurrent(html, "/documents/d1/extractions") {
		t.Error("only the active sub-nav link should be current")
	}

	overview := renderHTML(t, documentSubNav("d1", "overview"))
	if !linkMarkedCurrent(overview, "/documents/d1") {
		t.Error("overview link should be current on the overview view")
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
			html := renderHTML(t, DocumentDetailPage(tc.doc, preview, nil, "", nil))
			if !strings.Contains(html, tc.want) {
				t.Errorf("preview mode %s missing %q", tc.name, tc.want)
			}
			if !strings.Contains(html, `href="/documents/d1/file"`) {
				t.Errorf("preview mode %s missing download link", tc.name)
			}
		})
	}
}

// TestDocumentPreviewUnavailable asserts a failed server-side text fetch
// degrades to a note with a Chunks link rather than an error.
func TestDocumentPreviewUnavailable(t *testing.T) {
	doc := &Document{ID: "d1", Filename: "a.md", MimeType: "text/markdown"}
	html := renderHTML(t, DocumentDetailPage(doc, documentPreview{Mode: previewModeText, Err: errTest}, nil, "", nil))
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
		html := renderHTML(t, DocumentDetailPage(textDoc, p, nil, "", nil))
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
		html := renderHTML(t, DocumentDetailPage(textDoc, p, nil, "", nil))
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
	html := renderHTML(t, DocumentExtractionsPage(doc, nil, nil, "", nil))
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
	html := renderHTML(t, DocumentExtractionsPage(doc, jobs, nil, "", nil))
	for _, want := range []string{
		`data-testid="extraction-history"`,
		`id="extraction-detail-job1"`,
		`hx-get="/documents/d1/extractions/job1"`,
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
			`data-testid="extraction-merge-form"`,
			`action="/documents/d1/extractions/job1/merge"`,
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
		if strings.Contains(html, `data-testid="extraction-merge-form"`) {
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
		if strings.Contains(html, `data-testid="extraction-merge-form"`) {
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
		`href="/objects/o1"`, `href="/objects/o2"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("extraction job detail missing %q", want)
		}
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
	e.GET("/documents/:id/extractions/:jobId", s.uiDocumentExtractionDetail)
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
			extractionJobs: []ExtractionJob{{ID: "job1", Status: "completed", CreatedObjects: []string{"o1", "o2"}}},
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

	t.Run("unknown job", func(t *testing.T) {
		s := &Server{memory: &fakeMemory{}}
		d := s.loadExtractionJobDetail(context.Background(), "d1", "nope")
		if d.Err == nil {
			t.Fatal("expected an error for an unknown job")
		}
	})
}

// TestDocumentSubPageRoutes exercises the chunks/extractions sub-pages, the
// lazy per-job detail partial, the binary file proxy, and the merge PRG flow
// against the fake backend.
func TestDocumentSubPageRoutes(t *testing.T) {
	f := &fakeMemory{
		documents: []Document{{ID: "d1", Filename: "Spec.md", MimeType: "text/markdown", Chunks: 1}},
		chunks:    []Chunk{{ID: "c1", Index: 0, Size: 5, Text: "hello chunk"}},
		extractionJobs: []ExtractionJob{
			{ID: "job1", Status: "completed", CreatedAt: "2026-08-26T10:00:00Z", CreatedObjects: []string{"o1"}},
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
	e.GET("/documents/:id/extractions/:jobId", s.uiDocumentExtractionDetail)
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
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `hx-get="/documents/d1/extractions/job1"`) {
			t.Fatalf("extractions page: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("job detail partial", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/documents/d1/extractions/job1", nil)
		req.Header.Set("HX-Request-Type", "partial")
		e.ServeHTTP(rec, req)
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("job detail status = %d", rec.Code)
		}
		for _, want := range []string{
			`data-testid="extraction-job-detail"`,
			`data-testid="extraction-merge-form"`,
			`action="/documents/d1/extractions/job1/merge"`,
			"extract-1", "assigned to",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("job detail missing %q", want)
			}
		}
	})

	t.Run("missing job detail degrades", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents/d1/extractions/nope", nil))
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
