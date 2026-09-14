package memoryapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

const testToken = "emt_testtoken"

// newTestServer returns a server that records the request it received and
// replies with the given status/body. Checks (method, path, auth) are asserted
// by the caller through the returned request capture.
func newTestServer(t *testing.T, status int, body string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	cap := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.method = r.Method
		cap.path = r.URL.Path
		cap.auth = r.Header.Get("Authorization")
		cap.body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != "" {
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

type capturedRequest struct {
	method string
	path   string
	auth   string
	body   []byte
}

func newClientFor(t *testing.T, serverURL string) *Client {
	t.Helper()
	c, err := NewProjectClientWithHTTPClient(serverURL, testToken, nil)
	if err != nil {
		t.Fatalf("NewProjectClientWithHTTPClient: %v", err)
	}
	return c
}

func assertRequest(t *testing.T, got *capturedRequest, method, path string) {
	t.Helper()
	if got.method != method {
		t.Errorf("method = %s, want %s", got.method, method)
	}
	if got.path != path {
		t.Errorf("path = %s, want %s", got.path, path)
	}
	if got.auth != "Bearer "+testToken {
		t.Errorf("Authorization = %q, want %q", got.auth, "Bearer "+testToken)
	}
}

func TestListProjects(t *testing.T) {
	srv, cap := newTestServer(t, http.StatusOK, `[{"id":"p1","name":"Project One","orgId":"org-1"},{"id":"p2","name":"Project Two","orgId":"org-1"}]`)
	client := newClientFor(t, srv.URL)

	projects, err := client.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	assertRequest(t, cap, http.MethodGet, "/api/projects")
	want := []Project{{ID: "p1", Name: "Project One", OrgID: "org-1"}, {ID: "p2", Name: "Project Two", OrgID: "org-1"}}
	if !reflect.DeepEqual(projects, want) {
		t.Errorf("projects = %+v, want %+v", projects, want)
	}
}

func TestCreateToken(t *testing.T) {
	srv, cap := newTestServer(t, http.StatusOK, `{"id":"t1","name":"memory-connector-cli","token":"emt_created","tokenPrefix":"emt_cre","scopes":["data:read"],"createdAt":"2026-01-02T03:04:05Z"}`)
	client := newClientFor(t, srv.URL)

	created, err := client.CreateToken(context.Background(), "p1", "memory-connector-cli", []string{"data:read"})
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	assertRequest(t, cap, http.MethodPost, "/api/projects/p1/tokens")

	var req struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := json.Unmarshal(cap.body, &req); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if req.Name != "memory-connector-cli" || !reflect.DeepEqual(req.Scopes, []string{"data:read"}) {
		t.Errorf("request body = %s, want name/scopes for the created token", cap.body)
	}
	want := CreatedToken{ID: "t1", Name: "memory-connector-cli", Token: "emt_created", Prefix: "emt_cre", Scopes: []string{"data:read"}, CreatedAt: "2026-01-02T03:04:05Z"}
	if !reflect.DeepEqual(created, want) {
		t.Errorf("created = %+v, want %+v", created, want)
	}
}

func TestListTokens(t *testing.T) {
	srv, cap := newTestServer(t, http.StatusOK, `{"tokens":[{"id":"t1","name":"one","tokenPrefix":"emt_one","scopes":["data:read"],"createdAt":"2026-01-02T03:04:05Z"},{"id":"t2","name":"two","tokenPrefix":"emt_two","scopes":[],"createdAt":"2026-02-03T04:05:06Z","revokedAt":"2026-03-04T05:06:07Z"}]}`)
	client := newClientFor(t, srv.URL)

	tokens, err := client.ListTokens(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	assertRequest(t, cap, http.MethodGet, "/api/projects/p1/tokens")
	if len(tokens) != 2 {
		t.Fatalf("tokens = %+v, want 2", tokens)
	}
	if tokens[0].ID != "t1" || tokens[0].Name != "one" || tokens[0].Prefix != "emt_one" {
		t.Errorf("tokens[0] = %+v", tokens[0])
	}
	if tokens[1].RevokedAt == nil || *tokens[1].RevokedAt != "2026-03-04T05:06:07Z" {
		t.Errorf("tokens[1].RevokedAt = %v, want the revoked timestamp", tokens[1].RevokedAt)
	}
}

func TestRevokeToken(t *testing.T) {
	srv, cap := newTestServer(t, http.StatusNoContent, "")
	client := newClientFor(t, srv.URL)

	if err := client.RevokeToken(context.Background(), "p1", "t1"); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	assertRequest(t, cap, http.MethodDelete, "/api/projects/p1/tokens/t1")
}

func TestNewProjectClientRequiresToken(t *testing.T) {
	if _, err := NewProjectClient("https://example.test", ""); err == nil {
		t.Fatal("NewProjectClient with empty token: expected error, got nil")
	}
}

func TestNewBearerClientSendsBearerToken(t *testing.T) {
	srv, cap := newTestServer(t, http.StatusOK, `[{"id":"p1","name":"One"}]`)
	client, err := NewBearerClient(srv.URL, "oauth-access-token")
	if err != nil {
		t.Fatalf("NewBearerClient: %v", err)
	}
	if _, err := client.ListProjects(context.Background()); err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if cap.auth != "Bearer oauth-access-token" {
		t.Errorf("Authorization = %q, want Bearer oauth-access-token", cap.auth)
	}
}

type countingTransport struct {
	base  http.RoundTripper
	calls int
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.calls++
	return c.base.RoundTrip(r)
}

func TestNewBearerClientWithHTTPClientOption(t *testing.T) {
	srv, cap := newTestServer(t, http.StatusOK, `[]`)
	transport := &countingTransport{base: http.DefaultTransport}
	client, err := NewBearerClient(srv.URL, "tok", WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatalf("NewBearerClient: %v", err)
	}
	if _, err := client.ListProjects(context.Background()); err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if transport.calls == 0 {
		t.Error("injected HTTP client was not used")
	}
	if cap.auth != "Bearer tok" {
		t.Errorf("Authorization = %q, want Bearer tok", cap.auth)
	}
}
