package blueprints

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

// packManifestJSON builds the manifest JSON the import/fetch pipeline stores for
// a single-pack blueprint, so a seeded applied manifest round-trips identically
// to what a check fetches.
func packManifestJSON(name, version string) json.RawMessage {
	m := BlueprintManifest{Packs: []PackManifest{{
		Name:        name,
		Version:     version,
		ObjectTypes: []ObjectTypeDef{{Name: "Thing", Label: "Thing"}},
	}}}
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return json.RawMessage(b)
}

func packYAML(name, version string) string {
	return "name: " + name + "\nversion: " + version + "\nobjectTypes:\n  - name: Thing\n    label: Thing\n"
}

// packManifestJSONLabel is packManifestJSON with a distinct object label, used
// to simulate a manifest that a project model override rewrote locally.
func packManifestJSONLabel(name, version, label string) json.RawMessage {
	m := BlueprintManifest{Packs: []PackManifest{{
		Name:        name,
		Version:     version,
		ObjectTypes: []ObjectTypeDef{{Name: "Thing", Label: label}},
	}}}
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return json.RawMessage(b)
}

func TestCheckBlueprint_DetectsUpdate(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()
	_, proj := seedProject(t, db)

	name := uniqueName("widgets")
	appliedManifest := packManifestJSON(name, "1.0.0")
	bp := &Blueprint{
		Name:       name,
		Version:    "1.0.0",
		Status:     StatusPublished,
		Manifest:   appliedManifest,
		ProjectID:  &proj,
		SourceKind: "github",
		SourceURL:  "https://github.com/acme/widgets",
		SourceRef:  "",
		Checksum:   manifestChecksum(appliedManifest),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	require.NoError(t, svc.repo.Create(ctx, bp))
	require.NoError(t, svc.repo.RecordApplication(ctx, &BlueprintApplication{
		BlueprintID: bp.ID,
		ProjectID:   proj,
		Version:     "1.0.0",
		Checksum:    bp.Checksum,
		Status:      "applied",
		AppliedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}))

	// Upstream serves v2.0.0.
	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/packs/pack.yaml", body: packYAML(name, "2.0.0")},
	})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	check, err := svc.checkBlueprint(ctx, proj, name)
	require.NoError(t, err)
	assert.True(t, check.UpdateAvailable, "v2 upstream must flag an update")
	assert.Equal(t, "2.0.0", check.LatestVersion)
	assert.NotEmpty(t, check.LatestChecksum)

	// Persisted.
	stored, err := svc.repo.GetUpdateCheck(ctx, proj, name)
	require.NoError(t, err)
	assert.True(t, stored.UpdateAvailable)
	assert.Equal(t, "2.0.0", stored.LatestVersion)
}

func TestCheckBlueprint_NoUpdate(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()
	_, proj := seedProject(t, db)

	name := uniqueName("stable")
	appliedManifest := packManifestJSON(name, "1.0.0")
	bp := &Blueprint{
		Name:       name,
		Version:    "1.0.0",
		Status:     StatusPublished,
		Manifest:   appliedManifest,
		ProjectID:  &proj,
		SourceKind: "github",
		SourceURL:  "https://github.com/acme/widgets",
		Checksum:   manifestChecksum(appliedManifest),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	require.NoError(t, svc.repo.Create(ctx, bp))
	require.NoError(t, svc.repo.RecordApplication(ctx, &BlueprintApplication{
		BlueprintID: bp.ID,
		ProjectID:   proj,
		Version:     "1.0.0",
		Checksum:    bp.Checksum,
		Status:      "applied",
		AppliedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}))

	// Upstream serves the same v1.0.0.
	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/packs/pack.yaml", body: packYAML(name, "1.0.0")},
	})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	check, err := svc.checkBlueprint(ctx, proj, name)
	require.NoError(t, err)
	assert.False(t, check.UpdateAvailable, "same version + content must not flag an update")
	assert.Equal(t, "1.0.0", check.LatestVersion)
}

func TestGithubCredential_RoundTrip(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	_, proj := seedProject(t, db)

	hexKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	repo := NewRepository(db, testLogger())
	svc := NewService(ServiceParams{Repo: repo, Cfg: &config.Config{GitHubTokenEncryptionKey: hexKey}, Log: testLogger()})

	const token = "ghp_SECRET_PAT"
	require.NoError(t, svc.SaveGithubCredential(ctx, proj, token))

	// The stored row never contains the plaintext.
	cred, err := repo.GetGithubCredential(ctx, proj)
	require.NoError(t, err)
	assert.NotContains(t, string(cred.EncryptedToken), token)

	// Decryption recovers it for the outbound fetch only.
	decrypted, err := svc.decryptGithubToken(ctx, proj)
	require.NoError(t, err)
	assert.Equal(t, token, decrypted)
}

func TestGithubCredential_InertWhenUnconfigured(t *testing.T) {
	t.Parallel()
	svc := &Service{encryptor: nil, log: testLogger()}
	err := svc.SaveGithubCredential(context.Background(), uuid.NewString(), "ghp_x")
	require.Error(t, err)
	assert.Equal(t, apperror.ErrConflict.Code, err.(*apperror.Error).Code)
}

func TestGithubCredential_NoCredentialReturnsEmpty(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	_, proj := seedProject(t, db)

	hexKey, _ := crypto.GenerateKey()
	repo := NewRepository(db, testLogger())
	svc := NewService(ServiceParams{Repo: repo, Cfg: &config.Config{GitHubTokenEncryptionKey: hexKey}, Log: testLogger()})

	token, err := svc.decryptGithubToken(ctx, proj)
	require.NoError(t, err)
	assert.Empty(t, token, "no stored credential must yield an empty token, not an error")
}

func TestUpdateJob_ClaimRequeueAndBackoff(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	_, proj := seedProject(t, db)
	repo := NewRepository(db, testLogger())

	name := uniqueName("job")
	job, err := repo.EnqueueUpdateJob(ctx, proj, name, UpdateActionCheck)
	require.NoError(t, err)
	assert.Equal(t, UpdateJobStatusPending, job.Status)
	assert.Equal(t, 0, job.Attempts)

	// Idempotent enqueue: a second enqueue returns the same pending job.
	again, err := repo.EnqueueUpdateJob(ctx, proj, name, UpdateActionCheck)
	require.NoError(t, err)
	assert.Equal(t, job.ID, again.ID)

	// Claim marks it running and bumps attempts.
	claimed, err := repo.ClaimUpdateJobs(ctx, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, UpdateJobStatusRunning, claimed[0].Status)
	assert.Equal(t, 1, claimed[0].Attempts)

	// Requeue returns it to pending with the error and a failure timestamp.
	require.NoError(t, repo.RequeueUpdateJob(ctx, claimed[0].ID, "boom"))
	// Backoff (attempts=1 → 1m) means it is NOT immediately claimable.
	requeued, err := repo.ClaimUpdateJobs(ctx, 1)
	require.NoError(t, err)
	assert.Empty(t, requeued, "a just-requeued job must be blocked by backoff")
}

func TestUpdateJob_TerminalFailure(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	_, proj := seedProject(t, db)
	repo := NewRepository(db, testLogger())

	name := uniqueName("terminal")
	job, err := repo.EnqueueUpdateJob(ctx, proj, name, UpdateActionCheck)
	require.NoError(t, err)

	// Simulate a job that has already exhausted its attempts (claim bumps
	// attempts, so seed it at the cap via direct claims is slow; instead call
	// FailUpdateJob to prove the terminal transition).
	require.NoError(t, repo.FailUpdateJob(ctx, job.ID, "permanent"))
	got, err := repo.GetUpdateJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, UpdateJobStatusFailed, got.Status)
	assert.Equal(t, "permanent", got.LastError)
	assert.NotNil(t, got.FinishedAt)
}

// TestCheckBlueprint_OverrideImportNoFalsePositive guards the raw-upstream
// baseline: an import whose stored manifest was rewritten by project model
// overrides must NOT report a permanent same-version "update" when upstream is
// unchanged.
func TestCheckBlueprint_OverrideImportNoFalsePositive(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()
	_, proj := seedProject(t, db)

	name := uniqueName("overridden")
	// The raw upstream manifest the check will fetch, and the override-applied
	// manifest actually stored on the blueprint (different content, same ver).
	rawManifest := packManifestJSON(name, "1.0.0")
	overriddenManifest := packManifestJSONLabel(name, "1.0.0", "OverriddenThing")

	bp := &Blueprint{
		Name:                   name,
		Version:                "1.0.0",
		Status:                 StatusPublished,
		Manifest:               overriddenManifest,
		ProjectID:              &proj,
		SourceKind:             "github",
		SourceURL:              "https://github.com/acme/widgets",
		Checksum:               manifestChecksum(overriddenManifest),
		SourceManifestChecksum: canonicalManifestHash(rawManifest),
		CreatedAt:              time.Now(),
		UpdatedAt:              time.Now(),
	}
	require.NoError(t, svc.repo.Create(ctx, bp))
	require.NoError(t, svc.repo.RecordApplication(ctx, &BlueprintApplication{
		BlueprintID: bp.ID,
		ProjectID:   proj,
		Version:     "1.0.0",
		Checksum:    bp.Checksum,
		Status:      "applied",
		AppliedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}))

	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/packs/pack.yaml", body: packYAML(name, "1.0.0")},
	})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	check, err := svc.checkBlueprint(ctx, proj, name)
	require.NoError(t, err)
	assert.False(t, check.UpdateAvailable,
		"an override-rewritten import with unchanged upstream must not flag an update")
	assert.Equal(t, "1.0.0", check.LatestVersion)
}

// TestCheckBlueprint_ErrorPreservesPriorResult guards against a transient fetch
// failure clearing a previously-detected update.
func TestCheckBlueprint_ErrorPreservesPriorResult(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()
	_, proj := seedProject(t, db)

	name := uniqueName("sticky")
	appliedManifest := packManifestJSON(name, "1.0.0")
	bp := &Blueprint{
		Name:       name,
		Version:    "1.0.0",
		Status:     StatusPublished,
		Manifest:   appliedManifest,
		ProjectID:  &proj,
		SourceKind: "github",
		SourceURL:  "https://github.com/acme/widgets",
		Checksum:   manifestChecksum(appliedManifest),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	require.NoError(t, svc.repo.Create(ctx, bp))
	require.NoError(t, svc.repo.RecordApplication(ctx, &BlueprintApplication{
		BlueprintID: bp.ID,
		ProjectID:   proj,
		Version:     "1.0.0",
		Checksum:    bp.Checksum,
		Status:      "applied",
		AppliedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}))

	// A previously-successful check found an update.
	now := time.Now()
	require.NoError(t, svc.repo.UpsertUpdateCheck(ctx, &BlueprintUpdateCheck{
		ProjectID:       proj,
		BlueprintName:   name,
		LatestVersion:   "9.9.9",
		LatestChecksum:  "abc123",
		UpdateAvailable: true,
		LastCheckedAt:   &now,
		UpdatedAt:       now,
	}))

	// Upstream fetch fails (HTTP 500 → 502).
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(ts.Close)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	_, err := svc.checkBlueprint(ctx, proj, name)
	require.Error(t, err)

	stored, err := svc.repo.GetUpdateCheck(ctx, proj, name)
	require.NoError(t, err)
	assert.True(t, stored.UpdateAvailable, "a transient failure must not clear a known update")
	assert.Equal(t, "9.9.9", stored.LatestVersion, "a transient failure must not wipe latest_version")
	assert.NotEmpty(t, stored.LastCheckError)
}

// TestUpdateJob_ReclaimsStaleRunning verifies an orphaned running job (crashed
// or restarted mid-check) is reclaimed after its lease expires, while a fresh
// running job is not.
func TestUpdateJob_ReclaimsStaleRunning(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	_, proj := seedProject(t, db)
	repo := NewRepository(db, testLogger())

	name := uniqueName("stale")
	job, err := repo.EnqueueUpdateJob(ctx, proj, name, UpdateActionCheck)
	require.NoError(t, err)

	claimed, err := repo.ClaimUpdateJobs(ctx, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, UpdateJobStatusRunning, claimed[0].Status)

	// Within its lease a running job is not re-claimed.
	fresh, err := repo.ClaimUpdateJobs(ctx, 1)
	require.NoError(t, err)
	assert.Empty(t, fresh, "a fresh running job must not be reclaimed")

	// Age the lease past the reclaim window; it becomes claimable again.
	_, err = db.ExecContext(ctx,
		`UPDATE kb.blueprint_update_jobs SET started_at = now() - interval '31 minutes' WHERE id = ?`, job.ID)
	require.NoError(t, err)

	reclaimed, err := repo.ClaimUpdateJobs(ctx, 1)
	require.NoError(t, err)
	require.Len(t, reclaimed, 1, "an orphaned running job must be reclaimed")
	assert.Equal(t, job.ID, reclaimed[0].ID)
	assert.Equal(t, 2, reclaimed[0].Attempts)
}
