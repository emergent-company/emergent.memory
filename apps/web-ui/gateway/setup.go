package main

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	qrcode "github.com/skip2/go-qrcode"
)

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

// setupTokenCategory / setupTokenKey / setupTokenTTL are the settings
// coordinates of the current one-time setup token. The token is persisted in
// the memory backend (not in-process) so it survives gateway restarts — `air`
// hot-reload restarts the binary on any code change, and an in-memory token
// minted before a restart would be rejected with 401 afterwards. Stored value:
// {"token": "<32-hex>", "createdAt": "<RFC3339 UTC>", "used": false,
// "deviceToken": "<emt_* plaintext>", "deviceTokenId": "<uuid>"}. The device
// credential is minted server-side at setup-token mint time (session present)
// and stored here for a single claim at /api/setup — the same plaintext trust
// level as the setup token itself, with a 10-minute window.
const (
	setupTokenCategory = "ios_setup_token"
	setupTokenKey      = "current"
	setupTokenTTL      = 10 * time.Minute
)

// mintSetupToken returns the current one-time setup token. While the stored
// token is unexpired and unused it is REUSED, which keeps the QR stable across
// page reloads; otherwise a fresh 32-hex token is minted and persisted, and a
// fresh device credential is minted server-side alongside it (revoking any
// orphaned prior credential so an unclaimed QR never leaks a live credential).
func (s *Server) mintSetupToken(ctx context.Context) (string, error) {
	if ps, err := s.memory.GetProjectSetting(ctx, setupTokenCategory, setupTokenKey); err != nil {
		return "", err
	} else if ps != nil {
		token, _ := ps.Value["token"].(string)
		used, _ := ps.Value["used"].(bool)
		createdAt, _ := ps.Value["createdAt"].(string)
		if token != "" && !used && setupTokenFresh(createdAt) {
			return token, nil
		}
	}

	// Rotate: revoke any prior UNCLAIMED device credential so an abandoned QR
	// never leaks a live credential. A claimed credential (used=true) is a real
	// device and must survive a page reload — only an unclaimed orphan is
	// revoked here.
	if ps, err := s.memory.GetProjectSetting(ctx, setupTokenCategory, setupTokenKey); err == nil && ps != nil {
		used, _ := ps.Value["used"].(bool)
		if !used {
			if priorID, _ := ps.Value["deviceTokenId"].(string); priorID != "" {
				_ = s.memory.RevokeAPIToken(ctx, priorID)
			}
		}
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
	value := map[string]any{
		"token":     token,
		"createdAt": time.Now().UTC().Format(time.RFC3339),
		"used":      false,
	}
	if deviceToken != "" {
		value["deviceToken"] = deviceToken
		value["deviceTokenId"] = deviceTokenID
	}
	if err := s.memory.SetProjectSetting(ctx, setupTokenCategory, setupTokenKey, value); err != nil {
		return "", err
	}
	return token, nil
}

// consumeSetupToken validates and single-uses the current setup token, returning
// the device credential it carries. A nil setting, memory error,
// mismatched/used/expired/unparsable token, or a missing device credential all
// return ("", false). On success the same value is written back with used=true,
// so a second consume returns false.
func (s *Server) consumeSetupToken(ctx context.Context, token string) (string, bool) {
	ps, err := s.memory.GetProjectSetting(ctx, setupTokenCategory, setupTokenKey)
	if err != nil || ps == nil {
		return "", false
	}
	stored, _ := ps.Value["token"].(string)
	used, _ := ps.Value["used"].(bool)
	createdAt, _ := ps.Value["createdAt"].(string)
	if stored == "" || subtle.ConstantTimeCompare([]byte(stored), []byte(token)) != 1 || used || !setupTokenFresh(createdAt) {
		return "", false
	}
	deviceToken, _ := ps.Value["deviceToken"].(string)
	if deviceToken == "" {
		return "", false
	}
	// Mark used, preserving the already-issued device credential.
	if err := s.memory.SetProjectSetting(ctx, setupTokenCategory, setupTokenKey, map[string]any{
		"token":         stored,
		"createdAt":     createdAt,
		"used":          true,
		"deviceToken":   deviceToken,
		"deviceTokenId": ps.Value["deviceTokenId"],
	}); err != nil {
		return "", false
	}
	return deviceToken, true
}

// setupTokenFresh reports whether a stored RFC3339 createdAt is still within
// the setup-token TTL. Empty or unparsable timestamps are treated as expired.
func setupTokenFresh(createdAt string) bool {
	if createdAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return false
	}
	return time.Since(t) <= setupTokenTTL
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
	if ps, err := s.memory.GetProjectSetting(ctx, setupTokenCategory, setupTokenKey); err == nil && ps != nil {
		if used, _ := ps.Value["used"].(bool); !used {
			pendingID, _ = ps.Value["deviceTokenId"].(string)
		}
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
	deviceToken, ok := s.consumeSetupToken(c.Request().Context(), in.Token)
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
	})
}

// setupQRPayload returns the current one-time setup token (minting it if
// needed) and the JSON string a setup QR encodes: {"setupURL":..., "token":...}.
func (s *Server) setupQRPayload(c echo.Context) (string, error) {
	token, err := s.mintSetupToken(c.Request().Context())
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(map[string]string{
		"setupURL": s.publicBaseURL(c) + "/api/setup",
		"token":    token,
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
