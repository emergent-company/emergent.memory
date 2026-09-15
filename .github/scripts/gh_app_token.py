#!/usr/bin/env python3
"""Mint a short-lived GitHub App installation token from env vars.

Env:
  GH_APP_ID              (default 4884315)
  GH_APP_PRIVATE_KEY     PEM private key (may be stored with literal \\n)
  GH_APP_INSTALLATION_ID (default 160306576)

Prints the token to stdout. Requires openssl on PATH (for JWT RS256 signing).
"""
import base64
import json
import os
import subprocess
import sys
import tempfile
import time
import urllib.request

APP_ID = os.environ.get("GH_APP_ID", "4884315")
INSTALL_ID = os.environ.get("GH_APP_INSTALLATION_ID", "160306576")
KEY = (os.environ.get("GH_APP_PRIVATE_KEY") or "").replace("\\n", "\n")

if not KEY.strip():
    sys.exit("GH_APP_PRIVATE_KEY not set")


def b64(b: bytes) -> bytes:
    return base64.urlsafe_b64encode(b).rstrip(b"=")


# Sign the JWT with the PEM (openssl needs a file, so write a temp one).
fd, pem = tempfile.mkstemp(prefix="gh-app-key-")
try:
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        f.write(KEY)
    now = int(time.time())
    header = b64(json.dumps({"alg": "RS256", "typ": "JWT"}).encode())
    payload = b64(json.dumps({"iat": now - 60, "exp": now + 540, "iss": int(APP_ID)}).encode())
    signing = header + b"." + payload
    sig = subprocess.run(
        ["openssl", "dgst", "-sha256", "-sign", pem],
        input=signing, capture_output=True, check=True,
    ).stdout
    jwt = (signing + b"." + b64(sig)).decode()
finally:
    os.unlink(pem)

req = urllib.request.Request(
    f"https://api.github.com/app/installations/{INSTALL_ID}/access_tokens",
    method="POST",
    headers={"Authorization": f"Bearer {jwt}", "Accept": "application/vnd.github+json"},
)
with urllib.request.urlopen(req, timeout=30) as r:
    token = json.loads(r.read())["token"]

print(token)
