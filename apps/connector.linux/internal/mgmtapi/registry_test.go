package mgmtapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcphost"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcpregistry"
)

// stubRegistrySource is a configurable RegistrySource for handler tests.
type stubRegistrySource struct {
	searchFn func(ctx context.Context, query string, limit int, cursor string) (*mcpregistry.RegistrySearchResult, error)
	getFn    func(ctx context.Context, name string) (*mcpregistry.RegistryServer, error)
	gotName  string
}

func (s *stubRegistrySource) SearchRegistry(ctx context.Context, query string, limit int, cursor string) (*mcpregistry.RegistrySearchResult, error) {
	if s.searchFn == nil {
		return &mcpregistry.RegistrySearchResult{Servers: []mcpregistry.RegistryServer{}}, nil
	}
	return s.searchFn(ctx, query, limit, cursor)
}

func (s *stubRegistrySource) GetRegistryServer(ctx context.Context, name string) (*mcpregistry.RegistryServer, error) {
	s.gotName = name
	if s.getFn == nil {
		return nil, mcpregistry.ErrNotFound
	}
	return s.getFn(ctx, name)
}

func doGet(t *testing.T, h *harness, path string) (int, []byte) {
	t.Helper()
	resp, err := h.ts.Client().Get(h.ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, body
}

func TestRegistrySearchPassthrough(t *testing.T) {
	h := newHarness(t)
	h.server.SetRegistrySource(&stubRegistrySource{
		searchFn: func(_ context.Context, query string, limit int, cursor string) (*mcpregistry.RegistrySearchResult, error) {
			if query != "chrome" || limit != 10 || cursor != "abc" {
				t.Errorf("forwarded (%q, %d, %q)", query, limit, cursor)
			}
			return &mcpregistry.RegistrySearchResult{
				Servers: []mcpregistry.RegistryServer{
					{Name: "io.github.x/y", Title: "X/Y", Version: "1.0.0", HasPackages: true},
				},
				NextCursor: "abc123",
				Count:      1,
			}, nil
		},
	})

	status, body := doGet(t, h, "/api/mcp-registry/search?q=chrome&limit=10&cursor=abc")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body %s", status, body)
	}
	var out mcpregistry.RegistrySearchResult
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Count != 1 || out.NextCursor != "abc123" || len(out.Servers) != 1 {
		t.Errorf("result = %+v", out)
	}
	if out.Servers[0].Name != "io.github.x/y" {
		t.Errorf("servers[0].Name = %q", out.Servers[0].Name)
	}
}

func TestRegistrySearchUpstreamError(t *testing.T) {
	h := newHarness(t)
	h.server.SetRegistrySource(&stubRegistrySource{
		searchFn: func(context.Context, string, int, string) (*mcpregistry.RegistrySearchResult, error) {
			return nil, errors.New("boom")
		},
	})

	status, body := doGet(t, h, "/api/mcp-registry/search?q=x")
	if status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body %s", status, body)
	}
}

func TestRegistrySearchUnavailable(t *testing.T) {
	h := newHarness(t) // no source installed

	status, body := doGet(t, h, "/api/mcp-registry/search")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %s", status, body)
	}
}

func TestRegistryServerSuggestionShape(t *testing.T) {
	h := newHarness(t)
	h.server.SetRegistrySource(&stubRegistrySource{
		getFn: func(_ context.Context, name string) (*mcpregistry.RegistryServer, error) {
			return &mcpregistry.RegistryServer{
				Name:        "io.github.x/y",
				Title:       "X/Y",
				Description: "desc",
				Version:     "0.1.0",
				HasPackages: true,
				Packages: []mcpregistry.RegistryPackage{
					{RegistryType: "npm", Name: "y", Identifier: "y", Transport: "stdio"},
				},
				EnvVars: []mcpregistry.RegistryEnvVar{{Name: "API_KEY", IsRequired: true, IsSecret: true}},
			}, nil
		},
	})

	status, body := doGet(t, h, "/api/mcp-registry/servers/io.github.x%2Fy")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body %s", status, body)
	}

	var out struct {
		Server    mcpregistry.RegistryServer   `json:"server"`
		Suggested serverRequest                `json:"suggested"`
		EnvVars   []mcpregistry.RegistryEnvVar `json:"envVars"`
		Warnings  []string                     `json:"warnings"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := serverRequest{
		Name:      "y",
		Transport: mcphost.TransportStdio,
		Command:   "npx",
		Args:      []string{"-y", "y@0.1.0"},
		Env:       map[string]string{"API_KEY": ""},
		Headers:   map[string]string{},
	}
	if out.Suggested.Name != want.Name || out.Suggested.Transport != want.Transport ||
		out.Suggested.Command != want.Command || out.Suggested.URL != want.URL {
		t.Errorf("suggested = %+v, want %+v", out.Suggested, want)
	}
	if len(out.Suggested.Args) != len(want.Args) {
		t.Errorf("suggested.Args = %v, want %v", out.Suggested.Args, want.Args)
	} else {
		for i := range want.Args {
			if out.Suggested.Args[i] != want.Args[i] {
				t.Errorf("suggested.Args = %v, want %v", out.Suggested.Args, want.Args)
			}
		}
	}
	if out.Suggested.Env["API_KEY"] != "" {
		t.Errorf("suggested.Env = %v", out.Suggested.Env)
	}
	if len(out.Suggested.DisabledTools) != 0 {
		t.Errorf("suggested.DisabledTools = %v, want empty", out.Suggested.DisabledTools)
	}
	if out.Server.Name != "io.github.x/y" {
		t.Errorf("server.Name = %q", out.Server.Name)
	}
	if len(out.EnvVars) != 1 || !out.EnvVars[0].IsRequired || !out.EnvVars[0].IsSecret {
		t.Errorf("envVars = %+v", out.EnvVars)
	}

	// The suggested object must be JSON-round-trippable straight into
	// POST /api/mcp-servers (same field names as serverRequest).
	var req serverRequest
	raw, _ := json.Marshal(out.Suggested)
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("suggested not serverRequest-shaped: %v", err)
	}
	if req.Command != "npx" || req.Name != "y" {
		t.Errorf("round-tripped req = %+v", req)
	}
}

func TestRegistryServerRemoteSuggestion(t *testing.T) {
	h := newHarness(t)
	h.server.SetRegistrySource(&stubRegistrySource{
		getFn: func(_ context.Context, name string) (*mcpregistry.RegistryServer, error) {
			return &mcpregistry.RegistryServer{
				Name:       "io.github.x/remote",
				HasRemotes: true,
				Remotes: []mcpregistry.RegistryRemote{
					{Type: "streamable-http", URL: "https://mcp.example.com/mcp"},
				},
				Headers: []mcpregistry.RegistryHeader{{Name: "Authorization", IsRequired: true, IsSecret: true}},
			}, nil
		},
	})

	status, body := doGet(t, h, "/api/mcp-registry/servers/io.github.x%2Fremote")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body %s", status, body)
	}
	var out struct {
		Suggested serverRequest                `json:"suggested"`
		Headers   []mcpregistry.RegistryHeader `json:"headers"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Suggested.Transport != mcphost.TransportHTTP || out.Suggested.URL != "https://mcp.example.com/mcp" {
		t.Errorf("suggested = %+v", out.Suggested)
	}
	if out.Suggested.Command != "" || len(out.Suggested.Args) != 0 {
		t.Errorf("remote suggested must have no command/args: %+v", out.Suggested)
	}
	if out.Suggested.Headers["Authorization"] != "" {
		t.Errorf("suggested.Headers = %v", out.Suggested.Headers)
	}
	if len(out.Headers) != 1 || out.Headers[0].Name != "Authorization" || !out.Headers[0].IsSecret {
		t.Errorf("headers = %+v", out.Headers)
	}
}

func TestRegistryServerUnsupported(t *testing.T) {
	h := newHarness(t)
	h.server.SetRegistrySource(&stubRegistrySource{
		getFn: func(_ context.Context, name string) (*mcpregistry.RegistryServer, error) {
			return &mcpregistry.RegistryServer{
				Name:        "io.github.x/oci",
				HasPackages: true,
				Packages: []mcpregistry.RegistryPackage{
					{RegistryType: "oci", Name: "img", Identifier: "img", Transport: "stdio"},
				},
			}, nil
		},
	})

	status, body := doGet(t, h, "/api/mcp-registry/servers/io.github.x%2Foci")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body %s", status, body)
	}
	var out struct {
		Suggested json.RawMessage `json:"suggested"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(out.Suggested) != "null" {
		t.Errorf("suggested = %s, want null", out.Suggested)
	}
}

func TestRegistryServerNotFound(t *testing.T) {
	h := newHarness(t)
	h.server.SetRegistrySource(&stubRegistrySource{
		getFn: func(_ context.Context, name string) (*mcpregistry.RegistryServer, error) {
			return nil, mcpregistry.ErrNotFound
		},
	})

	status, body := doGet(t, h, "/api/mcp-registry/servers/io.github.x%2Fmissing")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %s", status, body)
	}
}

func TestRegistryServerUpstreamError(t *testing.T) {
	h := newHarness(t)
	h.server.SetRegistrySource(&stubRegistrySource{
		getFn: func(_ context.Context, name string) (*mcpregistry.RegistryServer, error) {
			return nil, errors.New("connection refused")
		},
	})

	status, body := doGet(t, h, "/api/mcp-registry/servers/io.github.x%2Fy")
	if status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body %s", status, body)
	}
}

func TestRegistryServerSlashNamePassthrough(t *testing.T) {
	h := newHarness(t)
	stub := &stubRegistrySource{
		getFn: func(_ context.Context, name string) (*mcpregistry.RegistryServer, error) {
			return &mcpregistry.RegistryServer{Name: name}, nil
		},
	}
	h.server.SetRegistrySource(stub)

	status, body := doGet(t, h, "/api/mcp-registry/servers/io.github.x%2Fslash%2Fname")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body %s", status, body)
	}
	// The ServeMux unescapes the captured segment, so the source sees the
	// decoded (slash-containing) name.
	if stub.gotName != "io.github.x/slash/name" {
		t.Errorf("source received name %q, want %q", stub.gotName, "io.github.x/slash/name")
	}
}

func TestRegistryServerUnavailable(t *testing.T) {
	h := newHarness(t) // no source

	status, body := doGet(t, h, "/api/mcp-registry/servers/x")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %s", status, body)
	}
}
