## Why

Alfred's admin UI is currently a single stdlib-HTTP server (`agent/admin.py`) that serves hardcoded daisyUI HTML and manages the system prompt by writing a text file (`prompt.txt`). We just built a proper control-plane REST API (FastAPI, OpenAPI 3.1) for agents and MCP servers, but there is no UI consuming it — configuring an agent still means hand-editing JSON via curl. We want a real, maintainable web UI built in Go on top of the go-daisy component library, so the API becomes the single source of truth for agent/MCP configuration.

## What Changes

- New Go web application (new module/package, e.g. `ui/`) built with **go-daisy** (a-h/templ + labstack/echo) that talks to the control-plane API over HTTP with `X-API-Key` auth.
- UI views for the control-plane surface:
  - Agents: list, create, edit, delete, activate/deactivate, and status (live sessions, version, config hash).
  - MCP servers: list, create, edit, delete.
  - Agent backend editor (openai_compat / realtime / a2a discriminated union), tools (builtin functions + MCP refs), and sub-agent references.
- Retire the admin.py prompt-editor and catalog-preview endpoints in favor of API-driven fields: `system_prompt` on the agent, `home_state_catalog` flag.
- **BREAKING**: the legacy `agent/admin.py` prompt editor (`GET/POST /api/prompt` → `prompt.txt`) is replaced by the agent `system_prompt` field in the control-plane API.
- Out of scope (unchanged): iOS token mint + QR onboarding (`POST /api/token`, `/api/qr-config`) and the sessions viewer remain served by `agent/admin.py`; they are iOS-client concerns, not part of the control-plane API.

## Capabilities

### New Capabilities

- `control-plane-ui`: a Go + go-daisy web UI that manages agents and MCP servers through the control-plane API.

### Modified Capabilities

<!-- none -->

## Impact

- New Go module + dependencies (`github.com/emergent-company/go-daisy`, `a-h/templ`, `labstack/echo`).
- Deployable as a single Go binary serving the UI; runs alongside the existing `alfred-api` (control plane), `alfred-worker@*` (agents), and `alfred-admin` (iOS onboarding).
- Consumes the control-plane API at `:8081` (Tailscale trust boundary, `X-API-Key`).
- No changes to the API itself; if a field is missing for a UI need (e.g. a device-catalog preview endpoint), it is added to the API in this change.
