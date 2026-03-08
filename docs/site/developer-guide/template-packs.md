# Template Packs

Template packs are versioned bundles of object type schemas, relationship type schemas, UI configurations, and extraction prompts. They let you define a domain model once and apply it to many projects.

## Concepts

| Concept | Description |
|---|---|
| **Template Pack** | A versioned bundle: type schemas, relationship schemas, UI configs, extraction prompts |
| **Assignment** | A link between a pack and a project |
| **Compiled types** | The merged type registry view for a project, combining all active packs |

---

## Pack lifecycle

```
draft → published → (deprecated)
```

- **Draft** packs are not visible to projects.
- **Published** packs appear in the `available` list for all projects.
- **Deprecated** packs remain installed on existing projects but cannot be newly assigned.

---

## Managing packs (admin)

### Create a pack

```bash
curl -X POST https://api.dev.emergent-company.ai/api/template-packs \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "legal-entities",
    "version": "1.0.0",
    "description": "Object types for legal entity extraction",
    "author": "Emergent Company",
    "source": "official",
    "license": "MIT",
    "draft": false,
    "objectTypeSchemas": {
      "Contract": {
        "type": "object",
        "properties": {
          "title":       { "type": "string" },
          "parties":     { "type": "array", "items": { "type": "string" } },
          "signedDate":  { "type": "string", "format": "date" },
          "jurisdiction":{ "type": "string" }
        },
        "required": ["title"]
      },
      "Clause": {
        "type": "object",
        "properties": {
          "clauseType": { "type": "string" },
          "text":       { "type": "string" }
        }
      }
    },
    "relationshipTypeSchemas": {
      "CONTAINS_CLAUSE": {
        "from": "Contract",
        "to": "Clause"
      }
    },
    "uiConfigs": {
      "Contract": { "icon": "file-text", "color": "#0ea5e9", "displayProperty": "title" },
      "Clause":    { "icon": "paragraph", "color": "#8b5cf6", "displayProperty": "clauseType" }
    },
    "extractionPrompts": {
      "Contract": {
        "systemPrompt": "Extract contract entities from the document.",
        "exampleJson": "{\"title\": \"Service Agreement\", \"parties\": [\"Acme\", \"Widgets Inc.\"]}"
      }
    }
  }'
```

### Get a pack

```bash
curl https://api.dev.emergent-company.ai/api/template-packs/<packId> \
  -H "Authorization: Bearer <token>"
```

### Update a pack

```bash
curl -X PUT https://api.dev.emergent-company.ai/api/template-packs/<packId> \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"version": "1.0.1", "description": "Updated description"}'
```

### Delete a pack

```bash
curl -X DELETE https://api.dev.emergent-company.ai/api/template-packs/<packId> \
  -H "Authorization: Bearer <token>"
```

!!! warning "In-use packs"
    Deleting a pack that is currently assigned to any project returns `409 Conflict`. Remove all assignments first.

---

## Pack field reference

| Field | Type | Description |
|---|---|---|
| `name` | string | Unique identifier name |
| `version` | string | Semver string, e.g. `1.0.0` |
| `description` | string | Human-readable description |
| `author` | string | Pack author |
| `source` | string | `official`, `community`, or custom label |
| `license` | string | SPDX license ID, e.g. `MIT` |
| `repositoryUrl` | string | Source repository URL |
| `documentationUrl` | string | Docs URL |
| `objectTypeSchemas` | object | Map of type name → JSON Schema |
| `relationshipTypeSchemas` | object | Map of relationship type → schema |
| `uiConfigs` | object | Map of type name → UI config |
| `extractionPrompts` | object | Map of type name → prompt config |
| `checksum` | string | SHA-256 of canonical content (auto-computed) |
| `draft` | bool | `true` = not visible to projects |
| `publishedAt` | timestamp | When the pack was published |
| `deprecatedAt` | timestamp | When the pack was deprecated |

---

## Assigning packs to projects

### Browse available packs

```bash
curl https://api.dev.emergent-company.ai/api/template-packs/projects/<projectId>/available \
  -H "Authorization: Bearer <token>"
```

### List installed packs

```bash
curl https://api.dev.emergent-company.ai/api/template-packs/projects/<projectId>/installed \
  -H "Authorization: Bearer <token>"
```

### Assign a pack

```bash
curl -X POST https://api.dev.emergent-company.ai/api/template-packs/projects/<projectId>/assign \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"templatePackId": "<packId>"}'
```

This creates an assignment with `active: true` by default.

### Enable / disable an assignment

```bash
curl -X PATCH https://api.dev.emergent-company.ai/api/template-packs/projects/<projectId>/assignments/<assignmentId> \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"active": false}'
```

Inactive assignments do not contribute types to the compiled registry.

### Remove an assignment

```bash
curl -X DELETE https://api.dev.emergent-company.ai/api/template-packs/projects/<projectId>/assignments/<assignmentId> \
  -H "Authorization: Bearer <token>"
```

---

## Compiled types

The compiled types endpoint merges all **active** pack assignments for a project into a single flat type map. Later-assigned packs override earlier ones for the same type name.

```bash
curl https://api.dev.emergent-company.ai/api/template-packs/projects/<projectId>/compiled-types \
  -H "Authorization: Bearer <token>"
```

This is the type registry the extraction pipeline uses. Types registered here that are not yet in the project's type registry table are automatically added with `source = "template"` on the next extraction run.

---

## Blueprints

You can also install template packs via the CLI blueprints workflow:

```bash
memory blueprints ./my-pack-dir --project <projectId>
```

See the [Agents — Blueprints](../user-guide/agents.md#blueprints-gitops) section for the blueprint file format. Template pack blueprints use the same YAML-based declarative config.
