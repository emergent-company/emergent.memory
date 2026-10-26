# apps/connector.mac — Mac connector

macOS menu-bar SwiftUI app (`MemoryConnector/`). Embeds the Go `memory-connector`
engine, built from `apps/connector.linux` by `MemoryConnector/Scripts/build-engine.sh`.
`project.yml` is XcodeGen input — the `.xcodeproj` is generated and never hand-edited.

All build and test tooling shells out to a **remote Mac** over ssh, because the
Xcode toolchain is expected there. There are **no repo defaults** — see
[Target config](#target-config).

## Layout

```
MemoryConnector/
  Sources/          app code (SwiftUI)
  Tests/            hosted unit tests (MemoryConnectorTests: XCTest + Swift Testing)
  UITests/          XCUITest smoke suite (MemoryConnectorUITests, 3 tests)
  Resources/        app resources
  Scripts/          build-engine.sh (embed the Go engine); NOT the host tooling
  project.yml       XcodeGen input -> MemoryConnector.xcodeproj (generated)
Scripts/            Mac HOST tooling (mac-build.sh, mac-doctor.sh, mac-uitest-agent.sh, ...)
.env.example        target config template
```

## Tests

Three suites. **None of them run in CI** — manual / on-demand by design. CI
does compile them (`connector.yml` → `mac-build`: xcodegen + `xcodebuild
build-for-testing`, ad-hoc signed), so a compile/type error anywhere in the app
or in the unit/UI test sources fails the connector gate; running the suites
below is still the only source of behavioural coverage.

| Suite | Covers | Command (task) | Host | ~Time | Green looks like |
|---|---|---|---|---|---|
| Hosted unit | Swift app logic (XCTest + Swift Testing) | `task connector-mac:test` (`mac-build.sh --test`) | Mac over ssh, no GUI | a few min | `** TEST SUCCEEDED **` |
| UI smoke | XCUITest: app launches + core UI renders, 3 tests | `task connector-mac:uitest:run` | Mac GUI session (via `gui/<uid>` LaunchAgent) | a few min (build + run) | exits 0, run-log shows 3 passed / 0 failed |
| Darwin engine e2e | real `memory-connector` binary vs in-process fake hub; asserts Apple tool registration | `task connector-mac:test:darwin` (or raw `go test -count=1 ./e2e/...` from `apps/connector.linux`) | Mac (darwin only) | seconds | `ok  .../apps/connector.linux/e2e` |

Raw equivalents (scripts, no task):

```bash
Scripts/mac-build.sh --test      # hosted unit tests
Scripts/mac-build.sh --uitest    # UI suite: BUILD ONLY (prints the GUI run command)
Scripts/mac-uitest-agent.sh run  # UI suite: build + run from ssh, exits with the suite's code
```

`--test` runs only `MemoryConnectorTests` because the UI target is in the
scheme's test action and XCUITest cannot run over a headless ssh session.

## Target config

Fed by `apps/connector.mac/Scripts/lib/mac-remote.sh`. No repo defaults — if no
host resolves, the scripts **fail fast** with an actionable message naming the
missing variable.

| Variable | Meaning |
|---|---|
| `MEMORY_MAC_HOST` | ssh target (`ssh` alias or `user@host`); empty = local-destination (tests / `--dry-run` only) |
| `MEMORY_MAC_HOST_NAME` | selector -> resolves `MEMORY_MAC_HOST_<NAME>` (uppercased, non-alnum -> `_`) |
| `MEMORY_MAC_HOST_<NAME>` | named-host ssh target (e.g. `MEMORY_MAC_HOST_MINI=you@my-mini.local`) |
| `MEMORY_MAC_PATH` | mirror dir on the Mac (`~`- or `/`-rooted, **no spaces**, NOT a git clone) |
| `MEMORY_MAC_PATH_<NAME>` | per-host mirror dir, overrides `MEMORY_MAC_PATH` for that named host |
| `MEMORY_MAC_ENV_FILE` | path of the env file to read (default `apps/connector.mac/.env`) — export it in the environment |

Config source precedence: an explicitly-set env var wins over everything; else
`MEMORY_MAC_HOST_NAME` selector; else `MEMORY_MAC_HOST` from the env file; else
fail. `.env` is **gitignored and per-checkout** (a fresh worktree has none), so
worktrees normally point one shared file at `~/.memory/mac.env`.

First time on a new machine:

```bash
# 1. on the Mac (one-time, idempotent): stable self-signed codesigning identity
apps/connector.mac/Scripts/mac-setup-signing.sh

# 2. from where you drive the build: set the target (or drop these in ~/.memory/mac.env)
export MEMORY_MAC_HOST=you@my-mini.local
export MEMORY_MAC_PATH=~/code/mac-connector-mirror    # dedicated rsync mirror, NOT a git clone

# 3. sanity, then build/test
task connector-mac:doctor
task connector-mac:build
task connector-mac:test

# 4. UI suite — one-time install, then run (first run needs one on-screen TCC approval at the Mac)
task connector-mac:uitest:install
task connector-mac:uitest:run
```

## How the UI suite actually runs

XCUITest needs a **WindowServer**, which exists only inside a user's Aqua (GUI)
login session. A plain ssh session is headless, so `xcodebuild test` hangs/fails
there. `mac-build.sh --uitest` therefore only *builds* the app + UI-test bundle.

`mac-uitest-agent.sh` bridges the gap: it bootstraps a LaunchAgent
(`com.emergent.memory.connector.uitest`) into the **`gui/<uid>`** domain, so the
job runs inside the Aqua session. `run` kickstarts it and polls the status file
(`DONE rc=<n>`) the on-Mac runner writes, then **exits with that code**. The
runner reuses the `build/DerivedData` the build produced, so no rebuild.

- `run [--no-build] [--timeout <secs>]` — build (unless `--no-build`), kickstart, wait; default timeout 900s.
- The **first** `run` triggers a one-time on-screen Automation/Accessibility (TCC)
  approval for the runner — approve it once at the Mac. Observed: a rebuild with a
  fresh ad-hoc signature re-runs **without** re-prompting; `mac-setup-signing.sh`
  keeps a stable identity if a host ever re-prompts.

## Gotchas

- The Mac mirror (`MEMORY_MAC_PATH`) must **not** be a git checkout: `mac-build.sh`
  writes/removes `go.work` at the mirror root and rsyncs with scoped `--delete`.
  A `.git` at the root/subtree aborts the sync unless `ALLOW_*_INTO_GIT=1`.
- `MEMORY_MAC_PATH` must be `~`- or `/`-rooted with **no spaces**.
- `xcodegen` and `go` resolve from `/opt/homebrew/bin` on the Mac — the scripts add
  it to `PATH` over ssh (non-interactive shells don't source brew shellenv).
- `--test` runs only `MemoryConnectorTests`; the UI target is in the scheme's test
  action and can't run headless.
- Signing: builds sign ad-hoc, then re-sign with a dedicated self-signed identity
  if `mac-setup-signing.sh` was run (its own keychain, works over ssh). Without it,
  fallback is ad-hoc. Real Apple Development identity needs `mac-sign.sh` from a
  GUI `Terminal.app` (login-keychain key is unreachable over ssh).

## Pointers

- `Scripts/README.md` — full human reference for the host tooling (read first).
- `apps/connector.linux/e2e/README.md` — darwin e2e variant details.
- `Scripts/README.md` § "Darwin engine e2e" — what the darwin suite covers.
