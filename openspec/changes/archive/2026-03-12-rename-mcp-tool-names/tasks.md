## 1. Rename tools in mcp/service.go

- [x] 1.1 Rename `get_project_info` → `project-get` (Name field + case)
- [x] 1.2 Rename `create_project` → `project-create` (Name field + case)
- [x] 1.3 Rename `schema_version` → `schema-version` (Name field + case)
- [x] 1.4 Rename `list_entity_types` → `entity-type-list` (Name field + case)
- [x] 1.5 Rename `query_entities` → `entity-query` (Name field + case)
- [x] 1.6 Rename `search_entities` → `entity-search` (Name field + case)
- [x] 1.7 Rename `get_entity_edges` → `entity-edges-get` (Name field + case)
- [x] 1.8 Rename `list_schemas` → `schema-list` (Name field + case)
- [x] 1.9 Rename `get_schema` → `schema-get` (Name field + case)
- [x] 1.10 Rename `get_available_templates` → `template-list-available` (Name field + case)
- [x] 1.11 Rename `get_installed_templates` → `template-list-installed` (Name field + case)
- [x] 1.12 Rename `assign_schema` → `schema-assign` (Name field + case)
- [x] 1.13 Rename `update_template_assignment` → `schema-assignment-update` (Name field + case)
- [x] 1.14 Rename `uninstall_schema` → `schema-uninstall` (Name field + case)
- [x] 1.15 Rename `create_schema` → `schema-create` (Name field + case)
- [x] 1.16 Rename `delete_schema` → `schema-delete` (Name field + case)
- [x] 1.17 Rename `create_entity` → `entity-create` (Name field + case)
- [x] 1.18 Rename `create_relationship` → `relationship-create` (Name field + case)
- [x] 1.19 Rename `update_entity` → `entity-update` (Name field + case)
- [x] 1.20 Rename `delete_entity` → `entity-delete` (Name field + case)
- [x] 1.21 Rename `restore_entity` → `entity-restore` (Name field + case)
- [x] 1.22 Rename `hybrid_search` → `search-hybrid` (Name field + case)
- [x] 1.23 Rename `semantic_search` → `search-semantic` (Name field + case)
- [x] 1.24 Rename `find_similar` → `search-similar` (Name field + case)
- [x] 1.25 Rename `traverse_graph` → `graph-traverse` (Name field + case)
- [x] 1.26 Rename `list_relationships` → `relationship-list` (Name field + case)
- [x] 1.27 Rename `update_relationship` → `relationship-update` (Name field + case)
- [x] 1.28 Rename `delete_relationship` → `relationship-delete` (Name field + case)
- [x] 1.29 Rename `list_tags` → `tag-list` (Name field + case)
- [x] 1.30 Rename `preview_schema_migration` → `schema-migration-preview` (Name field + case)
- [x] 1.31 Rename `list_migration_archives` → `migration-archive-list` (Name field + case)
- [x] 1.32 Rename `get_migration_archive` → `migration-archive-get` (Name field + case)

## 2. Rename tools in mcp/agent_ext_tools.go

- [x] 2.1 Rename `list_agent_questions` → `agent-question-list` (Name field)
- [x] 2.2 Rename `list_project_agent_questions` → `agent-question-list-project` (Name field)
- [x] 2.3 Rename `respond_to_agent_question` → `agent-question-respond` (Name field)
- [x] 2.4 Rename `list_agent_hooks` → `agent-hook-list` (Name field)
- [x] 2.5 Rename `create_agent_hook` → `agent-hook-create` (Name field)
- [x] 2.6 Rename `delete_agent_hook` → `agent-hook-delete` (Name field)
- [x] 2.7 Rename `list_adk_sessions` → `adk-session-list` (Name field)
- [x] 2.8 Rename `get_adk_session` → `adk-session-get` (Name field)
- [x] 2.9 Update dispatch case strings in service.go for all agent ext tools

## 3. Rename tools in mcp/brave_search.go, reddit_search.go, webfetch.go, query_tools.go

- [x] 3.1 Rename `brave_web_search` → `web-search-brave` (Name field + case)
- [x] 3.2 Rename `reddit_search` → `web-search-reddit` (Name field + case)
- [x] 3.3 Rename `webfetch` → `web-fetch` (Name field + case)
- [x] 3.4 Rename `query_knowledge` → `search-knowledge` (Name field + case)

## 4. Rename tools in mcp/documents_tools.go

- [x] 4.1 Rename `list_documents` → `document-list` (Name field + case)
- [x] 4.2 Rename `get_document` → `document-get` (Name field + case)
- [x] 4.3 Rename `upload_document` → `document-upload` (Name field + case)
- [x] 4.4 Rename `delete_document` → `document-delete` (Name field + case)

## 5. Rename tools in mcp/embeddings_tools.go

- [x] 5.1 Rename `get_embedding_status` → `embedding-status` (Name field + case)
- [x] 5.2 Rename `pause_embeddings` → `embedding-pause` (Name field + case)
- [x] 5.3 Rename `resume_embeddings` → `embedding-resume` (Name field + case)
- [x] 5.4 Rename `update_embedding_config` → `embedding-config-update` (Name field + case)

## 6. Rename tools in mcp/provider_tools.go

- [x] 6.1 Rename `list_org_providers` → `provider-list-org` (Name field + case)
- [x] 6.2 Rename `configure_org_provider` → `provider-configure-org` (Name field + case)
- [x] 6.3 Rename `configure_project_provider` → `provider-configure-project` (Name field + case)
- [x] 6.4 Rename `list_provider_models` → `provider-models-list` (Name field + case)
- [x] 6.5 Rename `test_provider` → `provider-test` (Name field + case)
- [x] 6.6 Rename `get_provider_usage` → `provider-usage-get` (Name field + case)

## 7. Rename tools in mcp/skills_tools.go

- [x] 7.1 Rename `list_skills` → `skill-list` (Name field + case)
- [x] 7.2 Rename `get_skill` → `skill-get` (Name field + case)
- [x] 7.3 Rename `create_skill` → `skill-create` (Name field + case)
- [x] 7.4 Rename `update_skill` → `skill-update` (Name field + case)
- [x] 7.5 Rename `delete_skill` → `skill-delete` (Name field + case)

## 8. Rename tools in mcp/token_tools.go

- [x] 8.1 Rename `list_project_api_tokens` → `token-list` (Name field + case)
- [x] 8.2 Rename `create_project_api_token` → `token-create` (Name field + case)
- [x] 8.3 Rename `get_project_api_token` → `token-get` (Name field + case)
- [x] 8.4 Rename `revoke_project_api_token` → `token-revoke` (Name field + case)

## 9. Rename tools in mcp/trace_tools.go

- [x] 9.1 Rename `list_traces` → `trace-list` (Name field + case)
- [x] 9.2 Rename `get_trace` → `trace-get` (Name field + case)

## 10. Rename tools in agents/mcp_tools.go

- [x] 10.1 Rename `list_agent_definitions` → `agent-def-list` (Name field + case)
- [x] 10.2 Rename `get_agent_definition` → `agent-def-get` (Name field + case)
- [x] 10.3 Rename `create_agent_definition` → `agent-def-create` (Name field + case)
- [x] 10.4 Rename `delete_agent_definition` → `agent-def-delete` (Name field + case)
- [x] 10.5 Rename `list_agents` → `agent-list` (Name field + case)
- [x] 10.6 Rename `get_agent` → `agent-get` (Name field + case)
- [x] 10.7 Rename `create_agent` → `agent-create` (Name field + case)
- [x] 10.8 Rename `delete_agent` → `agent-delete` (Name field + case)
- [x] 10.9 Rename `list_agent_runs` → `agent-run-list` (Name field + case)
- [x] 10.10 Rename `get_agent_run` → `agent-run-get` (Name field + case)
- [x] 10.11 Rename `get_agent_run_messages` → `agent-run-messages` (Name field + case)
- [x] 10.12 Rename `get_agent_run_tool_calls` → `agent-run-tool-calls` (Name field + case)
- [x] 10.13 Rename `get_run_status` → `agent-run-status` (Name field + case)
- [x] 10.14 Rename `list_available_agents` → `agent-list-available` (Name field + case)

## 11. Rename tools in mcpregistry/mcp_tools.go

- [x] 11.1 Rename `list_mcp_servers` → `mcp-server-list` (Name field + case)
- [x] 11.2 Rename `get_mcp_server` → `mcp-server-get` (Name field + case)
- [x] 11.3 Rename `create_mcp_server` → `mcp-server-create` (Name field + case)
- [x] 11.4 Rename `delete_mcp_server` → `mcp-server-delete` (Name field + case)
- [x] 11.5 Rename `inspect_mcp_server` → `mcp-server-inspect` (Name field + case)
- [x] 11.6 Rename `get_mcp_registry_server` → `mcp-registry-get` (Name field + case)
- [x] 11.7 Rename `install_mcp_from_registry` → `mcp-registry-install` (Name field + case)

## 12. Verify and test

- [x] 12.1 Run `task build` — confirm zero compilation errors
- [x] 12.2 Run `task test` — confirm all unit tests pass
- [x] 12.3 Confirm hot-reload picks up changes (check logs/server/server.log for fresh startup)
- [x] 12.4 Spot-check: call `entity-list` (formerly `list_entities`) via MCP and verify correct response
- [x] 12.5 Confirm old names (e.g., `list_agents`, `assign_schema`) return tool-not-found errors
- [x] 12.6 Update any e2e test fixtures or assertions that reference old tool names
