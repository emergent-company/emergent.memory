package extraction

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
)

// recordingGraphWriter records persistResults' graph writes instead of touching
// a database, so the identity-resolution wiring can be asserted end to end.
type recordingGraphWriter struct {
	upserts []*graph.CreateGraphObjectRequest
	creates []*graph.CreateGraphObjectRequest
	becomes []bool
}

func (r *recordingGraphWriter) Create(_ context.Context, _ uuid.UUID, req *graph.CreateGraphObjectRequest, _ *uuid.UUID) (*graph.GraphObjectResponse, error) {
	r.creates = append(r.creates, req)
	return &graph.GraphObjectResponse{ID: uuid.New(), CanonicalID: uuid.New()}, nil
}

func (r *recordingGraphWriter) CreateOrUpdate(_ context.Context, _ uuid.UUID, req *graph.CreateGraphObjectRequest, _ *uuid.UUID) (*graph.GraphObjectResponse, bool, error) {
	r.upserts = append(r.upserts, req)
	created := true
	if len(r.becomes) > 0 {
		created = r.becomes[len(r.upserts)-1]
	}
	return &graph.GraphObjectResponse{ID: uuid.New(), CanonicalID: uuid.New()}, created, nil
}

func (r *recordingGraphWriter) CreateRelationship(_ context.Context, _ uuid.UUID, _ *graph.CreateGraphRelationshipRequest, _ *uuid.UUID) (*graph.GraphRelationshipResponse, error) {
	return &graph.GraphRelationshipResponse{ID: uuid.New()}, nil
}

// TestPersistResultsHonoursExistingEntityID pins the wiring between the
// identity-resolution index and persistence (#1123): an entity carrying an
// existing_entity_id must upsert under the existing object's stored key/name
// and count as a merge, not insert a new object under the model's spelling.
func TestPersistResultsHonoursExistingEntityID(t *testing.T) {
	projectID := uuid.NewString()
	job := &ObjectExtractionJob{ID: uuid.NewString(), ProjectID: projectID}
	writer := &recordingGraphWriter{becomes: []bool{false}}
	w := &ObjectExtractionWorker{graphWriter: writer, log: quietLogger()}

	index := indexExistingEntities([]agents.ExistingEntityContext{{
		ID:       "canonical-1",
		Name:     "Katarzyna Nowak",
		TypeName: "Person",
		Key:      "katarzyna-nowak",
	}})
	output := &agents.ExtractionPipelineOutput{Entities: []agents.InternalEntity{{
		TempID:           "person_1",
		Name:             "Katarzyna Nowack",
		Type:             "Person",
		ExistingEntityID: "canonical-1",
	}}}

	result, err := w.persistResults(context.Background(), job, output, nil, 0, index, nil)
	require.NoError(t, err)
	require.Empty(t, writer.creates, "a resolved reference must upsert, not insert")
	require.Len(t, writer.upserts, 1)

	req := writer.upserts[0]
	require.NotNil(t, req.Key)
	require.Equal(t, "katarzyna-nowak", *req.Key, "must persist the existing object's stored key")
	require.NotNil(t, req.Name)
	require.Equal(t, "Katarzyna Nowak", *req.Name, "must keep the existing display name")
	require.Equal(t, "Katarzyna Nowack", req.Properties["_extraction_mention"],
		"must record the model's spelling so a wrong binding stays visible in review")
	require.Equal(t, 0, result.ObjectsCreated)
	require.Equal(t, 1, result.ObjectsMerged)
}

// TestPersistResultsWithoutIndexUsesModelSpelling is the other half of the
// wiring assertion: without the index the reference cannot resolve, so passing a
// nil index (the regression #1123 warns about) changes the outcome and fails the
// test above.
func TestPersistResultsWithoutIndexUsesModelSpelling(t *testing.T) {
	projectID := uuid.NewString()
	job := &ObjectExtractionJob{ID: uuid.NewString(), ProjectID: projectID}
	writer := &recordingGraphWriter{}
	w := &ObjectExtractionWorker{graphWriter: writer, log: quietLogger()}

	output := &agents.ExtractionPipelineOutput{Entities: []agents.InternalEntity{{
		TempID:           "person_1",
		Name:             "Katarzyna Nowack",
		Type:             "Person",
		ExistingEntityID: "canonical-1",
	}}}

	result, err := w.persistResults(context.Background(), job, output, nil, 0, nil, nil)
	require.NoError(t, err)
	require.Len(t, writer.upserts, 1)

	req := writer.upserts[0]
	require.NotNil(t, req.Key)
	require.Equal(t, "Katarzyna Nowack", *req.Key)
	require.NotContains(t, req.Properties, "_extraction_mention")
	require.Equal(t, 1, result.ObjectsCreated)
	require.Equal(t, 0, result.ObjectsMerged)
}
