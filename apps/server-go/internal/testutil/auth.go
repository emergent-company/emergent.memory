package testutil

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/pkg/auth"
)

// TestUser represents a test user fixture
type TestUser struct {
	ID            string
	ZitadelUserID string
	Email         string
	FirstName     string
	LastName      string
	Scopes        []string
}

// TestTokenConfig maps a test token to its user configuration.
// This is the single source of truth for test token -> user mappings.
type TestTokenConfig struct {
	Token  string   // The token string used in Authorization header
	Sub    string   // The zitadel_user_id (subject) this token maps to
	Scopes []string // Scopes granted to this token
}

// Predefined test users - these are created in the database by SetupTestFixtures.
// The ZitadelUserID must match the Sub field in TestTokenConfigs for the mapping to work.
var (
	// AdminUser - a user with admin privileges, used by e2e-test-user token
	AdminUser = TestUser{
		ID:            "00000000-0000-0000-0000-000000000001",
		ZitadelUserID: "test-admin-user",
		Email:         "admin@test.local",
		FirstName:     "Test",
		LastName:      "Admin",
		Scopes:        auth.GetAllScopes(),
	}

	// RegularUser - a standard user with basic scopes (no token maps to this by default)
	RegularUser = TestUser{
		ID:            "00000000-0000-0000-0000-000000000002",
		ZitadelUserID: "test-regular-user",
		Email:         "user@test.local",
		FirstName:     "Test",
		LastName:      "User",
		Scopes:        []string{"documents:read", "project:read", "search:read"},
	}

	// NoScopeUser - matches middleware "no-scope" test token
	NoScopeUser = TestUser{
		ID:            "00000000-0000-0000-0000-000000000003",
		ZitadelUserID: "test-user-no-scope",
		Email:         "noscope@test.local",
		FirstName:     "No",
		LastName:      "Scope",
		Scopes:        []string{},
	}

	// WithScopeUser - matches middleware "with-scope" test token
	WithScopeUser = TestUser{
		ID:            "00000000-0000-0000-0000-000000000004",
		ZitadelUserID: "test-user-with-scope",
		Email:         "withscope@test.local",
		FirstName:     "With",
		LastName:      "Scope",
		Scopes:        []string{"documents:read", "documents:write", "project:read"},
	}

	// AllScopesUser - matches middleware "all-scopes" test token
	AllScopesUser = TestUser{
		ID:            "00000000-0000-0000-0000-000000000005",
		ZitadelUserID: "test-user-all-scopes",
		Email:         "allscopes@test.local",
		FirstName:     "All",
		LastName:      "Scopes",
		Scopes:        auth.GetAllScopes(),
	}

	// GraphReadUser - matches middleware "graph-read" test token
	GraphReadUser = TestUser{
		ID:            "00000000-0000-0000-0000-000000000006",
		ZitadelUserID: "test-user-graph-read",
		Email:         "graphread@test.local",
		FirstName:     "Graph",
		LastName:      "Reader",
		Scopes:        []string{"graph:read", "graph:search:read"},
	}

	// ReadOnlyUser - matches middleware "read-only" test token (no write/delete permissions)
	ReadOnlyUser = TestUser{
		ID:            "00000000-0000-0000-0000-000000000007",
		ZitadelUserID: "test-user-read-only",
		Email:         "readonly@test.local",
		FirstName:     "Read",
		LastName:      "Only",
		Scopes:        []string{"documents:read", "project:read", "org:read", "chunks:read", "search:read", "graph:read"},
	}
)

// TestTokenConfigs defines all test tokens and their mappings.
// This should match the testTokens map in pkg/auth/middleware.go.
//
// Token naming convention:
//   - Simple tokens: "no-scope", "with-scope", "all-scopes", "graph-read", "read-only"
//   - E2E tokens: "e2e-test-user", "e2e-query-token" (mapped to AdminUser)
var TestTokenConfigs = []TestTokenConfig{
	{Token: "no-scope", Sub: "test-user-no-scope", Scopes: []string{}},
	{Token: "with-scope", Sub: "test-user-with-scope", Scopes: []string{"documents:read", "documents:write", "project:read"}},
	{Token: "read-only", Sub: "test-user-read-only", Scopes: []string{"documents:read", "project:read", "org:read", "chunks:read", "search:read", "graph:read"}},
	{Token: "graph-read", Sub: "test-user-graph-read", Scopes: []string{"graph:read", "graph:search:read"}},
	{Token: "all-scopes", Sub: "test-user-all-scopes", Scopes: auth.GetAllScopes()},
	{Token: "e2e-test-user", Sub: "test-admin-user", Scopes: auth.GetAllScopes()},
	{Token: "e2e-query-token", Sub: "test-admin-user", Scopes: auth.GetAllScopes()},
}

// GetTestTokenConfig returns the config for a given token, or nil if not found
func GetTestTokenConfig(token string) *TestTokenConfig {
	for _, cfg := range TestTokenConfigs {
		if cfg.Token == token {
			return &cfg
		}
	}
	return nil
}

// GetUserByZitadelID returns the TestUser that matches the given zitadel_user_id
func GetUserByZitadelID(zitadelUserID string) *TestUser {
	users := []TestUser{AdminUser, RegularUser, NoScopeUser, WithScopeUser, AllScopesUser, GraphReadUser}
	for _, u := range users {
		if u.ZitadelUserID == zitadelUserID {
			return &u
		}
	}
	return nil
}

// CreateTestUser inserts a test user into the database
func CreateTestUser(ctx context.Context, db bun.IDB, user TestUser) error {
	// Insert user profile
	_, err := db.NewRaw(`
		INSERT INTO core.user_profiles (id, zitadel_user_id, first_name, last_name, created_at, updated_at)
		VALUES (?, ?, ?, ?, NOW(), NOW())
		ON CONFLICT (zitadel_user_id) DO UPDATE SET
			first_name = EXCLUDED.first_name,
			last_name = EXCLUDED.last_name,
			updated_at = NOW()
	`, user.ID, user.ZitadelUserID, user.FirstName, user.LastName).Exec(ctx)
	if err != nil {
		return err
	}

	// Insert email if provided
	if user.Email != "" {
		_, err = db.NewRaw(`
			INSERT INTO core.user_emails (user_id, email, verified, created_at)
			VALUES (?, ?, true, NOW())
			ON CONFLICT (email) DO NOTHING
		`, user.ID, user.Email).Exec(ctx)
		if err != nil {
			return err
		}
	}

	return nil
}

// CreateTestAPIToken creates an API token for a test user
func CreateTestAPIToken(ctx context.Context, db bun.IDB, userID string, token string, scopes []string, projectID string) error {
	// Hash the token with SHA256 (64 hex chars fits varchar(64))
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])
	tokenPrefix := token[:min(12, len(token))]

	// Convert Go []string to PostgreSQL array literal format: {elem1,elem2,...}
	pgArray := "{" + strings.Join(scopes, ",") + "}"

	_, err := db.NewRaw(`
		INSERT INTO core.api_tokens (user_id, project_id, name, token_hash, token_prefix, scopes, created_at)
		VALUES (?, ?, 'test-token', ?, ?, ?::text[], NOW())
	`, userID, projectID, tokenHash, tokenPrefix, pgArray).Exec(ctx)

	return err
}

// CreateExpiredAPIToken creates an expired API token for testing
// Note: The actual schema doesn't have expires_at, so we create a revoked token instead
func CreateExpiredAPIToken(ctx context.Context, db bun.IDB, userID string, token string, projectID string) error {
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])
	tokenPrefix := token[:min(12, len(token))]

	// Simulate expired by setting revoked_at in the past
	_, err := db.NewRaw(`
		INSERT INTO core.api_tokens (user_id, project_id, name, token_hash, token_prefix, scopes, created_at, revoked_at)
		VALUES (?, ?, 'expired-token', ?, ?, '{}', NOW(), NOW() - INTERVAL '1 hour')
	`, userID, projectID, tokenHash, tokenPrefix).Exec(ctx)

	return err
}

// CreateDeletedAPIToken creates a soft-deleted (revoked) API token for testing
func CreateDeletedAPIToken(ctx context.Context, db bun.IDB, userID string, token string, projectID string) error {
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])
	tokenPrefix := token[:min(12, len(token))]

	_, err := db.NewRaw(`
		INSERT INTO core.api_tokens (user_id, project_id, name, token_hash, token_prefix, scopes, created_at, revoked_at)
		VALUES (?, ?, 'deleted-token', ?, ?, '{}', NOW(), NOW())
	`, userID, projectID, tokenHash, tokenPrefix).Exec(ctx)

	return err
}

// CacheIntrospectionResult caches a token introspection result
func CacheIntrospectionResult(ctx context.Context, db bun.IDB, token string, sub string, email string, scopes []string, expiresIn time.Duration) error {
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	scopeStr := ""
	for i, s := range scopes {
		if i > 0 {
			scopeStr += " "
		}
		scopeStr += s
	}

	data := map[string]any{
		"sub":   sub,
		"email": email,
		"scope": scopeStr,
	}

	_, err := db.NewRaw(`
		INSERT INTO kb.auth_introspection_cache (token_hash, introspection_data, expires_at)
		VALUES (?, ?, ?)
		ON CONFLICT (token_hash) DO UPDATE SET
			introspection_data = EXCLUDED.introspection_data,
			expires_at = EXCLUDED.expires_at
	`, tokenHash, data, time.Now().Add(expiresIn)).Exec(ctx)

	return err
}

// SetupTestFixtures creates all standard test fixtures
func SetupTestFixtures(ctx context.Context, db bun.IDB) error {
	// Create test users - include all predefined users that match middleware test tokens
	users := []TestUser{AdminUser, RegularUser, NoScopeUser, WithScopeUser, AllScopesUser, GraphReadUser, ReadOnlyUser}
	for _, user := range users {
		if err := CreateTestUser(ctx, db, user); err != nil {
			return err
		}
	}

	return nil
}

// AuthHeader returns an Authorization header value for a token
func AuthHeader(token string) string {
	return "Bearer " + token
}

// TestProject represents a test project fixture
type TestProject struct {
	ID    string
	Name  string
	OrgID string
}

// DefaultTestProject is a standard test project
var DefaultTestProject = TestProject{
	ID:    "00000000-0000-0000-0000-000000000100",
	Name:  "Test Project",
	OrgID: "00000000-0000-0000-0000-000000000200",
}

// CreateTestProject creates a test project in the database
func CreateTestProject(ctx context.Context, db bun.IDB, project TestProject, ownerID string) error {
	// First ensure the organization exists
	_, err := db.NewRaw(`
		INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, 'Test Organization', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`, project.OrgID).Exec(ctx)
	if err != nil {
		return err
	}

	// Create the project
	_, err = db.NewRaw(`
		INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at)
		VALUES (?, ?, ?, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			updated_at = NOW()
	`, project.ID, project.Name, project.OrgID).Exec(ctx)
	if err != nil {
		return err
	}

	return nil
}

// CreateTestOrganization creates a test organization in the database
func CreateTestOrganization(ctx context.Context, db bun.IDB, id, name string) error {
	_, err := db.NewRaw(`
		INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, ?, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			updated_at = NOW()
	`, id, name).Exec(ctx)
	return err
}

// TestDocument represents a test document fixture
type TestDocument struct {
	ID                      string
	ProjectID               string
	Filename                *string
	SourceURL               *string
	MimeType                *string
	Content                 *string
	SourceType              *string
	DataSourceIntegrationID *string
	ParentDocumentID        *string
}

// CreateTestDocument creates a test document in the database
func CreateTestDocument(ctx context.Context, db bun.IDB, doc TestDocument) error {
	// source_type defaults to 'upload' if not provided
	sourceType := "upload"
	if doc.SourceType != nil {
		sourceType = *doc.SourceType
	}

	_, err := db.NewRaw(`
		INSERT INTO kb.documents (
			id, project_id, filename, source_url, mime_type, content,
			source_type, data_source_integration_id, parent_document_id,
			created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			filename = EXCLUDED.filename,
			updated_at = NOW()
	`, doc.ID, doc.ProjectID, doc.Filename, doc.SourceURL, doc.MimeType,
		doc.Content, sourceType, doc.DataSourceIntegrationID, doc.ParentDocumentID).Exec(ctx)

	return err
}

// CreateTestDocumentWithTimestamp creates a test document with a specific timestamp
func CreateTestDocumentWithTimestamp(ctx context.Context, db bun.IDB, doc TestDocument, createdAt time.Time) error {
	// source_type defaults to 'upload' if not provided
	sourceType := "upload"
	if doc.SourceType != nil {
		sourceType = *doc.SourceType
	}

	_, err := db.NewRaw(`
		INSERT INTO kb.documents (
			id, project_id, filename, source_url, mime_type, content,
			source_type, data_source_integration_id, parent_document_id,
			created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			filename = EXCLUDED.filename,
			updated_at = NOW()
	`, doc.ID, doc.ProjectID, doc.Filename, doc.SourceURL, doc.MimeType,
		doc.Content, sourceType, doc.DataSourceIntegrationID, doc.ParentDocumentID, createdAt, createdAt).Exec(ctx)

	return err
}

// DeleteTestDocument deletes a test document (hard delete, no soft delete column exists)
func DeleteTestDocument(ctx context.Context, db bun.IDB, documentID string) error {
	_, err := db.NewRaw(`
		DELETE FROM kb.documents WHERE id = ?
	`, documentID).Exec(ctx)
	return err
}

// StringPtr is a helper to create a pointer to a string
func StringPtr(s string) *string {
	return &s
}

// CreateTestProjectMembership creates a project membership for a user
func CreateTestProjectMembership(ctx context.Context, db bun.IDB, projectID, userID, role string) error {
	_, err := db.NewRaw(`
		INSERT INTO kb.project_memberships (project_id, user_id, role, created_at)
		VALUES (?, ?, ?, NOW())
		ON CONFLICT (project_id, user_id) DO UPDATE SET
			role = EXCLUDED.role
	`, projectID, userID, role).Exec(ctx)
	return err
}

// CreateTestOrgMembership creates an organization membership for a user
func CreateTestOrgMembership(ctx context.Context, db bun.IDB, orgID, userID, role string) error {
	_, err := db.NewRaw(`
		INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at)
		VALUES (?, ?, ?, NOW())
		ON CONFLICT (organization_id, user_id) DO UPDATE SET
			role = EXCLUDED.role
	`, orgID, userID, role).Exec(ctx)
	return err
}

// TestChunk represents a test chunk fixture
type TestChunk struct {
	ID         string
	DocumentID string
	ChunkIndex int
	Text       string
}

// CreateTestChunk creates a test chunk in the database
func CreateTestChunk(ctx context.Context, db bun.IDB, chunk TestChunk) error {
	_, err := db.NewRaw(`
		INSERT INTO kb.chunks (id, document_id, chunk_index, text, created_at, updated_at)
		VALUES (?, ?, ?, ?, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			text = EXCLUDED.text,
			updated_at = NOW()
	`, chunk.ID, chunk.DocumentID, chunk.ChunkIndex, chunk.Text).Exec(ctx)
	return err
}
