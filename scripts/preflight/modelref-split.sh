#!/usr/bin/env bash
# Model-name prefix parsing must live in exactly one place: pkg/modelref.
#
# Issue #1064 removed the last ad-hoc "exactly one slash" prefix parsers
# (domain/provider stripModelPrefix, pkg/adk stripRoutingPrefix) in favour of
# modelref.Parse / modelref.StripRoutingPrefix. This gate runs an AST-based Go
# guard (apps/server/cmd/modelref-guard) that fails when a model name is split
# on a literal "/" with strings.Cut / strings.Split / strings.SplitN anywhere
# outside pkg/modelref, unless the site carries a documented modelref:allow
# marker explaining why it is not reconstructing provider identity from a routed
# model string.
#
# AST (not regex) is used for detection so nested calls and unrelated
# "strings.Cut(x, \"/\")" uses resolve exactly. Wired into CI via
# scripts/preflight/all.sh (the branch-protection-required `ci` job). The guard
# imports only the stdlib, so `go run` does not touch the network.
#
# Run from anywhere; resolves the repo root itself.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root/apps/server"

go run ./cmd/modelref-guard
