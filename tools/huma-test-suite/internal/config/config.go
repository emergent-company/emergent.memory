// Package config defines the shared configuration struct for the huma-test-suite.
package config

// Config holds all runtime configuration for the test suite.
type Config struct {
	// Phase controls which part of the pipeline to run.
	// Valid values: "download", "upload", "verify", "all"
	Phase string

	// FolderID is the Google Drive folder to sync from.
	FolderID string

	// CacheDir is the local directory where downloaded files are stored.
	CacheDir string

	// Concurrency is the number of parallel workers used in the upload phase.
	Concurrency int

	// Cleanup removes the local cache after the run if true.
	Cleanup bool

	// Emergent SDK settings
	EmergentServerURL string
	EmergentAPIKey    string
	EmergentOrgID     string
	EmergentProjectID string

	// Google Drive settings
	// GoogleServiceAccountJSON is the path to the service account key file.
	GoogleServiceAccountJSON string
}
