// Package testutil provides utilities for API testing.
package testutil

import (
	"os"

	"github.com/emergent/api-tests/client"
)

// Config holds test configuration loaded from environment.
type Config struct {
	// API base URL (default: http://localhost:3002)
	BaseURL string

	// Server type: "go" or "nestjs" (default: "go")
	ServerType client.ServerType

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

// PostgresDSN returns the PostgreSQL connection string.
func (c *Config) PostgresDSN() string {
	return "postgres://" + c.PostgresUser + ":" + c.PostgresPassword +
		"@" + c.PostgresHost + ":" + c.PostgresPort + "/" + c.PostgresDB + "?sslmode=disable"
}
