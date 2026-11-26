package extraction

import "testing"

func TestAccumulateBatchResultsFoldsDroppedCounts(t *testing.T) {
	total := &ObjectExtractionResults{DroppedObjectTypes: []string{"Org"}}
	batch := &ObjectExtractionResults{
		DroppedObjects:       3,
		DroppedRelationships: 1,
		DroppedObjectTypes:   []string{"Org", "Place"},
	}
	accumulateBatchResults(total, batch)

	if total.DroppedObjects != 3 || total.DroppedRelationships != 1 {
		t.Fatalf("dropped counts not folded: %+v", total)
	}
	if len(total.DroppedObjectTypes) != 2 {
		t.Fatalf("dropped types not deduped: %v", total.DroppedObjectTypes)
	}
}
