#!/usr/bin/env bash
# Choose which installed Xcode the macOS jobs build with, and make the choice
# visible in the run.
#
# Why this exists: every macOS workflow used to inline
#
#   XCODE=$(ls -d /Applications/Xcode.app 2>/dev/null \
#     || ls -d /Applications/Xcode_*.app 2>/dev/null | sort -V | tail -1)
#
# which is wrong three ways.
#
#   1. `/Applications/Xcode.app` wins unconditionally. That is the bundle App
#      Store's auto-update overwrites *in place*, so a known-good fallback
#      installed next to it is never used. This is how Xcode 27.2 replaced 27.1
#      inside a release window and App Store Connect began refusing uploads
#      with 90534 ("unsupported SDK or Xcode version") on unchanged code.
#   2. `sort -V` compares *paths*, not bundles: it orders "Xcode.app" before
#      "Xcode_27.1.app" no matter what is inside either, so "newest installed"
#      was right only by accident.
#   3. The chosen version and its iOS SDK were never reported, so a rejected
#      upload gave no clue which toolchain produced it.
#
# Selection order:
#   1. `--pin PATH` / `$ACCEPTED_XCODE_PATH` — a deliberate choice, used
#      verbatim. A missing pin is an error, never a silent fall back to
#      "newest": falling back is exactly what auto-update makes unsafe.
#   2. Newest **release** (non-beta) bundle under `--root`, by
#      CFBundleShortVersionString rather than by filename.
#   3. Newest bundle at all, when every install is a beta — with a warning,
#      because App Store Connect rejects beta SDKs for release uploads.
#
# stdout is the selected path and nothing else, so callers write:
#
#   XCODE=$(.github/scripts/select-xcode.sh)
#
# (with `errexit` on, a failed selection aborts the step before the empty value
# is used). Diagnostics go to stderr; a summary table is appended to
# `$GITHUB_STEP_SUMMARY`, and `xcode`/`version`/`sdk`/`reason` to
# `$GITHUB_OUTPUT`, both when those are set.
#
# Tests: select-xcode.test.sh (hermetic, no Xcode needed).
set -euo pipefail

root="${XCODE_ROOT:-/Applications}"
pin="${ACCEPTED_XCODE_PATH:-}"

usage() {
  cat <<'EOF'
usage: select-xcode.sh [--pin PATH] [--root DIR]

  --pin PATH   use this bundle verbatim (also $ACCEPTED_XCODE_PATH)
  --root DIR   where to look for Xcode*.app (default /Applications)
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --pin) pin="${2:-}"; shift 2 ;;
    --pin=*) pin="${1#*=}"; shift ;;
    --root) root="${2:-}"; shift 2 ;;
    --root=*) root="${1#*=}"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "select-xcode: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

# Version of a bundle without launching it. plutil handles the binary plists a
# real bundle ships; the XML branch keeps this runnable on the Linux CI runner
# that executes the tests (and covers hand-made bundles).
bundle_version() {
  local app="$1" plist="$1/Contents/Info.plist" v=""
  if [ -r "$plist" ] && command -v plutil >/dev/null 2>&1; then
    v=$(plutil -extract CFBundleShortVersionString raw -o - "$plist" 2>/dev/null || true)
  fi
  if [ -z "$v" ] && [ -r "$plist" ]; then
    v=$(tr -d '\n' <"$plist" 2>/dev/null |
      sed -n 's|.*<key>CFBundleShortVersionString</key>[[:space:]]*<string>\([^<]*\)</string>.*|\1|p' |
      head -1)
  fi
  printf '%s' "${v:-0}"
}

# Newest of the given bundles, by version. Versions are zero-padded so a plain
# lexical sort is numeric: 27.10 must beat 27.2, which `sort -V` and naive
# string compares disagree about once the minor reaches double digits.
newest_of() {
  while IFS= read -r app; do
    [ -n "$app" ] || continue
    printf '%s\t%s\n' \
      "$(bundle_version "$app" | awk -F. '{printf "%05d.%05d.%05d", $1, $2, $3}')" \
      "$app"
  done <<<"$1" | sort -t$'\t' -k1,1 | tail -1 | cut -f2-
}

# A beta is identified by its own bundle name, not by the path leading to it: a
# root such as /opt/beta/Xcode.app must not make a release look like a beta.
non_beta() {
  while IFS= read -r app; do
    [ -n "$app" ] || continue
    case "${app##*/}" in
      *[Bb][Ee][Tt][Aa]*) ;;
      *) printf '%s\n' "$app" ;;
    esac
  done
}

installed=$(ls -d "$root"/Xcode*.app 2>/dev/null || true)

if [ -z "$installed" ]; then
  echo "::error::No Xcode found in $root. Install one, or point ACCEPTED_XCODE_PATH at an existing bundle." >&2
  exit 1
fi

if [ -n "$pin" ]; then
  if [ ! -d "$pin" ]; then
    echo "::error::Pinned Xcode '$pin' is not installed on this host. Installed: $(echo "$installed" | tr '\n' ' ')" >&2
    echo "select-xcode: fix or clear ACCEPTED_XCODE_PATH / the xcode_path input rather than letting the build pick 'newest'." >&2
    exit 1
  fi
  chosen="$pin"
  reason="pinned"
else
  releases=$(printf '%s\n' "$installed" | non_beta)
  if [ -n "$releases" ]; then
    chosen=$(newest_of "$releases")
    reason="newest release"
  else
    chosen=$(newest_of "$installed")
    reason="newest (beta only)"
    echo "::warning::Every installed Xcode is a beta ($(echo "$installed" | tr '\n' ' ')). App Store Connect rejects beta SDKs for release uploads (90534); install a release Xcode before shipping." >&2
  fi
fi

version=$(bundle_version "$chosen")
# Resolve the SDK against the *chosen* bundle. `xcrun` follows the active
# developer directory, and callers run this before `xcode-select -s`, so an
# unqualified call reports the SDK of whatever the host was last using — which
# would put the wrong SDK in the very 90534 message this exists to make
# unambiguous (pinning 27.1 while the host sits on 27.2 named "27.1, iOS SDK
# 27.2"). DEVELOPER_DIR points xcrun at the bundle being reported.
sdk=""
if command -v xcrun >/dev/null 2>&1; then
  sdk=$(DEVELOPER_DIR="$chosen/Contents/Developer" \
    xcrun --sdk iphoneos --show-sdk-version 2>/dev/null || true)
fi

if [ -n "${GITHUB_OUTPUT:-}" ]; then
  {
    echo "xcode=$chosen"
    echo "version=$version"
    echo "sdk=$sdk"
    echo "reason=$reason"
  } >>"$GITHUB_OUTPUT"
fi

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  {
    echo "### Xcode"
    echo
    echo "| | |"
    echo "|---|---|"
    echo "| selected | \`$chosen\` ($reason) |"
    echo "| Xcode | $version |"
    echo "| iOS SDK | ${sdk:-unknown} |"
    echo "| installed | $(echo "$installed" | tr '\n' ' ') |"
    echo
    echo "Upload rejected with 90534? App Store Connect only accepts SDKs from an Xcode release it supports; pin the last accepted bundle with \`ACCEPTED_XCODE_PATH\`."
  } >>"$GITHUB_STEP_SUMMARY"
fi

echo "select-xcode: $chosen (Xcode $version${sdk:+, iOS SDK $sdk}; $reason)" >&2
printf '%s\n' "$chosen"
