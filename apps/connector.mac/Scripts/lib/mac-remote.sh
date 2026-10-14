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
# environment or in the gitignored apps/connector.mac/.env (loaded by
# mac_resolve_target; an explicitly-set environment variable always wins).
#
#   MEMORY_MAC_HOST        SSH host of the Mac build machine. An explicitly
#                          EMPTY value ('') means "local destination" (used by
#                          tests / --dry-run); an UNSET value is an error.
#   MEMORY_MAC_HOST_NAME   Optional selector for a NAMED host. When set (and
#                          MEMORY_MAC_HOST is not explicitly set), it resolves
#                          MEMORY_MAC_HOST_<NAME>, where <NAME> is the selector
#                          uppercased with every non-alphanumeric -> '_'.
#                          e.g. MEMORY_MAC_HOST_NAME=mini -> MEMORY_MAC_HOST_MINI.
#   MEMORY_MAC_HOST_<NAME> Named-host SSH targets, e.g. MEMORY_MAC_HOST_TOOL=mcj@tool.
#   MEMORY_MAC_PATH        Project dir on the Mac, absolute or ~-rooted, no
#                          spaces. Example: ~/code/emergent.memory-macbuild.
#   MEMORY_MAC_PATH_<NAME> Optional per-host project dir; overrides
#                          MEMORY_MAC_PATH when the matching named host is selected.
#   MEMORY_MAC_SUBTREE     Optional override of the per-script remote subtree
#                          (iOS scripts: client/ios; Mac connector:
#                          client/macos). Must be relative to MEMORY_MAC_PATH.
#   MEMORY_MAC_IOS_SUBDIR  Back-compat alias for MEMORY_MAC_SUBTREE on the iOS
#                          scripts (default: client/ios).
#   ALLOW_SYNC_INTO_GIT=1  Permit syncing into a git-checkout root (dangerous).
#   ALLOW_DELETE_IN_GIT=1  Permit --delete into a git work tree (dangerous).
#
# The variables are documented for humans in apps/connector.mac/.env.example.
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
# lib/ -> Scripts/ -> apps/connector.mac
app_dir="$(cd "$lib_dir/../.." && pwd)"
# Monorepo root (apps/connector.mac -> apps -> repo root). Read by sourcing
# scripts (e.g. mac-doctor.sh); unused within this library itself.
# shellcheck disable=SC2034
repo_root="$(cd "$app_dir/../.." && pwd)"
# Overridable for tests; default is the gitignored connector-local env file.
MAC_ENV_FILE="${MEMORY_MAC_ENV_FILE:-$app_dir/.env}"

# mac_die <message> — fail fast with a clear, actionable error.
mac_die() {
  echo "error: $*" >&2
  exit 1
}

# mac_normalize_name <name> — uppercase <name> and replace every non-alphanumeric
# character with '_', mapping a friendly host name to the suffix used in
# MEMORY_MAC_HOST_<NAME> / MEMORY_MAC_PATH_<NAME>. Uses tr (bash 3.2-safe; no
# ${var^^}, which is bash 4+ only).
mac_normalize_name() {
  printf '%s' "${1:-}" | tr '[:lower:]' '[:upper:]' | tr -c '[:alnum:]' '_'
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
        MEMORY_MAC_IOS_SUBDIR | MEMORY_MAC_HOST_NAME | \
        MEMORY_MAC_HOST_* | MEMORY_MAC_PATH_* | \
        ALLOW_SYNC_INTO_GIT | ALLOW_DELETE_IN_GIT) ;;
      *) continue ;;
    esac
    val="${line#*=}"
    val="${val%$'\r'}"           # strip CR (CRLF files)
    case "$val" in               # strip one layer of matching quotes
      \"*\") val="${val#\"}"; val="${val%\"}" ;;
      \'*\') val="${val#\'}"; val="${val%\'}" ;;
    esac
    # Only export a key that is NOT already set in the environment — an
    # explicitly-set variable (including an explicitly empty one) always wins.
    case "$key" in
      MEMORY_MAC_HOST)       [[ -n "${MEMORY_MAC_HOST+x}" ]]       || export MEMORY_MAC_HOST="$val" ;;
      MEMORY_MAC_PATH)       [[ -n "${MEMORY_MAC_PATH+x}" ]]       || export MEMORY_MAC_PATH="$val" ;;
      MEMORY_MAC_SUBTREE)    [[ -n "${MEMORY_MAC_SUBTREE+x}" ]]    || export MEMORY_MAC_SUBTREE="$val" ;;
      MEMORY_MAC_IOS_SUBDIR) [[ -n "${MEMORY_MAC_IOS_SUBDIR+x}" ]] || export MEMORY_MAC_IOS_SUBDIR="$val" ;;
      MEMORY_MAC_HOST_NAME)  [[ -n "${MEMORY_MAC_HOST_NAME+x}" ]]  || export MEMORY_MAC_HOST_NAME="$val" ;;
      MEMORY_MAC_HOST_*)     [[ -n "${!key+x}" ]]                  || export "$key=$val" ;;
      MEMORY_MAC_PATH_*)     [[ -n "${!key+x}" ]]                  || export "$key=$val" ;;
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

  # Snapshot the explicit environment BEFORE the env file is consulted, so an
  # explicitly-set MEMORY_MAC_HOST / MEMORY_MAC_PATH (even empty) wins over a
  # selector or a value defined in the file.
  local explicit_host=0 explicit_path=0
  [[ -n "${MEMORY_MAC_HOST+x}" ]] && explicit_host=1
  [[ -n "${MEMORY_MAC_PATH+x}" ]] && explicit_path=1

  mac_load_env "$MAC_ENV_FILE"

  # Normalized selector suffix (uppercase, non-alphanumerics -> '_'); empty when
  # no selector is set.
  local selector_suffix=""
  [[ -n "${MEMORY_MAC_HOST_NAME:-}" ]] \
    && selector_suffix="$(mac_normalize_name "$MEMORY_MAC_HOST_NAME")"

  # ---- host -------------------------------------------------------------
  # Precedence:
  #   1. MEMORY_MAC_HOST set explicitly in the environment (highest; empty
  #      = local-destination mode).
  #   2. MEMORY_MAC_HOST_NAME (selector) -> MEMORY_MAC_HOST_<NAME>.
  #   3. MEMORY_MAC_HOST set in the env file (single-host).
  #   4. neither -> fail fast, naming both options.
  local host_from_selector=0
  if [[ "$explicit_host" == 1 ]]; then
    MAC_HOST="$MEMORY_MAC_HOST"
  elif [[ -n "$selector_suffix" ]]; then
    local host_var="MEMORY_MAC_HOST_${selector_suffix}"
    if [[ -n "${!host_var+x}" ]]; then
      MAC_HOST="${!host_var}"
      host_from_selector=1
    else
      mac_die "MEMORY_MAC_HOST_NAME='$MEMORY_MAC_HOST_NAME' selects $host_var, but it is not set.
  Define $host_var in the environment or in $app_dir/.env, or set MEMORY_MAC_HOST
  directly (see apps/connector.mac/.env.example)."
    fi
  elif [[ -n "${MEMORY_MAC_HOST+x}" ]]; then
    MAC_HOST="$MEMORY_MAC_HOST"
  else
    mac_die "MEMORY_MAC_HOST is not set. Point it at the Mac build machine, e.g.:
    export MEMORY_MAC_HOST=mcj-mini
  or pick a named host, e.g.:
    export MEMORY_MAC_HOST_NAME=mini   # resolves MEMORY_MAC_HOST_MINI=<ssh-target>
  or set MEMORY_MAC_HOST (and MEMORY_MAC_PATH) in $app_dir/.env
  (see apps/connector.mac/.env.example)."
  fi

  # ---- checkout path -----------------------------------------------------
  # A named host may have its own project dir (MEMORY_MAC_PATH_<NAME>); it only
  # applies when the host was resolved via the selector. Otherwise fall back to
  # MEMORY_MAC_PATH.
  local path_var=""
  [[ "$host_from_selector" == 1 ]] && path_var="MEMORY_MAC_PATH_${selector_suffix}"
  if [[ "$explicit_path" == 1 ]]; then
    MAC_CHECKOUT_PATH="$MEMORY_MAC_PATH"
  elif [[ -n "$path_var" && -n "${!path_var+x}" ]]; then
    MAC_CHECKOUT_PATH="${!path_var}"
  elif [[ -n "${MEMORY_MAC_PATH:-}" ]]; then
    MAC_CHECKOUT_PATH="$MEMORY_MAC_PATH"
  elif [[ -n "$path_var" ]]; then
    mac_die "MEMORY_MAC_PATH is not set. Set the project dir on the Mac, e.g.:
    export MEMORY_MAC_PATH=~/code/emergent.memory-macbuild
  or set $path_var for the selected named host
  (absolute or ~-rooted, no spaces) in the environment or in $app_dir/.env."
  else
    mac_die "MEMORY_MAC_PATH is not set. Set the project dir on the Mac, e.g.:
    export MEMORY_MAC_PATH=~/code/emergent.memory-macbuild
  (absolute or ~-rooted, no spaces) in the environment or in $app_dir/.env."
  fi

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

# Safe defaults for every ssh connection to the Mac (and rsync's ssh transport,
# below). These are ALWAYS applied so an unreachable / asleep / mistyped host
# fails fast with ssh's own error instead of hanging forever:
#   BatchMode=yes          never prompt for a credential or host-key (no TTY in
#                          CI / non-interactive runs)
#   ConnectTimeout=10      bound the TCP connect (the old silent hang had none)
#   ServerAlive*           detect a dead peer mid-transfer
MAC_SSH_SAFE_OPTS="-o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=15 -o ServerAliveCountMax=3"

# mac_ssh <remote-command> — the single SSH invocation point. The host comes
# from MAC_HOST (set by mac_resolve_target). Local-destination mode has no
# remote to talk to, so fail clearly rather than invoking ssh with no host.
#
# Callers may append extra options via MAC_SSH_OPTS (a space-separated list,
# e.g. "-o LogLevel=VERBOSE"); they are added AFTER the safe defaults above.
mac_ssh() {
  [[ -n "${MAC_HOST:-}" ]] \
    || mac_die "mac_ssh: MAC_HOST is empty (local-destination mode has no remote host)"
  # MAC_SSH_SAFE_OPTS / MAC_SSH_OPTS are deliberate, word-split option lists.
  # shellcheck disable=SC2086,SC2029 # SC2029: callers pass pre-escaped commands meant to expand remotely
  ssh $MAC_SSH_SAFE_OPTS ${MAC_SSH_OPTS:-} "$MAC_HOST" "$@"
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
    # The connector's gitignored target config lives inside the synced subtree
    # (apps/connector.mac/.env); never mirror a local env file to the Mac.
    --exclude '.env'
  )
  [[ "${MAC_DRY_RUN:-0}" == 1 ]] && args+=(--dry-run -i)
  # rsync to a remote MAC_HOST must fail fast too, not hang on connect: route it
  # over the same safe ssh options mac_ssh uses. The -e transport is ignored for
  # a local destination (MAC_HOST empty), so local-destination mode is unchanged.
  rsync "${args[@]}" -e "ssh $MAC_SSH_SAFE_OPTS ${MAC_SSH_OPTS:-}" "$src/" "$dest/"
}
