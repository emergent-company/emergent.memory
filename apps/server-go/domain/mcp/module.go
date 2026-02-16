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
// - Tools (delegated to agents domain via AgentToolHandler):
//   - Agent Definitions: list_agent_definitions, get_agent_definition, create_agent_definition, update_agent_definition, delete_agent_definition
//   - Agents (runtime): list_agents, get_agent, create_agent, update_agent, delete_agent, trigger_agent
//   - Agent Runs: list_agent_runs, get_agent_run, get_agent_run_messages, get_agent_run_tool_calls
var Module = fx.Module("mcp",
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Provide(NewSSEHandler),
	fx.Provide(NewStreamableHTTPHandler),
	fx.Invoke(RegisterRoutes),
)
