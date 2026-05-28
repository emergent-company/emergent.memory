package projects

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// =============================================================================
// Stub BranchReader
// =============================================================================

type stubBranchReader struct {
	id  *string
	err error
}

func (s *stubBranchReader) GetMainBranchID(_ context.Context, _ string) (*string, error) {
	return s.id, s.err
}

// =============================================================================
// Helpers
// =============================================================================

func newTestService() *Service {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil)).With(logger.Scope("test"))
	return &Service{
		repo:      nil, // not needed for unit tests below
		agentRepo: nil,
		log:       log,
	}
}

// =============================================================================
// enrichWithMainBranch tests
// =============================================================================

func TestEnrichWithMainBranch_NoReader(t *testing.T) {
	svc := newTestService()
	// branchReader is nil — should be a no-op
	dto := &ProjectDTO{ID: "proj-1", Name: "test"}
	svc.enrichWithMainBranch(context.Background(), dto)
	assert.Nil(t, dto.MainBranchID, "expected nil when no branchReader configured")
}

func TestEnrichWithMainBranch_ReturnsID(t *testing.T) {
	svc := newTestService()
	mainID := "branch-abc"
	svc.SetBranchReader(&stubBranchReader{id: &mainID})

	dto := &ProjectDTO{ID: "proj-1", Name: "test"}
	svc.enrichWithMainBranch(context.Background(), dto)

	require.NotNil(t, dto.MainBranchID)
	assert.Equal(t, mainID, *dto.MainBranchID)
}

func TestEnrichWithMainBranch_NoBranch(t *testing.T) {
	svc := newTestService()
	svc.SetBranchReader(&stubBranchReader{id: nil}) // project has no branches yet

	dto := &ProjectDTO{ID: "proj-1", Name: "test"}
	svc.enrichWithMainBranch(context.Background(), dto)

	assert.Nil(t, dto.MainBranchID, "expected nil when project has no main branch")
}

func TestEnrichWithMainBranch_ErrorIsNonFatal(t *testing.T) {
	svc := newTestService()
	svc.SetBranchReader(&stubBranchReader{err: assert.AnError})

	dto := &ProjectDTO{ID: "proj-1", Name: "test"}
	// Must not panic or return error; MainBranchID stays nil
	svc.enrichWithMainBranch(context.Background(), dto)

	assert.Nil(t, dto.MainBranchID)
}

// =============================================================================
// ProjectDTO.ToDTO tests — main_branch_id is a service-level concern so we
// verify the base ToDTO stays clean and enrichment is additive.
// =============================================================================

func TestToDTODoesNotSetMainBranchID(t *testing.T) {
	p := &Project{
		ID:             "proj-1",
		OrganizationID: "org-1",
		Name:           "My Project",
	}
	dto := p.ToDTO()
	assert.Nil(t, dto.MainBranchID, "ToDTO should not set MainBranchID; enrichWithMainBranch does that")
}

// =============================================================================
// SetBranchReader wiring
// =============================================================================

func TestSetBranchReader(t *testing.T) {
	svc := newTestService()
	assert.Nil(t, svc.branchReader)

	reader := &stubBranchReader{}
	svc.SetBranchReader(reader)
	assert.Equal(t, reader, svc.branchReader)
}
