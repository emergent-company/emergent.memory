package auth

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"
	"github.com/zitadel/oidc/v3/pkg/client"
	"github.com/zitadel/oidc/v3/pkg/client/rs"

	"github.com/emergent/emergent-core/internal/config"
	"github.com/emergent/emergent-core/pkg/logger"
)

// ZitadelService handles token introspection with Zitadel
type ZitadelService struct {
	db  bun.IDB
	cfg *config.Config
	log *slog.Logger

	// Resource server for introspection (lazy initialized)
	resourceServer rs.ResourceServer
	rsOnce         sync.Once
	rsErr          error

	// Circuit breaker state
	lastFailureTime time.Time
	failureMu       sync.RWMutex

	// Request coalescing - prevent thundering herd
	inflight   map[string]*inflightRequest
	inflightMu sync.Mutex
}

// inflightRequest tracks an in-progress introspection
type inflightRequest struct {
	done   chan struct{}
	result *IntrospectionResult
	err    error
}

// IntrospectionResult holds the parsed introspection response
type IntrospectionResult struct {
	Active   bool      `json:"active"`
	Sub      string    `json:"sub"`
	Email    string    `json:"email"`
	Scope    string    `json:"scope"`    // Space-separated scopes
	Exp      int64     `json:"exp"`      // Expiration timestamp (Unix)
	ClientID string    `json:"client_id"`
	Username string    `json:"username"`
	Name     string    `json:"name"`
	
	// Zitadel-specific claims
	Claims map[string]any `json:"-"` // All claims for role extraction
}

const (
	circuitBreakerCooldown = 30 * time.Second
)

// NewZitadelService creates a new Zitadel introspection service
func NewZitadelService(db bun.IDB, cfg *config.Config, log *slog.Logger) *ZitadelService {
	return &ZitadelService{
		db:       db,
		cfg:      cfg,
		log:      log.With(logger.Scope("zitadel")),
		inflight: make(map[string]*inflightRequest),
	}
}

// Introspect validates a token and returns its claims
// Returns nil, nil if introspection is disabled or unavailable (caller should fall back to other methods)
func (z *ZitadelService) Introspect(ctx context.Context, token string) (*IntrospectionResult, error) {
	// Check if introspection is disabled
	if z.cfg.Zitadel.DisableIntrospection {
		return nil, nil
	}

	// Check if we have client credentials configured
	if z.cfg.Zitadel.ClientJWT == "" && z.cfg.Zitadel.ClientJWTPath == "" {
		z.log.Debug("no Zitadel client JWT configured, skipping introspection")
		return nil, nil
	}

	// Check circuit breaker
	z.failureMu.RLock()
	if time.Since(z.lastFailureTime) < circuitBreakerCooldown {
		z.failureMu.RUnlock()
		z.log.Debug("circuit breaker open, skipping introspection")
		return nil, nil
	}
	z.failureMu.RUnlock()

	// Check PostgreSQL cache first
	cached, err := z.getCached(ctx, token)
	if err == nil && cached != nil {
		z.log.Debug("introspection cache hit")
		return cached, nil
	}

	// Request coalescing - check if there's already a request in flight
	tokenHash := z.hashToken(token)
	z.inflightMu.Lock()
	if req, exists := z.inflight[tokenHash]; exists {
		z.inflightMu.Unlock()
		// Wait for existing request
		<-req.done
		return req.result, req.err
	}

	// Create new inflight request
	req := &inflightRequest{done: make(chan struct{})}
	z.inflight[tokenHash] = req
	z.inflightMu.Unlock()

	// Perform introspection
	result, err := z.doIntrospect(ctx, token)

	// Store result and notify waiters
	req.result = result
	req.err = err
	close(req.done)

	// Clean up inflight map
	z.inflightMu.Lock()
	delete(z.inflight, tokenHash)
	z.inflightMu.Unlock()

	return result, err
}

// doIntrospect performs the actual introspection call
func (z *ZitadelService) doIntrospect(ctx context.Context, token string) (*IntrospectionResult, error) {
	// Initialize resource server (lazy, once)
	z.rsOnce.Do(func() {
		z.resourceServer, z.rsErr = z.createResourceServer(ctx)
		if z.rsErr != nil {
			z.log.Error("failed to create resource server", logger.Error(z.rsErr))
		}
	})

	if z.rsErr != nil {
		return nil, fmt.Errorf("resource server init failed: %w", z.rsErr)
	}

	// Call Zitadel introspection endpoint
	resp, err := rs.Introspect[*introspectionResponse](ctx, z.resourceServer, token)
	if err != nil {
		// Log the specific error for debugging
		z.log.Error("introspection call failed",
			logger.Error(err),
			slog.String("token_prefix", token[:min(20, len(token))]+"..."),
		)
		// Trip circuit breaker on server errors
		z.tripCircuitBreaker()
		return nil, fmt.Errorf("introspection failed: %w", err)
	}

	if resp == nil || !resp.Active {
		// Token is inactive - cache this briefly to avoid repeated calls
		result := &IntrospectionResult{Active: false}
		_ = z.cacheResult(ctx, token, result, 1*time.Minute)
		return result, nil
	}

	// Convert response to our result type
	result := &IntrospectionResult{
		Active:   resp.Active,
		Sub:      resp.Subject,
		Email:    resp.GetEmail(),
		Scope:    resp.Scope,
		Exp:      resp.Expiration.AsTime().Unix(),
		ClientID: resp.ClientID,
		Username: resp.GetPreferredUsername(),
		Name:     resp.GetName(),
		Claims:   resp.Claims,
	}

	// Cache the result
	ttl := z.cfg.Zitadel.IntrospectCacheTTL
	tokenTTL := time.Until(resp.Expiration.AsTime())
	if tokenTTL > 0 && tokenTTL < ttl {
		ttl = tokenTTL
	}
	if ttl > 0 {
		_ = z.cacheResult(ctx, token, result, ttl)
	}

	return result, nil
}

// createResourceServer initializes the OIDC resource server for introspection
func (z *ZitadelService) createResourceServer(ctx context.Context) (rs.ResourceServer, error) {
	// Load key file
	var keyFile *client.KeyFile
	var err error

	if z.cfg.Zitadel.ClientJWT != "" {
		keyFile, err = client.ConfigFromKeyFileData([]byte(z.cfg.Zitadel.ClientJWT))
	} else if z.cfg.Zitadel.ClientJWTPath != "" {
		keyFile, err = client.ConfigFromKeyFile(z.cfg.Zitadel.ClientJWTPath)
	} else {
		return nil, fmt.Errorf("no Zitadel client JWT configured")
	}

	if err != nil {
		return nil, fmt.Errorf("parse key file: %w", err)
	}

	// For service account keys, the "clientId" field is empty and we use "userId" instead.
	// The Zitadel library's KeyFile struct has:
	//   - ClientID for type="application"
	//   - UserID for type="serviceaccount"
	clientID := keyFile.ClientID
	if clientID == "" && keyFile.UserID != "" {
		clientID = keyFile.UserID
	}

	issuer := z.cfg.Zitadel.GetIssuer()
	z.log.Info("initializing Zitadel resource server",
		slog.String("issuer", issuer),
		slog.String("client_id", clientID),
		slog.String("key_type", keyFile.Type),
	)

	return rs.NewResourceServerJWTProfile(ctx, issuer, clientID, keyFile.KeyID, []byte(keyFile.Key))
}

// introspectionResponse wraps the OIDC introspection response
type introspectionResponse struct {
	Active     bool   `json:"active"`
	Scope      string `json:"scope"`
	ClientID   string `json:"client_id"`
	TokenType  string `json:"token_type"`
	Expiration Time   `json:"exp"`
	IssuedAt   Time   `json:"iat"`
	NotBefore  Time   `json:"nbf"`
	Subject    string `json:"sub"`
	Audience   any    `json:"aud"`
	Issuer     string `json:"iss"`
	JWTID      string `json:"jti"`
	
	// Standard OIDC claims
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	GivenName         string `json:"given_name"`
	FamilyName        string `json:"family_name"`
	
	// All claims for extension
	Claims map[string]any `json:"-"`
}

// Implement required interface methods
func (r *introspectionResponse) IsActive() bool            { return r.Active }
func (r *introspectionResponse) SetActive(active bool)     { r.Active = active }
func (r *introspectionResponse) GetEmail() string          { return r.Email }
func (r *introspectionResponse) GetPreferredUsername() string { return r.PreferredUsername }
func (r *introspectionResponse) GetName() string           { return r.Name }

// Time wraps time.Time for JSON unmarshaling from Unix timestamp
type Time struct {
	time.Time
}

func (t *Time) UnmarshalJSON(data []byte) error {
	var timestamp int64
	if err := json.Unmarshal(data, &timestamp); err != nil {
		return err
	}
	t.Time = time.Unix(timestamp, 0)
	return nil
}

func (t Time) AsTime() time.Time {
	return t.Time
}

// getCached retrieves a cached introspection result from PostgreSQL
func (z *ZitadelService) getCached(ctx context.Context, token string) (*IntrospectionResult, error) {
	tokenHash := z.hashToken(token)

	var result struct {
		Data      json.RawMessage `bun:"introspection_data"`
		ExpiresAt time.Time       `bun:"expires_at"`
	}

	err := z.db.NewSelect().
		TableExpr("kb.auth_introspection_cache").
		Column("introspection_data", "expires_at").
		Where("token_hash = ?", tokenHash).
		Where("expires_at > NOW()").
		Scan(ctx, &result)

	if err != nil {
		return nil, err
	}

	var cached IntrospectionResult
	if err := json.Unmarshal(result.Data, &cached); err != nil {
		return nil, err
	}

	return &cached, nil
}

// cacheResult stores an introspection result in PostgreSQL
func (z *ZitadelService) cacheResult(ctx context.Context, token string, result *IntrospectionResult, ttl time.Duration) error {
	tokenHash := z.hashToken(token)
	expiresAt := time.Now().Add(ttl)

	data, err := json.Marshal(result)
	if err != nil {
		return err
	}

	_, err = z.db.NewInsert().
		TableExpr("kb.auth_introspection_cache").
		Model(&struct {
			TokenHash         string          `bun:"token_hash"`
			IntrospectionData json.RawMessage `bun:"introspection_data,type:jsonb"`
			ExpiresAt         time.Time       `bun:"expires_at"`
		}{
			TokenHash:         tokenHash,
			IntrospectionData: data,
			ExpiresAt:         expiresAt,
		}).
		On("CONFLICT (token_hash) DO UPDATE").
		Set("introspection_data = EXCLUDED.introspection_data").
		Set("expires_at = EXCLUDED.expires_at").
		Exec(ctx)

	return err
}

// hashToken creates a SHA-512 hash of the token for cache lookup
func (z *ZitadelService) hashToken(token string) string {
	hash := sha512.Sum512([]byte(token))
	return hex.EncodeToString(hash[:])
}

// tripCircuitBreaker marks a failure to prevent repeated calls
func (z *ZitadelService) tripCircuitBreaker() {
	z.failureMu.Lock()
	z.lastFailureTime = time.Now()
	z.failureMu.Unlock()
	z.log.Warn("circuit breaker tripped due to introspection failure")
}

// ParseScopes extracts scopes from space-separated string
func ParseScopes(scope string) []string {
	if scope == "" {
		return []string{}
	}
	return strings.Split(scope, " ")
}

// UserInfoResult holds the response from the userinfo endpoint
type UserInfoResult struct {
	Sub               string `json:"sub"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	GivenName         string `json:"given_name"`
	FamilyName        string `json:"family_name"`
}

// GetUserInfo calls the OIDC userinfo endpoint with the user's access token.
// This is a simpler alternative to introspection that works without special permissions.
// The userinfo endpoint validates the token and returns user claims.
func (z *ZitadelService) GetUserInfo(ctx context.Context, accessToken string) (*UserInfoResult, error) {
	issuer := z.cfg.Zitadel.GetIssuer()
	userinfoURL := issuer + "/oidc/v1/userinfo"

	req, err := http.NewRequestWithContext(ctx, "GET", userinfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		z.log.Warn("userinfo request failed",
			slog.Int("status", resp.StatusCode),
			slog.String("body", string(body)),
		)
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("unauthorized: token invalid or expired")
		}
		return nil, fmt.Errorf("userinfo failed with status %d", resp.StatusCode)
	}

	var result UserInfoResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}

	z.log.Debug("userinfo success",
		slog.String("sub", result.Sub),
		slog.String("email", result.Email),
	)

	return &result, nil
}
