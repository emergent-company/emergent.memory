package mcp

import (
	"go.uber.org/fx"
)

// Module provides MCP (Model Context Protocol) functionality
//
// Features:
// - JSON-RPC 2.0 over HTTP POST (/mcp/rpc)
// - SSE transport (/mcp/sse/:projectId)
// - Tools: schema_version, list_entity_types, query_entities, search_entities, get_entity_edges
// - Tools: create_entity, create_relationship, update_entity, delete_entity
// - Tools: list_template_packs, get_template_pack, get_available_templates, get_installed_templates
// - Tools: assign_template_pack, update_template_assignment, uninstall_template_pack
// - Tools: create_template_pack, delete_template_pack
var Module = fx.Module("mcp",
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Provide(NewSSEHandler),
	fx.Provide(NewStreamableHTTPHandler),
	fx.Invoke(RegisterRoutes),
)
