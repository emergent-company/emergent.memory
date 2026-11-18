package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/account"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
)

// authStdin is the source for `auth import` payloads. It is a variable so tests
// can feed JSON without touching the real process stdin. Import reads a JSON
// body from stdin (never argv) because command-line arguments are visible to
// other local processes via `ps`.
var authStdin io.Reader = os.Stdin

// authImportPayload is the JSON object `auth import` accepts on stdin:
//
//	{"access_token":"...","refresh_token":"...","expires_at":"<RFC3339>",
//	 "issuer":"...","client_id":"...","email":"...","needs_reauth":<bool>}
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
	// NeedsReauth lets an importer state that the session it is importing has
	// already been rejected by the provider. It is optional: `true` marks the
	// imported session, while `false` (or omitting it) never clears a mark the
	// CLI recorded — only replacing the rejected refresh token does. See
	// runAuthImport for the rule.
	NeedsReauth    *bool `json:"needs_reauth"`
	NeedsReauthAlt *bool `json:"needsReauth"`
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
// Import is a read-modify-write of the stored session, so it runs through
// Manager.UpdateSession, which holds the refresh lock across the read and the
// Save. Without that lock a concurrent `auth refresh` can rotate the single-use
// refresh token between the two, and the import then writes the pre-rotation
// token back — a session that is stale by construction (its next refresh gets
// invalid_grant and is marked). The lock makes the import either happen before
// the rotation or observe its result.
//
// Two fields are merged from the stored session rather than clobbered: the
// refresh token when the payload omits it, and the needs_reauth mark, which is
// a property of that stored refresh token and not of the import event — see the
// resolution below.
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

	importedRefreshToken := firstNonEmpty(payload.RefreshToken, payload.RefreshTokenAlt)

	m := newAuthManager(account.BaseDirForConfig(*configPath))
	sess, err := m.UpdateSession(*server, func(stored *account.Session) error {
		// Tokens carry no meaningful whitespace, so trim on the way in: a
		// payload that repeats the stored token with a trailing newline (as a
		// shell would pipe it) then neither replaces the token nor persists a
		// value the token endpoint would reject.
		refreshToken := strings.TrimSpace(importedRefreshToken)
		// Merge instead of clobber: when the payload omits the refresh token,
		// carry over the stored session's so an import that carries only the
		// access token does not destroy the IdP-minted refresh token.
		if refreshToken == "" {
			refreshToken = strings.TrimSpace(stored.RefreshToken)
		}

		// needs_reauth marks the stored refresh token the provider rejected, so
		// it belongs to that token rather than to the import event, and an
		// import must not clear it by omission: importing a session that still
		// carries the rejected token with the mark gone would leave a session
		// that looks renewable, and the client would only rediscover the
		// rejection on its next refresh. The mark therefore follows the token —
		// it survives while the import keeps the stored refresh token (the
		// payload omitted it, or repeated it) and drops when the import replaces
		// that token, because the rejection described the old one. Replacing the
		// refresh token is the deliberate reset.
		//
		// The comparison trims both sides: a payload that differs only in
		// whitespace repeats the token, so it must not count as a replacement
		// and clear the mark.
		needsReauth := stored.NeedsReauth &&
			strings.TrimSpace(refreshToken) == strings.TrimSpace(stored.RefreshToken)
		// An importer that knows the session is already rejected can say so,
		// which also marks a token the CLI has not seen rejected itself. `false`
		// is accepted (a client may echo a whole session document) but never
		// clears a mark recorded for a token that is still stored — that is the
		// resurrection this rule exists to prevent.
		if (payload.NeedsReauth != nil && *payload.NeedsReauth) ||
			(payload.NeedsReauthAlt != nil && *payload.NeedsReauthAlt) {
			needsReauth = true
		}

		// The payload document replaces the stored one, apart from the merged
		// token and mark above (import never persisted client_id or user_id).
		*stored = account.Session{
			IssuerURL:    firstNonEmpty(payload.Issuer, payload.IssuerURL),
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			ExpiresAt:    expiresAt,
			UserEmail:    firstNonEmpty(payload.Email, payload.UserEmail),
			NeedsReauth:  needsReauth,
		}
		return nil
	})
	if err != nil {
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
