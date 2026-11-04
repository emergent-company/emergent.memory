#!/usr/bin/env bash
# Run-message role guard (#1832).
#
# An agent turn is persisted in kb.agent_run_messages.role under the ADK event
# Author — the sanitized agent name ("coder", "Norwegian_Law_Assistant"), NOT
# the literal "assistant" (see persistEventContent and agents.IsAgentReplyRole).
# A consumer that equality-checks `role == "assistant"` therefore silently drops
# every real agent reply, with no error and often no test asserting the contract.
#
# This gate runs an AST-based Go guard (apps/server/cmd/runrole-guard) that fails
# when a run-message role is compared to the literal "assistant" with `==` / `!=`
# anywhere under apps/, unless the site carries a documented `runrole:allow`
# marker explaining why the comparison is not about run-message data.
#
# Provider-protocol literals are out of scope: apps/server/pkg/adk (OpenAI /
# Anthropic request+response roles) is exempt wholesale, and switch-case clauses
# are not flagged. Wired into CI via scripts/preflight/all.sh (the
# branch-protection-required `ci` job). The guard imports only the stdlib, so
# `go run` does not touch the network.
#
# Run from anywhere; resolves the repo root itself.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root/apps/server"

go run ./cmd/runrole-guard
