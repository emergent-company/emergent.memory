## Why

A sandbox-enabled agent chat fails with:

```
workspace provisioning failed: no provider available: no healthy providers available (fallback exhausted)
```

Two root causes make provider availability invisible and frozen:

1. **Health checks die ~15s after boot.** `registerProviders` starts the health monitor with the fx `OnStart` hook context, which fx cancels ~15s after startup (fx DefaultTimeout). The monitor goroutine runs its first `checkAllHealth`, then its `select { case <-ctx.Done(): return }` fires before the first 30s tick. Result: exactly one health check ever runs. A provider down at boot stays unhealthy forever; a provider up at boot stays "healthy" forever even if Docker dies later.
2. **Unavailable providers are silently omitted.** Only `gvisor` is registered on dev/prod (Firecracker skipped — no `/dev/kvm`; E2B skipped — no `E2B_API_KEY`). Those two are entirely absent from `GET /api/v1/agent/sandboxes/providers`, while the UI shows a hardcoded list of all three with no availability info.

## What Changes

- **Lifetime health monitoring:** the monitor goroutine detaches from the caller's context and runs for the whole process lifetime; shutdown is driven solely by an idempotent `StopHealthMonitoring`.
- **Always report every known provider:** `ListProviders` returns all three known provider types in a stable order, each with live health or an "unavailable + reason" status. Providers skipped at startup (KVM missing, API key unset, constructor failure) are recorded as known-but-unavailable via `Orchestrator.MarkUnavailable`.
- **Actionable selection errors:** `SelectProvider` / `SelectProviderWithFallback` append per-candidate rejection reasons so the failure message explains *why* each provider was rejected.

## Capabilities

### Modified Capabilities

- `agent-sandbox-providers`: provider availability is now always reported (with a reason) and health is refreshed periodically for the process lifetime; selection errors enumerate rejection reasons.

## Impact

### Code Changes
- `apps/server/domain/sandbox/orchestrator.go`
- `apps/server/domain/sandbox/module.go`
- `apps/server/domain/sandbox/dto.go`
- `apps/server/domain/sandbox/orchestrator_health_test.go` (new)
- `apps/server/domain/sandbox/orchestrator_test.go` (health-counting in test mock)

### API Changes
- `GET /api/v1/agent/sandboxes/providers` now returns all known providers, adds a `registered` field per entry, and omits `capabilities` for unregistered providers.

### Docs
- `docs/agent-sandbox/DEPLOYMENT.md`
- `docs/agent-sandbox/PROVIDER_SELECTION.md`
