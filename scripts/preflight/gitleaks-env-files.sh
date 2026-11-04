#!/usr/bin/env bash
# Scan every *tracked* env file, even if `.gitleaks.toml` grows a path allowlist.
#
# Why this exists. This repo tracks `.env` (local-dev defaults, no secrets) and
# the `.env*.example` templates. A path allowlist for `.env*` looks harmless — it
# silences a developer's untracked `.env.local` — but `gitleaks git --staged`
# only scans staged content and `gitleaks dir .` in CI only sees tracked files,
# so a path allowlist there hides exactly the files we ship. One sat in
# `.gitleaks.toml` until the public-repo prep removed it (2026-10), meaning a
# secret committed to `.env` or `.env.example` was never scanned by any gate.
#
# `gitleaks stdin` has no path, so path allowlists cannot apply to it: piping a
# tracked env file through it scans the content unconditionally. Value-pinned
# waivers (the intended mechanism) still apply, so the gate stays quiet for the
# placeholders those files are supposed to contain.
#
# Skipped, with a note, when gitleaks is not installed — matching the other
# secret-scan call sites (`GITLEAKS_OPTIONAL=1` for contribors without it). CI
# installs a pinned binary before running preflight, so the gate is never
# skipped there.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

if ! command -v gitleaks >/dev/null 2>&1; then
  if [ "${GITLEAKS_OPTIONAL:-0}" = "1" ]; then
    echo "gitleaks env files: SKIP (gitleaks not installed, GITLEAKS_OPTIONAL=1)"
    exit 0
  fi
  echo "gitleaks env files: gitleaks not installed — install it (bash .github/scripts/install-gitleaks.sh) or set GITLEAKS_OPTIONAL=1 to skip" >&2
  exit 1
fi

config=".gitleaks.toml"
[ -f "$config" ] || { echo "gitleaks env files: $config missing" >&2; exit 1; }

# Tracked env files: `.env`, `.envrc`, and `.env.<suffix>` at any depth.
# Gitignored files never appear here, which is the whole point.
mapfile -t files < <(git ls-files | grep -E '(^|/)\.env([^/]*)$' || true)

if [ "${#files[@]}" -eq 0 ]; then
  echo "gitleaks env files: OK (no tracked env files)"
  exit 0
fi

findings=0
for f in "${files[@]}"; do
  [ -f "$f" ] || continue
  if ! out="$(gitleaks stdin --config "$config" --no-banner --redact <"$f" 2>&1)"; then
    echo "gitleaks env files: finding in tracked env file: $f" >&2
    printf '%s\n' "$out" | sed 's/^/  /' >&2
    findings=1
  fi
done

if [ "$findings" -ne 0 ]; then
  cat >&2 <<'EOF'

A tracked env file tripped the secret scan. Tracked env files must hold
placeholders and non-secret local defaults only:

  - move the value to `.env.local` (gitignored) and leave a placeholder here
  - rotate the credential if it was ever pushed
  - never waive a real value in `.gitleaks.toml` to silence this gate
EOF
  exit 1
fi

echo "gitleaks env files: OK (${#files[@]} tracked env file(s) scanned)"
