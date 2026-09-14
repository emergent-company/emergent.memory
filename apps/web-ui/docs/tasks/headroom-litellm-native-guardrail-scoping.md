# Evaluate native LiteLLM Headroom guardrail for scoped compression

**Status:** proposed
**Created:** 2026-09-10
**Source:** [2026-09-10-headroom-proxy-install](../sessions/2026-09-10-headroom-proxy-install.md)

## What
If/when the tailnet `litellm` host is upgraded to LiteLLM ≥ 1.92, evaluate replacing (or
complementing) the local Headroom proxy with LiteLLM's native `headroom` guardrail so compression can
be scoped per virtual API key, per request, or per model group.

## Why
The gateway currently runs LiteLLM **v1.88.0**, which predates the native Headroom guardrail. The
local-proxy approach compresses all of a client's traffic, but cannot scope compression to specific
keys/models at the gateway. The native guardrail is the supported way to compress only dev-agent keys
while leaving product/user-facing traffic untouched.

## Depends on
- LiteLLM host upgraded to ≥ 1.92.
- Headroom sidecar reachable over HTTP from the gateway (`HEADROOM_COMPRESS_ALLOW_REMOTE=1`).

## Notes
- Scoping options (cleanest first): per-key `guardrails` on virtual keys; per-request
  `"guardrails": ["headroom-compression"]` with `default_on: false` (pure OSS); per-model (Enterprise).
- Confirm OSS vs Enterprise availability for per-key guardrail attachment against the installed
  version/license before committing to it.
- If adopted, decide whether the local alfred-dev proxy remains as a fallback or is retired.
