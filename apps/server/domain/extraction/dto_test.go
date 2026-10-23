package extraction

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestObjectExtractionJobToDTO(t *testing.T) {
	branchID := "br1"
	j := &ObjectExtractionJob{
		ID:               "job1",
		ProjectID:        "p1",
		ObjectsCreated:   31,
		CreatedObjectIDs: []string{"o1", "o2", "o3"},
		StagingBranchID:  &branchID,
		CreatedObjects:   JSONArray{"legacy-1", "legacy-2"},
		DiscoveredTypes:  JSONArray{"person", "task"},
		SourceType:       strPtr("document"),
	}

	dto := j.ToDTO()

	if dto.ObjectsCreated != 31 {
		t.Errorf("ObjectsCreated = %d, want 31", dto.ObjectsCreated)
	}
	if !reflect.DeepEqual(dto.CreatedObjectIDs, []string{"o1", "o2", "o3"}) {
		t.Errorf("CreatedObjectIDs = %v, want [o1 o2 o3]", dto.CreatedObjectIDs)
	}
	if dto.StagingBranchID == nil || *dto.StagingBranchID != "br1" {
		t.Errorf("StagingBranchID = %v, want br1", dto.StagingBranchID)
	}
	// Legacy created_objects (jsonb) still maps through.
	if !reflect.DeepEqual(dto.CreatedObjects, []string{"legacy-1", "legacy-2"}) {
		t.Errorf("CreatedObjects = %v, want [legacy-1 legacy-2]", dto.CreatedObjects)
	}
	if !reflect.DeepEqual(dto.DiscoveredTypes, []string{"person", "task"}) {
		t.Errorf("DiscoveredTypes = %v, want [person task]", dto.DiscoveredTypes)
	}
}

func TestObjectExtractionJobToDTONilCreatedObjectIDs(t *testing.T) {
	j := &ObjectExtractionJob{ID: "job1", ProjectID: "p1"}
	dto := j.ToDTO()

	// created_object_ids must serialise as [] (not null) when the entity slice
	// is nil — matching the jsonb→slice conventions in ToDTO.
	if dto.CreatedObjectIDs == nil {
		t.Fatal("CreatedObjectIDs must be a non-nil slice when the entity field is nil")
	}
	if len(dto.CreatedObjectIDs) != 0 {
		t.Fatalf("CreatedObjectIDs = %v, want empty", dto.CreatedObjectIDs)
	}
	if dto.CreatedObjects == nil {
		t.Fatal("CreatedObjects must be a non-nil slice when the entity field is nil")
	}
	if dto.StagingBranchID != nil {
		t.Errorf("StagingBranchID = %v, want nil", dto.StagingBranchID)
	}
}

// TestObjectExtractionJobToDTORetryCount ensures the entity's RetryCount flows
// through ToDTO and serialises as retry_count (without omitempty, so the gateway
// sees 0 rather than a missing key).
func TestObjectExtractionJobToDTORetryCount(t *testing.T) {
	j := &ObjectExtractionJob{ID: "job1", ProjectID: "p1", RetryCount: 2}

	dto := j.ToDTO()
	if dto.RetryCount != 2 {
		t.Fatalf("RetryCount = %d, want 2", dto.RetryCount)
	}

	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal DTO: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal DTO: %v", err)
	}
	got, ok := payload["retry_count"]
	if !ok {
		t.Fatalf("retry_count key missing from JSON: %s", raw)
	}
	if got != float64(2) {
		t.Fatalf("retry_count = %v, want 2", got)
	}
}
