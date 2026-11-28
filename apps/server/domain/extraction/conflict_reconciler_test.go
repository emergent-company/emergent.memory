package extraction

import (
	"context"
	"errors"
	"iter"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/modelconfig"
)

type reconcileTestLLM struct {
	response string
	err      error
}

func (m *reconcileTestLLM) Name() string { return "test-model" }

func (m *reconcileTestLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, streaming bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if m.err != nil {
			yield(nil, m.err)
			return
		}
		yield(&model.LLMResponse{Content: genai.NewContentFromText(m.response, "model")}, nil)
	}
}

type reconcileTestModelFactory struct {
	llm model.LLM
	err error
}

func (f *reconcileTestModelFactory) CreateModelWithName(ctx context.Context, modelName string) (model.LLM, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.llm, nil
}

type reconcileTestResolver struct {
	model string
	err   error
}

func (r *reconcileTestResolver) ResolveGenerativeModel(context.Context, uuid.UUID) (string, modelconfig.ModelSource, error) {
	if r.err != nil {
		return "", "", r.err
	}
	return r.model, modelconfig.ModelSourceProject, nil
}

func reconcileTestRequest() graph.ConflictReconcileRequest {
	return graph.ConflictReconcileRequest{
		ProjectID: uuid.New(),
		TypeName:  "Person",
		Conflicts: map[string]graph.ConflictValue{
			"job_title": {Source: "Waitress", Target: "Waitress at Central Perk"},
		},
	}
}

// TestConflictReconcilerMergesValidJSON pins the agents reconciler happy path:
// a valid JSON answer is parsed and returned keyed by property name.
func TestConflictReconcilerMergesValidJSON(t *testing.T) {
	r := NewConflictReconciler(
		&reconcileTestModelFactory{llm: &reconcileTestLLM{response: `{"job_title":"Waitress at Central Perk"}`}},
		&reconcileTestResolver{model: "test/test-model"},
		slog.Default(),
	)

	values, modelName, err := r.Reconcile(context.Background(), reconcileTestRequest())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if modelName != "test/test-model" {
		t.Fatalf("model = %q, want test/test-model", modelName)
	}
	if values["job_title"] != "Waitress at Central Perk" {
		t.Fatalf("job_title = %v, want combined value", values["job_title"])
	}
}

// TestConflictReconcilerRejectsInvalidJSON pins the fallback: malformed JSON is
// an error (the graph merge maps it to deterministic newest-wins).
func TestConflictReconcilerRejectsInvalidJSON(t *testing.T) {
	r := NewConflictReconciler(
		&reconcileTestModelFactory{llm: &reconcileTestLLM{response: `not-json`}},
		&reconcileTestResolver{model: "test/test-model"},
		slog.Default(),
	)
	_, _, err := r.Reconcile(context.Background(), reconcileTestRequest())
	if err == nil {
		t.Fatal("expected invalid JSON to error")
	}
}

// TestConflictReconcilerModelErrorPropagates pins the error passthrough from
// the model call.
func TestConflictReconcilerModelErrorPropagates(t *testing.T) {
	r := NewConflictReconciler(
		&reconcileTestModelFactory{llm: &reconcileTestLLM{err: errors.New("boom")}},
		&reconcileTestResolver{model: "test/test-model"},
		slog.Default(),
	)
	_, _, err := r.Reconcile(context.Background(), reconcileTestRequest())
	if err == nil {
		t.Fatal("expected model error to propagate")
	}
}

// TestConflictReconcilerCachesRepeatedCalls pins determinism: the same conflict
// set is answered from the cache, so the model is not called twice and re-runs
// are stable.
func TestConflictReconcilerCachesRepeatedCalls(t *testing.T) {
	llm := &reconcileTestLLM{response: `{"job_title":"Waitress at Central Perk"}`}
	factory := &reconcileTestModelFactory{llm: llm}
	r := NewConflictReconciler(factory, &reconcileTestResolver{model: "test/test-model"}, slog.Default())

	req := reconcileTestRequest()
	if _, _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	// A second identical request is served from the cache; a factory error
	// would surface if the model were consulted again.
	factory.llm = &reconcileTestLLM{err: errors.New("should not be called")}
	if _, _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("cached Reconcile: %v", err)
	}
}
