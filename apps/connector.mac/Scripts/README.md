# Mac connector build tooling

Scripts in this directory build the macOS connector (a Swift menu-bar app) on a
remote Mac — this Linux server has no Xcode. Everything derives from one shared
config: `lib/mac-remote.sh`, fed by the environment or the gitignored
`apps/connector.mac/.env`.

## Scripts

| Script | What it does |
|---|---|
| `mac-build.sh` | rsync `apps/connector.mac/` → `<MEMORY_MAC_PATH>/client/macos` plus the embedded engine's Go sources, XcodeGen + `xcodebuild`, then sign |
| `mac-doctor.sh` | `--check`: verify the Mac is reachable and usable (read-only) |
| `mac-sign.sh` | sign the installed `MemoryConnector.app` with a stable Apple Development identity (GUI only) |
| `mac-setup-signing.sh` | one-time (idempotent) setup of a dedicated self-signed codesigning identity |

The iOS build/debug scripts (`apps/web-ui/tools/ios-build-mac.sh`,
`ios-debug-mac.sh`, `memory-trace.sh`) and `apps/web-ui/deploy.sh` stay in
`apps/web-ui/tools/` but take their Mac target from the same shared library and
config.

## Configuration

Copy what you need into `apps/connector.mac/.env` (gitignored) or export it. An
explicitly-set environment variable always wins over `.env`. The full template is
`apps/connector.mac/.env.example`.

| Variable | Required | Meaning |
|---|---|---|
| `MEMORY_MAC_HOST` | yes¹ | SSH host of the Mac (`ssh` alias, or `user@host`). An explicitly empty value = local-destination mode (tests / `--dry-run` only). |
| `MEMORY_MAC_HOST_NAME` | no | Selector for a named host. Resolves `MEMORY_MAC_HOST_<NAME>` where `<NAME>` is the selector uppercased with non-alphanumerics → `_`. |
| `MEMORY_MAC_HOST_<NAME>` | no | Named-host SSH target (e.g. `MEMORY_MAC_HOST_TOOL=mcj@tool`). |
| `MEMORY_MAC_PATH` | yes² | Project directory on the Mac, absolute or `~`-rooted, no spaces. |
| `MEMORY_MAC_PATH_<NAME>` | no | Per-host project dir (same `<NAME>` normalization); overrides `MEMORY_MAC_PATH` when that named host is selected. |
| `MEMORY_MAC_SUBTREE` | no | Override the per-script remote subtree (mac connector default `client/macos`). |
| `MEMORY_MAC_IOS_SUBDIR` | no | Back-compat alias for the iOS subtree (default `client/ios`). |
| `ALLOW_SYNC_INTO_GIT` / `ALLOW_DELETE_IN_GIT` | no | Danger overrides for syncing into a real git clone. |
| `MAC_SSH_OPTS` | no | Extra `ssh` options (used by `mac-doctor.sh` to fail fast). |

¹ `MEMORY_MAC_HOST` is only required when you are **not** using named hosts.
² `MEMORY_MAC_PATH` is only required when the selected host has no `MEMORY_MAC_PATH_<NAME>`.

### Named hosts

If you have multiple Mac build machines, define one `MEMORY_MAC_HOST_<NAME>` per
host and select one with `MEMORY_MAC_HOST_NAME`:

```sh
MEMORY_MAC_HOST_TOOL=mcj@tool
MEMORY_MAC_HOST_MINI=mcj-mini
MEMORY_MAC_PATH_TOOL=~/code/emergent.memory
MEMORY_MAC_PATH_MINI=~/code/emergent.memory.mini
MEMORY_MAC_HOST_NAME=tool      # pick "tool" (or "mini")
```

`<NAME>` is normalized deterministically — uppercased with every non-alphanumeric
character replaced by `_` (so `tool`, `mini-2`, and `Mac Pro` map to
`MEMORY_MAC_HOST_TOOL`, `MEMORY_MAC_HOST_MINI_2`, and `MEMORY_MAC_HOST_MAC_PRO`).
Host precedence: an explicitly-set `MEMORY_MAC_HOST` wins; else the selector
resolves `MEMORY_MAC_HOST_<NAME>` (error if missing); else a `MEMORY_MAC_HOST`
from `.env`; else fail fast.

This repo's own values live in the gitignored `apps/connector.mac/.env`:
`MEMORY_MAC_HOST=mcj-mini`, `MEMORY_MAC_PATH=~/code/emergent.memory`.

## Expected Mac layout

`MEMORY_MAC_PATH` is the rsync'd build checkout root (it is **not** a git clone):

```
<MEMORY_MAC_PATH>/
  client/ios/            # apps/ios/            (owned by apps/web-ui/tools/ios-build-mac.sh)
  client/macos/          # apps/connector.mac/  (owned by mac-build.sh)
  apps/connector.linux/  # embedded engine source   (synced by mac-build.sh)
  apps/server/pkg/sdk/   # engine's sibling module  (synced by mac-build.sh)
  go.work                # minimal workspace mapping the two modules (written by mac-build.sh)
  build/                 # DerivedData, archives (untouched by --delete)
```

The engine embedded by `MemoryConnector/Scripts/build-engine.sh` is compiled from
`<MEMORY_MAC_PATH>/apps/connector.linux`, which imports `apps/server/pkg/sdk`.
`mac-build.sh` mirrors both and writes the `go.work`, so the engine resolves its
sibling module from the synced tree instead of the module cache — otherwise the
app can embed a stale engine.

The guarded rsync mirrors only those dedicated subtrees, so `--delete` can never
remove anything outside `client/macos`, `apps/connector.linux`, or
`apps/server/pkg/sdk`.

## Signing

Builds over ssh cannot use the login keychain's signing key, so `mac-build.sh`
signs ad-hoc during the build, then re-signs with a dedicated self-signed
identity for a stable code identity across rebuilds. Run
`apps/connector.mac/Scripts/mac-setup-signing.sh` **once** on the Mac to create
that identity (its own keychain, works non-interactively including over ssh).
Without it, builds fall back to ad-hoc signing.

For a real Apple Development identity, use `mac-sign.sh` from a GUI
`Terminal.app` on the Mac (the login keychain's key is unreachable over ssh).

## Tests

The Mac connector has three suites. **None are wired into CI yet** — run them
manually on the Mac as below.

### Hosted unit tests (`MemoryConnectorTests`)

Runs over ssh like a build; no GUI needed:

```sh
apps/connector.mac/Scripts/mac-build.sh --test
```

### UI smoke suite (`MemoryConnectorUITests`, XCUITest)

XCUITest needs a **GUI/WindowServer session** and a **stable signing identity**
(ad-hoc signing resets Automation/TCC grants), so it cannot run over the
headless ssh path. `--uitest` only *builds* the app + UI-test bundle and prints
the run command:

```sh
apps/connector.mac/Scripts/mac-build.sh --uitest
```

Then, **on the Mac in a GUI session** (locally or via Screen Sharing):

```sh
cd <MEMORY_MAC_PATH>/client/macos/MemoryConnector
xcodebuild test -project MemoryConnector.xcodeproj -scheme MemoryConnector \
  -destination 'platform=macOS' -derivedDataPath build/DerivedData \
  -only-testing:MemoryConnectorUITests
```

Run `mac-setup-signing.sh` **once** on the Mac first for a stable identity.

### Darwin engine e2e (`apps/connector.linux/e2e`)

The engine↔hub hermetic test (real `memory-connector` binary vs an in-process
fake relay hub). On the Mac, run it from the synced engine tree (or the repo
checkout):

```sh
cd <MEMORY_MAC_PATH>/apps/connector.linux
go test -count=1 ./e2e/...
```

It is skipped under `-short` and, for the darwin variant, on non-darwin hosts.
See `apps/connector.linux/e2e/README.md` for what each variant asserts.

## Doctor

```sh
apps/connector.mac/Scripts/mac-doctor.sh --check
```

Checks SSH reachability, the remote checkout path, `xcodebuild`, an available iOS
Simulator destination, and the signing team; exits non-zero on failure.

## Fail-fast

If `MEMORY_MAC_HOST` or `MEMORY_MAC_PATH` is unset, the scripts exit non-zero
with an actionable message naming the missing variable — there are no repo
defaults baked into the scripts.
