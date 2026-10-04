## 1. Protocol + data-driven selection

- [x] 1.1 Add `ProtocolOpenAIResponses` (`"openai-responses"`) to `domain/provider/protocol.go`; verify `go build ./...`
- [x] 1.2 Add `ProviderDefinition.ResponsesHosts` and `ProviderDefinition.ProtocolForBaseURL(baseURL)` (promote `openai-chat` → `openai-responses` only on a declared host); unit test first-party vs relay vs lookalike/nil in `protocol_select_test.go`
- [x] 1.3 Declare `ResponsesHosts` on the OpenAI builtin (shared list owned by `pkg/adk`) and resolve the protocol after the base URL in `CredentialService.applyDefinition`; unit test the resolved-credential selection
- [x] 1.4 Mutation-test the selection logic (force always-chat, always-responses, and naive substring host match) and confirm the tests fail each time

## 2. Responses adapter

- [x] 2.1 Implement `pkg/adk/openai_responses_model.go`: `POST {base}/responses`, `instructions`, `input` items (message/function_call/function_call_output), flat function tools, `reasoning.effort`, and parse `output` items + usage; unit test the request body shape and response parsing via `httptest`
- [x] 2.2 Send `tools` and a graded `reasoning.effort` in the same request; unit test `TestOpenAIResponsesModel_ToolsAndReasoningEffort`
- [x] 2.3 Preserve Responses call/output pairing by injecting synthetic `function_call_output` for orphaned calls; unit test `TestOpenAIResponsesModel_OrphanToolCallPairing`

## 3. Dispatch + verification

- [x] 3.1 Dispatch `openai-responses` in `pkg/adk/model.go` `CreateModelWithName` (DB credential path and `OPENAI_BASE_URL` env-var path, host-aware) and in `domain/provider/catalog.go` generate/embed paths; verify auth uses the existing `ApplyAuth` styles
- [x] 3.2 Run `go build ./...`, `go test -count=1 ./pkg/adk/... ./domain/provider/...`, and `bash apps/server/scripts/lint-ratchet.sh`
- [x] 3.3 `openspec validate add-openai-responses-protocol --strict`

## 4. Not done in this change

- 4.1 Live call to `api.openai.com/v1/responses` (no network access in this environment) — tracked as unverified in the PR

<!-- openspec:archive-ready -->
