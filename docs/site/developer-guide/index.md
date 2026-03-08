# Developer Guide

This guide covers topics for developers integrating with, extending, or operating the Memory platform.

<div class="grid cards" markdown>

-   **[Provider Setup](provider-setup.md)**

    Configure LLM providers (Google AI, Vertex AI) at the org or project level. Manage API keys, model overrides, and track token usage and costs.

-   **[Type Registry](type-registry.md)**

    Define custom object and relationship type schemas for your knowledge graph. Control what gets extracted, how it's displayed, and how it's embedded.

-   **[Template Packs](template-packs.md)**

    Bundle type schemas, UI configs, and extraction prompts into versioned packs. Assign packs to projects and compose compiled type registries.

-   **[MCP Servers](mcp-servers.md)**

    Register external MCP servers (stdio, SSE, HTTP) and the built-in Memory MCP server. Browse the official MCP registry and install community servers.

-   **[Workspaces](workspace.md)**

    Agent execution environments (Firecracker, E2B, gVisor). Manage workspace lifecycle, run tools inside sandboxes, host MCP servers as persistent containers.

-   **[Extraction Pipeline](extraction.md)**

    Monitor and control extraction jobs that turn documents into graph objects. View per-job logs including LLM calls, token counts, and created objects.

-   **[Email Setup](email-setup.md)**

    Configure Mailgun to enable transactional email (invitations, notifications). Understand the async job queue and delivery tracking.

-   **[Health & Ops](health-ops.md)**

    Health check endpoints, the diagnostics probe, debug stats, and the auto-generated OpenAPI spec.

-   **[Security Scopes](security-scopes.md)**

    Complete reference for all 38 permission scopes, umbrella scope expansions, and which scopes are assignable to API tokens.

</div>
