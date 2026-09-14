package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/memory.web-ui/connector/internal/account"
	"github.com/emergent-company/memory.web-ui/connector/internal/config"
	"github.com/emergent-company/memory.web-ui/connector/internal/memoryapi"
	"github.com/emergent-company/memory.web-ui/connector/internal/project"
)

type fakeProjectAPI struct {
	projects    []memoryapi.Project
	createCalls int
}

func (f *fakeProjectAPI) deps() project.Deps {
	return project.Deps{
		ListProjects: func(context.Context, string, string) ([]memoryapi.Project, error) {
			return f.projects, nil
		},
		CreateToken: func(_ context.Context, _, _, projectID, name string, scopes []string) (memoryapi.CreatedToken, error) {
			f.createCalls++
			return memoryapi.CreatedToken{ID: "tid-" + projectID, Name: name, Token: "emt_" + projectID, Scopes: scopes}, nil
		},
		RevokeToken: func(context.Context, string, string, string, string) error { return nil },
	}
}

func projectManagerFor(configPath string, api *fakeProjectAPI) *project.Manager {
	baseDir := account.BaseDirForConfig(configPath)
	return project.NewManagerWithDeps(baseDir, account.NewManager(baseDir), api.deps())
}

func withFakeProjectManager(t *testing.T, api *fakeProjectAPI) {
	t.Helper()
	old := newProjectManager
	newProjectManager = func(dir string) *project.Manager {
		return project.NewManagerWithDeps(dir, account.NewManager(dir), api.deps())
	}
	t.Cleanup(func() { newProjectManager = old })
}

func seedAccount(t *testing.T, configPath, serverURL string) {
	t.Helper()
	am := account.NewManager(account.BaseDirForConfig(configPath))
	if err := am.Save(serverURL, &account.Session{ServerURL: serverURL, AccessToken: "oauth-at", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := am.SetActive(serverURL); err != nil {
		t.Fatalf("seed active: %v", err)
	}
}

func runProjectsCapture(args ...string) (code int, stdout, stderr string) {
	var out, errBuf bytes.Buffer
	code = runProjects(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestProjectsListMarksActiveJSON(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	serverURL := "https://srv.test"
	seedAccount(t, configPath, serverURL)
	api := &fakeProjectAPI{projects: []memoryapi.Project{
		{ID: "p1", Name: "One", OrgID: "org-1"},
		{ID: "p2", Name: "Two"},
	}}
	withFakeProjectManager(t, api)
	if err := projectManagerFor(configPath, api).SetActive(serverURL, "p1", "One"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}

	code, stdout, stderr := runProjectsCapture("list", "--config", configPath, "--server", serverURL, "--json")
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var doc struct {
		SchemaVersion int  `json:"schema_version"`
		SignedIn      bool `json:"signed_in"`
		Projects      []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			OrgID  string `json:"org_id"`
			Active bool   `json:"active"`
		} `json:"projects"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout)
	}
	if doc.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", doc.SchemaVersion)
	}
	if len(doc.Projects) != 2 {
		t.Fatalf("projects = %+v, want 2", doc.Projects)
	}
	if doc.Projects[0].ID != "p1" || !doc.Projects[0].Active {
		t.Errorf("first project = %+v, want p1 active", doc.Projects[0])
	}
	if doc.Projects[0].OrgID != "org-1" {
		t.Errorf("first project org_id = %q, want org-1", doc.Projects[0].OrgID)
	}
	if doc.Projects[1].OrgID != "" {
		t.Errorf("second project org_id = %q, want empty", doc.Projects[1].OrgID)
	}
	if doc.Projects[1].Active {
		t.Errorf("second project should not be active: %+v", doc.Projects[1])
	}
	// The org-less project must omit the key entirely (omitempty), so older
	// consumers never see an empty string.
	if strings.Contains(stdout, `"org_id": ""`) {
		t.Errorf("stdout = %q, want org_id omitted when empty", stdout)
	}
}

func TestProjectsListTextMarksActive(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	serverURL := "https://srv.test"
	seedAccount(t, configPath, serverURL)
	api := &fakeProjectAPI{projects: []memoryapi.Project{{ID: "p1", Name: "One"}, {ID: "p2", Name: "Two"}}}
	withFakeProjectManager(t, api)
	if err := projectManagerFor(configPath, api).SetActive(serverURL, "p2", "Two"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}

	code, stdout, stderr := runProjectsCapture("list", "--config", configPath, "--server", serverURL)
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "* p2  Two") {
		t.Errorf("stdout = %q, want active marker on p2", stdout)
	}
	if !strings.Contains(stdout, "  p1  One") {
		t.Errorf("stdout = %q, want p1 unmarked", stdout)
	}
}

func TestProjectsListNotSignedIn(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	api := &fakeProjectAPI{}
	withFakeProjectManager(t, api)

	code, stdout, stderr := runProjectsCapture("list", "--config", configPath, "--server", "https://srv.test")
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "memory-connector auth login") {
		t.Errorf("stderr = %q, want a pointer to auth login", stderr)
	}
}

func TestProjectsUseSetsActiveWritesConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "memory-connector.yml")
	serverURL := "https://srv.test"
	seedAccount(t, configPath, serverURL)
	api := &fakeProjectAPI{projects: []memoryapi.Project{{ID: "p1", Name: "One"}, {ID: "p2", Name: "Two"}}}
	withFakeProjectManager(t, api)

	code, stdout, stderr := runProjectsCapture("use", "p2", "--config", configPath, "--server", serverURL)
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "active project Two (p2)") {
		t.Errorf("stdout = %q, want active project line", stdout)
	}
	if !strings.Contains(stdout, configPath) {
		t.Errorf("stdout = %q, want the written config path", stdout)
	}

	// Active selection persisted.
	active, ok := projectManagerFor(configPath, api).Active(serverURL)
	if !ok || active.ID != "p2" || active.Name != "Two" {
		t.Errorf("Active = (%+v, %v), want p2/Two", active, ok)
	}
	// Config materialized with the minted token.
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load materialized config: %v", err)
	}
	if cfg.ServerURL != serverURL || cfg.ProjectID != "p2" || cfg.Token != "emt_p2" {
		t.Errorf("materialized config = %+v", cfg)
	}
	if api.createCalls != 1 {
		t.Errorf("CreateToken calls = %d, want 1", api.createCalls)
	}
}

func TestProjectsUseByIDAndName(t *testing.T) {
	for _, target := range []string{"p2", "Two"} {
		t.Run(target, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
			serverURL := "https://srv.test"
			seedAccount(t, configPath, serverURL)
			api := &fakeProjectAPI{projects: []memoryapi.Project{{ID: "p1", Name: "One"}, {ID: "p2", Name: "Two"}}}
			withFakeProjectManager(t, api)

			if code, _, stderr := runProjectsCapture("use", target, "--config", configPath, "--server", serverURL); code != 0 {
				t.Fatalf("use %q code = %d (stderr: %s)", target, code, stderr)
			}
			if active, ok := projectManagerFor(configPath, api).Active(serverURL); !ok || active.ID != "p2" {
				t.Errorf("Active after use %q = (%+v, %v), want p2", target, active, ok)
			}
		})
	}
}

func TestProjectsUseUnknownProject(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	serverURL := "https://srv.test"
	seedAccount(t, configPath, serverURL)
	api := &fakeProjectAPI{projects: []memoryapi.Project{{ID: "p1", Name: "One"}}}
	withFakeProjectManager(t, api)

	code, _, stderr := runProjectsCapture("use", "nope", "--config", configPath, "--server", serverURL)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "no project matching") {
		t.Errorf("stderr = %q, want unknown-project message", stderr)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Errorf("config should not be written for an unknown project")
	}
}

func TestProjectsCurrent(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	serverURL := "https://srv.test"
	seedAccount(t, configPath, serverURL)
	api := &fakeProjectAPI{}
	withFakeProjectManager(t, api)

	if code, stdout, _ := runProjectsCapture("current", "--config", configPath, "--server", serverURL); code != 0 || !strings.Contains(stdout, "no active project") {
		t.Errorf("current signed-out-active = (%d, %q)", code, stdout)
	}
	if err := projectManagerFor(configPath, api).SetActive(serverURL, "p1", "One"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	code, stdout, stderr := runProjectsCapture("current", "--config", configPath, "--server", serverURL, "--json")
	if code != 0 {
		t.Fatalf("code = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, `"id": "p1"`) || !strings.Contains(stdout, `"name": "One"`) {
		t.Errorf("current --json = %q, want p1/One", stdout)
	}
}

func TestProjectsUsageErrors(t *testing.T) {
	cases := [][]string{
		{},
		{"bogus"},
		{"list", "extra"},
		{"use"},
		{"use", "a", "b"},
		{"list", "--bogus"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			if code, _, _ := runProjectsCapture(args...); code != 2 {
				t.Errorf("runProjects(%v) code = %d, want 2", args, code)
			}
		})
	}
}

func TestAuthLogoutClearsProjectTokens(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "memory-connector.yml")
	serverURL := "https://srv.test"
	seedAccount(t, configPath, serverURL)
	api := &fakeProjectAPI{}
	withFakeProjectManager(t, api)

	// Mint a project token so a token file exists.
	pm := projectManagerFor(configPath, api)
	if _, err := pm.EnsureToken(context.Background(), serverURL, "p1"); err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	tokenFiles, _ := filepath.Glob(filepath.Join(dir, "memory-connector", "tokens", "*.json"))
	if len(tokenFiles) != 1 {
		t.Fatalf("token files before logout = %v, want 1", tokenFiles)
	}

	if code, _, stderr := runAuthCapture("logout", "--config", configPath, "--server", serverURL); code != 0 {
		t.Fatalf("logout code = %d (stderr: %s)", code, stderr)
	}
	tokenFiles, _ = filepath.Glob(filepath.Join(dir, "memory-connector", "tokens", "*.json"))
	if len(tokenFiles) != 0 {
		t.Errorf("token files after logout = %v, want none", tokenFiles)
	}
}
