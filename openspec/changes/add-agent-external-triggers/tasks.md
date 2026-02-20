## 1. Database and Entities

- [x] 1.1 Create migration script `00028_create_agent_webhook_hooks.sql` to add `kb.agent_webhook_hooks` table, add `trigger_source` and `trigger_metadata` fields to `kb.agent_runs`, and update `AgentTriggerType` enum.
- [x] 1.2 Update `AgentRun` struct in `domain/agents/entity.go` to include `TriggerSource` and `TriggerMetadata` (JSONB).
- [x] 1.3 Add `"webhook"` value to `AgentTriggerType` enum in `domain/agents/entity.go`.
- [x] 1.4 Define `AgentWebhookHook` and `RateLimitConfig` structs in `domain/agents/entity.go`.

## 2. Repository and Core Logic

- [x] 2.1 Add CRUD operations for `AgentWebhookHook` in `domain/agents/repository.go` (create, list, delete, find_by_id).
- [x] 2.2 Create token utility functions for generating secure random bearer tokens and hashing them using bcrypt.
- [x] 2.3 Implement in-memory rate limiting mechanism (e.g., token bucket) for webhook hooks in the handler layer.

## 3. Webhook Hook Admin API

- [x] 3.1 Implement `CreateWebhookHook` handler in `domain/agents/handler.go` (generates token, saves hash, returns plaintext token once).
- [x] 3.2 Implement `ListWebhookHooks` handler in `domain/agents/handler.go`.
- [x] 3.3 Implement `DeleteWebhookHook` handler in `domain/agents/handler.go`.
- [x] 3.4 Register admin webhook hook routes in `domain/agents/routes.go` under `/api/admin/agents/:id/hooks`.

## 4. Public Webhook Receiver

- [x] 4.1 Implement `ReceiveWebhook` handler in `domain/agents/handler.go` to process incoming trigger requests.
- [x] 4.2 Add Bearer token extraction and verification logic to the receiver handler.
- [x] 4.3 Add payload parsing logic to extract optional `prompt` and `context` from the incoming JSON body.
- [x] 4.4 Update the `executor.Execute()` call within the receiver to queue the agent run, passing along `TriggerSource` and `TriggerMetadata`.
- [x] 4.5 Register public webhook routes in `domain/agents/routes.go` under a new group `/api/webhooks/agents` (without admin auth).

## 5. Event Bridge Integration (Reaction Triggers)

- [x] 5.1 Modify `domain/agents/module.go` to inject `events.Service` into the fx lifecycle.
- [x] 5.2 Add an `OnStart` fx hook to globally subscribe the `TriggerService` to the event bus.
- [x] 5.3 Implement loop prevention logic within the event subscription callback (ignore events where `ActorType == "agent"`).

## 6. Frontend Admin UI

- [x] 6.1 Update the Agent form dropdowns to include the `webhook` trigger type option.
- [x] 6.2 Add API methods to frontend API client/hooks to interact with `/api/admin/agents/:id/hooks`.
- [x] 6.3 Create a `WebhookHooksList` component on the Agent Detail page to display configured hooks.
- [x] 6.4 Implement hook creation modal and token reveal (with copy-to-clipboard functionality).
- [x] 6.5 Implement hook deletion flow with a confirmation prompt.

## 7. Testing and Verification

- [x] 7.1 Write Go E2E tests for the webhook hook CRUD endpoints and token visibility logic.
- [x] 7.2 Write Go E2E tests for the public webhook receiver endpoint (valid token, invalid token, rate limiting).
- [x] 7.3 Write integration tests for the Event Bridge to ensure reaction agents trigger appropriately without loops.
