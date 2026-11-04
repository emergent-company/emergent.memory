#!/usr/bin/env bash
# Bun-model registry completeness (schemadrift, #1127).
#
# The schemadrift package guards two things:
#   1. (DB-less, here) every bun model in the source tree is registered in
#      internal/schemadrift/models.go, so a newly added model cannot silently
#      escape the drift guard; and
#   2. (DB-backed, in CI's test-db job) every registered model's columns match
#      the real migrated PostgreSQL schema — the #1093 ADKState drift class.
#
# This gate runs (1), the deterministic half that needs no database, as a Go
# guard (cmd/schemadrift-guard). The guard reflects over the model registry via
# bun's own dialect and cross-checks it against a static source census
# (census.go uses go/parser), so the result is deterministic: it either matches
# on every run or fails loudly on a real, named table. There is no baseline to
# drift — a violation is a genuine unregistered model.
#
# Run from anywhere; resolves the repo root itself. Wired into CI via
# scripts/preflight/all.sh (the branch-protection-required `ci` job). Unlike the
# stdlib-only sibling guards, this one imports the server module (the model
# registry references the full domain tree), so `go run` compiles the module
# graph.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root/apps/server"

go run ./cmd/schemadrift-guard
