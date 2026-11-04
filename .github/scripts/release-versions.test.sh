#!/usr/bin/env bash
# Hermetic tests for release-versions.sh (#2111).
#
# Builds throwaway git repositories in a temp dir (no network) for the alias
# assertion, and stubs `docker buildx imagetools inspect` for the map builder
# and its candidate-tag prober (MAP_DOCKER). Run directly:
#
#   .github/scripts/release-versions.test.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/release-versions.sh"
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

# expect_rc <label> <want-rc> <command...>
expect_rc() {
  local label="$1" want="$2" got
  shift 2
  set +e
  "$@" >"$tmp/stdout" 2>"$tmp/stderr"
  got=$?
  set -e
  if [ "$got" -eq "$want" ]; then
    ok "$label"
  else
    bad "$label (want rc=$want, got rc=$got)"
    sed 's/^/       /' "$tmp/stderr" >&2
  fi
}

# expect_stderr <label> <substring>
expect_stderr() {
  local label="$1" want="$2"
  if grep -qF -- "$want" "$tmp/stderr"; then
    ok "$label"
  else
    bad "$label (stderr missing: $want)"
    sed 's/^/       /' "$tmp/stderr" >&2
  fi
}

# expect_stderr_missing <label> <substring>
expect_stderr_missing() {
  local label="$1" want="$2"
  if grep -qF -- "$want" "$tmp/stderr"; then
    bad "$label (stderr unexpectedly contains: $want)"
    sed 's/^/       /' "$tmp/stderr" >&2
  else
    ok "$label"
  fi
}

# expect_stdout <label> <substring>
expect_stdout() {
  local label="$1" want="$2"
  if grep -qF -- "$want" "$tmp/stdout"; then
    ok "$label"
  else
    bad "$label (stdout missing: $want)"
    sed 's/^/       /' "$tmp/stdout" >&2
  fi
}

# expect_jq <label> <file> <jq-filter> <expected>
expect_jq() {
  local label="$1" file="$2" filter="$3" want="$4" got
  got="$(jq -r "$filter" "$file" 2>/dev/null || true)"
  if [ "$got" = "$want" ]; then
    ok "$label"
  else
    bad "$label (want '$want', got '$got')"
  fi
}

commit() { # commit <message>
  local repo="$1" msg="$2"
  git -C "$repo" add -A
  git -C "$repo" commit -qm "$msg"
}

new_repo() { # new_repo <path>
  local repo="$1"
  mkdir -p "$repo"
  git -C "$repo" init -q -b main
  git -C "$repo" config user.email test@example.com
  git -C "$repo" config user.name test
}

# ---------------------------------------------------------------------------
# Fixture A: the v0.88.0 incident (#2087). A rolling tag `cli-dev` is
# force-moved onto the commit *after* the last release, so a diff from it hides
# the web-ui change that the release still has to ship.
#
#   c1 initial ............................ v0.87.3
#   c2 docs only .......................... v0.87.4
#   c3 web-ui change, cli-dev rolled here
#   c4 version bump ....................... v0.88.0
# ---------------------------------------------------------------------------
incident="$tmp/incident"
new_repo "$incident"
mkdir -p "$incident/apps/web-ui" "$incident/apps/server"
echo "web v1" >"$incident/apps/web-ui/app.txt"
echo "server v1" >"$incident/apps/server/main.txt"
# The alias check reads each image's Dockerfile inputs; `COPY . .` keeps these
# fixtures equivalent to the pre-#2143 path-scoped check.
printf 'FROM scratch\nCOPY . .\n' >"$incident/apps/web-ui/Dockerfile"
printf 'FROM scratch\nCOPY . .\n' >"$incident/apps/server/Dockerfile"
commit "$incident" "c1 initial"
git -C "$incident" tag v0.87.3
echo "docs" >"$incident/README.md"
commit "$incident" "c2 docs only"
git -C "$incident" tag v0.87.4
echo "web v2" >"$incident/apps/web-ui/app.txt"
commit "$incident" "c3 web-ui change"
git -C "$incident" tag cli-dev
echo "0.88.0" >"$incident/VERSION"
commit "$incident" "c4 bump"
git -C "$incident" tag v0.88.0

# Sanity: the buggy signal really is empty, and the true signal is not.
if [ -z "$(git -C "$incident" diff --name-only cli-dev HEAD -- apps/web-ui/)" ]; then
  ok "fixture: diff from rolling tag hides the web-ui change"
else
  bad "fixture: rolling-tag diff should be empty"
fi
if [ -n "$(git -C "$incident" diff --name-only v0.87.4 HEAD -- apps/web-ui/)" ]; then
  ok "fixture: diff from the real previous release sees the web-ui change"
else
  bad "fixture: previous-release diff should not be empty"
fi

prev="$(CHECK_REPO="$incident" CHECK_CURRENT_TAG=v0.88.0 "$script" prev-tag)"
if [ "$prev" = "v0.87.4" ]; then
  ok "prev-tag ignores the rolling tag and returns v0.87.4"
else
  bad "prev-tag: want v0.87.4, got '$prev'"
fi

expect_rc "incident: changed=false with rolling PREV is rejected" 1 \
  env CHECK_REPO="$incident" CHECK_SERVICE=web-ui CHECK_PATH=apps/web-ui \
  CHECK_CHANGED=false CHECK_PREV_TAG=cli-dev CHECK_CURRENT_TAG=v0.88.0 \
  "$script" check
expect_stderr "incident: rejection names the wrong alias source" \
  "used 'cli-dev', but the previous release tag is 'v0.87.4'"

expect_rc "incident: changed=false with a stale release PREV is rejected" 1 \
  env CHECK_REPO="$incident" CHECK_SERVICE=web-ui CHECK_PATH=apps/web-ui \
  CHECK_CHANGED=false CHECK_PREV_TAG=v0.87.3 CHECK_CURRENT_TAG=v0.88.0 \
  "$script" check

expect_rc "incident: changed=false with the resolved release PREV is rejected" 1 \
  env CHECK_REPO="$incident" CHECK_SERVICE=web-ui CHECK_PATH=apps/web-ui \
  CHECK_CHANGED=false CHECK_PREV_TAG=v0.87.4 CHECK_CURRENT_TAG=v0.88.0 \
  "$script" check
expect_stderr "incident: independent diff disagreement is reported" \
  "differ between v0.87.4 and v0.88.0"

expect_rc "incident: changed=true is allowed (rebuild)" 0 \
  env CHECK_REPO="$incident" CHECK_SERVICE=web-ui CHECK_PATH=apps/web-ui \
  CHECK_CHANGED=true CHECK_PREV_TAG=cli-dev CHECK_CURRENT_TAG=v0.88.0 \
  "$script" check

# ---------------------------------------------------------------------------
# Fixture B: clean history — web-ui and server are unchanged since v0.87.4.
#
#   c1 web-ui + server ................... v0.87.3
#   c2 web-ui change ..................... v0.87.4
#   c3 version bump ...................... v0.88.0
# ---------------------------------------------------------------------------
clean="$tmp/clean"
new_repo "$clean"
mkdir -p "$clean/apps/web-ui" "$clean/apps/server"
echo "web v1" >"$clean/apps/web-ui/app.txt"
echo "server v1" >"$clean/apps/server/main.txt"
printf 'FROM scratch\nCOPY . .\n' >"$clean/apps/web-ui/Dockerfile"
printf 'FROM scratch\nCOPY . .\n' >"$clean/apps/server/Dockerfile"
commit "$clean" "c1"
git -C "$clean" tag v0.87.3
echo "web v2" >"$clean/apps/web-ui/app.txt"
commit "$clean" "c2 web-ui"
git -C "$clean" tag v0.87.4
echo "0.88.0" >"$clean/VERSION"
commit "$clean" "c3 bump"
git -C "$clean" tag v0.88.0

expect_rc "clean: identical web-ui tree allows the alias" 0 \
  env CHECK_REPO="$clean" CHECK_SERVICE=web-ui CHECK_PATH=apps/web-ui \
  CHECK_CHANGED=false CHECK_PREV_TAG=v0.87.4 CHECK_CURRENT_TAG=v0.88.0 \
  "$script" check
expect_stdout "clean: alias success is logged" "alias=ok service=web-ui previous=v0.87.4"

expect_rc "clean: identical server tree allows the alias" 0 \
  env CHECK_REPO="$clean" CHECK_SERVICE=server CHECK_PATH=apps/server \
  CHECK_CHANGED=false CHECK_PREV_TAG=v0.87.4 CHECK_CURRENT_TAG=v0.88.0 \
  "$script" check

expect_rc "clean: rebuilding an unchanged tree is allowed" 0 \
  env CHECK_REPO="$clean" CHECK_SERVICE=web-ui CHECK_PATH=apps/web-ui \
  CHECK_CHANGED=true CHECK_PREV_TAG=v0.87.4 CHECK_CURRENT_TAG=v0.88.0 \
  "$script" check
expect_stderr "clean: redundant rebuild is warned, not failed" \
  "rebuilding is safe but unnecessary"

# Missing previous map (releases before #2111) degrades to the tree comparison.
expect_rc "clean: missing previous map degrades instead of failing" 0 \
  env CHECK_REPO="$clean" CHECK_SERVICE=web-ui CHECK_PATH=apps/web-ui \
  CHECK_CHANGED=false CHECK_PREV_TAG=v0.87.4 CHECK_CURRENT_TAG=v0.88.0 \
  CHECK_PREV_MAP="$tmp/no-such-map.json" "$script" check
expect_stderr "clean: missing previous map is logged" \
  "previous release version map not found"

# Previous map present: matching digest passes, mismatched digest fails.
prev_map="$tmp/prev-map.json"
cat >"$prev_map" <<'JSON'
{
  "schema_version": 1,
  "release": {"tag": "v0.87.4", "version": "0.87.4", "commit": "deadbee"},
  "previous_release": {"tag": "v0.87.3"},
  "degraded": false,
  "notes": [],
  "services": [
    {"service": "web-ui", "version": "0.86.9", "version_source": "previous-map",
     "digest": "sha256:webui-same", "source": "aliased-from:v0.87.3"}
  ]
}
JSON
expect_rc "clean: recorded digest matches the alias source" 0 \
  env CHECK_REPO="$clean" CHECK_SERVICE=web-ui CHECK_PATH=apps/web-ui \
  CHECK_CHANGED=false CHECK_PREV_TAG=v0.87.4 CHECK_CURRENT_TAG=v0.88.0 \
  CHECK_PREV_MAP="$prev_map" CHECK_PREV_DIGEST=sha256:webui-same \
  "$script" check
expect_rc "clean: moved previous tag (digest mismatch) fails" 1 \
  env CHECK_REPO="$clean" CHECK_SERVICE=web-ui CHECK_PATH=apps/web-ui \
  CHECK_CHANGED=false CHECK_PREV_TAG=v0.87.4 CHECK_CURRENT_TAG=v0.88.0 \
  CHECK_PREV_MAP="$prev_map" CHECK_PREV_DIGEST=sha256:something-else \
  "$script" check
expect_stderr "clean: digest mismatch names both digests" \
  "the previous release tag moved after publication"

# A map belonging to a different release is ignored, not applied to this one.
wrong_map="$tmp/wrong-map.json"
jq '.release.tag = "v0.87.3"' "$prev_map" >"$wrong_map"
expect_rc "clean: map for another release is ignored" 0 \
  env CHECK_REPO="$clean" CHECK_SERVICE=web-ui CHECK_PATH=apps/web-ui \
  CHECK_CHANGED=false CHECK_PREV_TAG=v0.87.4 CHECK_CURRENT_TAG=v0.88.0 \
  CHECK_PREV_MAP="$wrong_map" CHECK_PREV_DIGEST=sha256:something-else \
  "$script" check
expect_stderr "clean: ignored map is logged" \
  "previous version map is for v0.87.3, not v0.87.4"

# First release ever: no previous release tag -> unverified, never a failure.
git -C "$clean" checkout -q v0.87.3
expect_rc "first release: no previous tag degrades instead of failing" 0 \
  env CHECK_REPO="$clean" CHECK_SERVICE=web-ui CHECK_PATH=apps/web-ui \
  CHECK_CHANGED=false CHECK_CURRENT_TAG=v0.87.3 "$script" check
expect_stderr "first release: degradation is logged" \
  "no previous release tag reachable"
git -C "$clean" checkout -q v0.88.0

# ---------------------------------------------------------------------------
# Fixture C: the published-server layout (#2143). The image is built from
# deploy/self-hosted/Dockerfile.server with the repo root as context, so it
# reads apps/cli and deploy/self-hosted/* — outside CHECK_PATH=apps/server.
#
#   c1 apps/server + apps/cli + deploy/self-hosted ... v0.87.3
#   c2 docs only ..................................... v0.87.4
#   c3 deploy/self-hosted/entrypoint.sh ............... v0.88.0
#   c4 apps/cli/main.txt .............................. v0.88.1
#   c5 Dockerfile base-image tag ....................... v0.88.2
#   c6 apps/server/main.txt ........................... v0.88.3
#   c7 a file named only on a continued COPY line ...... v0.88.4
# ---------------------------------------------------------------------------
mirror="$tmp/mirror"
new_repo "$mirror"
mkdir -p "$mirror/apps/server/migrations" "$mirror/apps/cli" "$mirror/deploy/self-hosted" "$mirror/docs"
echo "server v1" >"$mirror/apps/server/main.txt"
echo "go.mod" >"$mirror/apps/server/go.mod"
echo "go.sum" >"$mirror/apps/server/go.sum"
echo "migrations v1" >"$mirror/apps/server/migrations/0001.sql"
echo "cli v1" >"$mirror/apps/cli/main.txt"
echo "entrypoint v1" >"$mirror/deploy/self-hosted/entrypoint.sh"
echo "wrapper v1" >"$mirror/deploy/self-hosted/memory-wrapper.sh"
echo "docs v1" >"$mirror/docs/notes.md"
cat >"$mirror/deploy/self-hosted/Dockerfile.server" <<'DOCKER'
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS server-builder
COPY apps/server/go.mod apps/server/go.sum ./
COPY apps/server/ ./
RUN go build ./...
FROM alpine:3.21
COPY --from=server-builder /out/server /usr/local/bin/
COPY apps/cli/ ./
COPY apps/server/migrations/*.sql /app/migrations/
COPY deploy/self-hosted/entrypoint.sh \
     deploy/self-hosted/healthcheck.sh /app/
COPY deploy/self-hosted/memory-wrapper.sh /usr/local/bin/emergent
DOCKER
# The default resolution (CHECK_DOCKERFILE unset) points at this one.
printf 'FROM scratch\nCOPY . .\n' >"$mirror/apps/server/Dockerfile"
commit "$mirror" "c1 inputs"
git -C "$mirror" tag v0.87.3
echo "docs v2" >"$mirror/docs/notes.md"
commit "$mirror" "c2 docs only"
git -C "$mirror" tag v0.87.4
echo "entrypoint v2" >"$mirror/deploy/self-hosted/entrypoint.sh"
commit "$mirror" "c3 entrypoint"
git -C "$mirror" tag v0.88.0
echo "cli v2" >"$mirror/apps/cli/main.txt"
commit "$mirror" "c4 cli"
git -C "$mirror" tag v0.88.1
sed -i 's/golang:1.26-alpine/golang:1.27-alpine/' "$mirror/deploy/self-hosted/Dockerfile.server"
commit "$mirror" "c5 base image"
git -C "$mirror" tag v0.88.2
echo "server v2" >"$mirror/apps/server/main.txt"
commit "$mirror" "c6 server tree"
git -C "$mirror" tag v0.88.3
# Only named on a continued COPY line: without joining continuations the parse
# sees `entrypoint.sh` and treats this line as an instruction of its own, so the
# file would be an invisible input.
echo "healthcheck v1" >"$mirror/deploy/self-hosted/healthcheck.sh"
commit "$mirror" "c7 healthcheck (continued COPY line)"
git -C "$mirror" tag v0.88.4

# Signal 2 is about the image, not the tree: a docs-only change between two
# releases leaves every input identical, so the alias is allowed. This case also
# exercises the default CHECK_DOCKERFILE=<CHECK_PATH>/Dockerfile.
expect_rc "inputs: a non-input change still allows the alias (default Dockerfile)" 0 \
  env CHECK_REPO="$mirror" CHECK_SERVICE=server CHECK_PATH=apps/server \
  CHECK_CHANGED=false CHECK_PREV_TAG=v0.87.3 CHECK_CURRENT_TAG=v0.87.4 \
  "$script" check
expect_stdout "inputs: alias success is logged" "alias=ok service=server previous=v0.87.3"

# The case from the issue: the entrypoint is copied into the image but lives
# outside apps/server, so the old path-scoped diff called it unchanged.
expect_rc "inputs: entrypoint.sh change outside CHECK_PATH is rejected" 1 \
  env CHECK_REPO="$mirror" CHECK_SERVICE=server CHECK_PATH=apps/server \
  CHECK_DOCKERFILE=deploy/self-hosted/Dockerfile.server CHECK_CONTEXT=. \
  CHECK_CHANGED=false CHECK_PREV_TAG=v0.87.4 CHECK_CURRENT_TAG=v0.88.0 \
  "$script" check
expect_stderr "inputs: the rejection names the Dockerfile's inputs" \
  "the image inputs (deploy/self-hosted/Dockerfile.server and every path it copies) differ"
expect_stderr "inputs: the diff names the entrypoint" "deploy/self-hosted/entrypoint.sh"

# A shared tree consumed by the server image (apps/cli is COPYed in) is an input
# too, even though it has its own release lane.
expect_rc "inputs: apps/cli change is rejected for the server image" 1 \
  env CHECK_REPO="$mirror" CHECK_SERVICE=server CHECK_PATH=apps/server \
  CHECK_DOCKERFILE=deploy/self-hosted/Dockerfile.server CHECK_CONTEXT=. \
  CHECK_CHANGED=false CHECK_PREV_TAG=v0.88.0 CHECK_CURRENT_TAG=v0.88.1 \
  "$script" check
expect_stderr "inputs: the diff names apps/cli" "apps/cli/main.txt"

# The Dockerfile is an input: a base-image bump (or any COPY line edit) changes
# the image even when every copied tree is identical.
expect_rc "inputs: Dockerfile change is rejected" 1 \
  env CHECK_REPO="$mirror" CHECK_SERVICE=server CHECK_PATH=apps/server \
  CHECK_DOCKERFILE=deploy/self-hosted/Dockerfile.server CHECK_CONTEXT=. \
  CHECK_CHANGED=false CHECK_PREV_TAG=v0.88.1 CHECK_CURRENT_TAG=v0.88.2 \
  "$script" check
expect_stderr "inputs: the diff names the Dockerfile" "deploy/self-hosted/Dockerfile.server"

# Sanity: the tree the check has always covered still trips it.
expect_rc "inputs: service-tree change is still rejected" 1 \
  env CHECK_REPO="$mirror" CHECK_SERVICE=server CHECK_PATH=apps/server \
  CHECK_DOCKERFILE=deploy/self-hosted/Dockerfile.server CHECK_CONTEXT=. \
  CHECK_CHANGED=false CHECK_PREV_TAG=v0.88.2 CHECK_CURRENT_TAG=v0.88.3 \
  "$script" check

# A source named only on a continued COPY line is still an input.
expect_rc "inputs: a continued COPY line's source is rejected" 1 \
  env CHECK_REPO="$mirror" CHECK_SERVICE=server CHECK_PATH=apps/server \
  CHECK_DOCKERFILE=deploy/self-hosted/Dockerfile.server CHECK_CONTEXT=. \
  CHECK_CHANGED=false CHECK_PREV_TAG=v0.88.3 CHECK_CURRENT_TAG=v0.88.4 \
  "$script" check
expect_stderr "inputs: the diff names the continued-line source" \
  "deploy/self-hosted/healthcheck.sh"

# A cross-stage copy (`COPY --from=...`) needs no pathspec of its own; the
# fixture Dockerfile has one and every case above would have failed on it.
ok "inputs: --from copies are skipped rather than treated as unresolved"

# ---------------------------------------------------------------------------
# Fixture D: inputs the check cannot see are a refusal, not a pass (#2143).
#   c1 no Dockerfile .................................. v1.0.0
#   c2 adds a Dockerfile with a build-variable source .. v1.0.1
#   c3 change outside every input ..................... v1.0.2
# ---------------------------------------------------------------------------
opaque="$tmp/opaque"
new_repo "$opaque"
mkdir -p "$opaque/service"
echo "v1" >"$opaque/service/main.txt"
echo "notes v1" >"$opaque/README.md"
commit "$opaque" "c1 no Dockerfile"
git -C "$opaque" tag v1.0.0
cat >"$opaque/service/Dockerfile" <<'DOCKER'
FROM alpine:3.21
ARG SRC=service
COPY $SRC/ /app/
DOCKER
commit "$opaque" "c2 variable source"
git -C "$opaque" tag v1.0.1
# Outside the service tree, so an unreadable COPY source is the only reason this
# can fail: without the fail-closed guard the diff is empty and the alias passes.
echo "notes v2" >"$opaque/README.md"
commit "$opaque" "c3 unrelated"
git -C "$opaque" tag v1.0.2

expect_rc "opaque: a build-variable COPY source refuses the alias" 1 \
  env CHECK_REPO="$opaque" CHECK_SERVICE=svc CHECK_PATH=service \
  CHECK_CHANGED=false CHECK_PREV_TAG=v1.0.1 CHECK_CURRENT_TAG=v1.0.2 \
  "$script" check
expect_stderr "opaque: the refusal explains the unresolved variable" \
  "is a build variable"

expect_rc "opaque: a Dockerfile absent at the previous release refuses the alias" 1 \
  env CHECK_REPO="$opaque" CHECK_SERVICE=svc CHECK_PATH=service \
  CHECK_DOCKERFILE=service/Dockerfile CHECK_CHANGED=false \
  CHECK_PREV_TAG=v1.0.0 CHECK_CURRENT_TAG=v1.0.1 \
  "$script" check
expect_stderr "opaque: the refusal says the Dockerfile is missing" \
  "cannot read"

# A rebuild needs no verification, so an unreadable Dockerfile is not a failure
# for changed=true.
expect_rc "opaque: changed=true still allows the rebuild" 0 \
  env CHECK_REPO="$opaque" CHECK_SERVICE=svc CHECK_PATH=service \
  CHECK_CHANGED=true CHECK_PREV_TAG=v1.0.1 CHECK_CURRENT_TAG=v1.0.2 \
  "$script" check

# A COPY whose continuation never ends cannot be parsed; refuse rather than
# silently drop whatever the missing lines list (#2143).
broken="$tmp/broken"
new_repo "$broken"
mkdir -p "$broken/service"
echo "v1" >"$broken/service/main.txt"
printf 'FROM alpine:3.21\nCOPY service/main.txt \\\n' >"$broken/service/Dockerfile"
commit "$broken" "c1"
git -C "$broken" tag v2.0.0
echo "notes v2" >"$broken/README.md"
commit "$broken" "c2 unrelated"
git -C "$broken" tag v2.0.1

expect_rc "opaque: an unterminated continuation refuses the alias" 1 \
  env CHECK_REPO="$broken" CHECK_SERVICE=svc CHECK_PATH=service \
  CHECK_CHANGED=false CHECK_PREV_TAG=v2.0.0 CHECK_CURRENT_TAG=v2.0.1 \
  "$script" check
expect_stderr "opaque: the refusal names the continuation" \
  "unterminated line continuation"

# ---------------------------------------------------------------------------
# Map builder. A fake `docker buildx imagetools inspect` answers from a table;
# no registry, no network.
# ---------------------------------------------------------------------------
mkdir -p "$tmp/bin"
cat >"$tmp/bin/docker" <<'STUB'
#!/usr/bin/env bash
ref="${!#}"
digest="$(awk -v r="$ref" '$1 == r {print $2}' "$FAKE_DIGESTS")"
if [ -z "$digest" ]; then
  echo "not found: $ref" >&2
  exit 1
fi
printf '{"digest":"%s"}\n' "$digest"
STUB
chmod +x "$tmp/bin/docker"

digests="$tmp/digests.txt"
cat >"$digests" <<'EOF'
ghcr.io/acme/memory-server:0.88.0 sha256:server-new
ghcr.io/acme/memory-server:0.88 sha256:server-new
ghcr.io/acme/memory-server:0 sha256:server-old
ghcr.io/acme/memory-server:0.87.4 sha256:server-old
ghcr.io/acme/memory-web-ui:0.88.0 sha256:webui-same
ghcr.io/acme/memory-web-ui:v0.88.0 sha256:webui-same
ghcr.io/acme/memory-web-ui:latest sha256:webui-same
ghcr.io/acme/memory-web-ui:0.87.4 sha256:webui-same
ghcr.io/acme/memory-cli:0.88.0 sha256:cli-new
ghcr.io/acme/memory-cli:latest sha256:cli-old
ghcr.io/acme/memory-cli:0.87.4 sha256:cli-old
ghcr.io/acme/memory-workspace:0.88.0 sha256:ws-same
ghcr.io/acme/memory-workspace:0.87.4 sha256:ws-same
EOF

# Candidate tags mirror the real disagreement: the sha tag is what
# `git rev-parse --short` produces (9 chars) while the publisher may have
# published 7; `0` points at another digest. The map must keep only the ones
# that resolve to the tags[0] digest.
services="$tmp/services.json"
cat >"$services" <<'JSON'
[
  {"service": "server", "path": "apps/server", "image": "ghcr.io/acme/memory-server",
   "tags": ["ghcr.io/acme/memory-server:0.88.0", "ghcr.io/acme/memory-server:0.88",
            "ghcr.io/acme/memory-server:0", "ghcr.io/acme/memory-server:sha-d15e77b7b"],
   "aliasable": true, "version_embedded": true, "version_style": "tag"},
  {"service": "web-ui", "path": "apps/web-ui", "image": "ghcr.io/acme/memory-web-ui",
   "tags": ["ghcr.io/acme/memory-web-ui:0.88.0", "ghcr.io/acme/memory-web-ui:v0.88.0",
            "ghcr.io/acme/memory-web-ui:latest"],
   "aliasable": true, "version_embedded": true, "version_style": "bare"},
  {"service": "cli", "path": "apps/cli", "image": "ghcr.io/acme/memory-cli",
   "tags": ["ghcr.io/acme/memory-cli:0.88.0", "ghcr.io/acme/memory-cli:latest"],
   "aliasable": false, "version_embedded": true, "version_style": "bare"},
  {"service": "workspace", "path": "docker", "image": "ghcr.io/acme/memory-workspace",
   "tags": ["ghcr.io/acme/memory-workspace:0.88.0"],
   "aliasable": false, "version_embedded": false}
]
JSON

# Two services, one built (server) and one aliased (web-ui); the aliased
# service's version comes from the previous release's map, not the new tag.
out="$tmp/map-1.json"
expect_rc "map: one built + one aliased" 0 \
  env FAKE_DIGESTS="$digests" MAP_TAG=v0.88.0 MAP_COMMIT=abc1234 \
  MAP_SERVICES="$services" MAP_PREV_TAG=v0.87.4 \
  MAP_DOCKER="$tmp/bin/docker" MAP_OUT="$out" \
  MAP_GENERATED_AT=2026-01-01T00:00:00Z MAP_PREV_MAP="$prev_map" "$script" map

expect_jq "map: schema_version" "$out" '.schema_version' "1"
expect_jq "map: release tag" "$out" '.release.tag' "v0.88.0"
expect_jq "map: release version" "$out" '.release.version' "0.88.0"
expect_jq "map: release commit" "$out" '.release.commit' "abc1234"
expect_jq "map: previous release" "$out" '.previous_release.tag' "v0.87.4"
expect_jq "map: not degraded when the previous map exists" "$out" '.degraded' "false"

expect_jq "map: built server source" "$out" \
  '.services[] | select(.service == "server") | .source' "built"
expect_jq "map: built server version is the tag the image self-reports" "$out" \
  '.services[] | select(.service == "server") | .version' "v0.88.0"
expect_jq "map: built server digest" "$out" \
  '.services[] | select(.service == "server") | .digest' "sha256:server-new"
expect_jq "map: only tags resolving to the tags[0] digest are kept" "$out" \
  '.services[] | select(.service == "server") | .tags | join(",")' \
  "ghcr.io/acme/memory-server:0.88.0,ghcr.io/acme/memory-server:0.88"
expect_jq "map: a candidate tag pointing at another digest is dropped" "$out" \
  '.services[] | select(.service == "server")
   | (.tags | index("ghcr.io/acme/memory-server:0")) == null' "true"
expect_jq "map: a candidate tag that does not exist is dropped" "$out" \
  '.services[] | select(.service == "server")
   | (.tags | index("ghcr.io/acme/memory-server:sha-d15e77b7b")) == null' "true"

expect_jq "map: aliased web-ui source names the previous tag" "$out" \
  '.services[] | select(.service == "web-ui") | .source' "aliased-from:v0.87.4"
expect_jq "map: aliased web-ui version is the PREVIOUS embedded version" "$out" \
  '.services[] | select(.service == "web-ui") | .version' "0.86.9"
expect_jq "map: aliased web-ui version does not equal the new tag" "$out" \
  '.services[] | select(.service == "web-ui") | (.version != "0.88.0")' "true"
expect_jq "map: aliased web-ui keeps every tag resolving to its digest" "$out" \
  '.services[] | select(.service == "web-ui") | .tags | join(",")' \
  "ghcr.io/acme/memory-web-ui:0.88.0,ghcr.io/acme/memory-web-ui:v0.88.0,ghcr.io/acme/memory-web-ui:latest"
expect_jq "map: aliased web-ui digest is the shared digest" "$out" \
  '.services[] | select(.service == "web-ui") | .digest' "sha256:webui-same"
expect_jq "map: aliased web-ui version provenance" "$out" \
  '.services[] | select(.service == "web-ui") | .version_source' "previous-map"

expect_jq "map: cli is always built" "$out" \
  '.services[] | select(.service == "cli") | .source' "built"
expect_jq "map: built cli version is the bare form the image self-reports" "$out" \
  '.services[] | select(.service == "cli") | .version' "0.88.0"
expect_jq "map: a stale latest tag pointing at another digest is dropped" "$out" \
  '.services[] | select(.service == "cli") | .tags | join(",")' \
  "ghcr.io/acme/memory-cli:0.88.0"
expect_jq "map: workspace embeds no version" "$out" \
  '.services[] | select(.service == "workspace") | .version' "null"
expect_jq "map: workspace version provenance" "$out" \
  '.services[] | select(.service == "workspace") | .version_source' "none"

# Missing previous map: alias still recorded, version falls back to the alias
# source tag and the map is flagged degraded (never a failure).
out2="$tmp/map-2.json"
expect_rc "map: no previous map still succeeds" 0 \
  env FAKE_DIGESTS="$digests" MAP_TAG=v0.88.0 MAP_COMMIT=abc1234 \
  MAP_SERVICES="$services" MAP_PREV_TAG=v0.87.4 \
  MAP_DOCKER="$tmp/bin/docker" MAP_OUT="$out2" \
  MAP_GENERATED_AT=2026-01-01T00:00:00Z "$script" map
expect_jq "map: no previous map falls back to the alias tag version" "$out2" \
  '.services[] | select(.service == "web-ui") | .version' "0.87.4"
expect_jq "map: no previous map provenance" "$out2" \
  '.services[] | select(.service == "web-ui") | .version_source' "alias-tag"
expect_jq "map: no previous map is flagged degraded" "$out2" '.degraded' "true"
expect_jq "map: degradation is explained in notes" "$out2" \
  '.notes | length' "1"

# Built services record exactly what their image self-reports: the server embeds
# the tag (`v0.88.0`), web-ui and cli embed the bare version (`0.88.0`). A
# previous release tag with no image in the fixture forces a build (no alias).
out5="$tmp/map-5.json"
expect_rc "map: missing previous image forces a build" 0 \
  env FAKE_DIGESTS="$digests" MAP_TAG=v0.88.0 MAP_COMMIT=abc1234 \
  MAP_SERVICES="$services" MAP_PREV_TAG=v0.86.0 \
  MAP_DOCKER="$tmp/bin/docker" MAP_OUT="$out5" \
  MAP_GENERATED_AT=2026-01-01T00:00:00Z "$script" map
expect_jq "map: built web-ui records the bare version" "$out5" \
  '.services[] | select(.service == "web-ui") | .version' "0.88.0"
expect_jq "map: built web-ui is not aliased" "$out5" \
  '.services[] | select(.service == "web-ui") | .source' "built"
expect_jq "map: built server keeps the v prefix in the same map" "$out5" \
  '.services[] | select(.service == "server") | .version' "v0.88.0"

# An alias with no previous map falls back to the alias source tag in the
# service's own form too: server -> v0.87.4, never a bare 0.87.4.
alias_digests="$tmp/alias-digests.txt"
cat >"$alias_digests" <<'EOF'
ghcr.io/acme/memory-server:0.88.0 sha256:server-same
ghcr.io/acme/memory-server:0.87.4 sha256:server-same
EOF
server_only="$tmp/server-only.json"
jq '[.[] | select(.service == "server")]' "$services" >"$server_only"
out_alias="$tmp/map-alias-tag.json"
expect_rc "map: server alias without a previous map succeeds" 0 \
  env FAKE_DIGESTS="$alias_digests" MAP_TAG=v0.88.0 MAP_COMMIT=abc1234 \
  MAP_SERVICES="$server_only" MAP_PREV_TAG=v0.87.4 \
  MAP_DOCKER="$tmp/bin/docker" MAP_OUT="$out_alias" \
  MAP_GENERATED_AT=2026-01-01T00:00:00Z "$script" map
expect_jq "map: server alias-tag fallback keeps the v prefix" "$out_alias" \
  '.services[0] | .version + " " + .version_source' "v0.87.4 alias-tag"
expect_jq "map: server alias without a previous map is degraded" "$out_alias" \
  '.degraded' "true"

# Probing fails entirely (registry auth/network): the map keeps tags[0] only
# and says so, instead of claiming tags it could not verify. It never fails the
# release map over an informational field.
mkdir -p "$tmp/flaky-bin"
cat >"$tmp/flaky-bin/docker" <<'STUB'
#!/usr/bin/env bash
ref="${!#}"
state="${FAKE_STATE:?}"
mkdir -p "$state"
# The first inspect (tags[0]) succeeds; everything after that fails, including
# the re-probe the prober uses to tell "tag absent" from "registry down".
if [ ! -f "$state/used" ]; then
  : >"$state/used"
  printf '{"digest":"sha256:cli-new"}\n'
  exit 0
fi
echo "registry unavailable" >&2
exit 1
STUB
chmod +x "$tmp/flaky-bin/docker"

cli_only="$tmp/cli-only.json"
jq '[.[] | select(.service == "cli")]' "$services" >"$cli_only"
out_flaky="$tmp/map-flaky.json"
expect_rc "map: registry failure during tag probing degrades instead of failing" 0 \
  env FAKE_STATE="$tmp/flaky-state" FAKE_DIGESTS="$digests" \
  MAP_TAG=v0.88.0 MAP_COMMIT=abc1234 MAP_SERVICES="$cli_only" \
  MAP_PREV_TAG=v0.87.4 MAP_DOCKER="$tmp/flaky-bin/docker" \
  MAP_OUT="$out_flaky" MAP_GENERATED_AT=2026-01-01T00:00:00Z "$script" map
expect_jq "map: degraded tag probe keeps only tags[0]" "$out_flaky" \
  '.services[0].tags | join(",")' "ghcr.io/acme/memory-cli:0.88.0"
expect_jq "map: degraded tag probe is flagged" "$out_flaky" '.degraded' "true"
expect_jq "map: degraded tag probe is explained in notes" "$out_flaky" \
  '.notes | map(select(contains("registry stopped resolving"))) | length' "1"

# A descriptor that embeds a version must state which form it reports.
no_style="$tmp/services-no-style.json"
jq 'map(if .service == "server" then del(.version_style) else . end)' \
  "$services" >"$no_style"
expect_rc "map: version_embedded without version_style fails closed" 1 \
  env FAKE_DIGESTS="$digests" MAP_TAG=v0.88.0 MAP_COMMIT=abc1234 \
  MAP_SERVICES="$no_style" MAP_PREV_TAG=v0.87.4 \
  MAP_DOCKER="$tmp/bin/docker" MAP_OUT="$tmp/map-no-style.json" \
  MAP_GENERATED_AT=2026-01-01T00:00:00Z "$script" map
expect_stderr "map: missing version_style is explained" \
  "must set version_style to 'tag' or 'bare'"

# The previous map may record a null version (workspace); treat as unknown and
# fall back instead of emitting a null for a version-bearing service.
null_map="$tmp/null-map.json"
cat >"$null_map" <<'JSON'
{
  "schema_version": 1,
  "release": {"tag": "v0.87.4", "version": "0.87.4", "commit": "deadbee"},
  "services": [
    {"service": "web-ui", "version": null, "version_source": "none"}
  ]
}
JSON
out3="$tmp/map-3.json"
expect_rc "map: null recorded version falls back" 0 \
  env FAKE_DIGESTS="$digests" MAP_TAG=v0.88.0 MAP_COMMIT=abc1234 \
  MAP_SERVICES="$services" MAP_PREV_TAG=v0.87.4 MAP_PREV_MAP="$null_map" \
  MAP_DOCKER="$tmp/bin/docker" MAP_OUT="$out3" \
  MAP_GENERATED_AT=2026-01-01T00:00:00Z "$script" map
expect_jq "map: null recorded version uses the alias tag" "$out3" \
  '.services[] | select(.service == "web-ui") | .version' "0.87.4"

# Previous tag resolved independently from the git repo (no MAP_PREV_TAG).
out4="$tmp/map-4.json"
expect_rc "map: resolves the previous release tag from git" 0 \
  env FAKE_DIGESTS="$digests" MAP_TAG=v0.88.0 MAP_COMMIT=abc1234 \
  MAP_SERVICES="$services" MAP_REPO="$clean" MAP_HEAD=v0.88.0 \
  MAP_DOCKER="$tmp/bin/docker" MAP_OUT="$out4" \
  MAP_GENERATED_AT=2026-01-01T00:00:00Z MAP_PREV_MAP="$prev_map" "$script" map
expect_jq "map: git-resolved previous release" "$out4" '.previous_release.tag' "v0.87.4"
expect_jq "map: git-resolved alias source" "$out4" \
  '.services[] | select(.service == "web-ui") | .source' "aliased-from:v0.87.4"

# A missing published image is a hard error: the map must never describe an
# image the release did not publish.
missing_services="$tmp/missing-services.json"
jq '.[0].tags[0] = "ghcr.io/acme/memory-server:9.9.9"' "$services" >"$missing_services"
expect_rc "map: missing published image fails" 1 \
  env FAKE_DIGESTS="$digests" MAP_TAG=v0.88.0 MAP_COMMIT=abc1234 \
  MAP_SERVICES="$missing_services" MAP_PREV_TAG=v0.87.4 \
  MAP_DOCKER="$tmp/bin/docker" MAP_OUT="$tmp/map-missing.json" \
  MAP_GENERATED_AT=2026-01-01T00:00:00Z "$script" map
expect_stderr "map: missing image error names the ref" \
  "could not resolve the published digest for ghcr.io/acme/memory-server:9.9.9"

# Every entry must carry the documented required fields.
if jq -e 'all(.services[]; has("service") and has("tags") and has("version")
    and has("digest") and has("source") and has("version_source"))' "$out" >/dev/null; then
  ok "map: every entry has the documented fields"
else
  bad "map: an entry is missing a documented field"
fi

# ---------------------------------------------------------------------------
# Fixture F: the publisher's changed flag and the input parser (#2169).
#
# `changed` must answer from the image's inputs, not from the service tree: the
# published server image is built from deploy/self-hosted/ with the repo root as
# its context, so apps/cli and deploy/self-hosted/* are inputs. The parser must
# also read the shapes Docker allows — a comment inside a continuation (with a
# "$" and a URL in its prose), the JSON form behind a flag, a `..foo` name, a
# whitespace-only line — and fail closed on a symlinked Dockerfile.
# ---------------------------------------------------------------------------
changed_repo="$tmp/changed"
new_repo "$changed_repo"
mkdir -p "$changed_repo/apps/server" "$changed_repo/apps/cli" "$changed_repo/deploy/self-hosted" "$changed_repo/pkg"
echo "server v1" >"$changed_repo/apps/server/main.txt"
echo "cli v1" >"$changed_repo/apps/cli/main.txt"
echo "wrapper v1" >"$changed_repo/deploy/self-hosted/memory-wrapper.sh"
echo "json v1" >"$changed_repo/pkg/json-only.txt"
cat >"$changed_repo/deploy/self-hosted/Dockerfile.server" <<'DOCKER'
FROM scratch
COPY apps/server \
     # the $VERSION is baked in at build time, so this line is prose
     # https://example.com is prose too
     apps/cli /dest/
COPY --chmod=755 ["pkg/json-only.txt", "deploy/self-hosted/memory-wrapper.sh", "/dest/"]
COPY ..foo /dest/foo
# a comment ending in a backslash \
COPY pkg/after-comment.txt /dest/after-comment.txt
COPY ["pkg/bracket]name.txt", "pkg/comma,name.txt", "/dest/"]
COPY --from=build build-only.txt /dest/build-only.txt
DOCKER
# A whitespace-only line used to abort the parser with `words[0]: unbound
# variable` under `set -u`.
printf ' \n\n' >>"$changed_repo/deploy/self-hosted/Dockerfile.server"
echo "foo v1" >"$changed_repo/..foo"
echo "after v1" >"$changed_repo/pkg/after-comment.txt"
echo "bracket v1" >"$changed_repo/pkg/bracket]name.txt"
echo "comma v1" >"$changed_repo/pkg/comma,name.txt"
# Copied only from the build stage, so it is not a context input.
echo "build v1" >"$changed_repo/build-only.txt"
printf 'apps/server/ignored\n' >"$changed_repo/.dockerignore"
commit "$changed_repo" "c1 inputs"
git -C "$changed_repo" tag v0.90.0

run_changed() { # run_changed <label> <want> <prev> [head]
  local label="$1" want="$2" prev="$3" head="${4:-HEAD}" got rc
  set +e
  got="$(env CHANGED_REPO="$changed_repo" \
    CHANGED_DOCKERFILE=deploy/self-hosted/Dockerfile.server CHANGED_CONTEXT=. \
    "$script" changed "$prev" "$head" 2>"$tmp/stderr")"
  rc=$?
  set -e
  if [ "$rc" -ne 0 ]; then
    bad "$label (rc=$rc)"
    sed 's/^/       /' "$tmp/stderr" >&2
  elif [ "$got" = "$want" ]; then
    ok "$label"
  else
    bad "$label (want $want, got '$got')"
  fi
}

# The continuation comment carries a "$" and a URL: folding it into the
# instruction would trip the build-variable/URL guards and refuse the release.
run_changed "changed: identical inputs answer false (comment-in-continuation is not refused)" \
  false v0.90.0

echo "cli v2" >"$changed_repo/apps/cli/main.txt"
commit "$changed_repo" "c2 cli only"
git -C "$changed_repo" tag v0.90.1
run_changed "changed: a CLI-only change rebuilds the server image" true v0.90.0

# pkg/json-only.txt is named only by the JSON form behind --chmod; a form-blind
# parse turned it into `["pkg/json-only.txt",` and would miss the change.
echo "json v2" >"$changed_repo/pkg/json-only.txt"
commit "$changed_repo" "c3 json-form source"
git -C "$changed_repo" tag v0.90.2
run_changed "changed: a JSON-form source behind a flag is an input" true v0.90.1

echo "foo v2" >"$changed_repo/..foo"
commit "$changed_repo" "c4 dotdot name"
git -C "$changed_repo" tag v0.90.3
run_changed "changed: a ..foo path is diffable, not refused" true v0.90.2

printf 'apps/server/ignored\napps/cli/generated\n' >"$changed_repo/.dockerignore"
commit "$changed_repo" "c5 dockerignore"
git -C "$changed_repo" tag v0.90.4
run_changed "changed: .dockerignore is an input" true v0.90.3

echo "docs" >"$changed_repo/README.md"
commit "$changed_repo" "c6 docs only"
git -C "$changed_repo" tag v0.90.5
run_changed "changed: a docs-only change does not rebuild" false v0.90.4

# A comment that *ends* in a backslash is still a comment: it must not be joined
# to the next instruction and swallow it.
echo "after v2" >"$changed_repo/pkg/after-comment.txt"
commit "$changed_repo" "c7 instruction after a backslash comment"
git -C "$changed_repo" tag v0.90.6
run_changed "changed: the instruction after a backslash-terminated comment is an input" true v0.90.5

# The JSON form's body is JSON, not a comma split: ']' and ',' are legal in a
# filename.
echo "comma v2" >"$changed_repo/pkg/comma,name.txt"
commit "$changed_repo" "c8 json comma name"
git -C "$changed_repo" tag v0.90.7
run_changed "changed: a JSON-form source containing a comma is an input" true v0.90.6

echo "bracket v2" >"$changed_repo/pkg/bracket]name.txt"
commit "$changed_repo" "c9 json bracket name"
git -C "$changed_repo" tag v0.90.8
run_changed "changed: a JSON-form source containing ']' is an input" true v0.90.7

# --from= names a path in another stage, so a same-named context file is not an
# input: changing it must not claim the image changed.
echo "build v2" >"$changed_repo/build-only.txt"
commit "$changed_repo" "c10 build-stage-only path"
git -C "$changed_repo" tag v0.90.9
run_changed "changed: a --from= source is not a context input" false v0.90.8

# The changed=true warning must talk about the image, not the service tree: a
# CLI-only release leaves apps/server untouched and still changes the server
# image, so warning "rebuilding is unnecessary" there is wrong (#2169, #2175).
expect_rc "check: a CLI-only rebuild is reported without a warning" 0 \
  env CHECK_REPO="$changed_repo" CHECK_SERVICE=server CHECK_PATH=apps/server \
  CHECK_DOCKERFILE=deploy/self-hosted/Dockerfile.server CHECK_CONTEXT=. \
  CHECK_CHANGED=true CHECK_PREV_TAG=v0.90.0 CHECK_CURRENT_TAG=v0.90.1 \
  "$script" check
expect_stderr_missing "check: a CLI-only rebuild does not warn about an identical tree" "identical"
expect_stdout "check: a CLI-only rebuild is reported as a rebuild" "alias=rebuild"

expect_rc "check: an identical image does warn" 0 \
  env CHECK_REPO="$changed_repo" CHECK_SERVICE=server CHECK_PATH=apps/server \
  CHECK_DOCKERFILE=deploy/self-hosted/Dockerfile.server CHECK_CONTEXT=. \
  CHECK_CHANGED=true CHECK_PREV_TAG=v0.90.4 CHECK_CURRENT_TAG=v0.90.5 \
  "$script" check
expect_stderr "check: the identical-image warning names the inputs" "the image inputs are identical"

# ---------------------------------------------------------------------------
# Fixture G: the alias assertion compares image inputs, not the service tree.
#
# web-ui's Dockerfile copies gateway/ and memory_bridge/, so an e2e spec is not
# an input. The publisher (correctly) aliases; the assertion must not read that
# as a disagreement (#2175).
# ---------------------------------------------------------------------------
alias_repo="$tmp/alias-subset"
new_repo "$alias_repo"
mkdir -p "$alias_repo/apps/web-ui/gateway" "$alias_repo/apps/web-ui/tests/e2e"
printf 'FROM scratch\nCOPY gateway/ ./gateway/\n' >"$alias_repo/apps/web-ui/Dockerfile"
echo "gw v1" >"$alias_repo/apps/web-ui/gateway/main.go"
echo "spec v1" >"$alias_repo/apps/web-ui/tests/e2e/a.spec.ts"
commit "$alias_repo" "c1"
git -C "$alias_repo" tag v0.92.0
echo "spec v2" >"$alias_repo/apps/web-ui/tests/e2e/a.spec.ts"
commit "$alias_repo" "c2 spec only"
git -C "$alias_repo" tag v0.92.1

expect_rc "check: an alias is accepted when only a non-input file changed" 0 \
  env CHECK_REPO="$alias_repo" CHECK_SERVICE=web-ui CHECK_PATH=apps/web-ui \
  CHECK_CHANGED=false CHECK_PREV_TAG=v0.92.0 CHECK_CURRENT_TAG=v0.92.1 \
  "$script" check
expect_stdout "check: the tree-only alias is reported ok" "alias=ok"

expect_rc "changed: a non-input file does not rebuild the image" 0 \
  env CHANGED_REPO="$alias_repo" CHANGED_DOCKERFILE=apps/web-ui/Dockerfile CHANGED_CONTEXT=apps/web-ui \
  "$script" changed v0.92.0 v0.92.1
expect_stdout "changed: the non-input change answers false" "false"

# The converse still fails closed: an input change with changed=false is a
# disagreement.
echo "gw v2" >"$alias_repo/apps/web-ui/gateway/main.go"
commit "$alias_repo" "c3 gateway only"
git -C "$alias_repo" tag v0.92.2
expect_rc "check: an input change with changed=false is refused" 1 \
  env CHECK_REPO="$alias_repo" CHECK_SERVICE=web-ui CHECK_PATH=apps/web-ui \
  CHECK_CHANGED=false CHECK_PREV_TAG=v0.92.1 CHECK_CURRENT_TAG=v0.92.2 \
  "$script" check
expect_stderr "check: the refusal names the inputs" "the image inputs"

expect_rc "changed: an input change rebuilds the image" 0 \
  env CHANGED_REPO="$alias_repo" CHANGED_DOCKERFILE=apps/web-ui/Dockerfile CHANGED_CONTEXT=apps/web-ui \
  "$script" changed v0.92.1 v0.92.2
expect_stdout "changed: the input change answers true" "true"

# A symlinked Dockerfile reads as its target path under `git show` (rc 0, no
# COPY lines). The parser must refuse rather than report an empty input set.
symlinked="$tmp/symlinked"
new_repo "$symlinked"
mkdir -p "$symlinked/apps"
printf 'FROM scratch\nCOPY . .\n' >"$symlinked/apps/real.Dockerfile"
ln -s real.Dockerfile "$symlinked/apps/Dockerfile"
commit "$symlinked" "c1 symlink"
git -C "$symlinked" tag v0.91.0
expect_rc "changed: a symlinked Dockerfile fails closed" 1 \
  env CHANGED_REPO="$symlinked" CHANGED_DOCKERFILE=apps/Dockerfile CHANGED_CONTEXT=apps \
  "$script" changed v0.91.0
expect_stderr "changed: the symlink refusal names the reason" "is a symlink"

# A Dockerfile that did not exist at PREV means a new build definition: rebuild.
printf 'FROM scratch\nCOPY . .\n' >"$symlinked/apps/new.Dockerfile"
commit "$symlinked" "c2 new dockerfile"
git -C "$symlinked" tag v0.91.1
expect_rc "changed: a Dockerfile absent at PREV answers true" 0 \
  env CHANGED_REPO="$symlinked" CHANGED_DOCKERFILE=apps/new.Dockerfile CHANGED_CONTEXT=apps \
  "$script" changed v0.91.0
expect_stdout "changed: the new-Dockerfile answer is true" "true"

echo
echo "passed: $pass, failed: $fail"
[ "$fail" -eq 0 ]
