package blueprints

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// applyAgentManifest is the agents-only manifest both the seeded applied
// blueprint and the fetched upstream use, so a no-op comparison round-trips.
func applyAgentManifest(name, prompt string) BlueprintManifest {
	return BlueprintManifest{
		Agents: []AgentManifest{{
			Name:         name,
			Description:  "test agent",
			SystemPrompt: prompt,
			Tools:        []string{"skill-list"},
		}},
	}
}

// agentBlueprintJSON builds a repo-root blueprint.json with record metadata plus
// the embedded agents-only manifest, as loadBlueprintDir would parse it.
func agentBlueprintJSON(name, version, prompt string) string {
	m := applyAgentManifest(name, prompt)
	b, err := json.Marshal(map[string]any{
		"name":    name,
		"version": version,
		"agents":  m.Agents,
	})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// seedAppliedGitHubBlueprint inserts a published, applied GitHub-imported
// blueprint (plus its application record) for the update job to act on.
func seedAppliedGitHubBlueprint(t *testing.T, svc *Service, proj, name, version, prompt string) {
	t.Helper()
	ctx := context.Background()
	manifest, err := json.Marshal(applyAgentManifest(name, prompt))
	require.NoError(t, err)
	bp := &Blueprint{
		Name:       name,
		Version:    version,
		Status:     StatusPublished,
		Manifest:   manifest,
		ProjectID:  &proj,
		SourceKind: "github",
		SourceURL:  "https://github.com/acme/" + name,
		SourceRef:  "",
		Checksum:   manifestChecksum(manifest),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	require.NoError(t, svc.repo.Create(ctx, bp))
	require.NoError(t, svc.repo.RecordApplication(ctx, &BlueprintApplication{
		BlueprintID: bp.ID,
		ProjectID:   proj,
		Version:     version,
		Checksum:    bp.Checksum,
		Status:      "applied",
		AppliedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}))
}

// notFoundServer serves 404 to every path (the codeload "repository not found"
// failure), so the fetch step errors.
func notFoundServer(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestApplyBlueprintUpdate_NewerVersion(t *testing.T) {
	db := connectTestDB(t)
	svc := newApplyService(t, db)
	ctx := context.Background()
	_, proj := seedProject(t, db)

	name := uniqueName("widgets")
	seedAppliedGitHubBlueprint(t, svc, proj, name, "1.0.0", "hi")

	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/blueprint.json", body: agentBlueprintJSON(name, "2.0.0", "hi v2")},
	})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	job, err := svc.EnqueueBlueprintUpdate(ctx, proj, name)
	require.NoError(t, err)
	require.NoError(t, svc.RunUpdateJobs(ctx))

	got, err := svc.repo.GetUpdateJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, UpdateJobStatusSucceeded, got.Status)

	// New version created with source provenance carried through.
	newBP, err := svc.repo.GetByNameVersion(ctx, proj, name, "2.0.0")
	require.NoError(t, err)
	assert.Equal(t, "github", newBP.SourceKind)
	assert.Equal(t, "https://github.com/acme/"+name, newBP.SourceURL)
	assert.Empty(t, newBP.SourceRef)
	assert.Equal(t, StatusPublished, newBP.Status)

	// Applied: the project now applies v2.0.0 (the v1.0.0 application superseded).
	applied, err := svc.repo.ListApplied(ctx, proj)
	require.NoError(t, err)
	require.Len(t, applied, 1)
	assert.Equal(t, "2.0.0", applied[0].Version)

	var res struct {
		Noop    bool   `json:"noop"`
		Applied bool   `json:"applied"`
		Version string `json:"version"`
	}
	require.NoError(t, json.Unmarshal(got.Result, &res))
	assert.False(t, res.Noop)
	assert.True(t, res.Applied)
	assert.Equal(t, "2.0.0", res.Version)
}

func TestApplyBlueprintUpdate_NoopWhenUpToDate(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()
	_, proj := seedProject(t, db)

	name := uniqueName("stable")
	seedAppliedGitHubBlueprint(t, svc, proj, name, "2.0.0", "hi")

	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/blueprint.json", body: agentBlueprintJSON(name, "2.0.0", "hi")},
	})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	job, err := svc.EnqueueBlueprintUpdate(ctx, proj, name)
	require.NoError(t, err)
	require.NoError(t, svc.RunUpdateJobs(ctx))

	got, err := svc.repo.GetUpdateJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, UpdateJobStatusSucceeded, got.Status)

	var res struct {
		Noop bool `json:"noop"`
	}
	require.NoError(t, json.Unmarshal(got.Result, &res))
	assert.True(t, res.Noop, "up-to-date upstream must record a no-op")

	// No duplicate version minted: still exactly one row for the name.
	versions, err := svc.repo.ListVersionsByName(ctx, proj, name)
	require.NoError(t, err)
	assert.Len(t, versions, 1)
}

func TestApplyBlueprintUpdate_FetchFailureRequeues(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()
	_, proj := seedProject(t, db)

	name := uniqueName("broken")
	seedAppliedGitHubBlueprint(t, svc, proj, name, "1.0.0", "hi")
	svc.fetcher = testGitHubFetcher(t, notFoundServer(t).URL, 1<<20, 1<<20, 100)

	job, err := svc.EnqueueBlueprintUpdate(ctx, proj, name)
	require.NoError(t, err)
	require.NoError(t, svc.RunUpdateJobs(ctx))

	got, err := svc.repo.GetUpdateJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, UpdateJobStatusPending, got.Status, "a transient fetch failure must requeue for retry")
	assert.Equal(t, 1, got.Attempts)
	assert.NotEmpty(t, got.LastError)
}

func TestApplyBlueprintUpdate_TerminalFailure(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()
	_, proj := seedProject(t, db)

	name := uniqueName("terminal")
	seedAppliedGitHubBlueprint(t, svc, proj, name, "1.0.0", "hi")
	svc.fetcher = testGitHubFetcher(t, notFoundServer(t).URL, 1<<20, 1<<20, 100)

	job, err := svc.EnqueueBlueprintUpdate(ctx, proj, name)
	require.NoError(t, err)

	// Seed the attempt count one below the cap so the next claim exhausts it and
	// the failure is terminal (a fresh pending job with finished_at NULL bypasses
	// backoff and is claimed immediately).
	_, err = db.NewUpdate().Model((*BlueprintUpdateJob)(nil)).
		Where("id = ?", job.ID).
		Set("attempts = ?", updateJobMaxAttempts-1).
		Exec(ctx)
	require.NoError(t, err)

	require.NoError(t, svc.RunUpdateJobs(ctx))

	got, err := svc.repo.GetUpdateJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, UpdateJobStatusFailed, got.Status)
	assert.Equal(t, updateJobMaxAttempts, got.Attempts)
	assert.NotEmpty(t, got.LastError)
}
