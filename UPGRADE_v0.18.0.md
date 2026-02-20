# Upgrade to v0.18.0: Agent Questions & Webhook Triggers

## Overview

Version 0.18.0 introduces two major features:

1. **Agent Questions** - Agents can pause and ask users for input via `ask_user` tool
2. **Webhook Triggers** - Trigger agents via external webhooks with rate limiting

## Prerequisites

- Database access (for migrations)
- SSH/deployment access to mcj-emergent server
- Admin UI access for testing

## Upgrade Steps

### 1. Pull Latest Code

On the mcj-emergent deployment server:

```bash
cd /path/to/emergent
git fetch origin
git checkout v0.18.0
```

### 2. Run Database Migrations

The following migrations will be applied:

- `00028_create_agent_webhook_hooks.sql` - Webhook hooks table
- `00029_create_agent_questions.sql` - Agent questions table

```bash
cd apps/server-go
goose -dir migrations postgres "$DATABASE_URL" up
```

**Expected output:**

```
OK   00028_create_agent_webhook_hooks.sql (XXms)
OK   00029_create_agent_questions.sql (XXms)
goose: successfully migrated database to version: 29
```

### 3. Verify Migration Success

```bash
# Check tables were created
psql "$DATABASE_URL" -c "\dt core.agent_questions"
psql "$DATABASE_URL" -c "\dt core.agent_webhook_hooks"

# Check indexes
psql "$DATABASE_URL" -c "\d core.agent_questions"
```

**Expected tables:**

- `core.agent_questions` - Stores questions asked by agents
- `core.agent_webhook_hooks` - Stores webhook trigger configurations

### 4. Restart Services

```bash
cd /root/emergent  # or wherever emergent is installed
pnpm run workspace:restart
```

**Wait for services to be healthy:**

```bash
pnpm run workspace:status
```

Expected output should show both `admin` and `server` as "running".

### 5. Verify OpenAPI Spec Updated

The MCP `api-client-mcp` should auto-discover new endpoints. Check swagger docs:

```bash
curl http://localhost:3002/swagger/index.html | grep -i "agent-questions"
```

Or visit: `https://api.dev.emergent-company.ai/swagger/index.html`

Look for these new endpoints under "agents" tag:

- `GET /api/projects/{projectId}/agent-questions`
- `GET /api/projects/{projectId}/agent-runs/{runId}/questions`
- `POST /api/projects/{projectId}/agent-questions/{questionId}/respond`

## Verification Tests

### Test 1: Agent Questions via UI

1. **Create a test agent** that uses the `ask_user` tool:

   ```python
   # Example agent code
   response = ask_user(
       question="What color should the theme be?",
       options=["blue", "green", "red"]
   )
   ```

2. **Trigger the agent** - it should pause with status `paused_for_input`

3. **Check notifications** in the admin UI:

   - Navigate to notifications bell icon
   - Should see "Agent Question" notification
   - Click to view question details

4. **Respond to question**:

   - Click "Respond" button in notification
   - Enter response
   - Verify agent run resumes automatically

5. **Check Q&A history**:
   - Navigate to agent detail page
   - Scroll to "Q&A History" section
   - Should show question and your response

### Test 2: Agent Questions via CLI

```bash
# List questions for a specific run
emergent-cli agents questions list <run-id>

# List all pending questions for a project
emergent-cli agents questions list-project --project-id <project-id> --status pending

# Respond to a question
emergent-cli agents questions respond <question-id> "my answer"
```

### Test 3: Agent Questions via MCP

Using the `api-client-mcp` tool:

```bash
# Discover agent endpoints
mcp-client call_api --action discover --tags agents

# List questions for a project
mcp-client call_api --action call --operationId listProjectQuestions \
  --projectId <project-id> --status pending

# Respond to a question
mcp-client call_api --action call --operationId respondToQuestion \
  --projectId <project-id> --questionId <question-id> \
  --body '{"response": "my answer"}'
```

### Test 4: Webhook Triggers

1. **Create a webhook hook** via UI or API
2. **Get the webhook URL and token**
3. **Trigger webhook**:
   ```bash
   curl -X POST https://api.dev.emergent-company.ai/webhooks/<token> \
     -H "Content-Type: application/json" \
     -d '{"input": "test data"}'
   ```
4. **Verify agent run started** in UI

## Rollback (If Needed)

If issues occur, rollback to v0.17.0:

```bash
# Stop services
pnpm run workspace:stop

# Rollback code
git checkout v0.17.0

# Rollback migrations
cd apps/server-go
goose -dir migrations postgres "$DATABASE_URL" down-to 27

# Restart
pnpm run workspace:restart
```

## New Features Summary

### Agent Questions (`ask_user` tool)

**Backend:**

- `BuildAskUserTool()` - Registers ask_user tool in toolpool
- Executor detects `AskPauseState` and pauses run with status "paused_for_input"
- 3 REST endpoints for listing/responding to questions
- Cross-domain notification integration

**Frontend:**

- `AgentQuestionNotification` - Modal for responding to questions
- `AgentRunQAHistory` - Shows question/answer history on agent detail page
- `useAgentRunQuestions` - React hook for fetching questions

**SDK & CLI:**

- Go SDK: `GetRunQuestions`, `ListProjectQuestions`, `RespondToQuestion`
- CLI: `emergent-cli agents questions [list|list-project|respond]`

**MCP:**

- `api-client-mcp` auto-discovers endpoints via OpenAPI spec

### Webhook Triggers

**Backend:**

- `agent_webhook_hooks` table with token management
- Rate limiting per webhook hook
- Token refresh capability

**Frontend:**

- `WebhookHooksList` component for managing webhooks

## Testing Coverage

- **18 E2E tests** (`apps/server-go/tests/e2e/agents_questions_test.go`)
- **11 unit tests** (`apps/server-go/domain/agents/ask_user_tool_test.go`)
- **11 SDK tests** (`apps/server-go/pkg/sdk/agents/client_test.go`)
- **5 CLI tests** (`tools/emergent-cli/internal/cmd/agents_questions_test.go`)
- **12 webhook E2E tests** (`apps/server-go/tests/e2e/agents_webhooks_test.go`)

**Total: 57 automated tests** ✅

## Support

If issues occur during upgrade:

- Check logs: `pnpm run workspace:status` and application logs
- Review database migration status: `goose -dir migrations postgres "$DATABASE_URL" status`
- Contact team with logs and error messages

## Related Documentation

- Agent Questions Design: `openspec/changes/agent-ask-tool/`
- Webhook Triggers Design: `openspec/changes/add-agent-external-triggers/`
- SDK Documentation: `apps/server-go/pkg/sdk/agents/README.md`
- CLI Documentation: `tools/emergent-cli/README.md`
