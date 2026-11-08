package skills

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func newTestRepo(t *testing.T) (*Repository, bun.IDB) {
	t.Helper()
	db := connectTestDB(t)
	return NewRepository(db, slog.Default(), WithEmbedder(&fakeEmbedder{vec: testVector()})), db
}

func TestCapture_ReviewQueueLifecycle(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()
	_, projectID := seedProject(t, db)
	pid := uuid.MustParse(projectID)
	agentID := uuid.New()
	runID := uuid.New()

	review := &SkillReview{
		ProjectID:  pid,
		AgentID:    &agentID,
		AgentRunID: &runID,
		AgentName:  "test-agent",
		Status:     ReviewStatusPending,
		Input:      map[string]any{"trigger_message": "hello"},
	}
	require.NoError(t, repo.CreateReview(ctx, review))

	// A duplicate insert for the same run is a no-op (partial unique index).
	dup := &SkillReview{
		ProjectID:  pid,
		AgentID:    &agentID,
		AgentRunID: &runID,
		AgentName:  "test-agent",
		Status:     ReviewStatusPending,
		Input:      map[string]any{"trigger_message": "again"},
	}
	require.NoError(t, repo.CreateReview(ctx, dup))
	var count int
	require.NoError(t, db.NewSelect().TableExpr("kb.skill_reviews").Where("agent_run_id = ?", runID).ColumnExpr("count(*)").Scan(ctx, &count))
	assert.Equal(t, 1, count)

	active, err := repo.HasActiveReview(ctx, agentID)
	require.NoError(t, err)
	assert.True(t, active)

	claimed, err := repo.ClaimPendingReview(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, runID, *claimed.AgentRunID)
	assert.Equal(t, ReviewStatusRunning, claimed.Status)

	// Nothing else to claim while it is running.
	next, err := repo.ClaimPendingReview(ctx)
	require.NoError(t, err)
	assert.Nil(t, next)

	require.NoError(t, repo.FinishReview(ctx, claimed.ID, ReviewStatusDone, "test/model", 10, 20, 1, ""))

	active, err = repo.HasActiveReview(ctx, agentID)
	require.NoError(t, err)
	assert.False(t, active)

	last, err := repo.LastReviewCreatedAt(ctx, agentID)
	require.NoError(t, err)
	assert.True(t, time.Since(last) < time.Minute, "last review is now")
}

func TestProposal_ApproveCreate(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()
	_, projectID := seedProject(t, db)
	pid := uuid.MustParse(projectID)
	agentID := uuid.New()
	proposals := NewProposalService(repo, slog.Default())

	p := &SkillProposal{
		ProjectID:   pid,
		AgentID:     &agentID,
		AgentName:   "research-assistant",
		Kind:        ProposalKindCreate,
		Name:        "research-assistant.empty-results",
		Description: "What to do when a search returns nothing",
		Content:     "# Empty results\n\nWiden the query, then say what you tried.\n",
		Source:      ProposalSourceBackground,
		Status:      ProposalStatusPending,
		Confidence:  0.9,
	}
	require.NoError(t, repo.CreateProposal(ctx, p))

	sk, err := proposals.Approve(ctx, pid, p.ID, uuid.New(), "looks useful")
	require.NoError(t, err)
	require.NotNil(t, sk)
	assert.Equal(t, p.Name, sk.Name)
	assert.True(t, sk.IsAgentCreated())
	assert.Equal(t, SkillLifecycleActive, sk.LifecycleStateOrDefault())
	require.NotNil(t, sk.ProjectID)
	assert.Equal(t, projectID, *sk.ProjectID)
	assert.NotEmpty(t, sk.ContentHashOf(), "the store computes the content hash")

	stored, err := repo.FindProposalByID(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, ProposalStatusApplied, stored.Status)
	assert.False(t, stored.AutoApplied)
	assert.Equal(t, "looks useful", stored.ReviewNote)

	// A second approve is a conflict, never a second write.
	_, err = proposals.Approve(ctx, pid, p.ID, uuid.New(), "")
	require.Error(t, err)
}

func TestProposal_ApprovePatchRefusesChangedBase(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()
	_, projectID := seedProject(t, db)
	pid := uuid.MustParse(projectID)
	proposals := NewProposalService(repo, slog.Default())

	base := &Skill{
		Name:        "agent.lesson",
		Description: "base",
		Content:     "alpha beta gamma",
		Metadata:    &SkillMetadata{Source: SourceAgent, CreatedBy: "agent", LifecycleState: SkillLifecycleActive},
		ProjectID:   &projectID,
	}
	require.NoError(t, repo.Create(ctx, base))

	p := &SkillProposal{
		ProjectID:       pid,
		AgentName:       "agent",
		Kind:            ProposalKindPatch,
		Name:            base.Name,
		Description:     base.Description,
		BaseSkillID:     &base.ID,
		BaseContentHash: base.ContentHashOf(),
		PatchOld:        "beta",
		PatchNew:        "BETA",
		Source:          ProposalSourceBackground,
		Status:          ProposalStatusPending,
		Confidence:      0.9,
	}
	require.NoError(t, repo.CreateProposal(ctx, p))

	// Change the base after staging.
	newContent := base.Content + " delta"
	_, err := repo.Update(ctx, base.ID, &UpdateSkillDTO{Content: &newContent})
	require.NoError(t, err)

	_, err = proposals.Approve(ctx, pid, p.ID, uuid.New(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed since")

	applied, err := repo.FindProposalByID(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, ProposalStatusPending, applied.Status, "a refused apply leaves the proposal reviewable")
}

func TestProposal_ApprovePatchPrefersPatchFieldsOverContent(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()
	_, projectID := seedProject(t, db)
	pid := uuid.MustParse(projectID)
	proposals := NewProposalService(repo, slog.Default())

	base := &Skill{
		Name:        "agent.patch-precedence",
		Description: "base",
		Content:     "alpha beta gamma",
		Metadata:    &SkillMetadata{Source: SourceAgent, CreatedBy: "agent", LifecycleState: SkillLifecycleActive},
		ProjectID:   &projectID,
	}
	require.NoError(t, repo.Create(ctx, base))

	// The model emitted BOTH a full-content rewrite and a targeted patch. The
	// patch is the change that will be written, so `content` must be ignored —
	// linters, the security scan and the write all judge the patched bytes.
	p := &SkillProposal{
		ProjectID:       pid,
		AgentName:       "agent",
		Kind:            ProposalKindPatch,
		Name:            base.Name,
		Description:     base.Description,
		Content:         "alpha OMEGA gamma", // the unrelated rewrite that must lose
		BaseSkillID:     &base.ID,
		BaseContentHash: base.ContentHashOf(),
		PatchOld:        "beta",
		PatchNew:        "BETA",
		Source:          ProposalSourceBackground,
		Status:          ProposalStatusPending,
		Confidence:      0.9,
	}
	require.NoError(t, repo.CreateProposal(ctx, p))

	applied, err := proposals.Approve(ctx, pid, p.ID, uuid.New(), "")
	require.NoError(t, err)
	require.NotNil(t, applied)

	stored, err := repo.FindByID(ctx, base.ID)
	require.NoError(t, err)
	assert.Equal(t, "alpha BETA gamma", stored.Content, "patch_old/patch_new win over content")
	assert.NotEqual(t, p.Content, stored.Content, "the model's content field must not be written")
	assert.Equal(t, computeContentHash("alpha BETA gamma"), stored.ContentHashOf())
}

func TestProposal_StaleApplyingIsRecoverable(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()
	_, projectID := seedProject(t, db)
	pid := uuid.MustParse(projectID)
	proposals := NewProposalService(repo, slog.Default())

	// stage creates a create-proposal and claims it, leaving a fresh `applying`
	// row exactly as a reviewer would between claim and write.
	stage := func(name string) *SkillProposal {
		t.Helper()
		p := &SkillProposal{
			ProjectID: pid, Kind: ProposalKindCreate, Name: name,
			Description: "d", Content: "# T\n\nbody", Source: ProposalSourceBackground,
			Status: ProposalStatusPending, Confidence: 0.9,
		}
		require.NoError(t, repo.CreateProposal(ctx, p))
		ok, err := repo.ClaimProposal(ctx, p.ID)
		require.NoError(t, err)
		require.True(t, ok)
		return p
	}
	backdateClaim := func(id uuid.UUID) {
		t.Helper()
		_, err := db.ExecContext(ctx,
			`UPDATE kb.skill_proposals SET claimed_at = now() - interval '20 minutes' WHERE id = ?`, id)
		require.NoError(t, err)
	}

	// A FRESH applying row is still refused by both review paths: that is the
	// double-review guard, not the bug.
	fresh := stage("agent.fresh-claim")
	_, err := proposals.Approve(ctx, pid, fresh.ID, uuid.New(), "")
	require.Error(t, err, "a fresh claim must not be approved")
	require.Error(t, proposals.Reject(ctx, pid, fresh.ID, uuid.New(), ""), "a fresh claim must not be rejected")

	// Once the claim is stale, the normal paths reclaim it.
	backdateClaim(fresh.ID)
	sk, err := proposals.Approve(ctx, pid, fresh.ID, uuid.New(), "recovered")
	require.NoError(t, err)
	require.NotNil(t, sk)

	reloaded, err := repo.FindProposalByID(ctx, fresh.ID)
	require.NoError(t, err)
	assert.Equal(t, ProposalStatusApplied, reloaded.Status)
	assert.Equal(t, "recovered", reloaded.ReviewNote)

	// Reject recovers a stale claim too, without touching the library.
	rejected := stage("agent.stale-reject")
	backdateClaim(rejected.ID)
	require.NoError(t, proposals.Reject(ctx, pid, rejected.ID, uuid.New(), "stale"))
	reloadedRejected, err := repo.FindProposalByID(ctx, rejected.ID)
	require.NoError(t, err)
	assert.Equal(t, ProposalStatusRejected, reloadedRejected.Status)

	// Reading the inbox sweeps a stranded claim back to pending so it is
	// visible again.
	swept := stage("agent.stale-sweep")
	backdateClaim(swept.ID)
	rows, err := proposals.List(ctx, pid, ProposalStatusPending, 50)
	require.NoError(t, err)
	found := false
	for _, row := range rows {
		if row.ID == swept.ID {
			found = true
			assert.Equal(t, ProposalStatusPending, row.Status)
		}
	}
	assert.True(t, found, "a swept proposal is back in the pending inbox")
}

func TestProposal_ApproveRejectsAgentActor(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()
	_, projectID := seedProject(t, db)
	pid := uuid.MustParse(projectID)
	proposals := NewProposalService(repo, slog.Default())

	p := &SkillProposal{
		ProjectID: pid, Kind: ProposalKindCreate, Name: "agent.self-review",
		Description: "d", Content: "# T\n\nbody", Source: ProposalSourceBackground,
		Status: ProposalStatusPending, Confidence: 0.9,
	}
	require.NoError(t, repo.CreateProposal(ctx, p))

	agentCtx := auth.WithActor(ctx, "agent", nil)
	_, err := proposals.Approve(agentCtx, pid, p.ID, uuid.New(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent may not approve")
}

func TestCurator_AgesAgentCreatedSkills(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()
	_, projectID := seedProject(t, db)
	pid := uuid.MustParse(projectID)

	old := &Skill{
		Name:        "agent.old-lesson",
		Description: "old",
		Content:     "# Old\n\nbody",
		Metadata:    &SkillMetadata{Source: SourceAgent, CreatedBy: "agent", LifecycleState: SkillLifecycleActive},
		ProjectID:   &projectID,
	}
	pinned := &Skill{
		Name:        "agent.pinned-lesson",
		Description: "pinned",
		Content:     "# Pinned\n\nbody",
		Metadata:    &SkillMetadata{Source: SourceAgent, CreatedBy: "agent", LifecycleState: SkillLifecycleActive, Pinned: true},
		ProjectID:   &projectID,
	}
	manual := &Skill{
		Name:        "human-authored",
		Description: "manual",
		Content:     "# Manual\n\nbody",
		ProjectID:   &projectID,
	}
	for _, sk := range []*Skill{old, pinned, manual} {
		require.NoError(t, repo.Create(ctx, sk))
	}
	_, err := db.ExecContext(ctx, `UPDATE kb.skills SET created_at = now() - interval '200 days', updated_at = now() - interval '200 days' WHERE project_id = ?`, pid)
	require.NoError(t, err)

	curator := NewCurator(repo, slog.Default())
	curator.SetProjectConfigReader(&stubCaptureReader{projectID: projectID})
	require.NoError(t, curator.Run(ctx))

	reloaded, err := repo.FindByID(ctx, old.ID)
	require.NoError(t, err)
	assert.Equal(t, SkillLifecycleArchived, reloaded.LifecycleStateOrDefault())

	reloadedPinned, err := repo.FindByID(ctx, pinned.ID)
	require.NoError(t, err)
	assert.Equal(t, SkillLifecycleActive, reloadedPinned.LifecycleStateOrDefault())

	reloadedManual, err := repo.FindByID(ctx, manual.ID)
	require.NoError(t, err)
	assert.Equal(t, SkillLifecycleActive, reloadedManual.LifecycleStateOrDefault())

	// Archived skills are not auto-loaded or semantically retrieved.
	visible, err := repo.FindForAgent(ctx, projectID, "")
	require.NoError(t, err)
	for _, sk := range visible {
		assert.NotEqual(t, old.Name, sk.Name)
	}
}

func TestNearestSkillSimilarity(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	for _, name := range []string{"alpha", "beta"} {
		sk := &Skill{Name: name, Description: name + " description", Content: "# " + name, ProjectID: &projectID}
		require.NoError(t, repo.Create(ctx, sk))
	}
	id, sim, err := repo.NearestSkillSimilarity(ctx, projectID, "", "alpha description")
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, id)
	assert.InDelta(t, 1.0, sim, 0.001, "the fake embedder returns one vector for every text")
}

type stubCaptureReader struct {
	projectID string
}

func (s *stubCaptureReader) SkillCaptureConfigForProject(context.Context, string) (*ProjectSkillCaptureConfig, error) {
	return &ProjectSkillCaptureConfig{
		Mode:             SkillCaptureSuggest,
		StaleAfterDays:   30,
		ArchiveAfterDays: 180,
	}, nil
}

func (s *stubCaptureReader) ListSkillCaptureProjects(context.Context) ([]string, error) {
	return []string{s.projectID}, nil
}

func TestRestoreSkill_ReactivatesArchivedSkillWithoutLosingMetadata(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	sk := &Skill{
		Name:        "agent.restorable",
		Description: "aged out",
		Content:     "# Lesson\n\nbody",
		Metadata: &SkillMetadata{
			Source:         SourceAgent,
			CreatedBy:      "agent",
			Author:         "diane",
			RunID:          "run-1",
			Version:        "1.2.3",
			LifecycleState: SkillLifecycleArchived,
		},
		ProjectID: &projectID,
	}
	require.NoError(t, repo.Create(ctx, sk))

	// Archived skills are out of the agent's view.
	visible, err := repo.FindForAgent(ctx, projectID, "")
	require.NoError(t, err)
	for _, v := range visible {
		assert.NotEqual(t, sk.Name, v.Name)
	}

	restored, err := repo.RestoreSkill(ctx, sk.ID, false)
	require.NoError(t, err)
	assert.Equal(t, SkillLifecycleActive, restored.LifecycleStateOrDefault())
	assert.False(t, restored.IsPinned())
	require.NotNil(t, restored.Metadata)
	assert.Equal(t, "diane", restored.Metadata.Author, "unrelated metadata survives the restore")
	assert.Equal(t, "run-1", restored.Metadata.RunID)
	assert.Equal(t, "1.2.3", restored.Metadata.Version)
	assert.Equal(t, computeContentHash(sk.Content), restored.ContentHashOf())

	// It is back in the agent's view.
	visible, err = repo.FindForAgent(ctx, projectID, "")
	require.NoError(t, err)
	found := false
	for _, v := range visible {
		if v.ID == sk.ID {
			found = true
		}
	}
	assert.True(t, found, "a restored skill is visible to the agent again")

	// Pinning is the durable form: the curator skips pinned skills.
	pinned, err := repo.RestoreSkill(ctx, sk.ID, true)
	require.NoError(t, err)
	assert.True(t, pinned.IsPinned())
	assert.Equal(t, SkillLifecycleActive, pinned.LifecycleStateOrDefault())
}

func TestProposal_ClaimIsExclusive(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()
	_, projectID := seedProject(t, db)
	pid := uuid.MustParse(projectID)

	p := &SkillProposal{
		ProjectID: pid, Kind: ProposalKindCreate, Name: "agent.claimed",
		Description: "d", Content: "# T\n\nbody", Source: ProposalSourceBackground,
		Status: ProposalStatusPending, Confidence: 0.9,
	}
	require.NoError(t, repo.CreateProposal(ctx, p))

	ok, err := repo.ClaimProposal(ctx, p.ID)
	require.NoError(t, err)
	assert.True(t, ok, "the first claim wins")

	ok, err = repo.ClaimProposal(ctx, p.ID)
	require.NoError(t, err)
	assert.False(t, ok, "a claimed proposal cannot be claimed twice")

	require.NoError(t, repo.MarkProposalApplied(ctx, p.ID, nil, false, ""))
	require.Error(t, repo.MarkProposalApplied(ctx, p.ID, nil, false, ""), "a proposal is applied once")

	// Reject is conditional too.
	ok, err = repo.MarkProposalRejected(ctx, p.ID, uuid.New(), "")
	require.NoError(t, err)
	assert.False(t, ok)
}
