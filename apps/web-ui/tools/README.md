# iOS remote build tooling

The Mac-connector build tooling now lives with the connector itself (see
`apps/connector.mac/Scripts/README.md`). This directory keeps only the iOS
tooling that remains here: `ios-build-mac.sh`, `ios-debug-mac.sh`, and
`memory-trace.sh`. They build the iOS app on a remote Mac (this Linux server has
no Xcode).

The shared config + plumbing moved to the Mac connector: `lib/mac-remote.sh`
now lives at `apps/connector.mac/Scripts/lib/mac-remote.sh`, fed by the
environment or the gitignored `apps/connector.mac/.env`. The iOS scripts here
source that connector lib.

## Scripts

| Script | What it does |
|---|---|
| `ios-build-mac.sh` | rsync `apps/ios/` → `<MEMORY_MAC_PATH>/client/ios`, then `xcodebuild` (simulator by default) |
| `ios-debug-mac.sh` | rsync + build + install/launch on the simulator, plus logs, screenshots, crash/backtrace helpers |
| `memory-trace.sh` | merge the iOS app trace + worker trace for a room |

For the macOS connector build/sign/doctor scripts (`mac-build.sh`,
`mac-doctor.sh`, `mac-sign.sh`, `mac-setup-signing.sh`), see
`apps/connector.mac/Scripts/README.md`.

`apps/web-ui/deploy.sh` and the `apps/web-ui/memory` launcher take their Mac host
from the same shared config (deploy delegates its iOS step to `ios-build-mac.sh`).

## Configuration

Copy what you need into `apps/connector.mac/.env` (gitignored) or export it. An
explicitly-set environment variable always wins over `.env`. The full template is
`apps/connector.mac/.env.example`.

| Variable | Required | Meaning |
|---|---|---|
| `MEMORY_MAC_HOST` | yes | SSH host of the Mac (`ssh` alias, or `user@host`). An explicitly empty value = local-destination mode (tests / `--dry-run` only). |
| `MEMORY_MAC_PATH` | yes | Project directory on the Mac, absolute or `~`-rooted, no spaces. |
| `MEMORY_MAC_SUBTREE` | no | Override the per-script remote subtree. |
| `MEMORY_MAC_IOS_SUBDIR` | no | Back-compat alias for the iOS subtree (default `client/ios`). |
| `DEVELOPMENT_TEAM` | no | Apple Developer Team ID; defaults to this repo's team (`74LC88G9SC`). |
| `ALLOW_SYNC_INTO_GIT` / `ALLOW_DELETE_IN_GIT` | no | Danger overrides for syncing into a real git clone. |
| `MAC_SSH_OPTS` | no | Extra `ssh` options (used by `mac-doctor.sh` to fail fast). |

The exact target values are per-machine and per-checkout — set `MEMORY_MAC_HOST`
and `MEMORY_MAC_PATH` in your own `apps/connector.mac/.env` (gitignored),
following `apps/connector.mac/.env.example`.

## Expected Mac layout

`MEMORY_MAC_PATH` is the rsync'd build checkout root (it is **not** a git clone):

```
<MEMORY_MAC_PATH>/
  client/ios/     # apps/ios/            (owned by ios-build-mac.sh)
  client/macos/   # apps/connector.mac/  (owned by mac-build.sh)
  build/          # DerivedData, archives (untouched by --delete)
```

The guarded rsync mirrors only the dedicated subtree, so `--delete` can never
remove anything outside `client/ios` or `client/macos`.

## Xcode

The Mac needs Xcode with a developer dir selected
(`sudo xcode-select -s /Applications/Xcode.app`). This repo's Mac is on
**Xcode 27.0**; the iOS release workflow requires Xcode 16+. `mac-doctor.sh
--check` reports the version and whether an iOS Simulator runtime is installed.

## Signing

Simulator builds pass `CODE_SIGNING_ALLOWED=NO` and need no team. Device and
release builds read `DEVELOPMENT_TEAM` (env or CI secret), defaulting to this
repo's team `74LC88G9SC`. The release paths (`build-dmg.sh`,
`ios-release.yml`) render a resolved `ExportOptions.plist` with that value, so a
fork only sets the variable/secret — it never edits a tracked file.

## MCP access to the Mac build host

The `xcodebuild` MCP server is not checked in: point your own MCP client at it
over `ssh` to `$MEMORY_MAC_HOST` (see the client's `mcp` config docs). Most
clients do not read `.env`, so export `MEMORY_MAC_HOST` in the environment that
launches the client, so the MCP and the build scripts agree.

## Fail-fast

If `MEMORY_MAC_HOST` or `MEMORY_MAC_PATH` is unset, the Mac-only scripts exit
non-zero with an actionable message naming the missing variable — there are no
repo defaults baked into the scripts.

## Doctor

```sh
cd apps/web-ui && task mac:doctor      # or: apps/connector.mac/Scripts/mac-doctor.sh --check
```

Checks SSH reachability, the remote checkout, `xcodebuild`, an available iOS
Simulator destination, and the signing team; exits non-zero on failure.
