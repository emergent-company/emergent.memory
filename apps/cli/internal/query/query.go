// Package query provides parsing and handling of query flags (filter, sort, pagination).
package query

import (
	"fmt"
	"strconv"
	"strings"
)

// Filter represents a single filter condition.
type Filter struct {
	Key      string
	Value    string
	Operator string // "eq", "ne", "gt", "lt", "contains", etc.
}

// Sort represents a sort directive.
type Sort struct {
	Field string
	Order string // "asc" or "desc"
}

// Pagination represents pagination parameters.
type Pagination struct {
	Limit  int
	Offset int
	Cursor string
}

// ParseFilters parses a filter string in format "key1=value1,key2=value2".
// Also supports operators: key>value, key<value, key!=value, key~value (contains)
func ParseFilters(filterStr string) ([]Filter, error) {
	if filterStr == "" {
		return nil, nil
	}

	parts := strings.Split(filterStr, ",")
	filters := make([]Filter, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check for operators
		var key, value, operator string

		if strings.Contains(part, "!=") {
			subparts := strings.SplitN(part, "!=", 2)
			key, value, operator = subparts[0], subparts[1], "ne"
		} else if strings.Contains(part, ">=") {
			subparts := strings.SplitN(part, ">=", 2)
			key, value, operator = subparts[0], subparts[1], "gte"
		} else if strings.Contains(part, "<=") {
			subparts := strings.SplitN(part, "<=", 2)
			key, value, operator = subparts[0], subparts[1], "lte"
		} else if strings.Contains(part, ">") {
			subparts := strings.SplitN(part, ">", 2)
			key, value, operator = subparts[0], subparts[1], "gt"
		} else if strings.Contains(part, "<") {
			subparts := strings.SplitN(part, "<", 2)
			key, value, operator = subparts[0], subparts[1], "lt"
		} else if strings.Contains(part, "~") {
			subparts := strings.SplitN(part, "~", 2)
			key, value, operator = subparts[0], subparts[1], "contains"
		} else if strings.Contains(part, "=") {
			subparts := strings.SplitN(part, "=", 2)
			key, value, operator = subparts[0], subparts[1], "eq"
		} else {
			return nil, fmt.Errorf("invalid filter format: %s (expected key=value or key<op>value)", part)
		}

		filters = append(filters, Filter{
			Key:      strings.TrimSpace(key),
			Value:    strings.TrimSpace(value),
			Operator: operator,
		})
	}

	return filters, nil
}

// ParseSort parses a sort string in format "field:order" or "field" (defaults to asc).
// Multiple sorts: "field1:asc,field2:desc"
func ParseSort(sortStr string) ([]Sort, error) {
	if sortStr == "" {
		return nil, nil
	}

	parts := strings.Split(sortStr, ",")
	sorts := make([]Sort, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		var field, order string
		if strings.Contains(part, ":") {
			subparts := strings.SplitN(part, ":", 2)
			field = strings.TrimSpace(subparts[0])
			order = strings.ToLower(strings.TrimSpace(subparts[1]))
		} else {
			field = part
			order = "asc"
		}

		if order != "asc" && order != "desc" {
			return nil, fmt.Errorf("invalid sort order '%s' for field '%s' (must be 'asc' or 'desc')", order, field)
		}

		sorts = append(sorts, Sort{
			Field: field,
			Order: order,
		})
	}

	return sorts, nil
}

// ParsePagination parses pagination parameters.
func ParsePagination(limit, offset int, cursor string) Pagination {
	if limit <= 0 {
		limit = 50 // default
	}
	if offset < 0 {
		offset = 0
	}

	return Pagination{
		Limit:  limit,
		Offset: offset,
		Cursor: cursor,
	}
}

// ApplyFilters applies filters to a map of values.
// Returns true if the item matches all filters.
func ApplyFilters(item map[string]interface{}, filters []Filter) bool {
	for _, f := range filters {
		value, exists := item[f.Key]
		if !exists {
			return false
		}

		if !matchFilter(value, f.Value, f.Operator) {
			return false
		}
	}
	return true
}

// matchFilter checks if a value matches a filter condition.
func matchFilter(itemValue interface{}, filterValue string, operator string) bool {
	switch operator {
	case "eq":
		return fmt.Sprintf("%v", itemValue) == filterValue
	case "ne":
		return fmt.Sprintf("%v", itemValue) != filterValue
	case "contains":
		return strings.Contains(strings.ToLower(fmt.Sprintf("%v", itemValue)), strings.ToLower(filterValue))
	case "gt":
		return compareValues(itemValue, filterValue) > 0
	case "lt":
		return compareValues(itemValue, filterValue) < 0
	case "gte":
		return compareValues(itemValue, filterValue) >= 0
	case "lte":
		return compareValues(itemValue, filterValue) <= 0
	default:
		return false
	}
}

// compareValues compares two values (attempts numeric comparison first).
func compareValues(a interface{}, b string) int {
	// Try numeric comparison
	aNum, aErr := toFloat(a)
	bNum, bErr := strconv.ParseFloat(b, 64)

	if aErr == nil && bErr == nil {
		if aNum < bNum {
			return -1
		} else if aNum > bNum {
			return 1
		}
		return 0
	}

	// String comparison
	aStr := fmt.Sprintf("%v", a)
	return strings.Compare(aStr, b)
}

// toFloat converts an interface{} to float64.
func toFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case float32:
		return float64(val), nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case string:
		return strconv.ParseFloat(val, 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", v)
	}
}

// FormatPaginationInfo formats pagination info message.
func FormatPaginationInfo(showing, total int, hasMore bool) string {
	if showing == 0 {
		return "No results"
	}

	if hasMore {
		return fmt.Sprintf("Showing %d results (more available)", showing)
	}

	if showing == total {
		return fmt.Sprintf("Showing all %d results", total)
	}

	return fmt.Sprintf("Showing %d of %d results", showing, total)
}
