package branches

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/branchorigin"
)

// TestToResponseOrigin covers the legacy-row default and the attribution
// passthrough — origin_id/origin_label are what the UI labels a branch with.
func TestToResponseOrigin(t *testing.T) {
	legacy := ToResponse(&Branch{ID: "b1", Name: "old-branch"})
	require.NotNil(t, legacy)
	assert.Equal(t, branchorigin.Manual, legacy.Origin, "a row predating the column reads as manual")
	assert.Nil(t, legacy.OriginID)
	assert.Nil(t, legacy.OriginLabel)

	jobID := "11111111-1111-1111-1111-111111111111"
	label := "Invoice.pdf"
	resp := ToResponse(&Branch{
		ID:          "b2",
		Name:        "extraction/aaaa/bbbb",
		Origin:      branchorigin.Extraction,
		OriginID:    &jobID,
		OriginLabel: &label,
	})
	require.NotNil(t, resp)
	assert.Equal(t, branchorigin.Extraction, resp.Origin)
	assert.Equal(t, &jobID, resp.OriginID)
	assert.Equal(t, &label, resp.OriginLabel)
	assert.Equal(t, "extraction/aaaa/bbbb", resp.Name, "the name is still returned alongside the label")
}

// TestCreateBranchRequestIgnoresAttributionFromJSON pins the security property:
// the attribution is server-derived, so a request body cannot claim an origin
// or another source's id (cleanup deletes on origin — #246).
func TestCreateBranchRequestIgnoresAttributionFromJSON(t *testing.T) {
	var req CreateBranchRequest
	body := []byte(`{"name":"x","origin":"extraction","origin_id":"11111111-1111-1111-1111-111111111111","origin_label":"Invoice.pdf"}`)
	require.NoError(t, json.Unmarshal(body, &req))
	assert.Nil(t, req.Attribution, "the attribution field must not be bindable from JSON")

	// A server-side caller sets it directly, which is the only way in.
	req.Attribution = &branchorigin.Attribution{Origin: branchorigin.Extraction}
	require.NotNil(t, req.Attribution)
}
