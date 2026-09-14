package appletools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// requireString returns the string value for key, which must be present and
// non-blank.
func requireString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string, got %T", key, v)
	}
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("argument %q must not be empty", key)
	}
	return s, nil
}

// optionalString returns the string value for key, or "" when absent or not a
// string.
func optionalString(args map[string]any, key string) string {
	s, ok := args[key].(string)
	if !ok {
		return ""
	}
	return s
}

// optionalBool returns the bool value for key, or def when absent.
func optionalBool(args map[string]any, key string, def bool) bool {
	if b, ok := args[key].(bool); ok {
		return b
	}
	return def
}

// optionalInt returns the integer value for key, or def when absent. MCP args
// decode from JSON, so numbers arrive as float64; json.Number is accepted too.
func optionalInt(args map[string]any, key string, def int) (int, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return def, nil
	}
	switch n := v.(type) {
	case float64:
		if n != float64(int(n)) {
			return 0, fmt.Errorf("argument %q must be an integer, got %v", key, n)
		}
		return int(n), nil
	case int:
		return n, nil
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, fmt.Errorf("argument %q must be an integer, got %q", key, n)
		}
		return int(i), nil
	default:
		return 0, fmt.Errorf("argument %q must be an integer, got %T", key, v)
	}
}
