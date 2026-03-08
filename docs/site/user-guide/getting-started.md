# Getting Started

This guide walks you through the first-time setup: creating an organization, setting up a project, inviting your team, and configuring your profile.

## 1. Create an Organization

An **organization** is the top-level tenant. All projects, members, and billing live under an org.

=== "Admin UI"
    1. Open the admin panel and sign in.
    2. Navigate to **Organizations** and click **New Organization**.
    3. Enter a name (1–120 characters) and confirm.

=== "CLI"
    ```bash
    memory orgs create --name "Acme Corp"
    ```

=== "API"
    ```http
    POST /api/orgs
    Content-Type: application/json

    { "name": "Acme Corp" }
    ```

    Response:
    ```json
    { "id": "org_abc123", "name": "Acme Corp", "createdAt": "..." }
    ```

---

## 2. Create a Project

A **project** is your isolated knowledge workspace. Objects, documents, agents, and branches all belong to a project.

=== "Admin UI"
    1. Inside your organization, click **New Project**.
    2. Give it a name and an optional purpose description.
    3. Choose whether to enable **automatic extraction** — the platform will extract graph objects from every document you upload.

=== "CLI"
    ```bash
    memory projects create --name "Product Research" --org <org-id>
    ```

=== "API"
    ```http
    POST /api/projects
    Content-Type: application/json

    {
      "name": "Product Research",
      "organizationId": "org_abc123",
      "kb_purpose": "Track competitor research and product decisions"
    }
    ```

### Project settings

| Field | Description |
|---|---|
| `name` | Display name for the project |
| `kb_purpose` | Optional: describes what knowledge this project holds (used as context by AI features) |
| `auto_extract_objects` | If true, documents are automatically processed for entity extraction on upload |
| `chat_prompt_template` | Optional: custom system prompt for chat conversations in this project |

---

## 3. Invite Team Members

Invite colleagues by email. They receive a link to accept the invitation and join your project or organization.

=== "Admin UI"
    Go to **Project Settings → Members → Invite**.

=== "API"
    ```http
    POST /api/invites
    Content-Type: application/json

    {
      "email": "colleague@example.com",
      "organizationId": "org_abc123",
      "projectId": "proj_xyz789",
      "role": "project_user"
    }
    ```

### Roles

| Role | Access |
|---|---|
| `project_admin` | Full project access: manage members, settings, agents, data sources |
| `project_user` | Read and write graph objects, documents, and chat |

### Managing pending invites

```http
GET /api/projects/{projectId}/invites      # list sent invites
DELETE /api/invites/{id}                   # revoke an invite
```

### Accepting an invite

When a user receives an invite email, they click the link which calls:

```http
POST /api/invites/accept
{ "token": "<token-from-email>" }
```

---

## 4. Manage Members

=== "Admin UI"
    Go to **Project Settings → Members** to view and remove members.

=== "API"
    ```http
    GET    /api/projects/{projectId}/members          # list members
    DELETE /api/projects/{projectId}/members/{userId} # remove a member
    ```

---

## 5. Set Up Your Profile

Update your display name and contact details.

=== "Admin UI"
    Click your avatar → **Profile Settings**.

=== "API"
    ```http
    GET /api/user/profile

    PUT /api/user/profile
    Content-Type: application/json

    {
      "firstName": "Jane",
      "lastName": "Smith",
      "displayName": "Jane",
      "phoneE164": "+15551234567"
    }
    ```

### Profile fields

| Field | Description |
|---|---|
| `firstName` / `lastName` | Your real name |
| `displayName` | How your name appears to teammates |
| `phoneE164` | Optional phone number in E.164 format |
| `avatarObjectKey` | Storage key for your avatar image (set via upload) |

---

## What's next?

Now that you have an org, a project, and your team, you're ready to start building your knowledge graph:

- [Add documents →](documents.md)
- [Create graph objects →](knowledge-graph.md)
- [Start a chat conversation →](chat.md)
