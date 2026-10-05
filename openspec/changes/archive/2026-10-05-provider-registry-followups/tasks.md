## 1. Graded reasoning effort end-to-end (#1462)

- [x] 1.1 Add `ExecuteRequest.ReasoningEffort` in `domain/agents/executor.go` and apply it to the resolved model via the `adk.ReasoningEffortConfigurator` interface in `runPipeline`
- [x] 1.2 Delegate `SetReasoningEffort` from `provider.TrackingModel` to its inner model (mirroring `SetEnableThinking`)
- [x] 1.3 Thread `ChatCompletionRequest.ReasoningEffort` into both the execute and resume `ExecuteRequest`s in `domain/agentcompat/service.go`
- [x] 1.4 Unit-test that `TrackingModel` delegates `SetReasoningEffort` and satisfies `adk.ReasoningEffortConfigurator`; mutation-check by emptying the delegation

## 2. Replay reasoning items across Responses tool loops (#1463)

- [x] 2.1 Capture the reasoning item `id` from Responses output and carry it (with summary) on the following `function_call` part's `PartMetadata`; unit-test the carry
- [x] 2.2 Replay the carried reasoning item as `{"type":"reasoning","id":...,"summary":[...]}` immediately before its `function_call` in `buildResponsesInput`
- [x] 2.3 Mutation-check: a two-step tool loop asserts the reasoning item directly precedes the replayed `function_call`
- [x] 2.4 Capture the reasoning item `encrypted_content` onto the following `function_call` part's `PartMetadata` and request it via the request `include` array, so stateless/ZDR (`store:false`) replay works
- [x] 2.5 Replay `encrypted_content` in preference to the id (omitting the id when the blob is present) and unit-test the ZDR path (blob present with id, and blob present without id); mutation-check that reverting to id-only replay fails the ZDR test

## 3. Model limits sync applies the models.dev feed (#1464)

- [x] 3.1 Drop the obsolete top-level `type` filter in `parseModelLimits` (key off a positive `limit.output`) and remove the dead `Type` field
- [x] 3.2 Raise the response body cap to 64 MB via a shared constant; make the fetch URL injectable for tests
- [x] 3.3 Unit-test type-less parsing, zero-output skipping, and a >4 MB body parse; mutation-check the old 4 MB cap and the old type filter

## 4. Verification

- [x] 4.1 `go build ./...`
- [x] 4.2 `go test -count=1 ./pkg/adk/... ./domain/provider/...`
- [x] 4.3 `bash apps/server/scripts/lint-ratchet.sh`
- [x] 4.4 `bash scripts/preflight/migration-number.sh`
- [x] 4.5 `openspec validate provider-registry-followups --strict`

## 5. Not done in this change

- 5.1 Live call to `api.openai.com/v1/responses` to observe reasoning-item replay and `max` effort on a real reasoning model (no network/model access in this environment) — tracked as unverified in the PR

<!-- openspec:archive-ready -->
