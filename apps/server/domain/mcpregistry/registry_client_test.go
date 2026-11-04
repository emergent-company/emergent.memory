package mcpregistry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewRegistryClientWithBaseURL_EmptyFallsBackToDefault(t *testing.T) {
	client := NewRegistryClientWithBaseURL("")
	if client.baseURL != DefaultRegistryBaseURL {
		t.Errorf("baseURL = %q, want %q", client.baseURL, DefaultRegistryBaseURL)
	}

	custom := NewRegistryClientWithBaseURL("http://example.test")
	if custom.baseURL != "http://example.test" {
		t.Errorf("baseURL = %q, want %q", custom.baseURL, "http://example.test")
	}
}

func TestRegistryClient_SearchServers(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/v0.1/servers" {
			t.Errorf("path = %q, want /v0.1/servers", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("search") != "github" {
			t.Errorf("search = %q, want github", q.Get("search"))
		}
		if q.Get("version") != "latest" {
			t.Errorf("version = %q, want latest", q.Get("version"))
		}
		if q.Get("limit") != "5" {
			t.Errorf("limit = %q, want 5", q.Get("limit"))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"servers":[{"server":{"name":"io.github.github/github-mcp-server","description":"x","version":"1.0.0"}}],"metadata":{"count":1}}`))
	}))
	defer ts.Close()

	client := NewRegistryClientWithBaseURL(ts.URL)
	resp, err := client.SearchServers(context.Background(), "github", 5, "")
	if err != nil {
		t.Fatalf("SearchServers: %v", err)
	}
	if len(resp.Servers) != 1 {
		t.Fatalf("len(Servers) = %d, want 1", len(resp.Servers))
	}
	if resp.Servers[0].Server.Name != "io.github.github/github-mcp-server" {
		t.Errorf("server name = %q", resp.Servers[0].Server.Name)
	}
	if resp.Metadata.Count != 1 {
		t.Errorf("metadata count = %d, want 1", resp.Metadata.Count)
	}
}

func TestRegistryClient_GetServerVersion_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()

	client := NewRegistryClientWithBaseURL(ts.URL)
	_, err := client.GetServer(context.Background(), "io.github.github/does-not-exist")
	if err == nil {
		t.Fatal("GetServer: expected error, got nil")
	}
}
