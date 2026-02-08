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
var Module = fx.Module("mcp",
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Provide(NewSSEHandler),
	fx.Provide(NewStreamableHTTPHandler),
	fx.Invoke(RegisterRoutes),
)
