## Why

Document extraction is served by Kreuzberg v4 (`ghcr.io/kreuzberg-dev/kreuzberg-full:4.10.3`). Kreuzberg has been rebranded and re-versioned as **xberg** by the same author — the upstream repo `github.com/Goldziher/kreuzberg` now redirects to `github.com/xberg-io/xberg`, and Kreuzberg v4 continues only as a best-effort LTS. Our integration surface is deliberately small (a plain HTTP client plus one Docker service; only `POST /extract` and `GET /health` are used), so migrating now captures the successor line while the change is bounded.

The migration is not a drop-in version bump: xberg's `/extract` response is an envelope (`{results, errors, summary}`) rather than Kreuzberg v4's bare JSON array, and all server env vars were renamed `KREUZBERG_*` → `XBERG_*` with no fallback.

## What Changes

- **Server client**: rename `pkg/kreuzberg` → `pkg/xberg`; parse the `/extract` envelope and surface per-input `errors`; drop `easyocr` from the accepted OCR backends (xberg supports `tesseract`, `paddleocr`, `sceptre`, `vlm`); rename identifiers (`ShouldUseXberg`, `IsXbergSupported`, `XbergSupportedMIMETypes`).
- **Config (BREAKING)**: hard rename env vars `KREUZBERG_ENABLED` / `KREUZBERG_SERVICE_URL` / `KREUZBERG_SERVICE_TIMEOUT` / `KREUZBERG_MAX_FILE_SIZE_MB` → `XBERG_*`. No back-compat aliases.
- **Health**: report the extraction component as `xberg` instead of `kreuzberg`.
- **Extraction worker**: extraction method label becomes `"xberg"`.
- **Deployment (BREAKING)**: pin `ghcr.io/xberg-io/xberg:1.3.0`; compose service `xberg` / container `memory-xberg`; `XBERG_PORT`, `XBERG_LOG_LEVEL` (mapped to `RUST_LOG`).
- **CLI**: installer template + `XbergImage` constant, doctor component/container names, tests, and the CI pin-sync grep.
- **Docs/skills/e2e**: rename references to xberg across deploy docs, skills, and e2e prose.

## Capabilities

### New Capabilities
- `document-extraction`: the server's integration with the xberg document extraction service — configuration, request/response contract, health reporting, and deployment pin.

### Modified Capabilities
- `e2e-extraction-test`: the document-conversion e2e requirement/exercises now reference the xberg converter.

## Impact

- `apps/server/pkg/xberg/` (renamed from `pkg/kreuzberg/`), `apps/server/internal/config/`, `apps/server/domain/extraction/`, `apps/server/domain/health/`, `apps/server/internal/testutil/`, `apps/server/cmd/server/main.go`.
- `apps/cli/internal/installer/`, `apps/cli/internal/cmd/doctor.go`.
- `deploy/self-hosted/` (compose + install scripts + docs), `.env.example`, `e2e/docker-compose.yml`, `apps/web-ui/tools/logs.sh`, `.github/workflows/cli.yml`.
- Docs/skills: `deploy/self-hosted/README.md`, `INSTALL.md`, `docs/MAC_STANDALONE_INSTALLATION.md`, `docs/GHCR_PACKAGE_SETUP.md`, `apps/server/README.md`, `AGENT.md`, env-editor skills.

## Migration / upgrade note

Existing self-hosted installs must rename `KREUZBERG_*` env vars to `XBERG_*` (the server no longer reads the old names). The xberg container also uses a fresh cache/namespace, so OCR/model assets are re-downloaded on first start. No database migration is required.
