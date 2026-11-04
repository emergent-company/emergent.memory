package sources

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// OAuthPendingState is a kb.oauth_pending_states row: a short-lived, single-use
// binding between an OAuth authorize request and its project/provider/user,
// holding the PKCE verifier and intended config until the callback consumes it.
type OAuthPendingState struct {
	bun.BaseModel `bun:"table:kb.oauth_pending_states,alias:ops"`

	ID           uuid.UUID      `bun:"id,pk,type:uuid" json:"-"`
	State        string         `bun:"state,notnull" json:"-"`
	ProjectID    uuid.UUID      `bun:"project_id,type:uuid,notnull" json:"-"`
	Provider     string         `bun:"provider,notnull" json:"-"`
	UserID       *uuid.UUID     `bun:"user_id,type:uuid" json:"-"`
	CodeVerifier string         `bun:"code_verifier,notnull" json:"-"`
	RedirectURI  string         `bun:"redirect_uri,notnull" json:"-"`
	Config       map[string]any `bun:"config,type:jsonb,notnull" json:"-"`
	Scopes       []string       `bun:"scopes,type:jsonb,notnull" json:"-"`
	ExpiresAt    time.Time      `bun:"expires_at,notnull" json:"-"`
	ConsumedAt   *time.Time     `bun:"consumed_at" json:"-"`
	CreatedAt    time.Time      `bun:"created_at,notnull,default:now()" json:"-"`
}

// CreateOAuthPendingState persists a pending state, assigning an id when unset.
func (r *Repository) CreateOAuthPendingState(ctx context.Context, st *OAuthPendingState) error {
	if st.ID == uuid.Nil {
		st.ID = uuid.New()
	}
	if _, err := r.db.NewInsert().Model(st).Exec(ctx); err != nil {
		r.log.Error("failed to create oauth pending state", logger.Error(err))
		return apperror.NewDatabase("failed to create oauth pending state", err)
	}
	return nil
}

// ConsumeOAuthPendingState atomically marks a state consumed and returns it,
// scoped to nothing but the unguessable state value. A missing, expired, or
// already-consumed state resolves to not-found, so a state can never be replayed.
func (r *Repository) ConsumeOAuthPendingState(ctx context.Context, state string) (*OAuthPendingState, error) {
	out := &OAuthPendingState{}
	err := r.db.NewUpdate().
		Model((*OAuthPendingState)(nil)).
		Set("consumed_at = now()").
		Where("state = ?", state).
		Where("consumed_at IS NULL").
		Where("expires_at > now()").
		Returning("*").
		Scan(ctx, out)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.ErrNotFound
		}
		r.log.Error("failed to consume oauth pending state", logger.Error(err))
		return nil, apperror.NewDatabase("failed to consume oauth pending state", err)
	}
	return out, nil
}

// DeleteExpiredOAuthPendingStates sweeps expired rows, returning the count.
func (r *Repository) DeleteExpiredOAuthPendingStates(ctx context.Context) (int, error) {
	res, err := r.db.NewDelete().
		Model((*OAuthPendingState)(nil)).
		Where("expires_at < now()").
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to delete expired oauth pending states", logger.Error(err))
		return 0, apperror.NewDatabase("failed to delete expired oauth pending states", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
