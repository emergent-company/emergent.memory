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

func TestScopeAuthorityInfo(t *testing.T) {
	tests := []struct {
		name              string
		trust             bool
		grantAll          bool
		introspect        bool
		wantTrust         bool
		wantPermissive    bool
		wantIntrospection bool
	}{
		{
			name:  "default posture: trust on, permissive on, introspection off",
			trust: true, grantAll: true,
			wantTrust: true, wantPermissive: true, wantIntrospection: false,
		},
		{
			name:  "trust off, introspection configured suppresses the permissive grant",
			trust: false, grantAll: true, introspect: true,
			wantTrust: false, wantPermissive: false, wantIntrospection: true,
		},
		{
			name:  "grant flag off",
			trust: true, grantAll: false,
			wantTrust: true, wantPermissive: false, wantIntrospection: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			z := config.ZitadelConfig{
				TrustTokenScopes:       tt.trust,
				UserinfoGrantAllScopes: tt.grantAll,
			}
			if tt.introspect {
				z.ClientJWT = "jwt"
			}
			h := &Handler{cfg: &config.Config{Zitadel: z}}
			got := h.scopeAuthorityInfo()
			if got.TokenScopesTrusted != tt.wantTrust {
				t.Errorf("token_scopes_trusted = %v, want %v", got.TokenScopesTrusted, tt.wantTrust)
			}
			if got.PermissiveAllGrant != tt.wantPermissive {
				t.Errorf("permissive_all_grant = %v, want %v", got.PermissiveAllGrant, tt.wantPermissive)
			}
			if got.IntrospectionConfigured != tt.wantIntrospection {
				t.Errorf("introspection_configured = %v, want %v", got.IntrospectionConfigured, tt.wantIntrospection)
			}
		})
	}
}

// TestOverallHealthIgnoresInformationalEntries pins the promise that any check
// entry outside the critical/optional component lists never changes system
// health semantics, so a "warning" (and even a hypothetical "unhealthy") entry
// must leave the overall status "healthy" with HTTP 200.
func TestOverallHealthIgnoresInformationalEntries(t *testing.T) {
	healthy := func() map[string]Check {
		return map[string]Check{
			"database":        {Status: "healthy"},
			"storage":         {Status: "healthy"},
			"auth":            {Status: "healthy"},
			"kreuzberg":       {Status: "healthy"},
			"whisper":         {Status: "healthy"},
			"embeddings":      {Status: "healthy"},
			"database_backup": {Status: "healthy"},
			"informational":   {Status: "warning", Message: "config warning"},
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
	checks["informational"] = Check{Status: "unhealthy"}
	status, code = overallHealth(checks)
	if status != "healthy" || code != http.StatusOK {
		t.Errorf("informational entry moved the overall status: status = %q, code = %d; want healthy/200", status, code)
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

// TestRunChecksDoesNotLeakOIDCAllGrant pins the retroactive #808 fix: the
// anonymous health checks map must NOT contain an oidc_all_grant entry (the
// permissive posture must not leak publicly). The scope-authority posture is
// served only on the authenticated /api/health/scope-authority endpoint.
func TestRunChecksDoesNotLeakOIDCAllGrant(t *testing.T) {
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
	if _, ok := checks["oidc_all_grant"]; ok {
		t.Fatalf("runChecks leaked the oidc_all_grant entry on the anonymous health response: %v", checks)
	}
}
