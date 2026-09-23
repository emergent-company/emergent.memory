package health

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/internal/version"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
	"github.com/emergent-company/emergent.memory/pkg/kreuzberg"
	"github.com/emergent-company/emergent.memory/pkg/whisper"
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

// TestOverallHealthIgnoresOIDCAllGrantWarning pins the promise that the new
// oidc_all_grant entry never changes system health semantics: it is deliberately
// absent from the critical/optional component lists, so a "warning" (and even a
// hypothetical "unhealthy") entry must leave the overall status "healthy" with
// HTTP 200.
func TestOverallHealthIgnoresOIDCAllGrantWarning(t *testing.T) {
	healthy := func() map[string]Check {
		return map[string]Check{
			"database":        {Status: "healthy"},
			"storage":         {Status: "healthy"},
			"auth":            {Status: "healthy"},
			"kreuzberg":       {Status: "healthy"},
			"whisper":         {Status: "healthy"},
			"embeddings":      {Status: "healthy"},
			"database_backup": {Status: "healthy"},
			"oidc_all_grant":  {Status: "warning", Message: "all-grant active"},
		}
	}

	status, code := overallHealth(healthy())
	if status != "healthy" {
		t.Errorf("overall status = %q, want %q", status, "healthy")
	}
	if code != http.StatusOK {
		t.Errorf("status code = %d, want %d", code, http.StatusOK)
	}

	checks := healthy()
	checks["oidc_all_grant"] = Check{Status: "unhealthy"}
	status, code = overallHealth(checks)
	if status != "healthy" || code != http.StatusOK {
		t.Errorf("oidc_all_grant moved the overall status: status = %q, code = %d; want healthy/200", status, code)
	}
}

// healthTestPool returns a pgxpool.Pool whose connections always fail to dial,
// so runChecks' database probe reports an error without touching a real
// database or the network.
func healthTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pc, err := pgxpool.ParseConfig("postgres://u:p@127.0.0.1:1/health_test")
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	pc.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("no database in unit tests")
	}
	p, err := pgxpool.NewWithConfig(context.Background(), pc)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// TestRunChecksEmitsOIDCAllGrant proves the registration itself, not just the
// check body: runChecks must emit the oidc_all_grant key with the warning status
// on the shipped default posture.
func TestRunChecksEmitsOIDCAllGrant(t *testing.T) {
	h := &Handler{
		pool:       healthTestPool(t),
		db:         fakeRowQuerier{row: fakeRow{scanErr: pgx.ErrNoRows}},
		cfg:        &config.Config{Zitadel: config.ZitadelConfig{UserinfoGrantAllScopes: true}},
		storage:    &storage.Service{},
		kreuzberg:  &kreuzberg.Client{},
		whisper:    &whisper.Client{},
		embeddings: &embeddings.Service{},
	}

	checks := h.runChecks(context.Background())
	got, ok := checks["oidc_all_grant"]
	if !ok {
		t.Fatalf("runChecks did not emit the oidc_all_grant entry; got keys %v", checks)
	}
	if got.Status != "warning" {
		t.Errorf("oidc_all_grant status = %q, want %q", got.Status, "warning")
	}
}
