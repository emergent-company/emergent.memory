package extraction

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainagents "github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/mcp"
)

type fakeExtractionAgentStore struct {
	settings map[string]*domainagents.ProjectSetting
	defs     map[string]*domainagents.AgentDefinition
	runtimes map[string]*domainagents.Agent
}

func (f *fakeExtractionAgentStore) GetProjectSetting(_ context.Context, _, category, key string) (*domainagents.ProjectSetting, error) {
	return f.settings[category+"/"+key], nil
}

func (f *fakeExtractionAgentStore) FindDefinitionByID(_ context.Context, id string, _ *string) (*domainagents.AgentDefinition, error) {
	return f.defs[id], nil
}

func (f *fakeExtractionAgentStore) FindByID(_ context.Context, id string, _ *string) (*domainagents.Agent, error) {
	return f.runtimes[id], nil
}

type fakeExtractionRunner struct {
	calls []domainagents.ExecuteRequest
}

func (f *fakeExtractionRunner) Execute(_ context.Context, req domainagents.ExecuteRequest) (*domainagents.ExecuteResult, error) {
	f.calls = append(f.calls, req)
	return &domainagents.ExecuteResult{RunID: "run-1", Steps: 3}, nil
}

func newTestWorker(store extractionAgentStore, runner extractionAgentRunner) *ObjectExtractionWorker {
	return &ObjectExtractionWorker{
		agentsRepo:  store,
		agentRunner: runner,
		log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func extractionTestJob() *ObjectExtractionJob {
	docID := "11111111-1111-1111-1111-111111111111"
	return &ObjectExtractionJob{ID: "22222222-2222-2222-2222-222222222222", ProjectID: "33333333-3333-3333-3333-333333333333", DocumentID: &docID}
}

func agentSetting(id string) *domainagents.ProjectSetting {
	return &domainagents.ProjectSetting{Value: map[string]any{"agentId": id}}
}

func TestSelectedExtractionAgent_FallsBackToBuiltin(t *testing.T) {
	cases := []struct {
		name  string
		store *fakeExtractionAgentStore
	}{
		{"unset", &fakeExtractionAgentStore{}},
		{"missing definition", &fakeExtractionAgentStore{settings: map[string]*domainagents.ProjectSetting{"extraction/agent_id": agentSetting("aaaa")}}},
		{"disabled definition", &fakeExtractionAgentStore{
			settings: map[string]*domainagents.ProjectSetting{"extraction/agent_id": agentSetting("aaaa")},
			defs:     map[string]*domainagents.AgentDefinition{"aaaa": {ID: "aaaa", Name: "disabled", Enabled: false}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorker(tc.store, &fakeExtractionRunner{})
			assert.Nil(t, w.selectedExtractionAgent(context.Background(), extractionTestJob()))
		})
	}
}

func TestSelectedExtractionAgent_ResolvesDefinitionAndLimits(t *testing.T) {
	store := &fakeExtractionAgentStore{
		settings: map[string]*domainagents.ProjectSetting{
			"extraction/agent_id":        agentSetting("aaaa"),
			"extraction/max_steps":       {Value: map[string]any{"value": float64(24)}},
			"extraction/timeout_seconds": {Value: map[string]any{"value": float64(90)}},
		},
		defs: map[string]*domainagents.AgentDefinition{"aaaa": {ID: "aaaa", Name: "extractor", Enabled: true}},
	}
	w := newTestWorker(store, &fakeExtractionRunner{})
	sel := w.selectedExtractionAgent(context.Background(), extractionTestJob())
	require.NotNil(t, sel)
	require.NotNil(t, sel.definition)
	assert.Equal(t, "extractor", sel.definition.Name)
	require.NotNil(t, sel.maxSteps)
	assert.Equal(t, 24, *sel.maxSteps)
	require.NotNil(t, sel.timeout)
	assert.Equal(t, "1m30s", sel.timeout.String())
}

func TestBuildAgentExtractionMessage_PinsContractAndBranch(t *testing.T) {
	job := extractionTestJob()
	def := &domainagents.AgentDefinition{ID: "aaaa", Name: "extractor"}
	schemas := &ExtractionSchemas{
		ObjectSchemas:       map[string]agents.ObjectSchema{"Person": {Name: "Person"}},
		RelationshipSchemas: map[string]agents.RelationshipSchema{"KNOWS": {Name: "KNOWS"}},
	}
	msg := buildAgentExtractionMessage(job, def, schemas, "Monica works at Javu.", "extraction/doc/job")
	for _, want := range []string{
		"extraction/doc/job",
		job.ID,
		`"Person"`,
		`"KNOWS"`,
		"Monica works at Javu.",
		"entity-create, relationship-create",
		"Do not fabricate",
	} {
		assert.True(t, strings.Contains(msg, want), "message must contain %q", want)
	}
}

func TestAgentExtractionRunContext_PinsJobBranchAndProvenance(t *testing.T) {
	job := extractionTestJob()
	job.SourceType = extractionStringPtr(string(SourceTypeDocument))
	job.SourceID = extractionStringPtr("44444444-4444-4444-4444-444444444444")
	job.CreatedAt = time.Now().UTC()

	ctx, err := agentExtractionRunContext(context.Background(), job, "extraction/doc/job", 3)
	require.NoError(t, err)

	run, ok := mcp.ExtractionRunFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, job.ID, run.JobID)
	assert.Equal(t, "extraction/doc/job", run.BranchRef)
	assert.Equal(t, "suggested", run.Status)
	assert.Equal(t, job.ID, run.Provenance["_extraction_job_id"])
	assert.Equal(t, "44444444-4444-4444-4444-444444444444", run.Provenance["_source_id"])
	if v, ok := run.Provenance["_document_version"].(int); ok {
		assert.Equal(t, 3, v)
	}

	assert.True(t, mcp.IsFixedBranchScope(ctx), "agent extraction must install a caller-fixed boundary")
	scope := mcp.BranchScopeFromContext(ctx)
	require.NotNil(t, scope)
	assert.True(t, scope.AllowsOrigin("extraction"))
	assert.True(t, scope.AllowsName("extraction/doc/job"))
	assert.False(t, scope.AllowsName("some/other-branch"))
}

func extractionStringPtr(s string) *string { return &s }
