package documents

import (
	"reflect"
	"sort"
	"testing"
)

func strPtr(s string) *string { return &s }

func TestResolveGraphRemovals(t *testing.T) {
	jobA := "job-a"
	jobB := "job-b"
	jobUnrelated := "job-unrelated"

	tests := []struct {
		name           string
		rows           []graphVersionRow
		jobIDs         []string
		wantObjectIDs  []string
		wantCanonicals []string
	}{
		{
			name: "single entity single version attributable",
			rows: []graphVersionRow{
				{ID: "row-1", CanonicalID: "can-1", ExtractionJobID: strPtr(jobA)},
			},
			jobIDs:         []string{jobA},
			wantObjectIDs:  []string{"row-1"},
			wantCanonicals: []string{"can-1"},
		},
		{
			name: "deleting first writer of two-version entity keeps entity",
			rows: []graphVersionRow{
				{ID: "row-1", CanonicalID: "can-1", ExtractionJobID: strPtr(jobA)},
				{ID: "row-2", CanonicalID: "can-1", ExtractionJobID: strPtr(jobB)},
			},
			jobIDs:         []string{jobA},
			wantObjectIDs:  []string{"row-1"},
			wantCanonicals: []string{},
		},
		{
			name: "deleting last writer fully removes entity when it owns all versions",
			rows: []graphVersionRow{
				{ID: "row-1", CanonicalID: "can-1", ExtractionJobID: strPtr(jobB)},
				{ID: "row-2", CanonicalID: "can-1", ExtractionJobID: strPtr(jobB)},
			},
			jobIDs:         []string{jobB},
			wantObjectIDs:  []string{"row-1", "row-2"},
			wantCanonicals: []string{"can-1"},
		},
		{
			name: "deleting last writer of two-version entity keeps other job's version",
			rows: []graphVersionRow{
				{ID: "row-1", CanonicalID: "can-1", ExtractionJobID: strPtr(jobA)},
				{ID: "row-2", CanonicalID: "can-1", ExtractionJobID: strPtr(jobB)},
			},
			jobIDs:         []string{jobB},
			wantObjectIDs:  []string{"row-2"},
			wantCanonicals: []string{},
		},
		{
			name: "null-provenance version never fully removed",
			rows: []graphVersionRow{
				{ID: "row-1", CanonicalID: "can-1", ExtractionJobID: strPtr(jobA)},
				{ID: "row-2", CanonicalID: "can-1", ExtractionJobID: nil},
			},
			jobIDs:         []string{jobA},
			wantObjectIDs:  []string{"row-1"},
			wantCanonicals: []string{},
		},
		{
			name: "unrelated job rows untouched",
			rows: []graphVersionRow{
				{ID: "row-1", CanonicalID: "can-1", ExtractionJobID: strPtr(jobA)},
				{ID: "row-2", CanonicalID: "can-2", ExtractionJobID: strPtr(jobUnrelated)},
			},
			jobIDs:         []string{jobA},
			wantObjectIDs:  []string{"row-1"},
			wantCanonicals: []string{"can-1"},
		},
		{
			name: "empty jobIDs yields empty results",
			rows: []graphVersionRow{
				{ID: "row-1", CanonicalID: "can-1", ExtractionJobID: strPtr(jobA)},
			},
			jobIDs:         []string{},
			wantObjectIDs:  []string{},
			wantCanonicals: []string{},
		},
		{
			name:           "empty rows yields empty results",
			rows:           []graphVersionRow{},
			jobIDs:         []string{jobA},
			wantObjectIDs:  []string{},
			wantCanonicals: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotObjects, gotCanonicals := resolveGraphRemovals(tt.rows, tt.jobIDs)

			sort.Strings(gotObjects)
			sort.Strings(gotCanonicals)
			sort.Strings(tt.wantObjectIDs)
			sort.Strings(tt.wantCanonicals)

			if !reflect.DeepEqual(gotObjects, tt.wantObjectIDs) {
				t.Errorf("objectRowIDs = %v, want %v", gotObjects, tt.wantObjectIDs)
			}
			if !reflect.DeepEqual(gotCanonicals, tt.wantCanonicals) {
				t.Errorf("canonicalIDs = %v, want %v", gotCanonicals, tt.wantCanonicals)
			}
		})
	}
}
