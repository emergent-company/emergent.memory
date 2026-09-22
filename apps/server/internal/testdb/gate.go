// Package testdb centralizes how database-backed tests pick up their DSN and
// how they behave when the database is unavailable.
//
// Two environment variables govern the behaviour:
//
//   - TEST_DATABASE_URL: an explicit PostgreSQL DSN used by database-backed
//     tests. When set it takes precedence over the ambient POSTGRES_* variables
//     (which default to the shared localhost:5432 instance), so a test run can
//     never accidentally connect to — or drop databases on — a shared or
//     application database.
//
//   - REQUIRE_DB: when set to a truthy value ("1", "true", "yes", "on"),
//     database-backed harnesses fail instead of skipping when the database is
//     missing, unreachable, or when the run is in short mode. CI's DB-backed
//     job sets this so that database coverage cannot silently disappear again
//     (see issue #778). Locally it stays unset, so a missing database still
//     skips for convenience.
package testdb

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/config"
)

const (
	// RequireEnv is the env var that turns unavailable-database skips into
	// failures. CI's DB-backed job sets it to "1".
	RequireEnv = "REQUIRE_DB"

	// URLEnv is the explicit test database DSN. When set it overrides the
	// ambient POSTGRES_* connection settings for database-backed tests.
	URLEnv = "TEST_DATABASE_URL"

	// UnavailableMsg is the conventional skip reason used when a database-backed
	// test cannot reach Postgres. Keep it stable so CI logs remain greppable.
	UnavailableMsg = "skipping: test database unavailable: %v"
)

// Required reports whether REQUIRE_DB is set to a truthy value.
func Required() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(RequireEnv))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// SkipOrFatal skips the calling test with the formatted reason, unless a
// database is required (REQUIRE_DB is truthy), in which case it fails the test.
// Use this for every "database unavailable" / short-mode skip in a DB-backed
// harness so that CI cannot silently pass without exercising the database.
func SkipOrFatal(t testing.TB, format string, args ...any) {
	t.Helper()
	if Required() {
		t.Fatalf("database required because %s is set; "+format, append([]any{RequireEnv}, args...)...)
	}
	t.Skipf(format, args...)
}

// URL returns the explicit test DSN from TEST_DATABASE_URL, and whether it was
// set to a non-empty value.
func URL() (string, bool) {
	dsn := strings.TrimSpace(os.Getenv(URLEnv))
	return dsn, dsn != ""
}

// Apply overrides cfg from TEST_DATABASE_URL when that variable is set. It is
// a no-op otherwise, leaving the ambient POSTGRES_* configuration in place for
// local development. The test DSN's database name is preserved so callers can
// still create throwaway databases on that server.
func Apply(cfg *config.DatabaseConfig) error {
	dsn, ok := URL()
	if !ok {
		return nil
	}

	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("parse %s: %w", URLEnv, err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("%s: unsupported scheme %q (want postgres://)", URLEnv, u.Scheme)
	}

	if host := u.Hostname(); host != "" {
		cfg.Host = host
	}
	if port := u.Port(); port != "" {
		p, convErr := strconv.Atoi(port)
		if convErr != nil {
			return fmt.Errorf("%s: invalid port %q: %w", URLEnv, port, convErr)
		}
		cfg.Port = p
	}
	if u.User != nil {
		if user := u.User.Username(); user != "" {
			cfg.User = user
		}
		if pw, set := u.User.Password(); set {
			cfg.Password = pw
		}
	}
	if name := strings.TrimPrefix(u.Path, "/"); name != "" {
		cfg.Database = name
	}
	if ssl := u.Query().Get("sslmode"); ssl != "" {
		cfg.SSLMode = ssl
	}

	return nil
}
