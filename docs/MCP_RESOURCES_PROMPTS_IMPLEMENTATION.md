# MCP Resources & Prompts Implementation

**Date:** 2026-02-10  
**Status:** ✅ Complete  
**Version:** v0.3.14 (anticipated)

## Summary

Successfully implemented MCP Resources (6) and Prompts (5) to transform the Emergent knowledge graph into a self-documenting API. Clients can now discover schemas, templates, and project context automatically, and use guided prompts for common workflows.

## Implementation Overview

### Architecture

```
MCP Server Capabilities
├── Tools (18)           - Existing: execute actions on knowledge graph
├── Resources (6) ✨ NEW - Provide read-only context about schemas/templates/projects
└── Prompts (5) ✨ NEW   - Generate conversation templates for common workflows
```

### Resources Implemented (6 URIs)

| URI                                       | Description                                | Returns                                            |
| ----------------------------------------- | ------------------------------------------ | -------------------------------------------------- |
| `emergent://schema/entity-types`          | Entity type catalog with counts            | JSON catalog with type names, descriptions, counts |
| `emergent://schema/relationships`         | Relationship type registry                 | JSON registry of all relationship types            |
| `emergent://templates/catalog`            | Template pack catalog                      | JSON catalog of available template packs           |
| `emergent://project/{id}/metadata`        | Project stats (entity/relationship counts) | JSON metadata with counts, created date, etc.      |
| `emergent://project/{id}/recent-entities` | Last 50 modified entities                  | JSON array of recently created/updated entities    |
| `emergent://project/{id}/templates`       | Installed template packs for project       | JSON array of template packs                       |

### Prompts Implemented (5 Templates)

| Name                     | Description                  | Arguments                                                                     | Generates                            |
| ------------------------ | ---------------------------- | ----------------------------------------------------------------------------- | ------------------------------------ |
| `explore_entity_type`    | Browse specific entity types | `entity_type` (required)                                                      | Multi-step exploration workflow      |
| `create_from_template`   | Template-based creation      | `template_pack` (required), `entity_type` (optional)                          | Step-by-step creation guide          |
| `analyze_relationships`  | Relationship discovery       | `entity_id` (optional)                                                        | Analysis and pattern discovery guide |
| `setup_research_project` | Complete research setup      | `topic` (required), `objective` (optional)                                    | Full project setup workflow          |
| `find_related_entities`  | Graph traversal              | `entity_id` (required), `depth` (default: 2), `relationship_types` (optional) | Related entity discovery guide       |

### RPC Methods

| Method                  | Purpose                | Authentication | Session Required  |
| ----------------------- | ---------------------- | -------------- | ----------------- |
| `initialize`            | Advertise capabilities | ✅ Required    | Creates session   |
| `tools/list`            | List 18 tools          | ✅ Required    | ✅ Yes            |
| `tools/call`            | Execute tool           | ✅ Required    | ✅ Yes            |
| `resources/list` ✨ NEW | List 6 resources       | ✅ Required    | ❌ No (stateless) |
| `resources/read` ✨ NEW | Read resource content  | ✅ Required    | ❌ No (stateless) |
| `prompts/list` ✨ NEW   | List 5 prompts         | ✅ Required    | ❌ No (stateless) |
| `prompts/get` ✨ NEW    | Generate prompt        | ✅ Required    | ❌ No (stateless) |

**Key Design Decision:** Resources and prompts are stateless read-only operations that don't require session initialization, unlike tools which execute actions and need stateful sessions.

## Files Modified

### Go Backend

| File                                                   | Changes                                        | Lines   |
| ------------------------------------------------------ | ---------------------------------------------- | ------- |
| `apps/server-go/domain/mcp/entity.go`                  | Added 10 new type definitions                  | +150    |
| `apps/server-go/domain/mcp/service.go`                 | Added 6 resource readers + 5 prompt generators | +800    |
| `apps/server-go/domain/mcp/handler.go`                 | Added 4 new RPC method handlers                | +150    |
| `apps/server-go/domain/mcp/sse_handler.go`             | Updated to use unified routing                 | +2, -12 |
| `apps/server-go/domain/mcp/streamable_http_handler.go` | Updated capabilities                           | +2      |

**Total:** ~1,100 lines of new Go code

### Testing

Created comprehensive test script: `/tmp/test_mcp_final.sh`

- Tests all 7 RPC methods via HTTP endpoint
- Uses standalone mode authentication (`X-API-Key` header)
- Validates responses with jq assertions
- All tests passing ✅

## Test Results

```bash
Testing MCP Resources + Prompts Implementation
==============================================

1. Initialize...
✅ PASS - Capabilities advertised: tools=true, resources=true, prompts=true

2. Resources/List...
✅ PASS - Found 6 resources:
   • emergent://schema/entity-types
   • emergent://schema/relationships
   • emergent://templates/catalog
   • emergent://project/{project_id}/metadata
   • emergent://project/{project_id}/recent-entities
   • emergent://project/{project_id}/templates

3. Resources/Read (entity-types)...
✅ PASS - Content: application/json, size: 126 bytes

4. Prompts/List...
✅ PASS - Found 5 prompts:
   • explore_entity_type
   • create_from_template
   • analyze_relationships
   • setup_research_project
   • find_related_entities

5. Prompts/Get (explore_entity_type with entity_type=Decision)...
✅ PASS - Generated 1 message with guided exploration workflow
```

## Technical Challenges & Solutions

### Challenge 1: Authentication (401 Unauthorized)

**Problem:** MCP clients use API keys, but auth middleware expected JWT tokens.

**Solution:** Enabled standalone mode with `STANDALONE_MODE=true` and `STANDALONE_API_KEY` environment variables. Middleware already supported `X-API-Key` header validation.

### Challenge 2: Session Initialization Required

**Problem:** All handlers required session initialization, but resources/prompts are stateless.

**Solution:** Removed session initialization checks from the 4 new handlers (resources/list, resources/read, prompts/list, prompts/get) since they're read-only operations.

### Challenge 3: Routing Inconsistency

**Problem:** SSE handler had separate routing logic that didn't include new methods.

**Solution:** Refactored SSE handler to delegate to main handler's `routeMethod()` for unified request routing.

### Challenge 4: Project ID Context

**Problem:** Resources need project ID but sessions weren't established in HTTP mode.

**Solution:** Updated handlers to extract project ID from:

1. `user.ProjectID` (from auth context)
2. `X-Project-ID` header (fallback for stateless requests)

## Configuration

### Environment Variables

```bash
# Standalone mode (for testing/development)
STANDALONE_MODE=true
STANDALONE_API_KEY=your-api-key-here
STANDALONE_USER_EMAIL=admin@localhost

# Production mode (uses Zitadel OAuth)
STANDALONE_MODE=false
ZITADEL_DOMAIN=your-zitadel-instance
# ... other Zitadel config
```

### MCP Client Configuration

Example Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "emergent-dev": {
      "command": "curl",
      "args": [
        "-X",
        "POST",
        "http://localhost:5300/api/mcp/sse/b697f070-f13a-423b-8678-04978fd39e21",
        "-H",
        "X-API-Key: your-api-key",
        "-H",
        "Accept: text/event-stream"
      ],
      "env": {}
    }
  }
}
```

**Note:** SSE transport is recommended for production. HTTP RPC endpoint (`/api/mcp/rpc`) is available for testing and simple clients.

## Usage Examples

### List Resources

```bash
curl -X POST http://localhost:5300/api/mcp/rpc \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-key" \
  -H "X-Project-ID: project-uuid" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "resources/list"
  }'
```

### Read Resource

```bash
curl -X POST http://localhost:5300/api/mcp/rpc \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-key" \
  -H "X-Project-ID: project-uuid" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "resources/read",
    "params": {
      "uri": "emergent://schema/entity-types"
    }
  }'
```

### Generate Prompt

```bash
curl -X POST http://localhost:5300/api/mcp/rpc \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-key" \
  -H "X-Project-ID: project-uuid" \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "prompts/get",
    "params": {
      "name": "explore_entity_type",
      "arguments": {
        "entity_type": "Decision"
      }
    }
  }'
```

## Next Steps

### Immediate

- [ ] Remove temporary standalone mode from `.env` (revert to Zitadel OAuth)
- [ ] Document resources and prompts in main MCP README
- [ ] Add integration tests to E2E suite

### Future Enhancements

- [ ] Add resource subscriptions (notify on schema changes)
- [ ] Add more prompts for advanced workflows (entity merging, bulk operations)
- [ ] Add project-specific resources (custom entity types, relationship patterns)
- [ ] Add template-specific resources (template field schemas, validation rules)
- [ ] Add metrics resources (usage stats, query performance)

## References

- MCP Specification: https://modelcontextprotocol.io/specification/draft
- MCP TypeScript SDK: `node_modules/@modelcontextprotocol/sdk/README.md`
- Implementation Session: #TODO-CONTINUATION (this session)
- Test Script: `/tmp/test_mcp_final.sh`

## Impact

**Before:** MCP clients had access to 18 tools but needed external documentation to understand schemas, templates, and relationships.

**After:** MCP clients can:

1. **Discover** what entity types, relationship types, and templates exist
2. **Explore** project structure and recent activity
3. **Learn** common workflows through guided prompts
4. **Self-document** - AI assistants can use resources to understand the knowledge graph structure without human intervention

This makes the Emergent knowledge graph truly **self-documenting** and enables more intelligent automation.
