#!/usr/bin/env bash
# shellcheck shell=bash
# mac-remote.sh — shared plumbing for the Mac remote build scripts.
#
# SOURCE this file; do not execute it:
#
#   HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
#   # shellcheck source=lib/mac-remote.sh
#   . "$HERE/lib/mac-remote.sh"
#
# It centralises the three things that used to be copy-pasted across
# ios-build-mac.sh, ios-debug-mac.sh and mac-build.sh (and drifted apart):
#   1. target resolution/validation (mac_resolve_target),
#   2. the guarded local -> Mac mirror (mac_rsync_guarded), with scoped
#      --delete, artifact excludes, destination guards and a --dry-run path,
#   3. a single SSH entry point (mac_ssh).
#
# ---- Target configuration -------------------------------------------------
# There are NO repo-specific defaults baked into any script. Every remote
# operation fails fast unless the target is configured, either in the
# environment or in the gitignored apps/web-ui/.env (loaded by
# mac_resolve_target; an explicitly-set environment variable always wins).
#
#   MEMORY_MAC_HOST        Required. SSH host of the Mac build machine.
#                          An explicitly EMPTY value ('') means "local
#                          destination" (used by tests / --dry-run); an UNSET
#                          value is an error.
#   MEMORY_MAC_PATH        Required. Project dir on the Mac, absolute or
#                          ~-rooted, no spaces. Example: ~/code/alftred
#   MEMORY_MAC_SUBTREE     Optional override of the per-script remote subtree
#                          (iOS scripts: client/ios; Mac connector:
#                          client/macos). Must be relative to MEMORY_MAC_PATH.
#   MEMORY_MAC_IOS_SUBDIR  Back-compat alias for MEMORY_MAC_SUBTREE on the iOS
#                          scripts (default: client/ios).
#   ALLOW_SYNC_INTO_GIT=1  Permit syncing into a git-checkout root (dangerous).
#   ALLOW_DELETE_IN_GIT=1  Permit --delete into a git work tree (dangerous).
#
# The variables are documented for humans in tools/mac-remote.env.example.
#
# ---- Safety model ---------------------------------------------------------
# * rsync mirrors are rooted at the DEDICATED remote subtree
#   (<MEMORY_MAC_PATH>/<subtree>), NEVER at the checkout root, so --delete can
#   only ever remove stale files inside that subtree.
# * Empty / '/' / '~' / '$HOME' / relative destinations are refused, and the
#   subtree may not contain '.' or '..' (no scope escape).
# * A .git at either the checkout root or the synced subtree aborts the sync
#   (that destination is a real clone, not the rsync'd Mac build checkout)
#   unless explicitly overridden.
# * MAC_DRY_RUN=1 makes mac_rsync_guarded append --dry-run -i and print the
#   plan without transferring or deleting anything.

# Refuse direct execution.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  echo "mac-remote.sh is a library; source it, do not execute it" >&2
  exit 2
fi

lib_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# lib/ -> tools/ -> apps/web-ui
webui_dir="$(cd "$lib_dir/../.." && pwd)"
# Overridable for tests; default is the gitignored repo-local env file.
MAC_ENV_FILE="${MEMORY_MAC_ENV_FILE:-$webui_dir/.env}"

# mac_die <message> — fail fast with a clear, actionable error.
mac_die() {
  echo "error: $*" >&2
  exit 1
}

# mac_load_env [<file>] — load the whitelisted MEMORY_MAC_* / ALLOW_* keys from
# the gitignored env file. Only keys we understand are read (the file is never
# sourced/executed), and any variable already present in the environment —
# including an explicitly empty one — is left untouched.
mac_load_env() {
  local file="${1:-$MAC_ENV_FILE}" line key val
  [[ -n "$file" && -f "$file" ]] || return 0
  while IFS= read -r line || [[ -n "$line" ]]; do
    case "$line" in ''|'#'*) continue ;; esac
    case "$line" in *=*) ;; *) continue ;; esac
    key="${line%%=*}"
    case "$key" in
      MEMORY_MAC_HOST | MEMORY_MAC_PATH | MEMORY_MAC_SUBTREE | \
        MEMORY_MAC_IOS_SUBDIR | ALLOW_SYNC_INTO_GIT | ALLOW_DELETE_IN_GIT) ;;
      *) continue ;;
    esac
    val="${line#*=}"
    val="${val%$'\r'}"           # strip CR (CRLF files)
    case "$val" in               # strip one layer of matching quotes
      \"*\") val="${val#\"}"; val="${val%\"}" ;;
      \'*\') val="${val#\'}"; val="${val%\'}" ;;
    esac
    case "$key" in
      MEMORY_MAC_HOST)       [[ -n "${MEMORY_MAC_HOST+x}" ]]       || export MEMORY_MAC_HOST="$val" ;;
      MEMORY_MAC_PATH)       [[ -n "${MEMORY_MAC_PATH+x}" ]]       || export MEMORY_MAC_PATH="$val" ;;
      MEMORY_MAC_SUBTREE)    [[ -n "${MEMORY_MAC_SUBTREE+x}" ]]    || export MEMORY_MAC_SUBTREE="$val" ;;
      MEMORY_MAC_IOS_SUBDIR) [[ -n "${MEMORY_MAC_IOS_SUBDIR+x}" ]] || export MEMORY_MAC_IOS_SUBDIR="$val" ;;
      ALLOW_SYNC_INTO_GIT)   [[ -n "${ALLOW_SYNC_INTO_GIT+x}" ]]   || export ALLOW_SYNC_INTO_GIT="$val" ;;
      ALLOW_DELETE_IN_GIT)   [[ -n "${ALLOW_DELETE_IN_GIT+x}" ]]   || export ALLOW_DELETE_IN_GIT="$val" ;;
    esac
  done < "$file"
}

# mac_resolve_target <default_subtree> [<override_env_name>]
#
# Resolves and validates the remote target from the environment (after loading
# the env file) and exports:
#   MAC_HOST            SSH host (may be empty = local destination)
#   MAC_CHECKOUT_PATH   validated MEMORY_MAC_PATH
#   MAC_SUBTREE         validated relative remote subtree
#   MAC_DEST_PATH       MAC_CHECKOUT_PATH/MAC_SUBTREE
#   MAC_RSYNC_DEST      MAC_HOST:MAC_DEST_PATH (or MAC_DEST_PATH when local)
mac_resolve_target() {
  local default_subtree="${1:?mac_resolve_target: missing default subtree}"
  local override_var="${2:-MEMORY_MAC_SUBTREE}"

  mac_load_env "$MAC_ENV_FILE"

  if [[ -z "${MEMORY_MAC_HOST+x}" ]]; then
    mac_die "MEMORY_MAC_HOST is not set. Point it at the Mac build machine, e.g.:
    export MEMORY_MAC_HOST=mcj-mini
  or set MEMORY_MAC_HOST (and MEMORY_MAC_PATH) in $webui_dir/.env
  (see tools/mac-remote.env.example)."
  fi
  MAC_HOST="$MEMORY_MAC_HOST"

  if [[ -z "${MEMORY_MAC_PATH:-}" ]]; then
    mac_die "MEMORY_MAC_PATH is not set. Set the project dir on the Mac, e.g.:
    export MEMORY_MAC_PATH=~/code/alftred
  (absolute or ~-rooted, no spaces) in the environment or in $webui_dir/.env."
  fi
  MAC_CHECKOUT_PATH="$MEMORY_MAC_PATH"

  # Matching a literal ~ / $HOME input (not an expanded path) is intentional.
  # shellcheck disable=SC2088,SC2016
  case "$MAC_CHECKOUT_PATH" in
    ''|'/'|'//'|'.'|'..'|'~'|'~/'|'$HOME'|'${HOME}')
      mac_die "refusing unsafe MEMORY_MAC_PATH='$MAC_CHECKOUT_PATH'" ;;
  esac
  [[ "$MAC_CHECKOUT_PATH" == '~'* || "$MAC_CHECKOUT_PATH" == '/'* ]] \
    || mac_die "MEMORY_MAC_PATH must be absolute or ~-rooted: '$MAC_CHECKOUT_PATH'"
  case "/$MAC_CHECKOUT_PATH/" in
    *'/../'*|*'/./'*) mac_die "MEMORY_MAC_PATH must not contain '.' or '..': '$MAC_CHECKOUT_PATH'" ;;
  esac

  MAC_SUBTREE="${!override_var:-$default_subtree}"
  [[ -n "$MAC_SUBTREE" ]] || mac_die "remote subtree must not be empty"
  [[ "$MAC_SUBTREE" != /* && "$MAC_SUBTREE" != '~'* ]] \
    || mac_die "remote subtree must be relative to MEMORY_MAC_PATH: '$MAC_SUBTREE'"
  case "/$MAC_SUBTREE/" in
    *'/../'*|*'/./'*) mac_die "remote subtree must not contain '.' or '..': '$MAC_SUBTREE'" ;;
  esac

  MAC_DEST_PATH="$MAC_CHECKOUT_PATH/$MAC_SUBTREE"
  if [[ -n "$MAC_HOST" ]]; then
    MAC_RSYNC_DEST="$MAC_HOST:$MAC_DEST_PATH"
  else
    MAC_RSYNC_DEST="$MAC_DEST_PATH"
  fi
  export MAC_RSYNC_DEST
}

# mac_ssh <remote-command> — the single SSH invocation point. The host comes
# from MAC_HOST (set by mac_resolve_target). Local-destination mode has no
# remote to talk to, so fail clearly rather than invoking ssh with no host.
mac_ssh() {
  [[ -n "${MAC_HOST:-}" ]] \
    || mac_die "mac_ssh: MAC_HOST is empty (local-destination mode has no remote host)"
  # shellcheck disable=SC2029 # callers pass pre-escaped commands meant to expand remotely
  ssh "$MAC_HOST" "$@"
}

# mac_path_exists <path> — read-only `test -e` probe, local or over ssh.
mac_path_exists() {
  local path="$1"
  if [[ -n "${MAC_HOST:-}" ]]; then
    # shellcheck disable=SC2029 # $path is an absolute/~/ remote path, must expand remotely
    mac_ssh "test -e $path"
  else
    test -e "$path"
  fi
}

# mac_rsync_guarded <src_dir> [<subtree>]
#
# Mirror <src_dir>/ into <MAC_CHECKOUT_PATH>/<subtree>/ using the #1154 rules:
# scoped --delete (confined to the dedicated subtree, never the checkout root),
# the artifact excludes, destination guards and a --dry-run path. Requires
# mac_resolve_target to have run; the subtree defaults to MAC_SUBTREE.
# Honors MAC_DRY_RUN=1 and the ALLOW_*_GIT overrides.
mac_rsync_guarded() {
  local src="${1:?mac_rsync_guarded: missing source dir}"
  local subtree="${2:-${MAC_SUBTREE:?mac_rsync_guarded: run mac_resolve_target first}}"
  local dest_path="$MAC_CHECKOUT_PATH/$subtree" dest

  [[ -d "$src" ]] \
    || mac_die "sync source not found: $src (expected app directory — repo layout drift?)"

  # Destination guards: the mirror target must be a relative subtree of a
  # validated, non-root checkout path.
  # shellcheck disable=SC2088,SC2016
  case "$MAC_CHECKOUT_PATH" in
    ''|'/'|'//'|'.'|'..'|'~'|'~/'|'$HOME'|'${HOME}')
      mac_die "refusing unsafe sync destination base MEMORY_MAC_PATH='$MAC_CHECKOUT_PATH'" ;;
  esac
  [[ -n "$subtree" && "$subtree" != /* && "$subtree" != '~'* ]] \
    || mac_die "refusing unsafe sync destination subtree '$subtree'"
  case "/$subtree/" in
    *'/../'*|*'/./'*) mac_die "refusing sync destination that climbs outside the checkout: '$subtree'" ;;
  esac

  if [[ -n "${MAC_HOST:-}" ]]; then
    dest="$MAC_HOST:$dest_path"
  else
    dest="$dest_path"
  fi

  # The Mac build checkout is rsync'd (it has no .git). A .git at either the
  # checkout root or the synced subtree means the destination is a real clone
  # that --delete could damage.
  if mac_path_exists "$MAC_CHECKOUT_PATH/.git"; then
    [[ "${ALLOW_SYNC_INTO_GIT:-0}" == 1 ]] \
      || mac_die "$MAC_CHECKOUT_PATH is a git checkout; set ALLOW_SYNC_INTO_GIT=1 to override"
  fi
  if mac_path_exists "$dest_path/.git"; then
    [[ "${ALLOW_DELETE_IN_GIT:-0}" == 1 ]] \
      || mac_die "$dest_path is a git work tree and --delete could remove tracked files; set ALLOW_DELETE_IN_GIT=1 to override"
  fi

  echo "==> rsync $src/ -> $dest/ (--delete scoped to '$subtree/' only)"

  local args=(
    -az --delete
    --exclude '.DS_Store'
    --exclude 'xcuserdata/'
    --exclude '*.xcuserstate'
    --exclude 'build/'
    --exclude 'DerivedData/'
    --exclude '.build/'
    --exclude '.swiftpm/'
    --exclude '.git/'
  )
  [[ "${MAC_DRY_RUN:-0}" == 1 ]] && args+=(--dry-run -i)
  rsync "${args[@]}" "$src/" "$dest/"
}
