package datasource

import (
	"testing"
)

func TestJSON_Value(t *testing.T) {
	tests := []struct {
		name        string
		input       JSON
		expectNil   bool
		expectError bool
	}{
		{
			name:      "nil JSON returns nil",
			input:     nil,
			expectNil: true,
		},
		{
			name:  "empty JSON",
			input: JSON{},
		},
		{
			name:  "JSON with string value",
			input: JSON{"key": "value"},
		},
		{
			name:  "JSON with nested object",
			input: JSON{"outer": map[string]interface{}{"inner": "value"}},
		},
		{
			name:  "JSON with various types",
			input: JSON{"str": "hello", "num": 42, "bool": true, "nil": nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.input.Value()

			if tt.expectError {
				if err == nil {
					t.Error("Value() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Value() unexpected error: %v", err)
				return
			}

			if tt.expectNil {
				if result != nil {
					t.Errorf("Value() = %v, want nil", result)
				}
				return
			}

			// Non-nil input should produce valid JSON bytes
			if result == nil {
				t.Error("Value() returned nil for non-nil input")
			}
		})
	}
}

func TestJSON_Scan(t *testing.T) {
	tests := []struct {
		name        string
		input       interface{}
		expectNil   bool
		expectError bool
		checkResult func(*JSON) bool
	}{
		{
			name:      "nil input",
			input:     nil,
			expectNil: true,
		},
		{
			name:  "valid JSON bytes",
			input: []byte(`{"key": "value"}`),
			checkResult: func(j *JSON) bool {
				return (*j)["key"] == "value"
			},
		},
		{
			name:  "empty JSON object",
			input: []byte(`{}`),
			checkResult: func(j *JSON) bool {
				return len(*j) == 0
			},
		},
		{
			name:        "invalid JSON",
			input:       []byte(`{invalid`),
			expectError: true,
		},
		{
			name:  "non-bytes input returns nil",
			input: "not bytes",
		},
		{
			name:  "nested JSON",
			input: []byte(`{"outer": {"inner": "value"}}`),
			checkResult: func(j *JSON) bool {
				outer, ok := (*j)["outer"].(map[string]interface{})
				return ok && outer["inner"] == "value"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JSON
			err := j.Scan(tt.input)

			if tt.expectError {
				if err == nil {
					t.Error("Scan() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Scan() unexpected error: %v", err)
				return
			}

			if tt.expectNil && j != nil {
				t.Errorf("Scan() expected nil, got %v", j)
				return
			}

			if tt.checkResult != nil && !tt.checkResult(&j) {
				t.Errorf("Scan() result check failed, got %v", j)
			}
		})
	}
}

func TestJSONArray_Value(t *testing.T) {
	tests := []struct {
		name        string
		input       JSONArray
		expectNil   bool
		expectError bool
	}{
		{
			name:      "nil JSONArray returns nil",
			input:     nil,
			expectNil: true,
		},
		{
			name:  "empty JSONArray",
			input: JSONArray{},
		},
		{
			name:  "JSONArray with strings",
			input: JSONArray{"a", "b", "c"},
		},
		{
			name:  "JSONArray with mixed types",
			input: JSONArray{"str", 42, true, nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.input.Value()

			if tt.expectError {
				if err == nil {
					t.Error("Value() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Value() unexpected error: %v", err)
				return
			}

			if tt.expectNil {
				if result != nil {
					t.Errorf("Value() = %v, want nil", result)
				}
				return
			}

			if result == nil {
				t.Error("Value() returned nil for non-nil input")
			}
		})
	}
}

func TestJSONArray_Scan(t *testing.T) {
	tests := []struct {
		name        string
		input       interface{}
		expectNil   bool
		expectError bool
		checkResult func(*JSONArray) bool
	}{
		{
			name:      "nil input",
			input:     nil,
			expectNil: true,
		},
		{
			name:  "valid JSON array bytes",
			input: []byte(`["a", "b", "c"]`),
			checkResult: func(j *JSONArray) bool {
				return len(*j) == 3 && (*j)[0] == "a"
			},
		},
		{
			name:  "empty JSON array",
			input: []byte(`[]`),
			checkResult: func(j *JSONArray) bool {
				return len(*j) == 0
			},
		},
		{
			name:        "invalid JSON",
			input:       []byte(`[invalid`),
			expectError: true,
		},
		{
			name:  "non-bytes input returns nil",
			input: 123,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JSONArray
			err := j.Scan(tt.input)

			if tt.expectError {
				if err == nil {
					t.Error("Scan() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Scan() unexpected error: %v", err)
				return
			}

			if tt.expectNil && j != nil {
				t.Errorf("Scan() expected nil, got %v", j)
				return
			}

			if tt.checkResult != nil && !tt.checkResult(&j) {
				t.Errorf("Scan() result check failed, got %v", j)
			}
		})
	}
}

func TestStringArray_Value(t *testing.T) {
	tests := []struct {
		name        string
		input       StringArray
		expectNil   bool
		expectError bool
	}{
		{
			name:      "nil StringArray returns nil",
			input:     nil,
			expectNil: true,
		},
		{
			name:  "empty StringArray",
			input: StringArray{},
		},
		{
			name:  "StringArray with values",
			input: StringArray{"one", "two", "three"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.input.Value()

			if tt.expectError {
				if err == nil {
					t.Error("Value() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Value() unexpected error: %v", err)
				return
			}

			if tt.expectNil {
				if result != nil {
					t.Errorf("Value() = %v, want nil", result)
				}
				return
			}

			if result == nil {
				t.Error("Value() returned nil for non-nil input")
			}
		})
	}
}

func TestStringArray_Scan(t *testing.T) {
	tests := []struct {
		name        string
		input       interface{}
		expectNil   bool
		expectError bool
		checkResult func(*StringArray) bool
	}{
		{
			name:      "nil input",
			input:     nil,
			expectNil: true,
		},
		{
			name:  "valid JSON array bytes",
			input: []byte(`["one", "two", "three"]`),
			checkResult: func(s *StringArray) bool {
				return len(*s) == 3 && (*s)[0] == "one" && (*s)[1] == "two" && (*s)[2] == "three"
			},
		},
		{
			name:  "empty JSON array",
			input: []byte(`[]`),
			checkResult: func(s *StringArray) bool {
				return len(*s) == 0
			},
		},
		{
			name:        "invalid JSON",
			input:       []byte(`["invalid`),
			expectError: true,
		},
		{
			name:  "non-bytes input returns nil",
			input: "not bytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s StringArray
			err := s.Scan(tt.input)

			if tt.expectError {
				if err == nil {
					t.Error("Scan() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Scan() unexpected error: %v", err)
				return
			}

			if tt.expectNil && s != nil {
				t.Errorf("Scan() expected nil, got %v", s)
				return
			}

			if tt.checkResult != nil && !tt.checkResult(&s) {
				t.Errorf("Scan() result check failed, got %v", s)
			}
		})
	}
}
