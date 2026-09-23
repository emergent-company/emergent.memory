package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func doRequest(h http.Handler, method, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSearchGitHub(t *testing.T) {
	h := newHandler(0)
	rec := doRequest(h, http.MethodGet, "/v0.1/servers?search=github&limit=5")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var resp RegistrySearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(resp.Servers) < 1 {
		t.Fatal("expected at least one server matching 'github'")
	}
	if resp.Servers[0].Server.Name == "" {
		t.Error("expected non-empty server name")
	}
	if resp.Metadata.Count != len(resp.Servers) {
		t.Errorf("metadata.count %d != list length %d", resp.Metadata.Count, len(resp.Servers))
	}
	// The github fixture itself must be present (its name matches the query).
	foundGitHub := false
	for _, e := range resp.Servers {
		if e.Server.Name == "io.github.github/github-mcp-server" {
			foundGitHub = true
		}
	}
	if !foundGitHub {
		t.Error("expected the github fixture in search=github results")
	}
}

func TestSearchLimit(t *testing.T) {
	h := newHandler(0)
	rec := doRequest(h, http.MethodGet, "/v0.1/servers?limit=3")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var resp RegistrySearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(resp.Servers) < 1 {
		t.Fatal("expected at least one server")
	}
	if len(resp.Servers) > 3 {
		t.Errorf("expected <= 3 servers with limit=3, got %d", len(resp.Servers))
	}
}

func TestDetailGitHub(t *testing.T) {
	h := newHandler(0)
	rec := doRequest(h, http.MethodGet, "/v0.1/servers/io.github.github%2Fgithub-mcp-server/versions/latest")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var resp RegistryServerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Server.Name != "io.github.github/github-mcp-server" {
		t.Errorf("expected name %q, got %q", "io.github.github/github-mcp-server", resp.Server.Name)
	}
	if len(resp.Server.Remotes) < 1 {
		t.Error("expected at least one remotes entry")
	}
}

func TestDetailUnknown(t *testing.T) {
	h := newHandler(0)
	rec := doRequest(h, http.MethodGet, "/v0.1/servers/io.nonexistent.server%2Fdoes-not-exist/versions/latest")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestDetailContext7StdioOnly(t *testing.T) {
	h := newHandler(0)
	rec := doRequest(h, http.MethodGet, "/v0.1/servers/io.github.upstash%2Fcontext7/versions/latest")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var resp RegistryServerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Server.Name != "io.github.upstash/context7" {
		t.Errorf("expected name %q, got %q", "io.github.upstash/context7", resp.Server.Name)
	}
	if len(resp.Server.Packages) < 1 {
		t.Error("expected non-empty packages")
	}
	if len(resp.Server.Remotes) != 0 {
		t.Errorf("expected empty remotes (stdio-only fixture), got %d", len(resp.Server.Remotes))
	}
}

func TestForceStatus(t *testing.T) {
	h := newHandler(500)
	rec := doRequest(h, http.MethodGet, "/v0.1/servers")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}
