package httputil_test

import (
	"encoding/json"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/httputil"
)

func TestNewSuccessResponse(t *testing.T) {
	resp := httputil.NewSuccessResponse("ok")
	if !resp.Success {
		t.Fatal("success flag must be true")
	}
	if resp.Data != "ok" {
		t.Fatalf("Data = %v, want %q", resp.Data, "ok")
	}
	if resp.Error != nil || resp.Message != nil {
		t.Fatal("Error/Message must be nil on a success response")
	}
}

func TestNewErrorResponse(t *testing.T) {
	resp := httputil.NewErrorResponse[any]("boom")
	if resp.Success {
		t.Fatal("success flag must be false on an error response")
	}
	if resp.Error == nil || *resp.Error != "boom" {
		t.Fatalf("Error = %v, want pointer to %q", resp.Error, "boom")
	}
}

func TestSuccessResponseJSONShape(t *testing.T) {
	resp := httputil.NewSuccessResponse(map[string]int{"a": 1})
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["success"] != true {
		t.Fatalf("success field missing or false in %s", b)
	}
	if _, ok := m["data"]; !ok {
		t.Fatalf("data field missing in %s", b)
	}
}

func TestPaginatedResponseFields(t *testing.T) {
	p := httputil.PaginatedResponse[int]{
		Items:      []int{1, 2, 3},
		TotalCount: 3,
		Limit:      10,
		Offset:     0,
		NextCursor: "abc",
	}
	if len(p.Items) != 3 || p.TotalCount != 3 || p.Limit != 10 || p.Offset != 0 || p.NextCursor != "abc" {
		t.Fatalf("PaginatedResponse fields not preserved: %+v", p)
	}
}
