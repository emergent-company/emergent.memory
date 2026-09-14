# Narrow the `MemoryBackend` god-interface

**Status:** proposed
**Created:** 2026-09-11
**Source:** [2026-09-11-source-audit-hardening](../sessions/2026-09-11-source-audit-hardening.md)

## What

`gateway/backend.go` declares a single `MemoryBackend` interface with ~167 methods, and
handlers take that whole interface. Split it into small per-consumer interfaces (e.g.
`agentDefinitions`, `providers`, `sessions`, `usage`, …) composed into `MemoryBackend` for
`*MemoryClient`, and have each handler/`Server` depend only on the narrow interface it uses.

## Why

Every new backend method forces edits to the interface **and** to every test double; the
red-master incident on 2026-09-11 (#81) happened precisely because a PR added handlers and
client methods but forgot the interface declaration. Narrow interfaces make the dependency
explicit and shrink the fake surface.

## Depends on

- The `memory.go` domain split (done, #73) already groups the concrete methods by domain —
  use that grouping as the interface boundary.

## Notes

- Start with the highest-churn domains; `fakeMemory` should implement only the narrow
  interfaces each test needs.
- Do not change behavior — this is a type/API-shape refactor. Wire the interfaces in
  `gateway/handlers.go` / `gateway/main.go`.
- Consider a `var _ MemoryBackend = (*MemoryClient)(nil)` guard (already present) plus
  `var _ MemoryBackend = (*fakeMemory)(nil)` so interface/fake drift fails at test compile.
