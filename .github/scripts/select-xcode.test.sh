#!/usr/bin/env bash
# Tests for select-xcode.sh — hermetic, runs on the Linux CI runner.
#
# The bundles are hand-made directories with an XML Info.plist, which is what
# `bundle_version` falls back to when plutil is absent. The real thing (binary
# plists, xcrun) is exercised wherever the script actually runs: the macOS
# runners.
#
# The selection bug these pin: "/Applications/Xcode.app wins
# unconditionally" and "sort -V compares paths, not bundles". Both meant the
# newest *installed* Xcode was not the one chosen, so an auto-update could not
# be routed around with a known-good copy.
set -uo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="$here/select-xcode.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

failures=0
ok() { printf 'ok   %s\n' "$1"; }
no() { printf 'FAIL %s\n' "$1"; failures=$((failures + 1)); }

# make_bundle <root> <name> <version>
make_bundle() {
  mkdir -p "$1/$2/Contents/Developer"
  cat >"$1/$2/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
  <key>CFBundleShortVersionString</key>
  <string>$3</string>
</dict>
</plist>
EOF
}

# select <root> [extra args...] — prints the selected path, stderr to a file.
# `env -u` so a pin exported on the host running the tests cannot change what
# the default-selection cases mean; the pin cases set it explicitly.
select_from() {
  local root="$1"
  shift
  env -u ACCEPTED_XCODE_PATH "$script" --root "$root" "$@" 2>"$tmp/stderr"
}

case_newest_release_beats_xcode_app() {
  local root="$tmp/named" out
  make_bundle "$root" Xcode.app 27.1
  make_bundle "$root" Xcode_27.2.app 27.2
  out=$(select_from "$root") || { no "named: script failed"; return; }
  if [ "$out" = "$root/Xcode_27.2.app" ]; then
    ok "named: newest bundle wins over bare Xcode.app (27.2 over 27.1)"
  else
    no "named: expected Xcode_27.2.app, got '$out'"
  fi
}

case_xcode_app_wins_when_newest() {
  local root="$tmp/bare-newest" out
  make_bundle "$root" Xcode.app 27.2
  make_bundle "$root" Xcode_27.1.app 27.1
  out=$(select_from "$root") || { no "bare: script failed"; return; }
  if [ "$out" = "$root/Xcode.app" ]; then
    ok "bare: a newer bare Xcode.app is still selectable"
  else
    no "bare: expected Xcode.app, got '$out'"
  fi
}

case_numeric_not_lexical() {
  local root="$tmp/numeric" out
  make_bundle "$root" Xcode_27.2.app 27.2
  make_bundle "$root" Xcode_27.10.app 27.10
  out=$(select_from "$root") || { no "numeric: script failed"; return; }
  if [ "$out" = "$root/Xcode_27.10.app" ]; then
    ok "numeric: 27.10 beats 27.2"
  else
    no "numeric: expected Xcode_27.10.app, got '$out'"
  fi
}

case_beta_skipped_when_release_exists() {
  local root="$tmp/mixed" out
  make_bundle "$root" Xcode-beta.app 28.0
  make_bundle "$root" Xcode.app 27.2
  out=$(select_from "$root") || { no "beta skip: script failed"; return; }
  if [ "$out" = "$root/Xcode.app" ]; then
    ok "beta: release preferred over a higher-numbered beta"
  else
    no "beta: expected Xcode.app, got '$out'"
  fi
}

case_beta_only_warns() {
  local root="$tmp/beta-only" out
  make_bundle "$root" Xcode-beta.app 28.0
  out=$(select_from "$root") || { no "beta only: script failed"; return; }
  if [ "$out" = "$root/Xcode-beta.app" ] && grep -q '::warning::' "$tmp/stderr"; then
    ok "beta only: usable, with a warning that uploads will be rejected"
  else
    no "beta only: expected the beta plus a warning, got '$out'"
  fi
}

case_pin_wins() {
  local root="$tmp/pin" out
  make_bundle "$root" Xcode.app 27.2
  make_bundle "$root" Xcode_27.1.app 27.1
  out=$(env ACCEPTED_XCODE_PATH="$root/Xcode_27.1.app" "$script" --root "$root" 2>"$tmp/stderr") ||
    { no "pin: script failed"; return; }
  if [ "$out" = "$root/Xcode_27.1.app" ]; then
    ok "pin: an older pinned bundle wins (the recovery path for 90534)"
  else
    no "pin: expected the pinned bundle, got '$out'"
  fi
}

case_pin_argument_wins() {
  local root="$tmp/pin-arg" out
  make_bundle "$root" Xcode.app 27.2
  make_bundle "$root" Xcode_27.1.app 27.1
  out=$(select_from "$root" --pin "$root/Xcode_27.1.app") ||
    { no "pin arg: script failed"; return; }
  if [ "$out" = "$root/Xcode_27.1.app" ]; then
    ok "pin: --pin works the same as the environment"
  else
    no "pin arg: expected the pinned bundle, got '$out'"
  fi
}

case_missing_pin_is_an_error() {
  local root="$tmp/pin-missing" out status
  make_bundle "$root" Xcode.app 27.2
  out=$(env ACCEPTED_XCODE_PATH="$root/Xcode_99.0.app" "$script" --root "$root" 2>"$tmp/stderr")
  status=$?
  if [ "$status" -ne 0 ] && grep -q 'not installed' "$tmp/stderr"; then
    ok "pin missing: fails instead of silently falling back to newest"
  else
    no "pin missing: expected a non-zero exit naming the pin, got status $status / '$out'"
  fi
}

case_pin_requires_developer_dir() {
  local root="$tmp/pin-nodev" out status
  make_bundle "$root" Xcode.app 27.2
  # A directory that exists but is not an Xcode bundle (no Contents/Developer).
  mkdir -p "$root/Xcode_broken.app"
  out=$(env ACCEPTED_XCODE_PATH="$root/Xcode_broken.app" "$script" --root "$root" 2>"$tmp/stderr")
  status=$?
  if [ "$status" -ne 0 ] && grep -q 'Contents/Developer' "$tmp/stderr"; then
    ok "pin: a pin without Contents/Developer is rejected as not a bundle"
  else
    no "pin nodev: expected a non-zero exit naming Contents/Developer, got status $status / '$out'"
  fi
}

case_unreadable_bundle_is_an_error() {
  local root="$tmp/unreadable" out status
  mkdir -p "$root/Xcode.app/Contents/Developer"
  # No Info.plist -> bundle_version cannot resolve a version; must error, not "0".
  out=$(select_from "$root")
  status=$?
  if [ "$status" -ne 0 ] && grep -q 'no readable version' "$tmp/stderr"; then
    ok "unreadable: a bundle with no readable version is an error, not version 0"
  else
    no "unreadable: expected a non-zero exit naming the unreadable bundle, got status $status / '$out'"
  fi
}

case_no_xcode_is_an_error() {
  local root="$tmp/empty" out status
  mkdir -p "$root"
  out=$(select_from "$root")
  status=$?
  if [ "$status" -ne 0 ] && grep -q 'No Xcode found' "$tmp/stderr"; then
    ok "empty: reports that nothing is installed"
  else
    no "empty: expected a non-zero exit, got status $status / '$out'"
  fi
}

case_writes_outputs_and_summary() {
  local root="$tmp/outputs" out xcode version reason summary
  make_bundle "$root" Xcode.app 27.1
  make_bundle "$root" Xcode_27.2.app 27.2
  : >"$tmp/gh_output"
  : >"$tmp/gh_summary"
  out=$(GITHUB_OUTPUT="$tmp/gh_output" GITHUB_STEP_SUMMARY="$tmp/gh_summary" \
    select_from "$root") || { no "outputs: script failed"; return; }
  xcode=$(sed -n 's/^xcode=//p' "$tmp/gh_output")
  version=$(sed -n 's/^version=//p' "$tmp/gh_output")
  reason=$(sed -n 's/^reason=//p' "$tmp/gh_output")
  summary=$(cat "$tmp/gh_summary")
  if [ "$xcode" = "$root/Xcode_27.2.app" ] && [ "$version" = "27.2" ] &&
    [ "$reason" = "newest release" ] && grep -q '| Xcode | 27.2 |' <<<"$summary"; then
    ok "outputs: step outputs and the summary carry the chosen bundle and its version"
  else
    no "outputs: got xcode='$xcode' version='$version' reason='$reason'"
  fi
}

case_root_named_beta_still_finds_release() {
  local root="$tmp/beta-root" out
  make_bundle "$root" Xcode.app 27.2
  make_bundle "$root" Xcode-beta.app 28.0
  out=$(select_from "$root") || { no "beta root: script failed"; return; }
  if [ "$out" = "$root/Xcode.app" ]; then
    ok "beta root: a directory named after a beta does not hide a release bundle"
  else
    no "beta root: expected Xcode.app, got '$out'"
  fi
}

case_sdk_follows_the_chosen_bundle() {
  local root="$tmp/sdk" out sdk
  make_bundle "$root" Xcode.app 27.2
  make_bundle "$root" Xcode_27.1.app 27.1
  mkdir -p "$tmp/bin"
  cat >"$tmp/bin/xcrun" <<'STUB'
#!/usr/bin/env bash
# Stands in for the real xcrun, which resolves the SDK from the *active*
# developer directory. Read the version out of whatever DEVELOPER_DIR points
# at, so a call that forgets to point it at the chosen bundle yields nothing —
# which is the failure this pins (pinning 27.1 while the host runs 27.2
# reported "27.1, iOS SDK 27.2").
app=${DEVELOPER_DIR%/Contents/Developer}
[ -n "$app" ] || { echo "no DEVELOPER_DIR" >&2; exit 1; }
tr -d '\n' <"$app/Contents/Info.plist" |
  sed -n 's|.*<key>CFBundleShortVersionString</key>[[:space:]]*<string>\([^<]*\)</string>.*|\1|p'
STUB
  chmod +x "$tmp/bin/xcrun"
  : >"$tmp/gh_output"
  out=$(PATH="$tmp/bin:$PATH" \
    env ACCEPTED_XCODE_PATH="$root/Xcode_27.1.app" GITHUB_OUTPUT="$tmp/gh_output" \
    "$script" --root "$root" 2>"$tmp/stderr") ||
    { no "sdk: script failed"; return; }
  sdk=$(sed -n 's/^sdk=//p' "$tmp/gh_output")
  if [ "$out" = "$root/Xcode_27.1.app" ] && [ "$sdk" = "27.1" ]; then
    ok "sdk: reported for the chosen bundle, not the active developer dir"
  else
    no "sdk: expected 27.1 from the pinned bundle, got '$sdk'"
  fi
}

case_newest_release_beats_xcode_app
case_xcode_app_wins_when_newest
case_numeric_not_lexical
case_beta_skipped_when_release_exists
case_beta_only_warns
case_pin_wins
case_pin_argument_wins
case_missing_pin_is_an_error
case_pin_requires_developer_dir
case_unreadable_bundle_is_an_error
case_no_xcode_is_an_error
case_root_named_beta_still_finds_release
case_sdk_follows_the_chosen_bundle
case_writes_outputs_and_summary

if [ "$failures" -ne 0 ]; then
  printf '\n%d test(s) failed\n' "$failures"
  exit 1
fi
printf '\nall select-xcode tests passed\n'
