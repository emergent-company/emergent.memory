## Context

Devices register through a one-time QR setup flow: the gateway mints a single-use setup token, the client POSTs it to `POST /api/setup` (unauthenticated), and the gateway issues a 64-hex per-device API key stored in the `ios_device_keys/registry` setting as `{"devices": {"<key>": {"createdAt": "<RFC3339>"}}}`. The Devices settings page renders each entry as a masked key plus a relative registration time (`project_settings.templ` → `deviceRow`). See proposal.md — Why for motivation.

The only client today is the native iOS app (`AlfredSetupClient` + `AlfredQRScannerView`), which already decodes the setup response. There is no spec-owned device registry today; the gateway logic in `gateway/setup.go` is un-spec'd.

## Goals / Non-Goals

**Goals:**
- Add an optional device manifest to the setup exchange and persist it with the registration.
- Store a `platform` field so the registry is no longer iOS-only.
- Render device name/model/OS in the settings UI with a masked-key fallback for legacy entries.
- Stay fully backward compatible: old clients and old registry entries keep working.

**Non-Goals:**
- No dedup/upsert on a stable client id (IDFV/self-generated UUID); the registry stays keyed by the issued API key.
- No `lastSeenAt` tracking (only registration time, which already exists as `createdAt`).
- No iOS 16+ user-assigned-device-name entitlement work; we send `UIDevice.current.name` as-is (may be generic on newer OS) and rely on `modelId` for real identification.
- No Android/web clients; the model just makes room for them.

## Decisions

### 1. Extend `POST /api/setup`, don't add a new endpoint

The setup exchange is the only moment the device has a working unauthenticated channel and is already the registration point, so it is the natural place for the introduction.

- **Alternative**: a separate `/api/device/hello` call after setup. Rejected — extra round trip and a second auth/replay surface for no added value.

Request body becomes `{"token": "...", "device": {...}}`. The `device` field is optional; a missing/empty `device` behaves exactly like today.

### 2. Manifest shape: a nested, all-optional `device` object

```
"device": {
  "platform":      "ios",          // ios | ipados | macos | android | web
  "formFactor":    "phone",        // phone | tablet | desktop (derived, optional)
  "name":          "John's iPhone",// user-assigned; optional
  "modelId":       "iPhone15,2",   // raw hardware identifier (uname/sysctlbyname)
  "modelDisplay":  "iPhone 14 Pro",// best-effort marketing name; optional
  "osName":        "iOS",
  "osVersion":     "18.3.1",
  "appVersion":    "1.2.0",
  "appBuild":      "42"
}
```

All fields optional strings. Nested (vs. flat top-level) keeps the manifest clearly separated from the transport token and makes future server-side schema evolution contained.

- **Alternative**: flat top-level fields (`platform`, `deviceName`, …) directly on the request. Rejected — pollutes the token exchange and mixes concerns.

### 3. Storage: additive registry entry

Registry entry becomes `{"createdAt": "<RFC3339>", "device": {<manifest>}}`. `createdAt` is already server-generated and satisfies the server-derived-timestamp requirement, so it is kept unchanged. Old entries simply lack `device`.

- **Alternative**: rename the settings category `ios_device_keys` → `device_keys` with a migration. Rejected — the category name is an internal storage key, not user-visible, and renaming forces a needless migration. The Go type and UI copy are generalized instead.

The `iosDevice` struct is renamed to `device` and gains `Platform`, `Name`, `ModelDisplay`, `OSName`, `OSVersion` fields; `listDeviceKeys` populates them from the stored `device` map when present.

### 4. Swift: small built-in device-info provider, no third-party dependency

A new `DeviceInfo` provider reads `UIDevice.current` (`name`, `systemName`, `systemVersion`) and the raw hardware identifier via `uname()`/`sysctlbyname("hw.machine")`. It stores the raw `modelId` always and maps common identifiers to a display name through a small in-repo table; unmapped identifiers fall back to the raw id.

- **Alternative**: adopt DeviceKit for marketing names. Rejected — adds a dependency for a display-only nicety; the raw id is the durable source of truth and the table goes stale every September anyway. The raw `modelId` keeps the record stable regardless.

### 5. UI fallback chain

`deviceRow` shows, in priority order: device `name` (when non-empty) → `modelDisplay`/`modelId` → masked key, plus platform and OS version when available. Legacy entries (no `device`) render exactly the current masked-key + time row.

## Risks / Trade-offs

- [Old registry entries have no metadata] → Mitigation: fallback renders the masked-key row; no migration required.
- [Device name is PII and generic on iOS 16+ without entitlement] → Mitigation: treat `name` as optional display sugar; `modelId` is the reliable discriminator; self-hosted backend, consent is the admin's own onboarding.
- [Marketing-name table drifts stale] → Mitigation: store raw `modelId`; display name is best-effort and falls back to the raw id.
- [Client-supplied fields are spoofable] → Mitigation: the manifest is descriptive only; authentication still relies on the issued key and one-time token, never on manifest fields.

## Migration Plan

- Deploy is additive: gateway change accepts the optional `device` field and stores it; no existing registry entry is rewritten.
- Rollback: reverting the gateway leaves old entries readable (the new code only reads a `device` key that may be absent); reverting the iOS app simply stops sending the manifest.
