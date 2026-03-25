## Context

The MCP domain (`apps/server/domain/mcp/`) authenticates requests via `Authorization: Bearer <token>` or `X-API-Key` headers, mapping tokens to in-memory `Session` objects. The `apitoken` domain already supports project-scoped tokens with a `project_viewer` role restricted to `viewerReadOnlyScopes` (`data:read`, `schema:read`, `agents:read`, `projects:read`). The `invites` domain sends Handlebars-rendered emails via `emailSvc.Enqueue`. There is no existing flow for sharing MCP access without granting full project membership.

## Goals / Non-Goals

**Goals:**
- Allow admins to generate a read-only MCP API token for a project in one action
- Return a pre-formatted shareable connection string (MCP server URL + API key)
- Allow admins to send an email to one or more addresses containing the token, endpoint, and an agent setup guide
- Reuse existing `apitoken.Service.Create` and the `project_viewer` scope set — no new auth primitives

**Non-Goals:**
- Writable MCP sharing (read-only only)
- OAuth / PKCE flows for MCP access
- Per-tool or per-resource granularity within read-only scope
- Tracking which external agent used the token (beyond `LastUsedAt` on the token)
- Revoking tokens from the email link itself (revocation is via existing token management UI)

## Decisions

### 1. Token generation reuses `apitoken.Service.Create` with `project_viewer` scopes

**Decision:** Call the existing `POST /api/projects/{projectId}/tokens` endpoint (or its service layer directly) with `scopes = viewerReadOnlyScopes` and a system-generated name like `"MCP Read-Only Share — <date>"`.

**Rationale:** No new token type, no schema changes, no new auth middleware. The token appears in the existing token list and is revocable via the existing UI. Alternatives considered:
- *New token type with a `readonly_mcp` flag*: Adds schema complexity for no functional gain — the scope set already encodes read-only.
- *Short-lived tokens*: Adds UX friction (expiry management); admins can revoke manually.

### 2. Share link is a formatted string, not a redirect URL

**Decision:** The "share link" is a copyable snippet — not a web URL that redirects. The UI presents it as a pre-filled config block (e.g., JSON for Claude Desktop, TOML for Cursor) that the user copies and pastes into their agent config.

**Rationale:** MCP clients are configured via local config files, not browser redirects. A deep-link URL would require the agent to support a custom URL scheme, which none of the target clients (Claude Desktop, Cursor, Windsurf) currently do. The snippet format is immediately actionable.

**Format returned by the API:**
```json
{
  "token": "emt_...",
  "mcpUrl": "https://api.dev.emergent-company.ai/api/mcp",
  "projectId": "<uuid>",
  "snippets": {
    "claude_desktop": "{ \"mcpServers\": { \"memory\": { \"url\": \"...\", \"headers\": { \"X-API-Key\": \"emt_...\" } } } }",
    "cursor": "..."
  }
}
```

### 3. New backend endpoint: `POST /api/projects/{projectId}/mcp/share`

**Decision:** Add a new handler in `apps/server/domain/mcp/handler.go` (or a new `share_handler.go`) rather than extending `apitoken/handler.go`.

**Rationale:** The MCP domain owns the concept of MCP access and the snippet format. The apitoken domain owns raw token CRUD. Keeping the share endpoint in the MCP domain keeps the snippet-generation logic co-located with the MCP URL constants and client config templates.

**Request body:**
```json
{
  "name": "optional custom token name",
  "emails": ["alice@example.com", "bob@example.com"]  // optional
}
```

**Response:** token value + snippets (see Decision 2). If `emails` is provided, the email invite is also dispatched.

### 4. Email uses a new `mcp-invite` Handlebars template

**Decision:** Add `apps/server/templates/email/mcp-invite.hbs` following the existing Handlebars + MJML pattern used by `project-invitation.hbs`.

**Template variables:**
- `projectName`, `mcpUrl`, `apiKey`, `projectId`
- `snippets.claudeDesktop`, `snippets.cursor` (pre-formatted config blocks)
- `senderName` (the admin who triggered the share)

**Rationale:** Reusing `project-invitation.hbs` would conflate two different flows (org membership vs. MCP access). A dedicated template keeps the content focused on agent configuration rather than account setup.

### 5. No new database table

**Decision:** The generated token is stored in the existing `kb.api_tokens` table. No `mcp_shares` table.

**Rationale:** The token entity already has `Name`, `Scopes`, `ProjectID`, and `CreatedAt`. The name convention (`"MCP Read-Only Share — ..."`) makes these tokens identifiable in the list. Adding a join table would add migration complexity with no query benefit.

## Risks / Trade-offs

- **Token in email is a secret** → Mitigation: Email body instructs recipients not to forward; token can be revoked instantly from the UI. This is the same pattern used by all API-key-based SaaS products.
- **In-memory session map in MCP handler** → The existing `sessions` map is keyed by raw token string. Read-only tokens work identically to regular tokens here — no change needed, but the map is not persisted across restarts. Mitigation: sessions are re-initialized on the next MCP `initialize` call, which is standard MCP behavior.
- **Snippet format drift** → If MCP client config formats change (e.g., Claude Desktop updates its schema), snippets become stale. Mitigation: snippets are generated server-side, so a single code update fixes all future shares.
- **Email deliverability of API keys** → Some spam filters flag emails containing long random strings. Mitigation: wrap the key in a `<code>` block and keep surrounding text natural-language; monitor bounce rates.

## Migration Plan

1. Add `mcp-invite.hbs` email template (no migration needed)
2. Add `POST /api/projects/{projectId}/mcp/share` handler + service method
3. Register new route in `mcp/routes.go`
4. Add UI: "Share MCP Access" button in project MCP settings → modal with token display + email input
5. Deploy: standard hot-reload for Go changes; UI deploy via normal CI

No database migrations required. Rollback: remove the route and UI button — existing tokens created via this flow remain valid until manually revoked.

## Open Questions

- Should the generated token have a default expiry (e.g., 90 days) or be non-expiring? The current `ApiToken` entity supports `ExpiresAt` but the existing UI doesn't expose it.
- Which agent config formats should be included in the initial snippet set? (Claude Desktop confirmed; Cursor and Windsurf TBD based on their MCP config schemas.)
- Should the email be sent immediately (synchronous) or via the existing email queue (`emailSvc.Enqueue`)? Prefer queue for consistency with existing invite flow.
