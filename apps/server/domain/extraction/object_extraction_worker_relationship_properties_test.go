package extraction

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
)

// TestRelationshipPersistProperties_CarriesEmittedProperties is the write-side
// half of #264: a property the model emits on an edge must reach the map handed
// to graph.CreateRelationship (where validateRelationshipProperties enforces the
// declared schema), not just the parsed output struct.
func TestRelationshipPersistProperties_CarriesEmittedProperties(t *testing.T) {
	rel := agents.ExtractedRelationship{
		SourceRef:   "person_alice",
		TargetRef:   "org_acme",
		Type:        "MEMBER_OF",
		Description: "Alice chairs the board",
		Properties: map[string]any{
			"role":       "chair",
			"term_count": 2,
			// A model can echo a key it saw in a prompt; provenance is
			// server-owned and must survive that (issue #310).
			"_extraction_job_id": "model-emitted-job",
		},
	}

	props := relationshipPersistProperties(rel, "job-123")

	assert.Equal(t, "chair", props["role"], "emitted edge property must be written")
	assert.Equal(t, 2, props["term_count"], "emitted edge property must keep its value")
	assert.Equal(t, "Alice chairs the board", props["description"])
	assert.Equal(t, "job-123", props["_extraction_job_id"],
		"a model-emitted provenance key must not replace the server-stamped job id")
}

// A model-emitted property named `description` follows the object-side merge
// order: the property wins over the derived description.
func TestRelationshipPersistProperties_PropertyOverridesDerivedFields(t *testing.T) {
	rel := agents.ExtractedRelationship{
		Description: "derived",
		Properties:  map[string]any{"description": "declared"},
	}

	props := relationshipPersistProperties(rel, "job-1")

	assert.Equal(t, "declared", props["description"])
}

// A relationship with no emitted properties still carries its description and
// provenance, so existing extraction output is unchanged.
func TestRelationshipPersistProperties_NoEmittedProperties(t *testing.T) {
	props := relationshipPersistProperties(agents.ExtractedRelationship{Description: "d"}, "job-1")

	assert.Equal(t, map[string]any{
		"description":        "d",
		"_extraction_job_id": "job-1",
	}, props)
}
