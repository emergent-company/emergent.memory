## Why

The Devices settings page only shows a masked API key and a registration time, and the registry assumes every client is an iOS device. When a device registers it never says what it is, so the user can't tell devices apart or know what software each one runs.

## What Changes

- The client sends a device "self-introduction" manifest during the one-time setup exchange (`POST /api/setup`), describing what it is: platform/system type, device name, hardware model, OS name and version, and app version.
- The gateway accepts an optional manifest on setup, stores it alongside the issued key, and records server-derived `firstSeenAt`/`lastSeenAt` timestamps.
- The device registry entry generalizes from `{createdAt}` to a richer record with a `platform` field instead of assuming iOS.
- The Devices settings UI renders the device name, model, OS version, and platform (falling back to the masked key when metadata is absent) so devices are distinguishable at a glance.
- The manifest is optional and additive: clients that predate this change still register and show the legacy masked-key row.

## Capabilities

### New Capabilities

- `device-registry`: server-side registry of registered devices, including the self-introduction manifest a device sends on registration, how that metadata is stored and keyed, and how it is surfaced in the settings UI.

### Modified Capabilities

- `ios-agent-management`: the QR authentication requirement now has the app send its device manifest as part of the setup exchange.

## Impact

- `gateway/setup.go` — setup handler, device registry storage shape, `iosDevice` type, issue/list/revoke.
- `gateway/project_settings.templ` — Devices panel and device row rendering.
- `gateway/settings_handlers.go` — Devices route data.
- `client/ios/VoiceAgent/Alfred/AlfredSetupClient.swift` + `AlfredQRScannerView.swift` — capture and send device metadata on setup.
- New Swift device-info provider (`UIDevice` + `uname`/`sysctlbyname`).
- Unit tests for registry shape, setup parsing, and UI rendering.
