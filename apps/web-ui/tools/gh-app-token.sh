#!/usr/bin/env bash
# Mint a short-lived GitHub App installation token (defaults: the
# emergent-code-reviewer App) and print it, or run a command with GH_TOKEN set.
#
# Usage:
#   GH_TOKEN="$(tools/gh-app-token.sh)" gh pr list ...
#   tools/gh-app-token.sh -- exec echo hi      # same, but exports GH_TOKEN
#   tools/gh-app-token.sh --path            # print the key path only (debug)
#
# Env overrides:
#   GH_APP_ID             App id                     (default 4884315)
#   GH_APP_KEY            private key PEM path
#   GH_APP_INSTALLATION_ID installation id           (default 160306576)
#
# The App identity is a distinct reviewer; it must have Pull requests: write
# (+ Contents: write to merge) and be installed on the target repo. See
# docs/pr-review-policy.md.
set -euo pipefail

APP_ID="${GH_APP_ID:-4884315}"
INSTALL_ID="${GH_APP_INSTALLATION_ID:-160306576}"
KEY="${GH_APP_KEY:-/root/Emergent Code Reviewer Private Key Sept 9 2026.pem}"

if [[ "${1:-}" == "--path" ]]; then
	printf '%s\n' "$KEY"
	exit 0
fi

if [[ ! -f "$KEY" ]]; then
	echo "gh-app-token: private key not found: $KEY" >&2
	exit 1
fi

JWT="$(
	GH_APP_ID="$APP_ID" GH_APP_KEY="$KEY" python3 - <<'PY'
import base64, json, os, subprocess, time
key = os.environ["GH_APP_KEY"]
app_id = int(os.environ["GH_APP_ID"])
b64 = lambda b: base64.urlsafe_b64encode(b).rstrip(b"=")
now = int(time.time())
header = b64(json.dumps({"alg": "RS256", "typ": "JWT"}).encode())
payload = b64(json.dumps({"iat": now - 60, "exp": now + 540, "iss": app_id}).encode())
signing = header + b"." + payload
sig = subprocess.run(
    ["openssl", "dgst", "-sha256", "-sign", key],
    input=signing, capture_output=True, check=True,
).stdout
print((signing + b"." + b64(sig)).decode())
PY
)"

TOKEN="$(
	curl -fsS -X POST \
		-H "Authorization: Bearer $JWT" \
		-H "Accept: application/vnd.github+json" \
		"https://api.github.com/app/installations/$INSTALL_ID/access_tokens" |
		python3 -c 'import sys,json; print(json.load(sys.stdin)["token"])'
)"

case "${1:-}" in
	"") printf '%s\n' "$TOKEN" ;;
	--) shift; export GH_TOKEN="$TOKEN"; exec "$@" ;;
	*) echo "gh-app-token: unknown argument: $1" >&2; exit 2 ;;
esac
