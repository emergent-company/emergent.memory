#!/usr/bin/env bash
# Hermetic tests for validate-extension-plists.py (the App Store Connect 90360
# guard). Builds throwaway plist trees in a temp dir and asserts the guard
# fails on a missing rule, passes on a valid one, ignores the app's own
# Info.plist, and refuses to pass vacuously when no extension plist matches.
#
# Run directly:
#
#   .github/scripts/validate-extension-plists.test.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/validate-extension-plists.py"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

pass=0
fail=0
ok()  { pass=$(( pass + 1 )); echo "ok   - $1"; }
bad() { fail=$(( fail + 1 )); echo "FAIL - $1" >&2; }

assert_contains() {
  case "$2" in
    *"$1"*) ok "$3" ;;
    *) bad "$3 (output did not contain '$1'; got: $2)" ;;
  esac
}

# run <root>: run the guard against ROOT, capturing rc + combined output.
run() {
  if out="$(python3 "$script" "$1" 2>&1)"; then rc=0; else rc=$?; fi
}

# write_extension <dir> <activation-rule-fragment>: an extension Info.plist.
write_extension() {
  mkdir -p "$1"
  cat > "$1/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>NSExtension</key>
  <dict>
    <key>NSExtensionPointIdentifier</key>
    <string>com.apple.share-services</string>$2
  </dict>
</dict>
</plist>
EOF
}

# write_app <dir>: an app Info.plist (no NSExtension key at all).
write_app() {
  mkdir -p "$1"
  cat > "$1/Info.plist" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key>
  <string>com.example.app</string>
</dict>
</plist>
EOF
}

# ---------------------------------------------------------------------------
# A declared rule passes, and the app's own plist is not treated as an
# extension (it has no NSExtension dict and must be skipped, not flagged).
write_extension "$tmp/good/Share" '
    <key>NSExtensionAttributes</key>
    <dict>
      <key>NSExtensionActivationRule</key>
      <dict>
        <key>NSExtensionActivationSupportsText</key>
        <true/>
      </dict>
    </dict>'
# A bare boolean rule is also valid: NSExtensionActivationRule: YES.
write_extension "$tmp/good-widget/Widget" '
    <key>NSExtensionAttributes</key>
    <dict>
      <key>NSExtensionActivationRule</key>
      <true/>
    </dict>'
write_app "$tmp/good/App"
run "$tmp/good"
if [ "$rc" -eq 0 ]; then ok "valid extension plist passes (rc=0)"; else bad "valid extension plist should pass (rc=$rc; $out)"; fi
assert_contains "extension Info.plists ok" "$out" "valid tree reports the checked count"
assert_contains "1 extension plist(s), 2 Info.plist(s)" "$out" "app plist is scanned but not counted as an extension"

# ---------------------------------------------------------------------------
# NSExtensionAttributes present but without the rule key -> failure naming it.
write_extension "$tmp/missing/Share" '
    <key>NSExtensionAttributes</key>
    <dict>
      <key>NSExtensionActivationSupportsWebURLWithMaxCount</key>
      <integer>1</integer>
    </dict>'
run "$tmp/missing"
if [ "$rc" -eq 1 ]; then ok "missing NSExtensionActivationRule fails (rc=1)"; else bad "missing rule should fail (rc=$rc)"; fi
assert_contains "90360" "$out" "failure names the App Store Connect error"
assert_contains "$tmp/missing/Share/Info.plist" "$out" "failure names the offending plist"

# ---------------------------------------------------------------------------
# NSExtension with no NSExtensionAttributes at all -> the #480 defect -> failure.
write_extension "$tmp/bare/Share" ''
run "$tmp/bare"
if [ "$rc" -eq 1 ]; then ok "extension without NSExtensionAttributes fails (rc=1)"; else bad "bare extension should fail (rc=$rc)"; fi

# ---------------------------------------------------------------------------
# An empty NSExtensionAttributes dict -> failure (the rule is required).
write_extension "$tmp/empty-attrs/Share" '
    <key>NSExtensionAttributes</key>
    <dict/>'
run "$tmp/empty-attrs"
if [ "$rc" -eq 1 ]; then ok "empty NSExtensionAttributes fails (rc=1)"; else bad "empty attributes should fail (rc=$rc)"; fi

# ---------------------------------------------------------------------------
# No extension plist anywhere -> a vacuous pass must not be reported as ok.
write_app "$tmp/app-only"
run "$tmp/app-only"
if [ "$rc" -eq 1 ]; then ok "no extension plist fails instead of passing vacuously (rc=1)"; else bad "vacuous scan should fail (rc=$rc)"; fi
assert_contains "would assert nothing" "$out" "vacuous scan explains why it is not a pass"

# ---------------------------------------------------------------------------
# A bad root is a usage error (rc=2), not a silent pass.
run "$tmp/does-not-exist"
if [ "$rc" -eq 2 ]; then ok "missing root is an error (rc=2)"; else bad "missing root should be rc=2 (got $rc)"; fi

# ---------------------------------------------------------------------------
echo
if [ "$fail" -ne 0 ]; then
  echo "FAILED: $fail assertion(s) failed, $pass passed" >&2
  exit 1
fi
echo "PASS: $pass assertions passed"
