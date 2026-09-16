## Context

Alfred currently has two HTTP surfaces:

- `agent/admin.py` — a stdlib-HTTP server (:8080) serving a single daisyUI page for iOS onboarding (token mint, QR) plus a prompt editor that writes `prompt.txt`, a device-catalog preview, and a sessions viewer.
- `agent/api/main.py` — the new control-plane API (:8081, FastAPI + OpenAPI 3.1) with agents and MCP-server CRUD, `X-API-Key` auth.

The UI to build is a Go application using the `go-daisy` library (`github.com/emergent-company/go-daisy`: daisyUI components over a-h/templ + labstack/echo). It consumes the control-plane API and becomes the front door for agent/MCP configuration. The API and workers run on `home2` LXC 112 behind a Tailscale-only trust boundary.

## Goals / Non-Goals

**Goals:**

- A type-safe, single-binary Go web UI that fully drives the control-plane API (no direct file or DB access).
- First-class agent editing: the discriminated backend (`openai_compat` / `realtime` / `a2a`), tools (builtin functions + MCP refs), and sub-agent references.
- MCP-server management (list/create/edit/delete).

**Non-Goals:**

- iOS onboarding (token mint, QR) and the sessions viewer — these stay in `agent/admin.py`; they are iOS-client concerns, not control-plane concerns.
- Changing the control-plane API's behavior; if a UI need requires a new endpoint (e.g. device-catalog preview), it is added to the API within this change rather than implemented out-of-band in the UI.
- Agent runtime / worker changes.

## Decisions

1. **Go + go-daisy + templ + echo (server-side rendered).** The existing admin UI is already daisyUI, so go-daisy preserves the look while giving typed components and no separate frontend build. Alternatives considered: a JS SPA (React/Svelte) — rejected, adds a second toolchain and an API-token-in-browser problem; plain Go `html/template` — rejected, loses the daisyUI component set.

2. **UI talks to the control-plane API over HTTP (`localhost:8081`) with `X-API-Key`.** The OpenAPI spec is the contract. The UI reads the key from an env var and forwards it; it never talks to SQLite or the agent code directly. This keeps the API as the single source of truth.

3. **Thin hand-written Go client, not generated.** The endpoint surface is small (~13 routes). Hand-writing keeps the module lean; switch to `oapi-codegen` from `/openapi.json` only if the surface grows. The client mirrors the Pydantic models (Backend discriminated union) as Go structs with `type`-based unmarshalling.

4. **Module location + port.** New `ui/` directory (own `go.mod`, `replace` directive pointing at the local `go-daisy` checkout) serving on `:8090` by default. Distinct from `:8080` (admin) and `:8081` (API).

5. **Backend form modeling.** One struct per backend variant with a `Type` discriminator, rendered as a switch of templ fragments; JSON marshals/unmarshals to the API's `oneOf` shape (`type` field).

## Risks / Trade-offs

- **[go-daisy is an internal, fast-moving library]** → pin via `go.mod replace` to the local `/root/go-daisy` path (or a specific commit); re-check on upgrade.
- **[templ codegen in build]** → `templ generate` is a required build step; document it and run it in CI/verify.
- **[API↔UI contract drift across two languages]** → single source of truth is the API's OpenAPI schema; the Go client structs are hand-mirrored and verified by a smoke test against the live API.
- **[No device-catalog preview endpoint in the API today]** → excluded from v1 (non-goal); if wanted, add `GET /api/agents/{id}/preview` to the API first, then surface it in the UI.
- **[X-API-Key lives in the UI process env]** → Tailscale-only trust boundary (same as the API); never log or render the key.

## Migration Plan

1. Scaffold `ui/` Go module wired to go-daisy; implement client + agents/MCP pages.
2. Run the UI on `:8090` alongside `admin.py` (which keeps iOS token/QR/sessions).
3. Retire `admin.py`'s prompt editor (superseded by agent `system_prompt`); keep the iOS endpoints.
4. Rollback is trivial: stop the UI process; `admin.py` remains unchanged.

## Open Questions

None — remaining choices (port, client style) are captured above and do not change the specs or task breakdown.
