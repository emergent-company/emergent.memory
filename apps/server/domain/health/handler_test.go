package health

import (
	"testing"

	"github.com/emergent-company/emergent.memory/internal/version"
)

func TestBuildInfo(t *testing.T) {
	origVersion, origCommit, origBuildTime := version.Version, version.GitCommit, version.BuildTime
	t.Cleanup(func() {
		version.Version = origVersion
		version.GitCommit = origCommit
		version.BuildTime = origBuildTime
	})

	version.Version = "v9.9.9"
	version.GitCommit = "abc1234"
	version.BuildTime = "2026-01-01T00:00:00Z"

	gotVersion, gotCommit, gotBuildTime := buildInfo()
	if gotVersion != "v9.9.9" {
		t.Errorf("version = %q, want %q", gotVersion, "v9.9.9")
	}
	if gotCommit != "abc1234" {
		t.Errorf("commit = %q, want %q", gotCommit, "abc1234")
	}
	if gotBuildTime != "2026-01-01T00:00:00Z" {
		t.Errorf("buildTime = %q, want %q", gotBuildTime, "2026-01-01T00:00:00Z")
	}
}

func TestBuildInfoNormalizesUnknownSentinel(t *testing.T) {
	origVersion, origCommit, origBuildTime := version.Version, version.GitCommit, version.BuildTime
	t.Cleanup(func() {
		version.Version = origVersion
		version.GitCommit = origCommit
		version.BuildTime = origBuildTime
	})

	// A build without ldflags leaves both fields at their "unknown" sentinel;
	// the health response must report them as absent, not as a bogus commit.
	version.Version = "dev"
	version.GitCommit = "unknown"
	version.BuildTime = "unknown"

	gotVersion, gotCommit, gotBuildTime := buildInfo()
	if gotVersion != "dev" {
		t.Errorf("version = %q, want %q", gotVersion, "dev")
	}
	if gotCommit != "" {
		t.Errorf("commit = %q, want empty string", gotCommit)
	}
	if gotBuildTime != "" {
		t.Errorf("buildTime = %q, want empty string", gotBuildTime)
	}
}
