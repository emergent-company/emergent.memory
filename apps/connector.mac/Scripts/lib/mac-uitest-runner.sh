#!/usr/bin/env bash
# mac-uitest-runner.sh — execute the MemoryConnector XCUITest suite inside the
# user's GUI (Aqua) session.
#
# This runs ON THE MAC, launched by the LaunchAgent that mac-uitest-agent.sh
# bootstraps into the `gui/<uid>` domain. launchd runs it inside the user's
# login (Aqua) session — the only context with a WindowServer, which XCUITest
# requires. It is not meant to be run by hand: the ssh side only ever builds
# (mac-build.sh --uitest) and kickstarts this job.
#
# Protocol (mac-uitest-agent.sh polls the status file; the line prefixes are a
# contract — do not change them without updating the agent):
#   1. write "RUNNING pid=<pid> managername=<...> started=<date>
#      mirror=<path> commit=<sha>" to status (managername= is the evidence
#      launchd put us in the Aqua session; mirror=/commit= are the tree this
#      result belongs to — see #261)
#   2. run xcodebuild test against the pre-built UI-test bundle
#   3. append "DONE rc=<rc> finished=<date>" and exit <rc>
#
# Config comes from the LaunchAgent plist's EnvironmentVariables; the fallbacks
# below let the script be run by hand for debugging.
#
# Deliberately NOT `set -e`: the xcodebuild exit code is the whole point — it is
# captured, reported, and becomes this process's exit status.

set -uo pipefail

MIRROR="${MAC_UITEST_MIRROR:-$HOME/code/mac-connector-mirror}"
LOG_DIR="${MAC_UITEST_LOG_DIR:-$HOME/Library/Logs/memory-connector/uitest}"

# The commit the mirror was synced from, recorded by `mac-build.sh --uitest`.
# mac-uitest-agent.sh compares it with the caller's HEAD before kickstarting;
# recording it here too means a stored status/run log carries the tree the
# result came from (#261).
STAMP="$MIRROR/.memory-mirror-stamp"
SOURCE_COMMIT="$(sed -n 's/^commit=//p' "$STAMP" 2>/dev/null | head -n 1)"
SOURCE_COMMIT="${SOURCE_COMMIT:-unknown}"

mkdir -p "$LOG_DIR"
STATUS_FILE="$LOG_DIR/status"

printf '==> mirror=%s commit=%s\n' "$MIRROR" "$SOURCE_COMMIT"
printf 'RUNNING pid=%s managername=%s started=%s mirror=%s commit=%s\n' \
  "$$" "$(launchctl managername)" "$(date '+%Y-%m-%d %H:%M:%S %z')" \
  "$MIRROR" "$SOURCE_COMMIT" > "$STATUS_FILE"

# No `cd`: absolute paths only, so the working directory and the derived-data
# path the build used are unambiguous. mac-build.sh cd's to the mirror root and
# builds with -derivedDataPath 'build/DerivedData', i.e. $MIRROR/build/DerivedData
# — reusing it here avoids a full rebuild.
# Code-signing/runtime flags MUST match mac-build.sh's `--uitest` build, or the
# `test` action re-signs the .xctest bundle with the project default (hardened
# runtime, -o runtime) and the ad-hoc runner cannot dlopen it: "mapping process
# and mapped file (non-platform) have different Team IDs". Ad-hoc signing needs
# no keychain and lets tests run; hardened runtime is not required for them.
/usr/bin/xcodebuild test \
  -project "$MIRROR/client/macos/MemoryConnector/MemoryConnector.xcodeproj" \
  -scheme MemoryConnector \
  -destination 'platform=macOS' \
  -derivedDataPath "$MIRROR/build/DerivedData" \
  -only-testing:MemoryConnectorUITests \
  CODE_SIGNING_ALLOWED=YES CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY=- \
  ENABLE_HARDENED_RUNTIME=NO
rc=$?

printf 'DONE rc=%s finished=%s\n' "$rc" "$(date '+%Y-%m-%d %H:%M:%S %z')" >> "$STATUS_FILE"

exit "$rc"
