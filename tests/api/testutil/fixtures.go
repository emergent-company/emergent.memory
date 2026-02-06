package testutil

import (
	"context"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// TestUser represents a test user fixture.
type TestUser struct {
	ID            string
	ZitadelUserID string
	Email         string
	FirstName     string
	LastName      string
}

// Predefined test users that match both Go and NestJS test token mappings.
var (
	// AdminUser - used by e2e-test-user token (both servers after alignment)
	AdminUser = TestUser{
		ID:            "00000000-0000-0000-0000-000000000001",
		ZitadelUserID: "test-admin-user",
		Email:         "admin@test.local",
		FirstName:     "Test",
		LastName:      "Admin",
	}

	// NoScopeUser - used by no-scope token
	NoScopeUser = TestUser{
		ID:            "00000000-0000-0000-0000-000000000003",
		ZitadelUserID: "test-user-no-scope",
		Email:         "noscope@test.local",
		FirstName:     "No",
		LastName:      "Scope",
	}

	// WithScopeUser - used by with-scope token
	WithScopeUser = TestUser{
		ID:            "00000000-0000-0000-0000-000000000004",
		ZitadelUserID: "test-user-with-scope",
		Email:         "withscope@test.local",
		FirstName:     "With",
		LastName:      "Scope",
	}

	// AllScopesUser - used by all-scopes token (Go)
	AllScopesUser = TestUser{
		ID:            "00000000-0000-0000-0000-000000000005",
		ZitadelUserID: "test-user-all-scopes",
		Email:         "allscopes@test.local",
		FirstName:     "All",
		LastName:      "Scopes",
	}

	// GraphReadUser - used by graph-read token
	GraphReadUser = TestUser{
		ID:            "00000000-0000-0000-0000-000000000006",
		ZitadelUserID: "test-user-graph-read",
		Email:         "graphread@test.local",
		FirstName:     "Graph",
		LastName:      "Reader",
	}

	// E2EAllUser - used by e2e-all token (NestJS)
	E2EAllUser = TestUser{
		ID:            "00000000-0000-0000-0000-000000000008",
		ZitadelUserID: "test-user-e2e-all",
		Email:         "e2eall@test.local",
		FirstName:     "E2E",
		LastName:      "All",
	}
)

// TestOrg represents a test organization fixture.
type TestOrg struct {
	ID   string
	Name string
}

// DefaultTestOrg is the default test organization.
var DefaultTestOrg = TestOrg{
	ID:   "00000000-0000-0000-0000-000000000200",
	Name: "Test Organization",
}

// TestProject represents a test project fixture.
type TestProject struct {
	ID    string
	Name  string
	OrgID string
}

// DefaultTestProject is the default test project.
var DefaultTestProject = TestProject{
	ID:    "00000000-0000-0000-0000-000000000100",
	Name:  "Test Project",
	OrgID: DefaultTestOrg.ID,
}

// SetupTestUsers creates all predefined test users in the database.
func SetupTestUsers(ctx context.Context, db *DB) error {
	users := []TestUser{AdminUser, NoScopeUser, WithScopeUser, AllScopesUser, GraphReadUser, E2EAllUser}

	for _, user := range users {
		if err := CreateUser(ctx, db, user); err != nil {
			return err
		}
	}
	return nil
}

// CreateUser creates a test user in the database.
// Returns the actual user ID (which may differ from user.ID if the user already exists).
func CreateUser(ctx context.Context, db *DB, user TestUser) error {
	// Use RETURNING to get the actual ID after upsert
	var actualID string
	err := db.NewRaw(`
		INSERT INTO core.user_profiles (id, zitadel_user_id, first_name, last_name, created_at, updated_at)
		VALUES (?, ?, ?, ?, NOW(), NOW())
		ON CONFLICT (zitadel_user_id) DO UPDATE SET
			first_name = EXCLUDED.first_name,
			last_name = EXCLUDED.last_name,
			updated_at = NOW()
		RETURNING id
	`, user.ID, user.ZitadelUserID, user.FirstName, user.LastName).Scan(ctx, &actualID)
	if err != nil {
		return err
	}

	if user.Email != "" {
		// Use the actual ID from the database, not the hardcoded one
		_, err = db.NewRaw(`
			INSERT INTO core.user_emails (user_id, email, verified, created_at)
			VALUES (?, ?, true, NOW())
			ON CONFLICT (email) DO NOTHING
		`, actualID, user.Email).Exec(ctx)
		if err != nil {
			return err
		}
	}

	return nil
}

// CreateOrg creates a test organization in the database.
func CreateOrg(ctx context.Context, db *DB, org TestOrg) error {
	_, err := db.NewRaw(`
		INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, ?, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			updated_at = NOW()
	`, org.ID, org.Name).Exec(ctx)
	return err
}

// CreateProject creates a test project in the database.
func CreateProject(ctx context.Context, db *DB, project TestProject) error {
	// Ensure org exists
	if err := CreateOrg(ctx, db, TestOrg{ID: project.OrgID, Name: "Test Org"}); err != nil {
		return err
	}

	_, err := db.NewRaw(`
		INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at)
		VALUES (?, ?, ?, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			updated_at = NOW()
	`, project.ID, project.Name, project.OrgID).Exec(ctx)
	return err
}

// CreateProjectMembership creates a project membership.
func CreateProjectMembership(ctx context.Context, db *DB, projectID, userID, role string) error {
	_, err := db.NewRaw(`
		INSERT INTO kb.project_memberships (project_id, user_id, role, created_at)
		VALUES (?, ?, ?, NOW())
		ON CONFLICT (project_id, user_id) DO UPDATE SET
			role = EXCLUDED.role
	`, projectID, userID, role).Exec(ctx)
	return err
}

// CreateOrgMembership creates an organization membership.
func CreateOrgMembership(ctx context.Context, db *DB, orgID, userID, role string) error {
	_, err := db.NewRaw(`
		INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at)
		VALUES (?, ?, ?, NOW())
		ON CONFLICT (organization_id, user_id) DO UPDATE SET
			role = EXCLUDED.role
	`, orgID, userID, role).Exec(ctx)
	return err
}

// SetupDefaultFixtures creates the default test fixtures:
// - All predefined test users
// - Default organization
// - Default project with admin as owner
func SetupDefaultFixtures(ctx context.Context, db *DB) error {
	// Create users
	if err := SetupTestUsers(ctx, db); err != nil {
		return err
	}

	// Create default org
	if err := CreateOrg(ctx, db, DefaultTestOrg); err != nil {
		return err
	}

	// Create default project
	if err := CreateProject(ctx, db, DefaultTestProject); err != nil {
		return err
	}

	// Get actual admin user ID from database
	adminID, err := GetUserIDByZitadelID(ctx, db, AdminUser.ZitadelUserID)
	if err != nil {
		return err
	}

	// Add admin as project owner
	if err := CreateProjectMembership(ctx, db, DefaultTestProject.ID, adminID, "owner"); err != nil {
		return err
	}

	// Add admin as org admin
	if err := CreateOrgMembership(ctx, db, DefaultTestOrg.ID, adminID, "admin"); err != nil {
		return err
	}

	return nil
}

// GetUserIDByZitadelID returns the user's UUID by their zitadel_user_id.
func GetUserIDByZitadelID(ctx context.Context, db *DB, zitadelUserID string) (string, error) {
	var id string
	err := db.NewRaw(`
		SELECT id FROM core.user_profiles WHERE zitadel_user_id = ?
	`, zitadelUserID).Scan(ctx, &id)
	return id, err
}

// NewUUID generates a new UUID string.
func NewUUID() string {
	return uuid.New().String()
}

// StringPtr returns a pointer to the given string.
func StringPtr(s string) *string {
	return &s
}

// TestDocument represents a test document for SQL insertion.
type TestDocument struct {
	ID        string
	ProjectID string
	Filename  string
}

// CreateTestDocument creates a document directly in the database.
// This is useful for tests that need to test chunk operations
// without going through the full document creation API.
func CreateTestDocument(ctx context.Context, db *DB, doc TestDocument) error {
	contentHash := NewUUID() // Random hash for testing
	_, err := db.NewRaw(`
		INSERT INTO kb.documents (id, project_id, filename, source_type, content_hash, file_size_bytes, sync_version, created_at, updated_at)
		VALUES (?, ?, ?, 'upload', ?, 1000, 1, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			filename = EXCLUDED.filename,
			updated_at = NOW()
	`, doc.ID, doc.ProjectID, doc.Filename, contentHash).Exec(ctx)
	return err
}

// TestChunk represents a test chunk for SQL insertion.
type TestChunk struct {
	ID         string
	DocumentID string
	ChunkIndex int
	Text       string
}

// CreateTestChunk creates a chunk directly in the database.
func CreateTestChunk(ctx context.Context, db *DB, chunk TestChunk) error {
	_, err := db.NewRaw(`
		INSERT INTO kb.chunks (id, document_id, chunk_index, text, created_at, updated_at)
		VALUES (?, ?, ?, ?, NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`, chunk.ID, chunk.DocumentID, chunk.ChunkIndex, chunk.Text).Exec(ctx)
	return err
}

// DeleteTestChunks deletes chunks by IDs (for test cleanup).
func DeleteTestChunks(ctx context.Context, db *DB, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := db.NewRaw(`DELETE FROM kb.chunks WHERE id IN (?)`, bun.In(ids)).Exec(ctx)
	return err
}

// DeleteTestDocuments deletes documents by IDs (for test cleanup).
func DeleteTestDocuments(ctx context.Context, db *DB, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	// First delete chunks
	_, _ = db.NewRaw(`DELETE FROM kb.chunks WHERE document_id IN (?)`, bun.In(ids)).Exec(ctx)
	// Then delete documents
	_, err := db.NewRaw(`DELETE FROM kb.documents WHERE id IN (?)`, bun.In(ids)).Exec(ctx)
	return err
}
