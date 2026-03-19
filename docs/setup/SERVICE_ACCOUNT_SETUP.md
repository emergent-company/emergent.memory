# Service Account Setup (Machine-to-Machine Access)

Use this guide to provision a persistent API token for an AI agent or automated tool that needs read-only access across all organisations, projects, and data.

---

## How it works

The system has two tiers of superadmin access stored in `core.superadmins`:

| Role | Can call |
|---|---|
| `superadmin_full` | All `/api/superadmin/*` endpoints including destructive operations |
| `superadmin_readonly` | Read-only `/api/superadmin/*` endpoints (list orgs, projects, users, jobs) |

Service accounts are granted `superadmin_readonly`. Their `emt_` API token also carries read-only data scopes (`data:read`, `schema:read`, `agents:read`, `projects:read`), which unlocks read access to any project's documents, graph, and traces when combined with an `X-Project-ID` header.

**Note:** There is no project membership check on data endpoints — authentication + a valid project ID is sufficient for read access.

---

## Prerequisites

Creating a service account requires **`superadmin_full`** role. This role can only be granted via direct database access (it is intentionally not self-serviceable through the API).

### Step 1 — Grant yourself `superadmin_full` (one-time, requires DB access)

Find your user ID:

```sql
SELECT id, display_name, zitadel_user_id
FROM core.user_profiles
WHERE deleted_at IS NULL
ORDER BY created_at;
```

Grant the role:

```sql
INSERT INTO core.superadmins (user_id, role, notes)
VALUES ('<your-user-uuid>', 'superadmin_full', 'Initial platform admin');
```

---

## Creating a service account token

Once you have `superadmin_full`, call the API — no further DB access needed.

### Request

```
POST /api/superadmin/service-tokens
Authorization: Bearer <your-session-or-api-token>
Content-Type: application/json

{
  "name": "ai-agent",
  "notes": "Claude coding agent — readonly access"
}
```

### Response

```json
{
  "userId":  "a1b2c3d4-...",
  "tokenId": "e5f6g7h8-...",
  "token":   "emt_abcdef1234...",
  "name":    "ai-agent"
}
```

> **The `token` value is shown only once.** Store it immediately — it cannot be retrieved again.

---

## Using the token

### Discover all organisations and projects

No project header needed for superadmin discovery endpoints:

```
GET /api/superadmin/organizations
GET /api/superadmin/projects?limit=100
Authorization: Bearer emt_...
```

### Read project data (documents, graph, traces)

Include the project ID as a header:

```
GET /api/documents
Authorization: Bearer emt_...
X-Project-ID: <project-uuid>
```

### Token scopes granted

| Scope | Access |
|---|---|
| `data:read` | Documents, chunks, search, graph, extraction, schema, tasks |
| `schema:read` | Graph schemas |
| `agents:read` | Chat sessions |
| `projects:read` | Project metadata |

Write operations are not permitted with this token.

---

## Revoking a service account

Service account tokens appear in `core.api_tokens` and can be revoked via the standard token management endpoints or directly in the DB:

```sql
-- Find the token
SELECT id, name, token_prefix, created_at
FROM core.api_tokens
WHERE user_id = '<service-account-user-uuid>'
  AND revoked_at IS NULL;

-- Revoke it
UPDATE core.api_tokens
SET revoked_at = NOW()
WHERE id = '<token-uuid>';
```

To fully remove the service account:

```sql
-- Revoke superadmin role
UPDATE core.superadmins
SET revoked_at = NOW(), revoked_by = '<your-user-uuid>'
WHERE user_id = '<service-account-user-uuid>';

-- Soft-delete the synthetic user
UPDATE core.user_profiles
SET deleted_at = NOW(), deleted_by = '<your-user-uuid>'
WHERE id = '<service-account-user-uuid>';
```

---

## Implementation reference

| Component | Location |
|---|---|
| Endpoint handler | `apps/server/domain/superadmin/handler.go` → `CreateServiceToken` |
| Route | `POST /api/superadmin/service-tokens` |
| Repository methods | `apps/server/domain/superadmin/repository.go` → `CreateServiceUser`, `GrantSuperadminToUser` |
| Token creation | delegates to `apitoken.Service.CreateAccountToken` |
| Superadmin role check | `core.superadmins` table, `revoked_at IS NULL` |
