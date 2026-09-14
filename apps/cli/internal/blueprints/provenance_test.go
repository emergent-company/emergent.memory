package blueprints

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdkskills "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noopAuth satisfies the SDK auth.Provider interface without credentials.
type noopAuth struct{}

func (noopAuth) Authenticate(r *http.Request) error { return nil }
func (noopAuth) Refresh(ctx context.Context) error  { return nil }

// blueprintSkillTestServer records skill create (POST /api/skills) and update
// (PATCH /api/skills/:id) requests.
type blueprintSkillTestServer struct {
	creates []sdkskills.CreateSkillRequest
	updates []sdkskills.UpdateSkillRequest
	posts   int
	patches int
}

func newBlueprintSkillTestServer(t *testing.T) (*httptest.Server, *blueprintSkillTestServer) {
	t.Helper()
	s := &blueprintSkillTestServer{}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/skills", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var req sdkskills.CreateSkillRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			s.creates = append(s.creates, req)
			s.posts++
			writeBlueprintSkillJSON(w, http.StatusCreated, req.Name, req.Description, req.Content, req.Metadata)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/skills/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPatch:
			var req sdkskills.UpdateSkillRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			s.updates = append(s.updates, req)
			s.patches++
			desc, content := "", ""
			if req.Description != nil {
				desc = *req.Description
			}
			if req.Content != nil {
				content = *req.Content
			}
			writeBlueprintSkillJSON(w, http.StatusOK, "deploy", desc, content, req.Metadata)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, s
}

func writeBlueprintSkillJSON(w http.ResponseWriter, status int, name, desc, content string, meta map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(sdkskills.Skill{
		ID: "skill-1", Name: name, Description: desc, Content: content, Metadata: meta,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
}

func newTestSkillsClient(t *testing.T, serverURL string) *sdkskills.Client {
	t.Helper()
	return sdkskills.NewClient(http.DefaultClient, serverURL, noopAuth{}, "", "")
}

// ──────────────────────────────────────────────────────────────────────────────
// Skill provenance metadata on blueprint apply
// ──────────────────────────────────────────────────────────────────────────────

func TestBlueprintSkillCreate_SetsProvenanceMetadata(t *testing.T) {
	srv, s := newBlueprintSkillTestServer(t)
	b := NewBlueprintsApplier(nil, "", nil, nil, newTestSkillsClient(t, srv.URL), false, false, &bytes.Buffer{})

	sk := SkillFile{
		Name:        "deploy",
		Description: "Deploy skill",
		Content:     "# Deploy",
		Version:     "1.2",
		License:     "MIT",
		Metadata:    map[string]any{"author": "emergent"},
		SourceFile:  "skills/deploy/SKILL.md",
	}

	r := b.blueprintSkill(context.Background(), sk, nil)
	assert.Equal(t, BlueprintsActionCreated, r.Action)

	require.Len(t, s.creates, 1)
	meta := s.creates[0].Metadata
	assert.Equal(t, "blueprint", meta["source"])
	assert.Equal(t, "1.2", meta["version"])
	assert.Equal(t, "MIT", meta["license"])
	assert.Equal(t, "emergent", meta["author"])
}

func TestBlueprintSkillCreate_NoVersionLicense_OnlySource(t *testing.T) {
	srv, s := newBlueprintSkillTestServer(t)
	b := NewBlueprintsApplier(nil, "", nil, nil, newTestSkillsClient(t, srv.URL), false, false, &bytes.Buffer{})

	sk := SkillFile{Name: "research", Description: "d", Content: "c", SourceFile: "skills/research/SKILL.md"}
	r := b.blueprintSkill(context.Background(), sk, nil)
	assert.Equal(t, BlueprintsActionCreated, r.Action)

	require.Len(t, s.creates, 1)
	assert.Equal(t, map[string]any{"source": "blueprint"}, s.creates[0].Metadata)
}

func TestBlueprintSkill_MatchingNameSkipsWithoutDuplicate(t *testing.T) {
	srv, s := newBlueprintSkillTestServer(t)
	b := NewBlueprintsApplier(nil, "", nil, nil, newTestSkillsClient(t, srv.URL), false, false, &bytes.Buffer{})

	sk := SkillFile{Name: "deploy", Description: "d", Content: "c", Version: "1.0", SourceFile: "skills/deploy/SKILL.md"}

	// First apply: no existing skill → create.
	r := b.blueprintSkill(context.Background(), sk, nil)
	assert.Equal(t, BlueprintsActionCreated, r.Action)
	require.Len(t, s.creates, 1)

	// Second apply: skill exists by name → skipped, no duplicate create.
	existing := map[string]*sdkskills.Skill{"deploy": {ID: "skill-1", Name: "deploy"}}
	r = b.blueprintSkill(context.Background(), sk, existing)
	assert.Equal(t, BlueprintsActionSkipped, r.Action)
	assert.Equal(t, 1, s.posts, "no second POST when the skill already exists by name")
}

func TestBlueprintSkillUpdate_SendsProvenance(t *testing.T) {
	srv, s := newBlueprintSkillTestServer(t)
	b := NewBlueprintsApplier(nil, "", nil, nil, newTestSkillsClient(t, srv.URL), false, true /* upgrade */, &bytes.Buffer{})

	sk := SkillFile{
		Name:        "deploy",
		Description: "d2",
		Content:     "c2",
		Version:     "1.3",
		License:     "Apache-2.0",
		SourceFile:  "skills/deploy/SKILL.md",
	}
	existing := map[string]*sdkskills.Skill{"deploy": {ID: "skill-1", Name: "deploy"}}

	r := b.blueprintSkill(context.Background(), sk, existing)
	assert.Equal(t, BlueprintsActionUpdated, r.Action)

	require.Len(t, s.updates, 1)
	meta := s.updates[0].Metadata
	assert.Equal(t, "blueprint", meta["source"])
	assert.Equal(t, "1.3", meta["version"])
	assert.Equal(t, "Apache-2.0", meta["license"])
}

// ──────────────────────────────────────────────────────────────────────────────
// parseSkillMD — top-level version/license capture
// ──────────────────────────────────────────────────────────────────────────────

func TestParseSkillMD_CapturesVersionAndLicense(t *testing.T) {
	sf, err := parseSkillMD([]byte("---\nname: deploy\ndescription: d\nversion: \"1.0\"\nlicense: MIT\nmetadata:\n  author: me\n---\n# body\n"))
	require.NoError(t, err)
	assert.Equal(t, "1.0", sf.Version)
	assert.Equal(t, "MIT", sf.License)
	assert.Equal(t, map[string]any{"author": "me"}, sf.Metadata)
	assert.Contains(t, sf.Content, "# body")
}
