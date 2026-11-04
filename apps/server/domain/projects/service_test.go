package projects

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
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
	svc.branchReader = &stubBranchReader{id: &mainID}

	dto := &ProjectDTO{ID: "proj-1", Name: "test"}
	svc.enrichWithMainBranch(context.Background(), dto)

	require.NotNil(t, dto.MainBranchID)
	assert.Equal(t, mainID, *dto.MainBranchID)
}

func TestEnrichWithMainBranch_NoBranch(t *testing.T) {
	svc := newTestService()
	svc.branchReader = &stubBranchReader{id: nil} // project has no branches yet

	dto := &ProjectDTO{ID: "proj-1", Name: "test"}
	svc.enrichWithMainBranch(context.Background(), dto)

	assert.Nil(t, dto.MainBranchID, "expected nil when project has no main branch")
}

func TestEnrichWithMainBranch_ErrorIsNonFatal(t *testing.T) {
	svc := newTestService()
	svc.branchReader = &stubBranchReader{err: assert.AnError}

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
// NewService wiring
// =============================================================================

func TestNewServiceWiresBranchReader(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil)).With(logger.Scope("test"))
	reader := &stubBranchReader{}
	svc := NewService(ServiceParams{
		Repo:         nil,
		AgentRepo:    nil,
		Log:          log,
		TokenRevoker: nil,
		BranchReader: reader,
	})
	assert.Equal(t, reader, svc.branchReader)
}

// =============================================================================
// Transfer validation — invalid IDs short-circuit before any repo/org lookups.
// =============================================================================

func TestTransfer_RejectsInvalidIDs(t *testing.T) {
	svc := newTestService()
	validUUID := "550e8400-e29b-41d4-a716-446655440000"
	userID := "00000000-0000-0000-0000-000000000001"

	// Invalid project ID.
	_, err := svc.Transfer(context.Background(), "not-a-uuid", validUUID, userID)
	require.Error(t, err)
	appErr, ok := err.(*apperror.Error)
	require.True(t, ok)
	assert.Equal(t, 400, appErr.HTTPStatus)
	assert.Equal(t, "invalid-uuid", appErr.Code)

	// Invalid destination org ID.
	_, err = svc.Transfer(context.Background(), validUUID, "not-a-uuid", userID)
	require.Error(t, err)
	appErr, ok = err.(*apperror.Error)
	require.True(t, ok)
	assert.Equal(t, 400, appErr.HTTPStatus)
	assert.Equal(t, "invalid-uuid", appErr.Code)
}

// =============================================================================
// Budget alert threshold validation
// =============================================================================

func TestValidateBudgetAlertThreshold(t *testing.T) {
	tests := []struct {
		name    string
		in      float64
		wantErr bool
	}{
		{name: "zero", in: 0, wantErr: true},
		{name: "negative", in: -0.2, wantErr: true},
		{name: "above one", in: 1.5, wantErr: true},
		{name: "half", in: 0.5, wantErr: false},
		{name: "exactly one", in: 1.0, wantErr: false},
		{name: "small fraction", in: 0.01, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBudgetAlertThreshold(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				appErr, ok := err.(*apperror.Error)
				require.True(t, ok)
				assert.Equal(t, 400, appErr.HTTPStatus)
				assert.Equal(t, "validation-failed", appErr.Code)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestToDTOIncludesBudgetAlertThreshold(t *testing.T) {
	p := &Project{
		ID:                   "p1",
		OrganizationID:       "o1",
		Name:                 "n",
		BudgetAlertThreshold: 0.35,
	}
	dto := p.ToDTO()
	require.NotNil(t, dto.BudgetAlertThreshold)
	assert.Equal(t, 0.35, *dto.BudgetAlertThreshold)
}
