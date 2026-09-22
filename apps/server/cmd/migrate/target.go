// Package main provides a CLI for database migrations.
package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Target describes the resolved PostgreSQL target for the migrate command.
//
// Resolution understands both the tool's historical DB_HOST/DB_PORT names and
// the POSTGRES_HOST/POSTGRES_PORT names used by the rest of the stack (compose
// services, .env, the container entrypoint), plus MEMORY_PG_* as a lowest
// precedence alias. The resolved target is always logged before connecting so
// the operator can see exactly where migrations will run.
type Target struct {
	Host     string
	Port     string
	User     string
	Database string
	SSLMode  string
	// Password is kept only to build the DSN. It is never included in LogLine.
	Password string

	// hostSource names the env pair (or the built-in default) that supplied the
	// host, e.g. "DB_HOST/DB_PORT", "POSTGRES_HOST/POSTGRES_PORT",
	// "MEMORY_PG_HOST/MEMORY_PG_PORT", "DATABASE_URL" or "built-in default".
	hostSource string
	// explicitHost is true when the host came from an environment variable
	// rather than the built-in localhost default.
	explicitHost bool
	// conflictEnv records a host env var that is set, non-loopback, and points
	// somewhere other than the resolved host (e.g. "POSTGRES_HOST=db"). A
	// non-empty value means the resolution is ambiguous.
	conflictEnv string
}

// IsLoopback reports whether Host is a loopback / local-only address.
func (t Target) IsLoopback() bool {
	h := strings.ToLower(strings.TrimSpace(t.Host))
	h = strings.Trim(h, "[]")
	switch h {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	default:
		return false
	}
}

// LogLine renders the resolved target for operator-visible startup logging.
// It must never include the password.
func (t Target) LogLine() string {
	return fmt.Sprintf("target: host=%s port=%s user=%s database=%s sslmode=%s (host source: %s)",
		t.Host, t.Port, t.User, t.Database, t.SSLMode, t.hostSource)
}

// DSN builds a libpq connection string from the resolved components. It is
// assembled with net/url so that IPv6 hosts are bracketed ([::1]:5432) and
// URI-reserved characters in the user or password are escaped rather than
// producing a malformed connection string.
func (t Target) DSN() string {
	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(t.User, t.Password),
		Host:     net.JoinHostPort(t.Host, t.Port),
		Path:     "/" + t.Database,
		RawQuery: url.Values{"sslmode": {t.SSLMode}}.Encode(),
	}
	return u.String()
}

// targetDecision is the outcome of the fail-closed check on a resolved target.
type targetDecision struct {
	Refuse bool
	Reason string // populated when Refuse is true
	Warn   string // populated when the run is allowed but suspicious
}

// resolveTarget resolves the migration target from the environment.
//
// Precedence:
//  1. DATABASE_URL, if set, is a complete override (host/port/db/user/ssl are
//     parsed from it).
//  2. Otherwise host is the first non-empty of DB_HOST, POSTGRES_HOST,
//     MEMORY_PG_HOST; port is the first non-empty of DB_PORT, POSTGRES_PORT,
//     MEMORY_PG_PORT; user POSTGRES_USER, MEMORY_PG_USER; database
//     POSTGRES_DATABASE, POSTGRES_DB, MEMORY_PG_DB; sslmode DB_SSL_MODE,
//     POSTGRES_SSL_MODE.
//  3. Missing values fall back to built-in defaults, including the historical
//     localhost:5432 footgun. The caller must fail closed on that implicit
//     loopback target (see decideTarget).
func resolveTarget(getenv func(string) string) (Target, error) {
	if raw := strings.TrimSpace(getenv("DATABASE_URL")); raw != "" {
		t, err := parseDatabaseURL(raw)
		if err != nil {
			return Target{}, err
		}
		t.conflictEnv = detectConflict(getenv, t.Host)
		return t, nil
	}

	host := firstNonEmpty(getenv, "DB_HOST", "POSTGRES_HOST", "MEMORY_PG_HOST")
	hostSource := "built-in default"
	switch {
	case host == "":
		host = "localhost"
	case getenv("DB_HOST") != "":
		hostSource = "DB_HOST/DB_PORT"
	case getenv("POSTGRES_HOST") != "":
		hostSource = "POSTGRES_HOST/POSTGRES_PORT"
	default:
		hostSource = "MEMORY_PG_HOST/MEMORY_PG_PORT"
	}

	port := firstNonEmpty(getenv, "DB_PORT", "POSTGRES_PORT", "MEMORY_PG_PORT")
	if port == "" {
		port = "5432"
	}
	user := firstNonEmpty(getenv, "POSTGRES_USER", "MEMORY_PG_USER")
	if user == "" {
		user = "emergent"
	}
	database := firstNonEmpty(getenv, "POSTGRES_DATABASE", "POSTGRES_DB", "MEMORY_PG_DB")
	if database == "" {
		database = "emergent"
	}
	sslMode := firstNonEmpty(getenv, "DB_SSL_MODE", "POSTGRES_SSL_MODE")
	if sslMode == "" {
		sslMode = "disable"
	}

	pass := getenv("POSTGRES_PASSWORD")

	// Build the fully-resolved (except credentials) target even when the
	// password is missing, so the caller can still print the target line and
	// apply the fail-closed rule before reporting the credential error.
	t := Target{
		Host:         host,
		Port:         port,
		User:         user,
		Database:     database,
		SSLMode:      sslMode,
		Password:     pass,
		hostSource:   hostSource,
		explicitHost: hostSource != "built-in default",
		conflictEnv:  detectConflict(getenv, host),
	}
	if pass == "" {
		return t, fmt.Errorf("POSTGRES_PASSWORD or DATABASE_URL must be set")
	}
	return t, nil
}

// parseDatabaseURL parses a full connection string into a Target.
func parseDatabaseURL(raw string) (Target, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Target{}, fmt.Errorf("invalid DATABASE_URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return Target{}, fmt.Errorf("invalid DATABASE_URL: missing scheme or host")
	}

	port := u.Port()
	if port == "" {
		port = "5432"
	}
	// u.User is nil when the URL carries no userinfo (e.g.
	// postgres://host:5432/db); treat that as an empty user/password instead of
	// dereferencing it.
	var user, pass string
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}
	database := strings.TrimPrefix(u.Path, "/")
	if database == "" {
		database = "emergent"
	}
	sslMode := u.Query().Get("sslmode")
	if sslMode == "" {
		sslMode = "disable"
	}

	return Target{
		Host:         u.Hostname(),
		Port:         port,
		User:         user,
		Database:     database,
		SSLMode:      sslMode,
		Password:     pass,
		hostSource:   "DATABASE_URL",
		explicitHost: true,
	}, nil
}

// detectConflict returns "NAME=value" for the first host env var that is set,
// non-loopback, and does not match resolvedHost. Conflicts are only meaningful
// when the resolved host is loopback: a non-loopback target is unambiguous and
// is the documented winner of the precedence order. Loopback values never count
// as conflicts with each other because they all name localhost.
func detectConflict(getenv func(string) string, resolvedHost string) string {
	if !(Target{Host: resolvedHost}).IsLoopback() {
		return ""
	}
	for _, name := range []string{"DB_HOST", "POSTGRES_HOST", "MEMORY_PG_HOST"} {
		value := strings.TrimSpace(getenv(name))
		if value == "" {
			continue
		}
		if (Target{Host: value}).IsLoopback() {
			continue
		}
		if !strings.EqualFold(value, resolvedHost) {
			return name + "=" + value
		}
	}
	return ""
}

// decideTarget implements the fail-closed policy.
//
// Mutating commands (up, up-to, down, mark-applied) refuse to run against a
// loopback target unless the target was requested explicitly and unambiguously,
// or the operator passed -allow-localhost. Read-only commands are always
// allowed so the target can still be inspected.
func decideTarget(t Target, mutating bool, allowLocalhost bool) targetDecision {
	if !mutating || !t.IsLoopback() {
		return targetDecision{}
	}
	if allowLocalhost {
		return targetDecision{Warn: fmt.Sprintf(
			"targeting loopback address %s:%s (-allow-localhost)", t.Host, t.Port)}
	}
	if t.conflictEnv != "" {
		return targetDecision{Refuse: true, Reason: fmt.Sprintf(
			"refusing to run against loopback target %s:%s while %s points elsewhere; set the target explicitly or pass -allow-localhost to override",
			t.Host, t.Port, t.conflictEnv)}
	}
	if !t.explicitHost {
		return targetDecision{Refuse: true, Reason: fmt.Sprintf(
			"refusing to run: no database host was configured, so the target would default to loopback %s:%s; set DB_HOST/POSTGRES_HOST/DATABASE_URL or pass -allow-localhost to override",
			t.Host, t.Port)}
	}
	return targetDecision{Warn: fmt.Sprintf(
		"target is loopback (%s:%s) and was set explicitly; verify this is intended", t.Host, t.Port)}
}

// isMutatingCommand reports whether a command writes to the target database.
func isMutatingCommand(command string) bool {
	switch command {
	case "up", "up-to", "down", "mark-applied":
		return true
	default:
		return false
	}
}

// firstNonEmpty returns the first non-empty value among keys, in order.
func firstNonEmpty(getenv func(string) string, keys ...string) string {
	for _, key := range keys {
		if value := getenv(key); value != "" {
			return value
		}
	}
	return ""
}
