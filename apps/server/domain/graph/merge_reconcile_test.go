package graph

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
)

type fakeSchemaProvider struct {
	schemas *ExtractionSchemas
}

func (f *fakeSchemaProvider) GetProjectSchemas(context.Context, string) (*ExtractionSchemas, error) {
	return f.schemas, nil
}

func (f *fakeSchemaProvider) InvalidateProjectCache(string) {}

type fakeReconciler struct {
	fn func(ctx context.Context, req ConflictReconcileRequest) (map[string]any, string, error)
}

func (f *fakeReconciler) Reconcile(ctx context.Context, req ConflictReconcileRequest) (map[string]any, string, error) {
	return f.fn(ctx, req)
}

func personSchemas() *ExtractionSchemas {
	return &ExtractionSchemas{
		ObjectSchemas: map[string]agents.ObjectSchema{
			"Person": {Properties: map[string]agents.PropertyDef{
				"role": {Type: "string"},
			}},
		},
	}
}

// TestReconcileResidualAcceptsValidValues pins the happy path of the reconcile
// residual resolver: a valid answer is returned unchanged.
func TestReconcileResidualAcceptsValidValues(t *testing.T) {
	svc := &Service{
		schemaProvider: &fakeSchemaProvider{schemas: personSchemas()},
		conflictReconciler: &fakeReconciler{fn: func(context.Context, ConflictReconcileRequest) (map[string]any, string, error) {
			return map[string]any{"role": "radio host and author"}, "model-x", nil
		}},
	}
	got, model, err := svc.reconcileResidual(context.Background(), uuid.New(), "Person",
		map[string]any{"role": "author"},
		map[string]any{"role": "radio host"},
		[]string{"/role"})
	require.NoError(t, err)
	assert.Equal(t, "model-x", model)
	assert.Equal(t, map[string]any{"role": "radio host and author"}, got)
}

// TestReconcileResidualRejectsSchemaViolation pins validation-before-apply: a
// value whose type violates the declared property schema is rejected.
func TestReconcileResidualRejectsSchemaViolation(t *testing.T) {
	svc := &Service{
		schemaProvider: &fakeSchemaProvider{schemas: &ExtractionSchemas{
			ObjectSchemas: map[string]agents.ObjectSchema{
				"Person": {Properties: map[string]agents.PropertyDef{
					"age": {Type: "integer"},
				}},
			},
		}},
		conflictReconciler: &fakeReconciler{fn: func(context.Context, ConflictReconcileRequest) (map[string]any, string, error) {
			return map[string]any{"age": "not-a-number"}, "model-x", nil // "age" is declared integer
		}},
	}
	_, _, err := svc.reconcileResidual(context.Background(), uuid.New(), "Person",
		map[string]any{"age": 41},
		map[string]any{"age": 40},
		[]string{"/age"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "violates schema")
}

// TestReconcileResidualRejectsPartialAnswer pins full coverage: the reconciler
// must answer every conflicting key or the whole object falls back.
func TestReconcileResidualRejectsPartialAnswer(t *testing.T) {
	svc := &Service{
		schemaProvider: &fakeSchemaProvider{schemas: personSchemas()},
		conflictReconciler: &fakeReconciler{fn: func(context.Context, ConflictReconcileRequest) (map[string]any, string, error) {
			return map[string]any{}, "model-x", nil
		}},
	}
	_, _, err := svc.reconcileResidual(context.Background(), uuid.New(), "Person",
		map[string]any{"role": "author"},
		map[string]any{"role": "radio host"},
		[]string{"/role"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not resolve")
}

// TestReconcileResidualPropagatesReconcilerError pins the error passthrough.
func TestReconcileResidualPropagatesReconcilerError(t *testing.T) {
	svc := &Service{
		schemaProvider: &fakeSchemaProvider{schemas: personSchemas()},
		conflictReconciler: &fakeReconciler{fn: func(context.Context, ConflictReconcileRequest) (map[string]any, string, error) {
			return nil, "", errors.New("boom")
		}},
	}
	_, _, err := svc.reconcileResidual(context.Background(), uuid.New(), "Person",
		map[string]any{"role": "author"},
		map[string]any{"role": "radio host"},
		[]string{"/role"})
	require.Error(t, err)
}
