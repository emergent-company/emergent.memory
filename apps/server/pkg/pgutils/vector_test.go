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
