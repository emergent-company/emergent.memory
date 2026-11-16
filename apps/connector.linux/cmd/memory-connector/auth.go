package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/account"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/project"
)

// authSchemaVersion is the schema version of the `auth status --json` document.
const authSchemaVersion = 1

// authLogin runs the device-flow login. It is a variable so tests can inject a
// network-free fake flow while still exercising session persistence.
var authLogin = func(ctx context.Context, m *account.Manager, serverURL string, opts account.LoginOptions, out io.Writer) (*account.Session, error) {
	return m.LoginWithOptions(ctx, serverURL, opts, out)
}

// newAuthManager builds the account manager for the auth subcommands
// (start/complete/cancel/access-token). It is a variable so tests can inject
// offline deps.
var newAuthManager = func(baseDir string) *account.Manager {
	return account.NewManager(baseDir)
}

// runAuth dispatches the `auth` subcommands.
func runAuth(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "memory-connector auth: missing subcommand (login, logout, start, complete, cancel, access-token, refresh, import, list, or status)")
		return 2
	}
	switch args[0] {
	case "login":
		return runAuthLogin(args[1:], stdout, stderr)
	case "logout":
		return runAuthLogout(args[1:], stdout, stderr)
	case "start":
		return runAuthStart(args[1:], stdout, stderr)
	case "complete":
		return runAuthComplete(args[1:], stdout, stderr)
	case "cancel":
		return runAuthCancel(args[1:], stdout, stderr)
	case "access-token":
		return runAuthAccessToken(args[1:], stdout, stderr)
	case "refresh":
		return runAuthRefresh(args[1:], stdout, stderr)
	case "import":
		return runAuthImport(args[1:], stdout, stderr)
	case "list":
		return runAuthList(args[1:], stdout, stderr)
	case "status":
		return runAuthStatus(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "memory-connector auth: unknown subcommand %q\n", args[0])
		return 2
	}
}

// authFlags are the flags shared by the auth subcommands.
type authFlags struct {
	configPath string
	server     string
	json       bool
}

// parseAuthFlags parses the shared --config/--server (and optionally --json)
// flags. The returned bool is false when the caller should return code.
func parseAuthFlags(name string, args []string, stderr io.Writer, withJSON bool) (authFlags, int, bool) {
	fs := flag.NewFlagSet("auth "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	server := fs.String("server", "", "Memory server URL (defaults to config server_url)")
	var jsonOut *bool
	if withJSON {
		jsonOut = fs.Bool("json", false, "emit machine-readable JSON")
	}
	if err := fs.Parse(args); err != nil {
		return authFlags{}, 2, false
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth %s: unexpected argument %q\n", name, fs.Arg(0))
		return authFlags{}, 2, false
	}
	f := authFlags{configPath: *configPath, server: *server}
	if jsonOut != nil {
		f.json = *jsonOut
	}
	return f, 0, true
}

// resolveAuthServer returns the explicit --server value, else the config file's
// server_url when present. A missing config is not an error (returns "").
func resolveAuthServer(explicit, configPath string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	b, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read config %s: %w", configPath, err)
	}
	var c struct {
		ServerURL string `yaml:"server_url"`
	}
	if err := yaml.Unmarshal(b, &c); err != nil {
		return "", fmt.Errorf("parse config %s: %w", configPath, err)
	}
	return c.ServerURL, nil
}

func runAuthLogin(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	server := fs.String("server", "", "Memory server URL (defaults to config server_url)")
	clientID := fs.String("client-id", account.ClientID, "OAuth client id (defaults to the connector client)")
	issuer := fs.String("issuer", "", "OIDC issuer URL (skips /api/auth/issuer discovery)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth login: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	serverURL, err := resolveAuthServer(*server, *configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth login: %v\n", err)
		return 1
	}
	if serverURL == "" {
		_, _ = fmt.Fprintln(stderr, "memory-connector auth login: no server URL — pass --server or set server_url in the config")
		return 2
	}

	m := account.NewManager(account.BaseDirForConfig(*configPath))
	sess, err := authLogin(context.Background(), m, serverURL, account.LoginOptions{ClientID: *clientID, Issuer: *issuer}, stdout)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth login: %v\n", err)
		return 1
	}
	if sess.UserEmail != "" {
		_, _ = fmt.Fprintf(stdout, "memory-connector auth: signed in as %s\n", sess.UserEmail)
	} else {
		_, _ = fmt.Fprintln(stdout, "memory-connector auth: signed in")
	}
	return 0
}

func runAuthLogout(args []string, stdout, stderr io.Writer) int {
	f, code, ok := parseAuthFlags("logout", args, stderr, false)
	if !ok {
		return code
	}
	m := account.NewManager(account.BaseDirForConfig(f.configPath))
	serverURL, err := resolveAuthServer(f.server, f.configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth logout: %v\n", err)
		return 1
	}
	if serverURL == "" {
		if active, activeOK := m.Active(); activeOK {
			serverURL = active
		}
	}
	if serverURL == "" {
		_, _ = fmt.Fprintln(stdout, "memory-connector auth: not signed in")
		return 0
	}
	if err := m.Logout(serverURL); err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth logout: %v\n", err)
		return 1
	}
	// Best-effort: signing out also drops that account's locally stored
	// project tokens so a later login re-mints instead of reusing stale ones.
	_ = project.NewManager(account.BaseDirForConfig(f.configPath)).ClearAccountTokens(serverURL)
	_, _ = fmt.Fprintf(stdout, "memory-connector auth: signed out of %s\n", serverURL)
	return 0
}

// authStatus is the machine-readable `auth status --json` document.
type authStatus struct {
	SchemaVersion int    `json:"schema_version"`
	Server        string `json:"server,omitempty"`
	SignedIn      bool   `json:"signed_in"`
	Email         string `json:"email,omitempty"`
	Issuer        string `json:"issuer,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	Expired       bool   `json:"expired,omitempty"`
	// CanRefresh reports whether a refresh token is stored, so a caller can
	// tell "the access token is stale but renewable" from "this session is
	// gone". NeedsReauth reports a session the provider has already rejected:
	// signing in again (silently or interactively) is the only fix.
	CanRefresh  bool `json:"can_refresh,omitempty"`
	NeedsReauth bool `json:"needs_reauth,omitempty"`
}

func runAuthStatus(args []string, stdout, stderr io.Writer) int {
	f, code, ok := parseAuthFlags("status", args, stderr, true)
	if !ok {
		return code
	}
	m := account.NewManager(account.BaseDirForConfig(f.configPath))
	serverURL, err := resolveAuthServer(f.server, f.configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth status: %v\n", err)
		return 1
	}
	if serverURL == "" {
		if active, activeOK := m.Active(); activeOK {
			serverURL = active
		}
	}

	state := authStatus{SchemaVersion: authSchemaVersion, Server: serverURL}
	if serverURL != "" {
		sess, err := m.SessionFor(serverURL)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector auth status: %v\n", err)
			return 1
		}
		// One shared builder: `auth complete` and `auth import` print the same
		// document, and a second inline copy here had already drifted once.
		state = authStatusFromSession(serverURL, sess)
	}

	if f.json {
		b, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector auth status: %v\n", err)
			return 1
		}
		_, _ = stdout.Write(append(b, '\n'))
		return 0
	}
	printAuthStatusText(stdout, state)
	return 0
}

func printAuthStatusText(w io.Writer, s authStatus) {
	_, _ = fmt.Fprintln(w, "memory-connector auth status")
	if s.Server != "" {
		_, _ = fmt.Fprintf(w, "server: %s\n", s.Server)
	}
	if !s.SignedIn {
		_, _ = fmt.Fprintln(w, "signed in: no")
		return
	}
	_, _ = fmt.Fprintln(w, "signed in: yes")
	if s.Email != "" {
		_, _ = fmt.Fprintf(w, "email: %s\n", s.Email)
	}
	if s.Issuer != "" {
		_, _ = fmt.Fprintf(w, "issuer: %s\n", s.Issuer)
	}
	if s.ExpiresAt != "" {
		_, _ = fmt.Fprintf(w, "expires at: %s\n", s.ExpiresAt)
	}
	if s.Expired {
		_, _ = fmt.Fprintln(w, "expired: yes")
	} else {
		_, _ = fmt.Fprintln(w, "expired: no")
	}
	if s.NeedsReauth {
		_, _ = fmt.Fprintln(w, "needs re-auth: yes")
	}
}

// authStartDoc is the machine-readable `auth start --json` document.
type authStartDoc struct {
	SchemaVersion int    `json:"schema_version"`
	LoginID       string `json:"login_id"`
	AuthorizeURL  string `json:"authorize_url"`
	State         string `json:"state"`
	ExpiresAt     string `json:"expires_at"`
}

// runAuthStart begins a two-step PKCE login for a coordinating app (for
// example the macOS app, which owns the browser + custom-scheme callback).
func runAuthStart(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("auth start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	server := fs.String("server", "", "Memory server URL (defaults to config server_url)")
	redirectURI := fs.String("redirect-uri", "", "OAuth redirect URI registered by the app")
	clientID := fs.String("client-id", account.ClientID, "OAuth client id (defaults to the connector client)")
	issuer := fs.String("issuer", "", "OIDC issuer URL (skips /api/auth/issuer discovery; --server may be omitted)")
	prompt := fs.String("prompt", "", "OIDC prompt parameter (none for a silent re-auth that shows no UI)")
	loginHint := fs.String("login-hint", "", "OIDC login_hint (the account email a silent re-auth is for)")
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth start: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if strings.TrimSpace(*redirectURI) == "" {
		_, _ = fmt.Fprintln(stderr, "memory-connector auth start: --redirect-uri is required")
		return 2
	}
	if !account.ValidPrompt(*prompt) {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth start: unsupported --prompt %q (want one of %s)\n",
			*prompt, strings.Join(account.PromptValues, ", "))
		return 2
	}
	serverURL, err := resolveAuthServer(*server, *configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth start: %v\n", err)
		return 1
	}
	if serverURL == "" && strings.TrimSpace(*issuer) == "" {
		_, _ = fmt.Fprintln(stderr, "memory-connector auth start: no server URL — pass --server, --issuer, or set server_url in the config")
		return 2
	}

	m := newAuthManager(account.BaseDirForConfig(*configPath))
	pending, err := m.StartPKCE(context.Background(), serverURL, *redirectURI, account.PKCEOptions{
		ClientID:  *clientID,
		Issuer:    *issuer,
		Prompt:    *prompt,
		LoginHint: *loginHint,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth start: %v\n", err)
		return 1
	}

	if *jsonOut {
		doc := authStartDoc{
			SchemaVersion: authSchemaVersion,
			LoginID:       pending.LoginID,
			AuthorizeURL:  pending.AuthorizeURL,
			State:         pending.State,
			ExpiresAt:     pending.ExpiresAt.UTC().Format(time.RFC3339),
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector auth start: %v\n", err)
			return 1
		}
		_, _ = stdout.Write(append(b, '\n'))
		return 0
	}

	_, _ = fmt.Fprintln(stdout, "memory-connector auth start")
	_, _ = fmt.Fprintf(stdout, "authorize url: %s\n", pending.AuthorizeURL)
	_, _ = fmt.Fprintf(stdout, "login id: %s\n", pending.LoginID)
	_, _ = fmt.Fprintf(stdout, "state: %s\n", pending.State)
	_, _ = fmt.Fprintf(stdout, "expires at: %s\n", pending.ExpiresAt.UTC().Format(time.RFC3339))
	return 0
}

// runAuthComplete finishes a PKCE login and reports the resulting account.
func runAuthComplete(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("auth complete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	server := fs.String("server", "", "Memory server URL (defaults to config server_url or the active account)")
	loginID := fs.String("login-id", "", "login id returned by 'auth start'")
	code := fs.String("code", "", "authorization code from the callback")
	state := fs.String("state", "", "state from the callback (must match 'auth start')")
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth complete: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if strings.TrimSpace(*loginID) == "" || strings.TrimSpace(*code) == "" || strings.TrimSpace(*state) == "" {
		_, _ = fmt.Fprintln(stderr, "memory-connector auth complete: --login-id, --code, and --state are required")
		return 2
	}

	m := newAuthManager(account.BaseDirForConfig(*configPath))
	serverURL, err := resolveAuthServer(*server, *configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth complete: %v\n", err)
		return 1
	}
	if serverURL == "" {
		if active, ok := m.Active(); ok {
			serverURL = active
		}
	}
	if serverURL == "" {
		_, _ = fmt.Fprintln(stderr, "memory-connector auth complete: no server URL — pass --server or set server_url in the config")
		return 2
	}

	sess, err := m.CompletePKCE(context.Background(), serverURL, *loginID, *code, *state)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth complete: %s\n", authCompleteError(err))
		return 1
	}

	stateDoc := authStatusFromSession(serverURL, sess)
	if *jsonOut {
		b, err := json.MarshalIndent(stateDoc, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector auth complete: %v\n", err)
			return 1
		}
		_, _ = stdout.Write(append(b, '\n'))
		return 0
	}
	printAuthStatusText(stdout, stateDoc)
	return 0
}

// authCompleteError maps PKCE errors to clear, actionable messages.
func authCompleteError(err error) string {
	switch {
	case errors.Is(err, account.ErrUnknownLogin):
		return "unknown or already-used login id"
	case errors.Is(err, account.ErrExpiredLogin):
		return "login expired; run 'memory-connector auth start' again"
	case errors.Is(err, account.ErrStateMismatch):
		return "state mismatch; the callback did not match this login"
	default:
		return err.Error()
	}
}

// runAuthCancel discards a pending PKCE login. It is best-effort.
func runAuthCancel(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("auth cancel", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	loginID := fs.String("login-id", "", "login id returned by 'auth start'")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth cancel: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if strings.TrimSpace(*loginID) == "" {
		_, _ = fmt.Fprintln(stderr, "memory-connector auth cancel: --login-id is required")
		return 2
	}

	m := newAuthManager(account.BaseDirForConfig(*configPath))
	if err := m.CancelPKCE(*loginID); err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth cancel: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "memory-connector auth: cancelled login %s\n", *loginID)
	return 0
}

// accessTokenRefreshWindow is how close to expiry a session may be before
// `auth access-token` / `auth refresh` renews it proactively. Five minutes
// covers a slow API burst without churning the token on every read.
const accessTokenRefreshWindow = 5 * time.Minute

// authAccessTokenDoc is the machine-readable `auth access-token --json`
// document. It never contains the refresh token.
type authAccessTokenDoc struct {
	SchemaVersion int    `json:"schema_version"`
	ServerURL     string `json:"server_url"`
	AccessToken   string `json:"access_token"`
	ExpiresAt     string `json:"expires_at"`
}

// runAuthAccessToken prints the stored account's access token, refreshing it
// first when it is expired or within accessTokenRefreshWindow of expiry. Text
// mode prints the raw token only; --json adds the server and expiry. It never
// reads or prints the refresh token. Exits 0 on success, 1 when not signed in
// or the session cannot be read/refreshed, and 2 on usage errors.
func runAuthAccessToken(args []string, stdout, stderr io.Writer) int {
	f, code, ok := parseAuthFlags("access-token", args, stderr, true)
	if !ok {
		return code
	}
	serverURL, err := resolveAuthServer(f.server, f.configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth access-token: %v\n", err)
		return 1
	}
	if serverURL == "" {
		_, _ = fmt.Fprintln(stderr, "memory-connector auth access-token: no server URL — pass --server or set server_url in the config")
		return 2
	}

	m := newAuthManager(account.BaseDirForConfig(f.configPath))
	token, err := m.AccessToken(context.Background(), serverURL, accessTokenRefreshWindow)
	if err != nil {
		if errors.Is(err, account.ErrNoSession) {
			_, _ = fmt.Fprintf(stderr, "memory-connector auth access-token: %s\n", projectsNotSignedIn)
			return 1
		}
		_, _ = fmt.Fprintf(stderr, "memory-connector auth access-token: %v\n", err)
		return 1
	}

	if f.json {
		expiresAt := ""
		if sess, serr := m.SessionFor(serverURL); serr == nil && sess != nil && !sess.ExpiresAt.IsZero() {
			expiresAt = sess.ExpiresAt.UTC().Format(time.RFC3339)
		}
		doc := authAccessTokenDoc{
			SchemaVersion: authSchemaVersion,
			ServerURL:     serverURL,
			AccessToken:   token,
			ExpiresAt:     expiresAt,
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector auth access-token: %v\n", err)
			return 1
		}
		_, _ = stdout.Write(append(b, '\n'))
		return 0
	}

	_, _ = fmt.Fprintln(stdout, token)
	return 0
}

// authRefreshDoc is the machine-readable `auth refresh --json` document.
type authRefreshDoc struct {
	SchemaVersion int    `json:"schema_version"`
	ServerURL     string `json:"server_url"`
	Refreshed     bool   `json:"refreshed"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	CanRefresh    bool   `json:"can_refresh"`
	NeedsReauth   bool   `json:"needs_reauth,omitempty"`
}

// runAuthRefresh renews the stored session's access token when it is within
// --min-ttl of expiry, and reports the resulting state. It is the explicit
// "make this session fresh before a burst of work" call a client makes at
// launch and after wake; project commands refresh on demand themselves.
func runAuthRefresh(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("auth refresh", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	server := fs.String("server", "", "server URL (defaults to config server_url or the active account)")
	minTTL := fs.Duration("min-ttl", accessTokenRefreshWindow, "refresh when the access token expires within this duration")
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	serverURL, err := resolveAuthServer(*server, *configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth refresh: %v\n", err)
		return 1
	}
	if serverURL == "" {
		_, _ = fmt.Fprintln(stderr, "memory-connector auth refresh: no server URL — pass --server or set server_url in the config")
		return 2
	}

	m := newAuthManager(account.BaseDirForConfig(*configPath))
	before, err := m.SessionFor(serverURL)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth refresh: %v\n", err)
		return 1
	}
	if before == nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth refresh: %s\n", projectsNotSignedIn)
		return 1
	}
	if _, err := m.AccessToken(context.Background(), serverURL, *minTTL); err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth refresh: %v\n", err)
		return 1
	}
	after, err := m.SessionFor(serverURL)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth refresh: %v\n", err)
		return 1
	}
	if after == nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth refresh: %s\n", projectsNotSignedIn)
		return 1
	}

	doc := authRefreshDoc{
		SchemaVersion: authSchemaVersion,
		ServerURL:     serverURL,
		Refreshed:     after.AccessToken != before.AccessToken,
		CanRefresh:    after.RefreshToken != "",
		NeedsReauth:   after.NeedsReauth,
	}
	if !after.ExpiresAt.IsZero() {
		doc.ExpiresAt = after.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if *jsonOut {
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector auth refresh: %v\n", err)
			return 1
		}
		_, _ = stdout.Write(append(b, '\n'))
		return 0
	}
	state := "fresh"
	if doc.Refreshed {
		state = "refreshed"
	}
	_, _ = fmt.Fprintf(stdout, "memory-connector auth: session %s for %s\n", state, serverURL)
	if doc.ExpiresAt != "" {
		_, _ = fmt.Fprintf(stdout, "expires at: %s\n", doc.ExpiresAt)
	}
	// A session whose refresh token the provider already rejected still has a
	// stored access token, so "fresh" alone would be misleading.
	if doc.NeedsReauth {
		_, _ = fmt.Fprintln(stdout, "needs re-auth: yes (the stored refresh token was rejected)")
	}
	return 0
}

// authStatusFromSession builds the shared account status document.
func authStatusFromSession(serverURL string, sess *account.Session) authStatus {
	state := authStatus{SchemaVersion: authSchemaVersion, Server: serverURL}
	if sess == nil {
		return state
	}
	state.SignedIn = true
	state.Email = sess.UserEmail
	state.Issuer = sess.IssuerURL
	// The refresh fields are part of the document everywhere it is produced:
	// `auth complete`/`auth import` answering without them made the same
	// session look different depending on which command printed it.
	state.CanRefresh = sess.RefreshToken != ""
	state.NeedsReauth = sess.NeedsReauth
	if !sess.ExpiresAt.IsZero() {
		state.ExpiresAt = sess.ExpiresAt.UTC().Format(time.RFC3339)
	}
	state.Expired = sess.Expired()
	return state
}
