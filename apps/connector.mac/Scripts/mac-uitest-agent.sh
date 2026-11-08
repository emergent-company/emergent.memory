#!/usr/bin/env bash
# mac-uitest-agent.sh — run the MemoryConnector XCUITest suite on demand from
# ssh, unattended, via a LaunchAgent bootstrapped into the user's GUI session.
#
# Why: XCUITest needs a WindowServer, which only exists inside a user's Aqua
# (GUI) login session. A plain ssh session is headless, so `xcodebuild test`
# hangs or fails there. A LaunchAgent loaded into the `gui/<uid>` domain runs
# INSIDE that Aqua session — `launchctl managername` there reports `Aqua` — so
# `mac-build.sh --uitest` (which only *builds* the app + UI-test bundle) can be
# followed by this script's `run`, which kickstarts the agent and waits for the
# result.
#
# The agent (com.emergent.memory.connector.uitest) is on-demand only: no
# KeepAlive and no RunAtLoad. `run` kickstarts it, then polls the status file
# that the runner writes until it sees a `DONE rc=<n>` line, and exits with that
# code — so `run` is the drop-in "run the UI suite from CI/ssh" entry point.
#
# `run` will not report a result it cannot attribute to the tree under test
# (#261): it re-renders the LaunchAgent when the plist's baked MAC_UITEST_MIRROR
# differs from the mirror this checkout resolves, and it refuses to kickstart
# (exit 3) unless the mirror's `.memory-mirror-stamp` — written by
# `mac-build.sh --uitest` on every sync — names the commit this script is run
# from. A stale mirror used to pass with DONE rc=0, which proved nothing.
#
# Usage:
#   apps/connector.mac/Scripts/mac-uitest-agent.sh install
#       bootstrap the LaunchAgent into gui/<uid> (idempotent)
#   apps/connector.mac/Scripts/mac-uitest-agent.sh run [--no-build] [--timeout <secs>]
#       build (unless --no-build), kickstart, wait for the result (default 900s);
#       exits with the suite's rc, or 3 if the mirror is not the commit under test
#   apps/connector.mac/Scripts/mac-uitest-agent.sh status
#       launchd state + tail of the status and log files
#   apps/connector.mac/Scripts/mac-uitest-agent.sh logs [-n <N>]
#       tail the run log (default 50 lines)
#   apps/connector.mac/Scripts/mac-uitest-agent.sh uninstall
#       bootout + remove the plist (keeps logs)
#
# Target config (same as mac-build.sh — see apps/connector.mac/.env.example):
#   MEMORY_MAC_HOST     SSH host, e.g. my-mini.local
#   MEMORY_MAC_PATH     project dir on Mac, e.g. ~/code/emergent.memory-macbuild
#
# First-run caveat: the very first `run` needs a one-time on-screen Automation /
# Accessibility (TCC) approval for the UI-test runner at the Mac. In our test a
# subsequent rebuild (fresh ad-hoc signature) re-ran without re-prompting.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# apps/connector.mac/Scripts -> apps/connector.mac -> apps -> repo root. The
# commit this script is run from is the commit the mirror must match (#261).
REPO_ROOT="$(cd "$HERE/../../.." && pwd)"

# shellcheck source=lib/mac-remote.sh
# shellcheck disable=SC1091
. "$HERE/lib/mac-remote.sh"

# Fixed identity of the LaunchAgent. Kept as a single constant so the plist's
# Label, the bootout/bootstrap targets, and the on-disk filename cannot drift.
LABEL="com.emergent.memory.connector.uitest"

usage() { sed -n '2,/^set -euo/p' "$0" | sed '$d'; }

# resolve_abs_paths — turn the ~-rooted target into absolute remote paths.
# launchd does NOT expand ~ (or $VAR) in plist values, so every path written
# into the plist must be already-absolute. Ask the remote shell to expand them:
# `echo $HOME` for the home dir, and `cd <unquoted path> && pwd` for the mirror
# root. NOTE: $MAC_CHECKOUT_PATH is left UNQUOTED inside the command string so
# the remote shell expands a leading ~ (a quoted "~/..." is a literal path) —
# the same trick mac-build.sh uses for its xcodegen step.
resolve_abs_paths() {
  ABS_HOME="$(mac_ssh 'echo $HOME')"
  # The mirror may not exist yet on a fresh Mac — mac-build.sh creates it on
  # the first --uitest sync, and `install` is documented to run before that.
  # A failed `cd` (no such directory) must not kill the script before the
  # "run mac-build.sh --uitest first" warning below can print, so fall back to
  # expanding a leading ~ against $ABS_HOME. mac_resolve_target only allows a
  # /- or ~-rooted MAC_CHECKOUT_PATH, so only the ~ case changes anything (the
  # /-rooted case is already absolute and passes through unchanged). ssh
  # connectivity is already proven by the $ABS_HOME probe above, so the only
  # expected failure here is "directory does not exist".
  if ! ABS_MIRROR="$(mac_ssh "cd $MAC_CHECKOUT_PATH && pwd" 2>/dev/null)"; then
    ABS_MIRROR="${MAC_CHECKOUT_PATH/#\~/$ABS_HOME}"
  fi
  ABS_LOG_DIR="$ABS_HOME/Library/Logs/memory-connector/uitest"
  ABS_PLIST="$ABS_HOME/Library/LaunchAgents/$LABEL.plist"
  # The runner lives in the synced mirror at <mirror>/client/macos/... (the
  # apps/connector.mac subtree maps to <mirror>/client/macos).
  ABS_RUNNER="$ABS_MIRROR/client/macos/Scripts/lib/mac-uitest-runner.sh"
  ABS_STATUS="$ABS_LOG_DIR/status"
  ABS_RUN_LOG="$ABS_LOG_DIR/run.log"
  MAC_UID="$(mac_ssh 'id -u')"
  DOMAIN="gui/$MAC_UID"
}

# render_plist — emit the LaunchAgent plist with every path already absolute.
# The heredoc delimiter is deliberately unquoted so $ABS_* expand here.
render_plist() {
  cat <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>$LABEL</string>
    <key>ProgramArguments</key>
    <array>
        <string>/bin/bash</string>
        <string>$ABS_RUNNER</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
        <key>MAC_UITEST_MIRROR</key>
        <string>$ABS_MIRROR</string>
        <key>MAC_UITEST_LOG_DIR</key>
        <string>$ABS_LOG_DIR</string>
    </dict>
    <key>LimitLoadToSessionType</key>
    <string>Aqua</string>
    <key>ProcessType</key>
    <string>Interactive</string>
    <key>WorkingDirectory</key>
    <string>$ABS_MIRROR</string>
    <key>StandardOutPath</key>
    <string>$ABS_RUN_LOG</string>
    <key>StandardErrorPath</key>
    <string>$ABS_RUN_LOG</string>
</dict>
</plist>
EOF
}

# installed_plist_mirror — the MAC_UITEST_MIRROR the INSTALLED LaunchAgent will
# actually hand the runner. launchd does not read the environment this script
# resolved; the plist is the only thing that decides which tree gets tested, so
# that is what has to be checked. Empty output means no plist / no such key.
installed_plist_mirror() {
  mac_ssh "test -f '$ABS_PLIST' \
      && plutil -extract MAC_UITEST_MIRROR raw -o - '$ABS_PLIST'" 2>/dev/null || true
}

# reconcile_agent_plist — make the LaunchAgent point at the mirror THIS checkout
# resolves, re-rendering it deterministically when it does not. `install` bakes
# the mirror of the moment into the plist and `run` used to only kickstart that
# existing plist, so a checkout that moved (or a host whose MEMORY_MAC_PATH
# changed) kept the old tree wired in with no warning — and the suite then
# passed against files that were not under test (#261).
reconcile_agent_plist() {
  local installed
  installed="$(installed_plist_mirror)"
  if [[ -z "$installed" ]]; then
    echo "==> no MAC_UITEST_MIRROR in $ABS_PLIST; installing the LaunchAgent for $ABS_MIRROR"
    install
  elif [[ "$installed" != "$ABS_MIRROR" ]]; then
    echo "==> WARNING: the installed LaunchAgent would test a STALE mirror (#261):" >&2
    echo "      plist MAC_UITEST_MIRROR = $installed" >&2
    echo "      this checkout resolves  = $ABS_MIRROR" >&2
    echo "    re-rendering the LaunchAgent so the suite runs the tree under test"
    install
  else
    echo "==> LaunchAgent mirror confirmed: $ABS_MIRROR"
    return 0
  fi

  installed="$(installed_plist_mirror)"
  [[ "$installed" == "$ABS_MIRROR" ]] \
    || mac_die "LaunchAgent still reports MAC_UITEST_MIRROR='$installed' after re-install (expected '$ABS_MIRROR'); run: $0 install"
}

# verify_mirror_commit — refuse to run the suite unless the mirror's recorded
# source commit is the commit this checkout is at. mac-build.sh --uitest writes
# .memory-mirror-stamp on every sync; no stamp means the mirror predates the
# guard (or was written by an older mac-build.sh), which is exactly the
# unprovable state this check exists to reject. Exit 3 = "would not test what
# you claim", distinct from a suite failure (1) and a timeout (124).
verify_mirror_commit() {
  local stamp_path stamp remote_commit local_commit dirty branch
  stamp_path="$ABS_MIRROR/.memory-mirror-stamp"
  stamp="$(mac_ssh "cat '$stamp_path' 2>/dev/null || true")"
  local_commit="$(git -C "$REPO_ROOT" rev-parse HEAD)" \
    || mac_die "cannot read the local HEAD of $REPO_ROOT; run this from the repo checkout that should be tested"

  if [[ -z "$stamp" ]]; then
    echo "error: no mirror stamp at $stamp_path on $MAC_HOST (#261)." >&2
    echo "       Nothing proves which commit that mirror holds, so a green suite" >&2
    echo "       would say nothing about $local_commit." >&2
    echo "       Fix: apps/connector.mac/Scripts/mac-build.sh --uitest" >&2
    exit 3
  fi

  remote_commit="$(printf '%s\n' "$stamp" | sed -n 's/^commit=//p' | head -n 1)"
  dirty="$(printf '%s\n' "$stamp" | sed -n 's/^dirty=//p' | head -n 1)"
  branch="$(printf '%s\n' "$stamp" | sed -n 's/^branch=//p' | head -n 1)"

  if [[ "$remote_commit" != "$local_commit" ]]; then
    echo "error: the Mac mirror is a STALE TREE — refusing to run the UI suite (#261)." >&2
    echo "      mirror $stamp_path: commit '$remote_commit' (branch '$branch')" >&2
    echo "      local  $REPO_ROOT: commit '$local_commit'" >&2
    echo "      A pass against the mirror would prove nothing about the current commit." >&2
    echo "      Fix: apps/connector.mac/Scripts/mac-build.sh --uitest" >&2
    exit 3
  fi

  echo "==> mirror stamp matches the checkout: $local_commit (branch $branch)"
  [[ "$dirty" == 0 ]] \
    || echo "note: the mirror was synced from a DIRTY working tree; uncommitted changes are what the suite will test" >&2
}

# install — idempotently bootstrap the agent into gui/<uid>.
install() {
  resolve_abs_paths

  echo "==> ssh $MAC_HOST: create log dir $ABS_LOG_DIR"
  mac_ssh "mkdir -p '$ABS_LOG_DIR'"

  # The runner is synced by mac-build.sh (it mirrors apps/connector.mac/ into
  # client/macos/). If it is not there yet the job will fail at kickstart time,
  # so warn now rather than fail the install.
  if ! mac_ssh "test -f '$ABS_RUNNER'"; then
    echo "warning: runner not found at $ABS_RUNNER on $MAC_HOST" >&2
    echo "         run apps/connector.mac/Scripts/mac-build.sh --uitest first (it syncs the tree)" >&2
  fi

  echo "==> writing LaunchAgent plist to $ABS_PLIST"
  local tmp
  tmp="$(mktemp "${TMPDIR:-/tmp}/mac-uitest-plist.XXXXXX")"
  render_plist > "$tmp"
  # `cat > path` with the plist fed over ssh stdin; $ABS_PLIST is absolute so it
  # is safe to single-quote for the remote shell.
  mac_ssh "cat > '$ABS_PLIST'" < "$tmp"
  rm -f "$tmp"

  # bootout clears any previous load of this label (idempotency); it fails when
  # nothing is loaded, which is expected, so ignore the failure.
  echo "==> bootout (ignore failure) + bootstrap into $DOMAIN"
  mac_ssh "launchctl bootout '$DOMAIN/$LABEL' 2>/dev/null || true"
  mac_ssh "launchctl bootstrap '$DOMAIN' '$ABS_PLIST'"

  echo "==> launchctl print $DOMAIN/$LABEL"
  mac_ssh "launchctl print '$DOMAIN/$LABEL'"
}

# run [--no-build] [--timeout <secs>] — build, kickstart, wait, exit with rc.
run() {
  local no_build=0 timeout=900

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --no-build) no_build=1; shift ;;
      --timeout)
        # Last value wins if --timeout is given more than once.
        [[ $# -gt 1 ]] || { echo "error: --timeout needs a value" >&2; usage >&2; exit 2; }
        timeout="$2"
        shift 2 ;;
      *) echo "unknown run arg: $1 (expected --no-build | --timeout <secs>)" >&2; usage >&2; exit 2 ;;
    esac
  done

  resolve_abs_paths

  # The LaunchAgent, not this shell, decides which tree the suite runs in.
  reconcile_agent_plist

  if [[ "$no_build" == 0 ]]; then
    echo "==> building app + UI-test bundle (mac-build.sh --uitest)"
    "$HERE/mac-build.sh" --uitest
  fi

  # mac-build.sh just stamped the mirror with the synced commit; after
  # --no-build the stamp is whatever an earlier sync left. Either way, failing
  # here is better than a green run against a tree nobody chose.
  verify_mirror_commit

  echo "==> clearing status file $ABS_STATUS"
  mac_ssh "rm -f '$ABS_STATUS'"

  echo "==> kickstart $DOMAIN/$LABEL (launchctl kickstart -k, mirror $ABS_MIRROR)"
  mac_ssh "launchctl kickstart -k '$DOMAIN/$LABEL'"

  # Poll the status file until the runner appends its DONE line. The runner
  # writes RUNNING first, then DONE rc=<n>; the rc is the xcodebuild exit code.
  local start now done_line rc
  start="$(date +%s)"
  while true; do
    done_line="$(mac_ssh "grep 'DONE rc=' '$ABS_STATUS' 2>/dev/null | tail -n 1 || true")"
    if [[ -n "$done_line" ]]; then
      rc="$(mac_ssh "sed -nE 's/.*DONE rc=([0-9]+).*/\1/p' '$ABS_STATUS' | tail -n 1 || true")"
      echo "==> suite finished ($rc): $done_line"
      echo "==> tail of run log ($ABS_RUN_LOG):"
      mac_ssh "tail -n 50 '$ABS_RUN_LOG' 2>/dev/null || true"
      exit "$rc"
    fi
    now="$(date +%s)"
    if (( now - start >= timeout )); then
      echo "error: timed out after ${timeout}s waiting for DONE rc= in $ABS_STATUS" >&2
      echo "==> status file:" >&2
      mac_ssh "cat '$ABS_STATUS' 2>/dev/null || true" >&2
      echo "==> tail of run log:" >&2
      mac_ssh "tail -n 50 '$ABS_RUN_LOG' 2>/dev/null || true" >&2
      local pid
      pid="$(mac_ssh "sed -nE 's/^RUNNING pid=([0-9]+).*/\1/p' '$ABS_STATUS' | tail -n 1 || true")"
      if [[ -n "$pid" ]]; then
        echo "==> job still running on $MAC_HOST with pid $pid" >&2
      fi
      # 124 is the conventional 'timeout' exit status.
      exit 124
    fi
    sleep 5
  done
}

# status — launchd state + tail of the status and log files.
status() {
  resolve_abs_paths
  echo "==> launchctl print $DOMAIN/$LABEL"
  mac_ssh "launchctl print '$DOMAIN/$LABEL'" || echo "    (service not loaded)"
  echo "==> status file ($ABS_STATUS):"
  mac_ssh "cat '$ABS_STATUS' 2>/dev/null || true"
  echo "==> tail of run log ($ABS_RUN_LOG):"
  mac_ssh "tail -n 50 '$ABS_RUN_LOG' 2>/dev/null || true"
}

# logs [-n <N>] — tail the run log.
logs() {
  local n=50
  if [[ $# -gt 0 ]]; then
    case "$1" in
      -n) [[ $# -gt 1 ]] || { echo "error: -n needs a value" >&2; usage >&2; exit 2; }
          n="$2" ;;
      *) echo "unknown logs arg: $1" >&2; usage >&2; exit 2 ;;
    esac
  fi
  resolve_abs_paths
  mac_ssh "tail -n '$n' '$ABS_RUN_LOG' 2>/dev/null || true"
}

# uninstall — bootout + remove the plist; keep the logs for post-mortem.
uninstall() {
  resolve_abs_paths
  echo "==> bootout $DOMAIN/$LABEL (ignore failure)"
  mac_ssh "launchctl bootout '$DOMAIN/$LABEL' 2>/dev/null || true"
  echo "==> removing $ABS_PLIST"
  mac_ssh "rm -f '$ABS_PLIST'"
  echo "    logs kept at $ABS_LOG_DIR"
}

# ---- dispatch ------------------------------------------------------------
CMD="${1:-}"
case "$CMD" in
  -h|--help) usage; exit 0 ;;
  '')        usage; exit 2 ;;
esac

# Everything below needs a configured, reachable Mac target (help/usage above
# must work without one).
mac_resolve_target client/macos
[[ -n "$MAC_HOST" ]] \
  || mac_die "local-destination mode has no remote host to bootstrap a LaunchAgent into"

shift
case "$CMD" in
  install)   install "$@" ;;
  run)       run "$@" ;;
  status)    status "$@" ;;
  logs)      logs "$@" ;;
  uninstall) uninstall "$@" ;;
  *)         echo "unknown command: $CMD" >&2; usage >&2; exit 2 ;;
esac
