package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestStub(t *testing.T) (*httptest.Server, *stub) {
	t.Helper()
	s := newStub()
	s.resetLocked()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return srv, s
}

func do(t *testing.T, method, url string, payload any) *http.Response {
	t.Helper()
	var body *strings.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		body = strings.NewReader(string(encoded))
	} else {
		body = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// Do not follow redirects: tests observe the raw 302/Location the stub
	// emits (the crawler's own client is what follows redirects in production).
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func TestStub_HealthAndFixedRoutes(t *testing.T) {
	srv, _ := newTestStub(t)

	resp := do(t, "GET", srv.URL+"/healthz", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Canonical "/docs" and the connection-test "/docs/" must both serve HTML.
	for _, path := range []string{"/docs", "/docs/"} {
		resp = do(t, "GET", srv.URL+path, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s content-type = %q, want text/html", path, ct)
		}
		_ = resp.Body.Close()
	}

	if resp := do(t, "GET", srv.URL+"/docs/gone", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/docs/gone status = %d, want 404", resp.StatusCode)
	} else {
		_ = resp.Body.Close()
	}
	if resp := do(t, "GET", srv.URL+"/docs/broken", nil); resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("/docs/broken status = %d, want 500", resp.StatusCode)
	} else {
		_ = resp.Body.Close()
	}

	// The redirect advertises /docs/a with a 302.
	redirect := do(t, "GET", srv.URL+"/docs/redirect", nil)
	if redirect.StatusCode != http.StatusFound {
		t.Fatalf("/docs/redirect status = %d, want 302", redirect.StatusCode)
	}
	if loc := redirect.Header.Get("Location"); loc != "/docs/a" {
		t.Errorf("/docs/redirect Location = %q, want /docs/a", loc)
	}
	_ = redirect.Body.Close()

	// The PDF route serves application/pdf with the deterministic body.
	pdf := do(t, "GET", srv.URL+"/files/manual.pdf", nil)
	if ct := pdf.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("/files/manual.pdf content-type = %q, want application/pdf", ct)
	}
	_ = pdf.Body.Close()

	// latin1 serves iso-8859-1 with a raw 0xE9 byte.
	latin := do(t, "GET", srv.URL+"/latin1", nil)
	if ct := latin.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html; charset=iso-8859-1") {
		t.Errorf("/latin1 content-type = %q", ct)
	}
	_ = latin.Body.Close()
}

func TestStub_ControlUpsertAndDelete(t *testing.T) {
	srv, _ := newTestStub(t)

	// Upsert a page that 404s, then an arbitrary new page.
	up := do(t, "POST", srv.URL+"/_control/page", map[string]any{
		"path": "/docs/b", "status": 404,
	})
	if up.StatusCode != http.StatusOK {
		t.Fatalf("upsert status = %d", up.StatusCode)
	}
	_ = up.Body.Close()

	if resp := do(t, "GET", srv.URL+"/docs/b", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/docs/b status = %d, want 404", resp.StatusCode)
	} else {
		_ = resp.Body.Close()
	}

	do(t, "POST", srv.URL+"/_control/page", map[string]any{
		"path": "/docs/c", "contentType": "text/html; charset=utf-8",
		"body": "<main><p>hello</p></main>",
	})
	if resp := do(t, "GET", srv.URL+"/docs/c", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("/docs/c status = %d, want 200", resp.StatusCode)
	} else {
		_ = resp.Body.Close()
	}

	// Delete removes the route entirely.
	del := do(t, "DELETE", srv.URL+"/_control/page", map[string]any{"path": "/docs/c"})
	if del.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", del.StatusCode)
	}
	_ = del.Body.Close()
	if resp := do(t, "GET", srv.URL+"/docs/c", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/docs/c after delete status = %d, want 404", resp.StatusCode)
	} else {
		_ = resp.Body.Close()
	}
}

func TestStub_HitsTracking(t *testing.T) {
	srv, _ := newTestStub(t)

	do(t, "GET", srv.URL+"/docs/a", nil).Body.Close()
	do(t, "GET", srv.URL+"/docs/a", nil).Body.Close()

	state := decode[struct {
		Hits map[string]int `json:"hits"`
	}](t, do(t, "GET", srv.URL+"/_control/state", nil))
	if state.Hits["/docs/a"] != 2 {
		t.Fatalf("hits[/docs/a] = %d, want 2", state.Hits["/docs/a"])
	}
	if state.Hits["/docs/b"] != 0 {
		t.Fatalf("hits[/docs/b] = %d, want 0 (never requested)", state.Hits["/docs/b"])
	}
}

func TestStub_ResetRestoresDefault(t *testing.T) {
	srv, _ := newTestStub(t)

	// Mutate, then reset; the default page must be back and hits cleared.
	do(t, "POST", srv.URL+"/_control/page", map[string]any{"path": "/docs/b", "status": 500}).Body.Close()
	do(t, "POST", srv.URL+"/_control/reset", nil).Body.Close()

	// Immediately after reset, hits are cleared and the default page map is
	// back (checked before any site fetch re-records a hit).
	state := decode[struct {
		Hits  map[string]int            `json:"hits"`
		Pages map[string]map[string]any `json:"pages"`
	}](t, do(t, "GET", srv.URL+"/_control/state", nil))
	if len(state.Hits) != 0 {
		t.Fatalf("hits not cleared by reset: %v", state.Hits)
	}
	if _, ok := state.Pages["/docs"]; !ok {
		t.Fatal("/docs missing from default site after reset")
	}
	if _, ok := state.Pages["/robots.txt"]; !ok {
		t.Fatal("/robots.txt missing from default site after reset")
	}

	if resp := do(t, "GET", srv.URL+"/docs/b", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("/docs/b after reset status = %d, want 200", resp.StatusCode)
	} else {
		_ = resp.Body.Close()
	}
}
