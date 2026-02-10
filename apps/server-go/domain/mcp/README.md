# Model Context Protocol (MCP) Server

The Emergent MCP server provides AI assistants with **self-documenting, schema-aware access** to the knowledge graph via the [Model Context Protocol](https://modelcontextprotocol.io/).

## Overview

This implementation exposes three core MCP capabilities:

1. **Tools** (18) - Execute operations on the knowledge graph (search, create, query)
2. **Resources** (6) - Browse schema, templates, and project metadata
3. **Prompts** (5) - Guided workflows for common tasks

## Quick Start

### HTTP Transport

```bash
# Initialize session
curl -X POST http://localhost:5300/api/mcp/rpc \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-key" \
  -H "X-Project-ID: project-uuid" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2025-11-25",
      "capabilities": {},
      "clientInfo": {"name": "test", "version": "1.0"}
    }
  }'

# List all resources
curl -X POST http://localhost:5300/api/mcp/rpc \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-key" \
  -H "X-Project-ID: project-uuid" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "resources/list"
  }'
```

### SSE Transport

```bash
# Connect via Server-Sent Events
curl -N http://localhost:5300/mcp/sse \
  -H "X-API-Key: your-key" \
  -H "X-Project-ID: project-uuid"
```

**SSE Configuration:**

- **Connection Timeout**: 10 minutes (600s)
- **Ping Interval**: 4 minutes (240s)
- **Auto-Reconnect**: Client should reconnect on disconnect

## Authentication

All requests require:

- `X-API-Key` header with valid API token
- `X-Project-ID` header with target project UUID

## Protocol Methods

### Core Methods

| Method           | Description              | Params                            | Returns                   |
| ---------------- | ------------------------ | --------------------------------- | ------------------------- |
| `initialize`     | Start session            | `protocolVersion`, `capabilities` | Server capabilities       |
| `tools/list`     | List available tools     | -                                 | Array of tool definitions |
| `tools/call`     | Execute a tool           | `name`, `arguments`               | Tool result               |
| `resources/list` | List available resources | -                                 | Array of resource URIs    |
| `resources/read` | Read resource content    | `uri`                             | Resource contents         |
| `prompts/list`   | List available prompts   | -                                 | Array of prompt templates |
| `prompts/get`    | Generate prompt          | `name`, `arguments`               | Formatted prompt          |

## Resources (Self-Documenting Context)

Resources provide **read-only access** to schema, templates, and project metadata.

### Global Resources (No Project Required)

#### `emergent://schema/entity-types`

**Entity Type Catalog** - All registered entity types with counts

```json
{
  "uri": "emergent://schema/entity-types",
  "mimeType": "application/json",
  "text": "{\"entity_types\": [{\"name\": \"Person\", \"count\": 142, ...}]}"
}
```

**Use When:**

- Discovering what entity types exist
- Understanding schema structure
- Finding type-specific counts

---

#### `emergent://schema/relationships`

**Relationship Type Registry** - All valid relationship types

```json
{
  "uri": "emergent://schema/relationships",
  "mimeType": "application/json",
  "text": "{\"relationship_types\": [{\"name\": \"works_for\", ...}]}"
}
```

**Use When:**

- Discovering valid relationship types
- Understanding graph connections
- Designing queries

---

#### `emergent://templates/catalog`

**Template Pack Catalog** - All available template packs

```json
{
  "uri": "emergent://templates/catalog",
  "mimeType": "application/json",
  "text": "{\"template_packs\": [{\"id\": \"...\", \"name\": \"Research Project\", ...}]}"
}
```

**Use When:**

- Browsing available templates
- Finding templates for specific domains
- Understanding template structure

---

### Project-Scoped Resources

Require `X-Project-ID` header or `project_id` in URI.

#### `emergent://project/{id}/metadata`

**Project Statistics** - Entity/relationship counts, recent activity

```json
{
  "uri": "emergent://project/uuid/metadata",
  "mimeType": "application/json",
  "text": "{\"project_id\": \"...\", \"total_entities\": 1523, \"total_relationships\": 842, ...}"
}
```

**Use When:**

- Understanding project scope
- Monitoring graph growth
- Checking recent activity

---

#### `emergent://project/{id}/recent-entities`

**Recent Entities** - Last 50 modified entities

```json
{
  "uri": "emergent://project/uuid/recent-entities",
  "mimeType": "application/json",
  "text": "{\"entities\": [{\"id\": \"...\", \"type_name\": \"Person\", \"name\": \"Jane Doe\", ...}]}"
}
```

**Use When:**

- Tracking recent changes
- Finding recently added entities
- Understanding user activity

---

#### `emergent://project/{id}/templates`

**Installed Templates** - Template packs installed in project

```json
{
  "uri": "emergent://project/uuid/templates",
  "mimeType": "application/json",
  "text": "{\"installed_packs\": [{\"pack_id\": \"...\", \"installed_at\": \"...\", ...}]}"
}
```

**Use When:**

- Checking which templates are available
- Understanding project configuration
- Finding template-based creation options

---

## Prompts (Guided Workflows)

Prompts generate **formatted guidance** for common tasks. Each prompt accepts arguments and returns a structured message.

### Available Prompts

#### `explore_entity_type`

**Browse Specific Entity Type** - Paginated exploration with filtering

**Arguments:**

- `entity_type` (required) - Type name to explore (e.g., "Person", "Organization")
- `limit` (optional, default: 50) - Results per page
- `offset` (optional, default: 0) - Pagination offset

**Example:**

```json
{
  "method": "prompts/get",
  "params": {
    "name": "explore_entity_type",
    "arguments": {
      "entity_type": "Decision",
      "limit": 20
    }
  }
}
```

**Returns:**

- Guidance on exploring entities
- Tool call: `search_entities` with pre-filled parameters
- Example usage patterns

---

#### `create_from_template`

**Template-Based Entity Creation** - Step-by-step creation workflow

**Arguments:**

- `template_pack_id` (required) - UUID of template pack
- `entity_type` (optional) - Specific entity type from pack

**Example:**

```json
{
  "method": "prompts/get",
  "params": {
    "name": "create_from_template",
    "arguments": {
      "template_pack_id": "pack-uuid",
      "entity_type": "ResearchQuestion"
    }
  }
}
```

**Returns:**

- Template selection guidance
- Required fields explanation
- Tool call: `create_entity` with template structure

---

#### `analyze_relationships`

**Relationship Discovery** - Find connections between entities

**Arguments:**

- `entity_id` (optional) - Starting entity UUID
- `depth` (optional, default: 1) - Traversal depth (1-3)
- `relationship_type` (optional) - Filter by relationship type

**Example:**

```json
{
  "method": "prompts/get",
  "params": {
    "name": "analyze_relationships",
    "arguments": {
      "entity_id": "entity-uuid",
      "depth": 2,
      "relationship_type": "collaborates_with"
    }
  }
}
```

**Returns:**

- Relationship exploration strategy
- Tool calls: `get_relationships`, `traverse_graph`
- Visualization suggestions

---

#### `setup_research_project`

**Complete Research Project Setup** - Multi-step project initialization

**Arguments:**

- `research_domain` (required) - Domain/field of research
- `include_templates` (optional, default: true) - Install domain templates

**Example:**

```json
{
  "method": "prompts/get",
  "params": {
    "name": "setup_research_project",
    "arguments": {
      "research_domain": "machine learning",
      "include_templates": true
    }
  }
}
```

**Returns:**

- Step-by-step setup guide
- Entity type recommendations
- Template pack suggestions
- Tool calls: `create_entity_type`, `install_template_pack`

---

#### `find_related_entities`

**Graph Traversal** - Discover connected entities

**Arguments:**

- `starting_entity_name` (required) - Name to search for
- `relationship_types` (optional) - Filter by specific relationship types
- `max_depth` (optional, default: 2) - Maximum traversal depth

**Example:**

```json
{
  "method": "prompts/get",
  "params": {
    "name": "find_related_entities",
    "arguments": {
      "starting_entity_name": "John Doe",
      "relationship_types": ["works_for", "manages"],
      "max_depth": 3
    }
  }
}
```

**Returns:**

- Search strategy
- Traversal guidance
- Tool calls: `search_entities`, `get_relationships`, `traverse_graph`

---

## Tools (Operations)

18 tools for graph operations. See [MCP_TOOLS.md](./MCP_TOOLS.md) for complete reference.

**Categories:**

- **Entity Management** (5): Create, read, update, delete, search
- **Relationship Management** (3): Create, read, traverse
- **Schema Management** (4): Types, templates, field definitions
- **Search \u0026 Discovery** (3): Vector search, hybrid search, suggestions
- **Project Management** (3): CRUD operations on projects

## Error Handling

All methods return standard JSON-RPC 2.0 error responses:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32603,
    "message": "Entity not found",
    "data": {
      "entity_id": "...",
      "project_id": "..."
    }
  }
}
```

**Common Error Codes:**

- `-32700` - Parse error (invalid JSON)
- `-32600` - Invalid request (missing params)
- `-32601` - Method not found
- `-32603` - Internal error (database, validation)

## Performance \u0026 Limits

### Resource Limits

- **Recent Entities**: Maximum 50 items
- **Entity Search**: Default 50, max 100 per page
- **Traversal Depth**: Maximum 5 levels (recommended: 1-3)

### Timeout Configuration

- **HTTP Request**: 30 seconds
- **SSE Connection**: 10 minutes (600s)
- **SSE Ping Interval**: 4 minutes (240s)

### Caching

- Schema metadata cached for 5 minutes
- Template catalogs cached for 10 minutes
- Project metadata refreshed on-demand

## Best Practices

### 1. Start with Resources

Before calling tools, browse resources to understand:

- Available entity types (`emergent://schema/entity-types`)
- Valid relationships (`emergent://schema/relationships`)
- Project context (`emergent://project/{id}/metadata`)

### 2. Use Prompts for Guidance

Instead of manually constructing tool calls:

1. Call `prompts/get` with task-appropriate prompt
2. Review returned guidance
3. Execute suggested tool calls

### 3. Handle Errors Gracefully

- Check resource existence before operations
- Validate entity types against schema
- Use search before create to avoid duplicates

### 4. Optimize Traversals

- Start with depth=1, increase if needed
- Filter by relationship type when possible
- Use recent entities for quick discovery

## Integration Examples

### Python Client

```python
import requests

class EmergentMCP:
    def __init__(self, base_url, api_key, project_id):
        self.base_url = base_url
        self.headers = {
            "Content-Type": "application/json",
            "X-API-Key": api_key,
            "X-Project-ID": project_id
        }

    def call_method(self, method, params=None):
        payload = {
            "jsonrpc": "2.0",
            "id": 1,
            "method": method,
            "params": params or {}
        }
        resp = requests.post(
            f"{self.base_url}/api/mcp/rpc",
            json=payload,
            headers=self.headers
        )
        return resp.json()["result"]

    # Resources
    def list_resources(self):
        return self.call_method("resources/list")

    def read_resource(self, uri):
        return self.call_method("resources/read", {"uri": uri})

    # Prompts
    def get_prompt(self, name, arguments):
        return self.call_method("prompts/get", {
            "name": name,
            "arguments": arguments
        })

    # Tools
    def search_entities(self, query, type_filter=None):
        return self.call_method("tools/call", {
            "name": "search_entities",
            "arguments": {
                "query": query,
                "type_filter": type_filter
            }
        })

# Usage
client = EmergentMCP(
    base_url="http://localhost:5300",
    api_key="your-key",
    project_id="project-uuid"
)

# Browse schema
resources = client.list_resources()
entity_types = client.read_resource("emergent://schema/entity-types")

# Get guidance
prompt = client.get_prompt("explore_entity_type", {"entity_type": "Person"})

# Execute operation
results = client.search_entities("John Doe", type_filter=["Person"])
```

### TypeScript Client

```typescript
interface MCPClient {
  callMethod<T>(method: string, params?: any): Promise<T>;
}

class EmergentMCP implements MCPClient {
  constructor(
    private baseURL: string,
    private apiKey: string,
    private projectID: string
  ) {}

  async callMethod<T>(method: string, params?: any): Promise<T> {
    const response = await fetch(`${this.baseURL}/api/mcp/rpc`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-API-Key': this.apiKey,
        'X-Project-ID': this.projectID,
      },
      body: JSON.stringify({
        jsonrpc: '2.0',
        id: 1,
        method,
        params: params || {},
      }),
    });
    const data = await response.json();
    if (data.error) throw new Error(data.error.message);
    return data.result;
  }

  // Resources
  listResources() {
    return this.callMethod('resources/list');
  }

  readResource(uri: string) {
    return this.callMethod('resources/read', { uri });
  }

  // Prompts
  getPrompt(name: string, arguments: any) {
    return this.callMethod('prompts/get', { name, arguments });
  }

  // Tools
  searchEntities(query: string, typeFilter?: string[]) {
    return this.callMethod('tools/call', {
      name: 'search_entities',
      arguments: { query, type_filter: typeFilter },
    });
  }
}

// Usage
const client = new EmergentMCP(
  'http://localhost:5300',
  'your-key',
  'project-uuid'
);

// Browse + Execute
const types = await client.readResource('emergent://schema/entity-types');
const prompt = await client.getPrompt('explore_entity_type', {
  entity_type: 'Decision',
});
const results = await client.searchEntities('strategic decisions');
```

## Troubleshooting

### Common Issues

**"Method not found" error:**

- Check method name spelling (case-sensitive)
- Verify protocol version in initialize (2025-11-25)

**"Resource not found" error:**

- Verify URI format: `emergent://category/resource`
- For project resources, ensure `X-Project-ID` header is set
- Check resource list: `resources/list`

**"Invalid arguments" error:**

- Check prompt argument names (case-sensitive)
- Required arguments must be provided
- Validate argument types (string, number, boolean)

**SSE Connection Drops:**

- Normal after 10 minutes (configured timeout)
- Implement auto-reconnect in client
- Check firewall/proxy settings

### Debug Mode

Enable verbose logging:

```bash
LOG_LEVEL=debug /usr/local/go/bin/go run ./cmd/server
```

Check logs:

```bash
tail -f logs/mcp.log | grep -E "(RESOURCE|PROMPT|TOOL)"
```

## Development

### Adding a New Resource

1. **Define resource URI** in `service.go`:

   ```go
   const ResourceNewFeature = "emergent://category/new-feature"
   ```

2. **Add to resource list** in `listResources()`:

   ```go
   {
       URI:         ResourceNewFeature,
       Name:        "New Feature Resource",
       Description: "Description of what it provides",
       MimeType:    "application/json",
   }
   ```

3. **Implement reader** in `service.go`:

   ```go
   func (s *Service) readNewFeatureResource(ctx context.Context, projectID string) (string, error) {
       // Fetch data
       // Return JSON string
   }
   ```

4. **Add to router** in `readResource()`:
   ```go
   case ResourceNewFeature:
       return s.readNewFeatureResource(ctx, projectID)
   ```

### Adding a New Prompt

1. **Define prompt definition** in `listPrompts()`:

   ```go
   {
       Name:        "new_workflow",
       Description: "Guide for new workflow",
       Arguments: []PromptArgument{
           {Name: "param1", Description: "Parameter description", Required: true},
       },
   }
   ```

2. **Implement generator** in `service.go`:

   ```go
   func (s *Service) generateNewWorkflowPrompt(args map[string]interface{}) (PromptMessage, error) {
       // Validate arguments
       // Generate guidance
       // Return PromptMessage
   }
   ```

3. **Add to router** in `getPrompt()`:
   ```go
   case "new_workflow":
       return s.generateNewWorkflowPrompt(args)
   ```

### Testing

Run E2E tests:

```bash
cd apps/server-go
POSTGRES_PASSWORD=... /usr/local/go/bin/go test ./tests/e2e/... -run "TestMCP" -v
```

Test via HTTP:

```bash
./scripts/test_mcp.sh
```

## References

- [MCP Specification](https://modelcontextprotocol.io/specification/draft)
- [Implementation Guide](./MCP_RESOURCES_PROMPTS_IMPLEMENTATION.md)
- [Timeout Optimization](./MCP_SSE_TIMEOUT_INCREASE.md)
- [Tools Reference](./MCP_TOOLS.md)

## Changelog

### 2025-02-10

- Added 6 resources for self-documenting context
- Added 5 prompts for guided workflows
- Increased SSE timeout to 10 minutes
- Optimized ping interval to 4 minutes
- 95% reduction in reconnection churn

### 2024-12-XX

- Initial MCP implementation
- 18 tools for graph operations
- HTTP and SSE transports
- Authentication via API keys
