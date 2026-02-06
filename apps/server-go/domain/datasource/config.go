package datasource

import (
	"os"
	"strconv"
	"time"
)

// Config holds data source sync configuration
type Config struct {
	// Enabled controls whether the sync worker runs
	Enabled bool

	// WorkerIntervalMs is the polling interval in milliseconds
	WorkerIntervalMs int

	// WorkerBatchSize is the number of jobs to process per batch
	WorkerBatchSize int

	// StaleJobMinutes is how long a job can be running before it's considered stale
	StaleJobMinutes int

	// SyncTimeoutMinutes is the max time a single sync can run
	SyncTimeoutMinutes int
}

// NewConfig creates a new Config from environment variables
func NewConfig() *Config {
	return &Config{
		Enabled:            getEnvBool("DATASOURCE_SYNC_WORKER_ENABLED", true),
		WorkerIntervalMs:   getEnvInt("DATASOURCE_SYNC_WORKER_INTERVAL_MS", 30000),
		WorkerBatchSize:    getEnvInt("DATASOURCE_SYNC_WORKER_BATCH_SIZE", 5),
		StaleJobMinutes:    getEnvInt("DATASOURCE_SYNC_STALE_JOB_MINUTES", 10),
		SyncTimeoutMinutes: getEnvInt("DATASOURCE_SYNC_TIMEOUT_MINUTES", 30),
	}
}

// WorkerInterval returns the polling interval as a duration
func (c *Config) WorkerInterval() time.Duration {
	return time.Duration(c.WorkerIntervalMs) * time.Millisecond
}

// SyncTimeout returns the sync timeout as a duration
func (c *Config) SyncTimeout() time.Duration {
	return time.Duration(c.SyncTimeoutMinutes) * time.Minute
}

// getEnvBool returns a boolean from an environment variable
func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return defaultVal
}

// getEnvInt returns an integer from an environment variable
func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}
