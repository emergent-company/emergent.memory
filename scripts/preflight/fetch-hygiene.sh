#!/usr/bin/env bash
# Reject `git fetch <remote> <bare-ref>` in every tracked script and workflow.
#
# WHY (#956, #1158). A tag and a branch can share a name -- this repo's `dev`
# branch shares one with the connector's rolling `refs/tags/dev` release tag
# (connector-dev.yml re-creates it on every build; #983 is the open item that
# moves it aside). `git fetch origin dev` resolves the SHORT name against
# refs/tags/dev before refs/heads/dev: it fetches the tag into FETCH_HEAD,
# leaves refs/remotes/origin/dev untouched, and exits 0. So the documented
# "fetch before you conclude" step reads a stale ref and reports "not merged"
# for something that is, or reviews against an old base.
#
# `--no-tags` is not a mitigation: a tag named explicitly on the command line
# is still fetched (#956 records this against git itself).
#
# The fix is always an explicit refspec:
#
#   git fetch --no-tags --prune origin \
#     "+refs/heads/${GITHUB_BASE_REF}:refs/remotes/origin/${GITHUB_BASE_REF}"
#
# What passes here: a bare `git fetch origin` with no refname (it updates every
# configured remote-tracking branch), source-only full refnames
# (`refs/heads/dev`), explicit destinations (`dev:dev`), `tag <name>`, commit
# SHAs and `HEAD`/`FETCH_HEAD`. What fails: a refname git has to resolve -- a
# literal word (`dev`, `gh-pages`) or a single variable expansion
# (`"${GITHUB_BASE_REF}"`), which is the same ambiguity one indirection removed.
#
# The baseline (scripts/preflight/fetch-hygiene.baseline, one `path<TAB>command`
# per line) is a SHRINKING list of known-unfixed sites, not a place to park a
# new one. When a baselined site is fixed the guard fails and names the stale
# entry, so the list cannot rot: delete the entry in the same PR that fixes the
# line. The four entries it ships with are the workflow sites of #1158, which
# need `workflows: write` to change; this gate runs in the required `ci` job
# (scripts/preflight/all.sh) so the class cannot grow while that lands.
#
# Known blind spot: a refspec on the NEXT logical line is joined before judging,
# but an array or command substitution (`$flags[@]`) can hide any number of
# words, so such a line is skipped rather than guessed at.
#
# Usage:
#   scripts/preflight/fetch-hygiene.sh [--list] [--baseline PATH]
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
usage: fetch-hygiene.sh [--list] [--baseline PATH]

  --list           print every bare-name fetch site and exit 0 (no check)
  --baseline PATH  accepted sites (default scripts/preflight/fetch-hygiene.baseline)

FETCH_HYGIENE_ROOT overrides the repository root (the test suite uses it).
EOF
}

mode=check
baseline_path=""
while (( $# )); do
  case "$1" in
    --list)
      mode=list
      ;;
    --baseline)
      (( $# >= 2 )) || { usage; exit 2; }
      baseline_path="$2"
      shift
      ;;
    --help | -h)
      usage
      exit 0
      ;;
    *)
      printf 'fetch-hygiene: unknown argument: %s\n' "$1" >&2
      usage
      exit 2
      ;;
  esac
  shift
done

root="${FETCH_HYGIENE_ROOT:-}"
if [[ -z "$root" ]]; then
  root="$(git rev-parse --show-toplevel 2>/dev/null)" || {
    printf 'fetch-hygiene: not inside a git repository\n' >&2
    exit 2
  }
fi
cd "$root"
if [[ -z "$baseline_path" ]]; then
  baseline_path="$root/scripts/preflight/fetch-hygiene.baseline"
fi

# The three helpers below are fork-free on purpose: they run on every line of
# every file that mentions a fetch, and a subshell per call (plus `tr`/`printf`
# pipelines) made this gate take half a minute on this tree.
trim_into() {
  _trimmed=""
  IFS=$' \t' read -r _trimmed <<< "$1" || true
}

# Collapse runs of whitespace so a baseline entry stays stable when a command
# is re-indented or joined across continuation lines.
normalize_into() {
  local -a _words=()
  read -r -a _words <<< "$1" || true
  local IFS=' '
  _normalized="${_words[*]}"
}

findings=() # "path<TAB>line<TAB>normalized command"

# Decide one logical line (continuations already joined).
judge_line() {
  local path=$1 lineno=$2 text=$3
  local t tok cmd
  trim_into "$text"
  t="$_trimmed"
  [[ -z "$t" ]] && return 0
  [[ "$t" == \#* ]] && return 0 # whole-line comment
  # A trailing shell comment ends the command; no fetch argument here holds a
  # literal " #".
  t="${t%% \#*}"

  # YAML list marker and `run:` key: `- run: git fetch ...` is one command.
  t="${t#- }"
  trim_into "$t"
  t="$_trimmed"
  if [[ "$t" == run:\ * ]]; then
    trim_into "${t#run:}"
    t="$_trimmed"
  fi

  # Only a line whose command position is `git fetch` is judged, so an `echo`
  # or a prose string that merely mentions the command is not a fetch.
  if [[ ! "$t" =~ ^((if|then|sudo|command)[[:space:]]+\!?[[:space:]]*)?\!?[[:space:]]*git[[:space:]]+fetch([[:space:]]|$) ]]; then
    return 0
  fi
  cmd="${t#*git }" # "git fetch <args>" -> "fetch <args>"
  normalize_into "git $cmd"
  cmd="$_normalized"

  # Make shell operators their own words so a refname glued to one
  # (`main; then ...`, `dev&&echo`) is still judged as a refname.
  local flat="${t#*git }"
  flat="${flat//\&\&/ \&\& }"
  flat="${flat//||/ || }"
  flat="${flat//;/ ; }"
  local -a words=()
  read -r -a words <<< "$flat" || true
  local -a tokens=()
  for tok in "${words[@]}"; do
    tok="${tok#[\"\']}"
    tok="${tok%[\"\']}"
    tokens+=("$tok")
  done

  local remote_seen=0 skip_next=0 ref
  local -a refs=()
  for tok in "${tokens[@]:1}"; do # tokens[0] is `fetch`
    if (( skip_next )); then
      skip_next=0
      continue
    fi
    case "$tok" in
      '' | '||' | '&&' | '|' | ';' | '&' | then | else | elif | fi | do | done)
        break
        ;;
      *';' | *'&&' | *'||')
        break
        ;;
      -*) # option; the value-taking ones can precede the remote
        case "$tok" in
          --depth | --deepen | --shallow-since | --shallow-exclude | --upload-pack | -u | --server-option | -o | --filter | --negotiation-tip | --refmap | --jobs | -j)
            skip_next=1
            ;;
        esac
        continue
        ;;
    esac
    # Unknown arity: an array or command substitution can hide any number of
    # words, so the rest of the line cannot be judged.
    case "$tok" in
      *'[@]'* | *'[*]'* | *'$('* | *'`'*)
        return 0
        ;;
    esac
    if (( remote_seen == 0 )); then
      remote_seen=1
      continue
    fi
    refs+=("$tok")
  done
  (( ${#refs[@]} )) || return 0

  local skip_tag_name=0
  for ref in "${refs[@]}"; do
    if (( skip_tag_name )); then # the name after `tag`
      skip_tag_name=0
      continue
    fi
    case "$ref" in
      *:*) # explicit refspec with a destination
        continue
        ;;
      refs/* | +refs/*) # full refname source, no tag ambiguity
        continue
        ;;
      tag | tags) # `git fetch origin tag v1` names the tag explicitly, on purpose
        skip_tag_name=1
        continue
        ;;
      HEAD | FETCH_HEAD | ORIG_HEAD | MERGE_HEAD | CHERRY_PICK_HEAD)
        continue
        ;;
    esac
    # A commit SHA needs no resolution.
    [[ "$ref" =~ ^[0-9a-fA-F]{7,64}$ ]] && continue
    # A bare refname: literal word, or a lone variable holding one.
    if [[ "$ref" =~ ^\$(\{[A-Za-z_][A-Za-z0-9_]*\}|[A-Za-z_][A-Za-z0-9_]*)$ ]] \
      || [[ "$ref" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
      findings+=("$path"$'\t'"$lineno"$'\t'"$cmd")
      return 0
    fi
  done
}

# Shell scripts and YAML are the only places that run a fetch (workflows,
# composite actions, task files). The `-l` pass keeps the sweep off every file
# that never mentions a fetch, generated YAML included.
#
# One file is exempt: this gate's own suite. Its heredocs are throwaway fixture
# repositories -- bad fetches written on purpose, never executed here -- so
# scanning it would report the fixtures as if they were this repo's commands.
# Every other tracked script is scanned, the sibling *.test.sh suites included.
mapfile -t files < <(git grep -l -F -- "git fetch" -- \
  '*.sh' '*.bash' '*.yml' '*.yaml' \
  ':(exclude)scripts/preflight/fetch-hygiene.test.sh' || true)
for file in "${files[@]}"; do
  lineno=0
  start=0
  joined=""
  while IFS= read -r line || [[ -n "$line" ]]; do
    lineno=$((lineno + 1))
    # Trim trailing whitespace (pure parameter expansion: no fork per line).
    rtrimmed="${line%"${line##*[![:space:]]}"}"
    if [[ -z "$joined" ]]; then
      # Fast path: only a line that mentions `git fetch`, or one that continues
      # onto the next line, can start a candidate command.
      if [[ "$line" != *"git fetch"* && "$rtrimmed" != *\\ ]]; then
        continue
      fi
      start=$lineno
    fi
    if [[ "$rtrimmed" == *\\ ]]; then
      joined+="${rtrimmed%\\} "
      continue
    fi
    joined+="$line"
    judge_line "$file" "$start" "$joined"
    joined=""
  done < "$file"
  if [[ -n "$joined" ]]; then
    judge_line "$file" "$start" "$joined"
  fi
done

if [[ "$mode" == list ]]; then
  for entry in "${findings[@]}"; do
    IFS=$'\t' read -r path lineno cmd <<< "$entry"
    printf '%s:%s: %s\n' "$path" "$lineno" "$cmd"
  done
  if (( ${#findings[@]} == 0 )); then
    printf 'fetch-hygiene: no bare-name fetch sites\n'
  fi
  exit 0
fi

tmp_find="$(mktemp)"
tmp_base="$(mktemp)"
tmp_left="$(mktemp)"
trap 'rm -f "$tmp_find" "$tmp_base" "$tmp_left"' EXIT

for entry in "${findings[@]}"; do
  IFS=$'\t' read -r path lineno cmd <<< "$entry"
  printf '%s\t%s\n' "$path" "$cmd" >> "$tmp_find"
done
sort -o "$tmp_find" "$tmp_find"

: > "$tmp_base"
if [[ -f "$baseline_path" ]]; then
  grep -v -e '^[[:space:]]*#' -e '^[[:space:]]*$' "$baseline_path" | sort > "$tmp_base" || true
fi

# Match as a multiset, not a set: one baseline entry covers exactly one site,
# so a second copy of a bad line is still reported. `consume` removes the first
# matching entry, which leaves the leftovers as the stale ones.
cp "$tmp_base" "$tmp_left"
consume() {
  awk -v want="$1" '
    !done && $0 == want { done = 1; next }
    { print }
  ' "$tmp_left" > "$tmp_left.next" && mv "$tmp_left.next" "$tmp_left"
}

status=0
new_count=0
stale_count=0

for entry in "${findings[@]}"; do
  IFS=$'\t' read -r path lineno cmd <<< "$entry"
  key="$path"$'\t'"$cmd"
  if grep -Fxq -e "$key" "$tmp_left"; then
    consume "$key"
    continue
  fi
  if (( new_count == 0 )); then
    printf 'fetch-hygiene: bare-name fetch site(s) that resolve a tag named like the branch (#956, #1158):\n' >&2
  fi
  new_count=$((new_count + 1))
  printf '  %s:%s: %s\n' "$path" "$lineno" "$cmd" >&2
done

while IFS= read -r entry; do
  [[ -z "$entry" ]] && continue
  if (( stale_count == 0 )); then
    printf 'fetch-hygiene: stale baseline entr(ies) -- no such site remains, so delete them from %s:\n' "$baseline_path" >&2
  fi
  stale_count=$((stale_count + 1))
  printf '  %s\n' "${entry/$'\t'/: }" >&2
done < "$tmp_left"

if (( new_count > 0 )); then
  cat >&2 <<'EOF'

Fix: name the destination, never the bare branch:
  git fetch --no-tags --prune origin \
    "+refs/heads/<branch>:refs/remotes/origin/<branch>"
The leading `+` lets a force-pushed base refresh instead of failing the fetch.
EOF
fi

if (( new_count > 0 || stale_count > 0 )); then
  status=1
fi

if (( status == 0 )); then
  printf 'fetch-hygiene: OK (%d baselined site(s), no unresolved refnames)\n' "${#findings[@]}"
fi
exit "$status"
