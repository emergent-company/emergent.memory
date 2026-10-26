package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	qrcode "github.com/skip2/go-qrcode"
)

// errSetupNoProjectScope is returned when a setup QR cannot name the project it
// onboards. In session mode the devices page is always project-scoped, so this
// means the request had no resolved project — fail closed rather than mint a
// QR that cannot say which project it onboards.
var errSetupNoProjectScope = errors.New("setup QR requires a resolved project scope")

// device is one registered device credential, as shown in the settings UI. It
// mirrors a project-scoped core.api_tokens row carrying the reserved device:api
// marker. The credential itself is never exposed — only its id (for revoke),
// name and masked prefix.
type device struct {
	ID         string
	Name       string
	Prefix     string
	CreatedAt  string // RFC3339
	IsRevoked  bool
	LastUsedAt *time.Time
}

// setupTokenTTL bounds a one-time setup token's lifetime. A pending token older
// than this is treated as expired and re-minted on the next devices-page load.
const setupTokenTTL = 10 * time.Minute

// setupEntry is one pending one-time setup token held in the gateway-local
// store: the token, the project it onboards to, and the pre-minted device
// credential claimed at /api/setup.
type setupEntry struct {
	Token         string
	OrgID         string
	ProjectID     string
	ProjectName   string
	DeviceToken   string // emt_* plaintext, minted server-side at QR time
	DeviceTokenID string // id of DeviceToken, for revoke on rotation
	CreatedAt     time.Time
	Used          bool
}

// setupTokenStore is the in-process store of pending one-time setup tokens,
// keyed by token. It holds no process-global credential: minting runs on the
// session-authenticated devices page (which also mints the device credential
// server-side), and consuming is a pure local lookup — so POST /api/setup
// succeeds with no session. The store is deliberately not persisted: a gateway
// restart drops pending (unclaimed) tokens, and the QR is re-minted on the next
// devices-page load (bounded by setupTokenTTL).
type setupTokenStore struct {
	mu sync.Mutex
	m  map[string]setupEntry
}

func newSetupTokenStore() *setupTokenStore {
	return &setupTokenStore{m: make(map[string]setupEntry)}
}

// set stores an entry keyed by its token.
func (st *setupTokenStore) set(e setupEntry) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.m[e.Token] = e
}

// get returns the entry for token.
func (st *setupTokenStore) get(token string) (setupEntry, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	e, ok := st.m[token]
	return e, ok
}

// freshFor returns the unexpired, unused entry for the given project identity,
// or ok=false. Reusing it keeps the QR stable across page reloads within the TTL.
func (st *setupTokenStore) freshFor(orgID, projectID string) (setupEntry, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, e := range st.m {
		if e.OrgID == orgID && e.ProjectID == projectID && !e.Used && setupTokenFresh(e.CreatedAt) {
			return e, true
		}
	}
	return setupEntry{}, false
}

// unclaimedIDFor returns the device-token id of an unclaimed entry for the
// given project identity (the pending-onboarding credential hidden from the
// devices list), or "".
func (st *setupTokenStore) unclaimedIDFor(orgID, projectID string) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, e := range st.m {
		if e.OrgID == orgID && e.ProjectID == projectID && !e.Used && e.DeviceTokenID != "" {
			return e.DeviceTokenID
		}
	}
	return ""
}

// consume atomically marks the entry for token used and returns it, or ok=false
// when the token is unknown, already used, expired, or has no device credential.
func (st *setupTokenStore) consume(token string) (setupEntry, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	e, ok := st.m[token]
	if !ok || e.Used || !setupTokenFresh(e.CreatedAt) || e.DeviceToken == "" {
		return setupEntry{}, false
	}
	e.Used = true
	st.m[token] = e
	return e, true
}

// setupProjectIdentity resolves the onboarding project identity for a request:
// org/project from the request scope (scoped-path URL), else the session
// context, else the static project id (dev/API-key mode). The project name is
// best-effort from GetCurrentProject ("" on error).
func (s *Server) setupProjectIdentity(ctx context.Context) (orgID, projectID, projectName string) {
	if sc, ok := scopeFromContext(ctx); ok {
		projectID = sc.ResolvedProjectID
		orgID = sc.ResolvedOrgID
	}
	if sess, ok := sessionContextFrom(ctx); ok {
		if projectID == "" {
			projectID = sess.ProjectID
		}
		if orgID == "" {
			orgID = sess.OrgID
		}
	}
	if projectID == "" {
		projectID = s.cfg.MemoryProjectID
	}
	if p, err := s.memory.GetCurrentProject(ctx); err == nil && p != nil {
		projectName = p.Name
	}
	return orgID, projectID, projectName
}

// mintSetupToken returns the current one-time setup token for the onboarding
// project identity. While a store entry for that identity is unexpired and
// unused it is REUSED, which keeps the QR stable across page reloads; otherwise
// a fresh 32-hex token is minted alongside a fresh server-side device
// credential, revoking any prior unclaimed orphan so an abandoned QR never
// leaks a live credential.
func (s *Server) mintSetupToken(ctx context.Context, orgID, projectID, projectName string) (string, error) {
	st := s.setupStore()
	if e, ok := st.freshFor(orgID, projectID); ok {
		return e.Token, nil
	}

	// Rotate: revoke any prior UNCLAIMED device credential so an abandoned QR
	// never leaks a live credential. A claimed credential (used=true) is a real
	// device and must survive a page reload — only an unclaimed orphan is
	// revoked here.
	if priorID := st.unclaimedIDFor(orgID, projectID); priorID != "" {
		_ = s.memory.RevokeAPIToken(ctx, priorID)
	}

	// Mint the device credential server-side while the session context is
	// present (the devices page is session-authenticated). In dev mode there is
	// no session token, so the mint fails and the setup token is issued without
	// a device credential — /api/setup then reports the credential is
	// unavailable rather than proxying an empty bearer.
	var deviceToken, deviceTokenID string
	if dc, err := s.memory.CreateDeviceToken(ctx, "device"); err == nil && dc != nil {
		deviceToken = dc.Token
		deviceTokenID = dc.ID
	} else if err != nil {
		captureError(err)
	}

	token := randomHex(32)
	st.set(setupEntry{
		Token:         token,
		OrgID:         orgID,
		ProjectID:     projectID,
		ProjectName:   projectName,
		DeviceToken:   deviceToken,
		DeviceTokenID: deviceTokenID,
		CreatedAt:     time.Now(),
	})
	return token, nil
}

// consumeSetupToken validates and single-uses a setup token, returning the
// device credential and the onboarding project identity it carries. It is a
// local-only lookup (no memory call): unknown, used, expired, or
// missing-device-credential tokens all return ok=false.
func (s *Server) consumeSetupToken(token string) (deviceToken, orgID, projectID, projectName string, ok bool) {
	e, ok := s.setupStore().consume(token)
	if !ok {
		return "", "", "", "", false
	}
	return e.DeviceToken, e.OrgID, e.ProjectID, e.ProjectName, true
}

// setupStore returns s.setupTokens, lazily initializing it for bare test
// servers (main.go constructs it eagerly).
func (s *Server) setupStore() *setupTokenStore {
	if s.setupTokens == nil {
		s.setupTokens = newSetupTokenStore()
	}
	return s.setupTokens
}

// setupTokenFresh reports whether a setup-token createdAt is still within the
// setup-token TTL. A zero/absent timestamp is treated as expired.
func setupTokenFresh(createdAt time.Time) bool {
	return time.Since(createdAt) <= setupTokenTTL
}

// deviceManifest is the optional self-description a client sends when it
// exchanges a setup token for a per-device key: platform, form factor,
// user-assigned name, hardware model, OS, and app version. All fields are
// optional strings (camelCase JSON keys); the gateway persists only the
// non-empty ones alongside the issued key.
type deviceManifest struct {
	Platform     string `json:"platform,omitempty"`
	FormFactor   string `json:"formFactor,omitempty"`
	Name         string `json:"name,omitempty"`
	ModelID      string `json:"modelId,omitempty"`
	ModelDisplay string `json:"modelDisplay,omitempty"`
	OSName       string `json:"osName,omitempty"`
	OSVersion    string `json:"osVersion,omitempty"`
	AppVersion   string `json:"appVersion,omitempty"`
	AppBuild     string `json:"appBuild,omitempty"`
}

// hasDeviceScope reports whether a token's scope set carries the reserved
// device:api marker, i.e. the token is a device credential.
func hasDeviceScope(scopes []string) bool {
	for _, s := range scopes {
		if s == "device:api" {
			return true
		}
	}
	return false
}

// listDeviceTokens returns the project's device credentials, most recently
// issued first. The list reuses the project-token listing and filters to the
// device:api marker; only non-revoked credentials are shown (a revoked device
// is no longer "registered"). The unclaimed credential referenced by the
// current setup token is excluded — it is pending onboarding, not yet a
// registered device.
func (s *Server) listDeviceTokens(ctx context.Context) ([]device, error) {
	pendingID := ""
	if s.setupTokens != nil {
		orgID, projectID, _ := s.setupProjectIdentity(ctx)
		pendingID = s.setupTokens.unclaimedIDFor(orgID, projectID)
	}
	tokens, err := s.memory.ListAPITokens(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]device, 0, len(tokens))
	for _, t := range tokens {
		if t.IsRevoked || !hasDeviceScope(t.Scopes) || (pendingID != "" && t.ID == pendingID) {
			continue
		}
		out = append(out, device{
			ID:         t.ID,
			Name:       t.Name,
			Prefix:     t.TokenPrefix,
			CreatedAt:  t.CreatedAt,
			IsRevoked:  t.IsRevoked,
			LastUsedAt: t.LastUsedAt,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out, nil
}

// revokeDeviceToken revokes a device credential by token id via the shared
// project-token revoke endpoint.
func (s *Server) revokeDeviceToken(ctx context.Context, tokenID string) error {
	return s.memory.RevokeAPIToken(ctx, tokenID)
}

// publicBaseURL returns the externally-reachable base URL (scheme://host[:port],
// no trailing slash) used for the client config + setup URLs. Uses PUBLIC_BASE_URL
// when set, otherwise derives from the request Host (http scheme).
func (s *Server) publicBaseURL(c echo.Context) string {
	if b := strings.TrimSpace(s.cfg.PublicBaseURL); b != "" {
		return strings.TrimRight(b, "/")
	}
	return "http://" + c.Request().Host
}

// serverPublicBaseURL returns the externally-reachable base URL
// (scheme://host[:port], no trailing slash) of the memory server — the origin
// MCP routes are actually served from. It is the configured MEMORY_PUBLIC_URL
// (defaulting to MEMORY_URL); it MUST NOT fall back to the request Host, so the
// origin advertised to clients can never be attacker-influenceable. Empty means
// no server origin is configured: callers return the backend-reported URL
// unchanged rather than fabricating one.
func (s *Server) serverPublicBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(s.cfg.MemoryPublicURL), "/")
}

// ttsStrategy maps the TTS_PROVIDER config to the client-facing strategy the
// iOS app should use: "client" (device does local TTS from text) for
// none/client/empty, "server" (audio comes from the worker) otherwise.
func (s *Server) ttsStrategy() string {
	switch strings.ToLower(strings.TrimSpace(s.cfg.TTSProvider)) {
	case "", "none", "client":
		return "client"
	default:
		return "server"
	}
}

// setupClient implements POST /api/setup (no auth): exchange a one-time setup
// token for a scoped per-device credential (an emt_* bearer carrying the
// device:api marker). The returned URLs point at this gateway (single origin),
// pinned to PUBLIC_BASE_URL when configured.
func (s *Server) setupClient(c echo.Context) error {
	var in struct {
		Token  string          `json:"token"`
		Device *deviceManifest `json:"device"` // accepted for back-compat; the credential is minted at QR time
	}
	if err := c.Bind(&in); err != nil || in.Token == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid or missing token"})
	}
	deviceToken, orgID, projectID, projectName, ok := s.consumeSetupToken(in.Token)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unknown, expired, or already-used token"})
	}
	serverURL := s.cfg.LiveKitPublicURL
	if serverURL == "" {
		serverURL = s.cfg.LiveKitURL
	}
	base := s.publicBaseURL(c)
	return c.JSON(http.StatusOK, map[string]string{
		"serverURL":     serverURL,
		"tokenEndpoint": base + "/api/token",
		"apiBaseURL":    base + "/",
		"apiKey":        deviceToken,
		"ttsStrategy":   s.ttsStrategy(),
		"orgID":         orgID,
		"projectID":     projectID,
		"projectName":   projectName,
	})
}

// setupQRPayload returns the current one-time setup token (minting it if
// needed) and the JSON string a setup QR encodes: the setup URL, the token, and
// the onboarding project identity (orgID/projectID/projectName).
func (s *Server) setupQRPayload(c echo.Context) (string, error) {
	ctx := c.Request().Context()
	orgID, projectID, projectName := s.setupProjectIdentity(ctx)
	// A setup QR onboards exactly one project. In session mode the devices page
	// is always project-scoped, so a missing resolved project means we cannot
	// mint a project-bound QR — fail closed rather than emit a project-less one.
	if s.cfg.AuthMode == "session" && (orgID == "" || projectID == "") {
		return "", errSetupNoProjectScope
	}
	token, err := s.mintSetupToken(ctx, orgID, projectID, projectName)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(map[string]string{
		"setupURL":    s.publicBaseURL(c) + "/api/setup",
		"token":       token,
		"orgID":       orgID,
		"projectID":   projectID,
		"projectName": projectName,
	})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// setupQRImage returns a base64 data-URI PNG of the setup-token QR, or "" on
// encode failure. A fresh one-time token is minted on every call.
func (s *Server) setupQRImage(c echo.Context) string {
	payload, err := s.setupQRPayload(c)
	if err != nil {
		return ""
	}
	png, err := qrcode.Encode(payload, qrcode.Medium, 256)
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}
