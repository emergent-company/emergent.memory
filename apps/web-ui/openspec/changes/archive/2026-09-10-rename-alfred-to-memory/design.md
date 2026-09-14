## Context

Alfred is becoming the **Memory web console** — the browser + iOS front door for the
Emergent Memory backend. "alfred" appears across the repo as: the Go module path
(`github.com/emergent-company/alfred` in `gateway/go.mod:1`), the binary name, the
`ALFRED_PORT` env var, LiveKit default agent/room identifiers (`alfred`, `alfred-`), the
Python voice bridge package `alfred_bridge`, UI branding (`window.AlfredApp`, `alfred-*`
CSS classes, the "Alfred" sidebar/navbar/monogram), Docker/compose service + image
references, deploy scripts, iOS app display name/bundle id, and prose throughout
`docs/spec/`, `MEMORY_GUIDE.md`, `INFRASTRUCTURE.md`, and `openspec/specs/`.

This is a pure rename — no behavior change (`skip_specs: true`). The goal is a mechanical,
grep-verifiable sweep that leaves `grep -ri alfred` returning no code/config references,
without altering runtime behavior, API contracts, or data.

## Goals / Non-Goals

**Goals:**

- Rename the Go module to `github.com/emergent-company/memory` and the binary to `memory`.
- Rename `ALFRED_*` env vars and LiveKit default agent/room identifiers.
- Rename Docker/compose service + image, deploy script paths/labels, and systemd/plist references.
- Rename UI branding (titles, `window.*App` globals, `alfred-*` CSS classes, logo fn).
- Update living docs and `openspec/specs/` prose.
- Leave `grep -ri alfred` clean across code + config (excluding intentional historical records).

**Non-Goals:**

- No behavior change, no new specs (rename only — hence `skip_specs: true`).
- **Naming collision is explicitly resolved below**: "memory" (console) vs "Emergent Memory"
  (backend) must stay crisply distinct in code, env, and docs.
- **Wake word** (`hey_alfred`, `models/hey_alfred.onnx`) — retraining an ONNX wake-word model
  is a product decision, not a rename. Out of scope (flagged in Open Questions).
- **iOS bundle id** (`com.mcj.alfred`) — changing it orphans the installed app, keychain
  entries, and provisioning profile. Out of scope (flagged in Open Questions).
- **Historical archived changes** under `openspec/changes/archive/` are a permanent record and
  are not rewritten; they may still contain "alfred".
- **Remote on-disk paths** (`/root/alfred` on `home2`, `/Users/mcj/alfred` on the Mac) are
  deployment topology, not product naming. Directory renames are out of scope; only the
  identifiers/labels that reference the product are changed (see Risks).

## Decisions

### D1 — Rename the module and binary to `memory`

- **Decision:** `gateway/go.mod` module becomes `github.com/emergent-company/memory.web-ui`
  (repo `emergent-company/memory.web-ui`); the binary
  is built as `memory` (`go build -o memory .`), and all import paths (`.../memory.web-ui/webui`) follow.
- **Rationale:** the proposal mandates matching the binary and module to the served domain. The
  import path is the root of the module identity; renaming it first unblocks everything else.
- **Rejected alternative:** keep the module path but rename only the binary. Leaves a dangling
  `alfred` import path and half-renamed state; `grep -ri alfred` stays dirty.

### D2 — Crisp naming distinction: "memory" (console) vs "Emergent Memory" (backend)

- **Decision:** the product/binary/UI is **"Memory"** (this repo); the backend it talks to is
  **"Emergent Memory"** / `emergent.memory` (a separate service). Backend connection env vars —
  `MEMORY_URL`, `MEMORY_TOKEN`, `MEMORY_PROJECT_ID` — are **unchanged** and continue to name the
  backend. Console runtime env var `ALFRED_PORT` becomes `MEMORY_PORT`.
- **Rationale:** the console is being deployed at `memory.emergent-company.ai`, the same host the
  backend `MEMORY_URL` already points to — a real collision risk. The distinction lives in naming:
  the console's own knobs are `MEMORY_*`, while the backend is always spelled "Emergent Memory" /
  `emergent.memory` in docs and "backend" in comments, so a reader never confuses `MEMORY_PORT`
  (console listen port) with `MEMORY_URL` (backend endpoint).
- **Rejected alternative:** rename backend env vars to `MEMORY_BACKEND_URL` etc. to disambiguate.
  That is behavior/config churn beyond a rename and touches the backend contract for no rename
  benefit.

### D3 — `ALFRED_PORT` → `MEMORY_PORT`: hard break, no backward-compat shim

- **Decision:** `ALFRED_PORT` → `MEMORY_PORT` (and tool-script `ALFRED_MAC_HOST`/`ALFRED_MAC_PATH`
  → `MEMORY_MAC_HOST`/`MEMORY_MAC_PATH`; legacy plist `ALFRED_ENV` → `MEMORY_ENV`). No
  fallback-to-old-var shim in `LoadConfig`.
- **Rationale:** single-owner, internal deployment; `deploy.sh` ships `.env` + compose atomically
  in the same commit. A shim leaves dead code and a second source of truth for the same knob.
- **Rejected alternative:** read `ALFRED_PORT` as fallback for one release. Unnecessary
  indirection for a controlled, tailnet-only deploy; the proposal already declares this BREAKING.

### D4 — LiveKit defaults: `alfred` → `memory`

- **Decision:** `LIVEKIT_AGENT_NAME` default `"alfred"` → `"memory"` (`gateway/config.go:51`); the
  legacy room-prefix fallback `"alfred-"` → `"memory-"` (`gateway/token.go:104`); the JWT display
  name `"Alfred iOS Client"` → `"Memory iOS Client"` (`gateway/token.go:54`); `"alfred-gateway"`
  client info → `"memory-gateway"` (`gateway/memory.go:600`); the `project_settings.templ` agent
  placeholder `"alfred"` → `"memory"`. `.env.example` defaults (`alfred-google-rt`,
  room-prefix comment) follow.
- **Rationale:** matches the product name; the agent/room identifiers are user-visible in the
  LiveKit dashboard and iOS dispatch.
- **Rejected alternative:** keep `alfred-google-rt` as a seed agent name. That is a *data* value
  already persisted in memory (an agent definition), not code; renaming live seed agents would be
  a data migration, out of scope. Only *defaults* and *fallbacks* in code change.

### D5 — Rename the Python bridge package `alfred_bridge` → `memory_bridge`

- **Decision:** rename the `alfred_bridge/` directory and its `-m alfred_bridge` default
  (`gateway/config.go:41`, `Dockerfile` `BRIDGE_ARGS`, `deploy.sh` tar path, logger name
  `alfred.bridge` → `memory.bridge`).
- **Rationale:** the module name is user-visible in process listings (`python -m alfred_bridge
  start`) and leaves a `grep -ri alfred` hit if skipped.
- **Rejected alternative:** leave `alfred_bridge` as an opaque internal package name. Acceptable
  but leaves the rename incomplete; low cost to fix now.

### D6 — UI/JS/CSS identifiers

- **Decision:** rename `window.AlfredApp` → `window.MemoryApp`, `window.AlfredSidepanel` →
  `window.MemorySidepanel`, `alfredLogo()` → `memoryLogo()`, and the `alfred-*` CSS
  classes/keys (`alfred-monogram`, `alfred-toast`, `alfred-suggestion`, `alfred-rise`,
  `alfred-md`, `alfred.sidepanel.v1`) → `memory-*`. Page titles `"— Alfred"` → `"— Memory"`;
  brand strings "Alfred" → "Memory" in templ (`ui.templ`, `chat.templ`, `agent.templ`,
  `skills.templ`, `project_settings.templ`).
- **Rationale:** user-visible branding; a clean rename must not leave "Alfred" in the rendered UI.
- **Rejected alternative:** keep `alfred-*` as internal-only CSS/JS tokens. Saves churn but leaves
  the console rendering/branding half-renamed and `grep -ri alfred` dirty in `_templ.go`.

### D7 — Rename order (dependency-ordered)

- **Decision:** module path → binary name (build/test green) → env vars + LiveKit defaults
  (`config.go`/`token.go`/`memory.go`) → Docker/compose/deploy/systemd/plists → UI branding +
  bridge package → docs + `openspec/specs` → iOS display name (optional last).
- **Rationale:** the module import path is load-bearing; every later step recompiles against it.
  Build/grep gates after each group localize breakage.
- **Rejected alternative:** big-bang single commit. A single sweeping commit is harder to bisect;
  grouped, dependency-ordered steps give a green build at each gate.

## Risks / Trade-offs

- **[Risk] Deploy scripts break** — `deploy.sh` hardcodes `/root/alfred` (remote), the
  `/Users/mcj/alfred` Mac iOS dir, and the `alfred` binary/tar paths; `tools/ios-*-mac.sh`
  hardcode `Alfred.app`, `com.mcj.alfred`, and `--exclude alfred`.
  → **Mitigation:** update `deploy.sh`/`tools` in the Docker/deploy group, then run a
  `--dry-run`-style rsync/tar review before any real deploy.

- **[Risk] Stale docs / spec prose** — `docs/spec/`, `MEMORY_GUIDE.md`, `INFRASTRUCTURE.md`,
  and `openspec/specs/` all reference "alfred".
  → **Mitigation:** a final `grep -rniE "alfred"` gate (excluding `openspec/changes/archive/`
  and the wake-word `hey_alfred` entries) must return zero code/config/doc hits; the change is
  not done until it does.

- **[Risk] Env-var churn breaks running deploy** — `ALFRED_PORT`/compose service name change
  mid-flight.
  → **Mitigation:** flag-day deployment — ship `.env`, `docker-compose.yml`, and the binary in
  one `deploy.sh` run; document the BREAKING var rename in the change/release note.

- **[Risk] Generated `_templ.go` staleness** — templ sources rename but generated files drift.
  → **Mitigation:** `templ generate ./...` + `go build ./...` after every templ/JS/CSS edit;
  `_templ.go` files are regenerated, never hand-edited.

- **[Risk] Naming collision "memory" vs "Emergent Memory"** — console and backend share the
  `memory.emergent-company.ai` hostname and `MEMORY_*` prefix.
  → **Mitigation:** D2 — the console's own vars (`MEMORY_PORT`) are documented as console
  runtime; backend stays "Emergent Memory" / `MEMORY_URL/TOKEN/PROJECT_ID`. Docs spell out the
  distinction.

- **[Risk] LiveKit default change breaks running voice sessions** — existing rooms use the
  `alfred-` prefix / `alfred` agent.
  → **Mitigation:** default-change only (D4); running sessions use already-minted tokens and are
  unaffected until next dispatch. Note the default change in the release note.

## Migration Plan

1. Rename module + binary (D1); `go build ./...` and `go test ./...` green.
2. Rename env vars + LiveKit defaults + bridge package (D3/D4/D5); build + unit tests green.
3. Rename Docker/compose/deploy/systemd/plist references (D5, deploy surfaces); compose config
   validates, `deploy.sh` dry-run reviewed.
4. Rename UI branding + regenerate templ (D6); `templ generate` + build + `task dev` smoke test.
5. Update docs + `openspec/specs` prose; final `grep -rniE "alfred"` gate (excl. archive + wake
   word) returns zero.
6. (Optional, last) iOS display name / Swift identifiers (D8 scope); bundle id + wake word deferred.

**Rollback:** `git revert` of the change restores the prior module/binary/env/branding wholesale —
a rename has no data migration, so rollback is a clean reversion with no schema or data
consequences. Backend env vars (`MEMORY_URL/TOKEN/PROJECT_ID`) are untouched throughout.

## Open Questions

- **iOS app-name scope** — **RESOLVED 2026-09-04**: `PRODUCT_NAME`/`CFBundleDisplayName` ("Alfred" →
  "Memory") and Swift type identifiers (`AlfredConfig`, `AlfredApp`, `Alfred/*.swift`) are in scope.
  Bundle id `com.mcj.alfred` and wake word `hey_alfred` are **deferred** (bundle-id change orphans the
  installed app/keychain/provisioning; wake word needs ONNX retraining).
- **`hey_alfred` wake-word model** — out of scope here (D-non-goal); needs a separate
  model-retraining change.
- **Remote checkout dirs** (`/root/alfred`, `/Users/mcj/alfred`) — should the on-disk directories
  themselves be renamed as part of deploy hardening? Deferred (path ≠ product name), but worth
  confirming to avoid future confusion.
