package health

import (
	"strings"
	"testing"
	"time"

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

func strPtr(s string) *string        { return &s }
func timePtr(t time.Time) *time.Time { return &t }

func TestClassifyDatabaseBackup(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	staleStart := now.Add(-7 * time.Hour)
	freshStart := now.Add(-1 * time.Hour)

	// 250-char message, so truncation is observable.
	longMsg := strings.Repeat("x", 250)

	tests := []struct {
		name       string
		status     string
		errMsg     *string
		startedAt  *time.Time
		wantStatus string
		wantMsg    string // "" means no assertion on message
		msgHas     string // substring the message must contain
	}{
		{
			name:       "completed",
			status:     "completed",
			wantStatus: "healthy",
		},
		{
			name:       "failed",
			status:     "failed",
			errMsg:     strPtr("connection refused"),
			wantStatus: "unhealthy",
			msgHas:     "connection refused",
		},
		{
			name:       "failed nil message",
			status:     "failed",
			wantStatus: "unhealthy",
			msgHas:     "database backup failed",
		},
		{
			name:       "failed message truncated",
			status:     "failed",
			errMsg:     strPtr(longMsg),
			wantStatus: "unhealthy",
			wantMsg:    longMsg[:200] + "...",
		},
		{
			name:       "stale running",
			status:     "running",
			startedAt:  timePtr(staleStart),
			wantStatus: "unhealthy",
			msgHas:     staleStart.Format(time.RFC3339),
		},
		{
			name:       "stale pending",
			status:     "pending",
			startedAt:  timePtr(staleStart),
			wantStatus: "unhealthy",
		},
		{
			name:       "fresh running",
			status:     "running",
			startedAt:  timePtr(freshStart),
			wantStatus: "healthy",
		},
		{
			name:       "running no start time",
			status:     "running",
			wantStatus: "healthy",
		},
		{
			name:       "unknown status",
			status:     "weird",
			wantStatus: "healthy",
			msgHas:     "weird",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyDatabaseBackup(tt.status, tt.errMsg, tt.startedAt, now)
			if got.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tt.wantStatus)
			}
			if tt.wantMsg != "" && got.Message != tt.wantMsg {
				t.Errorf("message = %q, want %q", got.Message, tt.wantMsg)
			}
			if tt.msgHas != "" && !strings.Contains(got.Message, tt.msgHas) {
				t.Errorf("message = %q, want it to contain %q", got.Message, tt.msgHas)
			}
		})
	}
}
