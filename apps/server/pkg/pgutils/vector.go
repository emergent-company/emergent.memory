// Package pgutils provides PostgreSQL utility functions for the Go server.
package pgutils

import (
	"fmt"
	"strconv"
	"strings"
)

// FormatVector converts a float32 slice to PostgreSQL vector literal format.
// Example: []float32{0.1, 0.2, 0.3} -> "[0.1,0.2,0.3]"
func FormatVector(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}

	var buf strings.Builder
	buf.Grow(len(v)*12 + 2) // Pre-allocate buffer for efficiency
	buf.WriteByte('[')

	for i, f := range v {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}

	buf.WriteByte(']')
	return buf.String()
}

// ParseVector parses a PostgreSQL vector literal into a float32 slice.
// Accepts the format produced by pgvector: "[x1,x2,...,xN]"
// Returns an empty slice for empty vectors ("[]" or "").
func ParseVector(s string) ([]float32, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "[]" {
		return []float32{}, nil
	}
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, fmt.Errorf("pgutils: invalid vector format %q", s)
	}
	inner := s[1 : len(s)-1]
	parts := strings.Split(inner, ",")
	out := make([]float32, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("pgutils: invalid vector element %q: %w", p, err)
		}
		out[i] = float32(f)
	}
	return out, nil
}
