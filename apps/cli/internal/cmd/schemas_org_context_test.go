package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// The project IDs below are real UUIDs because commands validate UUID-shaped
// project arguments against the server.
const (
	orgCtxProjectA = "33333333-3333-3333-3333-333333333333" // org-a, the project token's project
	orgCtxProjectB = "22222222-2222-2222-2222-222222222222" // org-b, the --project override
	orgCtxProjectC = "11111111-1111-1111-1111-111111111111" // org-correct, no token involved
)

type orgCtxRequest struct {
	Path      string
	OrgID     string
	ProjectID string
	APIKey    string
	Auth      string
}

// orgCtxServer is a fake API that records the context headers of every request.
// It deliberately reports a different org first in /api/orgs: the pre-fix code
// guessed orgs[0] and sent it as X-Org-ID, which is exactly the 403 mismatch
// reported in issue #324.
type orgCtxServer struct {
	mu       sync.Mutex
	requests []orgCtxRequest
	orgs     map[string]string // project ID -> owning org ID
	names    map[string]string // project ID -> project name
}

func newOrgCtxServer(t *testing.T) (*httptest.Server, *orgCtxServer) {
	t.Helper()
	s := &orgCtxServer{
		orgs: map[string]string{
			orgCtxProjectA: "org-a",
			orgCtxProjectB: "org-b",
			orgCtxProjectC: "org-correct",
		},
		names: map[string]string{
			orgCtxProjectA: "alpha",
			orgCtxProjectB: "beta",
			orgCtxProjectC: "gamma",
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, orgCtxRequest{
			Path:      r.URL.Path,
			OrgID:     r.Header.Get("X-Org-ID"),
			ProjectID: r.Header.Get("X-Project-ID"),
			APIKey:    r.Header.Get("X-API-Key"),
			Auth:      r.Header.Get("Authorization"),
		})
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/auth/me":
			// The only token this fake knows is bound to project A.
			_, _ = w.Write([]byte(`{"type":"api_token","project_id":"` + orgCtxProjectA + `","project_name":"alpha","org_id":"org-a"}`))
		case r.URL.Path == "/api/orgs":
			_, _ = w.Write([]byte(`[{"id":"org-wrong","name":"wrong"},{"id":"org-correct","name":"correct"}]`))
		case strings.HasPrefix(r.URL.Path, "/api/projects/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/projects/")
			org, ok := s.orgs[id]
			if !ok {
				http.Error(w, `{"error":{"code":"not_found","message":"project not found"}}`, http.StatusNotFound)
				return
			}
			_, _ = fmt.Fprintf(w, `{"id":%q,"name":%q,"orgId":%q}`, id, s.names[id], org)
		case strings.HasSuffix(r.URL.Path, "/installed"):
			_, _ = w.Write([]byte(`[]`))
		default:
			http.Error(w, `{"error":{"code":"not_found","message":"not found"}}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, s
}

func (s *orgCtxServer) lastInstalled(t *testing.T) orgCtxRequest {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.requests) - 1; i >= 0; i-- {
		if strings.HasSuffix(s.requests[i].Path, "/installed") {
			return s.requests[i]
		}
	}
	t.Fatalf("no /installed request was made; requests: %+v", s.requests)
	return orgCtxRequest{}
}

// isolateOrgCtxState points the CLI at the fake server and restores the global
// flag/viper state that getClient and resolveProjectContext read.
func isolateOrgCtxState(t *testing.T, serverURL, projectFlag string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CI", "1")
	t.Setenv("MEMORY_SERVER_URL", serverURL)
	t.Setenv("MEMORY_PROJECT", "")
	t.Setenv("MEMORY_PROJECT_ID", "")
	t.Setenv("MEMORY_ORG_ID", "")
	t.Setenv("MEMORY_PROJECT_TOKEN", "")

	prevServer := viper.GetString("server")
	prevProjectToken := viper.GetString("project_token")
	viper.Set("server", serverURL)
	viper.Set("project_token", "")
	t.Cleanup(func() {
		viper.Set("server", prevServer)
		viper.Set("project_token", prevProjectToken)
	})

	prevProject := schemaProjectFlag
	schemaProjectFlag = projectFlag
	t.Cleanup(func() { schemaProjectFlag = prevProject })

	prevOutput := schemaOutputFlag
	schemaOutputFlag = "json"
	t.Cleanup(func() { schemaOutputFlag = prevOutput })
}

// orgCtxCommand builds the minimal command surface getClient expects, optionally
// carrying an explicit --project override.
func orgCtxCommand(configPath, project string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("config", configPath, "")
	cmd.Flags().String("project-token", "", "")
	cmd.Flags().String("project", "", "")
	if project != "" {
		_ = cmd.Flags().Set("project", project)
	}
	return cmd
}

// TestSchemasInstalled_ResolvesOrgFromProjectWhenUnset reproduces issue #324:
// with org_id unset, `schemas installed` must send the org of the target
// project, not an arbitrary org from the account's org list.
func TestSchemasInstalled_ResolvesOrgFromProjectWhenUnset(t *testing.T) {
	srv, fake := newOrgCtxServer(t)
	isolateOrgCtxState(t, srv.URL, orgCtxProjectC)
	t.Setenv("MEMORY_ACCOUNT_API_KEY", "acct-key")
	t.Setenv("MEMORY_PROJECT_API_KEY", "")

	tp, err := getSchemasClient(orgCtxCommand("", ""))
	if err != nil {
		t.Fatalf("getSchemasClient failed: %v", err)
	}
	if _, err := tp.GetInstalledPacks(context.Background()); err != nil {
		t.Fatalf("GetInstalledPacks failed: %v", err)
	}

	req := fake.lastInstalled(t)
	if req.OrgID != "org-correct" {
		t.Fatalf("X-Org-ID = %q, want the target project's org %q", req.OrgID, "org-correct")
	}
	if req.ProjectID != orgCtxProjectC {
		t.Fatalf("X-Project-ID = %q, want %q", req.ProjectID, orgCtxProjectC)
	}
	if req.APIKey != "acct-key" {
		t.Fatalf("X-API-Key = %q, want the account key", req.APIKey)
	}
}

// TestSchemasInstalled_OmitsOrgHeaderForProjectToken covers the project-token
// case from the issue: the token already pins the project and its org, so the
// CLI must omit X-Org-ID instead of guessing one.
func TestSchemasInstalled_OmitsOrgHeaderForProjectToken(t *testing.T) {
	srv, fake := newOrgCtxServer(t)
	isolateOrgCtxState(t, srv.URL, orgCtxProjectA)
	t.Setenv("MEMORY_ACCOUNT_API_KEY", "")
	t.Setenv("MEMORY_PROJECT_API_KEY", "emt_proj_a")

	tp, err := getSchemasClient(orgCtxCommand("", ""))
	if err != nil {
		t.Fatalf("getSchemasClient failed: %v", err)
	}
	if _, err := tp.GetInstalledPacks(context.Background()); err != nil {
		t.Fatalf("GetInstalledPacks failed: %v", err)
	}

	req := fake.lastInstalled(t)
	if req.OrgID != "" {
		t.Fatalf("X-Org-ID = %q, want it omitted for a project token", req.OrgID)
	}
	if req.ProjectID != orgCtxProjectA {
		t.Fatalf("X-Project-ID = %q, want %q", req.ProjectID, orgCtxProjectA)
	}
	if req.Auth != "Bearer emt_proj_a" {
		t.Fatalf("Authorization = %q, want the configured project token", req.Auth)
	}
}

// TestGetClient_ExplicitProjectOverridesOtherProjectToken covers the second
// half of issue #324: --project must win over a configured project token that
// belongs to a different project. The token must not leak into the request.
func TestGetClient_ExplicitProjectOverridesOtherProjectToken(t *testing.T) {
	srv, fake := newOrgCtxServer(t)
	isolateOrgCtxState(t, srv.URL, orgCtxProjectB)
	t.Setenv("MEMORY_ACCOUNT_API_KEY", "acct-key")
	t.Setenv("MEMORY_PROJECT_API_KEY", "")

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfgYAML := "server_url: " + srv.URL + "\n" +
		"project_id: " + orgCtxProjectA + "\n" +
		"project_token: emt_proj_a\n" +
		"org_id: \"\"\n"
	if err := os.WriteFile(configPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	tp, err := getSchemasClient(orgCtxCommand(configPath, orgCtxProjectB))
	if err != nil {
		t.Fatalf("getSchemasClient failed: %v", err)
	}
	if _, err := tp.GetInstalledPacks(context.Background()); err != nil {
		t.Fatalf("GetInstalledPacks failed: %v", err)
	}

	req := fake.lastInstalled(t)
	if req.Auth != "" {
		t.Fatalf("Authorization = %q, want the project token dropped for a different --project", req.Auth)
	}
	if req.APIKey != "acct-key" {
		t.Fatalf("X-API-Key = %q, want the account key", req.APIKey)
	}
	if req.ProjectID != orgCtxProjectB {
		t.Fatalf("X-Project-ID = %q, want the --project value %q", req.ProjectID, orgCtxProjectB)
	}
	if req.OrgID != "org-b" {
		t.Fatalf("X-Org-ID = %q, want the target project's org %q", req.OrgID, "org-b")
	}
}
