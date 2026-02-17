package scheduler

import (
	"os"
	"strconv"
	"time"
)

// Config holds scheduler configuration
type Config struct {
	// Enabled controls whether the scheduler runs
	Enabled bool

	// RevisionCountRefreshInterval is the interval for refreshing revision counts
	RevisionCountRefreshInterval time.Duration

	// TagCleanupInterval is the interval for cleaning up unused tags
	TagCleanupInterval time.Duration

	// CacheCleanupInterval is the interval for cleaning up expired cache entries
	CacheCleanupInterval time.Duration

	// StaleJobCleanupInterval is the interval for cleaning up stale jobs
	StaleJobCleanupInterval time.Duration

	// StaleJobMinutes is how long a job can be running before it's considered stale
	StaleJobMinutes int

	// Cron schedule overrides (take precedence over intervals when set)
	// Standard cron format: "minute hour day-of-month month day-of-week"
	// Examples: "*/5 * * * *" (every 5 min), "0 2 * * *" (daily at 2am)
	RevisionCountRefreshSchedule string
	TagCleanupSchedule           string
	CacheCleanupSchedule         string
	StaleJobCleanupSchedule      string
}

// NewConfig creates a new Config from environment variables
func NewConfig() *Config {
	return &Config{
		Enabled:                      getEnvBool("SCHEDULER_ENABLED", true),
		RevisionCountRefreshInterval: getEnvDuration("REVISION_COUNT_REFRESH_INTERVAL_MS", 5*time.Minute),
		TagCleanupInterval:           getEnvDuration("TAG_CLEANUP_INTERVAL_MS", 5*time.Minute),
		CacheCleanupInterval:         getEnvDuration("CACHE_CLEANUP_INTERVAL", 15*time.Minute),
		StaleJobCleanupInterval:      getEnvDuration("STALE_JOB_CLEANUP_INTERVAL_MS", 10*time.Minute),
		StaleJobMinutes:              getEnvInt("STALE_JOB_MINUTES", 30),
		// Cron schedule overrides (empty string means use interval)
		RevisionCountRefreshSchedule: getEnvString("REVISION_COUNT_REFRESH_SCHEDULE", ""),
		TagCleanupSchedule:           getEnvString("TAG_CLEANUP_SCHEDULE", ""),
		CacheCleanupSchedule:         getEnvString("CACHE_CLEANUP_SCHEDULE", ""),
		StaleJobCleanupSchedule:      getEnvString("STALE_JOB_CLEANUP_SCHEDULE", ""),
	}
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

// getEnvDuration returns a duration from an environment variable (in milliseconds)
func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if ms, err := strconv.Atoi(val); err == nil {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return defaultVal
}

// getEnvString returns a string from an environment variable
func getEnvString(key string, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
