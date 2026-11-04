#!/usr/bin/env bash
# Guard the style of secret-scan waivers (issue #2022).
#
# Waivers live in `.gitleaks.toml` as value-pinned `[[allowlists]]` entries: the
# regex matches the *fabricated value*, so the waiver survives any edit that
# shifts line numbers and any new secret is still reported.
#
# `.gitleaksignore` entries are `path:rule:line` fingerprints. They re-open the
# finding whenever lines above it move, which turns the required `ci` secret scan
# red for an unrelated edit — so the file must stay absent (or comment-only).
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

ignore=".gitleaksignore"

if [ ! -f "$ignore" ]; then
  echo "gitleaks waivers: OK (value-pinned in .gitleaks.toml)"
  exit 0
fi

active="$(grep -vE '^[[:space:]]*(#|$)' "$ignore" || true)"
if [ -z "$active" ]; then
  echo "gitleaks waivers: OK ($ignore is comment-only)"
  exit 0
fi

count="$(printf '%s\n' "$active" | wc -l | tr -d ' ')"
echo "gitleaks waivers: $count line-pinned fingerprint(s) in $ignore" >&2
printf '%s\n' "$active" | sed 's/^/  /' >&2
cat >&2 <<'EOF'

A `<path>:<rule>:<line>` fingerprint moves whenever lines above the finding
shift, so an unrelated edit re-opens the waived finding and turns the secret
scan red. Add a value-pinned entry to `.gitleaks.toml` instead:

  [[allowlists]]
  description = "<what the fixture is and why it cannot be a secret>"
  regexes     = ['''<the exact fabricated value>''']

Never waive a value that could be a live credential — rotate and remove it.
EOF
exit 1
