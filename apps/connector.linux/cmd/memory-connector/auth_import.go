package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/emergent-company/memory.web-ui/connector/internal/account"
	"github.com/emergent-company/memory.web-ui/connector/internal/config"
)

// authStdin is the source for `auth import` payloads. It is a variable so tests
// can feed JSON without touching the real process stdin. Import reads a JSON
// body from stdin (never argv) because command-line arguments are visible to
// other local processes via `ps`.
var authStdin io.Reader = os.Stdin

// authImportPayload is the JSON object `auth import` accepts on stdin:
//
//	{"access_token":"...","refresh_token":"...","expires_at":"<RFC3339>",
//	 "issuer":"...","client_id":"...","email":"..."}
//
// The snake_case keys are canonical. A few camelCase aliases are accepted so
// the macOS app can pipe its own session shape; `client_id` is accepted but not
// persisted by import (device-flow login persists the effective client id).
type authImportPayload struct {
	AccessToken     string `json:"access_token"`
	AccessTokenAlt  string `json:"accessToken"`
	RefreshToken    string `json:"refresh_token"`
	RefreshTokenAlt string `json:"refreshToken"`
	ExpiresAt       string `json:"expires_at"`
	ExpiresAtAlt    string `json:"expiresAt"`
	Issuer          string `json:"issuer"`
	IssuerURL       string `json:"issuer_url"`
	Email           string `json:"email"`
	UserEmail       string `json:"user_email"`
}

// runAuthImport stores a session piped on stdin and prints the resulting
// `auth status` document. It is the migration path for the macOS app, the only
// process that can read the legacy Keychain: the app pipes the session JSON to
// the CLI over stdin so the tokens never appear in argv.
//
// Identity is stored as-is from the payload; no /api/auth/me round-trip is
// made, so import is fast, deterministic, and works offline. The payload's
// optional `email` is trusted when present.
//
// Exit codes: 0 success, 1 runtime error, 2 usage (missing --server or
// access_token, malformed JSON, or an unparseable expiry).
func runAuthImport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("auth import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	server := fs.String("server", "", "Memory server URL (required)")
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth import: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if strings.TrimSpace(*server) == "" {
		_, _ = fmt.Fprintln(stderr, "memory-connector auth import: --server is required")
		return 2
	}

	raw, err := io.ReadAll(authStdin)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth import: read stdin: %v\n", err)
		return 1
	}
	payload := authImportPayload{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth import: invalid JSON on stdin: %v\n", err)
		return 2
	}

	accessToken := firstNonEmpty(payload.AccessToken, payload.AccessTokenAlt)
	if strings.TrimSpace(accessToken) == "" {
		_, _ = fmt.Fprintln(stderr, "memory-connector auth import: stdin JSON must include a non-empty access_token")
		return 2
	}

	var expiresAt time.Time
	if raw := firstNonEmpty(payload.ExpiresAt, payload.ExpiresAtAlt); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector auth import: invalid expires_at %q (want RFC3339): %v\n", raw, err)
			return 2
		}
		expiresAt = parsed
	}

	sess := &account.Session{
		ServerURL:    *server,
		IssuerURL:    firstNonEmpty(payload.Issuer, payload.IssuerURL),
		AccessToken:  accessToken,
		RefreshToken: firstNonEmpty(payload.RefreshToken, payload.RefreshTokenAlt),
		ExpiresAt:    expiresAt,
		UserEmail:    firstNonEmpty(payload.Email, payload.UserEmail),
	}

	m := newAuthManager(account.BaseDirForConfig(*configPath))
	if err := m.Save(*server, sess); err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth import: %v\n", err)
		return 1
	}
	if err := m.SetActive(*server); err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth import: %v\n", err)
		return 1
	}

	// Reuse the `auth status` rendering (never includes the tokens).
	state := authStatusFromSession(*server, sess)
	if *jsonOut {
		b, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector auth import: %v\n", err)
			return 1
		}
		_, _ = stdout.Write(append(b, '\n'))
		return 0
	}
	printAuthStatusText(stdout, state)
	return 0
}

// firstNonEmpty returns the first non-empty value (used to accept snake_case
// and camelCase aliases from the importer).
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
