#!/usr/bin/env bash
# Fail when this branch and the live base branch add the same package-level
# declaration to the same Go package (#945).
#
# WHY. Two pull requests can each be green on their own head and still leave the
# base branch uncompilable once both merge -- the only build of the COMBINATION
# is the post-merge one. #886 and #890 both added
# `TestRetryAllConversions{RedirectIsScopedWithoutMiddleware,PlainFormRedirect,
# ScopesTheWireRedirect}` to `apps/web-ui/gateway`; neither diff conflicted
# textually, both `ci` runs passed on their own head, and `dev`'s next push died
# with:
#
#   TestRetryAllConversionsRedirectIsScopedWithoutMiddleware redeclared in this block
#
# `ci` is a required status check, so the red base blocked every other merge
# (12 open PRs, one reviewed and ready) until #944 repaired it by hand.
#
# This is the Go analogue of scripts/preflight/migration-number.sh, which
# guards the same shape for migration versions (00180: #977 vs #981, 00183:
# #1057 vs #1053) by comparing a branch's new files with the LIVE base branch.
# Both guards answer one question on the PR side: "does something that landed on
# the base while this branch was open already declare what this branch declares
# in the same package?" A textual diff cannot see that; the compiler can, but
# only after the merge.
#
# WHY NOT COMPILE THE MERGED TREE. The issue's option 3 sketches exactly that,
# scoped to the packages whose files changed. In this repository that is not
# cheap the way it sounds: `apps/web-ui` embeds a CSS bundle produced by
# tailwind and templ output, so `go build` of the very package in the incident
# fails for missing generated files unless generation runs first (~minutes, and
# it is why the gateway block at the end of `ci` generates before it compiles).
# A package-level declaration comparison needs no toolchain, no generated files
# and no network beyond the base fetch, and it names the class precisely:
#
#   * funcs, types, vars, consts share one package-level namespace, so the same
#     identifier declared in two files of one package is a compile error; a spec
#     can name several identifiers (`var First, Second = 1, 2`) and all count;
#   * methods live in the receiver's namespace, so the key is `Recv.Name`;
#   * `pkg` and the external test package `pkg_test` share a directory but not a
#     namespace, so the package clause scopes every key;
#   * `init` and `_` may legally repeat and are ignored.
#
# LIMITS, stated plainly, so nobody over-trusts it:
#   * It only sees declarations that BOTH sides introduce. A collision with a
#     PR merged after this branch's last run is invisible until the branch is
#     updated or the run is repeated -- exactly the gap that makes "require
#     branches to be up to date" (or a merge queue) the complete fix; this
#     guard closes the window for every PR that is rebased/re-run, and for
#     every branch already behind the base when it runs.
#   * Files under a build constraint (GOOS/GOARCH filename suffix, `//go:build`
#     or `// +build`) are skipped: the same name in mutually exclusive files is
#     legal, so comparing them would produce false failures.
#   * It is a declaration scan, not a parser. It relies on gofmt, which `ci`
#     enforces for every tracked Go file, so package-level declarations start in
#     column 0 and grouped `type`/`var`/`const` entries are indented.
#   * A multi-line raw string whose continuation line starts with `type Foo` at
#     column 0 could contribute a phantom name. Such a name collides only if the
#     base introduced the identical phantom in the same package, so the cost is
#     bounded and the failure message shows both files.
#
# COST. A collision needs both sides to have changed the same package, so the
# guard intersects the two changed-package sets first: a PR that touches
# packages the base did not costs two diffs and nothing else. Only overlapping
# packages are extracted and scanned (~2.1 s for apps/web-ui/gateway's 443 files,
# ~2.4 s for apps/server/domain's 1618, with three trees each).
#
# Offline is tolerated the same way as base-check.sh and migration-number.sh:
# fall back to a cached base ref, or warn and skip when none exists.
#
# Its hermetic suite is scripts/preflight/go-decl-collision.test.sh (throwaway
# git repositories, no network); run it directly with any change to this file.
#
# Usage:
#   go-decl-collision.sh [--base <ref>] [--head <ref>]
#
#   (no arguments)   base = origin/${PREFLIGHT_BASE:-${GITHUB_BASE_REF:-dev}}
#                    head = HEAD, or HEAD^2 in a pull_request CI checkout (where
#                    HEAD is GitHub's synthetic merge commit, not the PR head)
#   --base <ref>     compare against this ref instead; it is not fetched and it
#                    must resolve (a typo is an error, not a silent skip)
#   --head <ref>     scan this ref instead of HEAD/HEAD^2 (used by the test)
# Byte order keeps sort and comm agreeing on locale-poor runners.
export LC_ALL=C

set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

usage() {
  printf 'Usage: %s [--base <ref>] [--head <ref>]\n' "${0##*/}"
}

die() {
  printf 'go-decl-collision: %s\n' "$*" >&2
  exit 1
}

base_ref=""
head_ref=""
base_branch="${PREFLIGHT_BASE:-${GITHUB_BASE_REF:-dev}}"

while (($#)); do
  case "$1" in
    --base)
      (($# >= 2)) || {
        usage >&2
        exit 2
      }
      base_ref=$2
      shift 2
      ;;
    --head)
      (($# >= 2)) || {
        usage >&2
        exit 2
      }
      head_ref=$2
      shift 2
      ;;
    --help | -h)
      usage
      exit 0
      ;;
    *)
      printf 'go-decl-collision: unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

# ---- resolve the two sides --------------------------------------------------

if [[ -z "$base_ref" ]]; then
  base_ref="origin/${base_branch}"
  # Refresh first: a stale local base ref would miss exactly the collision this
  # guard exists to catch. Tolerate no-network like base-check.sh.
  #
  # An explicit refspec, never the bare branch name (#1158): a tag with the
  # same name as the branch makes `git fetch origin <branch>` resolve the TAG,
  # print `* tag <branch> -> FETCH_HEAD` and exit 0 (even with --no-tags),
  # leaving origin/<branch> stale -- so this guard would compare against the
  # old tip and miss the very collision it exists to catch. The leading `+`
  # lets a force-pushed base refresh instead of failing the fetch.
  if ! git fetch --quiet --no-tags origin \
       "+refs/heads/$base_branch:refs/remotes/origin/$base_branch" 2>/dev/null; then
    if git rev-parse --verify --quiet "$base_ref" >/dev/null 2>&1; then
      printf 'go-decl-collision: WARNING could not reach origin; using cached %s\n' "$base_ref" >&2
    else
      printf 'go-decl-collision: WARNING could not reach origin and no cached %s; skipping\n' "$base_ref" >&2
      exit 0
    fi
  fi
  if ! git rev-parse --verify --quiet "$base_ref" >/dev/null 2>&1; then
    printf 'go-decl-collision: WARNING %s unavailable; skipping\n' "$base_ref" >&2
    exit 0
  fi
elif ! git rev-parse --verify --quiet "${base_ref}^{commit}" >/dev/null 2>&1; then
  die "--base ${base_ref} does not resolve to a commit"
fi

if [[ -z "$head_ref" ]]; then
  # `actions/checkout` hands a pull_request job GitHub's synthetic merge commit
  # as HEAD; its second parent is the PR head. Comparing the merge commit itself
  # would hide the base side of every collision.
  if [[ "${GITHUB_EVENT_NAME:-}" == "pull_request" ]] &&
    git rev-parse --verify --quiet 'HEAD^2' >/dev/null 2>&1; then
    head_ref='HEAD^2'
  else
    head_ref='HEAD'
  fi
elif ! git rev-parse --verify --quiet "${head_ref}^{commit}" >/dev/null 2>&1; then
  die "--head ${head_ref} does not resolve to a commit"
fi

if ! merge_base=$(git merge-base "$head_ref" "$base_ref" 2>/dev/null); then
  printf 'go-decl-collision: WARNING %s and %s share no history; skipping\n' "$head_ref" "$base_ref" >&2
  exit 0
fi

printf 'go-decl-collision: head=%s base=%s merge-base=%s\n' \
  "$(git rev-parse --short "$head_ref")" \
  "$(git rev-parse --short "$base_ref")" \
  "$(git rev-parse --short "$merge_base")"

# Package directories containing Go files this branch added or modified. Files
# only this branch touches are irrelevant to a *cross-branch* collision, but
# their packages are where one can happen, so the package is the unit.
changed=$(git diff --name-only --diff-filter=ACMRT "$merge_base" "$head_ref" -- '*.go' |
  grep -v '/testdata/' || true)
if [[ -z "$changed" ]]; then
  printf 'go-decl-collision: OK (no Go files changed against %s)\n' "$base_ref"
  exit 0
fi
packages=$(printf '%s\n' "$changed" |
  awk -F/ -v OFS=/ 'NF == 0 { next } { if (NF == 1) { print "." } else { NF--; print } }' | sort -u)

# A collision needs BOTH sides to change the same package. Intersecting the two
# changed-package sets first makes the common PR (touching packages the base did
# not) cost two diffs and nothing else -- no archive, no scan.
base_changed=$(git diff --name-only --diff-filter=ACMRT "$merge_base" "$base_ref" -- '*.go' |
  grep -v '/testdata/' || true)
base_packages=$(printf '%s\n' "$base_changed" |
  awk -F/ -v OFS=/ 'NF == 0 { next } { if (NF == 1) { print "." } else { NF--; print } }' | sort -u)
packages=$(comm -12   <(printf '%s\n' "$packages" | sed '/^$/d')   <(printf '%s\n' "$base_packages" | sed '/^$/d'))
if [[ -z "$packages" ]]; then
  printf 'go-decl-collision: OK (no Go package changed on both sides of %s)\n' "$base_ref"
  exit 0
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Extract package-level identifiers from the Go files of one package in one
# tree. Output: "<key>\t<path>", where <key> is the identifier for package-level
# declarations or "method <RecvType>.<Name>" for methods.
cat >"$tmp/extract.awk" <<'AWK'
# Package-level identifiers of one Go file, one "<key>\t<path>" per line.
# Keys are the identifier for funcs/types/vars/consts (one package namespace) and
# "method <RecvType>.<Name>" for methods (one namespace per receiver type).
BEGIN { group = "" }
FNR == 1 { group = ""; pkg = "" }
{
  line = $0
  # The package clause scopes every key: `pkg` and `pkg_test` are different
  # packages that share a directory, so `setup` in each is not a collision.
  if (pkg == "" && match(line, /^package[ \t]+[A-Za-z_][A-Za-z0-9_]*/)) {
    pkg = substr(line, RSTART, RLENGTH)
    sub(/^package[ \t]+/, "", pkg)
    next
  }
  if (group != "") {
    # Member of a grouped type/var/const block: names are indented and may be a
    # comma list (`Foo, Bar = 1, 2`).
    if (line ~ /^\)/) {
      group = ""
      next
    }
    if (match(line, /^[ \t]+/)) {
      names(substr(line, RLENGTH + 1))
    }
    next
  }
  if (line ~ /^(type|var|const)[ \t]*\(/) {
    group = "grouped"
    next
  }
  if (substr(line, 1, 6) == "func (") {
    # Method: key by receiver type, because `Serve` on two types is legal.
    close_paren = index(line, ")")
    if (close_paren > 0) {
      recv = substr(line, 7, close_paren - 7)
      rest = substr(line, close_paren + 1)
      sub(/^[ \t]+/, "", rest)
      if (match(rest, /^[A-Za-z_][A-Za-z0-9_]*/)) {
        emit("method " recv_type(recv) "." substr(rest, RSTART, RLENGTH))
      }
    }
    next
  }
  if (substr(line, 1, 5) == "func ") {
    rest = substr(line, 6)
    if (match(rest, /^[A-Za-z_][A-Za-z0-9_]*/)) emit(substr(rest, RSTART, RLENGTH))
    next
  }
  if (match(line, /^(type|var|const)[ \t]+/)) {
    keyword = substr(line, RSTART, RLENGTH)
    if (keyword ~ /^type/) {
      rest = substr(line, RLENGTH + 1)
      if (match(rest, /^[A-Za-z_][A-Za-z0-9_]*/)) emit(substr(rest, RSTART, RLENGTH))
    } else {
      # `var Foo = 1`, `var Foo, Bar = 1, 2`, `var Foo *T`: the names end at
      # the first `=`, and a spec may name several identifiers before its type.
      names(substr(line, RLENGTH + 1))
    }
  }
}
# Emit every identifier in a comma-separated name list up to any `= value`.
function names(text,   parts, n, i) {
  sub(/[ \t]*=.*/, "", text)
  n = split(text, parts, ",")
  for (i = 1; i <= n; i++) {
    if (match(parts[i], /[A-Za-z_][A-Za-z0-9_]*/)) {
      emit(substr(parts[i], RSTART, RLENGTH))
    }
  }
}
# Receiver text is `s *Server` or `s Server[T]`; the type is its last word.
function recv_type(recv,   parts, n, t) {
  n = split(recv, parts, /[ \t]+/)
  t = parts[n]
  sub(/^\*/, "", t)
  sub(/\[.*$/, "", t)
  return t
}
# `init` may repeat, and `_` is the blank identifier: neither is a collision.
# FILENAME is the temporary extraction path; report the repository-relative one.
function emit(name) {
  if (pkg == "" || name == "" || name == "_" || name == "init") return
  print pkg ":" name "\t" substr(FILENAME, length(prefix) + 1)
}
AWK

# names_in <tree> <package> <dest> -> "<key>\t<path>" for every Go file of that
# package in that tree. The path has the temporary extraction prefix removed.
names_in() {
  local tree=$1 pkgdir=$2 dest=$3
  rm -rf "$dest"
  mkdir -p "$dest"
  git archive "$tree" -- "$pkgdir" 2>/dev/null | tar -x -C "$dest" 2>/dev/null || true
  # A package can be absent from one side (new on the branch, deleted on the
  # base). Without nullglob the failed glob would be handed to awk as a literal
  # filename, so expand it only inside a directory that exists.
  local files=()
  if [[ -d "$dest/$pkgdir" ]]; then
    local had_nullglob
    had_nullglob=$(shopt -p nullglob || true)
    shopt -s nullglob
    files=("$dest/$pkgdir"/*.go)
    eval "$had_nullglob"
  fi
  ((${#files[@]})) || return 0
  awk -v prefix="$dest/" -f "$tmp/extract.awk" "${files[@]}"
}

# names_only <classified-lines> -> sorted unique keys
names_only() { cut -f1 | sed '/^$/d' | sort -u; }

# A file the compiler may include alongside any other file. Build-constrained
# files are excluded from the comparison: two mutually exclusive files may
# legally declare the same name.
os_arch='_(aix|android|darwin|dragonfly|freebsd|illumos|ios|js|linux|netbsd|openbsd|plan9|solaris|wasip1|windows|zos|386|amd64|arm|arm64|loong64|mips|mips64|mips64le|mipsle|ppc64|ppc64le|riscv64|s390x|wasm)(_[a-z0-9]+)?\.go$'
constrained() {
  local tree=$1 path=$2 lead
  if [[ ${path##*/} =~ $os_arch ]]; then return 0; fi
  # Read the head of the file once: with `pipefail`, piping `git show` straight
  # into `grep -q` can report 141 (SIGPIPE) and make a constrained file look
  # unconstrained.
  lead=$(git show "${tree}:${path}" 2>/dev/null | head -40 || true)
  if grep -qE '^//(go:build|[[:space:]]+build)' <<<"$lead"; then
    return 0
  fi
  return 1
}

# The first file declaring <key> that has no build constraint; prints nothing
# when every declaring file is constrained.
unconstrained_file() {
  local tree=$1 key=$2 classified=$3 file
  while IFS= read -r file; do
    [[ -n "$file" ]] || continue
    if ! constrained "$tree" "$file"; then
      printf '%s\n' "$file"
      return 0
    fi
  done < <(awk -F'\t' -v k="$key" '$1 == k { print $2 }' <<<"$classified")
  return 1
}

status=0
collisions=0
checked=0

for pkgdir in $packages; do
  # The repository root is not a Go package (no go.mod there); `git archive` of
  # "." would also tar the whole tree for nothing.
  [[ $pkgdir == "." ]] && continue
  checked=$((checked + 1))
  head_lines=$(names_in "$head_ref" "$pkgdir" "$tmp/head")
  merge_base_lines=$(names_in "$merge_base" "$pkgdir" "$tmp/merge-base")

  head_introduced=$(comm -13 \
    <(printf '%s\n' "$merge_base_lines" | names_only) \
    <(printf '%s\n' "$head_lines" | names_only))
  [[ -n "$head_introduced" ]] || continue

  # The base scan is the expensive half; only do it when this branch actually
  # introduced a package-level name in this package.
  base_lines=$(names_in "$base_ref" "$pkgdir" "$tmp/base")
  base_introduced=$(comm -13 \
    <(printf '%s\n' "$merge_base_lines" | names_only) \
    <(printf '%s\n' "$base_lines" | names_only))
  [[ -n "$base_introduced" ]] || continue

  both=$(comm -12 <(printf '%s\n' "$head_introduced") <(printf '%s\n' "$base_introduced"))
  [[ -n "$both" ]] || continue

  while IFS= read -r key; do
    [[ -n "$key" ]] || continue
    head_file=$(unconstrained_file "$head_ref" "$key" "$head_lines") || continue
    base_file=$(unconstrained_file "$base_ref" "$key" "$base_lines") || continue
    collisions=$((collisions + 1))
    printf 'go-decl-collision: FAIL %s: %s is added by both this branch (%s) and %s (%s); merging them is a redeclaration and does not compile\n' \
      "$pkgdir" "${key#*:}" "$head_file" "$base_ref" "$base_file" >&2
    status=1
  done <<<"$both"
done

if ((status != 0)); then
  printf 'go-decl-collision: FAILED (%d collision(s) across %d changed package(s)); rename one side or coordinate with the other PR before merging\n' \
    "$collisions" "$checked" >&2
  exit 1
fi

printf 'go-decl-collision: OK (no declaration collisions with %s across %d package(s) changed on both sides)\n' \
  "$base_ref" "$checked"
