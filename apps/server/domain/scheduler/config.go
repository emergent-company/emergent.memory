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

	// DocumentParsingStaleMinutes is the stale threshold specifically for document parsing jobs
	// (audio transcription can take hours). Defaults to 480 (8 hours).
	DocumentParsingStaleMinutes int

	// StaleBackupCleanupInterval is the interval for marking stuck backups as failed
	StaleBackupCleanupInterval time.Duration

	// StaleBackupMinutes is how long a backup can be "creating" before it is considered stale
	StaleBackupMinutes int

	// Cron schedule overrides (take precedence over intervals when set)
	// Standard cron format with seconds: "second minute hour day-of-month month day-of-week"
	// Examples: "0 */5 * * * *" (every 5 min), "0 0 2 * * *" (daily at 2am)
	RevisionCountRefreshSchedule string
	TagCleanupSchedule           string
	CacheCleanupSchedule         string
	StaleJobCleanupSchedule      string
	StaleBackupCleanupSchedule   string

	// DatabaseBackupSchedule is the cron schedule for full database backups
	// Standard cron format with seconds: "second minute hour day-of-month month day-of-week"
	// Default: "0 0 7 * * *" (daily at 7am)
	DatabaseBackupSchedule string

	// ZitadelProfileSyncSchedule is the cron schedule for syncing user profiles from Zitadel.
	// Default: "0 0 * * * *" (every hour)
	ZitadelProfileSyncSchedule string

	// SessionCleanupSchedule is the cron schedule for deleting ADK session data
	// for old completed runs. Default: "0 0 3 * * *" (daily at 3am).
	SessionCleanupSchedule string

	// SessionRetentionDays is the minimum age (in days) of a completed run before
	// its ADK session data (events, states, session rows) is deleted. Default: 90.
	SessionRetentionDays int

	// RetrievalTraceRetention is how long retrieval traces are kept before the
	// cleanup task deletes them. Default: 720h (30 days).
	RetrievalTraceRetention time.Duration

	// RetrievalTraceCleanupSchedule is the cron schedule for deleting expired
	// retrieval traces. Default: "0 0 4 * * *" (daily at 4am).
	RetrievalTraceCleanupSchedule string

	// RetrievalTraceCleanupInterval is the fallback interval used when the cron
	// schedule is unset or invalid. Default: 1h.
	RetrievalTraceCleanupInterval time.Duration

	// ProjectDeletionSweepInterval is the interval for hard-purging projects whose
	// deletion grace period has elapsed. Set via PROJECT_DELETION_SWEEP_INTERVAL
	// as a Go duration string (e.g. "1m", "90s"). Default: 1 minute.
	ProjectDeletionSweepInterval time.Duration

	// ProjectDeletionSweepSchedule is the cron schedule override for the project
	// deletion sweep. Empty string means use the interval.
	ProjectDeletionSweepSchedule string
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
		DocumentParsingStaleMinutes:  getEnvInt("DOCUMENT_PARSING_STALE_MINUTES", 480),
		// Cron schedule overrides (empty string means use interval)
		RevisionCountRefreshSchedule:  getEnvString("REVISION_COUNT_REFRESH_SCHEDULE", ""),
		TagCleanupSchedule:            getEnvString("TAG_CLEANUP_SCHEDULE", ""),
		CacheCleanupSchedule:          getEnvString("CACHE_CLEANUP_SCHEDULE", ""),
		StaleJobCleanupSchedule:       getEnvString("STALE_JOB_CLEANUP_SCHEDULE", ""),
		StaleBackupCleanupInterval:    getEnvDuration("STALE_BACKUP_CLEANUP_INTERVAL_MS", 15*time.Minute),
		StaleBackupMinutes:            getEnvInt("STALE_BACKUP_MINUTES", 60),
		StaleBackupCleanupSchedule:    getEnvString("STALE_BACKUP_CLEANUP_SCHEDULE", ""),
		DatabaseBackupSchedule:        getEnvString("DATABASE_BACKUP_SCHEDULE", "0 0 7 * * *"),
		ZitadelProfileSyncSchedule:    getEnvString("ZITADEL_PROFILE_SYNC_SCHEDULE", "0 0 * * * *"),
		SessionCleanupSchedule:        getEnvString("SESSION_CLEANUP_SCHEDULE", "0 0 3 * * *"),
		SessionRetentionDays:          getEnvInt("SESSION_RETENTION_DAYS", 90),
		RetrievalTraceRetention:       getEnvDuration("SEARCH_TRACE_RETENTION", 720*time.Hour),
		RetrievalTraceCleanupSchedule: getEnvString("RETRIEVAL_TRACE_CLEANUP_SCHEDULE", "0 0 4 * * *"),
		RetrievalTraceCleanupInterval: getEnvDuration("RETRIEVAL_TRACE_CLEANUP_INTERVAL", time.Hour),
		// Project deletion sweep: a Go duration string (e.g. "1m"), consistent
		// with PROJECT_DELETION_GRACE_PERIOD; default 1 minute.
		ProjectDeletionSweepInterval: getEnvDurationString("PROJECT_DELETION_SWEEP_INTERVAL", time.Minute),
		ProjectDeletionSweepSchedule: getEnvString("PROJECT_DELETION_SWEEP_SCHEDULE", ""),
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

// getEnvDurationString returns a duration parsed from a Go duration string
// (e.g. "1m", "90s"). Empty or invalid values return defaultVal.
func getEnvDurationString(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
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
