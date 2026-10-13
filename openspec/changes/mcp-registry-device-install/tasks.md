# Tasks — mcp-registry-device-install

TDD throughout: each implementation task ships with the unit test named beside
it. Build/test per module (`apps/server`, `apps/web-ui/gateway`).

## 1. Server — device install path

- [x] 1.1 `InstallFromRegistryDTO` gains `Target` / `TargetInstanceID`, so the
  install route can ask for a device install.
- [x] 1.2 `buildDeviceInstallDTO` translates an entry's first locally-runnable
  package (npm → `npx -y`, pypi → `uvx`, registry order) into a stdio
  `CreateMCPServerDTO` with rendered runtime/package arguments, the pinned
  version, env placeholders, secret keys and required config; rejects an entry
  with no locally-runnable package with `ErrUnsupportedRegistryTransport`.
  Tests: `registry_device_install_test.go`.
- [x] 1.3 `InstallFromRegistry` selects the device path on
  `Target == TargetDevice`, sets the target fields, and leaves the outbound path
  unchanged.
- [x] 1.4 Build + `go test ./domain/mcpregistry/...`.

## 2. Gateway — registry page device install

- [x] 2.1 `MemoryClient.InstallRegistryServerToDevice` posts the device-target
  install; `MemoryBackend` gains the method and `fakeMemory` the default.
  Test: `TestInstallRegistryServerToDevice`.
- [x] 2.2 Registry page loads connected devices (`ListRelaySessions`) into
  `mcpRegistryPageData`, degrading to `DevicesErr` when the fetch fails.
- [x] 2.3 `mcpRegistryEntry` renders a device-install trigger + dialog for
  package-only entries: a device picker when a device is connected, connector
  setup steps when none is, and an error state when the list failed.
  Tests: `TestRenderMCPRegistryPage`,
  `TestRenderMCPRegistryPackageOnlyDeviceInstall`,
  `TestRenderMCPRegistryDeviceDialogWithDevices`,
  `TestRenderMCPRegistryDeviceDialogDevicesError`.
- [x] 2.4 `uiMCPRegistryInstallToDevice` handler (PRG) with the route registered;
  required-config installs go to the configure form, successes to the devices
  page, rejections back to the listing, and missing name/device never reach the
  backend. Tests: `TestUIMCPRegistryInstallToDevice*`.
- [x] 2.5 Connected devices page confirms the install (`mcp_installed`).

## 3. Verification

- [x] 3.1 `templ generate` + `go generate ./...` (component graph refreshed).
- [x] 3.2 `go build ./...` and `go test ./...` in both modules.
- [x] 3.3 `golangci-lint run` on both modules.
- [x] 3.4 `openspec validate mcp-registry-device-install --strict`.

## 4. Follow-ups (not this change)

- [ ] 4.1 OAuth connect for registry servers (`mcp-oauth-connect`).
- [ ] 4.2 Authoring device-targeted servers from the MCP servers form (today
  only a registry device install creates one), and showing the target on the
  server row.
