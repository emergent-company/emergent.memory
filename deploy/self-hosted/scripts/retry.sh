#!/bin/sh
# Bounded retry for a build step whose failure mode is the network.
#
# WHY: an image build failed on
#
#   go.opentelemetry.io/otel@v1.46.0: stream error: stream ID 3; INTERNAL_ERROR
#
# a transport fault with nothing wrong in this tree. Only the steps that resolve
# modules over the network use this: `go mod download`, `go mod tidy -diff` (it
# resolves the test dependencies of dependencies) and `go install <tool>@<ver>`.
# A plain compile or a test does not fetch, so it is left to fail on its own
# rather than retried into a slower failure.
#
# Usage:
#   retry.sh <attempts> <delay-seconds> <label> -- <command> [args...]
#
#   retry.sh 3 10 "go mod download" -- go mod download
#
# Output: each attempt's combined stdout/stderr is captured, so a transient
# failure cannot bury the reason under later attempts. The captured output is
# printed when the command succeeds (usually empty) and when the last attempt
# fails, followed by the label and attempt count on stderr. The caller exits
# with the command's own status -- 1 after exhaustion -- so a Dockerfile can add
# its own guidance around a failure.
#
# POSIX sh only: this runs in the builder image (busybox ash on Alpine), before
# any toolchain beyond /bin/sh is available.
set -eu

if [ "$#" -lt 4 ]; then
  echo "usage: retry.sh <attempts> <delay-seconds> <label> -- <command> [args...]" >&2
  exit 2
fi

attempts=$1
delay=$2
label=$3
shift 3

[ "${1:-}" = "--" ] || {
  echo "retry.sh: expected -- before the command, got '$1'" >&2
  exit 2
}
shift

# A missing command would otherwise "succeed" instantly, which turns a Dockerfile
# typo into a silently skipped build step -- the opposite of what this helper is
# for.
[ "$#" -ge 1 ] || {
  echo "retry.sh: no command after --" >&2
  exit 2
}

case $attempts in
  '' | *[!0-9]*) echo "retry.sh: attempts must be a positive integer, got '$attempts'" >&2; exit 2 ;;
esac
case $delay in
  '' | *[!0-9]*) echo "retry.sh: delay must be a non-negative integer, got '$delay'" >&2; exit 2 ;;
esac
[ "$attempts" -ge 1 ] || { echo "retry.sh: attempts must be at least 1" >&2; exit 2; }

i=1
while :; do
  status=0
  out=$("$@" 2>&1) || status=$?
  if [ "$status" -eq 0 ]; then
    [ -n "$out" ] && printf '%s\n' "$out"
    exit 0
  fi
  if [ "$i" -ge "$attempts" ]; then
    [ -n "$out" ] && printf '%s\n' "$out" >&2
    echo "$label: failed after $i attempt(s)" >&2
    exit "$status"
  fi
  echo "$label: attempt $i/$attempts failed; retrying in ${delay}s" >&2
  i=$((i + 1))
  [ "$delay" -gt 0 ] && sleep "$delay"
done
