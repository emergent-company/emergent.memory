#!/usr/bin/env bash
# Hermetic regression suite for go-decl-collision.sh (#945).
#
# WHY. The guard's whole value is one judgement: "did the live base and this
# branch introduce the same package-level name into the same Go package?" Get it
# wrong in the permissive direction and the collision the guard exists for (#886
# and #890 both added `TestRetryAllConversions*` to `apps/web-ui/gateway`) merges
# anyway, reddening `dev` and blocking every PR. Get it wrong in the strict
# direction and every branch that adds a legal name in a package the base also
# touched goes red, so lanes learn to bypass the check.
#
# The judgements are small and easy to invert silently:
#   * package-level funcs/types/vars/consts share one namespace, but methods are
#     per-receiver -- `Serve` on `Server` and on `Client` is legal;
#   * `init` and `_` may repeat, so they must never collide;
#   * files under a build constraint are mutually exclusive and may declare the
#     same name, so they must be skipped;
#   * a pull_request checkout has GitHub's synthetic merge commit as HEAD; using
#     it instead of its second parent makes the base side look like the branch's
#     own work.
#
# `bash -n` and a YAML parse cannot see any of that, so every case builds a
# throwaway git repository (no remote, no network, no toolchain beyond git and
# awk), creates the two diverging branches and asserts exit status plus the
# reported evidence. The suite also asserts that the guard is still wired into
# scripts/preflight/all.sh -- a guard nobody runs is worse than none.
#
# Usage:
#   scripts/preflight/go-decl-collision.test.sh
set -uo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="$here/go-decl-collision.sh"
all_sh="$here/all.sh"

for cmd in git awk; do
  command -v "$cmd" >/dev/null 2>&1 || {
    printf 'FAIL [go-decl-collision]: %s is required\n' "$cmd" >&2
    exit 2
  }
done

failures=0
checks=0
ok() {
  checks=$((checks + 1))
  printf 'ok   [go-decl-collision]: %s\n' "$1"
}
no() {
  checks=$((checks + 1))
  failures=$((failures + 1))
  printf 'FAIL [go-decl-collision]: %s\n' "$1" >&2
  printf '%s\n' "$out" | sed 's/^/    /' >&2
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
work="$tmp/repo"

put() {
  mkdir -p "$work/$(dirname "$1")"
  cat >"$work/$1"
}
commit() {
  git -C "$work" add -A >/dev/null
  git -C "$work" commit -qm "$1"
}
git_() { git -C "$work" "$@"; }

# fresh <package-file-content>: an empty repository whose `main` has one package
# and one file, so every case starts from a shared ancestor.
fresh() {
  rm -rf "$work"
  mkdir -p "$work"
  git_ init -q
  git_ config user.email test@example.com
  git_ config user.name test
  git_ config commit.gpgsign false
  put pkg/base.go <<'GO'
package pkg
GO
  commit "initial"
  git_ branch -M main
}

# Two branches from `main`: `live` (what landed on the base while the branch was
# open) and `head` (this branch).
diverge() {
  git_ checkout -q -b live main
  git_ checkout -q main
  git_ checkout -q -b head main
}

EVENT=""
out=""
status=0
run_guard() {
  status=0
  out=$(cd "$work" && env -u PREFLIGHT_BASE -u GITHUB_BASE_REF \
    GITHUB_EVENT_NAME="$EVENT" "$script" "$@" 2>&1) || status=$?
}

expect_status() {
  if ((status == $1)); then
    ok "$2"
  else
    no "$2 (exit ${status}, expected $1)"
  fi
}

expect_contains() {
  if grep -Fq -- "$2" <<<"$out"; then
    ok "$1"
  else
    no "$1 (output does not contain: $2)"
  fi
}

expect_not_contains() {
  if grep -Fq -- "$2" <<<"$out"; then
    no "$1 (output unexpectedly contains: $2)"
  else
    ok "$1"
  fi
}

# ---------------------------------------------------------------------------
# 1. Nothing changed against the base: no Go scan, no failure.
# ---------------------------------------------------------------------------
fresh
git_ checkout -q -b head main
run_guard --base main --head head
expect_status 0 "an unchanged branch is OK"
expect_contains "an unchanged branch says why it did nothing" "no Go files changed"

# ---------------------------------------------------------------------------
# 2. The two branches add different names: no collision.
# ---------------------------------------------------------------------------
fresh
diverge
git_ checkout -q live
put pkg/live.go <<'GO'
package pkg

func OnBase() {}
GO
commit "live adds OnBase"
git_ checkout -q head
put pkg/head.go <<'GO'
package pkg

func OnBranch() {}
GO
commit "head adds OnBranch"
run_guard --base live --head head
expect_status 0 "different names do not collide"
expect_contains "a clean run reports the comparison" "OK (no declaration collisions"

# ---------------------------------------------------------------------------
# 3. The incident: the same func added on both sides of the same package.
# ---------------------------------------------------------------------------
fresh
diverge
git_ checkout -q live
put pkg/live_test.go <<'GO'
package pkg

func TestRetryAllConversionsPlainFormRedirect(t *testing.T) {}
GO
commit "live adds the test"
git_ checkout -q head
put pkg/head_test.go <<'GO'
package pkg

func TestRetryAllConversionsPlainFormRedirect(t *testing.T) {}
GO
commit "head adds the test"
run_guard --base live --head head
expect_status 1 "the same func on both sides is a collision"
expect_contains "the collision names the branch file" "pkg/head_test.go"
expect_contains "the collision names the base file" "pkg/live_test.go"
expect_contains "the failure explains the redeclaration" "redeclaration"

# ---------------------------------------------------------------------------
# 4. Same name, different packages: legal.
# ---------------------------------------------------------------------------
fresh
diverge
git_ checkout -q live
put pkg/live.go <<'GO'
package pkg

func Shared() {}
GO
commit "live adds Shared in pkg"
git_ checkout -q head
put other/head.go <<'GO'
package other

func Shared() {}
GO
commit "head adds Shared in other"
run_guard --base live --head head
expect_status 0 "the same name in different packages is legal"
expect_contains "the disjoint-package fast path is reported" "no Go package changed on both sides"

# ---------------------------------------------------------------------------
# 4b. Same directory, different package clauses: `pkg` and `pkg_test` are
#     separate namespaces, so the same helper name in each is legal. Directory-
#     only grouping would wrongly fail this.
# ---------------------------------------------------------------------------
fresh
diverge
git_ checkout -q live
put pkg/helpers.go <<'GO'
package pkg

func setup() {}
GO
commit "live adds setup to package pkg"
git_ checkout -q head
put pkg/helpers_test.go <<'GO'
package pkg_test

func setup() {}
GO
commit "head adds setup to package pkg_test"
run_guard --base live --head head
expect_status 0 "the same name in package pkg and pkg_test is legal"

# ---------------------------------------------------------------------------
# 5. Types, vars and grouped consts share the package namespace too.
# ---------------------------------------------------------------------------
fresh
diverge
git_ checkout -q live
put pkg/live.go <<'GO'
package pkg

type Widget struct{}

const (
	Limit = 10
)

var ErrGone = "gone"

var First, Second = 1, 2

var Typed int
GO
commit "live adds type/const/var"
git_ checkout -q head
put pkg/head.go <<'GO'
package pkg

type Widget struct{}

const (
	Limit = 5
)

var ErrGone = "gone"

var First, Second = 3, 4

var Typed int
GO
commit "head adds the same type/const/var"
run_guard --base live --head head
expect_status 1 "redeclared types/consts/vars are collisions"
expect_contains "the type is named" "Widget is added by both"
expect_contains "the grouped const is named" "Limit is added by both"
expect_contains "the var is named" "ErrGone is added by both"
expect_contains "a comma list names its first identifier" "First is added by both"
expect_contains "a comma list names its second identifier" "Second is added by both"
expect_contains "a typed var names its identifier" "Typed is added by both"

# ---------------------------------------------------------------------------
# 6. Methods are per receiver: same name on different types is legal, same
#    receiver and name is not.
# ---------------------------------------------------------------------------
fresh
put pkg/types.go <<'GO'
package pkg

type Server struct{}

type Client struct{}
GO
commit "add both receiver types to the ancestor"
diverge
git_ checkout -q live
put pkg/live.go <<'GO'
package pkg

func (s *Server) Serve() {}

func (s *Server) Stop() {}
GO
commit "live adds methods"
git_ checkout -q head
put pkg/head.go <<'GO'
package pkg

func (c *Client) Serve() {}

func (s *Server) Stop() {}
GO
commit "head adds methods"
run_guard --base live --head head
expect_status 1 "a method on the same receiver collides"
expect_contains "the receiver-qualified method is named" "method Server.Stop is added by both"
expect_not_contains "the same method name on another receiver is not a collision" "method Server.Serve is added by both"
expect_not_contains "a method added on one side only is not a collision" "method Client.Serve is added by both"

# ---------------------------------------------------------------------------
# 7. `init` and the blank identifier may legally repeat.
# ---------------------------------------------------------------------------
fresh
diverge
git_ checkout -q live
put pkg/live.go <<'GO'
package pkg

func init() {}

var _ = 1
GO
commit "live repeats init and blank"
git_ checkout -q head
put pkg/head.go <<'GO'
package pkg

func init() {}

var _ = 2
GO
commit "head repeats init and blank"
run_guard --base live --head head
expect_status 0 "init and _ may repeat"
expect_not_contains "init is never reported" "init is added by both"

# ---------------------------------------------------------------------------
# 8. Build-constrained files are mutually exclusive and skipped.
# ---------------------------------------------------------------------------
fresh
diverge
git_ checkout -q live
put pkg/x_linux.go <<'GO'
package pkg

func platformName() {}
GO
put pkg/tagged.go <<'GO'
//go:build ignore

package pkg

func taggedOnly() {}
GO
commit "live adds constrained declarations"
git_ checkout -q head
put pkg/head.go <<'GO'
package pkg

func platformName() {}

func taggedOnly() {}
GO
commit "head adds the same names unconstrained"
run_guard --base live --head head
expect_status 0 "collisions with build-constrained files are skipped"
expect_not_contains "the GOOS-suffixed file is not reported" "platformName is added by both"
expect_not_contains "the tagged file is not reported" "taggedOnly is added by both"

# ---------------------------------------------------------------------------
# 9. A pull_request checkout: HEAD is the synthetic merge commit, so the guard
#    must compare its SECOND parent (the PR head). Using HEAD would claim the
#    base's own landing commit as work done by this branch.
# ---------------------------------------------------------------------------
fresh
diverge
git_ checkout -q live
put pkg/live.go <<'GO'
package pkg

func OnlyOnBase() {}
GO
commit "live adds OnlyOnBase"
git_ checkout -q head
put pkg/head.go <<'GO'
package pkg

func OnlyOnBranch() {}
GO
commit "head adds a different name"
# GitHub's merge commit for the PR: parents are (base, head).
git_ checkout -q live
git_ merge -q --no-ff head -m "synthetic PR merge"
EVENT=pull_request
run_guard --base live
EVENT=""
expect_status 0 "the PR merge commit is read through its second parent"
expect_contains "the second-parent scan compares the PR head" "head="

# ---------------------------------------------------------------------------
# 10. Formatting-only changes in a package introduce no names: the base scan is
#     skipped and nothing fails.
# ---------------------------------------------------------------------------
fresh
diverge
git_ checkout -q live
put pkg/live.go <<'GO'
package pkg

func OnBase() {}
GO
commit "live adds a name"
git_ checkout -q head
put pkg/base.go <<'GO'
// Package pkg is a test fixture.
package pkg
GO
commit "head changes only a comment"
run_guard --base live --head head
expect_status 0 "a comment-only change does not trigger the base scan"

# ---------------------------------------------------------------------------
# 11. Defaults: no origin and no cached base means warn-and-skip, not failure
#     (the network must never turn a PR red).
# ---------------------------------------------------------------------------
fresh
git_ checkout -q -b head main
run_guard
expect_status 0 "an unreachable default base skips"
expect_contains "the skip is explained" "skipping"

# ---------------------------------------------------------------------------
# 12. A bad explicit ref is an error, not a silent skip.
# ---------------------------------------------------------------------------
fresh
run_guard --base refs/heads/does-not-exist --head main
expect_status 1 "an unresolvable --base fails"
expect_contains "the bad --base is named" "does not resolve to a commit"

run_guard --nonsense
expect_status 2 "an unknown argument is a usage error"

# ---------------------------------------------------------------------------
# 13. Wiring: the guard must stay in all.sh (the required `ci` job runs it), and
#     it must parse.
# ---------------------------------------------------------------------------
if grep -Fq "go-decl-collision.sh" "$all_sh"; then
  ok "all.sh runs the guard"
else
  no "all.sh runs the guard"
fi
if bash -n "$script"; then
  ok "the guard parses"
else
  no "the guard parses"
fi

if ((failures > 0)); then
  printf '\nFAIL [go-decl-collision]: %d of %d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi
printf '\nok [go-decl-collision]: %d checks passed\n' "$checks"
