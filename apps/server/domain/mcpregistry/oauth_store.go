package mcpregistry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

// --- OAuth credential rows (kb.mcp_server_oauth) ---

// FindOAuthByServerID returns the OAuth credential row for a server, or nil when
// none exists.
func (r *Repository) FindOAuthByServerID(ctx context.Context, serverID string) (*MCPServerOAuth, error) {
	row := &MCPServerOAuth{}
	err := r.db.NewSelect().
		Model(row).
		Where("server_id = ?", serverID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row, nil
}

// FindOAuthByServerIDs returns the OAuth credential rows for a set of servers,
// keyed by server id. It is the bulk read behind the list endpoint's per-server
// authorization summary.
func (r *Repository) FindOAuthByServerIDs(ctx context.Context, projectID string, serverIDs []string) (map[string]*MCPServerOAuth, error) {
	out := make(map[string]*MCPServerOAuth, len(serverIDs))
	if len(serverIDs) == 0 {
		return out, nil
	}
	rows := []*MCPServerOAuth{}
	err := r.db.NewSelect().
		Model(&rows).
		Where("project_id = ?", projectID).
		Where("server_id IN (?)", bun.In(serverIDs)).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ServerID] = row
	}
	return out, nil
}

// UpsertOAuth inserts or updates the OAuth row for a server. Callers pass the
// full desired state; created_at is preserved on update.
func (r *Repository) UpsertOAuth(ctx context.Context, row *MCPServerOAuth) error {
	if row == nil || row.ServerID == "" {
		return fmt.Errorf("oauth row requires a server id")
	}
	_, err := r.db.NewInsert().
		Model(row).
		On("CONFLICT (server_id) DO UPDATE").
		Set("project_id = EXCLUDED.project_id").
		Set("authorization_server = EXCLUDED.authorization_server").
		Set("token_endpoint = EXCLUDED.token_endpoint").
		Set("registration_endpoint = EXCLUDED.registration_endpoint").
		Set("resource = EXCLUDED.resource").
		Set("scope = EXCLUDED.scope").
		Set("client_id = EXCLUDED.client_id").
		Set("client_secret = EXCLUDED.client_secret").
		Set("access_token = EXCLUDED.access_token").
		Set("refresh_token = EXCLUDED.refresh_token").
		Set("expires_at = EXCLUDED.expires_at").
		Set("updated_at = now()").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("upserting mcp server oauth: %w", err)
	}
	return nil
}

// UpdateOAuthTokens persists a refreshed/new token set for a server without
// touching the registered client identity or the discovered endpoints.
func (r *Repository) UpdateOAuthTokens(ctx context.Context, serverID string, access, refresh *EncryptedSecret, scope string, expiresAt *time.Time) error {
	q := r.db.NewUpdate().
		Model((*MCPServerOAuth)(nil)).
		Set("access_token = ?", access).
		Set("refresh_token = ?", refresh).
		Set("updated_at = now()")
	if scope != "" {
		q = q.Set("scope = ?", scope)
	}
	if expiresAt != nil {
		q = q.Set("expires_at = ?", expiresAt)
	}
	if _, err := q.Where("server_id = ?", serverID).Exec(ctx); err != nil {
		return fmt.Errorf("updating mcp server oauth tokens: %w", err)
	}
	return nil
}

// DeleteOAuthByServerID removes the OAuth credential row for a server. It is a
// no-op when no row exists.
func (r *Repository) DeleteOAuthByServerID(ctx context.Context, serverID string) error {
	if _, err := r.db.NewDelete().
		Model((*MCPServerOAuth)(nil)).
		Where("server_id = ?", serverID).
		Exec(ctx); err != nil {
		return fmt.Errorf("deleting mcp server oauth: %w", err)
	}
	return nil
}

// --- Pending authorize flows (kb.oauth_pending_states) ---
//
// The pending state table is shared with the data-source connect flow: it is a
// provider-generic, single-use, short-lived binding between an authorize request
// and its project/user, carrying the PKCE verifier until the callback consumes
// it. The MCP flow writes provider = oauthProviderMCP and stores the server id
// in the config JSONB.

// mcpOAuthPendingState is the row shape of kb.oauth_pending_states.
type mcpOAuthPendingState struct {
	bun.BaseModel `bun:"table:kb.oauth_pending_states,alias:ops"`

	ID           string         `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"-"`
	State        string         `bun:"state,notnull" json:"-"`
	ProjectID    string         `bun:"project_id,type:uuid,notnull" json:"-"`
	Provider     string         `bun:"provider,notnull" json:"-"`
	UserID       *string        `bun:"user_id,type:uuid" json:"-"`
	CodeVerifier string         `bun:"code_verifier,notnull" json:"-"`
	RedirectURI  string         `bun:"redirect_uri,notnull" json:"-"`
	Config       map[string]any `bun:"config,type:jsonb,notnull" json:"-"`
	Scopes       []string       `bun:"scopes,type:jsonb,notnull" json:"-"`
	ExpiresAt    time.Time      `bun:"expires_at,notnull" json:"-"`
	ConsumedAt   *time.Time     `bun:"consumed_at" json:"-"`
	CreatedAt    time.Time      `bun:"created_at,notnull,default:now()" json:"-"`
}

// CreateOAuthPendingState persists a pending authorize flow.
func (r *Repository) CreateOAuthPendingState(ctx context.Context, st *mcpOAuthPendingState) error {
	if _, err := r.db.NewInsert().Model(st).Exec(ctx); err != nil {
		return fmt.Errorf("creating oauth pending state: %w", err)
	}
	return nil
}

// ConsumeOAuthPendingState atomically marks a pending state consumed and returns
// it. A missing, expired, or already-consumed state resolves to nil, so a state
// can never be replayed.
func (r *Repository) ConsumeOAuthPendingState(ctx context.Context, state string) (*mcpOAuthPendingState, error) {
	out := &mcpOAuthPendingState{}
	err := r.db.NewUpdate().
		Model((*mcpOAuthPendingState)(nil)).
		Set("consumed_at = now()").
		Where("state = ?", state).
		Where("consumed_at IS NULL").
		Where("expires_at > now()").
		Returning("*").
		Scan(ctx, out)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("consuming oauth pending state: %w", err)
	}
	return out, nil
}

// --- Encryption helpers ---

// encryptSecretValue encrypts a plaintext secret for storage alongside a server.
// An empty plaintext returns nil (nothing to store).
func encryptSecretValue(enc *crypto.Encryptor, plaintext string) (*EncryptedSecret, error) {
	if plaintext == "" {
		return nil, nil
	}
	if enc == nil {
		return nil, fmt.Errorf("credential encryption not configured (LLM_ENCRYPTION_KEY missing)")
	}
	ct, nonce, err := enc.Encrypt([]byte(plaintext))
	if err != nil {
		return nil, fmt.Errorf("encrypting secret value: %w", err)
	}
	return &EncryptedSecret{Ciphertext: ct, Nonce: nonce}, nil
}

// decryptSecretValue decrypts a stored secret, returning "" for an absent value.
func decryptSecretValue(enc *crypto.Encryptor, secret *EncryptedSecret) (string, error) {
	if secret == nil || len(secret.Ciphertext) == 0 {
		return "", nil
	}
	if enc == nil {
		return "", fmt.Errorf("credential encryption not configured (LLM_ENCRYPTION_KEY missing)")
	}
	plaintext, err := enc.Decrypt(secret.Ciphertext, secret.Nonce)
	if err != nil {
		return "", fmt.Errorf("decrypting secret value: %w", err)
	}
	return string(plaintext), nil
}
