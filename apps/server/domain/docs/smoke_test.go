package docs

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestParseFrontmatter(t *testing.T) {
	t.Run("no frontmatter", func(t *testing.T) {
		fm, content, err := parseFrontmatter([]byte("just a body"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fm != nil {
			t.Fatalf("expected nil frontmatter, got %+v", fm)
		}
		if content != "just a body" {
			t.Fatalf("content = %q, want original", content)
		}
	})

	t.Run("valid frontmatter", func(t *testing.T) {
		in := "---\nid: doc-1\ntitle: Hello\n---\n\nBody content\n"
		fm, content, err := parseFrontmatter([]byte(in))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fm == nil || fm.ID != "doc-1" || fm.Title != "Hello" {
			t.Fatalf("frontmatter = %+v, want id=doc-1 title=Hello", fm)
		}
		if !strings.Contains(content, "Body content") {
			t.Fatalf("content = %q, want body", content)
		}
	})

	t.Run("missing closing delimiter", func(t *testing.T) {
		if _, _, err := parseFrontmatter([]byte("---\nid: doc-1\n")); err == nil {
			t.Fatal("expected error for missing closing delimiter")
		}
	})
}

func TestSlugFromPath(t *testing.T) {
	cases := map[string]string{
		"docs/public/foo.md": "foo",
		"bar.txt":            "bar",
		"nofile":             "nofile",
	}
	for in, want := range cases {
		if got := slugFromPath(in); got != want {
			t.Fatalf("slugFromPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()

	md := "---\nid: doc-1\ntitle: Hello World\ncategory: guides\ntags:\n  - a\n---\n\nBody content here\n"
	if err := os.WriteFile(filepath.Join(dir, "hello-world.md"), []byte(md), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	index := `{"categories":[{"name":"guides","description":"How-tos","icon":"book"}]}`
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(index), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	return &Service{
		logger:  discardLogger(),
		baseDir: dir,
		cache:   make(map[string]*Document),
	}
}

func TestGetDocument(t *testing.T) {
	svc := newTestService(t)

	doc, err := svc.GetDocument("hello-world")
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	if doc.ID != "doc-1" || doc.Title != "Hello World" || doc.Slug != "hello-world" {
		t.Fatalf("document = %+v, want id=doc-1 title=Hello World slug=hello-world", doc)
	}
	if !strings.Contains(doc.Content, "Body content here") {
		t.Fatalf("content = %q, want body", doc.Content)
	}
}

func TestGetDocumentNotFound(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.GetDocument("does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown slug")
	}
	if !errors.Is(err, ErrDocumentNotFound) {
		t.Fatalf("error = %v, want it to wrap ErrDocumentNotFound", err)
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("error = %q, want it to name the missing slug", err)
	}
}

func TestGetDocumentHandlerNotFound(t *testing.T) {
	t.Run("maps missing document to not found", func(t *testing.T) {
		assertGetDocumentNotFound(t)
	})

	// The 404 mapping must key on the sentinel, not on the error text, so that
	// rewording the service message cannot silently turn every 404 into a 500.
	t.Run("does not depend on error message wording", func(t *testing.T) {
		original := ErrDocumentNotFound
		ErrDocumentNotFound = errors.New("no documentation matches this slug")
		t.Cleanup(func() { ErrDocumentNotFound = original })

		assertGetDocumentNotFound(t)
	})
}

func assertGetDocumentNotFound(t *testing.T) {
	t.Helper()

	e := echo.New()
	h := NewHandler(newTestService(t))

	req := httptest.NewRequest(http.MethodGet, "/api/docs/does-not-exist", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/docs/:slug")
	c.SetParamNames("slug")
	c.SetParamValues("does-not-exist")

	err := h.GetDocument(c)
	if err == nil {
		t.Fatal("expected handler error for unknown slug")
	}

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error = %T (%v), want *apperror.Error", err, err)
	}
	if appErr.HTTPStatus != http.StatusNotFound {
		t.Fatalf("HTTPStatus = %d (%s), want 404", appErr.HTTPStatus, appErr.Code)
	}
	if appErr.Code != apperror.ErrNotFound.Code {
		t.Fatalf("Code = %q, want %q", appErr.Code, apperror.ErrNotFound.Code)
	}
}

func TestListDocuments(t *testing.T) {
	svc := newTestService(t)
	docs, err := svc.ListDocuments()
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("ListDocuments returned %d docs, want 1", len(docs))
	}
	if docs[0].Slug != "hello-world" || docs[0].Title != "Hello World" {
		t.Fatalf("doc = %+v, want slug=hello-world title=Hello World", docs[0])
	}
}

func TestGetCategories(t *testing.T) {
	svc := newTestService(t)
	cats, err := svc.GetCategories()
	if err != nil {
		t.Fatalf("GetCategories: %v", err)
	}
	if len(cats) != 1 || cats[0].Name != "guides" {
		t.Fatalf("categories = %+v, want single 'guides'", cats)
	}
}

func TestClearCache(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.GetDocument("hello-world"); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	svc.ClearCache()
	if len(svc.cache) != 0 {
		t.Fatalf("cache not cleared: %d entries", len(svc.cache))
	}
}

func TestRegisterRoutes(t *testing.T) {
	e := echo.New()
	h := NewHandler(newTestService(t))
	RegisterRoutes(e, h, discardLogger())

	got := map[string]bool{}
	for _, r := range e.Routes() {
		got[r.Method+" "+r.Path] = true
	}

	for _, want := range []string{
		"GET /api/docs",
		"GET /api/docs/categories",
		"GET /api/docs/:slug",
	} {
		if !got[want] {
			t.Fatalf("route %q not registered; have %v", want, got)
		}
	}
}

func TestListDocumentsHandler(t *testing.T) {
	e := echo.New()
	h := NewHandler(newTestService(t))

	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.ListDocuments(c); err != nil {
		t.Fatalf("ListDocuments handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
