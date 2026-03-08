# Type Registry

The type registry defines the schema of objects that can live in your project's knowledge graph. Every object has a **type name**, and the registry stores the JSON Schema, display config, and extraction hints for each type.

## Concepts

| Concept | Description |
|---|---|
| **Type** | A named schema: what properties an object can have and how it behaves |
| **Source** | Where the type came from: `template`, `custom`, or `discovered` |
| **Embedding policy** | Controls whether and how objects of this type are embedded for vector search |

---

## Type sources

| Source | Description |
|---|---|
| `template` | Installed from a template pack |
| `custom` | Created directly by a developer |
| `discovered` | Proposed automatically by the extraction pipeline |

---

## Managing types via API

### List types

```bash
curl https://api.dev.emergent-company.ai/api/type-registry/projects/<projectId> \
  -H "Authorization: Bearer <token>"
```

### Get a specific type

```bash
curl https://api.dev.emergent-company.ai/api/type-registry/projects/<projectId>/types/Person \
  -H "Authorization: Bearer <token>"
```

### Get type stats

```bash
curl https://api.dev.emergent-company.ai/api/type-registry/projects/<projectId>/stats \
  -H "Authorization: Bearer <token>"
```

Returns counts of total / enabled / by-source types.

### Create a custom type

```bash
curl -X POST https://api.dev.emergent-company.ai/api/type-registry/projects/<projectId>/types \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "typeName": "Company",
    "description": "A business entity",
    "jsonSchema": {
      "type": "object",
      "properties": {
        "name":        { "type": "string" },
        "industry":    { "type": "string" },
        "founded":     { "type": "integer" },
        "headquarters":{ "type": "string" }
      },
      "required": ["name"]
    },
    "uiConfig": {
      "icon": "building",
      "color": "#4f46e5",
      "displayProperty": "name"
    },
    "extractionConfig": {
      "extractRelationships": true,
      "confidenceThreshold": 0.7
    },
    "enabled": true
  }'
```

### Update a type

```bash
curl -X PUT https://api.dev.emergent-company.ai/api/type-registry/projects/<projectId>/types/Company \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "description": "A business entity (updated)",
    "enabled": true
  }'
```

### Delete a type

```bash
curl -X DELETE https://api.dev.emergent-company.ai/api/type-registry/projects/<projectId>/types/Company \
  -H "Authorization: Bearer <token>"
```

!!! warning "Cascade impact"
    Deleting a type does not delete existing graph objects of that type. Those objects remain but will no longer be validated against the schema or included in extractions.

---

## Entity reference

**`ProjectObjectTypeRegistry`** — table `kb.project_object_type_registry`

| Field | Type | Description |
|---|---|---|
| `id` | UUID | Primary key |
| `projectId` | UUID | Owning project |
| `typeName` | string | Unique within the project |
| `source` | string | `template` \| `custom` \| `discovered` |
| `templatePackId` | UUID | Set when `source = "template"` |
| `schemaVersion` | int | Incremented on schema changes |
| `jsonSchema` | object | JSON Schema for object properties |
| `uiConfig` | object | Display hints for the admin UI |
| `extractionConfig` | object | Hints for the extraction pipeline |
| `enabled` | bool | Disabled types are skipped during extraction |
| `discoveryConfidence` | float | Set when `source = "discovered"` |
| `description` | string | Human-readable description |
| `createdBy` | UUID | User who created the type |
| `createdAt` | timestamp | |
| `updatedAt` | timestamp | |

---

## Embedding policies

Embedding policies control which objects receive vector embeddings. Each policy is scoped to a project and a type name.

### List policies

```bash
curl https://api.dev.emergent-company.ai/api/graph/embedding-policies \
  -H "Authorization: Bearer <token>"
```

### Create a policy

```bash
curl -X POST https://api.dev.emergent-company.ai/api/graph/embedding-policies \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "objectType": "Company",
    "enabled": true,
    "maxPropertySize": 4096,
    "requiredLabels": ["verified"],
    "excludedLabels": ["draft"],
    "relevantPaths": ["$.name", "$.industry", "$.description"],
    "excludedStatuses": ["archived"]
  }'
```

### Update a policy (PATCH — all fields optional)

```bash
curl -X PATCH https://api.dev.emergent-company.ai/api/graph/embedding-policies/<policyId> \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

### Delete a policy

```bash
curl -X DELETE https://api.dev.emergent-company.ai/api/graph/embedding-policies/<policyId> \
  -H "Authorization: Bearer <token>"
```

### Embedding policy fields

| Field | Description |
|---|---|
| `objectType` | Matches a type name in the type registry |
| `enabled` | Master on/off switch for this type |
| `maxPropertySize` | Max bytes of any single property passed to the embedding model |
| `requiredLabels` | Object must have **all** of these labels to be embedded |
| `excludedLabels` | Object must have **none** of these labels |
| `relevantPaths` | JSON paths to include in the embedding text (e.g. `$.name`) |
| `excludedStatuses` | Object status values that skip embedding |

---

## Workflow: adding a new entity type

1. **Define the schema** — create a type via the API with a JSON Schema describing your object's properties.
2. **Set UI config** — add an icon, color, and `displayProperty` so the admin UI renders it nicely.
3. **Set extraction config** — tell the extraction pipeline what confidence threshold to use and whether to extract relationships.
4. **Create an embedding policy** — decide which properties are relevant for vector search and any label/status filters.
5. **Run extraction** — trigger an extraction job on a document or datasource; new objects of your type will be created.
