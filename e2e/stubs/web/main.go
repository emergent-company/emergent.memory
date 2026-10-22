// Command web-stub is a hermetic stand-in for a small website, used by the e2e
// harness. It lets the web data-source connector be exercised end to end — link
// following, robots.txt, content revision, tombstones, and the SSRF allow-list —
// without a real site or network egress.
//
// The server reaches the stub as http://web-stub:8080 (deployment policy:
// WEB_SOURCE_ALLOWED_HOSTS). Host-run API tests seed and mutate the site through
// the stub's /_control routes.
//
// Site routes:
//
//	GET /healthz            200 "ok"
//	GET /robots.txt         robots.txt (default: allow all; overridable)
//	GET /sitemap.xml        sitemap (404 unless seeded via /_control/page)
//	GET /sitemap-index.xml  sitemap index (404 unless seeded)
//	GET /sitemap.xml.gz     gzipped sitemap (404 unless seeded)
//	GET /docs               HTML index, links to /docs/a and /docs/b
//	GET /docs/a             HTML page, links to /docs/a/deep
//	GET /docs/a/deep        HTML page
//	GET /docs/b             HTML page
//	GET /docs/gone          404
//	GET /docs/broken        500
//	GET /docs/redirect      302 -> /docs/a
//	GET /files/manual.pdf   application/pdf, deterministic bytes
//	GET /slow               200 after a 3s sleep
//	GET /latin1             iso-8859-1 HTML
//
// Test-control routes (no auth; not part of a site contract):
//
//	POST   /_control/reset   seed/replace the default site
//	POST   /_control/page    upsert a page: {path, status?, contentType?, body?, headers?}
//	DELETE /_control/page    remove a page: {path}
//	GET    /_control/state   per-path hit counts + the page map
//	GET    /_control/sitemap the current page paths (the "site map")
//
// Configuration (env):
//
//	PORT  listen port (default 8080)
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultPort = "8080"

// page is one servable resource: a status, content type, body, and optional
// extra headers (e.g. Location). The zero status means 200.
type page struct {
	Status      int               `json:"status"`
	ContentType string            `json:"contentType"`
	Body        string            `json:"-"`
	Headers     map[string]string `json:"-"`
}

// stub is the in-memory website impersonation.
type stub struct {
	mu    sync.Mutex
	pages map[string]page
	hits  map[string]int
}

func newStub() *stub {
	return &stub{
		pages: map[string]page{},
		hits:  map[string]int{},
	}
}

func main() {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = defaultPort
	}

	s := newStub()
	s.resetLocked()

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("web-stub listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("web-stub: %v", err)
	}
}

func (s *stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := normalizePath(r.URL.Path)

	// Health and control are not part of the crawled site.
	if path == "/healthz" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
		return
	}
	if strings.HasPrefix(path, "/_control") {
		s.serveControl(w, r, strings.TrimPrefix(path, "/_control"))
		return
	}

	// Every non-control fetch is counted so tests can assert what the crawler
	// did and did not request (robots.txt exclusion, redirect following).
	s.mu.Lock()
	s.hits[path]++
	s.mu.Unlock()

	// Fixed routes take precedence over the page map.
	switch path {
	case "/docs/gone":
		w.WriteHeader(http.StatusNotFound)
		return
	case "/docs/broken":
		w.WriteHeader(http.StatusInternalServerError)
		return
	case "/docs/redirect":
		w.Header().Set("Location", "/docs/a")
		w.WriteHeader(http.StatusFound)
		return
	case "/files/manual.pdf":
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte(manualPDF))
		return
	case "/slow":
		time.Sleep(3 * time.Second)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body><main><h1>Slow</h1></main></body></html>"))
		return
	case "/latin1":
		w.Header().Set("Content-Type", "text/html; charset=iso-8859-1")
		_, _ = w.Write([]byte(latin1HTML))
		return
	}

	s.mu.Lock()
	pg, ok := s.pages[path]
	s.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	pg.write(w)
}

func (p page) write(w http.ResponseWriter) {
	for k, v := range p.Headers {
		w.Header().Set(k, v)
	}
	if p.ContentType != "" {
		w.Header().Set("Content-Type", p.ContentType)
	}
	status := p.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(p.Body))
}

// ─────────────────────────────────────────────────────────────────────────────
// Control routes
// ─────────────────────────────────────────────────────────────────────────────

func (s *stub) serveControl(w http.ResponseWriter, r *http.Request, rest string) {
	rest = strings.TrimPrefix(rest, "/")
	switch rest {
	case "reset":
		s.controlReset(w, r)
	case "page":
		s.controlPage(w, r)
	case "state":
		s.controlState(w, r)
	case "sitemap":
		s.controlSitemap(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "unknown control route"})
	}
}

func (s *stub) controlReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"message": "POST required"})
		return
	}
	s.mu.Lock()
	s.resetLocked()
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"reset": true})
}

// resetLocked seeds the default site, replacing every page and clearing hits.
// Callers hold s.mu.
func (s *stub) resetLocked() {
	s.pages = map[string]page{
		"/robots.txt":  {ContentType: "text/plain; charset=utf-8", Body: "User-agent: *\nAllow: /\n"},
		"/docs":        {ContentType: "text/html; charset=utf-8", Body: docsIndexHTML},
		"/docs/a":      {ContentType: "text/html; charset=utf-8", Body: docsAHTML},
		"/docs/a/deep": {ContentType: "text/html; charset=utf-8", Body: docsDeepHTML},
		"/docs/b":      {ContentType: "text/html; charset=utf-8", Body: docsBHTML},
	}
	s.hits = map[string]int{}
}

// pageUpsert is the request shape for POST /_control/page.
type pageUpsert struct {
	Path        string            `json:"path"`
	Status      int               `json:"status"`
	ContentType string            `json:"contentType"`
	Body        string            `json:"body"`
	Headers     map[string]string `json:"headers"`
}

func (s *stub) controlPage(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req pageUpsert
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "invalid body: " + err.Error()})
			return
		}
		path := normalizePath(strings.TrimSpace(req.Path))
		if path == "" || path == "/" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "path is required"})
			return
		}
		s.mu.Lock()
		s.pages[path] = page{
			Status:      req.Status,
			ContentType: req.ContentType,
			Body:        req.Body,
			Headers:     req.Headers,
		}
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"path": path, "status": statusOf(req.Status)})

	case http.MethodDelete:
		var req struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "invalid body: " + err.Error()})
			return
		}
		path := normalizePath(strings.TrimSpace(req.Path))
		s.mu.Lock()
		delete(s.pages, path)
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"path": path, "deleted": true})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"message": "POST or DELETE required"})
	}
}

func (s *stub) controlState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"message": "GET required"})
		return
	}
	s.mu.Lock()
	hits := make(map[string]int, len(s.hits))
	for k, v := range s.hits {
		hits[k] = v
	}
	pages := make(map[string]any, len(s.pages))
	for k, v := range s.pages {
		pages[k] = map[string]any{
			"status":      statusOf(v.Status),
			"contentType": v.ContentType,
		}
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits, "pages": pages})
}

func (s *stub) controlSitemap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"message": "GET required"})
		return
	}
	s.mu.Lock()
	urls := make([]string, 0, len(s.pages))
	for k := range s.pages {
		urls = append(urls, k)
	}
	s.mu.Unlock()
	sort.Strings(urls)
	writeJSON(w, http.StatusOK, map[string]any{"urls": urls})
}

func statusOf(status int) int {
	if status == 0 {
		return http.StatusOK
	}
	return status
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// normalizePath collapses a single trailing slash (except the root) so the
// crawler's canonical URL "http://web-stub:8080/docs" and the connection-test
// URL "http://web-stub:8080/docs/" both resolve to the same page.
func normalizePath(p string) string {
	if p == "" {
		return "/"
	}
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		return strings.TrimSuffix(p, "/")
	}
	return p
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// ─────────────────────────────────────────────────────────────────────────────
// Default site content
// ─────────────────────────────────────────────────────────────────────────────

// docsIndexHTML is the seed page. It links to /docs/a and /docs/b only, so the
// default crawl is a deterministic four-page site (docs, a, a/deep, b) with no
// sitemap discovery and no error pages.
const docsIndexHTML = `<!doctype html>
<html><head><title>Docs Index</title></head>
<body><header>site chrome</header>
<main>
<h1>Docs Index</h1>
<p>Entry point.</p>
<a href="/docs/a">A</a>
<a href="/docs/b">B</a>
</main>
<footer>footer</footer>
</body></html>`

const docsAHTML = `<!doctype html>
<html><head><title>Page A</title></head>
<body>
<main>
<h1>Page A</h1>
<p>Version one.</p>
<a href="/docs/a/deep">Deep</a>
</main>
</body></html>`

const docsDeepHTML = `<!doctype html>
<html><head><title>Deep</title></head>
<body>
<main>
<h1>Deep</h1>
<p>Deep content.</p>
</main>
</body></html>`

const docsBHTML = `<!doctype html>
<html><head><title>Page B</title></head>
<body>
<main>
<h1>Page B</h1>
<p>B content.</p>
</main>
</body></html>`

// manualPDF is a deterministic, minimal PDF body (content, not structure, is
// what the harness cares about: the connector routes application/pdf through
// the extract path).
const manualPDF = "%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF\n"

// latin1HTML carries a single non-ASCII byte (é, 0xE9) in Latin-1 so the
// charset-decoding path in the connector can be exercised.
const latin1HTML = "<!doctype html>\n<html><head><meta charset=\"iso-8859-1\"></head>\n<body><main><p>caf\xe9</p></main></body></html>\n"
