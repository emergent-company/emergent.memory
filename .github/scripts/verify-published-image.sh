#!/usr/bin/env bash
# Verify that a publisher's image actually reached the registry (#1209).
#
# Why this exists
# ---------------
# A publish job's exit status is not the same fact as "the image exists".
# `docker buildx build --push` can fail *after* it has exported the manifest
# list and named the tags — the status stream drops with
# `failed to receive status: rpc error: code = Unavailable`, or the tag
# authorization fails transiently with `403 Forbidden` on an operation that did
# not need to happen. v0.105.0's `Web UI Build & Publish` run (46063546732)
# failed on exactly that pair while the published image was complete and
# pullable, so an operator had to run `docker manifest inspect` and `docker pull`
# by hand to tell "red but fine" from "red and broken".
#
# The publishers therefore decide the run's verdict here, not on a step's exit
# code: after the tags have been (possibly) published, this script asks the
# registry whether the *whole tag set* the job intended to publish resolves to
# one image. That is the deployable-artifact test:
#
#   * every tag resolves and they all name one manifest digest -> verified. The
#     release is pullable by its pinned tag, and the `sha-<short>` tag published
#     from the same build is the witness that this is *this commit's* image.
#     When the tag set carries a `sha-` tag, that witness must match
#     VERIFY_COMMIT (#1224): a release re-run or a moved tag otherwise verifies
#     green against a tag set that was already there, although the image need
#     not be this commit's.
#   * a tag is missing -> the publish is incomplete. A build that never finished
#     leaves nothing named; a build interrupted mid-naming leaves a subset.
#     Both are red, and the remedy is a re-run, not an assumption.
#   * the tags resolve to different digests -> the set is not one image (the
#     first attempt named some tags, a retry named others). Red and re-run: a
#     consumer pinning one tag would get a different image than another tag
#     names.
#
# Called between the first build attempt and the retry, the check also makes
# the retry unnecessary when the first attempt already landed the tags. That is
# deliberate: the retry rebuilds with `no-cache` (cache poisoning is what it
# exists for), so it produces *different* digests and re-stamps tags that were
# already correct — strictly worse than doing nothing. Probe first, retry only
# when the probe says something is actually missing.
#
# Because this read is now the release verdict, a single flaky registry answer
# must not turn into a red release: the probe is retried (VERIFY_ATTEMPTS, 5s
# apart) before it reports "not verified". The retries only ever delay a red;
# they never turn an unverified tag set into a verified one, since every
# attempt re-reads the registry.
#
# Inputs (environment)
# --------------------
#   VERIFY_TAGS           image refs the publisher intended to publish; newline
#                         and/or comma separated, as `metadata-action` emits
#                         them                                            (required)
#   VERIFY_COMMIT         short commit the build ran at; the `sha-` tag whose
#                         suffix is a prefix of it is the witness. Required to
#                         match when the tag set carries a `sha-` tag (#1224)   (optional)
#   VERIFY_OUTCOME        the first build step's outcome, for the message      (optional)
#   VERIFY_RETRY_OUTCOME  the retry step's outcome, for the message            (optional)
#   VERIFY_REQUIRE        `true` (default) fails the run when the tag set is
#                         not verified; `false` only reports (probe mode, used
#                         before the retry — the exit code then carries no
#                         verdict and the caller reads the `verified` output)
#   VERIFY_ATTEMPTS       probes before reporting "not verified" (default: 3)
#   VERIFY_INTERVAL       seconds between probes (default: 5; tests use 0)
#   VERIFY_DOCKER         docker binary (default: docker); tests inject a stub
#   VERIFY_OUTPUT         file to append `verified=`/`digest=`/`pinned=` to
#                         (default: $GITHUB_OUTPUT when set)
#
# Outputs written to VERIFY_OUTPUT (when it is set):
#   verified=true|false
#   digest=<manifest digest of the first resolving tag>   (empty when unverified)
#   pinned=<first resolving tag>                          (empty when unverified)
#
# Exit codes: 0 verified (or probe mode), 1 not verified in require mode,
# 2 usage error.
set -euo pipefail

usage() {
  cat <<'EOF'
verify-published-image.sh — is the published tag set actually in the registry?

Usage: verify-published-image.sh          # inputs via VERIFY_* env vars

See the header of this script for the input list and what "verified" means.
EOF
}

die() {
  echo "::error::$*" >&2
  exit 1
}

warn() {
  echo "::warning::$*" >&2
}

notice() {
  echo "::notice::$*"
}

# inspect_digest <docker> <ref>: print the manifest digest `ref` resolves to.
# A manifest list resolves to one digest as a whole, which is the artifact a
# consumer pins. Any unreadable ref (404, auth, transport) is just "not
# resolved" — the caller's verdict is "not verified", never a guess.
#
# Deliberately duplicated in release-versions.sh (#1224): both are standalone
# CLI entry points, so there is no shared source to import; noted rather than
# extracted for two callers.
inspect_digest() {
  local docker="$1" ref="$2" out
  out="$("$docker" buildx imagetools inspect --format '{{json .Manifest}}' "$ref" 2>/dev/null)" || return 1
  jq -er '.digest // empty' <<<"$out" 2>/dev/null
}

# Commas and newlines both appear in tag lists copied between steps; one ref per
# line is the canonical form.
read_tags() {
  local raw="$1" line token
  while IFS= read -r line; do
    for token in ${line//,/ }; do
      [ -n "$token" ] && printf '%s\n' "$token"
    done
  done <<<"$raw"
}

# positive_int <name> <value>: refuse anything that is not a positive integer.
positive_int() {
  local name="$1" value="$2"
  case "$value" in
    '' | *[!0-9]* | 0) die "${name} must be a positive integer (got '${value}')" ;;
  esac
}

main() {
  local docker="${VERIFY_DOCKER:-docker}"
  local require="${VERIFY_REQUIRE:-true}"
  local commit="${VERIFY_COMMIT:-}"
  local outcome="${VERIFY_OUTCOME:-}"
  local retry="${VERIFY_RETRY_OUTCOME:-}"
  local output="${VERIFY_OUTPUT-${GITHUB_OUTPUT:-}}"
  local raw="${VERIFY_TAGS:-}"
  local max_attempts="${VERIFY_ATTEMPTS:-3}"
  local interval="${VERIFY_INTERVAL:-5}"

  case "$require" in
    true | false) ;;
    *) die "VERIFY_REQUIRE must be 'true' or 'false' (got '${require}')" ;;
  esac
  positive_int VERIFY_ATTEMPTS "$max_attempts"
  case "$interval" in
    '' | *[!0-9]*) die "VERIFY_INTERVAL must be a whole number of seconds (got '${interval}')" ;;
  esac

  local -a tags=()
  mapfile -t tags < <(read_tags "$raw")
  ((${#tags[@]} > 0)) || die "VERIFY_TAGS is empty: nothing to verify, so the publisher cannot prove the image exists (#1209)"

  local tag digest
  local -a resolved_tags=() resolved_digests=() missing_tags=() unique_digests=()
  local verified=false reason="" attempt=1

  while :; do
    resolved_tags=()
    resolved_digests=()
    missing_tags=()
    unique_digests=()

    for tag in "${tags[@]}"; do
      if digest="$(inspect_digest "$docker" "$tag")" && [ -n "$digest" ]; then
        resolved_tags+=("$tag")
        resolved_digests+=("$digest")
      else
        missing_tags+=("$tag")
      fi
    done

    # Distinct digests among the tags that do resolve. One image under several
    # tags is what a single successful publish produces; anything else is a
    # mixed set left by an interrupted push.
    local d known seen
    for d in "${resolved_digests[@]}"; do
      known=false
      for seen in "${unique_digests[@]}"; do
        if [ "$seen" = "$d" ]; then
          known=true
          break
        fi
      done
      if ! $known; then
        unique_digests+=("$d")
      fi
    done

    if ((${#resolved_tags[@]} == 0)); then
      verified=false
      reason="none of the ${#tags[@]} pushed tag(s) resolve, so the build never finished: ${tags[*]}"
    elif ((${#missing_tags[@]} > 0)); then
      verified=false
      reason="only ${#resolved_tags[@]} of ${#tags[@]} pushed tag(s) resolve; missing: ${missing_tags[*]}"
    elif ((${#unique_digests[@]} > 1)); then
      verified=false
      reason="the pushed tags do not name one image (${#unique_digests[@]} digests: ${unique_digests[*]}), so the tag set is left over from different builds"
    else
      verified=true
      reason=""
    fi

    $verified && break
    ((attempt >= max_attempts)) && break
    notice "publish verification attempt ${attempt}/${max_attempts} not verified: ${reason}; retrying in ${interval}s (#1209)"
    sleep "$interval"
    attempt=$((attempt + 1))
  done

  local pinned="" pinned_digest=""
  if ((${#resolved_tags[@]} > 0)); then
    pinned="${resolved_tags[0]}"
    pinned_digest="${resolved_digests[0]}"
  fi

  # The `sha-<short>` tag is the witness that the pinned tag names this commit's
  # image. metadata-action publishes a 7-character suffix while the publishers
  # record `git rev-parse --short` (9 characters here), so either form may be a
  # prefix of the other.
  #
  # When the tag set carries a `sha-` tag, the witness is required (#1224): a
  # release re-run, or a tag that moved, otherwise verifies green against a tag
  # set that was already there although the image need not be this commit's.
  # A tag set with no `sha-` tag at all keeps the advisory behaviour, so a
  # caller that does not publish one is not newly failed.
  local witness="" witness_digest="" suffix i sha_tag_present=false
  local -a unmatched_sha_tags=()
  if [ -n "$commit" ]; then
    for i in "${!resolved_tags[@]}"; do
      tag="${resolved_tags[$i]}"
      case "$tag" in
        *:sha-*)
          sha_tag_present=true
          suffix="${tag##*:sha-}"
          if [ -n "$suffix" ] \
            && { [[ "$commit" == "$suffix"* ]] || [[ "$suffix" == "$commit"* ]]; }; then
            witness="$tag"
            witness_digest="${resolved_digests[$i]}"
          else
            unmatched_sha_tags+=("$tag")
          fi
          ;;
      esac
    done
  fi

  # The tag set resolving to one image is not enough when it carries a sha tag:
  # unless that tag names this commit, the tags may be a pre-existing release's
  # image (#1224). The mismatch is deterministic, so it is checked once after
  # the probe loop rather than retried.
  if $verified && [ -n "$commit" ] && $sha_tag_present && [ -z "$witness" ]; then
    verified=false
    reason="the tag set carries a sha tag that does not witness commit ${commit} (${unmatched_sha_tags[*]}); the tags may belong to a different build, such as a release re-run or a moved tag"
  fi

  if [ -n "$output" ]; then
    {
      echo "verified=${verified}"
      echo "digest=${pinned_digest}"
      echo "pinned=${pinned}"
    } >>"$output"
  fi

  if $verified; then
    echo "publish verified: ${pinned} digest=${pinned_digest} tags=${#tags[@]} attempt=${attempt}"
    if [ -n "$witness" ]; then
      echo "publish witness: ${witness} digest=${witness_digest} commit=${commit}"
    fi

    # Only the last word on publishing matters: a successful retry is the
    # normal path and needs no remark. Red is worth a line when the attempt
    # that was supposed to publish reported failure — including a skipped
    # retry, which is this check's own "the first attempt already did it".
    local red=false detail=""
    if [ "$retry" = "failure" ]; then
      red=true
      detail="the build and the retry both reported failure"
    elif [ "$outcome" = "failure" ] && [ "$retry" != "success" ]; then
      red=true
      case "$retry" in
        skipped) detail="the build reported failure and the retry was skipped because the tags were already published" ;;
        '') detail="the build reported failure" ;;
        *) detail="the build reported failure (retry ${retry})" ;;
      esac
    fi
    if $red; then
      # Deliberately greppable: an operator searching the log for the release
      # verdict must find the digest, not infer it from two step outcomes.
      warn "${detail}, but every pushed tag resolves — treating the publish as successful. publish red but tag verified: ${pinned_digest} (#1209)"
    fi
    return 0
  fi

  if [ "$require" = "true" ]; then
    die "publish failed: ${reason} (after ${attempt}/${max_attempts} probe(s)). If no tag resolves the build never finished; if only some do, the push was interrupted. Do not assume the image exists — re-run the job (#1209)."
  fi
  notice "publish not verified: ${reason}; the caller should rebuild (#1209)"
  return 0
}

main "$@"
