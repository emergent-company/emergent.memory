package testutil

import (
	"context"
	"os"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
	"github.com/uptrace/bun"
)

// BaseSuite provides common test infrastructure with automatic fixture setup.
// Embed this in your test suite to get:
//   - Automatic database setup/teardown per suite
//   - Per-test transaction isolation with rollback (fast cleanup)
//   - Common test fixtures (users, org, project)
//
// Environment variables:
//   - TEST_SERVER_URL: External server URL (e.g., "http://localhost:3002")
//   - If not set, uses in-process Go test server (requires DB access)
//
// Usage:
//
//	type MySuite struct {
//	    testutil.BaseSuite
//	}
//
//	func (s *MySuite) TestSomething() {
//	    // s.Client, s.ProjectID, s.OrgID are available
//	    resp := s.Client.GET("/api/something", testutil.WithAuth("e2e-test-user"))
//	}
type BaseSuite struct {
	suite.Suite
	TestDB    *TestDB
	Server    *TestServer // Deprecated: Use Client instead for new tests
	Client    *HTTPClient
	Ctx       context.Context
	ProjectID string
	OrgID     string

	// dbSuffix is used to create unique database names
	dbSuffix string

	// externalServer indicates if we're using an external server
	externalServer bool
}

// SetDBSuffix sets the database name suffix. Call this in your suite's SetupSuite
// before calling BaseSuite.SetupSuite.
func (s *BaseSuite) SetDBSuffix(suffix string) {
	s.dbSuffix = suffix
}

// SetupSuite creates the test database and server.
// If you override this, call s.BaseSuite.SetupSuite() first.
func (s *BaseSuite) SetupSuite() {
	s.Ctx = context.Background()

	if serverURL := os.Getenv("TEST_SERVER_URL"); serverURL != "" {
		s.T().Logf("Using external server: %s", serverURL)
		s.externalServer = true
		s.Client = NewExternalHTTPClient(serverURL)
		
		// Create DB connection for direct DB assertions if env vars are present
		if os.Getenv("POSTGRES_PORT") != "" {
			db, err := SetupTestDB(s.Ctx, "emergent")
			if err == nil {
				s.TestDB = db
			} else {
				s.T().Logf("Failed to setup external DB connection: %v", err)
			}
		}
	} else {
		s.T().Log("Using in-process test server")

		suffix := s.dbSuffix
		if suffix == "" {
			suffix = "test"
		}

		// Create isolated test database
		testDB, err := SetupTestDB(s.Ctx, suffix)
		s.Require().NoError(err, "Failed to setup test database")
		s.TestDB = testDB

		// Create test server with base DB (will be rebuilt per-test with transaction)
		s.Server = NewTestServer(testDB)
		s.Client = NewHTTPClient(s.Server.Echo)
	}
}

// TearDownSuite closes the test database.
// If you override this, call s.BaseSuite.TearDownSuite() at the end.
func (s *BaseSuite) TearDownSuite() {
	if s.TestDB != nil {
		s.TestDB.Close()
	}
}

// SetupTest starts a transaction and sets up fixtures.
// All changes within a test are automatically rolled back in TearDownTest.
// If you override this, call s.BaseSuite.SetupTest() first.
func (s *BaseSuite) SetupTest() {
	if s.externalServer {
		// For external server, create fixtures via API
		s.setupExternalFixtures()
		return
	}

	// Start a transaction for test isolation
	err := s.TestDB.BeginTestTx(s.Ctx)
	s.Require().NoError(err, "Failed to begin test transaction")

	// Rebuild server to use the transaction
	db := s.TestDB.GetDB()
	s.Server = newTestServerWithDB(s.TestDB, db)
	s.Client = NewHTTPClient(s.Server.Echo)

	// Set up test fixtures (users)
	err = SetupTestFixtures(s.Ctx, s.TestDB.GetDB())
	s.Require().NoError(err)

	// Create default org and project
	s.OrgID = uuid.New().String()
	s.ProjectID = uuid.New().String()

	err = CreateTestOrganization(s.Ctx, s.TestDB.GetDB(), s.OrgID, "Test Org")
	s.Require().NoError(err)

	err = CreateTestProject(s.Ctx, s.TestDB.GetDB(), TestProject{
		ID:    s.ProjectID,
		OrgID: s.OrgID,
		Name:  "Test Project",
	}, AdminUser.ID)
	s.Require().NoError(err)

	// Create memberships for the admin user
	err = CreateTestOrgMembership(s.Ctx, s.TestDB.GetDB(), s.OrgID, AdminUser.ID, "org_admin")
	s.Require().NoError(err)

	err = CreateTestProjectMembership(s.Ctx, s.TestDB.GetDB(), s.ProjectID, AdminUser.ID, "project_admin")
	s.Require().NoError(err)
}

// setupExternalFixtures creates test fixtures via API when using external server
func (s *BaseSuite) setupExternalFixtures() {
	// Create org via API
	orgID, err := s.Client.CreateOrg("Test Org "+uuid.New().String()[:8], "e2e-test-user")
	s.Require().NoError(err, "Failed to create org via API")
	s.OrgID = orgID

	// Create project via API
	projectID, err := s.Client.CreateProject("Test Project", orgID, "e2e-test-user")
	s.Require().NoError(err, "Failed to create project via API")
	s.ProjectID = projectID
}

// TearDownTest rolls back the transaction, discarding all test changes.
// This is much faster than TRUNCATE (~0ms vs ~500ms).
// Override this if you need test-specific cleanup.
func (s *BaseSuite) TearDownTest() {
	if s.externalServer {
		// For external server, clean up created fixtures via API
		if s.OrgID != "" {
			// Deleting org cascades to projects and memberships
			_ = s.Client.DeleteOrg(s.OrgID, "e2e-test-user")
		}
		return
	}
	_ = s.TestDB.RollbackTestTx()
}

// DB returns the current database connection (transaction if active, otherwise base DB).
// Returns nil if using external server (no DB access).
func (s *BaseSuite) DB() bun.IDB {
	if s.externalServer {
		return nil
	}
	return s.TestDB.GetDB()
}

// IsExternal returns true if using an external server
func (s *BaseSuite) IsExternal() bool {
	return s.externalServer
}

// SkipIfExternalServer skips the test if running against an external server.
// Use this for tests that require direct database access or test internal services
// that are not exposed via HTTP APIs.
func (s *BaseSuite) SkipIfExternalServer(reason string) {
	if s.externalServer {
		s.T().Skipf("Skipping in external server mode: %s", reason)
	}
}

// IsExternalServerMode returns true if TEST_SERVER_URL is set, indicating tests
// should run against an external server rather than in-process.
// Use this standalone function for test suites that don't embed BaseSuite.
func IsExternalServerMode() bool {
	return os.Getenv("TEST_SERVER_URL") != ""
}

// SkipInExternalMode skips the test if running in external server mode.
// Use this for tests that require direct database access or test internal services.
// This is a standalone function for test suites that don't embed BaseSuite.
//
// Example:
//
//	func (s *MySuite) SetupSuite() {
//	    testutil.SkipInExternalMode(s.T(), "tests internal scheduler service")
//	    // ... rest of setup
//	}
func SkipInExternalMode(t interface{ Skip(...any) }, reason string) {
	if IsExternalServerMode() {
		t.Skip("Skipping in external server mode: " + reason)
	}
}
