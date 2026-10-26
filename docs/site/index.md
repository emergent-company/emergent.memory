# Memory Documentation

**Memory** is a graph-native AI memory platform. This site documents how to use the platform, integrate with it, and build on top of it.

## Guides

### User Guide

Step-by-step guides for end users of the Memory platform — knowledge graph, documents, agents, chat, and more.

[User Guide](user-guide/index.md){ .md-button .md-button--primary }

### Developer Guide

Reference for developers integrating with or extending Memory — provider setup, type registry, MCP servers, extraction pipeline, security scopes.

[Developer Guide](developer-guide/index.md){ .md-button .md-button--primary }

---

## SDKs

### Go SDK

The Go SDK provides a fully type-safe client library covering the complete Memory API surface — 30 service clients, dual authentication, multi-tenancy, streaming SSE, and structured error handling.

**Module:** `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk`

[Get Started with Go SDK](go-sdk/index.md){ .md-button .md-button--primary }
[Reference](go-sdk/reference/graph.md){ .md-button }

### Swift SDK

The Swift surface is the `EmergentKit` package under [`swift/EmergentKit`](https://github.com/emergent-company/emergent.memory/tree/main/swift/EmergentKit), used by the iOS and Mac apps in this repository. It is not yet published as a standalone Swift Package, so it has no reference documentation on this site.

### Python SDK

The Python SDK (`emergent-memory`) provides a full-featured async-capable client library for the Memory API — 13 sub-clients, three auth modes, SSE streaming, and structured error handling.

**Package:** `emergent-memory` (PyPI)

**Source:** `sdk/python/` in this repo

[Python SDK README](https://github.com/emergent-company/emergent.memory/blob/main/sdk/python/README.md){ .md-button .md-button--primary }

---

## LLM Reference Files

For LLM context injection, flat markdown reference files are available:

- [`docs/llms.md`](https://github.com/emergent-company/emergent.memory/blob/main/docs/llms.md) — Combined reference
- [`docs/llms-go-sdk.md`](https://github.com/emergent-company/emergent.memory/blob/main/docs/llms-go-sdk.md) — Go SDK reference
- [`docs/llms-python-sdk.md`](https://github.com/emergent-company/emergent.memory/blob/main/docs/llms-python-sdk.md) — Python SDK reference
