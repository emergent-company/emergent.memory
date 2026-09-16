# Tasks — rename alfred → memory

Pure rename, no behavior change. Verification is build/grep-based (no new unit tests: the
existing suite must stay green but nothing new is added). Each task ends with its gate.

## 1. Module path

- [x] 1.1 Rewrite `gateway/go.mod` module `github.com/emergent-company/alfred` → `github.com/emergent-company/memory.web-ui` and update every import (`github.com/emergent-company/alfred/webui` → `.../memory.web-ui/webui` in `gateway/main.go`, `gateway/ui.templ`, `gateway/chat.templ`, and any others). Verify: `cd gateway && go build ./...` passes; `grep -rn "emergent-company/alfred" gateway/` returns no hits.

## 2. Binary name

- [x] 2.1 Rename the build output in `gateway/Taskfile.yml` (`go build -o alfred .` → `-o memory`, `./alfred` → `./memory`), `gateway/.air.toml` (`entrypoint = ["./alfred"]` → `["./memory"]`), `Dockerfile` (`-o /out/alfred` → `/out/memory`, `COPY ... alfred` → `memory`, `CMD ["alfred"]` → `["memory"]`). Verify: `task build` produces `gateway/memory`; `grep -rn "\balfred\b" gateway/Taskfile.yml gateway/.air.toml Dockerfile` returns no hits (binary-name references only).
- [x] 2.2 Update the checked-in `alfred` binary artifact (root `alfred` executable) and `.gitignore` entry `/gateway/alfred` → `/gateway/memory`. Verify: `ls gateway/memory` exists after build; `.gitignore` no longer names `alfred`.

## 3. Env vars

- [x] 3.1 `gateway/config.go:35` — `envOr("ALFRED_PORT", "8095")` → `envOr("MEMORY_PORT", "8095")`. Verify: `go build ./...`; `grep -rn "ALFRED_" gateway/` returns no hits.
- [x] 3.2 `docker-compose.yml:27` — `ALFRED_PORT: 8080` → `MEMORY_PORT: 8080`. Verify: `docker compose config` parses; `grep -rn "ALFRED_" docker-compose*.yml` clean.
- [x] 3.3 `tests/e2e/run-e2e.sh` — `ALFRED_PORT=8095` → `MEMORY_PORT=8095`. Verify: `grep -rn "ALFRED_" tests/` clean; script still executes the gateway (manual smoke).
- [x] 3.4 Legacy macOS plists `client/com.alfred.cloud.plist` + `client/com.alfred.realtime.plist` — `ALFRED_ENV` → `MEMORY_ENV` (and label/filename `com.alfred.*` → `com.memory.*`, `__HOME__/alfred` path noted). Verify: `grep -rn "ALFRED_" client/` clean.
- [x] 3.5 Build tooling `tools/ios-build-mac.sh` + `tools/ios-debug-mac.sh` — `ALFRED_MAC_HOST`/`ALFRED_MAC_PATH` → `MEMORY_MAC_HOST`/`MEMORY_MAC_PATH`. Verify: `grep -rn "ALFRED_" tools/` clean.

## 4. LiveKit defaults

- [x] 4.1 `gateway/config.go:51` — `LIVEKIT_AGENT_NAME` default `"alfred"` → `"memory"`. Verify: build; `grep -rn '"alfred"' gateway/config.go` clean.
- [x] 4.2 `gateway/token.go` — JWT name `"Alfred iOS Client"` → `"Memory iOS Client"` (line 54); legacy room-prefix fallback `"alfred-"` → `"memory-"` (line 104) and its comment. Verify: `grep -rn "alfred" gateway/token.go` clean; `go test ./...` (token tests) green.
- [x] 4.3 `gateway/memory.go:600` — client info `"alfred-gateway"` → `"memory-gateway"`. Verify: `grep -rn "alfred" gateway/memory.go` clean.
- [x] 4.4 `.env.example` — `LIVEKIT_AGENT_NAME=alfred-google-rt` default comment/example → `memory-google-rt`; room-prefix comment `alfred-` → `memory-`. Verify: `grep -rni "alfred" .env.example` clean (except `hey_alfred` wake-word lines, explicitly deferred).

## 5. Bridge package

- [x] 5.1 Rename `alfred_bridge/` → `memory_bridge/`; update `gateway/config.go:41` default `-m alfred_bridge start` → `-m memory_bridge start`; `Dockerfile` `BRIDGE_ARGS="-m alfred_bridge"` → `-m memory_bridge`; `deploy.sh` tar path `alfred_bridge` → `memory_bridge`; `alfred_bridge/__init__.py`/`__main__.py`/`worker.py` docstring + logger `alfred.bridge` → `memory.bridge`. Verify: `grep -rn "alfred_bridge" . --include="*.go" --include="*.py" --include="Dockerfile" --include="*.sh"` clean; `python -m memory_bridge --help` resolves.

## 6. Docker / deploy / systemd / plists

- [x] 6.1 `docker-compose.yml` — service `alfred:` → `memory:` and top-of-file comments; `docker-compose.ollama.yml` comment "for Alfred" → "for Memory". Verify: `docker compose config` valid; `grep -rni "alfred" docker-compose*.yml` clean.
- [x] 6.2 `deploy.sh` — comments, `MAC_IOS_DIR=/Users/mcj/alfred/client/ios` and remote `tar -C /root/alfred` / `cd /root/alfred` paths → `memory` (or leave as flagged topology; see Open Questions). Verify: `grep -rni "alfred" deploy.sh` clean; shellcheck/`bash -n deploy.sh` passes.
- [x] 6.3 systemd unit references (out-of-repo on `home2`): note the `alfred.service` → `memory.service` rename and `EnvironmentFile=/opt/alfred/.env` → `/opt/memory/.env` as an external action item; no repo file to edit. Verify: documented in change note; no in-repo `*.service` hits remain.

## 7. UI branding

- [x] 7.1 Rename JS/CSS/globals in templ: `window.AlfredApp` → `window.MemoryApp`, `window.AlfredSidepanel` → `window.MemorySidepanel`, `alfredLogo()` → `memoryLogo()`, and `alfred-*` tokens/classes (`alfred-monogram`, `alfred-toast`, `alfred-suggestion`, `alfred-rise`, `alfred-md`, `alfred.sidepanel.v1`) → `memory-*`. Files: `gateway/ui.templ`, `gateway/skills.templ`, `gateway/chat.templ`, `gateway/agent.templ`, `gateway/project_settings.templ`, `gateway/toast.templ`, `gateway/ui.go`, `gateway/agent.go`, `gateway/objects.go`, `gateway/schema.go`, `gateway/blueprints_handlers.go`, `gateway/settings_handlers.go`, `gateway/approvals_handlers.go`, `gateway/migrations.go`.
- [x] 7.2 Replace user-visible brand strings "Alfred" → "Memory" in page titles (`"— Alfred"` → `"— Memory"`), `apple-mobile-web-app-title`, sidebar/navbar, monogram `A` → `M`, and body copy ("Chat with Alfred", "Alfred answers…", "You are Alfred, the household agent"). Verify: `templ generate ./...` then `go build ./...`; `grep -rni "alfred" gateway/*.templ gateway/*.go | grep -v _templ.go` returns no non-wake-word hits.
- [x] 7.3 Verify no `alfred` remains in generated `_templ.go` (regenerated in 7.2) or `gateway/webui/` static JS/CSS. Verify: `grep -rni "alfred" gateway/` clean (excl. deferred `hey_alfred`).

## 8. Docs + specs

- [x] 8.1 Update `docs/spec/README.md` ("Alfred — System Specification" → "Memory — System Specification") and the spec files that say "Alfred" as the product name (`02-memory-backend.md`, `08-deployment.md`, `10-api-contracts.md`, `11-reuse-from-diane.md`, `13-roadmap.md`, `14-assistant-agent.md`). Keep backend references as "Emergent Memory". Verify: `grep -rni "alfred" docs/spec/` clean (except intentional historical/seed-agent IDs).
- [x] 8.2 Update `MEMORY_GUIDE.md` + `INFRASTRUCTURE.md` product references. Verify: `grep -rni "alfred" MEMORY_GUIDE.md INFRASTRUCTURE.md` clean.
- [x] 8.3 Update `openspec/specs/**/spec.md` prose ("Alfred" → "Memory" where it names the product). Do **not** touch `openspec/changes/archive/`. Verify: `grep -rni "alfred" openspec/specs/` clean.

## 9. iOS (optional — confirm scope first)

- [x] 9.1 If in scope: `client/ios/VoiceAgent/VoiceAgent.xcconfig` `PRODUCT_NAME = Alfred` → `Memory`; `Info.plist` `CFBundleDisplayName` "Alfred" → "Memory" + NS usage strings. Leave `PRODUCT_BUNDLE_IDENTIFIER = com.mcj.alfred` (deferred). Verify: `tools/ios-build-mac.sh` builds.
- [x] 9.2 Rename Swift type identifiers (`AlfredConfig` → `MemoryConfig`, `AlfredApp` → `MemoryApp`, `Alfred/*.swift` files) and their references; leave bundle id + wake word. Verify: xcodebuild (simulator) succeeds; `grep -rni "alfred" client/ios/` clean except deferred `com.mcj.alfred`.

## 10. Final verify

- [x] 10.1 Full compile + test: `cd gateway && go build ./... && go test ./...` and `templ generate ./...` green. Verify: no compile errors, all tests pass.
- [x] 10.2 Grep gate: `grep -rniE "alfred" . --exclude-dir=.git --exclude-dir=.venv --exclude-dir=node_modules --exclude-dir=.slim` returns **only** the intentional remainders — `openspec/changes/archive/**`, the wake-word lines (`hey_alfred`, `.onnx`), and the deferred iOS bundle id `com.mcj.alfred`. Verify: enumerate the remaining hits and confirm each is on the allow-list.
- [x] 10.3 `task dev` smoke test (local, port 8095) renders the Memory shell (title, sidebar, monogram) with no "Alfred" in the DOM. Verify: browser check; no `window.AlfredApp` console errors.
