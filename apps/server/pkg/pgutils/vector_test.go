package pgutils

import (
	"testing"
)

func TestFormatVector(t *testing.T) {
	tests := []struct {
		name string
		v    []float32
		want string
	}{
		{
			name: "empty slice",
			v:    []float32{},
			want: "[]",
		},
		{
			name: "nil slice",
			v:    nil,
			want: "[]",
		},
		{
			name: "single element",
			v:    []float32{0.5},
			want: "[0.5]",
		},
		{
			name: "two elements",
			v:    []float32{0.1, 0.2},
			want: "[0.1,0.2]",
		},
		{
			name: "three elements",
			v:    []float32{0.1, 0.2, 0.3},
			want: "[0.1,0.2,0.3]",
		},
		{
			name: "integer values",
			v:    []float32{1, 2, 3},
			want: "[1,2,3]",
		},
		{
			name: "negative values",
			v:    []float32{-0.5, 0, 0.5},
			want: "[-0.5,0,0.5]",
		},
		{
			name: "very small values",
			v:    []float32{0.0001, 0.0002},
			want: "[0.0001,0.0002]",
		},
		{
			name: "large values",
			v:    []float32{1000.5, 2000.25},
			want: "[1000.5,2000.25]",
		},
		{
			name: "mixed positive and negative",
			v:    []float32{-1.5, 0, 1.5, -2.25, 2.25},
			want: "[-1.5,0,1.5,-2.25,2.25]",
		},
		{
			name: "typical embedding dimension sample",
			v:    []float32{0.123, -0.456, 0.789, -0.012, 0.345},
			want: "[0.123,-0.456,0.789,-0.012,0.345]",
		},
		{
			name: "zeros",
			v:    []float32{0, 0, 0},
			want: "[0,0,0]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatVector(tt.v)
			if got != tt.want {
				t.Errorf("FormatVector() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseVector(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []float32
		wantErr bool
	}{
		{
			name:  "empty string",
			input: "",
			want:  []float32{},
		},
		{
			name:  "empty vector",
			input: "[]",
			want:  []float32{},
		},
		{
			name:  "single element",
			input: "[0.5]",
			want:  []float32{0.5},
		},
		{
			name:  "multiple elements",
			input: "[0.1,0.2,0.3]",
			want:  []float32{0.1, 0.2, 0.3},
		},
		{
			name:  "negative values",
			input: "[-0.5,0,0.5]",
			want:  []float32{-0.5, 0, 0.5},
		},
		{
			name:  "spaces around brackets",
			input: "  [0.1,0.2]  ",
			want:  []float32{0.1, 0.2},
		},
		{
			name:    "missing opening bracket",
			input:   "0.1,0.2,0.3]",
			wantErr: true,
		},
		{
			name:    "missing closing bracket",
			input:   "[0.1,0.2,0.3",
			wantErr: true,
		},
		{
			name:    "invalid element",
			input:   "[0.1,notanumber,0.3]",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVector(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseVector(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseVector(%q) unexpected error: %v", tt.input, err)
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("ParseVector(%q) len = %d, want %d", tt.input, len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ParseVector(%q)[%d] = %v, want %v", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseVectorRoundTrip(t *testing.T) {
	// Verify that FormatVector → ParseVector round-trips correctly.
	original := []float32{0.1, -0.2, 0.3, 0, 1.0, -1.0}
	formatted := FormatVector(original)
	parsed, err := ParseVector(formatted)
	if err != nil {
		t.Fatalf("ParseVector(FormatVector(%v)) error: %v", original, err)
	}
	if len(parsed) != len(original) {
		t.Fatalf("round-trip length mismatch: got %d, want %d", len(parsed), len(original))
	}
	for i := range parsed {
		if parsed[i] != original[i] {
			t.Errorf("round-trip[%d]: got %v, want %v", i, parsed[i], original[i])
		}
	}
}

func TestFormatVectorLargeSlice(t *testing.T) {
	// Test with a larger slice to ensure buffer allocation works
	size := 100
	v := make([]float32, size)
	for i := 0; i < size; i++ {
		v[i] = float32(i) * 0.1
	}

	result := FormatVector(v)

	// Should start with [ and end with ]
	if result[0] != '[' {
		t.Errorf("FormatVector() should start with '[', got %q", result[0])
	}
	if result[len(result)-1] != ']' {
		t.Errorf("FormatVector() should end with ']', got %q", result[len(result)-1])
	}

	// Should contain commas (at least size-1 of them)
	commaCount := 0
	for _, c := range result {
		if c == ',' {
			commaCount++
		}
	}
	if commaCount != size-1 {
		t.Errorf("FormatVector() should have %d commas, got %d", size-1, commaCount)
	}
}
