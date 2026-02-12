---
id: template-pack-creation
title: Template Pack Creation Guide
category: guides
tags: [template-packs, mcp, api, ai-agents]
description: Comprehensive guide for AI agents to create and manage template packs via MCP and REST API
lastUpdated: 2025-02-11
readTime: 15
related: [mcp-quick-reference, template-pack-examples]
---

# Template Pack Creation Guide

This guide explains how to create and manage **template packs** in Emergent. Template packs define reusable schemas for entity types, relationships, UI configurations, and extraction prompts.

## What is a Template Pack?

A **template pack** is a versioned collection of:

- **Entity type schemas** (required) - Define object types and their properties
- **Relationship type schemas** (optional) - Define connections between entities
- **UI configurations** (optional) - Control how entities are displayed
- **Extraction prompts** (optional) - Guide AI extraction for entity types

## Access Methods

Template packs can be created via:

1. **MCP Tool** - `create_template_pack` (AI agents, programmatic access)
2. **REST API** - `/api/template-packs/projects/:projectId/*` (HTTP clients)

Both methods use identical field definitions and validation.

---

## Field Definitions

### Required Fields

| Field                 | Type   | Description                                                      | Example                                         |
| --------------------- | ------ | ---------------------------------------------------------------- | ----------------------------------------------- |
| `name`                | string | Name of the template pack                                        | `"Research Project Template"`                   |
| `version`             | string | Semantic version string                                          | `"1.0.0"`                                       |
| `object_type_schemas` | object | JSON Schema definitions for entity types (at least one required) | See [Entity Type Schemas](#entity-type-schemas) |

### Optional Fields

| Field                       | Type   | Description                                    | Example                                                     |
| --------------------------- | ------ | ---------------------------------------------- | ----------------------------------------------------------- |
| `description`               | string | Description of the template pack               | `"Template for academic research projects"`                 |
| `author`                    | string | Author name or organization                    | `"Emergent Research Team"`                                  |
| `relationship_type_schemas` | object | JSON Schema definitions for relationship types | See [Relationship Type Schemas](#relationship-type-schemas) |
| `ui_configs`                | object | UI display configuration per entity type       | See [UI Configurations](#ui-configurations)                 |
| `extraction_prompts`        | object | AI extraction prompts per entity type          | See [Extraction Prompts](#extraction-prompts)               |

### Auto-Generated Fields

The following fields are **automatically generated** by the system and **cannot be specified** during creation:

| Field          | Type      | Description                                  | Format           |
| -------------- | --------- | -------------------------------------------- | ---------------- |
| `id`           | UUID      | Unique identifier                            | `"550e8400-..."` |
| `checksum`     | string    | Content hash for version tracking            | SHA-256 hash     |
| `draft`        | boolean   | Draft status (always `false` after creation) | `false`          |
| `published_at` | timestamp | Publication timestamp                        | RFC3339          |
| `created_at`   | timestamp | Creation timestamp                           | RFC3339          |
| `updated_at`   | timestamp | Last modification timestamp                  | RFC3339          |

**Date Format**: All timestamps use **RFC3339** format (e.g., `"2025-02-10T09:26:57Z"`).

---

## Entity Type Schemas

Entity type schemas use **JSON Schema** to define object properties, validation rules, and constraints.

### Minimal Example

```json
{
  "object_type_schemas": {
    "Person": {
      "type": "object",
      "properties": {
        "name": {
          "type": "string",
          "description": "Full name"
        },
        "email": {
          "type": "string",
          "format": "email"
        }
      },
      "required": ["name"]
    }
  }
}
```

### Advanced Example with Validation

```json
{
  "object_type_schemas": {
    "ResearchPaper": {
      "type": "object",
      "properties": {
        "title": {
          "type": "string",
          "minLength": 10,
          "maxLength": 500,
          "description": "Paper title"
        },
        "abstract": {
          "type": "string",
          "description": "Abstract or summary"
        },
        "publication_date": {
          "type": "string",
          "format": "date",
          "description": "Date published (YYYY-MM-DD)"
        },
        "authors": {
          "type": "array",
          "items": {
            "type": "string"
          },
          "minItems": 1,
          "description": "List of author names"
        },
        "citations": {
          "type": "integer",
          "minimum": 0,
          "description": "Number of citations"
        },
        "peer_reviewed": {
          "type": "boolean",
          "default": false
        },
        "tags": {
          "type": "array",
          "items": {
            "type": "string"
          },
          "uniqueItems": true
        }
      },
      "required": ["title", "authors", "publication_date"]
    }
  }
}
```

### Supported JSON Schema Features

| Feature        | Type        | Example                                        |
| -------------- | ----------- | ---------------------------------------------- |
| **String**     | `string`    | `"type": "string"`                             |
| **Number**     | `number`    | `"type": "number"`                             |
| **Integer**    | `integer`   | `"type": "integer", "minimum": 0`              |
| **Boolean**    | `boolean`   | `"type": "boolean", "default": false`          |
| **Array**      | `array`     | `"type": "array", "items": {"type": "string"}` |
| **Object**     | `object`    | `"type": "object", "properties": {...}`        |
| **Enum**       | `enum`      | `"enum": ["draft", "published", "archived"]`   |
| **Format**     | `format`    | `"format": "email"` / `"date"` / `"uri"`       |
| **Validation** | constraints | `minLength`, `maxLength`, `minimum`, `maximum` |
| **Required**   | `required`  | `"required": ["field1", "field2"]`             |

---

## Relationship Type Schemas

Define how entities can be connected to each other.

### Example

```json
{
  "relationship_type_schemas": {
    "authored_by": {
      "type": "object",
      "description": "Paper authored by Person",
      "sourceTypes": ["ResearchPaper"],
      "targetTypes": ["Person"],
      "properties": {
        "role": {
          "type": "string",
          "enum": ["primary", "co-author", "contributor"],
          "default": "co-author"
        },
        "affiliation_at_time": {
          "type": "string",
          "description": "Author's affiliation when paper was written"
        }
      }
    },
    "cites": {
      "type": "object",
      "description": "Paper cites another paper",
      "sourceTypes": ["ResearchPaper"],
      "targetTypes": ["ResearchPaper"],
      "properties": {
        "context": {
          "type": "string",
          "description": "How the citation is used"
        }
      }
    }
  }
}
```

### Fields

| Field         | Required | Description                                           |
| ------------- | -------- | ----------------------------------------------------- |
| `sourceTypes` | Yes      | Array of entity types that originate the relationship |
| `targetTypes` | Yes      | Array of entity types that receive the relationship   |
| `description` | No       | Human-readable description                            |
| `properties`  | No       | JSON Schema for relationship metadata                 |

> **Note:** The server also accepts `fromTypes`/`toTypes`, `source_types`/`target_types`, and singular `source`/`target` strings for backward compatibility, but `sourceTypes`/`targetTypes` is the canonical format.

---

## UI Configurations

Control how entities are displayed in the UI.

### Example

```json
{
  "ui_configs": {
    "ResearchPaper": {
      "icon": "file-text",
      "color": "#3B82F6",
      "display_template": "{{title}} ({{publication_date}})",
      "summary_fields": ["title", "authors", "publication_date"],
      "card_layout": {
        "title": "{{title}}",
        "subtitle": "{{authors}}",
        "metadata": ["publication_date", "citations", "peer_reviewed"]
      },
      "list_view": {
        "primary": "title",
        "secondary": "authors",
        "badge": "citations"
      }
    }
  }
}
```

### Configuration Options

| Field              | Type   | Description                            |
| ------------------ | ------ | -------------------------------------- |
| `icon`             | string | Icon name (Iconify or custom)          |
| `color`            | string | Hex color code for entity type         |
| `display_template` | string | Handlebars template for entity display |
| `summary_fields`   | array  | Fields to show in summary view         |
| `card_layout`      | object | Configuration for card view            |
| `list_view`        | object | Configuration for list view            |

---

## Extraction Prompts

Guide AI systems on how to extract entities from documents.

### Example

```json
{
  "extraction_prompts": {
    "ResearchPaper": {
      "system_prompt": "You are extracting research paper metadata from academic documents.",
      "extraction_instructions": "Identify the title, authors, publication date, and abstract. Look for DOI numbers and citation counts if available.",
      "examples": [
        {
          "input": "Machine Learning for Healthcare by J. Smith et al., published in Nature 2024.",
          "output": {
            "title": "Machine Learning for Healthcare",
            "authors": ["J. Smith"],
            "publication_date": "2024-01-01",
            "peer_reviewed": true
          }
        }
      ],
      "field_hints": {
        "title": "Usually at the top of the document in larger font",
        "authors": "Listed below title or at end with affiliations",
        "publication_date": "Check header, footer, or metadata",
        "abstract": "Section labeled 'Abstract' or 'Summary'"
      }
    }
  }
}
```

### Prompt Structure

| Field                     | Type   | Description                                      |
| ------------------------- | ------ | ------------------------------------------------ |
| `system_prompt`           | string | High-level instructions for the AI               |
| `extraction_instructions` | string | Detailed extraction guidance                     |
| `examples`                | array  | Example input/output pairs for few-shot learning |
| `field_hints`             | object | Per-field extraction hints                       |

---

## Complete Example

Here's a complete template pack for a research project domain:

```json
{
  "name": "Academic Research Template",
  "version": "1.0.0",
  "description": "Template pack for academic research projects with papers, people, and citations",
  "author": "Emergent Research Team",
  "object_type_schemas": {
    "ResearchPaper": {
      "type": "object",
      "properties": {
        "title": { "type": "string", "minLength": 10 },
        "abstract": { "type": "string" },
        "publication_date": { "type": "string", "format": "date" },
        "authors": {
          "type": "array",
          "items": { "type": "string" },
          "minItems": 1
        },
        "doi": { "type": "string", "pattern": "^10\\.\\d+/.+$" },
        "citations": { "type": "integer", "minimum": 0 },
        "peer_reviewed": { "type": "boolean", "default": false }
      },
      "required": ["title", "authors", "publication_date"]
    },
    "Person": {
      "type": "object",
      "properties": {
        "name": { "type": "string" },
        "email": { "type": "string", "format": "email" },
        "affiliation": { "type": "string" },
        "h_index": { "type": "integer", "minimum": 0 }
      },
      "required": ["name"]
    }
  },
  "relationship_type_schemas": {
    "authored_by": {
      "sourceTypes": ["ResearchPaper"],
      "targetTypes": ["Person"],
      "properties": {
        "role": {
          "type": "string",
          "enum": ["primary", "co-author", "contributor"]
        }
      }
    },
    "cites": {
      "sourceTypes": ["ResearchPaper"],
      "targetTypes": ["ResearchPaper"],
      "properties": {
        "context": { "type": "string" }
      }
    }
  },
  "ui_configs": {
    "ResearchPaper": {
      "icon": "file-text",
      "color": "#3B82F6",
      "display_template": "{{title}} ({{publication_date}})",
      "summary_fields": ["title", "authors", "publication_date"]
    },
    "Person": {
      "icon": "user",
      "color": "#10B981",
      "display_template": "{{name}} ({{affiliation}})",
      "summary_fields": ["name", "affiliation", "h_index"]
    }
  },
  "extraction_prompts": {
    "ResearchPaper": {
      "system_prompt": "Extract research paper metadata from academic documents.",
      "extraction_instructions": "Identify title, authors, publication date, abstract, and DOI.",
      "field_hints": {
        "title": "At the top in larger font",
        "authors": "Listed below title",
        "doi": "Digital Object Identifier (10.xxxx/yyyy format)"
      }
    }
  }
}
```

---

## Creating via MCP

### Using the `create_template_pack` Tool

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "create_template_pack",
    "arguments": {
      "name": "Academic Research Template",
      "version": "1.0.0",
      "description": "Template for research projects",
      "author": "Emergent Research Team",
      "object_type_schemas": {
        "ResearchPaper": {
          "type": "object",
          "properties": {
            "title": { "type": "string" },
            "authors": { "type": "array", "items": { "type": "string" } }
          },
          "required": ["title", "authors"]
        }
      }
    }
  }
}
```

### Response

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "Academic Research Template",
    "version": "1.0.0",
    "checksum": "a3f5b2c1...",
    "created_at": "2025-02-11T09:26:57Z",
    "published_at": "2025-02-11T09:26:57Z",
    "draft": false
  }
}
```

---

## Creating via REST API

### Endpoint

```
POST /api/template-packs/projects/:projectId/
```

### Headers

```
Content-Type: application/json
Authorization: Bearer <token>
```

### Request Body

Same structure as MCP arguments (see [Complete Example](#complete-example)).

### Response

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "Academic Research Template",
  "version": "1.0.0",
  "checksum": "a3f5b2c1...",
  "created_at": "2025-02-11T09:26:57Z",
  "published_at": "2025-02-11T09:26:57Z",
  "draft": false
}
```

---

## Validation Rules

### Required Validations

1. **Name** - Non-empty string
2. **Version** - Valid semantic version (e.g., `1.0.0`)
3. **Object Type Schemas** - At least one entity type defined
4. **JSON Schema Validity** - All schemas must be valid JSON Schema

### Recommended Validations

1. **Entity Type Names** - PascalCase (e.g., `ResearchPaper`, not `research_paper`)
2. **Relationship Type Names** - snake_case (e.g., `authored_by`, not `AuthoredBy`)
3. **Version Incrementing** - Follow semantic versioning rules
4. **Description** - Provide meaningful context for users

### Common Errors

| Error                       | Cause                                               | Fix                                        |
| --------------------------- | --------------------------------------------------- | ------------------------------------------ |
| `missing_required_field`    | Missing `name`, `version`, or `object_type_schemas` | Add all required fields                    |
| `invalid_version_format`    | Version not semver                                  | Use `X.Y.Z` format                         |
| `invalid_json_schema`       | Malformed JSON Schema                               | Validate against JSON Schema specification |
| `empty_object_type_schemas` | No entity types defined                             | Add at least one entity type               |
| `duplicate_template_name`   | Template with same name/version exists              | Increment version or change name           |

---

## Best Practices

### 1. Start Simple, Iterate

Begin with minimal schemas and expand based on usage:

```json
{
  "object_type_schemas": {
    "Task": {
      "type": "object",
      "properties": {
        "title": { "type": "string" },
        "completed": { "type": "boolean" }
      },
      "required": ["title"]
    }
  }
}
```

Then add more fields as needed in version `1.1.0`, `1.2.0`, etc.

### 2. Use Descriptive Names

- **Good**: `ResearchPaper`, `OrganizationUnit`, `ProjectMilestone`
- **Bad**: `Item`, `Thing`, `Data`

### 3. Provide Rich Metadata

- Always include `description` for entity types and fields
- Use `format` for typed fields (email, date, uri)
- Add `examples` in extraction prompts

### 4. Design for Relationships

Think about how entities connect:

```json
{
  "relationship_type_schemas": {
    "part_of": {
      "sourceTypes": ["Task"],
      "targetTypes": ["Project"]
    },
    "depends_on": {
      "sourceTypes": ["Task"],
      "targetTypes": ["Task"]
    }
  }
}
```

### 5. Version Semantically

- **Major** (1.0.0 → 2.0.0) - Breaking changes (removed fields, changed types)
- **Minor** (1.0.0 → 1.1.0) - New features (added fields, new entity types)
- **Patch** (1.0.0 → 1.0.1) - Bug fixes (description updates, UI tweaks)

### 6. Test Extraction Prompts

If using `extraction_prompts`, test with real documents to ensure AI extracts correctly.

---

## Deleting Template Packs

### Via MCP

```json
{
  "method": "tools/call",
  "params": {
    "name": "delete_template_pack",
    "arguments": {
      "pack_id": "550e8400-e29b-41d4-a716-446655440000"
    }
  }
}
```

### Via REST API

```
DELETE /api/template-packs/projects/:projectId/:packId
```

### Restrictions

- Cannot delete **system template packs** (built-in templates)
- Cannot delete packs that are **currently installed** in any project
- Must uninstall from all projects first using `uninstall_template_pack`

---

## Related Resources

- **[MCP Quick Reference](../api-reference/mcp-quick-reference.md)** - Complete MCP tool documentation
- **[Template Pack Examples](../examples/template-pack-examples.md)** - Real-world template pack examples
- **[MCP Tools Documentation](/root/emergent/apps/server-go/domain/mcp/MCP_TOOLS.md)** - Detailed tool specifications

---

## FAQ

### Q: Can I modify a template pack after creation?

**A**: No. Template packs are **immutable** once published. To make changes:

1. Create a new version (e.g., `1.0.0` → `1.1.0`)
2. Uninstall the old version from projects
3. Install the new version

### Q: What happens to entities if I delete a template pack?

**A**: You **cannot delete** a template pack that has entities associated with it. You must first:

1. Uninstall the pack from all projects
2. Migrate or delete entities using that pack
3. Then delete the pack

### Q: Can I have multiple versions of the same template pack?

**A**: Yes. Each `name` + `version` combination is unique. Projects can have different versions installed simultaneously (e.g., Project A uses `1.0.0`, Project B uses `1.1.0`).

### Q: How do I share template packs between projects?

**A**: Template packs are **global** by default. Once created, they appear in the **template catalog** (`emergent://templates/catalog` via MCP) and can be installed in any project using `assign_template_pack`.

### Q: What date format should I use for timestamps?

**A**: All timestamps **auto-generated by the system** use **RFC3339** format (e.g., `2025-02-10T09:26:57Z`). For **entity properties**, you can use any format defined in your JSON Schema (e.g., `"format": "date"` for YYYY-MM-DD).

---

## Summary

**Template packs** are the foundation of structured knowledge in Emergent:

- ✅ **Define once, use everywhere** - Reusable schemas across projects
- ✅ **Type-safe** - JSON Schema validation ensures data quality
- ✅ **AI-friendly** - Extraction prompts guide automated entity creation
- ✅ **Versioned** - Semantic versioning enables safe evolution
- ✅ **Accessible** - Available via MCP and REST API

Start with simple schemas, iterate based on usage, and leverage extraction prompts for AI-powered knowledge extraction.
Test content update
