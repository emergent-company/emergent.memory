## 1. Server client migration

- [x] 1.1 Rename `apps/server/pkg/kreuzberg/` to `apps/server/pkg/xberg/`, change the package declaration, fx module name, and logger scope to `xberg`. Verify: `go build ./...` in `apps/server` compiles.
- [x] 1.2 Parse the xberg `/extract` envelope (`{results, errors, summary}`) instead of the Kreuzberg v4 bare array, returning `results[0]` on success and surfacing `errors` when `results` is empty. Verify: `go test ./pkg/xberg/...` passes.
- [x] 1.3 Rename exported identifiers to xberg (`ShouldUseXberg`, `IsXbergSupported`, `XbergSupportedMIMETypes`) and drop `easyocr` from the OCR backends. Verify: `go build ./...` compiles.
- [x] 1.4 Update all importers (`cmd/server/main.go`, `domain/extraction`, `domain/health`, `internal/testutil`, and tests) to the new package path and `Xberg` symbols. Verify: `go vet ./...` passes.

## 2. Server config, health, and worker

- [x] 2.1 Rename `config.KreuzbergConfig`/`Kreuzberg` field to `XbergConfig`/`Xberg` and env tags to `XBERG_ENABLED` / `XBERG_SERVICE_URL` / `XBERG_SERVICE_TIMEOUT` / `XBERG_MAX_FILE_SIZE_MB`. Verify: `go test ./internal/config/...` passes.
- [x] 2.2 Report the health component as `xberg` (component key, messages, and tests). Verify: `go test ./domain/health/...` passes.
- [x] 2.3 Label extraction method `"xberg"` in the document parsing worker. Verify: `go build ./...` compiles.

## 3. CLI and deployment

- [x] 3.1 Rename the CLI installer constant to `XbergImage = ghcr.io/xberg-io/xberg:1.3.0` and update the rendered compose template (service `xberg`, container `memory-xberg`, `XBERG_PORT`, `RUST_LOG`/`XBERG_LOG_LEVEL`, `XBERG_SERVICE_URL`, `XBERG_ENABLED`). Verify: `go test ./internal/installer/...` passes.
- [x] 3.2 Update CLI doctor component/container names and installer tests. Verify: `go build ./...` in `apps/cli` compiles.
- [x] 3.3 Update the self-hosted static copies (`docker-compose.yml`, `docker-compose.local.yml`, `install-online.sh`, `install.sh`, `.env.example`) to the xberg image and env names. Verify: `bash -n` on the scripts.
- [x] 3.4 Update `.github/workflows/cli.yml` pin-sync grep to `XbergImage` without weakening the check.
- [x] 3.5 Update root `.env`/`.env.example`, `e2e/docker-compose.yml`, and `apps/web-ui/tools/logs.sh` env/service names.

## 4. Docs, skills, e2e prose

- [x] 4.1 Update deploy/self-hosted docs, `docs/MAC_STANDALONE_INSTALLATION.md`, `docs/GHCR_PACKAGE_SETUP.md`, `apps/server/README.md`, `AGENT.md`, `UPLOAD_API.md`, and env-editor skills to xberg.
- [x] 4.2 Update e2e CLI test prose to reference xberg.
- [x] 4.3 Add the upgrade note for the `KREUZBERG_*` → `XBERG_*` rename.

## 5. Verification

- [x] 5.1 `go build ./...` and `go test ./...` in `apps/server` (unit packages) pass.
- [x] 5.2 `go build ./...` and `go test ./internal/installer/...` in `apps/cli` pass.
- [x] 5.3 `openspec validate migrate-kreuzberg-to-xberg` passes.
- [x] 5.4 `task lint` / lefthook lint passes.
