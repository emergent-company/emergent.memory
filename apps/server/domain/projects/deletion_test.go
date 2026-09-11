package projects

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const testProjectUUID = "550e8400-e29b-41d4-a716-446655440000"

// fakeDeletionRepo is a DB-free test double for the deletionRepository interface.
type fakeDeletionRepo struct {
	stateFn  func(ctx context.Context, id string) (*time.Time, *time.Time, bool, error)
	markFn   func(ctx context.Context, id string, userID string, scheduleAt time.Time) (bool, error)
	cancelFn func(ctx context.Context, id string) (bool, error)
}

func (f *fakeDeletionRepo) GetDeletionState(ctx context.Context, id string) (*time.Time, *time.Time, bool, error) {
	if f.stateFn == nil {
		return nil, nil, false, nil
	}
	return f.stateFn(ctx, id)
}

func (f *fakeDeletionRepo) MarkPendingDeletion(ctx context.Context, id string, userID string, scheduleAt time.Time) (bool, error) {
	if f.markFn == nil {
		return false, nil
	}
	return f.markFn(ctx, id, userID, scheduleAt)
}

func (f *fakeDeletionRepo) CancelPendingDeletion(ctx context.Context, id string) (bool, error) {
	if f.cancelFn == nil {
		return false, nil
	}
	return f.cancelFn(ctx, id)
}

func newDeletionTestService(repo deletionRepository) *Service {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return &Service{
		deletionRepo: repo,
		gracePeriod:  DefaultDeletionGracePeriod,
		log:          log.With(logger.Scope("test")),
	}
}

// =============================================================================
// SetDeletionGracePeriod
// =============================================================================

func TestSetDeletionGracePeriod(t *testing.T) {
	svc := newDeletionTestService(&fakeDeletionRepo{})
	assert.Equal(t, DefaultDeletionGracePeriod, svc.gracePeriod)

	svc.ConfigureDeletionGracePeriod(2 * time.Hour)
	assert.Equal(t, 2*time.Hour, svc.gracePeriod)

	// Non-positive values are ignored, keeping the previous value.
	svc.ConfigureDeletionGracePeriod(0)
	assert.Equal(t, 2*time.Hour, svc.gracePeriod)
	svc.ConfigureDeletionGracePeriod(-time.Minute)
	assert.Equal(t, 2*time.Hour, svc.gracePeriod)
}

// =============================================================================
// RequestDeletion
// =============================================================================

func TestRequestDeletion_InvalidUUID(t *testing.T) {
	svc := newDeletionTestService(&fakeDeletionRepo{})
	_, err := svc.RequestDeletion(context.Background(), "not-a-uuid", "user-1")
	require.Error(t, err)

	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, "invalid-uuid", appErr.Code)
}

func TestRequestDeletion_AlreadyPendingIsIdempotent(t *testing.T) {
	existing := time.Now().Add(30 * time.Minute)
	markCalled := false

	repo := &fakeDeletionRepo{
		stateFn: func(_ context.Context, _ string) (*time.Time, *time.Time, bool, error) {
			return &existing, nil, true, nil
		},
		markFn: func(_ context.Context, _ string, _ string, _ time.Time) (bool, error) {
			markCalled = true
			return true, nil
		},
	}
	svc := newDeletionTestService(repo)

	info, err := svc.RequestDeletion(context.Background(), testProjectUUID, "user-1")
	require.NoError(t, err)
	assert.True(t, info.AlreadyPending)
	assert.WithinDuration(t, existing, info.ScheduledFor, time.Second)
	assert.False(t, markCalled, "mark must not run when already pending")
}

func TestRequestDeletion_MarksActiveProject(t *testing.T) {
	var capturedUser string
	var capturedAt time.Time

	repo := &fakeDeletionRepo{
		stateFn: func(_ context.Context, _ string) (*time.Time, *time.Time, bool, error) {
			return nil, nil, true, nil
		},
		markFn: func(_ context.Context, _ string, userID string, scheduleAt time.Time) (bool, error) {
			capturedUser = userID
			capturedAt = scheduleAt
			return true, nil
		},
	}
	svc := newDeletionTestService(repo)
	svc.ConfigureDeletionGracePeriod(time.Hour)

	before := time.Now()
	info, err := svc.RequestDeletion(context.Background(), testProjectUUID, "user-42")
	require.NoError(t, err)
	assert.False(t, info.AlreadyPending)
	assert.Equal(t, "user-42", capturedUser)
	assert.WithinDuration(t, before.Add(time.Hour), capturedAt, 5*time.Second)
	assert.WithinDuration(t, capturedAt, info.ScheduledFor, time.Millisecond)
}

func TestRequestDeletion_NotFound(t *testing.T) {
	repo := &fakeDeletionRepo{
		stateFn: func(_ context.Context, _ string) (*time.Time, *time.Time, bool, error) {
			return nil, nil, false, nil
		},
		markFn: func(_ context.Context, _ string, _ string, _ time.Time) (bool, error) {
			return false, nil
		},
	}
	svc := newDeletionTestService(repo)

	_, err := svc.RequestDeletion(context.Background(), testProjectUUID, "user-1")
	require.Error(t, err)

	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, "not_found", appErr.Code)
}

func TestRequestDeletion_LostRaceReturnsPending(t *testing.T) {
	// First read: no state. Mark affects 0 rows because a concurrent request won.
	// Re-read: now pending. Should be treated as idempotent success.
	existing := time.Now().Add(15 * time.Minute)
	calls := 0

	repo := &fakeDeletionRepo{
		stateFn: func(_ context.Context, _ string) (*time.Time, *time.Time, bool, error) {
			calls++
			if calls == 1 {
				return nil, nil, true, nil
			}
			return &existing, nil, true, nil
		},
		markFn: func(_ context.Context, _ string, _ string, _ time.Time) (bool, error) {
			return false, nil
		},
	}
	svc := newDeletionTestService(repo)

	info, err := svc.RequestDeletion(context.Background(), testProjectUUID, "user-1")
	require.NoError(t, err)
	assert.True(t, info.AlreadyPending)
	assert.WithinDuration(t, existing, info.ScheduledFor, time.Second)
	assert.Equal(t, 2, calls)
}

// =============================================================================
// CancelDeletion
// =============================================================================

func TestCancelDeletion(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := &fakeDeletionRepo{
			cancelFn: func(_ context.Context, _ string) (bool, error) { return true, nil },
		}
		svc := newDeletionTestService(repo)
		require.NoError(t, svc.CancelDeletion(context.Background(), testProjectUUID))
	})

	t.Run("not pending", func(t *testing.T) {
		repo := &fakeDeletionRepo{
			cancelFn: func(_ context.Context, _ string) (bool, error) { return false, nil },
		}
		svc := newDeletionTestService(repo)

		err := svc.CancelDeletion(context.Background(), testProjectUUID)
		require.Error(t, err)

		var appErr *apperror.Error
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, "not_found", appErr.Code)
		assert.Contains(t, appErr.Message, "not pending deletion")
	})

	t.Run("invalid uuid", func(t *testing.T) {
		svc := newDeletionTestService(&fakeDeletionRepo{})
		err := svc.CancelDeletion(context.Background(), "nope")
		require.Error(t, err)

		var appErr *apperror.Error
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, "invalid-uuid", appErr.Code)
	})
}

// =============================================================================
// DTO deletion status
// =============================================================================

func TestProjectToDTO_DeletionStatus(t *testing.T) {
	t.Run("active", func(t *testing.T) {
		p := &Project{ID: "p1", OrganizationID: "o1", Name: "n"}
		dto := p.ToDTO()
		assert.Equal(t, "active", dto.DeletionStatus)
		assert.Nil(t, dto.DeletionScheduledFor)
	})

	t.Run("pending deletion", func(t *testing.T) {
		when := time.Now().Add(time.Hour)
		p := &Project{ID: "p1", OrganizationID: "o1", Name: "n", DeletionScheduledFor: &when}
		dto := p.ToDTO()
		assert.Equal(t, "pending_deletion", dto.DeletionStatus)
		require.NotNil(t, dto.DeletionScheduledFor)
		assert.Equal(t, when, *dto.DeletionScheduledFor)
	})
}
