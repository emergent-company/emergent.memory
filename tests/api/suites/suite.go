// Package suites contains API test suites.
package suites

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent/api-tests/client"
	"github.com/emergent/api-tests/testutil"
)

// BaseSuite provides common functionality for all test suites.
type BaseSuite struct {
	suite.Suite
	Config  *testutil.Config
	Client  *client.Client
	DB      *testutil.DB
	Ctx     context.Context
	Tokens  *client.TestTokens
	Project string // Default test project ID
}

// SetupSuite initializes the test suite.
func (s *BaseSuite) SetupSuite() {
	s.Ctx = context.Background()

	// Load config
	s.Config = testutil.LoadConfig()

	// Create HTTP client
	s.Client = client.New(client.Config{
		BaseURL:    s.Config.BaseURL,
		ServerType: s.Config.ServerType,
	})
	s.Tokens = s.Client.Tokens()

	// Connect to database
	db, err := testutil.ConnectDB(s.Config)
	s.Require().NoError(err, "Failed to connect to database")
	s.DB = db

	// Setup default fixtures
	err = testutil.SetupDefaultFixtures(s.Ctx, s.DB)
	s.Require().NoError(err, "Failed to setup fixtures")

	s.Project = testutil.DefaultTestProject.ID
}

// TearDownSuite cleans up after the test suite.
func (s *BaseSuite) TearDownSuite() {
	if s.DB != nil {
		s.DB.Close()
	}
}

// SetupTest runs before each test.
func (s *BaseSuite) SetupTest() {
	// Reset metrics before each test
	s.Client.ResetMetrics()
}

// RunSuite runs a test suite with the given name.
func RunSuite(t *testing.T, s suite.TestingSuite) {
	suite.Run(t, s)
}

// AdminAuth returns auth option with admin token.
func (s *BaseSuite) AdminAuth() client.Option {
	return client.WithAuth(s.Tokens.Admin())
}

// AllScopesAuth returns auth option with all-scopes token.
func (s *BaseSuite) AllScopesAuth() client.Option {
	return client.WithAuth(s.Tokens.AllScopes())
}

// ProjectHeader returns the default project header option.
func (s *BaseSuite) ProjectHeader() client.Option {
	return client.WithProjectID(s.Project)
}
