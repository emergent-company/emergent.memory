# Mac connector build tooling

Scripts in this directory build the macOS connector (a Swift menu-bar app) on a
remote Mac — this Linux server has no Xcode. Everything derives from one shared
config: `lib/mac-remote.sh`, fed by the environment or the gitignored
`apps/connector.mac/.env`.

## Scripts

| Script | What it does |
|---|---|
| `mac-build.sh` | rsync `apps/connector.mac/` → `<MEMORY_MAC_PATH>/client/macos`, XcodeGen + `xcodebuild`, then sign |
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
| `MEMORY_MAC_HOST` | yes | SSH host of the Mac (`ssh` alias, or `user@host`). An explicitly empty value = local-destination mode (tests / `--dry-run` only). |
| `MEMORY_MAC_PATH` | yes | Project directory on the Mac, absolute or `~`-rooted, no spaces. |
| `MEMORY_MAC_SUBTREE` | no | Override the per-script remote subtree (mac connector default `client/macos`). |
| `MEMORY_MAC_IOS_SUBDIR` | no | Back-compat alias for the iOS subtree (default `client/ios`). |
| `ALLOW_SYNC_INTO_GIT` / `ALLOW_DELETE_IN_GIT` | no | Danger overrides for syncing into a real git clone. |
| `MAC_SSH_OPTS` | no | Extra `ssh` options (used by `mac-doctor.sh` to fail fast). |

This repo's own values live in the gitignored `apps/connector.mac/.env`:
`MEMORY_MAC_HOST=mcj-mini`, `MEMORY_MAC_PATH=~/code/alftred`.

## Expected Mac layout

`MEMORY_MAC_PATH` is the rsync'd build checkout root (it is **not** a git clone):

```
<MEMORY_MAC_PATH>/
  client/ios/     # apps/ios/            (owned by apps/web-ui/tools/ios-build-mac.sh)
  client/macos/   # apps/connector.mac/  (owned by mac-build.sh)
  build/          # DerivedData, archives (untouched by --delete)
```

The guarded rsync mirrors only the dedicated subtree (`client/macos` for this
script), so `--delete` can never remove anything outside `client/macos`.

## Signing

Builds over ssh cannot use the login keychain's signing key, so `mac-build.sh`
signs ad-hoc during the build, then re-signs with a dedicated self-signed
identity for a stable code identity across rebuilds. Run
`apps/connector.mac/Scripts/mac-setup-signing.sh` **once** on the Mac to create
that identity (its own keychain, works non-interactively including over ssh).
Without it, builds fall back to ad-hoc signing.

For a real Apple Development identity, use `mac-sign.sh` from a GUI
`Terminal.app` on the Mac (the login keychain's key is unreachable over ssh).

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
