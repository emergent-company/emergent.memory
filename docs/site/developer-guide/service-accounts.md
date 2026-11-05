# Service Account Setup (Machine-to-Machine Access)

Use this guide to provision a persistent API token for an AI agent or automated tool that needs read-only access across organisations, projects, and project data.

---

## How it works

The system has two tiers of superadmin access stored in `core.superadmins`:

| Role | Can call |
|---|---|
| `superadmin_full` | All `/api/superadmin/*` endpoints including destructive operations |
| `superadmin_readonly` | Read-only `/api/superadmin/*` endpoints (list orgs, projects, users, jobs) |

Service accounts are granted `superadmin_readonly`. Their `emt_` API token is an **account-level** token carrying the fine-grained read-only scopes `documents:read`, `search`, `graph:read`, `journal:read`, `schema:read`, `agents:read` and `projects:read` (the coarse `data:read` umbrella is retired from token creation).

Two different checks apply, and they are not interchangeable:

| Endpoint group | What is checked |
|---|---|
| `/api/superadmin/*` | The `superadmin_readonly` role — a fresh service account works immediately |
| Project data (`/api/documents`, `/api/graph`, …) | Membership of the organisation that owns the project (or a project-bound `emt_` token) |

A freshly minted service account belongs to **no** organisation, so project data calls return `403 access to project denied` until you add it — see [Grant project access](#grant-project-access).

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
  "token":   "<emt_… token, shown once>",
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
Authorization: Bearer <service-account-token>
```

### Grant project access

Project data endpoints require the caller to be a member of the organisation that owns the project (`kb.organization_memberships`). Add the service user once per organisation it should read:

```sql
INSERT INTO kb.organization_memberships (organization_id, user_id, role)
VALUES ('<org-uuid>', '<service-account-user-uuid>', 'member');
```

Find the organisation ID with `GET /api/superadmin/organizations`; the user ID is the `userId` returned when the token was created.

### Read project data (documents, graph, traces)

With org membership granted, include the project ID as a header:

```
GET /api/documents
Authorization: Bearer <service-account-token>
X-Project-ID: <project-uuid>
```

### Token scopes granted

| Scope | Access |
|---|---|
| `documents:read` | Documents and their content |
| `search` | Hybrid and semantic search |
| `graph:read` | Graph objects |
| `journal:read` | Project journal entries |
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
