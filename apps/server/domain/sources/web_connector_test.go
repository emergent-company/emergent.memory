package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/outbound"
	"github.com/emergent-company/emergent.memory/internal/webcrawl"
)

func newTestWebConnector(deps ConnectorDeps) *webConnector {
	return newWebConnector(outbound.Policy{AllowPrivate: true}, webUserAgent, deps)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestWebConnector_EnumerateTwoPages(t *testing.T) {
	const pageTwo = `<html><body><main><h1>Two</h1><p>second page</p></main></body></html>`

	var mu sync.Mutex
	etags := map[string]string{"/": `"etag-root"`}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		mu.Lock()
		etag := etags[r.URL.Path]
		mu.Unlock()
		if etag != "" {
			w.Header().Set("ETag", etag)
		}
		switch r.URL.Path {
		case "/":
			_, _ = fmt.Fprint(w, `<html><body><a href="/page2">p2</a>root content</body></html>`)
		case "/page2":
			_, _ = fmt.Fprint(w, pageTwo)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	conn := newTestWebConnector(ConnectorDeps{})
	items, err := conn.Enumerate(context.Background(), map[string]any{
		"start_urls": []any{srv.URL},
		"max_depth":  1,
	})
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("Enumerate() returned %d items, want 2", len(items))
	}

	byID := map[string]SourceItem{}
	for _, it := range items {
		byID[it.ID] = it
	}

	rootCanonical := srv.URL + "/" // Canonical keeps the root slash (only non-root trailing slashes are dropped)
	root, ok := byID[rootCanonical]
	if !ok {
		t.Fatalf("no item for root canonical URL %q; have %v", rootCanonical, itemIDs(items))
	}
	if !strings.HasSuffix(root.Name, ".md") {
		t.Errorf("root Name = %q, want .md suffix", root.Name)
	}
	if root.Name != "127.0.0.1_index.md" {
		t.Errorf("root Name = %q, want %q", root.Name, "127.0.0.1_index.md")
	}
	if root.MimeType != webMarkdownMimeType {
		t.Errorf("root MimeType = %q, want %q", root.MimeType, webMarkdownMimeType)
	}
	if root.RevisionID != `"etag-root"` {
		t.Errorf("root RevisionID = %q, want the ETag value", root.RevisionID)
	}

	page2, ok := byID[srv.URL+"/page2"]
	if !ok {
		t.Fatalf("no item for /page2 canonical URL %q; have %v", srv.URL+"/page2", itemIDs(items))
	}
	if want := sha256Hex([]byte(pageTwo)); page2.RevisionID != want {
		t.Errorf("page2 RevisionID = %q, want sha256 of body %q", page2.RevisionID, want)
	}
	if page2.Name != "127.0.0.1_page2.md" {
		t.Errorf("page2 Name = %q, want %q", page2.Name, "127.0.0.1_page2.md")
	}
}

func itemIDs(items []SourceItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func TestWebConnector_FetchServesFromCache(t *testing.T) {
	var pageRequests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			atomic.AddInt32(&pageRequests, 1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, `<html><body><main><h1>Hello</h1><p>world</p></main></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	conn := newTestWebConnector(ConnectorDeps{})
	items, err := conn.Enumerate(context.Background(), map[string]any{
		"start_urls": []any{srv.URL},
		"max_depth":  0,
	})
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Enumerate() returned %d items, want 1", len(items))
	}

	content, mime, err := conn.Fetch(context.Background(), items[0])
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if mime != "text/markdown" {
		t.Errorf("Fetch() mime = %q, want %q", mime, "text/markdown")
	}
	if !strings.Contains(content, "Hello") {
		t.Errorf("Fetch() content = %q, want converted markdown containing Hello", content)
	}
	if got := atomic.LoadInt32(&pageRequests); got != 1 {
		t.Errorf("page fetched %d times, want 1 (Fetch must serve from the crawl cache)", got)
	}
}

func TestWebConnector_TombstonesAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/":
			_, _ = fmt.Fprint(w, `<html><body><a href="/gone">gone</a><a href="/broken">broken</a>root</body></html>`)
		case "/gone":
			http.NotFound(w, r)
		case "/broken":
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	conn := newTestWebConnector(ConnectorDeps{})
	items, err := conn.Enumerate(context.Background(), map[string]any{
		"start_urls": []any{srv.URL},
		"max_depth":  1,
	})
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Enumerate() returned %d items, want 1 (only the root); have %v", len(items), itemIDs(items))
	}
	if items[0].ID != srv.URL+"/" {
		t.Errorf("item ID = %q, want %q", items[0].ID, srv.URL+"/")
	}

	ts := conn.Tombstones()
	if len(ts) != 1 || ts[0] != srv.URL+"/gone" {
		t.Errorf("Tombstones() = %v, want [%s]", ts, srv.URL+"/gone")
	}
	if conn.EnumerationComplete() {
		t.Error("EnumerationComplete() = true, want false (a 500 page error occurred)")
	}
}

// cancelAfterFirstFetcher serves a 404 for its first URL and cancels the parent
// context, so a second candidate is never re-checked. It fails the test if it is
// called again: an unverified candidate must not be re-requested.
type cancelAfterFirstFetcher struct {
	cancel context.CancelFunc
	calls  atomic.Int32
}

func (f *cancelAfterFirstFetcher) Get(_ context.Context, rawURL string) (*webcrawl.Response, error) {
	if f.calls.Add(1) == 1 {
		f.cancel()
		return &webcrawl.Response{URL: rawURL, Status: http.StatusNotFound}, nil
	}
	return nil, fmt.Errorf("fetcher called for unverified candidate %q", rawURL)
}

func TestWebConnector_TombstoneRecheckUnverifiedNotDeleted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &cancelAfterFirstFetcher{cancel: cancel}
	conn := &webConnector{fetcher: f}

	confirmed := conn.confirmTombstones(ctx, []string{"https://x/a", "https://x/b"})

	if len(confirmed) != 1 || confirmed[0] != "https://x/a" {
		t.Fatalf("confirmed = %v, want only [https://x/a] (the verified 404); /b was never re-checked and must not be treated as a removal", confirmed)
	}
	if got := f.calls.Load(); got != 1 {
		t.Errorf("fetcher calls = %d, want 1 (unverified candidate must not be re-checked after ctx cancellation)", got)
	}
}

func TestWebConnector_TombstoneRecheckSparesTransientMiss(t *testing.T) {
	// /flaky 404s on the crawl request but is healthy on the confirmation
	// re-check: a transient miss must not become deletion evidence.
	var flakyHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/":
			_, _ = fmt.Fprint(w, `<html><body><a href="/flaky">flaky</a>root</body></html>`)
		case "/flaky":
			if flakyHits.Add(1) == 1 {
				http.NotFound(w, r)
				return
			}
			_, _ = fmt.Fprint(w, `<html><body><main><h1>Back</h1></main></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	conn := newTestWebConnector(ConnectorDeps{})
	if _, err := conn.Enumerate(context.Background(), map[string]any{
		"start_urls": []any{srv.URL},
		"max_depth":  1,
	}); err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	if ts := conn.Tombstones(); len(ts) != 0 {
		t.Errorf("Tombstones() = %v, want none (the page recovered on the re-check)", ts)
	}
	if got := flakyHits.Load(); got != 2 {
		t.Errorf("/flaky hits = %d, want 2 (crawl + one re-check)", got)
	}
}

func TestWebConnector_PageCapTruncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/":
			_, _ = fmt.Fprint(w, `<html><body><a href="/a">a</a><a href="/b">b</a>root</body></html>`)
		case "/a", "/b":
			_, _ = fmt.Fprint(w, `<html><body>leaf</body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	conn := newTestWebConnector(ConnectorDeps{})
	items, err := conn.Enumerate(context.Background(), map[string]any{
		"start_urls": []any{srv.URL},
		"max_depth":  2,
		"max_pages":  1,
	})
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Enumerate() returned %d items, want 1 (page cap)", len(items))
	}
	if conn.EnumerationComplete() {
		t.Error("EnumerationComplete() = true, want false (page cap truncated the crawl)")
	}
}

func TestWebConnector_DepthBounds(t *testing.T) {
	var mu sync.Mutex
	requests := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/":
			_, _ = fmt.Fprint(w, `<html><body><a href="/child">child</a>root</body></html>`)
		case "/child":
			_, _ = fmt.Fprint(w, `<html><body><a href="/grandchild">g</a>child</body></html>`)
		case "/grandchild":
			_, _ = fmt.Fprint(w, `<html><body>grandchild</body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	count := func(path string) int {
		mu.Lock()
		defer mu.Unlock()
		return requests[path]
	}

	// max_depth 0 → pages-only: only the seed is fetched.
	conn := newTestWebConnector(ConnectorDeps{})
	if _, err := conn.Enumerate(context.Background(), map[string]any{
		"start_urls": []any{srv.URL},
		"max_depth":  0,
	}); err != nil {
		t.Fatalf("Enumerate(max_depth=0) error = %v", err)
	}
	if got := count("/child"); got != 0 {
		t.Errorf("max_depth=0 fetched /child %d times, want 0", got)
	}

	// max_depth 1 → one hop: / and /child fetched, /grandchild not.
	conn2 := newTestWebConnector(ConnectorDeps{})
	if _, err := conn2.Enumerate(context.Background(), map[string]any{
		"start_urls": []any{srv.URL},
		"max_depth":  1,
	}); err != nil {
		t.Fatalf("Enumerate(max_depth=1) error = %v", err)
	}
	if got := count("/child"); got < 1 {
		t.Errorf("max_depth=1 fetched /child %d times, want >= 1", got)
	}
	if got := count("/grandchild"); got != 0 {
		t.Errorf("max_depth=1 fetched /grandchild %d times, want 0", got)
	}
}

func TestWebConnector_ExtractRoutesPDF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/doc":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.4 fake"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	extracted := ""
	deps := ConnectorDeps{Extract: func(_ context.Context, content []byte, filename, mimeType string) (string, error) {
		if mimeType != "application/pdf" {
			t.Errorf("Extract mimeType = %q, want application/pdf", mimeType)
		}
		extracted = "extracted text"
		return extracted, nil
	}}

	conn := newTestWebConnector(deps)
	items, err := conn.Enumerate(context.Background(), map[string]any{
		"start_urls": []any{srv.URL + "/doc"},
		"max_depth":  0,
	})
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Enumerate() returned %d items, want 1", len(items))
	}

	content, mime, err := conn.Fetch(context.Background(), items[0])
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if content != "extracted text" {
		t.Errorf("Fetch() content = %q, want %q", content, "extracted text")
	}
	if mime != "text/plain" {
		t.Errorf("Fetch() mime = %q, want %q", mime, "text/plain")
	}
}

func TestWebConnector_ExtractNilReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/doc":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.4 fake"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	conn := newTestWebConnector(ConnectorDeps{}) // no Extract
	items, err := conn.Enumerate(context.Background(), map[string]any{
		"start_urls": []any{srv.URL + "/doc"},
		"max_depth":  0,
	})
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	if _, _, err := conn.Fetch(context.Background(), items[0]); err == nil {
		t.Fatal("Fetch() with nil Extract = nil, want error")
	}
}

func TestWebConnector_BinaryIsPerItemError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/blob":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte{0x00, 0x01, 0x02, 0x03})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	conn := newTestWebConnector(ConnectorDeps{})
	items, err := conn.Enumerate(context.Background(), map[string]any{
		"start_urls": []any{srv.URL + "/blob"},
		"max_depth":  0,
	})
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Enumerate() returned %d items, want 1", len(items))
	}
	_, _, err = conn.Fetch(context.Background(), items[0])
	if err == nil {
		t.Fatal("Fetch() of a binary = nil, want error")
	}
	if !strings.Contains(err.Error(), "unsupported content type") {
		t.Errorf("Fetch() error = %q, want an unsupported-content-type message", err)
	}
}

func TestWebConnector_TestConnection(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><body>ok</body></html>"))
		}))
		defer srv.Close()
		conn := newTestWebConnector(ConnectorDeps{})
		if err := conn.TestConnection(context.Background(), map[string]any{"start_urls": []any{srv.URL}}); err != nil {
			t.Errorf("TestConnection() error = %v, want nil", err)
		}
	})

	t.Run("http 500", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()
		conn := newTestWebConnector(ConnectorDeps{})
		err := conn.TestConnection(context.Background(), map[string]any{"start_urls": []any{srv.URL}})
		if err == nil {
			t.Fatal("TestConnection() = nil, want HTTP status error")
		}
		if !strings.Contains(err.Error(), "500") {
			t.Errorf("TestConnection() error = %q, want it to name the HTTP status", err)
		}
	})

	t.Run("blocked host", func(t *testing.T) {
		// A policy without AllowPrivate blocks loopback at validation time.
		conn := newWebConnector(outbound.DefaultPolicy(), webUserAgent, ConnectorDeps{})
		err := conn.TestConnection(context.Background(), map[string]any{"start_urls": []any{"http://127.0.0.1:8080/"}})
		if err == nil {
			t.Fatal("TestConnection() = nil, want a blocked-host error")
		}
		if !strings.Contains(err.Error(), "blocked") {
			t.Errorf("TestConnection() error = %q, want a blocked-host message", err)
		}
	})

	t.Run("empty start_urls", func(t *testing.T) {
		conn := newTestWebConnector(ConnectorDeps{})
		err := conn.TestConnection(context.Background(), map[string]any{"start_urls": []any{}})
		if err == nil {
			t.Fatal("TestConnection() = nil, want validation error")
		}
	})
}
