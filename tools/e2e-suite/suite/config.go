// Package suite provides shared infrastructure for e2e test suites.
package suite

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config holds shared configuration for all e2e suites.
type Config struct {
	// Server
	ServerURL string // MEMORY_SERVER_URL (default: http://mcj-emergent:3002)
	APIKey    string // MEMORY_API_KEY
	OrgID     string // MEMORY_ORG_ID
	ProjectID string // MEMORY_PROJECT_ID

	// Runtime
	Concurrency int           // --concurrency (default: 4)
	Timeout     time.Duration // --timeout (default: 30m)
	DryRun      bool          // --dry-run

	// Output
	OutputFormat string // --output: "text" | "json"

	// EnvFile path loaded at startup
	EnvFile string
}

// Load reads config from environment variables (after loading the .env file if present).
// Caller may override individual fields after calling Load.
func Load(envFile string) (*Config, error) {
	if envFile != "" {
		if err := godotenv.Load(envFile); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("loading env file %s: %w", envFile, err)
		}
	} else {
		// Try default .env silently
		_ = godotenv.Load()
	}

	cfg := &Config{
		ServerURL:    getEnv("MEMORY_SERVER_URL", "http://mcj-emergent:3002"),
		APIKey:       os.Getenv("MEMORY_API_KEY"),
		OrgID:        os.Getenv("MEMORY_ORG_ID"),
		ProjectID:    os.Getenv("MEMORY_PROJECT_ID"),
		Concurrency:  getEnvInt("E2E_CONCURRENCY", 4),
		Timeout:      getEnvDuration("E2E_TIMEOUT", 30*time.Minute),
		DryRun:       os.Getenv("E2E_DRY_RUN") == "true",
		OutputFormat: getEnv("E2E_OUTPUT", "text"),
		EnvFile:      envFile,
	}

	return cfg, nil
}

// Validate checks that mandatory fields are set.
func (c *Config) Validate() error {
	if c.ServerURL == "" {
		return fmt.Errorf("MEMORY_SERVER_URL is required")
	}
	if c.APIKey == "" {
		return fmt.Errorf("MEMORY_API_KEY is required")
	}
	if c.ProjectID == "" {
		return fmt.Errorf("MEMORY_PROJECT_ID is required")
	}
	return nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
