// Package testutil provides utilities for API testing.
package testutil

import (
	"os"
	"strconv"

	"github.com/emergent/api-tests/client"
)

// Config holds test configuration loaded from environment.
type Config struct {
	// API base URL (default: http://localhost:3002)
	BaseURL string

	// Server type: "go" or "nestjs" (default: "go")
	ServerType client.ServerType

	// Token is a single auth token used in remote mode (no token mapping).
	// When set, it overrides all token-selection methods on the client.
	Token string

	// OrgID / ProjectID override the default test org/project IDs (remote mode).
	OrgID     string
	ProjectID string

	// SkipDB disables direct database access and fixture setup (remote mode).
	SkipDB bool

	// Database connection
	PostgresHost     string
	PostgresPort     string
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string
}

// LoadConfig loads configuration from environment variables.
func LoadConfig() *Config {
	cfg := &Config{
		BaseURL:          getEnv("API_BASE_URL", "http://localhost:3002"),
		ServerType:       client.ServerType(getEnv("SERVER_TYPE", "go")),
		PostgresHost:     getEnv("POSTGRES_HOST", "localhost"),
		PostgresPort:     getEnv("POSTGRES_PORT", "5432"),
		PostgresUser:     getEnv("POSTGRES_USER", "emergent"),
		PostgresPassword: getEnv("POSTGRES_PASSWORD", "emergent-dev-password"),
		PostgresDB:       getEnv("POSTGRES_DB", "emergent"),
		Token:            getEnv("E2E_API_TOKEN", ""),
		OrgID:            getEnv("E2E_ORG_ID", ""),
		ProjectID:        getEnv("E2E_PROJECT_ID", ""),
		SkipDB:           getBool("E2E_SKIP_DB", false),
	}

	// Env overrides for the default test org/project (remote mode targets a
	// deployed server whose org/project IDs differ from the hardcoded fixtures).
	if cfg.OrgID != "" {
		DefaultTestOrg.ID = cfg.OrgID
	}
	if cfg.ProjectID != "" {
		DefaultTestProject.ID = cfg.ProjectID
	}

	return cfg
}

// getEnv returns the environment variable value or the default.
func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// getBool returns the environment variable parsed as a bool, or the default.
// An empty or unparseable value falls back to the default.
func getBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// PostgresDSN returns the PostgreSQL connection string.
func (c *Config) PostgresDSN() string {
	return "postgres://" + c.PostgresUser + ":" + c.PostgresPassword +
		"@" + c.PostgresHost + ":" + c.PostgresPort + "/" + c.PostgresDB + "?sslmode=disable"
}
