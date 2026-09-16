## 1. Gateway — accept and store the device manifest

- [x] 1.1 Add a `deviceManifest` struct (platform, formFactor, name, modelId, modelDisplay, osName, osVersion, appVersion, appBuild — all optional strings) and extend `POST /api/setup` request binding to decode an optional `device` object; verify `go build ./...` compiles and a setup POST with no `device` field still returns a key
- [x] 1.2 Write `TestSetupClientAcceptsDeviceManifest`: POST `/api/setup` with a `device` object asserts the issued registry entry contains the manifest fields; verify `go test ./... -run TestSetupClientAcceptsDeviceManifest` passes
- [x] 1.3 Update `issueDeviceKey` to persist the manifest under the entry's `device` key; verify the registry-shape test asserts `{"createdAt": ..., "device": {...}}` and passes
- [x] 1.4 Write `TestSetupClientWithoutManifest`: POST `/api/setup` with no `device` asserts a key is still issued and the entry has no `device` field; verify `go test ./... -run TestSetupClientWithoutManifest` passes

## 2. Gateway — generalize the device type and settings UI

- [x] 2.1 Rename `iosDevice` → `device` and add `Platform`, `Name`, `ModelDisplay`, `OSName`, `OSVersion` fields; update `listDeviceKeys` to read them from the stored `device` map; verify `go build ./...` compiles
- [x] 2.2 Write `TestListDeviceKeysPopulatesMetadata`: seed the registry with a `device` entry and assert `listDeviceKeys` returns the metadata, plus a legacy entry (no `device`) returns empty fields; verify `go test ./... -run TestListDeviceKeysPopulatesMetadata` passes
- [x] 2.3 Update `deviceRow` in `project_settings.templ` to render name (or modelDisplay/modelId fallback) + platform + OS version, with the masked-key row as the fallback when metadata is absent; verify `templ generate` succeeds and `go build ./...` compiles
- [x] 2.4 Extend `project_settings_ui_test.go` device-row assertions to cover a metadata device and a legacy device; verify `go test ./... -run TestUIProjectDeviceSettings` passes

## 3. iOS — capture device information

- [x] 3.1 Add a `DeviceInfo` provider reading `UIDevice.current` (name, systemName, systemVersion) and the raw hardware identifier via `uname`/`sysctlbyname("hw.machine")`, with a small in-repo identifier→marketing-name table that falls back to the raw id; verify the app builds on simulator via `tools/ios-build-mac.sh`
- [x] 3.2 Write a Swift unit test in `VoiceAgentTests` that `DeviceInfo` produces a manifest with a non-empty `platform`, `modelId`, `osName`, and `osVersion`; verify the test target builds and passes
- [x] 3.3 Derive `platform` (ios/ipados) and `formFactor` from `systemName`/`userInterfaceIdiom`; verify the same Swift unit test asserts the correct platform value

## 4. iOS — send the manifest on setup

- [x] 4.1 Extend `AlfredSetupClient.Payload` to carry an optional `device` object and `AlfredQRScannerView` to build it from `DeviceInfo` before calling setup; verify the app builds on simulator via `tools/ios-build-mac.sh`
- [x] 4.2 Write a Swift unit test that `AlfredSetupClient` encodes the `device` manifest into the request body; verify the test target passes

## 5. Verification

- [x] 5.1 Run `PATH="/root/go/bin:$PATH" templ generate` and `go build ./...` from `gateway/`; verify both succeed with no diffs to generated files
- [x] 5.2 Run `PATH="/root/go/bin:$PATH" task lint` and `go test ./...` from `gateway/`; verify lint is clean and all tests pass
- [x] 5.3 Build the iOS app for the simulator via `tools/ios-build-mac.sh` and run the `VoiceAgentTests` suite on the Mac; verify build + tests pass
- [ ] 5.4 Manual check: start the dev server (`task dev`), register a device via the QR flow, and confirm the Devices page shows the device name, platform, and OS version
