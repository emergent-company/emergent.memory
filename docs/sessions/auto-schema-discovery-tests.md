# Auto Schema Discovery — Test Suite Session Notes

## What We Built

End-to-end test suite for the domain-aware extraction pipeline:
schema discovery, HITL confirmation, re-extraction, and heuristic matching.

### Two test scripts

| Script | Purpose |
|--------|---------|
| `bench/ask-test/test_ask_user.py` | Single HITL test — forces `ask_user`, responds, verifies resume+complete |
| `bench/domain-test/run_domain_test.py` | Full 6-doc domain pipeline test |

### Blueprint agent

`blueprints/test-agents/agents/remember-test.yaml`

Agent name: `remember-test`  
Model: `openai-compatible/deepseek-v4-flash`  
`enableThinking: false` (required — DeepSeek errors on 2nd call if reasoning_content stripped)

**Tools wired:**
- `ask_user` — HITL pause
- `classify-document` — classify doc against installed schemas
- `list-installed-schemas` — list project schemas
- `finalize-discovery` — create new schema pack
- `queue-reextraction` — queue re-extraction after schema created
- `memory_search`, `graph_query`, `graph_write`

---

## Domain Test (`run_domain_test.py`)

### Test documents (in order)

| # | File | Expected stage | Expected pack | Notes |
|---|------|---------------|--------------|-------|
| 1 | `ai-chat-1.txt` | `new_domain` | AI Chat | Creates pack, asks user, creates schema |
| 2 | `personal-notes.txt` | `new_domain` | Personal Notes | Same |
| 3 | `medical-lab-1.txt` | `new_domain` | Medical Records | Same |
| 4 | `supplier-agreement.txt` | `new_domain` | Supplier Agreements | Same |
| 5 | `ai-chat-2.txt` | `heuristic` | AI Chat | Should match existing pack ≥0.7 confidence |
| 6 | `medical-lab-2.txt` | `heuristic` | Medical Records | Same |

Docs 1-4 trigger `finalize-discovery` → `ask_user` pause → auto-respond "Create new pack" → `queue-reextraction`.  
Docs 5-6 should NOT trigger discovery — heuristic classifier matches existing schema.

### What each assertion checks

**new_domain docs:**
- `domain_label == new_domain` (pre-agent snapshot — before agent installs schema)
- `matched_schema_id == null` (same snapshot)
- schema with expected pack name created in project
- schema has non-empty `extractionPrompts.domainContext` and `typeHints`
- reextraction job queued for doc

**heuristic docs:**
- `domain_label` set and not `new_domain`
- `domain_confidence >= 0.7`
- `matched_schema_id` set
- no new discovery job fired

### How HITL pause/resume works (key flow)

1. Agent calls `ask_user` → run status → `paused`
2. Test calls `GET /api/projects/{pid}/agent-questions?run_id=...&status=pending`
3. Test calls `POST /api/projects/{pid}/agent-questions/{qid}/respond` with chosen option label
4. Response body: `{"data": {"resumeRunId": "<new-run-id>"}}` — poll this new ID
5. New run completes with status `completed`

### Known assertion gaps

- `assert_entities_typed` is **always SKIP** — no doc-scoped graph filter available yet
- `assert_no_discovery_fired` may SKIP if endpoint doesn't support `triggered_by_run` filter

---

## HITL Test (`test_ask_user.py`)

Simpler single-agent test:
1. Sends message: "Ask me a test question using ask_user with options Yes and No"
2. Waits for `paused`
3. Finds pending question, responds "Yes"
4. Polls `resumeRunId` → expects `success`/`completed`

---

## How to Run

### Prerequisites

```bash
export EMERGENT_MEMORY_TOKEN=emt_90e466b66031ef242148336a85152d30f78ba3e723fb81dc7ebed0fefc9156de
export MEMORY_ORG_TOKEN=$EMERGENT_MEMORY_TOKEN
export MEMORY_ORG_ID=256508f5-6cbf-46bb-8c29-d8f839dd4ba8
export LITELLM_KEY=<litellm-key>
export LITELLM_BASE_URL=http://litellm:4000/v1   # internal docker network
# Or use Google AI:
export GOOGLE_API_KEY=<key>
```

### Run full domain test (creates fresh project)

```bash
cd /root/emergent.memory
python3 bench/domain-test/run_domain_test.py
```

### Run single doc (e.g. doc 1 only)

```bash
python3 bench/domain-test/run_domain_test.py --doc 1
```

### Reuse existing project

```bash
python3 bench/domain-test/run_domain_test.py --project-id <uuid>
```

### Cleanup after test

```bash
python3 bench/domain-test/run_domain_test.py --cleanup
```

### HITL-only test

```bash
python3 bench/ask-test/test_ask_user.py
```

---

## Server / Infrastructure

- Prod server: `https://memory.emergent-company.ai`
- Provider used in domain test: LiteLLM proxy → `deepseek-v4-flash`
  - configured via `PUT /api/projects/{pid}/providers/openai-compatible`
  - `baseUrl: http://litellm:4000/v1`, `generativeModel: deepseek-v4-flash`
- Blueprint install: `~/.memory/bin/memory blueprints install blueprints/test-agents --project <pid> --server <url>`
  - uses `MEMORY_PROJECT_TOKEN` (not `EMERGENT_MEMORY_TOKEN`)

---

## Key Bugs Fixed in This Session

| Bug | Fix |
|-----|-----|
| DeepSeek "reasoning_content must be passed back" error on 2nd LLM call | `enableThinking: false` in blueprint YAML → propagates through applier → SDK → executor → `openai_model.go` |
| `enableThinking` not propagating to agent | Added `EnableThinking *bool` to `AgentModel` (CLI types), `ModelConfig` (SDK), applier create+update paths |
| `suspend_context` not persisted | Migration `00112_add_agent_run_suspend_context.sql` adds `suspend_context jsonb` to `kb.agent_runs` |
| `resume_run_id` not returned in respond response | Stored in `suspend_context["resume_run_id"]`; `RunToACPObject` reads it; `AgentQuestionDTO.ResumeRunID` added |
| Agent paused but test polls original run ID forever | Test switches poll target to `resume_run_id` after respond |
| `document_excerpt` missing from `classify-document` response | Added to MCP tool response; agent uses it for FORMAT-based pack naming |
| Agent naming packs by topic (e.g. "Task Requests") instead of format | Strengthened system prompt with explicit FORMAT rules |
| `extractionPrompts` missing from installed schemas API | Added to `GET /api/schemas/projects/{pid}/installed` response |

---

## What's Left / Next Steps

1. **Verify v0.41.46 deployed to prod** — CI workflow `Publish Self-Hosted Images` was in progress at session end
2. **Run `test_ask_user.py` on prod** — confirm HITL e2e with `enableThinking: false` works end-to-end
3. **Run full `run_domain_test.py`** — 6-doc test not yet run against current prod build
4. **Entity type assertion** — `assert_entities_typed` is always SKIP; needs doc-scoped graph filter
5. **`assert_no_discovery_fired`** — may need endpoint fix to support `triggered_by_run` query param

---

## Relevant Files

```
bench/ask-test/test_ask_user.py
bench/domain-test/run_domain_test.py
bench/domain-test/fixtures/
  ai-chat-1.txt  ai-chat-2.txt
  personal-notes.txt
  medical-lab-1.txt  medical-lab-2.txt
  supplier-agreement.txt
blueprints/test-agents/agents/remember-test.yaml
apps/server/migrations/00112_add_agent_run_suspend_context.sql
apps/server/domain/agents/suspend_signal.go
apps/server/domain/agents/entity.go
apps/server/domain/agents/executor.go
apps/server/domain/agents/handler.go
apps/server/domain/agents/dto.go
apps/server/domain/agents/acp_handler.go
apps/server/domain/agents/acp_dto.go
apps/server/pkg/adk/openai_model.go
apps/server/pkg/sdk/agentdefinitions/client.go
tools/cli/internal/blueprints/types.go
tools/cli/internal/blueprints/applier.go
```
