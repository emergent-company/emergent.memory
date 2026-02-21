# v0.18.0 Deployment Verification - mcj-emergent

**Date**: 2026-02-20  
**Server**: mcj-emergent  
**Deployed Version**: v0.18.0  
**Deployment Status**: ✅ **SUCCESS**

---

## Deployment Summary

Successfully deployed v0.18.0 to mcj-emergent server with Agent Questions and Webhook Triggers features.

### Steps Completed

1. ✅ **Version Bump & Release**

   - Updated `VERSION` file from 0.17.0 to 0.18.0
   - Updated OpenAPI annotation in `main.go`
   - Created git tag `v0.18.0` and pushed to GitHub
   - Created GitHub release: https://github.com/emergent-company/emergent/releases/tag/v0.18.0

2. ✅ **CI Build**

   - Docker image build workflow completed successfully in 5m29s
   - Image pushed to `ghcr.io/emergent-company/emergent-server-with-cli:0.18.0`

3. ✅ **Server Upgrade**

   - SSH connected to mcj-emergent
   - Updated `docker-compose.yml` to use version 0.18.0
   - Pulled new Docker image
   - Ran database migrations via temporary container
   - Restarted emergent-server service

4. ✅ **Database Migrations**

   - Migration 27: `create_workspace_images.sql` - OK (2.65s)
   - Migration 28: `create_agent_webhook_hooks.sql` - OK (1.91s)
   - Migration 29: `create_agent_questions.sql` - OK (1.18s)
   - **Database version: 29** ✅

5. ✅ **Health Check**

   - Server status: `healthy`
   - Database: `healthy`
   - Uptime: Running without errors
   - Health endpoint: `http://localhost:3002/health`

6. ✅ **Table Verification**
   ```sql
   kb.agent_questions           -- ✅ Created
   kb.agent_webhook_hooks       -- ✅ Created
   ```

---

## Feature Verification Status

### Agent Questions Feature

**Backend:**

- ✅ Database table `kb.agent_questions` created
- ✅ Migrations applied successfully
- ⏳ **Pending verification**: Create test agent with `ask_user` tool
- ⏳ **Pending verification**: Trigger agent and verify pause state
- ⏳ **Pending verification**: Respond to question via API/UI

**API Endpoints** (should be available):

```
GET  /api/projects/{projectId}/agent-questions
GET  /api/projects/{projectId}/agent-runs/{runId}/questions
POST /api/projects/{projectId}/agent-questions/{questionId}/respond
```

**Frontend:**

- ⏳ **Pending verification**: AgentQuestionNotification component
- ⏳ **Pending verification**: Q&A history on agent detail page
- ⏳ **Pending verification**: Notification bell shows agent questions

**SDK & CLI:**

- ⏳ **Pending verification**: Go SDK methods (`GetRunQuestions`, etc.)
- ⏳ **Pending verification**: CLI commands (`emergent-cli agents questions list`)

**MCP Integration:**

- ⏳ **Pending verification**: `api-client-mcp` discovers agent questions endpoints

### Webhook Triggers Feature

**Backend:**

- ✅ Database table `kb.agent_webhook_hooks` created
- ✅ Migrations applied successfully
- ⏳ **Pending verification**: Create webhook hook
- ⏳ **Pending verification**: Trigger webhook and verify agent runs

---

## Next Steps

### 1. Manual Testing Required

The following manual tests should be performed to fully verify the deployment:

#### Test 1: Agent Questions via UI

1. Navigate to mcj-emergent admin UI
2. Create a test agent that uses `ask_user` tool
3. Trigger the agent
4. Verify agent pauses with status "paused_for_input"
5. Check that notification appears in notification bell
6. Click notification and respond to question
7. Verify agent run resumes automatically
8. Check Q&A history on agent detail page

#### Test 2: Agent Questions via CLI

**Prerequisites**: emergent-cli must be installed on server or locally with remote access

```bash
# List questions for a run
emergent-cli agents questions list <run-id>

# List pending questions for project
emergent-cli agents questions list-project --project-id <id> --status pending

# Respond to a question
emergent-cli agents questions respond <question-id> "my answer"
```

#### Test 3: Agent Questions via MCP

Using the `api-client-mcp` tool:

```bash
# Discover endpoints
mcp-client call_api --action discover --tags agents

# List questions
mcp-client call_api --action call --operationId listProjectQuestions \
  --projectId <id>

# Respond to question
mcp-client call_api --action call --operationId respondToQuestion \
  --projectId <id> --questionId <qid> --body '{"response": "answer"}'
```

#### Test 4: Webhook Triggers

1. Create a webhook hook via UI or API
2. Copy webhook URL and token
3. Trigger webhook with curl:
   ```bash
   curl -X POST http://mcj-emergent:3002/webhooks/<token> \
     -H "Content-Type: application/json" \
     -d '{"input": "test"}'
   ```
4. Verify agent run started in UI

### 2. Rollback Plan (If Needed)

If critical issues are discovered:

```bash
# SSH to mcj-emergent
ssh root@mcj-emergent

# Stop services
cd ~/.emergent/docker
docker compose down

# Update docker-compose.yml to previous version
sed -i 's/0.18.0/0.17.0/' docker-compose.yml

# Rollback database migrations
docker run --rm --network docker_emergent \
  -e DATABASE_URL='postgres://emergent:PASSWORD@emergent-db:5432/emergent?sslmode=disable' \
  ghcr.io/emergent-company/emergent-server-with-cli:0.17.0 \
  goose -dir /app/migrations postgres "$DATABASE_URL" down-to 27

# Restart with old version
docker compose up -d
```

---

## Technical Details

### Docker Configuration

**Image**: `ghcr.io/emergent-company/emergent-server-with-cli:0.18.0`

**Deployment location**: `~/.emergent/docker/`

**Network**: `docker_emergent` (bridge)

**Dependencies**:

- `emergent-db` (pgvector/pgvector:pg16) - Port 15432
- `emergent-kreuzberg` (goldziher/kreuzberg:latest) - Port 18000
- `emergent-minio` (minio/minio:latest) - Port 19000

### Database Connection

```
Host: emergent-db (Docker container)
Port: 5432 (internal), 15432 (host)
Database: emergent
User: emergent
Schemas: kb, core, public
```

### Migration Files

```
apps/server-go/migrations/00028_create_agent_webhook_hooks.sql
apps/server-go/migrations/00029_create_agent_questions.sql
```

### New Database Tables

```sql
-- Agent Questions
CREATE TABLE kb.agent_questions (
  id UUID PRIMARY KEY,
  run_id UUID NOT NULL REFERENCES kb.agent_runs(id),
  project_id UUID NOT NULL REFERENCES kb.projects(id),
  question TEXT NOT NULL,
  response TEXT,
  status VARCHAR(20) NOT NULL DEFAULT 'pending',
  options JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  answered_at TIMESTAMPTZ,
  CONSTRAINT fk_run FOREIGN KEY (run_id) REFERENCES kb.agent_runs(id) ON DELETE CASCADE
);

-- Indexes
CREATE INDEX idx_agent_questions_run_id ON kb.agent_questions(run_id);
CREATE INDEX idx_agent_questions_project_id ON kb.agent_questions(project_id);
CREATE INDEX idx_agent_questions_status ON kb.agent_questions(status);
CREATE INDEX idx_agent_questions_created_at ON kb.agent_questions(created_at DESC);

-- Agent Webhook Hooks
CREATE TABLE kb.agent_webhook_hooks (
  id UUID PRIMARY KEY,
  agent_id UUID NOT NULL REFERENCES kb.agents(id) ON DELETE CASCADE,
  token TEXT NOT NULL UNIQUE,
  rate_limit_per_minute INTEGER NOT NULL DEFAULT 60,
  enabled BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_agent_webhook_hooks_agent_id ON kb.agent_webhook_hooks(agent_id);
CREATE INDEX idx_agent_webhook_hooks_token ON kb.agent_webhook_hooks(token);
```

---

## Changelog

### Features Added in v0.18.0

#### Agent Questions (`ask_user` tool)

- Backend executor pauses agent runs when `AskPauseState` detected
- 3 REST API endpoints for listing and responding to questions
- Cross-domain notification integration
- Go SDK with 3 new methods
- CLI with 3 new commands under `agents questions`
- React components: `AgentQuestionNotification`, `AgentRunQAHistory`
- MCP auto-discovery via OpenAPI spec

#### Webhook Triggers

- External webhook triggers for agents
- Rate limiting per webhook hook
- Secure token management with refresh capability
- Frontend webhook management UI

### Test Coverage

- 18 E2E tests (agent questions)
- 11 unit tests (ask_user tool)
- 11 SDK tests
- 5 CLI tests
- 12 E2E tests (webhook triggers)
- **Total: 57 automated tests** ✅

---

## Support & Resources

- **Release Notes**: https://github.com/emergent-company/emergent/releases/tag/v0.18.0
- **Upgrade Guide**: `/root/emergent/UPGRADE_v0.18.0.md`
- **OpenSpec Documentation**:
  - `openspec/changes/agent-ask-tool/`
  - `openspec/changes/add-agent-external-triggers/`
- **Server Health**: `http://mcj-emergent:3002/health`
- **Swagger UI**: `http://mcj-emergent:3002/swagger/index.html`

---

## Deployment Completed

**Date**: 2026-02-20 15:47 UTC  
**Duration**: ~15 minutes (including migration time)  
**Status**: ✅ **Deployment Successful - Ready for Manual Verification**

The mcj-emergent server is now running v0.18.0 with all database migrations applied successfully. The agent questions and webhook triggers features are deployed and ready for manual testing.
