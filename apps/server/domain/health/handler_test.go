package health

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/emergent-company/emergent.memory/internal/config"
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

// fakeRow is a minimal pgx.Row whose Scan populates the same destinations as
// the database_backup query (string, *string, *time.Time), or returns a canned
// error (pgx.ErrNoRows, or an arbitrary query error).
type fakeRow struct {
	scanErr error
	status  string
	errMsg  *string
	started *time.Time
}

func (r fakeRow) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if len(dest) > 0 {
		if s, ok := dest[0].(*string); ok {
			*s = r.status
		}
	}
	if len(dest) > 1 {
		if s, ok := dest[1].(**string); ok {
			*s = r.errMsg
		}
	}
	if len(dest) > 2 {
		if s, ok := dest[2].(**time.Time); ok {
			*s = r.started
		}
	}
	return nil
}

type fakeRowQuerier struct {
	row pgx.Row
}

func (f fakeRowQuerier) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return f.row
}

func TestDatabaseBackupCheck(t *testing.T) {
	tests := []struct {
		name       string
		row        pgx.Row
		wantStatus string
		wantMsg    string
		msgHas     string
	}{
		{
			name:       "no rows recorded",
			row:        fakeRow{scanErr: pgx.ErrNoRows},
			wantStatus: "healthy",
			wantMsg:    "no backups recorded yet",
		},
		{
			name:       "query error",
			row:        fakeRow{scanErr: errors.New("db down")},
			wantStatus: "healthy",
			msgHas:     "backup status unavailable: db down",
		},
		{
			name:       "completed backup",
			row:        fakeRow{status: "completed"},
			wantStatus: "healthy",
		},
		{
			name:       "failed backup with nullable fields populated",
			row:        fakeRow{status: "failed", errMsg: strPtr("connection refused"), started: timePtr(time.Now())},
			wantStatus: "unhealthy",
			msgHas:     "connection refused",
		},
		{
			name:       "failed backup with null error",
			row:        fakeRow{status: "failed"},
			wantStatus: "unhealthy",
			msgHas:     "database backup failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{db: fakeRowQuerier{row: tt.row}}
			got := h.databaseBackupCheck(context.Background())
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

func TestOIDCAllGrantCheck(t *testing.T) {
	tests := []struct {
		name       string
		flag       bool
		introspect bool
		wantStatus string
	}{
		{name: "flag on, introspection unconfigured", flag: true, wantStatus: "warning"},
		{name: "flag on, introspection configured", flag: true, introspect: true, wantStatus: "healthy"},
		{name: "flag off, introspection unconfigured", flag: false, wantStatus: "healthy"},
		{name: "flag off, introspection configured", flag: false, introspect: true, wantStatus: "healthy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			z := config.ZitadelConfig{UserinfoGrantAllScopes: tt.flag}
			if tt.introspect {
				z.ClientJWT = "jwt"
			}
			h := &Handler{cfg: &config.Config{Zitadel: z}}
			got := h.oidcAllGrantCheck()
			if got.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tt.wantStatus)
			}
			if tt.wantStatus == "warning" {
				if !strings.Contains(got.Message, "ZITADEL_CLIENT_JWT") {
					t.Errorf("message = %q, want it to contain %q", got.Message, "ZITADEL_CLIENT_JWT")
				}
				if !strings.Contains(got.Message, "ZITADEL_USERINFO_GRANT_ALL_SCOPES=false") {
					t.Errorf("message = %q, want it to contain %q", got.Message, "ZITADEL_USERINFO_GRANT_ALL_SCOPES=false")
				}
			}
		})
	}
}
