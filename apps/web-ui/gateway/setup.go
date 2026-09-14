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

// deviceKeyRegistryCategory / deviceKeyRegistryKey are the single settings
// coordinates of the device-key registry. Memory has no list-by-category HTTP
// route, so per-key settings could never be enumerated — instead all device
// keys live in ONE setting whose value is
// {"devices": {"<device-key-hex>": {"createdAt": "<RFC3339>"}}}.
const (
	deviceKeyRegistryCategory = "ios_device_keys"
	deviceKeyRegistryKey      = "registry"
)

// device is one registered device, as shown in the settings UI. Key/CreatedAt
// always come from the registry entry; the metadata fields (Platform, Name,
// ModelID, ModelDisplay, OSName, OSVersion) are only populated when the entry
// carries a self-reported "device" manifest (absent for legacy entries).
type device struct {
	Key          string
	CreatedAt    string // RFC3339
	Platform     string
	Name         string
	ModelID      string
	ModelDisplay string
	OSName       string
	OSVersion    string
}

// setupTokenCategory / setupTokenKey / setupTokenTTL are the settings
// coordinates of the current one-time setup token. The token is persisted in
// the memory backend (not in-process) so it survives gateway restarts — `air`
// hot-reload restarts the binary on any code change, and an in-memory token
// minted before a restart would be rejected with 401 afterwards. Stored value:
// {"token": "<32-hex>", "createdAt": "<RFC3339 UTC>", "used": false}.
const (
	setupTokenCategory = "ios_setup_token"
	setupTokenKey      = "current"
	setupTokenTTL      = 10 * time.Minute
)

// mintSetupToken returns the current one-time setup token. While the stored
// token is unexpired and unused it is REUSED, which keeps the QR stable across
// page reloads; otherwise a fresh 32-hex token is minted and persisted.
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
	token := randomHex(32)
	if err := s.memory.SetProjectSetting(ctx, setupTokenCategory, setupTokenKey, map[string]any{
		"token":     token,
		"createdAt": time.Now().UTC().Format(time.RFC3339),
		"used":      false,
	}); err != nil {
		return "", err
	}
	return token, nil
}

// consumeSetupToken validates and single-uses the current setup token. A nil
// setting, memory error, mismatched/used/expired/unparsable token all return
// false. On success the same value is written back with used=true, so a second
// consume returns false.
func (s *Server) consumeSetupToken(ctx context.Context, token string) bool {
	ps, err := s.memory.GetProjectSetting(ctx, setupTokenCategory, setupTokenKey)
	if err != nil || ps == nil {
		return false
	}
	stored, _ := ps.Value["token"].(string)
	used, _ := ps.Value["used"].(bool)
	createdAt, _ := ps.Value["createdAt"].(string)
	if stored == "" || subtle.ConstantTimeCompare([]byte(stored), []byte(token)) != 1 || used || !setupTokenFresh(createdAt) {
		return false
	}
	if err := s.memory.SetProjectSetting(ctx, setupTokenCategory, setupTokenKey, map[string]any{
		"token":     stored,
		"createdAt": createdAt,
		"used":      true,
	}); err != nil {
		return false
	}
	return true
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

// toMap renders the manifest as the stored map, keeping only the non-empty
// fields under their JSON keys. An empty result means nothing is worth
// persisting. Stored as map[string]any (never a struct) because the memory
// backend JSON-round-trips the registry setting.
func (m deviceManifest) toMap() map[string]any {
	out := map[string]any{}
	for _, kv := range []struct{ k, v string }{
		{"platform", m.Platform},
		{"formFactor", m.FormFactor},
		{"name", m.Name},
		{"modelId", m.ModelID},
		{"modelDisplay", m.ModelDisplay},
		{"osName", m.OSName},
		{"osVersion", m.OSVersion},
		{"appVersion", m.AppVersion},
		{"appBuild", m.AppBuild},
	} {
		if kv.v != "" {
			out[kv.k] = kv.v
		}
	}
	return out
}

// deviceRegistry reads the device-key registry setting, returning its devices
// map. Nil map = no registry yet; error = memory unreachable.
func (s *Server) deviceRegistry(ctx context.Context) (map[string]any, error) {
	ps, err := s.memory.GetProjectSetting(ctx, deviceKeyRegistryCategory, deviceKeyRegistryKey)
	if err != nil {
		return nil, err
	}
	if ps == nil {
		return nil, nil
	}
	devices, _ := ps.Value["devices"].(map[string]any)
	return devices, nil
}

// deviceKeyValid reports whether key is a registered per-device key (looked up
// in the device-key registry). GetProjectSetting returns (nil, nil) on 404,
// which is treated as invalid.
func (s *Server) deviceKeyValid(ctx context.Context, key string) bool {
	devices, err := s.deviceRegistry(ctx)
	if err != nil || devices == nil {
		return false
	}
	_, ok := devices[key]
	return ok
}

// issueDeviceKey mints a fresh 32-hex per-device key and inserts it into the
// device-key registry. When m is non-nil, the non-empty manifest fields are
// persisted under the entry's "device" key (a nil/empty manifest yields the
// legacy {"createdAt": ...} shape). The read-modify-write is not locked: a
// concurrent issue could drop a key, which is acceptable for a single-admin
// self-host (setup tokens are single-use, which already serializes real-world
// issuance).
func (s *Server) issueDeviceKey(ctx context.Context, m *deviceManifest) (string, error) {
	key := randomHex(32)
	devices, err := s.deviceRegistry(ctx)
	if err != nil {
		return "", err
	}
	if devices == nil {
		devices = map[string]any{}
	}
	entry := map[string]any{"createdAt": time.Now().UTC().Format(time.RFC3339)}
	if m != nil {
		if dev := m.toMap(); len(dev) > 0 {
			entry["device"] = dev
		}
	}
	devices[key] = entry
	if err := s.memory.SetProjectSetting(ctx, deviceKeyRegistryCategory, deviceKeyRegistryKey, map[string]any{"devices": devices}); err != nil {
		return "", err
	}
	return key, nil
}

// listDeviceKeys returns the registered devices, most recently issued first.
// An empty or absent registry yields an empty slice (RFC3339 UTC timestamps
// sort lexicographically, so a plain string comparison is correct). Metadata
// fields are read from the entry's "device" manifest when present; legacy
// entries (no "device") keep those fields empty.
func (s *Server) listDeviceKeys(ctx context.Context) ([]device, error) {
	devices, err := s.deviceRegistry(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]device, 0, len(devices))
	for key, v := range devices {
		d := device{Key: key}
		if m, ok := v.(map[string]any); ok {
			d.CreatedAt, _ = m["createdAt"].(string)
			if dev, ok := m["device"].(map[string]any); ok {
				d.Platform, _ = dev["platform"].(string)
				d.Name, _ = dev["name"].(string)
				d.ModelID, _ = dev["modelId"].(string)
				d.ModelDisplay, _ = dev["modelDisplay"].(string)
				d.OSName, _ = dev["osName"].(string)
				d.OSVersion, _ = dev["osVersion"].(string)
			}
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out, nil
}

// revokeDeviceKey removes key from the device-key registry. Removing an
// absent key is a no-op (the registry is still written back unchanged).
func (s *Server) revokeDeviceKey(ctx context.Context, key string) error {
	devices, err := s.deviceRegistry(ctx)
	if err != nil {
		return err
	}
	if devices == nil {
		return nil
	}
	delete(devices, key)
	return s.memory.SetProjectSetting(ctx, deviceKeyRegistryCategory, deviceKeyRegistryKey, map[string]any{"devices": devices})
}

// maskDeviceKey renders a device key for display: a bullet mask plus the last
// 6 characters (device keys are 64-hex; the suffix is enough to tell devices
// apart without exposing the full credential).
func maskDeviceKey(key string) string {
	if len(key) <= 6 {
		return key
	}
	return "••••••" + key[len(key)-6:]
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
// token for a per-device API key. The returned URLs point at this gateway
// (single origin), pinned to PUBLIC_BASE_URL when configured.
func (s *Server) setupClient(c echo.Context) error {
	var in struct {
		Token  string          `json:"token"`
		Device *deviceManifest `json:"device"`
	}
	if err := c.Bind(&in); err != nil || in.Token == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid or missing token"})
	}
	if !s.consumeSetupToken(c.Request().Context(), in.Token) {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unknown, expired, or already-used token"})
	}
	key, err := s.issueDeviceKey(c.Request().Context(), in.Device)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
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
		"apiKey":        key,
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
