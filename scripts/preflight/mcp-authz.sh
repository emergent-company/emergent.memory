#!/usr/bin/env bash
# Method-level authorization boundary for domain/mcp (#1089).
#
# The #1092 depguard rule stops domain/mcp from *importing* a new sibling
# domain's store/repository, but it cannot stop a tool from calling a method on
# an already allow-listed raw handle in a way that skips the domain's shared
# Authorize* helper — the exact shape of #1040, where the MCP skills delete path
# called skills.Repository.Delete directly and destroyed another project's
# skill.
#
# This gate runs an AST-based Go guard (cmd/mcp-authz-guard) that:
#   1. fails when a call on the raw skills.Repository handle uses an
#      unclassified method, or a method that crosses a project boundary
#      (FindByID/Update/Delete) without the shared Authorize* helper in the
#      same function; and
#   2. fails when a function issues raw s.db.* SQL (against tables no import
#      rule can see) that is not declared in apps/server/mcp-authz.yaml.
#
# Regex is deliberately not used for detection: receiver/method resolution
# needs the real call-graph shape (go/ast), and a regex would produce the false
# positives/negatives that make a guard untrustworthy.
#
# Run from anywhere; resolves the repo root itself. Wired into CI via
# scripts/preflight/all.sh (the branch-protection-required `ci` job). The guard
# imports only the stdlib, so `go run` does not touch the network.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root/apps/server"

go run ./cmd/mcp-authz-guard
