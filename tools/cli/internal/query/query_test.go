package query

import (
	"testing"
)

func TestParseFilters(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []Filter
		wantErr  bool
	}{
		{
			name:  "simple equality",
			input: "name=test",
			expected: []Filter{
				{Key: "name", Value: "test", Operator: "eq"},
			},
		},
		{
			name:  "multiple filters",
			input: "name=test,status=active",
			expected: []Filter{
				{Key: "name", Value: "test", Operator: "eq"},
				{Key: "status", Value: "active", Operator: "eq"},
			},
		},
		{
			name:  "not equal operator",
			input: "status!=inactive",
			expected: []Filter{
				{Key: "status", Value: "inactive", Operator: "ne"},
			},
		},
		{
			name:  "greater than operator",
			input: "count>10",
			expected: []Filter{
				{Key: "count", Value: "10", Operator: "gt"},
			},
		},
		{
			name:  "less than operator",
			input: "count<100",
			expected: []Filter{
				{Key: "count", Value: "100", Operator: "lt"},
			},
		},
		{
			name:  "contains operator",
			input: "description~keyword",
			expected: []Filter{
				{Key: "description", Value: "keyword", Operator: "contains"},
			},
		},
		{
			name:     "empty input",
			input:    "",
			expected: nil,
		},
		{
			name:    "invalid format",
			input:   "invalidfilter",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseFilters(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(result) != len(tt.expected) {
				t.Fatalf("Expected %d filters, got %d", len(tt.expected), len(result))
			}

			for i, expected := range tt.expected {
				if result[i].Key != expected.Key {
					t.Errorf("Filter %d: expected key %s, got %s", i, expected.Key, result[i].Key)
				}
				if result[i].Value != expected.Value {
					t.Errorf("Filter %d: expected value %s, got %s", i, expected.Value, result[i].Value)
				}
				if result[i].Operator != expected.Operator {
					t.Errorf("Filter %d: expected operator %s, got %s", i, expected.Operator, result[i].Operator)
				}
			}
		})
	}
}

func TestParseSort(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []Sort
		wantErr  bool
	}{
		{
			name:  "single field ascending",
			input: "name:asc",
			expected: []Sort{
				{Field: "name", Order: "asc"},
			},
		},
		{
			name:  "single field descending",
			input: "created_at:desc",
			expected: []Sort{
				{Field: "created_at", Order: "desc"},
			},
		},
		{
			name:  "default to ascending",
			input: "name",
			expected: []Sort{
				{Field: "name", Order: "asc"},
			},
		},
		{
			name:  "multiple sorts",
			input: "name:asc,created_at:desc",
			expected: []Sort{
				{Field: "name", Order: "asc"},
				{Field: "created_at", Order: "desc"},
			},
		},
		{
			name:     "empty input",
			input:    "",
			expected: nil,
		},
		{
			name:    "invalid order",
			input:   "name:invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseSort(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(result) != len(tt.expected) {
				t.Fatalf("Expected %d sorts, got %d", len(tt.expected), len(result))
			}

			for i, expected := range tt.expected {
				if result[i].Field != expected.Field {
					t.Errorf("Sort %d: expected field %s, got %s", i, expected.Field, result[i].Field)
				}
				if result[i].Order != expected.Order {
					t.Errorf("Sort %d: expected order %s, got %s", i, expected.Order, result[i].Order)
				}
			}
		})
	}
}

func TestApplyFilters(t *testing.T) {
	item := map[string]interface{}{
		"name":   "Test Project",
		"status": "active",
		"count":  42,
	}

	tests := []struct {
		name     string
		filters  []Filter
		expected bool
	}{
		{
			name:     "no filters",
			filters:  []Filter{},
			expected: true,
		},
		{
			name: "matching equality",
			filters: []Filter{
				{Key: "status", Value: "active", Operator: "eq"},
			},
			expected: true,
		},
		{
			name: "non-matching equality",
			filters: []Filter{
				{Key: "status", Value: "inactive", Operator: "eq"},
			},
			expected: false,
		},
		{
			name: "contains match",
			filters: []Filter{
				{Key: "name", Value: "test", Operator: "contains"},
			},
			expected: true,
		},
		{
			name: "greater than match",
			filters: []Filter{
				{Key: "count", Value: "40", Operator: "gt"},
			},
			expected: true,
		},
		{
			name: "less than no match",
			filters: []Filter{
				{Key: "count", Value: "40", Operator: "lt"},
			},
			expected: false,
		},
		{
			name: "multiple filters all match",
			filters: []Filter{
				{Key: "status", Value: "active", Operator: "eq"},
				{Key: "count", Value: "40", Operator: "gt"},
			},
			expected: true,
		},
		{
			name: "multiple filters one doesn't match",
			filters: []Filter{
				{Key: "status", Value: "active", Operator: "eq"},
				{Key: "count", Value: "50", Operator: "gt"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ApplyFilters(item, tt.filters)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestFormatPaginationInfo(t *testing.T) {
	tests := []struct {
		name     string
		showing  int
		total    int
		hasMore  bool
		expected string
	}{
		{
			name:     "no results",
			showing:  0,
			total:    0,
			hasMore:  false,
			expected: "No results",
		},
		{
			name:     "showing all",
			showing:  10,
			total:    10,
			hasMore:  false,
			expected: "Showing all 10 results",
		},
		{
			name:     "showing partial",
			showing:  10,
			total:    50,
			hasMore:  false,
			expected: "Showing 10 of 50 results",
		},
		{
			name:     "has more available",
			showing:  10,
			total:    50,
			hasMore:  true,
			expected: "Showing 10 results (more available)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatPaginationInfo(tt.showing, tt.total, tt.hasMore)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}
