// Package version provides build-time version information.
// The variables are set via ldflags during build.
package version

// These variables are set at build time via -ldflags
var (
	// Version is the semantic version (e.g., "1.0.0")
	Version = "dev"

	// GitCommit is the short git commit hash
	GitCommit = "unknown"

	// BuildTime is the build timestamp in RFC3339 format
	BuildTime = "unknown"
)

// Info returns all version information as a struct
func Info() VersionInfo {
	return VersionInfo{
		Version:   Version,
		GitCommit: GitCommit,
		BuildTime: BuildTime,
	}
}

// VersionInfo holds all version-related information
type VersionInfo struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildTime string `json:"build_time"`
}
