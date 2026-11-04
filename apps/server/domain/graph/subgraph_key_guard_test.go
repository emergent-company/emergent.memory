package graph

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// subgraphKeyGuardSchemas has one board-enabled type ("Task") and one
// non-board type ("Note").
func subgraphKeyGuardSchemas() *ExtractionSchemas {
	return &ExtractionSchemas{
		ObjectSchemas: map[string]agents.ObjectSchema{
			"Task": {Name: "Task", ObjectTypeWorkConfig: agents.ObjectTypeWorkConfig{BoardEnabled: true}},
			"Note": {Name: "Note", ObjectTypeWorkConfig: agents.ObjectTypeWorkConfig{BoardEnabled: false}},
		},
	}
}

func requireWorkStatusKeyRequired(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "work_status_key_required", appErr.Code)
	require.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
}

// TestValidateSubgraphObjectKeys covers the board-enabled stable-key guard at
// the validation seam for the subgraph/bulk path (issue #1655).
func TestValidateSubgraphObjectKeys(t *testing.T) {
	key := "stable-key"

	t.Run("(a) board-enabled type without key is rejected", func(t *testing.T) {
		err := validateSubgraphObjectKeys(subgraphKeyGuardSchemas(), []SubgraphObjectRequest{
			{Type: "Task", Properties: map[string]any{}},
		})
		requireWorkStatusKeyRequired(t, err)
	})

	t.Run("(a) board-enabled type with empty key is rejected", func(t *testing.T) {
		empty := ""
		err := validateSubgraphObjectKeys(subgraphKeyGuardSchemas(), []SubgraphObjectRequest{
			{Type: "Task", Key: &empty},
		})
		requireWorkStatusKeyRequired(t, err)
	})

	t.Run("(a) one keyless board-enabled object rejects the whole batch", func(t *testing.T) {
		err := validateSubgraphObjectKeys(subgraphKeyGuardSchemas(), []SubgraphObjectRequest{
			{Type: "Task", Key: &key},
			{Type: "Task", Properties: map[string]any{}}, // keyless
		})
		requireWorkStatusKeyRequired(t, err)
	})

	t.Run("(b) board-enabled type WITH key passes", func(t *testing.T) {
		require.NoError(t, validateSubgraphObjectKeys(subgraphKeyGuardSchemas(), []SubgraphObjectRequest{
			{Type: "Task", Key: &key},
		}))
	})

	t.Run("(c) non-board type without key passes", func(t *testing.T) {
		require.NoError(t, validateSubgraphObjectKeys(subgraphKeyGuardSchemas(), []SubgraphObjectRequest{
			{Type: "Note", Properties: map[string]any{}},
		}))
	})

	t.Run("unknown type without key passes (never board-enabled)", func(t *testing.T) {
		require.NoError(t, validateSubgraphObjectKeys(subgraphKeyGuardSchemas(), []SubgraphObjectRequest{
			{Type: "Unknown"},
		}))
	})

	t.Run("nil schemas leaves the guard inert", func(t *testing.T) {
		require.NoError(t, validateSubgraphObjectKeys(nil, []SubgraphObjectRequest{
			{Type: "Task"},
		}))
	})
}

// TestCreateSubgraph_RejectsKeylessBoardEnabledBeforeWrite proves the guard
// fires in the real entry point before any database work: the service is wired
// with a nil repo (so reaching the transaction would panic). No rows are
// created because validation happens before BeginTx.
func TestCreateSubgraph_RejectsKeylessBoardEnabledBeforeWrite(t *testing.T) {
	svc := newTestService(&mockSchemaProvider{schemas: subgraphKeyGuardSchemas()})

	_, err := svc.CreateSubgraph(context.Background(), uuid.New(), &CreateSubgraphRequest{
		Objects: []SubgraphObjectRequest{{Type: "Task", Properties: map[string]any{}}},
	}, nil)

	requireWorkStatusKeyRequired(t, err)
}
