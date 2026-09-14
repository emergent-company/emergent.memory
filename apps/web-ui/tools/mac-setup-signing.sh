#!/usr/bin/env bash
# One-time (idempotent) setup of a DEDICATED local code-signing identity.
#
# Why: build-machine builds over ssh cannot use the login keychain's signing
# key (codesign fails with errSecInternalComponent), so they fall back to
# ad-hoc signing. Ad-hoc changes the app's identity on every rebuild, which
# makes macOS treat each build as a new app and re-prompt for Keychain tokens
# and Automation (TCC) grants. A dedicated self-signed codeSigning identity
# lives in its own keychain that codesign can use NON-INTERACTIVELY (also over
# ssh), giving every build the same stable identity.
#
# Run once, on the Mac (ssh is fine):
#   tools/mac-setup-signing.sh
#
# It creates:
#   ~/Library/Keychains/memory-dev.keychain-db       dedicated keychain
#   ~/.config/memory-connector/signing.keychain.pw   keychain password (0600)
#   ~/.config/memory-connector/signing.env           keychain path + identity (0600)
#
# No secret material is printed. Afterwards, tools/mac-build.sh signs builds
# with this identity automatically.

set -euo pipefail

KEYCHAIN="$HOME/Library/Keychains/memory-dev.keychain-db"
CONF_DIR="$HOME/.config/memory-connector"
PW_FILE="$CONF_DIR/signing.keychain.pw"
ENV_FILE="$CONF_DIR/signing.env"
IDENTITY_CN="Memory Local Dev"
DAYS=3650

umask 077
mkdir -p "$CONF_DIR"

have_identity() {
  # NB: no -v. A self-signed cert is "not trusted", so `-v` (valid only)
  # hides it even though codesign can use it.
  security find-identity -p codesigning "$KEYCHAIN" 2>/dev/null | grep -q "$IDENTITY_CN"
}

if [[ -f "$KEYCHAIN" && -f "$PW_FILE" && -f "$ENV_FILE" ]] && have_identity; then
  echo "==> dedicated signing identity already set up (nothing to do)"
  echo "    keychain: $KEYCHAIN"
  echo "    identity: $IDENTITY_CN"
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  echo "    env:      $ENV_FILE"
  exit 0
fi

if [[ -f "$KEYCHAIN" && ! -f "$PW_FILE" ]]; then
  echo "error: $KEYCHAIN exists but $PW_FILE is missing." >&2
  echo "       Delete the keychain and re-run this script:" >&2
  echo "         security delete-keychain '$KEYCHAIN'" >&2
  exit 1
fi

# Partial state from an earlier failed run (keychain/pw/env present but no
# usable identity): start clean rather than exiting as "already set up".
if [[ -f "$KEYCHAIN" ]] && ! have_identity; then
  echo "==> removing incomplete signing setup and recreating"
  security delete-keychain "$KEYCHAIN" >/dev/null 2>&1 || true
  rm -f "$PW_FILE" "$ENV_FILE"
fi

TMPDIR_SETUP="$(mktemp -d)"
cleanup() { rm -rf "$TMPDIR_SETUP"; }
trap cleanup EXIT

KEYCHAIN_PW="$(openssl rand -base64 32)"
P12_PASS="$(openssl rand -base64 32)"

printf '%s' "$KEYCHAIN_PW" > "$PW_FILE"
chmod 600 "$PW_FILE"

echo "==> creating dedicated keychain: $KEYCHAIN"
security create-keychain -p "$KEYCHAIN_PW" "$KEYCHAIN"
security set-keychain-settings -lut 21600 "$KEYCHAIN"
security unlock-keychain -p "$KEYCHAIN_PW" "$KEYCHAIN"

cat > "$TMPDIR_SETUP/openssl.cnf" <<EOF
[req]
distinguished_name = dn
x509_extensions = v3
prompt = no
[dn]
CN = $IDENTITY_CN
[v3]
basicConstraints = critical,CA:FALSE
keyUsage = critical,digitalSignature
extendedKeyUsage = critical,codeSigning
EOF

echo "==> generating self-signed codeSigning certificate ($IDENTITY_CN)"
openssl req -x509 -newkey rsa:2048 \
  -keyout "$TMPDIR_SETUP/key.pem" \
  -out "$TMPDIR_SETUP/cert.pem" \
  -days "$DAYS" -nodes \
  -config "$TMPDIR_SETUP/openssl.cnf" >/dev/null 2>&1

echo "==> importing identity (granting codesign/security access)"
# Export a PKCS#12 with legacy algorithms and import it: macOS `security
# import` rejects OpenSSL 3's default p12 encryption ("MAC verification
# failed"), and importing PEM key+cert separately does not persist. The legacy
# ciphers (3DES/SHA1) are understood by `security import`.
openssl pkcs12 -export \
  -inkey "$TMPDIR_SETUP/key.pem" \
  -in "$TMPDIR_SETUP/cert.pem" \
  -out "$TMPDIR_SETUP/cert.p12" \
  -name "$IDENTITY_CN" \
  -keypbe PBE-SHA1-3DES -certpbe PBE-SHA1-3DES -macalg sha1 \
  -passout pass:"$P12_PASS" >/dev/null 2>&1
security import "$TMPDIR_SETUP/cert.p12" -k "$KEYCHAIN" -P "$P12_PASS" \
  -T /usr/bin/codesign -T /usr/bin/security >/dev/null

# Allow codesign to use the key without an interactive prompt.
security set-key-partition-list -S apple-tool:,apple:,codesign: \
  -s -k "$KEYCHAIN_PW" "$KEYCHAIN" >/dev/null 2>&1

# codesign only searches identities in the user keychain search list, so add
# the dedicated keychain (keeping existing entries; NUL-separated xargs
# preserves paths containing spaces).
security list-keychains -d user | tr -d '"' | sed 's/^[[:space:]]*//; s/[[:space:]]*$//' | grep -v "^$KEYCHAIN$" | tr '\n' '\0' | xargs -0 security list-keychains -d user -s "$KEYCHAIN" >/dev/null 2>&1 || true

# A self-signed certificate yields a cdhash-based designated requirement by
# default (unstable across rebuilds). Pin the leaf certificate instead so the
# app's code identity stays stable — that is what lets macOS keep Keychain
# access and Automation (TCC) grants across builds.
CERT_SHA1="$(openssl x509 -in "$TMPDIR_SETUP/cert.pem" -noout -fingerprint -sha1 \
  | sed -E 's/^.*=//; s/://g' | tr 'A-Z' 'a-z')"
REQUIREMENT='=designated => identifier "com.emergent.memory.connector" and certificate leaf = H"'"$CERT_SHA1"'"'

# NB: single-quote the requirement so its inner double quotes survive when the
# file is sourced (double-quoting would strip them and corrupt the requirement).
cat > "$ENV_FILE" <<EOF
MEMORY_SIGN_KEYCHAIN="$KEYCHAIN"
MEMORY_SIGN_IDENTITY="$IDENTITY_CN"
MEMORY_SIGN_REQUIREMENT='$REQUIREMENT'
EOF
chmod 600 "$ENV_FILE"

echo "==> verifying identity"
if ! have_identity; then
  echo "error: identity not found after import; codesigning may not work." >&2
  exit 1
fi
# No -v: the certificate is self-signed ("not trusted"), so -v (valid only)
# would hide the identity we just created.
security find-identity -p codesigning "$KEYCHAIN" | grep "$IDENTITY_CN"

cat <<EOF

Done. Stable signing identity is ready:
  keychain: $KEYCHAIN
  identity: $IDENTITY_CN
  config:   $ENV_FILE

tools/mac-build.sh now signs builds with this identity automatically, so
Keychain tokens and Automation grants persist across rebuilds.
EOF
