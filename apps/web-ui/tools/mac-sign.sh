#!/usr/bin/env bash
# Sign the INSTALLED MemoryConnector app manually.
#
# RECOMMENDED: use tools/mac-setup-signing.sh instead — it creates a dedicated
# self-signed codesigning keychain that works automatically (and over ssh), so
# tools/mac-build.sh signs every build with a stable identity and Keychain
# tokens / Automation (TCC) grants persist. Use THIS script only if you prefer
# the real Apple Development identity and can run it in a GUI Terminal.app.
#
# Why ad-hoc is a problem: builds done over ssh are ad-hoc signed, so every
# rebuild changes the app's code identity and macOS re-prompts for every
# Keychain item. Signing the installed app with a stable identity (and choosing
# "Always Allow" once) stabilizes the Keychain/TCC identity across launches.
#
# IMPORTANT — this Apple Development path only works from GUI Terminal.app on
# the Mac, NOT over ssh: the signing key lives in the login keychain and is not
# reachable from a non-GUI ssh session, so `codesign` fails with
# errSecInternalComponent. For ssh/automatic signing use the dedicated identity
# from tools/mac-setup-signing.sh.
#
# This script signs the INSTALLED app at ~/Applications/MemoryConnector.app
# (override with --app).
#
# Usage:
#   tools/mac-sign.sh                              # sign ~/Applications/MemoryConnector.app (installed)
#   tools/mac-sign.sh --app /path/to/App.app       # sign a specific app bundle
#
# Env:
#   SIGN_IDENTITY   signing identity (default: "Apple Development")
#                   e.g. SIGN_IDENTITY="Apple Development: Maciej Kucharz (MAWLDMPUH4)"

set -euo pipefail

APP_PATH="${HOME}/Applications/MemoryConnector.app"
IDENTITY="${SIGN_IDENTITY:-Apple Development}"

usage() { sed -n '2,32p' "$0"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --app) APP_PATH="${2:?--app needs a path}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ ! -d "$APP_PATH" ]]; then
  echo "error: app not found at $APP_PATH" >&2
  echo "       build/install first (tools/mac-build.sh --install), or pass --app <path>" >&2
  exit 1
fi

echo "==> signing $APP_PATH with identity: $IDENTITY"
if ! codesign --force --options runtime --sign "$IDENTITY" "$APP_PATH"; then
  cat >&2 <<'EOF'

error: codesign failed.
If the message mentions errSecInternalComponent (or the keychain/signing key),
you are almost certainly running this over ssh. Rerun the same command in
Terminal.app on the Mac, where the login keychain's signing key is available.
EOF
  exit 1
fi

echo "==> signature:"
codesign --display --verbose=2 "$APP_PATH"

cat <<'EOF'

Done. Launch the app and choose "Always Allow" on the first Keychain prompt;
subsequent launches will not re-prompt. After each new build, re-run this
script (the build is ad-hoc signed again) and approve once more.
EOF
