package graph

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseRelativeTime parses relative time shorthands like "90d", "12h", "6M"
// and returns the corresponding absolute time (time.Now().UTC() minus the duration).
// Supported units: d (days), h (hours), M (months).
// Returns the parsed time and nil error on success, or zero time and error on failure.
func parseRelativeTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return time.Time{}, fmt.Errorf("invalid relative time: %q", s)
	}

	unit := string(s[len(s)-1])
	numStr := s[:len(s)-1]

	n, err := strconv.Atoi(numStr)
	if err != nil || n <= 0 {
		return time.Time{}, fmt.Errorf("invalid relative time: %q", s)
	}

	now := time.Now().UTC()
	switch unit {
	case "h":
		return now.Add(-time.Duration(n) * time.Hour), nil
	case "d":
		return now.Add(-time.Duration(n) * 24 * time.Hour), nil
	case "M":
		return now.AddDate(0, -n, 0), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported time unit %q in %q (use h, d, M)", unit, s)
	}
}

// isRelativeTime returns true if the string looks like a relative time shorthand.
func isRelativeTime(s string) bool {
	if len(s) < 2 {
		return false
	}
	unit := string(s[len(s)-1])
	if unit != "h" && unit != "d" && unit != "M" {
		return false
	}
	_, err := strconv.Atoi(s[:len(s)-1])
	return err == nil
}
