# Emergent Documentation

**Emergent** is a graph-native AI memory platform. This site documents how to use the platform, integrate with it, and build on top of it.

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

The Go SDK provides a fully type-safe client library covering the complete Emergent API surface — 30 service clients, dual authentication, multi-tenancy, streaming SSE, and structured error handling.

**Module:** `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk`

[Get Started with Go SDK](go-sdk/index.md){ .md-button .md-button--primary }
[Reference](go-sdk/reference/graph.md){ .md-button }

### Swift SDK

The Swift Core layer (`EmergentAPIClient`) provides a lightweight async/await HTTP client for use in the Emergent Mac app, covering projects, graph objects, documents, agents, MCP servers, and user profile.

**Source:** `emergent-company/emergent.memory.mac` — `Emergent/Core/`

[Swift SDK Overview](swift-sdk/index.md){ .md-button .md-button--primary }

---

## LLM Reference Files

For LLM context injection, flat markdown reference files are available:

- [`docs/llms.md`](https://github.com/emergent-company/emergent/blob/main/docs/llms.md) — Combined reference
- [`docs/llms-go-sdk.md`](https://github.com/emergent-company/emergent/blob/main/docs/llms-go-sdk.md) — Go SDK reference
- [`docs/llms-swift-sdk.md`](https://github.com/emergent-company/emergent/blob/main/docs/llms-swift-sdk.md) — Swift SDK reference

---

!!! note "Admin: Enable GitHub Pages"
    This site is deployed via the `gh-pages` branch. To go live, a repo admin must enable
    GitHub Pages once: **Settings → Pages → Source: `gh-pages` branch**.
