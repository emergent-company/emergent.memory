## 1. Schema primitives

- [x] 1.1 Add `AdditionalProperties *bool \`json:"additionalProperties,omitempty"\`` to `InputSchema` in `apps/server/domain/mcp/entity.go`
- [x] 1.2 Add `objectOutputSchema() *InputSchema` (permissive root object, `additionalProperties: true`) in `apps/server/domain/mcp/envelope.go`
- [x] 1.3 Add `typedObjectOutputSchema(props map[string]PropertySchema, required []string) *InputSchema` in `apps/server/domain/mcp/envelope.go`
- [x] 1.4 Add package-level `proseOnlyTools []string` allowlist for raw text/markdown results

## 2. structuredContent emission

- [x] 2.1 Export `StructuredContentFromJSON(jsonBytes []byte) map[string]any` in `apps/server/domain/mcp/response_contract.go` (object → itself; array → `{"results": [...]}`; primitive → `{"value": ...}`; nil/invalid → nil)
- [x] 2.2 Use it in `wrapResult` and `wrapResultCompact`
- [x] 2.3 Reroute raw `json.Marshal`-built results (project-create, schema-migrate-*, classify-document, list-installed-schemas, finalize-discovery, queue-reextraction, session-todo-update) through `wrapResultCompact`
- [x] 2.4 `session-todo-list`: keep the array text block, set `StructuredContent = {"todos": [...]}`, declare the typed schema
- [x] 2.5 `queue-reextraction`: declare `typedObjectOutputSchema({"job_id": string}, ["job_id"])`

## 3. Declared outputSchema coverage

- [x] 3.1 Declare `objectOutputSchema()` on every non-envelope static tool in `service.go` and all `*_tools.go`
- [x] 3.2 Keep `envelopeOutputSchema()` on the envelope tools
- [x] 3.3 Leave `OutputSchema` nil for the prose-only allowlist (`project-get`, `schema-assign`, `schema-assignment-update`, `schema-uninstall`, `schema-create`, `schema-migration-preview`, `migration-archive-list`, `migration-archive-get`, `project-briefing`, `call_agent`)
- [x] 3.4 Set `StructuredContent` in the blueprints domain tool-result helper (`domain/blueprints/mcp_tools.go`)
- [x] 3.5 Set `StructuredContent` in the agents domain tool-result helper (`domain/agents/mcp_tools.go`)

## 4. Tests

- [x] 4.1 Rewrite `TestToolsListDeclaresOutputSchema` to cover ALL tools: every non-allowlist tool has a non-nil object-root `OutputSchema`
- [x] 4.2 Add `StructuredContentFromJSON` mapping test (object/array/primitive/nil)
- [x] 4.3 Add serialization test: `objectOutputSchema()` emits `additionalProperties: true`; input schemas do NOT emit it
- [x] 4.4 Add session-todo-list test (text array + `{"todos": [...]}` structuredContent)
- [x] 4.5 Keep `TestEnvelopeResultSetsStructuredContent` and `TestAgentEndpointToolDefinitionsDeclareOutputSchema` green

## 5. Verification

- [x] 5.1 `go build ./...` from `apps/server`
- [x] 5.2 `go test ./domain/mcp/... ./domain/agents/...` (pass)
- [x] 5.3 `go test ./domain/blueprints/...` — pre-existing unrelated failure `TestApply_AgentMaterialization_NonDestructiveUpdate` confirmed at baseline
- [x] 5.4 `openspec validate mcp-output-schema-coverage --strict`
- [ ] 5.5 `task lint` (server)
