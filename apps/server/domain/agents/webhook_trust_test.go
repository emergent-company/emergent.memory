package agents

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestWebhookTrigger_RunIsUntrusted_CannotReachInternal is the fail-first
// regression for the webhook trust misclassification. The public webhook
// receiver (POST /api/webhooks/agents/:hookId) is an external-facing surface
// authenticated only by a per-hook bearer token, so the run it starts must be
// untrusted (trusted_internal=false) and therefore unable to list or spawn an
// internal-visible agent. A prior copy-paste set TrustedInternal=true here (the
// same "session UI is a trusted surface" comment), which let a holder of a
// shared/leaked webhook secret reach internal agents — the exact boundary #954
// fixed. This fails if ReceiveWebhook marks the run trusted.
func TestWebhookTrigger_RunIsUntrusted_CannotReachInternal(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_webhook_trust")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)

	// An internal-visible agent (must be unreachable from the webhook surface)
	// and a project-visible agent (must remain reachable) for catalog contrast.
	insertAgentDefinition(t, tdb.DB, ctx, projectID, "internal-child", string(VisibilityInternal))
	insertAgentDefinition(t, tdb.DB, ctx, projectID, "project-child", string(VisibilityProject))
	insertRuntimeAgent(t, tdb.DB, ctx, projectID, "internal-child")
	insertRuntimeAgent(t, tdb.DB, ctx, projectID, "project-child")

	// The webhook-bound agent.
	boundAgentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "webhook-bound")

	// A webhook hook bound to that agent with a known bearer token.
	const token = "whk_trusttest_token"
	hookID := insertWebhookHook(t, tdb.DB, ctx, boundAgentID, projectID, token)

	repo := NewRepository(tdb.DB)
	ae := trustTestExecutor(repo)
	h := &Handler{repo: repo, executor: ae}

	// Drive the real public webhook receiver.
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/agents/"+hookID,
		strings.NewReader(`{"prompt":"list agents"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/webhooks/agents/:hookId")
	c.SetParamNames("hookId")
	c.SetParamValues(hookID)

	err := h.ReceiveWebhook(c)
	require.NoError(t, err)

	// The run row the webhook started must be persisted untrusted. This is the
	// observable boolean that gates internal-agent reachability downstream.
	trusted := runTrustedByTriggerSource(t, tdb.DB, ctx, "webhook:"+hookID)
	require.False(t, trusted,
		"webhook-triggered run must be untrusted (trusted_internal=false)")

	// Observable catalog reachability: with the run's observed trust value, the
	// coordination catalog must exclude the internal agent and keep the project one.
	defs, err := repo.FindAllDefinitions(ctx, projectID, true)
	require.NoError(t, err)
	catalog := buildAgentCatalog(defs, nil, trusted)
	inCatalog := map[string]bool{}
	for _, a := range catalog {
		inCatalog[a.Name] = true
	}
	require.False(t, inCatalog["internal-child"],
		"webhook run catalog must exclude the internal-visible agent")
	require.True(t, inCatalog["project-child"],
		"webhook run catalog must still include the project-visible agent")

	// Observable spawn refusal: an internal target must be blocked for the
	// webhook run's trust value.
	internalDef := findDefByName(t, defs, "internal-child")
	reason, blocked := spawnTargetBlocked(trusted, internalDef)
	require.True(t, blocked,
		"spawnTargetBlocked must refuse an internal target for an untrusted caller")
	require.Contains(t, reason, "internal")
}

// insertWebhookHook inserts an enabled webhook hook bound to agentID, hashing
// the given plaintext token so the receiver's VerifyWebhookToken accepts it.
func insertWebhookHook(t *testing.T, db *bun.DB, ctx context.Context, agentID, projectID, token string) string {
	t.Helper()
	hash, err := HashWebhookToken(token)
	require.NoError(t, err)
	id := uuid.NewString()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.agent_webhook_hooks (id, agent_id, project_id, label, token_hash, enabled) VALUES (?, ?, ?, ?, ?, true)`,
		id, agentID, projectID, "test-hook", hash)
	require.NoError(t, err)
	return id
}

// runTrustedByTriggerSource reads the persisted trust marker for the most recent
// run with the given trigger source.
func runTrustedByTriggerSource(t *testing.T, db *bun.DB, ctx context.Context, triggerSource string) bool {
	t.Helper()
	var trusted bool
	err := db.NewRaw(
		`SELECT trusted_internal FROM kb.agent_runs WHERE trigger_source = ? ORDER BY created_at DESC LIMIT 1`,
		triggerSource).Scan(ctx, &trusted)
	require.NoError(t, err)
	return trusted
}

func findDefByName(t *testing.T, defs []*AgentDefinition, name string) *AgentDefinition {
	t.Helper()
	for _, d := range defs {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("definition %q not found", name)
	return nil
}
