package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkskills "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/skills"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/client"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// buildSkillImportMetadata
// ---------------------------------------------------------------------------

func TestBuildSkillImportMetadata(t *testing.T) {
	// Always sets source=cli, even with no frontmatter metadata.
	meta := buildSkillImportMetadata("", "", nil)
	assert.Equal(t, map[string]any{"source": "cli"}, meta)

	// Version/license populate when present; frontmatter keys pass through.
	meta = buildSkillImportMetadata("1.0", "MIT", map[string]any{"author": "emergent"})
	assert.Equal(t, "cli", meta["source"])
	assert.Equal(t, "1.0", meta["version"])
	assert.Equal(t, "MIT", meta["license"])
	assert.Equal(t, "emergent", meta["author"])

	// Explicit top-level fields override nested metadata values.
	meta = buildSkillImportMetadata("2.0", "Apache-2.0", map[string]any{"version": "1.0", "license": "MIT"})
	assert.Equal(t, "2.0", meta["version"])
	assert.Equal(t, "Apache-2.0", meta["license"])
}

// ---------------------------------------------------------------------------
// skillFrontmatter.EffectiveVersion
// ---------------------------------------------------------------------------

func TestSkillFrontmatterEffectiveVersion(t *testing.T) {
	fm := &skillFrontmatter{Version: "1.2", Metadata: map[string]any{"version": "0.9"}}
	assert.Equal(t, "1.2", fm.EffectiveVersion(), "top-level version wins")

	fm = &skillFrontmatter{Metadata: map[string]any{"version": "1.2"}}
	assert.Equal(t, "1.2", fm.EffectiveVersion(), "nested metadata.version is the fallback")

	fm = &skillFrontmatter{}
	assert.Equal(t, "", fm.EffectiveVersion())
}

// ---------------------------------------------------------------------------
// parseSkillFile — top-level version/license + nested metadata
// ---------------------------------------------------------------------------

func TestParseSkillFile_CapturesVersionAndLicense(t *testing.T) {
	data := []byte("---\nname: my-skill\ndescription: d\nversion: \"1.0\"\nlicense: MIT\n---\n# body\n")
	fm, content, err := parseSkillFile(data)
	require.NoError(t, err)
	assert.Equal(t, "1.0", fm.Version)
	assert.Equal(t, "MIT", fm.License)
	assert.Contains(t, content, "# body")
}

func TestParseSkillFile_NestedMetadataVersion(t *testing.T) {
	data := []byte("---\nname: my-skill\ndescription: d\nmetadata:\n  version: \"1.1\"\n  author: emergent\n---\nbody\n")
	fm, content, err := parseSkillFile(data)
	require.NoError(t, err)
	assert.Equal(t, "1.1", fm.EffectiveVersion())
	assert.Equal(t, "emergent", fm.Metadata["author"])
	assert.Contains(t, content, "body")
}

// ---------------------------------------------------------------------------
// importFoundSkills — provenance + idempotency (httptest against the SDK)
// ---------------------------------------------------------------------------

// importTestServer mimics the skills REST API: GET /api/skills lists stored
// skills, POST /api/skills creates or returns 409 on duplicate names.
type importTestServer struct {
	mu      sync.Mutex
	skills  []*sdkskills.Skill
	creates []sdkskills.CreateSkillRequest
	// listErr makes GET return 500 — forces the 409-conflict skip path.
	listErr bool
}

func newImportTestServer(t *testing.T, listErr bool) (*httptest.Server, *importTestServer) {
	t.Helper()
	s := &importTestServer{listErr: listErr}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/skills", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if s.listErr {
				http.Error(w, "list unavailable", http.StatusInternalServerError)
				return
			}
			s.mu.Lock()
			skills := make([]*sdkskills.Skill, len(s.skills))
			copy(skills, s.skills)
			s.mu.Unlock()
			writeSkillsJSON(w, http.StatusOK, skills)
		case http.MethodPost:
			var req sdkskills.CreateSkillRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			s.mu.Lock()
			for _, existing := range s.skills {
				if existing.Name == req.Name {
					s.mu.Unlock()
					writeSkillsJSON(w, http.StatusConflict, nil)
					return
				}
			}
			skill := &sdkskills.Skill{
				ID:          "skill-" + strconv.Itoa(len(s.skills)+1),
				Name:        req.Name,
				Description: req.Description,
				Content:     req.Content,
				Metadata:    req.Metadata,
				CreatedAt:   time.Now(),
				UpdatedAt:   time.Now(),
			}
			s.skills = append(s.skills, skill)
			s.creates = append(s.creates, req)
			s.mu.Unlock()
			writeSkillsJSON(w, http.StatusCreated, []*sdkskills.Skill{skill})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, s
}

func writeSkillsJSON(w http.ResponseWriter, status int, skills []*sdkskills.Skill) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if skills == nil {
		_, _ = w.Write([]byte(`{"error":{"code":"duplicate_name","message":"skill name already exists"}}`))
		return
	}
	if len(skills) == 1 {
		_ = json.NewEncoder(w).Encode(skills[0])
		return
	}
	_ = json.NewEncoder(w).Encode(sdkskills.ListSkillsResponse{Skills: skills})
}

// newImportTestClient builds a CLI client whose SDK is wired to serverURL.
func newImportTestClient(t *testing.T, serverURL string) *client.Client {
	t.Helper()
	sdkClient, err := sdk.New(sdk.Config{
		ServerURL: serverURL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test-key"},
	})
	require.NoError(t, err)
	return &client.Client{SDK: sdkClient}
}

// runImportFoundSkills executes importFoundSkills in non-interactive mode
// (all=true) and returns the captured stdout/stderr.
func runImportFoundSkills(t *testing.T, c *client.Client, projectID, orgID string, skills []FoundSkill) (string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	err := importFoundSkills(cmd, c, projectID, orgID, skills, true)
	require.NoError(t, err)
	return out.String(), errOut.String()
}

func TestImportFoundSkills_SetsProvenanceMetadata(t *testing.T) {
	srv, s := newImportTestServer(t, false)
	c := newImportTestClient(t, srv.URL)

	skills := []FoundSkill{
		{
			Name:        "deploy",
			Description: "Deploy skill",
			Version:     "1.0",
			License:     "MIT",
			Content:     "# Deploy",
			Metadata:    map[string]any{"author": "emergent"},
		},
		{Name: "research", Description: "Research skill", Content: "# Research"},
	}

	out, _ := runImportFoundSkills(t, c, "", "", skills)
	assert.Contains(t, out, "Imported 'deploy'")
	assert.Contains(t, out, "Imported 'research'")
	assert.Contains(t, out, "Done: 2 imported, 0 skipped")

	require.Len(t, s.creates, 2)

	meta := s.creates[0].Metadata
	assert.Equal(t, "cli", meta["source"])
	assert.Equal(t, "1.0", meta["version"])
	assert.Equal(t, "MIT", meta["license"])
	assert.Equal(t, "emergent", meta["author"])

	// Skill without license/version still carries source=cli.
	assert.Equal(t, map[string]any{"source": "cli"}, s.creates[1].Metadata)
}

func TestImportFoundSkills_IdempotentReRun(t *testing.T) {
	srv, s := newImportTestServer(t, false)
	c := newImportTestClient(t, srv.URL)

	skills := []FoundSkill{
		{Name: "deploy", Description: "Deploy skill", Version: "1.0", License: "MIT", Content: "# Deploy"},
		{Name: "research", Description: "Research skill", Content: "# Research"},
	}

	out1, _ := runImportFoundSkills(t, c, "", "", skills)
	assert.Contains(t, out1, "Done: 2 imported, 0 skipped")

	// Second run must match by name and create no duplicates.
	out2, _ := runImportFoundSkills(t, c, "", "", skills)
	assert.Contains(t, out2, "Skipped 'deploy' (already exists)")
	assert.Contains(t, out2, "Skipped 'research' (already exists)")
	assert.Contains(t, out2, "Done: 0 imported, 2 skipped")

	// Only two skills ever created server-side.
	s.mu.Lock()
	defer s.mu.Unlock()
	require.Len(t, s.skills, 2)
	require.Len(t, s.creates, 2)
	names := map[string]bool{}
	for _, sk := range s.skills {
		names[sk.Name] = true
	}
	assert.True(t, names["deploy"])
	assert.True(t, names["research"])
}

// TestImportFoundSkills_ConflictTreatedAsSkip simulates a list failure (or a
// create/list race): the pre-check is unavailable, so idempotency relies on the
// server's 409 conflict response, which must be treated as a skip, not an error.
func TestImportFoundSkills_ConflictTreatedAsSkip(t *testing.T) {
	srv, s := newImportTestServer(t, true /* listErr */)
	c := newImportTestClient(t, srv.URL)

	skills := []FoundSkill{
		{Name: "deploy", Description: "Deploy skill", Content: "# Deploy"},
	}

	out1, errOut1 := runImportFoundSkills(t, c, "", "", skills)
	assert.Contains(t, errOut1, "Warning: could not list existing skills")
	assert.Contains(t, out1, "Done: 1 imported, 0 skipped")

	out2, errOut2 := runImportFoundSkills(t, c, "", "", skills)
	assert.Contains(t, errOut2, "Warning: could not list existing skills")
	assert.Contains(t, out2, "Skipped 'deploy' (already exists)")
	assert.Contains(t, out2, "Done: 0 imported, 1 skipped")

	s.mu.Lock()
	defer s.mu.Unlock()
	require.Len(t, s.skills, 1)
	require.Len(t, s.creates, 1)
}

// TestImportFoundSkills_OrgScope verifies org-scoped import routes through
// ListOrgSkills/CreateOrgSkill and stays idempotent.
func TestImportFoundSkills_OrgScope(t *testing.T) {
	// The org-scoped endpoints are not registered on the shared mux above, so
	// build a dedicated server that serves the org paths with real
	// duplicate detection (like the global-scope server).
	var mu sync.Mutex
	var stored []*sdkskills.Skill
	creates := 0
	listCalls := 0
	orgMux := http.NewServeMux()
	orgMux.HandleFunc("/api/orgs/org-1/skills", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			mu.Lock()
			listCalls++
			skills := make([]*sdkskills.Skill, len(stored))
			copy(skills, stored)
			mu.Unlock()
			writeSkillsJSON(w, http.StatusOK, skills)
		case http.MethodPost:
			var req sdkskills.CreateSkillRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			mu.Lock()
			for _, existing := range stored {
				if existing.Name == req.Name {
					mu.Unlock()
					writeSkillsJSON(w, http.StatusConflict, nil)
					return
				}
			}
			creates++
			stored = append(stored, &sdkskills.Skill{
				ID: "skill-1", Name: req.Name, Description: req.Description,
				Content: req.Content, Metadata: req.Metadata,
				CreatedAt: time.Now(), UpdatedAt: time.Now(),
			})
			mu.Unlock()
			writeSkillsJSON(w, http.StatusCreated, stored[len(stored)-1:])
		}
	})
	orgSrv := httptest.NewServer(orgMux)
	t.Cleanup(orgSrv.Close)

	c := newImportTestClient(t, orgSrv.URL)
	skills := []FoundSkill{{Name: "deploy", Description: "d", Content: "c", Version: "1.0", License: "MIT"}}

	out1, _ := runImportFoundSkills(t, c, "", "org-1", skills)
	assert.Contains(t, out1, "Done: 1 imported, 0 skipped")
	out2, _ := runImportFoundSkills(t, c, "", "org-1", skills)
	assert.Contains(t, out2, "Done: 0 imported, 1 skipped")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, creates, "org create should happen exactly once across two runs")
	assert.Equal(t, 2, listCalls, "org list should be called once per run")
}
