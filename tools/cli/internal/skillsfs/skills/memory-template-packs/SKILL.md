---
name: memory-template-packs
description: Manage Emergent template packs — discover, install, and remove reusable sets of object and relationship types in a project. Use when the user wants to configure what types of knowledge objects a project can contain.
metadata:
  author: emergent
  version: "1.1"
---

Manage template packs using `emergent template-packs`. Template packs define reusable sets of object types and relationship types that can be installed into a project's knowledge graph schema.

> **New to Emergent?** Load the `memory-onboard` skill first — it walks through designing and installing a template pack from scratch.

## Concepts

- **Template pack** — a versioned bundle of `objectTypeSchemas` and `relationshipTypeSchemas`. Immutable once created; new versions get new IDs.
- **Installed pack** — a pack assigned to a specific project. Multiple packs can be installed; their types are merged into the project's compiled type registry.
- **Compiled types** — the merged view of all object + relationship types from all installed packs in a project.

---

## Commands (when available)

### List available template packs
```bash
emergent template-packs list
emergent template-packs list --output json
```

### Get pack details
```bash
emergent template-packs get <pack-id>
```
Shows object types, relationship types, version, description.

### Create a new pack
```bash
emergent template-packs create --file pack.json
```

Pack JSON structure:
```json
{
  "name": "my-pack",
  "version": "1.0",
  "description": "Object types for my domain",
  "objectTypeSchemas": [
    {
      "name": "Requirement",
      "label": "Requirement",
      "description": "A product requirement",
      "properties": {}
    }
  ],
  "relationshipTypeSchemas": [
    {
      "name": "implements",
      "label": "Implements",
      "fromTypes": ["Task"],
      "toTypes": ["Requirement"]
    }
  ]
}
```

### Validate a pack file before creating
```bash
emergent template-packs validate --file pack.json
```

### List installed packs in the current project
```bash
emergent template-packs installed
emergent template-packs installed --output json
```

### Install a pack into the current project
```bash
emergent template-packs install <pack-id>
```

### Uninstall a pack from the current project
```bash
emergent template-packs uninstall <pack-id>
```
Warns if objects exist using types from this pack.

### View compiled types (merged schema)
```bash
emergent template-packs compiled-types
emergent template-packs compiled-types --output json
```
Shows all object and relationship types available in the current project, with which pack each comes from.

---

## Workflow

1. **Set up a project schema**: `list` to find existing packs → `install <pack-id>` to add to project → `compiled-types` to verify
2. **Create a custom pack**: write a JSON file → `validate` to check → `create --file pack.json` → `install` the new pack
3. **Inspect project schema**: `compiled-types` to see all available types before creating objects
4. **Remove a pack**: `uninstall <pack-id>` — review the warning about affected objects before confirming

## Notes

- Pack IDs are UUIDs; use `list --output json` to find by name
- Packs are immutable — creating a pack with the same name but different content creates a new version with a new ID
- `--project-id` global flag selects the project for `installed`, `install`, `uninstall`, and `compiled-types`
- `list` and `create` are org-scoped (no project needed)
