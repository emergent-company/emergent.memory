# 2026-09-04 — Rename alfred → memory (product + repo)

## Goal

Rename the product from **Alfred** to **Memory** and move the repo to `emergent-company/memory.web-ui`. Executed from the `rename-alfred-to-memory` OpenSpec change (proposal/design/tasks).

## Outcome

Done. Pure mechanical rename, no behavior change. Repo created and pushed to `emergent-company/memory.web-ui` (private). Three commits: `a8d1585` (infra/deploy/tools/legacy-client), `ceff504` (comprehensive rename), `3b4493a` (mark spec tasks complete).

Scope covered:
- Go module `github.com/emergent-company/alfred` → `github.com/emergent-company/memory.web-ui`; binary `alfred` → `memory`.
- Env vars `ALFRED_*` → `MEMORY_*`; LiveKit defaults (`LIVEKIT_AGENT_NAME` `"alfred"`→`"memory"`, room-prefix `"alfred-"`→`"memory-"`, token name `"Alfred iOS Client"`→`"Memory iOS Client"`).
- Python bridge `alfred_bridge/` → `memory_bridge/`; `pyproject.toml` name → `memory`.
- UI branding: `window.MemoryApp`, `memory-*` CSS/JS tokens, `memoryLogo()`, monogram `A`→`M`, titles `"… — Memory"`.
- Docker/compose service `alfred`→`memory`, `deploy.sh`, `.env.example`, `.gitignore`, CI/lefthook/gitleaks, root `alfred` script → `memory`.
- Legacy client scripts `alfredctl`→`memoryctl`, `com.alfred.*.plist`→`com.memory.*.plist`.
- Docs: `docs/spec/`, `openspec/specs/`, `MEMORY_GUIDE.md`, `INFRASTRUCTURE.md`, `docs/tasks/`.
- iOS: display name `Alfred`→`Memory` + Swift types (`AlfredConfig`→`MemoryConfig`, `AlfredSessionController`→`MemorySessionController`, module `import Alfred`→`Memory`, dir `Alfred/`→`Memory/`, pbxproj/xcconfig/xcscheme).
- `.opencode` skills: `alfred-trace`→`memory-trace` + prose in `ios-debug`/`session-analyze`.
- Local dev systemd unit `alfred-dev.service` → `memory-dev.service`.

## Decisions

- Module path `github.com/emergent-company/memory.web-ui` (not `memory`) — matches the repo URL; user confirmed the repo name is `memory.web-ui`.
- Repo `emergent-company/memory.web-ui` (private), attempt create + push — created via `gh repo create`, re-pointed `origin`, pushed `master`.
- iOS scope = display name + Swift identifiers in-scope; **bundle id `com.mcj.alfred` + wake word `hey_alfred` deferred** (bundle-id change orphans installed app/keychain/provisioning; wake word needs ONNX retraining).
- `openspec/changes/**` (active + archive) left as-is — working/historical records; canonical doc rename scoped to `openspec/specs/` per task 8.3.
- Backend always "Emergent Memory" / `MEMORY_URL/TOKEN/PROJECT_ID` untouched; console's own knobs are `MEMORY_*` (D2).
- On-disk directory path literals (`/root/alfred`, `/Users/mcj/alfred`, `/opt/alfred`) kept — deployment topology, dirs not renamed.

## Changes

- `gateway/` (76 files) — module path, binary, env, LiveKit defaults, templ/go/js/css branding; `_templ.go` regenerated.
- `alfred_bridge/` → `memory_bridge/` (12 files) — imports/loggers `alfred.bridge`→`memory.bridge`; `pyproject.toml`.
- `Dockerfile`, `docker-compose.yml`, `docker-compose.ollama.yml`, `deploy.sh`, `.env.example`, `.gitignore`, `Taskfile.yml`, `.gitleaks.toml`, `.github/workflows/ci.yaml`, `lefthook.yml`, `alfred` → `memory`.
- `client/` — `alfredctl*`→`memoryctl*`, `com.alfred.*.plist`→`com.memory.*.plist`, `start/stop/status.sh`, `wakeword_client.py`, `diag_scores.py`.
- `client/ios/` — Swift type renames, `Alfred/`→`Memory/`, pbxproj/xcconfig/Info.plist/xcscheme/README/Localizable.xcstrings.
- `tools/` — `alfred-trace.sh`→`memory-trace.sh`, `ios-build-mac.sh`, `ios-debug-mac.sh` (`ALFRED_MAC_*`→`MEMORY_MAC_*`).
- `docs/spec/`, `openspec/specs/`, `MEMORY_GUIDE.md`, `INFRASTRUCTURE.md`, `docs/tasks/BACKLOG.md`.
- `.opencode/skills/alfred-trace`→`memory-trace`, `ios-debug`, `session-analyze`, `.opencode/command/session-analyze.md`.
- `openspec/changes/rename-alfred-to-memory/{proposal,design,tasks}.md` — updated module/repo/iOS-scope, tasks marked complete.

## Verification

- `cd gateway && go build ./...` — pass.
- `cd gateway && go test ./...` — pass (module `github.com/emergent-company/memory.web-ui`).
- `PATH="/root/go/bin:$PATH" templ generate ./...` — pass.
- `task build` — pass; produced `gateway/memory` (tailwind CSS clean).
- `tools/ios-build-mac.sh` (remote xcodebuild, simulator) — pass (EXIT 0).
- `docker compose config -q` — pass; `bash -n` on deploy/memory/client scripts — pass.
- `.venv/bin/python -m memory_bridge --help` — resolves.
- Smoke test (dev server): `Agents — Memory` title, `memory-monogram`, `apple-mobile-web-app-title "Memory"`; no `window.AlfredApp` / `alfred-*` classes. Remaining DOM "alfred" = persisted backend agent/project named "alfred" (data).

## Open questions / follow-ups

- Deferred rename items (see Tasks): iOS bundle id, wake word, prod systemd + on-disk paths, backend seed data.
- `MEMORY_GUIDE.md` had a "Memory … Memory" collision (product vs backend) from the mechanical sweep — fixed title + intro line to spell the backend "Emergent Memory"; a fuller D2 prose pass (backend always "Emergent Memory") is optional polish.

## Tasks

- [ios-bundle-id-rename](../tasks/ios-bundle-id-rename.md) — rename `com.mcj.alfred` bundle id (provisioning migration)
- [wake-word-retrain](../tasks/wake-word-retrain.md) — retrain `hey_alfred` → `hey_memory` ONNX
- [deploy-topology-rename](../tasks/deploy-topology-rename.md) — home2 systemd + `/opt/alfred` + checkout dirs
- [backend-seed-data-rename](../tasks/backend-seed-data-rename.md) — rename persisted agent/project "alfred" → "memory"
