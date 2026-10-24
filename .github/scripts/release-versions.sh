#!/usr/bin/env bash
# Release version map + alias assertion (issue #2111).
#
# A release publishes image tags; when a service tree is unchanged the
# publisher *aliases* the previous release's image instead of rebuilding it.
# An aliased image legitimately embeds the PREVIOUS release's version, so a
# consumer cannot infer the embedded version from the tag. This script produces
# the machine-readable record of that intent and independently checks the
# alias decision.
#
# Subcommands
# -----------
#   prev-tag [CURRENT_TAG]
#       Print the previous release tag (`vX.Y.Z`), resolved independently of
#       `git describe`: enumerate the `vX.Y.Z` tags merged into HEAD^ and
#       version-sort them. Empty when no previous release is reachable.
#
#   changed PREV [HEAD]
#       Print `true` when any input of the image built from CHANGED_DOCKERFILE
#       changed between PREV and HEAD, `false` when none did. This is the
#       publisher's "alias or rebuild" flag, computed from the same input set
#       that `check` asserts against, so the two cannot disagree about what the
#       image contains (#2169). Inputs (environment):
#         CHANGED_DOCKERFILE  Dockerfile the image is built from   (required)
#         CHANGED_CONTEXT     its build context, repo-relative (default: .)
#         CHANGED_REPO        git checkout (default: .)
#       A Dockerfile that does not exist at PREV prints `true` (the image's
#       build definition is new). An unreadable input set fails the release
#       rather than answering — the caller treats a non-zero as "rebuild".
#
#   check
#       Assert a publisher's `changed=false` alias decision against independent
#       signals. Exits non-zero on disagreement. Inputs (environment):
#         CHECK_PATH          service tree, e.g. apps/web-ui        (required)
#         CHECK_CHANGED       the publisher's changed flag          (required)
#         CHECK_SERVICE       label used in messages (default: service)
#         CHECK_PREV_TAG      the PREV the publisher resolved (may be empty)
#         CHECK_CURRENT_TAG   the release tag being published (optional)
#         CHECK_PREV_MAP      path to the previous release map (optional)
#         CHECK_PREV_DIGEST   digest of the image we are about to alias from
#         CHECK_DOCKERFILE    Dockerfile the image is built from (default:
#                             <CHECK_PATH>/Dockerfile)
#         CHECK_CONTEXT       its build context, repo-relative (default:
#                             CHECK_PATH)
#         CHECK_REPO          git checkout (default: .)
#         CHECK_HEAD          commit to compare against (default: CHECK_CURRENT_TAG
#                             when it resolves, else HEAD)
#       Signals for changed=false:
#         1. CHECK_PREV_TAG must BE the previous release tag. A force-moved
#            rolling tag (`cli-dev`, `dev`, `mac-dev`, ...) is the #2087/0.88.0
#            root cause; failing this alone catches that incident.
#         2. The image *inputs* must diff empty against the *independently*
#            resolved previous release tag: the service tree plus every local
#            COPY/ADD source in CHECK_DOCKERFILE, resolved against
#            CHECK_CONTEXT and read from both refs (so a file only the previous
#            release copied still counts). This is what makes the check about
#            the image rather than the tree — deploy/self-hosted/entrypoint.sh
#            and apps/cli are in the server image but not under apps/server
#            (#2143).
#         3. When the previous release's map exists, the digest recorded there
#            for this service must equal CHECK_PREV_DIGEST (the digest about to
#            be aliased), i.e. the previous tag was not moved after publication.
#       A missing previous release/map degrades to the tree comparison and never
#       fails the release; a disagreement never degrades.
#
#       Signal 2 fails closed when it cannot see an input: a Dockerfile that is
#       absent at either ref, a COPY source fetched at build time (URL) or named
#       through a build variable, or a source escaping the context. Failing here
#       costs a rebuild; guessing costs a mislabelled image (#2087). It still
#       cannot see registry drift under a fixed base-image tag (`FROM
#       golang:1.26` gaining a new patch) — nothing in git can; a base-image
#       change that matters must bump the tag so the Dockerfile diff sees it.
#
#   map
#       Build release-versions.json for one release. Inputs (environment):
#         MAP_TAG         release tag, `vX.Y.Z` or `X.Y.Z`           (required)
#         MAP_SERVICES    JSON array of service descriptors          (required)
#         MAP_COMMIT      release commit (default: git rev-parse --short)
#         MAP_OUT         output path (default: release-versions.json)
#         MAP_PREV_TAG    previous release tag (default: prev-tag)
#         MAP_PREV_MAP    previous release-versions.json (optional)
#         MAP_DOCKER      docker binary (default: docker); also the tag prober,
#                         so tests can inject a stub and stay hermetic
#         MAP_REPO        git checkout (default: .)
#         MAP_HEAD        commit to compare against (default: HEAD)
#         MAP_GENERATED_AT  timestamp override (tests)
#       Each descriptor: {service, path, image, tags[], aliasable,
#       version_embedded, version_style}. `tags[0]` is inspected for the digest;
#       every other tag is *probed* and kept only when it resolves to the same
#       digest. version_style is required when version_embedded is true.
#
# Map contract (schema_version 1) — consumers (the infra repo's deploy jobs)
# MUST read this, not infer from tags:
#
#   {
#     "schema_version": 1,
#     "generated_at": "<ISO-8601 UTC>",
#     "release":         {"tag": "v0.88.0", "version": "0.88.0", "commit": "abc1234"},
#     "previous_release": {"tag": "v0.87.4"} | null,
#     "degraded": false,
#     "notes": [],
#     "services": [
#       {
#         "service": "web-ui",
#         "path": "apps/web-ui",
#         "image": "ghcr.io/owner/memory-web-ui",
#         "tags": ["ghcr.io/owner/memory-web-ui:0.88.0", ...],
#         "version": "0.87.4",                    # embedded in the published image
#         "version_source": "previous-map",       # built|previous-map|alias-tag|none
#         "digest": "sha256:...",                 # manifest digest tags resolved to
#         "source": "aliased-from:v0.87.4"        # built | aliased-from:<prev-tag>
#       }
#     ]
#   }
#
#   * version is exactly the string the image reports about itself, because the
#     deploy side compares it literally against what the container says. The
#     form is per service: the publishers embed it differently, and the
#     descriptor's version_style selects it.
#       server  -> "v0.88.0"   (version_style `tag`; publish-self-hosted-images.yml
#                 passes VERSION=${GITHUB_REF#refs/tags/} to internal/version.Version,
#                 and apps/server/domain/health returns it verbatim)
#       web-ui  -> "0.88.0"    (version_style `bare`; build-publish.yml passes
#                 VERSION=${GITHUB_REF_NAME#v})
#       cli     -> "0.88.0"    (version_style `bare`; cli.yml strips the `v`)
#     An aliased service inherits the previous map's literal string, so the form
#     is stable across releases; an alias with no previous map falls back to the
#     alias source tag in that service's own form (server: "v0.87.4").
#     Consumers MUST NOT strip or add a leading `v`: the server really does
#     self-report `v0.88.0`, and a bare record compared against it is exactly
#     the class of false failure this map exists to prevent. Only the
#     release-wide `release.version` is bare.
#   * version_source is provenance for version: `built` (this release compiled
#     it), `previous-map` (read from the previous release's map), `alias-tag`
#     (previous map unavailable; assumed equal to the alias source tag),
#     `none` (image embeds no version, e.g. the workspace base).
#   * source is `built` or `aliased-from:<prev-tag>`.
#   * tags lists only tags the registry *confirmed* at map-build time: each
#     candidate resolves to the same digest as tags[0], which is always present.
#     A candidate that 404s or resolves to a different digest is dropped (with
#     a warning), because this field is what a consumer may pin — a tag that
#     404s is worse than an absent one. Probing is not a convention guess:
#     metadata-action publishes 7-character `sha-` tags, the publisher alias
#     paths use `git rev-parse --short` (9 characters in this repo), and
#     web-ui's alias path publishes neither `latest` nor `sha-`; only the
#     registry knows which are real, and `latest` may point at another image.
#     If the registry stops answering mid-probe, the map keeps tags[0] only,
#     sets degraded and adds a note: an unverified tag is dropped rather than
#     claimed, and a map that describes the release imperfectly is still far
#     better for the deploy side than no map.
#   * `degraded` is true when any version was assumed rather than read from a
#     previous map, or when candidate-tag probing could not complete; `notes`
#     says which.
#   * version may be null only when version_source is `none`.
#
# Keep this contract stable: the infra deploy side consumes it.
set -euo pipefail

usage() {
  cat <<'EOF'
release-versions.sh — release version map + alias assertion (#2111)

Usage:
  release-versions.sh prev-tag [CURRENT_TAG]
  release-versions.sh check        # inputs via CHECK_* env vars
  release-versions.sh changed PREV [HEAD]
                                   # inputs via CHANGED_* env vars; prints
                                   # true/false for "did the image's inputs
                                   # change since PREV"
  release-versions.sh map          # inputs via MAP_* env vars

See the header of this script for the full input list and the
release-versions.json field contract (schema_version 1).
EOF
}

die() {
  echo "::error::$*" >&2
  exit 1
}

warn() {
  echo "::warning::$*" >&2
}

is_release_tag() {
  [[ "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]
}

# version_gt A B: true when A > B under version sort.
version_gt() {
  [ "$1" != "$2" ] || return 1
  [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -n 1)" = "$1" ]
}

# Resolve the previous release tag independently of `git describe`.
#
# `git describe` returns the *nearest* tag by commit distance, which a
# force-moved rolling tag can win. This enumerates every strict `vX.Y.Z` tag
# merged into HEAD^ and takes the greatest by version sort — a different
# computation, so it is a genuine second opinion on the publisher's PREV.
resolve_prev_tag() {
  local repo="$1" current="$2" head="$3" parent tag
  if ! parent="$(git -C "$repo" rev-parse --verify --quiet "${head}^")"; then
    return 0
  fi
  git -C "$repo" tag --list 'v*' --merged "$parent" 2>/dev/null \
    | while IFS= read -r tag; do
        is_release_tag "$tag" || continue
        if [ -n "$current" ] && ! version_gt "$current" "$tag"; then
          continue
        fi
        printf '%s\n' "$tag"
      done \
    | sort -V \
    | tail -n 1
}

# check_previous_map <service> <prev-release> <map-path> <alias-source-digest>
check_previous_map() {
  local service="$1" prev_release="$2" map="$3" prev_digest="$4"
  if [ -z "$map" ]; then
    return 0
  fi
  if [ ! -f "$map" ]; then
    warn "previous release version map not found at ${map}; degrading to the tree comparison (#2111)."
    return 0
  fi

  local map_tag
  map_tag="$(jq -r '.release.tag // empty' "$map" 2>/dev/null || true)"
  if [ -n "$map_tag" ] && [ "$map_tag" != "$prev_release" ]; then
    warn "previous version map is for ${map_tag}, not ${prev_release}; ignoring its recorded digest."
    return 0
  fi

  local recorded
  recorded="$(jq -r --arg s "$service" \
    '[.services[]? | select(.service == $s)][0] | .digest // empty' \
    "$map" 2>/dev/null || true)"
  if [ -z "$recorded" ]; then
    warn "previous version map has no digest for ${service}; degrading to the tree comparison."
    return 0
  fi

  if [ -n "$prev_digest" ] && [ "$recorded" != "$prev_digest" ]; then
    die "alias source digest ${prev_digest} != digest ${recorded} recorded for ${service} in ${map_tag:-the previous release}; the previous release tag moved after publication (#2111)."
  fi
}

# dockerfile_inputs <repo> <ref> <dockerfile> <context>
#
# Print, one per line, the repo-relative paths the image built from <dockerfile>
# at <ref> reads out of the checkout: the Dockerfile itself plus every local
# COPY/ADD source, resolved against the build <context>.
#
# Unresolvable inputs are an error (rc 1, reason on stderr), never a silent skip:
# a check that cannot see an input must refuse the alias instead of assuming the
# input was unchanged (#2143). Exemptions that are genuinely covered elsewhere:
#   * `--from=<stage>` copies — the stage's own COPY lines are inputs already, and
#     a registry stage is pinned by the (diffed) Dockerfile line;
#   * Dockerfile comments and non-COPY instructions — ENV/ARG/RUN cannot read the
#     checkout, and RUN network fetches are pinned by their line in the file.
dockerfile_inputs() {
  local repo="$1" ref="$2" dockerfile="$3" context="$4"
  local content line joined trimmed rest token source flag mode ignore payload after
  local -a words sources kept
  local stage

  # A symlinked Dockerfile reads as its target *path*, not its instructions:
  # `git show` returns the link text with rc 0, so the parse would find no COPY
  # lines and silently report an empty input set. Fail closed instead (#2169).
  mode="$(git -C "$repo" ls-tree "$ref" -- "$dockerfile" 2>/dev/null | awk '{print $1}')"
  if [ "$mode" = "120000" ]; then
    echo "${dockerfile} at ${ref}: is a symlink, so its instructions cannot be resolved" >&2
    return 1
  fi
  if ! content="$(git -C "$repo" show "${ref}:${dockerfile}" 2>/dev/null)"; then
    echo "cannot read ${dockerfile} at ${ref}: the image's own Dockerfile is not in that tree" >&2
    return 1
  fi
  printf '%s\n' "$dockerfile"

  # The context's .dockerignore decides which files the build context even
  # contains, so a change there can change the image. Conservative on purpose: a
  # false positive costs a rebuild, a false negative ships a stale alias (#2169).
  ignore=".dockerignore"
  case "$context" in
    '' | .) ;;
    *) ignore="${context%/}/.dockerignore" ;;
  esac
  if [ "$ignore" != "$dockerfile" ] \
    && git -C "$repo" cat-file -e "${ref}:${ignore}" 2>/dev/null; then
    printf '%s\n' "$ignore"
  fi

  # Dockerfile line continuations: a `\` at end of line joins the next, so a
  # COPY's sources can span lines. Joining first means the parse sees the whole
  # instruction; an unterminated continuation is refused rather than guessed.
  local pending=""
  while IFS= read -r line; do
    line="${line%$'\r'}"
    trimmed="${line#"${line%%[![:space:]]*}"}"
    if [[ "$trimmed" == '#'* ]]; then
      # Docker ignores a comment line wherever it appears, including inside a
      # continuation, where it does not end the instruction. Skipping it before
      # the continuation test also stops a comment that happens to *end* in a
      # backslash from being joined to — and swallowing — the next instruction:
      # a comment is never part of an instruction (#2169).
      continue
    fi
    if [[ "$line" == *\\ ]]; then
      pending+="${line%\\} "
      continue
    fi
    joined="${pending}${line}"
    pending=""
    joined="${joined#"${joined%%[![:space:]]*}"}"
    [ -n "$joined" ] || continue
    read -ra words <<<"$joined"
    case "${words[0],,}" in
      copy | add) ;;
      *) continue ;;
    esac

    rest="${joined#"${words[0]}"}"
    rest="${rest#"${rest%%[![:space:]]*}"}"

    # Leading flags come before the sources and, for the JSON form, before the
    # bracket: `COPY --chmod=755 ["a", "b"] dst` is a JSON copy with a flag, and
    # detecting the form on the raw text made the source `["a",` (#2169).
    # --from= means a cross-stage copy: the source stage's own COPY lines are in
    # this list already, and a registry stage is pinned by the Dockerfile line.
    stage=0
    while [[ "$rest" == --* ]]; do
      if [[ "$rest" != *[[:space:]]* ]]; then
        echo "${dockerfile} at ${ref}: cannot parse '$(printf '%s' "$joined")'" >&2
        return 1
      fi
      flag="${rest%%[[:space:]]*}"
      case "$flag" in
        --from=*) stage=1 ;;
      esac
      rest="${rest#*[[:space:]]}"
      rest="${rest#"${rest%%[![:space:]]*}"}"
    done
    [ "$stage" -eq 0 ] || continue

    sources=()
    if [[ "$rest" == \[* ]]; then
      # Docker's JSON form is COPY ["<src>", ..., "<dest>"]: the destination is
      # the LAST array element, and the array closes at the last ']' of the
      # instruction (a filename may itself contain ']' or ','). Cutting at the
      # first bracket, or splitting the body on commas, truncated the list and
      # lost inputs (#2169, #2175). Text after the closing bracket is ignored
      # here when resolving inputs — Docker rejects such a line outright
      # (parseMaybeJSONToList falls back to shell splitting, which cannot build),
      # so there is no output to be wrong about. A body jq cannot read is
      # refused rather than guessed.
      after="${rest##*\]}"
      payload="${rest%"$after"}"
      if ! payload="$(printf '%s' "$payload" | jq -er 'if type == "array" then .[] else error("not a JSON array") end' 2>/dev/null)"; then
        echo "${dockerfile} at ${ref}: cannot parse the JSON form of '$(printf '%s' "$joined")'" >&2
        return 1
      fi
      mapfile -t words <<<"$payload"
      sources=("${words[@]}")
    else
      read -ra words <<<"$rest"
      sources=("${words[@]}")
    fi

    kept=()
    for token in "${sources[@]}"; do
      token="${token#"${token%%[![:space:]]*}"}"
      token="${token%"${token##*[![:space:]]}"}"
      [ -n "$token" ] || continue
      case "$token" in
        --from=*) stage=1 ;;
        --*) ;;
        *) kept+=("$token") ;;
      esac
    done
    [ "$stage" -eq 0 ] || continue
    # The last token is the destination; everything before it is read.
    if [ "${#kept[@]}" -lt 2 ]; then
      echo "${dockerfile} at ${ref}: cannot parse '$(printf '%s' "$joined")'" >&2
      return 1
    fi

    for token in "${kept[@]:0:${#kept[@]}-1}"; do
      case "$token" in
        *://* | git@*)
          echo "${dockerfile} at ${ref}: '${token}' is fetched at build time, so its contents cannot be diffed" >&2
          return 1
          ;;
        *'$'*)
          echo "${dockerfile} at ${ref}: '${token}' is a build variable, so its path cannot be resolved" >&2
          return 1
          ;;
        .. | ../* | */../* | */..)
          echo "${dockerfile} at ${ref}: '${token}' escapes the build context" >&2
          return 1
          ;;
      esac
      source="${token#./}"
      source="${source#/}"
      source="${source%/}"
      case "$source" in
        '' | .) joined="$context" ;;
        *) joined="${context%/}/${source}" ;;
      esac
      joined="${joined#./}"
      while [[ "$joined" == *//* ]]; do joined="${joined//\/\//\/}"; done
      printf '%s\n' "$joined"
    done
  done <<<"$content"

  if [ -n "$pending" ]; then
    echo "${dockerfile} at ${ref}: unterminated line continuation, so the last instruction's sources cannot be parsed" >&2
    return 1
  fi
}

# run_changed PREV_TAG [HEAD_REF]: print true when any input of the image built
# from CHANGED_DOCKERFILE changed since PREV_TAG, false when none did.
#
# The publisher used to decide "alias or rebuild" by diffing the service *tree*
# (apps/server), while the image is built from deploy/self-hosted/ with the repo
# root as its context and also contains apps/cli and the migrations. A CLI-only
# release therefore said "unchanged", and the alias assertion (#2143) — which
# does diff the image inputs — then refused it: fail-closed, but a legitimate
# release turned red. Both sides use dockerfile_inputs now, so the flag and the
# assertion agree by construction; the only remaining difference is which
# previous tag each resolved, which is exactly what the assertion exists to
# catch (#2169).
run_changed() {
  local repo="${CHANGED_REPO:-.}" prev="${1:-}" head="${2:-HEAD}"
  local dockerfile="${CHANGED_DOCKERFILE:-}" context="${CHANGED_CONTEXT:-.}"
  local at_prev at_head diff
  local -a inputs=()

  [ -n "$prev" ] || die "changed: a previous tag is required"
  [ -n "$dockerfile" ] || die "changed: CHANGED_DOCKERFILE is required"
  if ! git -C "$repo" cat-file -e "${prev}:${dockerfile}" 2>/dev/null; then
    # The image's Dockerfile did not exist at the previous tag, so its build
    # definition is new: there is no previous image that could be aliased.
    echo true
    return 0
  fi
  at_prev="$(dockerfile_inputs "$repo" "$prev" "$dockerfile" "$context")" \
    || die "changed: cannot resolve the inputs of ${dockerfile} at ${prev}"
  at_head="$(dockerfile_inputs "$repo" "$head" "$dockerfile" "$context")" \
    || die "changed: cannot resolve the inputs of ${dockerfile} at ${head}"
  mapfile -t inputs < <(printf '%s\n%s\n' "$at_prev" "$at_head" | awk 'NF && !seen[$0]++')
  [ "${#inputs[@]}" -gt 0 ] || die "changed: ${dockerfile} declares no inputs"

  diff="$(git -C "$repo" diff --name-only "$prev" "$head" -- "${inputs[@]}")"
  if [ -n "$diff" ]; then
    echo true
  else
    echo false
  fi
}
run_check() {
  local repo="${CHECK_REPO:-.}"
  local service="${CHECK_SERVICE:-service}"
  local path="${CHECK_PATH:?CHECK_PATH is required}"
  local changed="${CHECK_CHANGED:?CHECK_CHANGED is required}"
  local head="${CHECK_HEAD:-}"
  local current="${CHECK_CURRENT_TAG:-}"
  local publisher_prev="${CHECK_PREV_TAG:-}"
  local prev_map="${CHECK_PREV_MAP:-}"
  local prev_digest="${CHECK_PREV_DIGEST:-}"

  case "$changed" in
    true | false) ;;
    *) die "CHECK_CHANGED must be 'true' or 'false', got '${changed}'" ;;
  esac

  # Prefer the release tag itself over whatever HEAD happens to be: the check is
  # about the release being published, and a caller that passes the tag should
  # get that commit even from a checkout that has moved on.
  if [ -z "$head" ]; then
    if [ -n "$current" ] && git -C "$repo" rev-parse --verify --quiet "${current}^{commit}" >/dev/null; then
      head="$current"
    else
      head="HEAD"
    fi
  fi

  local prev_release
  prev_release="$(resolve_prev_tag "$repo" "$current" "$head")"

  # The image's Dockerfile and build context. Both the alias assertion (signal 2)
  # and the changed=true warning compare the set of paths the Dockerfile copies —
  # the same input set the publisher's `changed` computes (#2169).
  local dockerfile="${CHECK_DOCKERFILE:-${path%/}/Dockerfile}"
  local build_context="${CHECK_CONTEXT:-${path%/}}"

  if [ "$changed" = "false" ]; then
    if [ -z "$prev_release" ]; then
      warn "no previous release tag reachable from ${head}^; cannot independently verify the alias decision for ${service}. Degrading to the publisher's changed=false."
      echo "alias=unverified service=${service}"
      return 0
    fi

    # Signal 1: the alias source must be the previous release tag. This is the
    # v0.88.0 (#2087) case: PREV resolved to a force-moved rolling tag.
    if [ -n "$publisher_prev" ] && [ "$publisher_prev" != "$prev_release" ]; then
      die "alias decision for ${service} used '${publisher_prev}', but the previous release tag is '${prev_release}'; refusing to alias an image that is not the previous release (#2111)."
    fi

    # Signal 2: an independent diff of the *image inputs* between the previous
    # release and HEAD — every path the Dockerfile copies out of the checkout,
    # read from both refs. Computed here, not from the publisher's PREV. A
    # path-scoped diff alone missed inputs outside the tree
    # (deploy/self-hosted/entrypoint.sh, apps/cli for the server image, go.work),
    # so an image could change while the check said "alias" (#2143).
    #
    # Inputs only, deliberately: the service *tree* is not compared. An image
    # whose Dockerfile copies a subset of its tree (web-ui copies gateway/ and
    # memory_bridge/) does not change when a file it never copies does — an e2e
    # spec, a taskfile — so adding $path here made a legitimate alias read as a
    # disagreement (#2175). This is also exactly what `changed` diffs, so the
    # flag and the assertion describe the same thing.
    local prev_inputs head_inputs
    if ! prev_inputs="$(dockerfile_inputs "$repo" "$prev_release" "$dockerfile" "$build_context" 2>&1)"; then
      die "cannot verify the alias decision for ${service}: ${prev_inputs}"
    fi
    if ! head_inputs="$(dockerfile_inputs "$repo" "$head" "$dockerfile" "$build_context" 2>&1)"; then
      die "cannot verify the alias decision for ${service}: ${head_inputs}"
    fi

    local -a inputs
    mapfile -t inputs < <(
      { printf '%s\n' "$prev_inputs" "$head_inputs"; } \
        | sed '/^[[:space:]]*$/d' | sort -u
    )
    local stat
    if ! stat="$(git -C "$repo" diff --stat "$prev_release" "$head" -- "${inputs[@]}")"; then
      die "could not diff ${prev_release}..${head} for ${path}"
    fi
    if [ -n "$stat" ]; then
      {
        echo "::error::alias disagreement for ${service}: changed=false, but the image inputs (${dockerfile} and every path it copies) differ between ${prev_release} and ${head} (#2111, #2143)."
        printf '%s\n' "$stat"
      } >&2
      exit 1
    fi

    # Signal 3 (optional): the previous release's recorded digest.
    check_previous_map "$service" "$prev_release" "$prev_map" "$prev_digest"

    echo "alias=ok service=${service} previous=${prev_release}"
    return 0
  fi

  # changed=true: a rebuild of an identical image is safe (just wasteful), so it
  # is a warning, never a release failure. Compare the image inputs, not the
  # service tree: a CLI-only change leaves apps/server untouched and does change
  # the server image, so a tree diff would warn about a necessary rebuild (#2169).
  if [ -n "$prev_release" ]; then
    local prev_inputs head_inputs
    if prev_inputs="$(dockerfile_inputs "$repo" "$prev_release" "$dockerfile" "$build_context" 2>/dev/null)" \
      && head_inputs="$(dockerfile_inputs "$repo" "$head" "$dockerfile" "$build_context" 2>/dev/null)"; then
      local -a inputs
      mapfile -t inputs < <(
        { printf '%s\n' "$prev_inputs" "$head_inputs"; } \
          | sed '/^[[:space:]]*$/d' | sort -u
      )
      local stat
      if [ "${#inputs[@]}" -gt 0 ] \
        && stat="$(git -C "$repo" diff --stat "$prev_release" "$head" -- "${inputs[@]}")" \
        && [ -z "$stat" ]; then
        warn "changed=true but the image inputs are identical between ${prev_release} and ${head}; rebuilding is safe but unnecessary (#2111)."
      fi
    fi
  fi
  echo "alias=rebuild service=${service}"
}

inspect_digest() {
  local docker="$1" ref="$2" out
  out="$("$docker" buildx imagetools inspect --format '{{json .Manifest}}' "$ref" 2>/dev/null)" || return 1
  jq -er '.digest // empty' <<<"$out" 2>/dev/null
}

# probe_tags <docker> <authoritative-ref> <authoritative-digest> <candidate>...
#
# Print the candidate tags that resolve to <authoritative-digest>, one per line,
# starting with <authoritative-ref> (the caller already resolved that one, so it
# is never re-verified here). A candidate that 404s, resolves to another digest,
# or cannot be read is dropped with a warning: `tags` is what a consumer may
# pin, so an absent tag is better than a claimed one.
#
# Returns 2 when the registry itself stops answering (a re-probe of the
# authoritative ref fails). The caller then keeps only the authoritative tag
# instead of recording absences it could not prove. The docker binary is
# MAP_DOCKER, so tests inject a stub and never touch a registry.
probe_tags() {
  local docker="$1" ref="$2" digest="$3"
  shift 3
  local candidate cand_digest recheck
  printf '%s\n' "$ref"
  for candidate in "$@"; do
    [ "$candidate" = "$ref" ] && continue
    if cand_digest="$(inspect_digest "$docker" "$candidate")" \
      && [ -n "$cand_digest" ] && [ "$cand_digest" = "$digest" ]; then
      printf '%s\n' "$candidate"
      continue
    fi
    # A failed read is only evidence of absence while the registry answers.
    if ! recheck="$(inspect_digest "$docker" "$ref")" || [ -z "$recheck" ]; then
      return 2
    fi
    warn "dropping tag ${candidate}: it does not resolve to the ${ref} digest (${digest})"
  done
}

# map_version <map> <prev-release> <service>: recorded embedded version or empty.
map_version() {
  local map="$1" prev="$2" service="$3" map_tag
  if [ -z "$map" ] || [ ! -f "$map" ]; then
    return 0
  fi
  map_tag="$(jq -r '.release.tag // empty' "$map" 2>/dev/null || true)"
  if [ -n "$map_tag" ] && [ "$map_tag" != "$prev" ]; then
    return 0
  fi
  jq -r --arg s "$service" \
    '[.services[]? | select(.service == $s)][0] | if . == null then empty elif .version == null then empty else .version end' \
    "$map" 2>/dev/null || true
}

run_map() {
  local tag="${MAP_TAG:?MAP_TAG is required}"
  local services_file="${MAP_SERVICES:?MAP_SERVICES is required}"
  local repo="${MAP_REPO:-.}"
  local head="${MAP_HEAD:-HEAD}"
  local out="${MAP_OUT:-release-versions.json}"
  local docker="${MAP_DOCKER:-docker}"
  local prev_map="${MAP_PREV_MAP:-}"
  local generated_at="${MAP_GENERATED_AT:-}"

  local version="${tag#v}"
  if ! is_release_tag "v${version}"; then
    die "MAP_TAG must be vMAJOR.MINOR.PATCH (got '${tag}')"
  fi
  local release_tag="v${version}"

  [ -f "$services_file" ] || die "services file not found: ${services_file}"
  jq -e 'type == "array" and length > 0' "$services_file" >/dev/null \
    || die "services file must be a non-empty JSON array"

  local commit="${MAP_COMMIT:-}"
  if [ -z "$commit" ]; then
    commit="$(git -C "$repo" rev-parse --short HEAD)"
  fi
  if [ -z "$generated_at" ]; then
    generated_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  fi

  local prev_release="${MAP_PREV_TAG:-}"
  if [ -z "$prev_release" ]; then
    prev_release="$(resolve_prev_tag "$repo" "$release_tag" "$head")"
  fi

  local entries notes=() degraded=0
  entries="$(mktemp)"
  # Expand the path now: `entries` is a function-local and is gone when EXIT
  # fires (and `set -u` would make an unexpanded reference an error).
  # shellcheck disable=SC2064
  trap "rm -f $(printf '%q' "$entries")" EXIT

  local svc_json service path image aliasable version_embedded version_style
  local tags_json digest_ref
  local current_digest prev_digest source entry_version version_source
  local -a candidate_tags=()
  local kept_tags kept_rc candidate
  while IFS= read -r svc_json; do
    service="$(jq -r '.service // empty' <<<"$svc_json")"
    path="$(jq -r '.path // empty' <<<"$svc_json")"
    image="$(jq -r '.image // empty' <<<"$svc_json")"
    aliasable="$(jq -r 'if has("aliasable") then .aliasable else false end' <<<"$svc_json")"
    version_embedded="$(jq -r 'if has("version_embedded") then .version_embedded else true end' <<<"$svc_json")"
    version_style="$(jq -r '.version_style // empty' <<<"$svc_json")"
    digest_ref="$(jq -r '.tags[0] // empty' <<<"$svc_json")"

    [ -n "$service" ] || die "service descriptor is missing 'service': ${svc_json}"
    [ -n "$image" ] || die "service '${service}' is missing 'image'"
    [ -n "$digest_ref" ] || die "service '${service}' is missing 'tags[0]'"

    if [ "$version_embedded" = "true" ]; then
      case "$version_style" in
        tag | bare) ;;
        *) die "service '${service}' must set version_style to 'tag' or 'bare' (the form the image self-reports)" ;;
      esac
    fi

    if ! current_digest="$(inspect_digest "$docker" "$digest_ref")" || [ -z "$current_digest" ]; then
      die "could not resolve the published digest for ${digest_ref}; has the image been published?"
    fi

    # Keep only tags the registry confirms point at the same image. The
    # publishers disagree on conventions (7-char metadata-action `sha-` vs
    # 9-char `git rev-parse --short`, alias paths that publish neither `latest`
    # nor `sha-`), so probing is the only honest source; a listed tag that 404s
    # is worse for a consumer than an absent one.
    candidate_tags=()
    while IFS= read -r candidate; do
      if [ -n "$candidate" ] && [ "$candidate" != "$digest_ref" ]; then
        candidate_tags+=("$candidate")
      fi
    done < <(jq -r '.tags // [] | .[]' <<<"$svc_json")
    kept_rc=0
    if [ "${#candidate_tags[@]}" -gt 0 ]; then
      kept_tags="$(probe_tags "$docker" "$digest_ref" "$current_digest" "${candidate_tags[@]}")" || kept_rc=$?
    else
      kept_tags="$digest_ref"
    fi
    if [ "$kept_rc" -eq 2 ]; then
      kept_tags="$digest_ref"
      degraded=1
      notes+=("${service}: registry stopped resolving candidate tags; recorded tags[0] only")
    fi
    tags_json="$(printf '%s\n' "$kept_tags" | jq -R -s 'split("\n") | map(select(length > 0))')"

    prev_digest=""
    if [ "$aliasable" = "true" ] && [ -n "$prev_release" ]; then
      prev_digest="$(inspect_digest "$docker" "${image}:${prev_release#v}")" || prev_digest=""
    fi

    source="built"
    version_source="built"
    entry_version="$version"
    if [ "$version_embedded" = "true" ] && [ "$version_style" = "tag" ]; then
      # The server embeds the tag verbatim (v0.88.0), not the bare version.
      entry_version="$release_tag"
    fi
    if [ "$version_embedded" != "true" ]; then
      entry_version=""
      version_source="none"
    fi

    if [ "$aliasable" = "true" ] && [ -n "$prev_digest" ] && [ "$prev_digest" = "$current_digest" ]; then
      source="aliased-from:${prev_release}"
      if [ "$version_embedded" != "true" ]; then
        entry_version=""
        version_source="none"
      else
        entry_version="$(map_version "$prev_map" "$prev_release" "$service")"
        if [ -n "$entry_version" ]; then
          version_source="previous-map"
        else
          # Older release predates the map: assume the alias source tag is the
          # embedded version, in this service's own form. Recorded explicitly so
          # consumers can see it is an assumption, and never a failure.
          if [ "$version_style" = "tag" ]; then
            entry_version="$prev_release"
          else
            entry_version="${prev_release#v}"
          fi
          version_source="alias-tag"
          degraded=1
          notes+=("${service}: no previous release map; version assumed equal to alias source tag ${prev_release}")
        fi
      fi
    fi

    local version_json
    if [ -n "$entry_version" ]; then
      version_json="$(jq -Rn --arg v "$entry_version" '$v')"
    else
      version_json="null"
    fi

    jq -n \
      --arg service "$service" \
      --arg path "$path" \
      --arg image "$image" \
      --argjson tags "$tags_json" \
      --argjson version "$version_json" \
      --arg version_source "$version_source" \
      --arg digest "$current_digest" \
      --arg source "$source" \
      '{
        service: $service,
        path: $path,
        image: $image,
        tags: $tags,
        version: $version,
        version_source: $version_source,
        digest: $digest,
        source: $source
      }' >>"$entries"
  done < <(jq -c '.[]' "$services_file")

  local services_json prev_json notes_json degraded_json
  services_json="$(jq -s '.' "$entries")"
  if [ -n "$prev_release" ]; then
    prev_json="$(jq -Rn --arg t "$prev_release" '{tag: $t}')"
  else
    prev_json="null"
  fi
  notes_json="$(printf '%s\n' "${notes[@]:-}" | jq -R -s 'split("\n") | map(select(length > 0))')"
  if [ "$degraded" -eq 1 ]; then degraded_json="true"; else degraded_json="false"; fi

  jq -n \
    --argjson services "$services_json" \
    --arg tag "$release_tag" \
    --arg version "$version" \
    --arg commit "$commit" \
    --arg generated_at "$generated_at" \
    --argjson previous_release "$prev_json" \
    --argjson degraded "$degraded_json" \
    --argjson notes "$notes_json" \
    '{
      schema_version: 1,
      generated_at: $generated_at,
      release: {tag: $tag, version: $version, commit: $commit},
      previous_release: $previous_release,
      degraded: $degraded,
      notes: $notes,
      services: $services
    }' >"$out"

  local aliased
  aliased="$(jq '[.services[] | select(.source | startswith("aliased-from:"))] | length' "$out")"
  echo "map=ok release=${release_tag} services=$(jq '.services | length' "$out") aliased=${aliased} degraded=${degraded_json} out=${out}"
}

cmd="${1:-}"
case "$cmd" in
  prev-tag)
    shift || true
    resolve_prev_tag "${MAP_REPO:-${CHECK_REPO:-.}}" \
      "${1:-${CHECK_CURRENT_TAG:-}}" "${CHECK_HEAD:-HEAD}"
    ;;
  check)
    run_check
    ;;
  changed)
    shift || true
    run_changed "${1:-}" "${2:-HEAD}"
    ;;
  map)
    run_map
    ;;
  help | -h | --help)
    usage
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
