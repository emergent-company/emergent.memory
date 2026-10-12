## 1. Lane A — Mac app UI smoke (XCUITest)

- [x] 1.1 Add `MemoryConnectorUITests` (`bundle.ui-testing`, `TEST_TARGET_NAME: MemoryConnector`) target in `apps/connector.mac/MemoryConnector/project.yml`; add it to the `MemoryConnector` scheme test action
- [x] 1.2 Add stable `accessibilityIdentifier`s in `Sources/`: sidebar rows (`sidebar.<page>`), page roots (`page.<name>`), toolbar controls (`toolbar.project`, `toolbar.account`), status footer (`status.footer`)
- [x] 1.3 Add a `UITestMode` launch-argument hook and wire isolation into `AppEnvironment`/`AppDelegate`/`EngineManager` (temp Application Support dir + isolated UserDefaults suite, skip legacy import, no engine start, no status polling)
- [x] 1.4 Add `UITests/ConnectorUISmokeTests.swift`: main window opens; each of the 7 sidebar pages renders; toolbar + footer present (isolated profile + `-uiTestMode`)
- [x] 1.5 Add an `--uitest` mode to `apps/connector.mac/Scripts/mac-build.sh` (GUI-session run; documented that the ssh path cannot run XCUITest)

## 2. Lane B — Darwin engine↔hub e2e (Go)

- [x] 2.1 Add a darwin runtime-gated e2e (`e2e/mac_daemon_e2e_test.go`) reusing the existing `fakeHub`/`startDaemon`/`runStatusJSON` helpers
- [x] 2.2 Assert `register` frame + Apple tool ids present in the tool list; assert `status --json` is connected (`instance_id` match, `hub_state == "connected"`, `schema_version == 1`)
- [x] 2.3 Skip under `-short` and on non-darwin hosts; keep the Linux test unchanged
- [x] 2.4 Update `apps/connector.linux/e2e/README.md` to describe the darwin variant

## 3. Spec + docs

- [x] 3.1 OpenSpec change `add-mac-connector-e2e` (proposal, design, tasks, `specs/mac-connector-e2e/spec.md`)
- [x] 3.2 `openspec validate add-mac-connector-e2e`
- [x] 3.3 Update `apps/web-ui/docs/tasks/mac-connector-e2e-coverage.md` (proposed → in progress) and note remaining manual smokes
- [x] 3.4 Document how to run each suite manually in `apps/connector.mac/Scripts/README.md` (Tests section)

## 4. Verification

- [x] 4.1 `cd apps/connector.linux && go build ./... && go vet ./...` — PASS
- [x] 4.2 `cd apps/connector.linux && go test -count=1 ./internal/relay/...` — PASS
- [x] 4.3 Darwin path type-checks: `GOOS=darwin go vet ./e2e/... && GOOS=darwin go build ./...` — PASS; `go test -count=1 ./e2e/...` — Linux tests PASS, darwin test SKIP (expected); `-short` skips all
- [x] 4.4 **Mac runner (`mcj@tool`, macOS 27 / Xcode 27)**: `mac-build.sh --test` → `** TEST SUCCEEDED **`, 424 tests, 0 failures; the `MemoryConnectorUITests` target compiles cleanly
- [x] 4.5 **Mac runner**: darwin engine e2e run for real — `go test -count=1 ./e2e/...` on macOS → `TestDarwinDaemonRegisterAndStatus` **PASS**; Linux tests skip on darwin (expected)
- [ ] 4.6 **Mac runner (GUI session)**: actually RUN the UI suite — `xcodebuild test -only-testing:MemoryConnectorUITests`. It compiles (4.4) but XCUITest needs a WindowServer session + stable signing, so it was not executed over ssh
- [ ] 4.7 **Mac runner**: `Scripts/lint-swift.sh` (SwiftLint) — not run; Go side linted (`gofmt -l`, `go vet`), `shellcheck` clean on `mac-build.sh`
- [x] 4.8 Scoped lint on the authoring host: `gofmt -l ./e2e/` clean, `go vet ./...` clean, `shellcheck mac-build.sh` clean

> **UI suite not yet EXECUTED.** It compiles cleanly on the Mac (4.4), but running XCUITest needs a macOS GUI session + a stable signing identity (see `apps/connector.mac/Scripts/README.md` → Tests). The darwin engine e2e and the hosted unit tests are verified on macOS. CI wiring is **deliberately deferred** (decision: document the manual runs instead).
