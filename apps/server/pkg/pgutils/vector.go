// Package pgutils provides PostgreSQL utility functions for the Go server.
package pgutils

import (
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
