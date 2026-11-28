#!/usr/bin/env bash
# Hermetic tests for verify-published-image.sh (#1209).
#
# The publisher's verdict must be the published tag set, not a build step's
# exit code. Every case drives the script against a stub `docker buildx
# imagetools inspect` (VERIFY_DOCKER), so nothing here touches a registry. Run
# directly:
#
#   .github/scripts/verify-published-image.test.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/verify-published-image.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

pass=0
fail=0

ok() {
  echo "ok   - $1"
  pass=$((pass + 1))
}

bad() {
  echo "FAIL - $1" >&2
  fail=$((fail + 1))
}

# ── Stub docker ──────────────────────────────────────────────────────────────
# Maps `<ref> <digest>` pairs in $STUB_TAGS_FILE onto whatever `docker buildx
# imagetools inspect --format '{{json .Manifest}}' <ref>` is asked for. An
# unknown ref is a 404 (rc 1); STUB_FAIL_ALL makes the registry unreachable.
mkdir -p "$tmp/bin"
stub="$tmp/bin/docker"
cat >"$stub" <<'STUB'
#!/usr/bin/env bash
if [ "$1" != "buildx" ] || [ "$2" != "imagetools" ] || [ "$3" != "inspect" ]; then
  echo "stub docker: unsupported invocation: $*" >&2
  exit 9
fi
ref="${@: -1}"
if [ -n "${STUB_FAIL_ALL:-}" ]; then
  echo "stub docker: registry unavailable" >&2
  exit 1
fi
# STUB_FLAKY=N: the first N probes fail as if registry auth were transient,
# counted in STUB_STATE (a file), so the caller's probe retry can be exercised.
if [ -n "${STUB_FLAKY:-}" ]; then
  count=0
  [ -f "${STUB_STATE:?}" ] && count=$(cat "${STUB_STATE}")
  if [ "$count" -lt "$STUB_FLAKY" ]; then
    echo $((count + 1)) >"${STUB_STATE}"
    echo "stub docker: transient registry failure" >&2
    exit 1
  fi
fi
while read -r known digest; do
  [ -n "$known" ] || continue
  if [ "$known" = "$ref" ]; then
    printf '{"digest":"%s","mediaType":"application/vnd.oci.image.index.v1+json"}\n' "$digest"
    exit 0
  fi
done <"${STUB_TAGS_FILE:?STUB_TAGS_FILE is required}"
echo "ERROR: ${ref}: not found" >&2
exit 1
STUB
chmod +x "$stub"

tags_file="$tmp/tags"
export STUB_TAGS_FILE="$tags_file"

# fixture <ref> <digest> [<ref> <digest> ...]: what the registry resolves.
fixture() {
  : >"$tags_file"
  while (($#)); do
    printf '%s %s\n' "$1" "$2" >>"$tags_file"
    shift 2
  done
}

# ── Harness ──────────────────────────────────────────────────────────────────
# expect_rc <label> <want-rc> <ENV=value ...>
# Runs the script with the stub docker plus the caller's env assignments and
# captures stdout/stderr in $tmp/stdout/$tmp/stderr and outputs in $tmp/out.
expect_rc() {
  local label="$1" want="$2" got
  shift 2
  : >"$tmp/out"
  : >"$tmp/stdout"
  : >"$tmp/stderr"
  set +e
  # One probe by default: the cases assert the verdict, not the retry delay.
  # A caller that wants the probe retry passes VERIFY_ATTEMPTS explicitly.
  env VERIFY_DOCKER="$stub" VERIFY_OUTPUT="$tmp/out" \
    VERIFY_ATTEMPTS=1 VERIFY_INTERVAL=0 "$@" "$script" \
    >"$tmp/stdout" 2>"$tmp/stderr"
  got=$?
  set -e
  if [ "$got" -eq "$want" ]; then
    ok "$label"
    return 0
  fi
  bad "$label (want rc=$want, got rc=$got)"
  sed 's/^/       stdout: /' "$tmp/stdout" >&2
  sed 's/^/       stderr: /' "$tmp/stderr" >&2
}

expect_stdout() {
  local label="$1" want="$2"
  if grep -qF -- "$want" "$tmp/stdout"; then
    ok "$label"
  else
    bad "$label (stdout missing: $want)"
  fi
}

expect_stdout_missing() {
  local label="$1" never="$2"
  if grep -qF -- "$never" "$tmp/stdout"; then
    bad "$label (stdout unexpectedly contains: $never)"
  else
    ok "$label"
  fi
}

expect_stderr() {
  local label="$1" want="$2"
  if grep -qF -- "$want" "$tmp/stderr"; then
    ok "$label"
  else
    bad "$label (stderr missing: $want)"
  fi
}

expect_stderr_missing() {
  local label="$1" never="$2"
  if grep -qF -- "$never" "$tmp/stderr"; then
    bad "$label (stderr unexpectedly contains: $never)"
  else
    ok "$label"
  fi
}

expect_output() {
  local label="$1" want="$2"
  if grep -qxF -- "$want" "$tmp/out"; then
    ok "$label"
  else
    bad "$label (output missing line: $want)"
    sed 's/^/       /' "$tmp/out" >&2
  fi
}

ui="ghcr.io/acme/memory-web-ui"

# ── The incident: both attempts red, the tag resolves ────────────────────────
# v0.105.0: attempt 1 exported the manifest list and named the tags, then lost
# its status stream; the retry rebuilt and was refused one tag. The release is
# deployable, so the verdict must be green.
fixture \
  "$ui:0.105.0" sha256:1bdce9f0 \
  "$ui:v0.105.0" sha256:1bdce9f0 \
  "$ui:latest" sha256:1bdce9f0 \
  "$ui:sha-5cb0eeb" sha256:1bdce9f0
expect_rc "red build + failed retry behind a complete tag set is green" 0 \
  "VERIFY_TAGS=$ui:0.105.0
$ui:v0.105.0
$ui:latest
$ui:sha-5cb0eeb" \
  VERIFY_COMMIT=5cb0eebd5 \
  VERIFY_OUTCOME=failure \
  VERIFY_RETRY_OUTCOME=failure
expect_stdout "verified tag set is logged with its digest" "publish verified: ${ui}:0.105.0 digest=sha256:1bdce9f0 tags=4"
expect_stdout "the sha tag witnesses this commit's build" "publish witness: ${ui}:sha-5cb0eeb digest=sha256:1bdce9f0 commit=5cb0eebd5"
expect_stderr "a red publish that is verified is called out greppably" "publish red but tag verified: sha256:1bdce9f0"
expect_output "verified is exposed to the caller" "verified=true"
expect_output "the pinned digest is exposed to the caller" "digest=sha256:1bdce9f0"
expect_output "the pinned tag is exposed to the caller" "pinned=${ui}:0.105.0"

# A successful retry is the normal path: green, and no red-but-verified remark.
expect_rc "a successful retry behind a complete tag set is green" 0 \
  "VERIFY_TAGS=$ui:0.105.0" \
  VERIFY_OUTCOME=failure \
  VERIFY_RETRY_OUTCOME=success
expect_stderr_missing "a successful retry is not reported as red" "publish red but tag verified:"

# A skipped retry is this check's own "the first attempt already did it".
expect_rc "a skipped retry behind a complete tag set is green" 0 \
  "VERIFY_TAGS=$ui:0.105.0" \
  VERIFY_OUTCOME=failure \
  VERIFY_RETRY_OUTCOME=skipped
expect_stderr "a skipped retry is explained" "the retry was skipped because the tags were already published"
expect_stderr "a skipped retry still names the verified digest" "publish red but tag verified: sha256:1bdce9f0"

# ── Probe before retry: complete tag set needs no retry ─────────────────────
expect_rc "probe mode is green when the tag set is complete" 0 \
  "VERIFY_TAGS=$ui:0.105.0
$ui:v0.105.0" \
  VERIFY_REQUIRE=false \
  VERIFY_COMMIT=5cb0eebd5
expect_output "probe mode tells the caller not to retry" "verified=true"

# ── Nothing published is red ─────────────────────────────────────────────────
fixture
expect_rc "no tag resolving is red" 1 \
  "VERIFY_TAGS=$ui:0.105.0
$ui:v0.105.0" \
  VERIFY_OUTCOME=failure \
  VERIFY_RETRY_OUTCOME=failure
expect_stderr "the failure names the never-finished build" "the build never finished"
expect_output "an unverified publish is reported as such" "verified=false"

expect_rc "probe mode reports an empty registry without failing" 0 \
  "VERIFY_TAGS=$ui:0.105.0" \
  VERIFY_REQUIRE=false
expect_stdout "probe mode asks the caller to rebuild" "the caller should rebuild"
expect_output "probe mode still reports verified=false" "verified=false"

# ── A partial tag set is red ─────────────────────────────────────────────────
fixture "$ui:0.105.0" sha256:1bdce9f0
expect_rc "a missing tag is a red publish" 1 \
  "VERIFY_TAGS=$ui:0.105.0
$ui:v0.105.0" \
  VERIFY_OUTCOME=failure
expect_stderr "the failure names the missing tag" "missing: ${ui}:v0.105.0"

expect_rc "probe mode reports a partial tag set without failing" 0 \
  "VERIFY_TAGS=$ui:0.105.0
$ui:v0.105.0" \
  VERIFY_REQUIRE=false
expect_output "a partial tag set is not verified" "verified=false"

# ── A mixed tag set (first attempt + retry) is red ───────────────────────────
fixture \
  "$ui:0.105.0" sha256:1bdce9f0 \
  "$ui:v0.105.0" sha256:5abbcbf6
expect_rc "tags from different builds are red" 1 \
  "VERIFY_TAGS=$ui:0.105.0
$ui:v0.105.0" \
  VERIFY_OUTCOME=failure \
  VERIFY_RETRY_OUTCOME=failure
expect_stderr "the failure says the tags do not name one image" "do not name one image"
expect_stdout_missing "a mixed set never claims a verified publish" "publish verified:"

# ── Witness matching ─────────────────────────────────────────────────────────
# #1224: when the tag set carries a `sha-` tag it must witness this commit.
# Otherwise a release re-run or a moved tag verifies a tag set that was already
# there, although the image need not be this commit's.
fixture \
  "$ui:0.105.0" sha256:1bdce9f0 \
  "$ui:sha-deadbee" sha256:1bdce9f0
expect_rc "a sha tag for another commit is red" 1 \
  "VERIFY_TAGS=$ui:0.105.0
$ui:sha-deadbee" \
  VERIFY_COMMIT=5cb0eebd5
expect_stderr "the failure names the non-witness sha tag" "does not witness commit 5cb0eebd5"
expect_stdout_missing "a sha tag for another commit is not a witness" "publish witness:"
expect_output "a non-witness tag set is not verified" "verified=false"

expect_rc "probe mode reports a non-witness tag set without failing" 0 \
  "VERIFY_TAGS=$ui:0.105.0
$ui:sha-deadbee" \
  VERIFY_REQUIRE=false \
  VERIFY_COMMIT=5cb0eebd5
expect_output "probe mode does not verify a non-witness tag set" "verified=false"

# A tag set with no sha tag keeps the advisory behaviour: the caller does not
# publish a witness, so none is required.
fixture "$ui:0.105.0" sha256:1bdce9f0
expect_rc "a tag set without a sha tag verifies without a witness" 0 \
  "VERIFY_TAGS=$ui:0.105.0" \
  VERIFY_COMMIT=5cb0eebd5
expect_stdout_missing "no witness is reported when none was published" "publish witness:"

fixture \
  "$ui:0.105.0" sha256:1bdce9f0 \
  "$ui:sha-5cb0eeb" sha256:1bdce9f0
expect_rc "a 9-character commit matches a 7-character sha tag" 0 \
  "VERIFY_TAGS=$ui:0.105.0
$ui:sha-5cb0eeb" \
  VERIFY_COMMIT=5cb0eebd5
expect_stdout "the shorter sha tag is reported as the witness" "publish witness: ${ui}:sha-5cb0eeb"

# ── Tag list parsing ─────────────────────────────────────────────────────────
expect_rc "comma-separated tags are parsed like newline-separated ones" 0 \
  "VERIFY_TAGS=${ui}:0.105.0,${ui}:sha-5cb0eeb" \
  VERIFY_COMMIT=5cb0eebd5
expect_stdout "both comma-separated tags were probed" "tags=2"

# ── Usage errors and defaults ────────────────────────────────────────────────
expect_rc "an empty tag list is a usage error" 1 VERIFY_TAGS=
expect_stderr "the empty list explains why it cannot verify" "VERIFY_TAGS is empty"

expect_rc "an invalid VERIFY_REQUIRE is a usage error" 1 \
  "VERIFY_TAGS=$ui:0.105.0" \
  VERIFY_REQUIRE=maybe
expect_stderr "the bad VERIFY_REQUIRE is named" "must be 'true' or 'false'"

fixture "$ui:0.105.0" sha256:1bdce9f0
: >"$tmp/gho"
set +e
env VERIFY_DOCKER="$stub" GITHUB_OUTPUT="$tmp/gho" \
  "VERIFY_TAGS=$ui:0.105.0" "$script" >"$tmp/stdout" 2>"$tmp/stderr"
got=$?
set -e
if [ "$got" -eq 0 ] && grep -qxF "verified=true" "$tmp/gho"; then
  ok "GITHUB_OUTPUT is used when VERIFY_OUTPUT is unset"
else
  bad "GITHUB_OUTPUT is used when VERIFY_OUTPUT is unset (rc=$got)"
fi

# A local run has neither VERIFY_OUTPUT nor GITHUB_OUTPUT: the verdict is still
# on stdout, and there is no output file to miss (#1224).
fixture "$ui:0.105.0" sha256:1bdce9f0
: >"$tmp/out"
set +e
env -u GITHUB_OUTPUT VERIFY_DOCKER="$stub" \
  "VERIFY_TAGS=$ui:0.105.0" "$script" >"$tmp/stdout" 2>"$tmp/stderr"
got=$?
set -e
if [ -s "$tmp/out" ]; then
  bad "a local run writes no output file"
elif [ "$got" -eq 0 ] && grep -qF "publish verified: ${ui}:0.105.0" "$tmp/stdout"; then
  ok "a local run reports the verdict on stdout without an output file"
else
  bad "a local run reports the verdict on stdout without an output file (rc=$got)"
fi

# An unreachable registry is "not verified", not a crash or a pass.
fixture "$ui:0.105.0" sha256:1bdce9f0
expect_rc "an unreadable registry is not verified" 1 \
  "VERIFY_TAGS=$ui:0.105.0" \
  STUB_FAIL_ALL=1
expect_output "an unreadable registry reports verified=false" "verified=false"

# ── A flaky registry read is retried before it reds a release ────────────────
fixture "$ui:0.105.0" sha256:1bdce9f0
rm -f "$tmp/state"
expect_rc "a transient probe failure is retried, not a red release" 0 \
  "VERIFY_TAGS=$ui:0.105.0" \
  VERIFY_ATTEMPTS=2 \
  VERIFY_INTERVAL=0 \
  STUB_FLAKY=1 \
  "STUB_STATE=$tmp/state"
expect_stdout "the retry is logged" "publish verification attempt 1/2 not verified"
expect_stdout "the retried probe verifies the tag" "publish verified: ${ui}:0.105.0"
expect_output "a recovered probe reports verified=true" "verified=true"

# A registry that never answers is still red once the attempts run out.
fixture "$ui:0.105.0" sha256:1bdce9f0
rm -f "$tmp/state"
expect_rc "an exhausted probe is red" 1 \
  "VERIFY_TAGS=$ui:0.105.0" \
  VERIFY_ATTEMPTS=2 \
  VERIFY_INTERVAL=0 \
  "STUB_FAIL_ALL=1"
expect_stderr "the exhausted probe reports the attempt count" "after 2/2 probe(s)"

expect_rc "a non-positive VERIFY_ATTEMPTS is a usage error" 1 \
  "VERIFY_TAGS=$ui:0.105.0" \
  VERIFY_ATTEMPTS=0
expect_stderr "the bad VERIFY_ATTEMPTS is named" "VERIFY_ATTEMPTS must be a positive integer"

# ── Summary ──────────────────────────────────────────────────────────────────
echo
if [ "$fail" -ne 0 ]; then
  echo "verify-published-image.test.sh: ${fail} check(s) failed, ${pass} passed" >&2
  exit 1
fi
echo "verify-published-image.test.sh: all ${pass} checks passed"
