package graph

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"golang.org/x/text/currency"
	"golang.org/x/text/language"
)

func coerceToNumber(value any) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, fmt.Errorf("empty string cannot be converted to number")
		}
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid number format: %s", v)
		}
		return parsed, nil
	case bool:
		if v {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("cannot convert %T to number", value)
	}
}

func coerceToBoolean(value any) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		trimmed := strings.ToLower(strings.TrimSpace(v))
		switch trimmed {
		case "true", "t", "yes", "y", "1":
			return true, nil
		case "false", "f", "no", "n", "0", "":
			return false, nil
		default:
			return false, fmt.Errorf("invalid boolean format: %s", v)
		}
	case int, int64, int32, float64, float32:
		return fmt.Sprintf("%v", v) != "0", nil
	default:
		return false, fmt.Errorf("cannot convert %T to boolean", value)
	}
}

// coerceToURL validates a URL-typed property: an absolute http(s) URL with a
// host. Surrounding whitespace is trimmed; the trimmed value is stored.
func coerceToURL(value any) (string, error) {
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("expected url string, got %T", value)
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", fmt.Errorf("empty string cannot be converted to url")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid url %q: expected an http or https scheme", trimmed)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("invalid url %q: missing host", trimmed)
	}
	return trimmed, nil
}

// hashTypeLengths maps each hex-digest property type to its digest length in
// characters. These are the types the schema registry accepts and validates.
var hashTypeLengths = map[string]int{
	"md5":    32,
	"sha1":   40,
	"sha256": 64,
	"sha512": 128,
}

// coerceToHash validates a hex digest of the exact length its type declares.
// Surrounding whitespace is trimmed and the value is stored lower-case (hashes
// are case-insensitive); a wrong length or a non-hex character is rejected.
func coerceToHash(value any, kind string) (string, error) {
	n, ok := hashTypeLengths[kind]
	if !ok {
		return "", fmt.Errorf("unknown hash type %q", kind)
	}
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("expected %s hex string, got %T", kind, value)
	}
	trimmed := strings.TrimSpace(s)
	if len(trimmed) != n {
		return "", fmt.Errorf("%s must be %d hex characters, got %d", kind, n, len(trimmed))
	}
	if _, err := hex.DecodeString(trimmed); err != nil {
		return "", fmt.Errorf("%s must be hexadecimal", kind)
	}
	return strings.ToLower(trimmed), nil
}

// coerceToDate validates a date-typed property and stores it in the canonical
// calendar shape YYYY-MM-DD (issue #402). A time component is discarded: a
// `date` is a day, not an instant, so "2026-12-01T00:00:00Z" and "2026-12-01"
// must not coexist for the same property. `datetime` properties are untouched
// and stay RFC 3339.
func coerceToDate(value any) (string, error) {
	switch v := value.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return "", fmt.Errorf("empty string cannot be converted to date")
		}
		if normalised, ok := normaliseDateValue(trimmed); ok {
			return normalised, nil
		}
		return "", fmt.Errorf("invalid date format: %s (expected ISO 8601 or common formats)", v)

	case time.Time:
		return v.Format(dateLayout), nil

	default:
		return "", fmt.Errorf("cannot convert %T to date", value)
	}
}

// coerceDateArrayDefault renders an array-of-date schema default as a []any of
// YYYY-MM-DD strings. The bool is false when any element is not a date, in
// which case the caller keeps the raw default rather than half-converting it.
func coerceDateArrayDefault(value any) ([]any, bool) {
	switch elements := value.(type) {
	case []any:
		out := make([]any, len(elements))
		for i, element := range elements {
			date, err := coerceToDate(element)
			if err != nil {
				return nil, false
			}
			out[i] = date
		}
		return out, true
	case []string:
		out := make([]any, len(elements))
		for i, element := range elements {
			date, err := coerceToDate(element)
			if err != nil {
				return nil, false
			}
			out[i] = date
		}
		return out, true
	default:
		return nil, false
	}
}

// coerceToEmail validates an email-typed property: one bare RFC 5322 address
// with no display name. The address is trimmed and stored lower-case, which is
// how mail providers treat it and what makes duplicate detection useful; the
// local part's theoretical case sensitivity is not preserved.
func coerceToEmail(value any) (string, error) {
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("expected email string, got %T", value)
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", fmt.Errorf("empty string cannot be converted to email")
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid email %q", trimmed)
	}
	if addr.Name != "" || addr.Address != trimmed {
		return "", fmt.Errorf("invalid email %q: expected a bare address, not a display form", trimmed)
	}
	return strings.ToLower(addr.Address), nil
}

// coerceToPhone validates a phone-typed property: an optional leading "+",
// then digits with spaces, dashes, dots or parentheses, 7–15 digits in total
// (the E.164 range). The trimmed input is stored as typed — normalizing to
// E.164 needs a default region, which a schema property does not carry.
func coerceToPhone(value any) (string, error) {
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("expected phone string, got %T", value)
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", fmt.Errorf("empty string cannot be converted to phone")
	}
	digits := 0
	for i, r := range trimmed {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == ' ' || r == '-' || r == '.' || r == '(' || r == ')':
		case r == '+' && i == 0:
		default:
			return "", fmt.Errorf("invalid phone %q: unexpected character %q", trimmed, r)
		}
	}
	if digits < 7 || digits > 15 {
		return "", fmt.Errorf("invalid phone %q: expected 7-15 digits, got %d", trimmed, digits)
	}
	return trimmed, nil
}

// coerceToCountry validates a country-typed property as an ISO 3166-1 region
// (two- or three-letter code) and stores the canonical two-letter upper-case
// form.
func coerceToCountry(value any) (string, error) {
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("expected country string, got %T", value)
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", fmt.Errorf("empty string cannot be converted to country")
	}
	region, err := language.ParseRegion(trimmed)
	if err != nil || !region.IsCountry() {
		return "", fmt.Errorf("invalid country %q: expected an ISO 3166-1 country code", trimmed)
	}
	return region.String(), nil
}

// coerceToCurrency validates a currency-typed property as an ISO 4217 code and
// stores the canonical upper-case form.
func coerceToCurrency(value any) (string, error) {
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("expected currency string, got %T", value)
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", fmt.Errorf("empty string cannot be converted to currency")
	}
	unit, err := currency.ParseISO(strings.ToUpper(trimmed))
	if err != nil {
		return "", fmt.Errorf("invalid currency %q: expected an ISO 4217 code", trimmed)
	}
	return unit.String(), nil
}

// coerceToTime validates a time-typed property as a 24-hour wall-clock time and
// stores it as HH:MM (seconds are dropped, which is the precision a time-of-day
// property is used at).
func coerceToTime(value any) (string, error) {
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("expected time string, got %T", value)
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", fmt.Errorf("empty string cannot be converted to time")
	}
	for _, layout := range []string{"15:04", "15:04:05"} {
		if t, err := time.Parse(layout, trimmed); err == nil {
			return t.Format("15:04"), nil
		}
	}
	return "", fmt.Errorf("invalid time %q: expected HH:MM (24-hour)", trimmed)
}

// checkNumericBounds rejects a number outside the property's declared
// minimum/maximum. The bound keywords were documented but never enforced, so a
// latitude could be stored as 1234 and a rating as 9.
func checkNumericBounds(n float64, def agents.PropertyDef) (float64, error) {
	if def.Minimum != nil && n < *def.Minimum {
		return 0, fmt.Errorf("%v is below the minimum %v", n, *def.Minimum)
	}
	if def.Maximum != nil && n > *def.Maximum {
		return 0, fmt.Errorf("%v is above the maximum %v", n, *def.Maximum)
	}
	return n, nil
}

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ValidationResult struct {
	Valid   bool              `json:"valid"`
	Coerced map[string]any    `json:"coerced,omitempty"`
	Errors  []ValidationError `json:"errors,omitempty"`
}

type PropertyValidator struct{}

func NewPropertyValidator() *PropertyValidator {
	return &PropertyValidator{}
}

func (v *PropertyValidator) ValidateProperties(
	props map[string]any,
	schema *agents.ObjectSchema,
) *ValidationResult {
	if schema == nil || len(schema.Properties) == 0 {
		return &ValidationResult{
			Valid:   true,
			Coerced: props,
			Errors:  []ValidationError{},
		}
	}

	validated := make(map[string]any)
	var errors []ValidationError

	for key, value := range props {
		propDef, hasDef := schema.Properties[key]
		if !hasDef {
			validated[key] = value
			continue
		}

		if value == nil {
			validated[key] = nil
			continue
		}

		coerced, err := coercePropertyValue(value, propDef)
		if err != nil {
			errors = append(errors, ValidationError{
				Field:   key,
				Message: fmt.Sprintf("coercion failed: %v", err),
			})
		} else {
			validated[key] = coerced
		}
	}

	for _, required := range schema.Required {
		if _, ok := validated[required]; !ok {
			errors = append(errors, ValidationError{
				Field:   required,
				Message: "missing required field",
			})
		}
	}

	return &ValidationResult{
		Valid:   len(errors) == 0,
		Coerced: validated,
		Errors:  errors,
	}
}

// coercePropertyValue coerces value to def's declared type and enforces its
// declared enum. Enum matching runs after coercion, so a canonical value such
// as "DE" (from "de") still matches the enum the schema declares. Unknown
// property types pass through unchanged, matching the long-standing leniency
// of the type vocabulary; an enum on a non-primitive type is documentation
// only.
func coercePropertyValue(value any, def agents.PropertyDef) (any, error) {
	coerced, err := coerceScalarValue(value, def)
	if err != nil {
		return nil, err
	}
	return checkEnumValue(coerced, def)
}

// coerceScalarValue coerces value to def's declared type. An array is also
// checked against its declared element schema: when def.Items is set, every
// element is coerced the same way, so a wrong element type is rejected (with
// its index) instead of being stored.
func coerceScalarValue(value any, def agents.PropertyDef) (any, error) {
	switch def.Type {
	case "number":
		n, err := coerceToNumber(value)
		if err != nil {
			return nil, err
		}
		return checkNumericBounds(n, def)
	case "integer":
		n, err := coerceToNumber(value)
		if err != nil {
			return nil, err
		}
		if n != math.Trunc(n) {
			return nil, fmt.Errorf("expected integer, got %v", n)
		}
		return checkNumericBounds(n, def)
	case "string":
		// A `format` names a semantic string format; it validates exactly like
		// the matching type so the JSON-Schema spelling is not a no-op.
		switch strings.ToLower(strings.TrimSpace(def.Format)) {
		case "email":
			return coerceToEmail(value)
		case "phone":
			return coerceToPhone(value)
		case "country":
			return coerceToCountry(value)
		case "currency":
			return coerceToCurrency(value)
		case "time":
			return coerceToTime(value)
		case "date":
			return coerceToDate(value)
		}
		return value, nil
	case "boolean":
		return coerceToBoolean(value)
	case "date":
		return coerceToDate(value)
	case "url":
		return coerceToURL(value)
	case "email":
		return coerceToEmail(value)
	case "phone":
		return coerceToPhone(value)
	case "country":
		return coerceToCountry(value)
	case "currency":
		return coerceToCurrency(value)
	case "time":
		return coerceToTime(value)
	case "md5", "sha1", "sha256", "sha512":
		return coerceToHash(value, def.Type)
	case "array":
		return coerceArrayElements(value, def.Items)
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return nil, fmt.Errorf("expected object, got %T", value)
		}
		return value, nil
	default:
		return value, nil
	}
}

// checkEnumValue rejects a coerced value that is not one of the property's
// declared enum values. An enum on a non-primitive type is documentation only
// (the CLI warns about it), so it does not gate writes here.
func checkEnumValue(value any, def agents.PropertyDef) (any, error) {
	if len(def.Enum) == 0 || !enumAppliesToType(def.Type) {
		return value, nil
	}
	for _, allowed := range def.Enum {
		if enumValueMatches(value, allowed) {
			return value, nil
		}
	}
	allowed := make([]string, 0, len(def.Enum))
	for _, v := range def.Enum {
		allowed = append(allowed, formatEnumValue(v))
	}
	return nil, fmt.Errorf("%v is not one of the allowed values: %s", value, strings.Join(allowed, ", "))
}

// enumAppliesToType reports whether a property type takes a write-time enum.
// The set mirrors the CLI validator's primitive-type rule.
func enumAppliesToType(t string) bool {
	switch t {
	case "string", "number", "integer", "boolean",
		"date", "time", "url", "email", "phone", "country", "currency",
		"md5", "sha1", "sha256", "sha512":
		return true
	default:
		return false
	}
}

// enumValueMatches compares a coerced value with one declared enum entry.
// Numbers may arrive as any Go numeric type or json.Number; strings and
// booleans compare exactly. The reflect fallback covers values a future
// decoder hands over in a different shape.
func enumValueMatches(value, allowed any) bool {
	if s, ok := allowed.(string); ok {
		v, ok := value.(string)
		return ok && v == s
	}
	if b, ok := allowed.(bool); ok {
		v, ok := value.(bool)
		return ok && v == b
	}
	if a, ok := numericValue(allowed); ok {
		v, ok := numericValue(value)
		return ok && a == v
	}
	return reflect.DeepEqual(value, allowed)
}

// numericValue converts the numeric shapes a decoder may produce. A string is
// deliberately not parsed: a numeric enum must not match the string "1".
func numericValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// formatEnumValue renders one allowed value for an error message. Strings are
// quoted so an empty or whitespace value stays visible.
func formatEnumValue(v any) string {
	if s, ok := v.(string); ok {
		return strconv.Quote(s)
	}
	return fmt.Sprintf("%v", v)
}

// coerceArrayElements checks that value is a list and, when the schema declares
// an element schema, coerces every element to it. The first failing element
// names its index, so a rejected document points at the element to fix.
func coerceArrayElements(value any, items *agents.PropertyDef) ([]any, error) {
	vals, ok := value.([]any)
	if !ok {
		// Legacy storage: a list was previously kept as its serialized JSON
		// string. Accept one so converting a property from string to array does
		// not strand existing rows, and so a writer that serialized a list still
		// lands a real array.
		if s, isString := value.(string); isString {
			trimmed := strings.TrimSpace(s)
			if strings.HasPrefix(trimmed, "[") {
				var parsed []any
				if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
					vals, ok = parsed, true
				}
			}
		}
	}
	if !ok {
		return nil, fmt.Errorf("expected array, got %T", value)
	}
	// An items schema with no declared type constrains nothing (same rule as an
	// unknown top-level type), so the elements are stored as supplied.
	if items == nil || items.Type == "" {
		return vals, nil
	}
	out := make([]any, len(vals))
	for i, el := range vals {
		coerced, err := coerceElementValue(el, items)
		if err != nil {
			return nil, fmt.Errorf("element %d: %v", i, err)
		}
		out[i] = coerced
	}
	return out, nil
}

// coerceElementValue coerces one array element to the schema's declared element
// type and enforces the element schema's enum, if it declares one.
func coerceElementValue(value any, def *agents.PropertyDef) (any, error) {
	coerced, err := coerceElementScalar(value, def)
	if err != nil {
		return nil, err
	}
	return checkEnumValue(coerced, *def)
}

// coerceElementScalar coerces one array element to the schema's declared
// element type. Unlike a top-level property — whose string and unknown types
// are stored as supplied for backward compatibility — a declared element type
// is enforced: a string array rejects a number and an integer array rejects a
// fraction.
func coerceElementScalar(value any, def *agents.PropertyDef) (any, error) {
	switch def.Type {
	case "string":
		s, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("expected string, got %T", value)
		}
		// A declared `format` is a contract on the element too: route it through
		// the scalar path so `items: {type: string, format: email}` validates.
		if strings.TrimSpace(def.Format) != "" {
			return coerceScalarValue(value, *def)
		}
		return s, nil
	case "integer":
		n, err := coerceToNumber(value)
		if err != nil {
			return nil, err
		}
		if n != math.Trunc(n) {
			return nil, fmt.Errorf("expected integer, got %v", n)
		}
		// Bounds declared on the element schema apply to every element, exactly
		// as they do for a scalar integer property.
		return checkNumericBounds(n, *def)
	default:
		return coerceScalarValue(value, *def)
	}
}

// ValidateAndCoerceProperties validates and coerces properties according to schema.
// This is exported for use by migration tools.
func ValidateAndCoerceProperties(
	props map[string]any,
	schema agents.ObjectSchema,
) (map[string]any, error) {
	return validateProperties(props, schema)
}

func validateProperties(
	props map[string]any,
	schema agents.ObjectSchema,
) (map[string]any, error) {
	if len(schema.Properties) == 0 {
		return props, nil
	}

	validated := make(map[string]any)
	var validationErrors []string

	for key, value := range props {
		propDef, hasDef := schema.Properties[key]
		if !hasDef {
			// Unknown properties are passed through as-is. The schema defines
			// known properties for type coercion and validation, but does not
			// act as an allowlist — users may store arbitrary metadata keys.
			validated[key] = value
			continue
		}

		if value == nil {
			validated[key] = nil
			continue
		}

		coerced, err := coercePropertyValue(value, propDef)
		if err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("%s: %v", key, err))
		} else {
			validated[key] = coerced
		}
	}

	for _, required := range schema.Required {
		if _, ok := validated[required]; !ok {
			validationErrors = append(validationErrors, fmt.Sprintf("missing required field: %s", required))
		}
	}

	if len(validationErrors) > 0 {
		return nil, fmt.Errorf("property validation failed: %s", strings.Join(validationErrors, "; "))
	}

	return validated, nil
}

// applySchemaDefaults fills schema-declared default values for properties
// absent from the supplied map, then derives a confidence value from the
// "source" property when the schema declares both "confidence" and "source"
// (e.g. Note). Confidence decay/gain was previously maintained by dedicated
// note tools; this centralizes it in the schema-driven create path.
func applySchemaDefaults(props map[string]any, schema agents.ObjectSchema) {
	if props == nil || len(schema.Properties) == 0 {
		return
	}

	for name, def := range schema.Properties {
		if _, present := props[name]; present {
			continue
		}
		if def.Default != nil {
			// A schema default is a write just like any other value. Narrow to
			// dates so a date default cannot escape coercion as RFC 3339
			// (#402) while every other default keeps its existing behaviour.
			if isDateProperty(def) {
				if coerced, err := coerceToDate(def.Default); err == nil {
					props[name] = coerced
					continue
				}
			}
			// An array-of-date default would otherwise store its elements in
			// whatever shape the pack author wrote.
			if strings.EqualFold(strings.TrimSpace(def.Type), "array") && def.Items != nil && isDateProperty(*def.Items) {
				if coerced, ok := coerceDateArrayDefault(def.Default); ok {
					props[name] = coerced
					continue
				}
			}
			props[name] = def.Default
		}
	}

	if _, present := props["confidence"]; present {
		return
	}
	if _, hasConf := schema.Properties["confidence"]; !hasConf {
		return
	}
	srcDef, hasSrc := schema.Properties["source"]
	if !hasSrc {
		return
	}
	src, _ := props["source"].(string)
	if src == "" {
		if s, ok := srcDef.Default.(string); ok {
			src = s
		}
	}
	confidence := 0.7
	switch src {
	case "explicit":
		confidence = 1.0
	case "corrected":
		confidence = 0.9
	}
	props["confidence"] = confidence
}

// validatePatchProperties validates only the properties being set or added by a patch request
// against the current schema. Unlike validateProperties it does NOT enforce required fields,
// because the object already exists and may have been created under an older schema version
// where those fields were not required (or didn't exist yet). Only keys present in patchProps
// (i.e. the delta) are checked; keys already stored on the object that are not touched by the
// patch are intentionally ignored.
func validatePatchProperties(
	patchProps map[string]any,
	schema agents.ObjectSchema,
) (map[string]any, error) {
	return validatePatchDelta(patchProps, nil, schema)
}

// validateRelationshipPatchProperties validates a relationship patch's property
// delta and enforces required fields against the merged result (existing
// properties plus the patch delta).
//
// Unlike validatePatchProperties (the object patch path), required fields ARE
// enforced here against mergedProps, so a patch can neither clear nor omit a
// required relationship property and still succeed (issue #989). Type coercion
// remains delta-only: keys already stored on the relationship that are not
// touched by the patch are left alone, because they may predate the schema
// version that declared their type.
//
// Unknown keys are passed through as-is (the schema is not an allowlist).
func validateRelationshipPatchProperties(
	patchProps map[string]any,
	mergedProps map[string]any,
	schema agents.ObjectSchema,
) (map[string]any, error) {
	return validatePatchDelta(patchProps, mergedProps, schema)
}

// validatePatchDelta type-coerces the patch delta keys. When mergedProps is
// non-nil it also enforces required fields against that merged result, so a
// patch cannot leave a required property absent (whether omitted or cleared).
func validatePatchDelta(
	patchProps map[string]any,
	mergedProps map[string]any,
	schema agents.ObjectSchema,
) (map[string]any, error) {
	if len(schema.Properties) == 0 && len(schema.Required) == 0 {
		return patchProps, nil
	}

	validated := make(map[string]any)
	var validationErrors []string

	for key, value := range patchProps {
		// null means "delete this property" — allowed at the delta level. When
		// mergedProps is non-nil and the deleted key is required, the merged
		// required check below rejects the patch.
		if value == nil {
			validated[key] = nil
			continue
		}

		propDef, hasDef := schema.Properties[key]
		if !hasDef {
			// Unknown properties are passed through as-is (same policy as validateProperties).
			validated[key] = value
			continue
		}

		coerced, err := coercePropertyValue(value, propDef)
		if err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("%s: %v", key, err))
		} else {
			validated[key] = coerced
		}
	}

	if mergedProps != nil {
		for _, required := range schema.Required {
			if v, ok := mergedProps[required]; !ok || v == nil {
				validationErrors = append(validationErrors, fmt.Sprintf("missing required field: %s", required))
			}
		}
	}

	if len(validationErrors) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(validationErrors, "; "))
	}

	return validated, nil
}

// relationshipSchemaFor resolves the property schema for an edge of relType
// between srcType and dstType from the project's relationship schema registry.
//
// Lookup is by the same (name, source type, target type) identity that
// domain/schemas keys compiled types on. The most specific declaration wins:
//
//  1. (name, source, target) — the exact pair;
//  2. (name, source, any)   — a declaration that pins the source end only;
//  3. (name, any, target)   — pins the target end only;
//  4. (name, any, any)      — the name-wide default.
//
// Both the raw and the normalised relationship type are tried at each step, so a
// caller that passes an un-normalised type (`KNOWS`) still finds the stored
// `knows` schema (#176) and hand-built maps keyed by the literal type keep
// working. Without this, same-named relationship types from different packs
// validated against whichever definition was read last (#322).
//
// Both relationship writes resolve through this one helper: create validates
// the caller's properties against it, and patch validates its delta against the
// schema for the stored edge's own endpoints (#361).
func relationshipSchemaFor(
	schemas map[string]agents.RelationshipSchema,
	relType string,
	srcType string,
	dstType string,
) (agents.RelationshipSchema, bool) {
	names := []string{relType}
	if normalized := normalizeRelationType(relType); normalized != relType {
		names = append(names, normalized)
	}

	for _, endpoints := range [][2]string{
		{srcType, dstType},
		{srcType, ""},
		{"", dstType},
		{"", ""},
	} {
		for _, name := range names {
			if schema, ok := schemas[relationshipSchemaKey(name, endpoints[0], endpoints[1])]; ok {
				return schema, true
			}
		}
	}

	return agents.RelationshipSchema{}, false
}

// validateRelationshipProperties validates a relationship create against the
// installed schema and returns the coerced property map to store. It checks:
// type is allowed, source/destination object types satisfy fromTypes/toTypes
// constraints, and relationship properties are valid. The properties pass
// through unchanged when the schema has no relationship schemas (nil map) or
// no constraints for the type, meaning no schema is installed for it.
//
// The schema is resolved by (name, source type, target type), the identity the
// rest of the system uses: a project may declare `owns` Person→Vehicle
// (share_percent) in one pack and `owns` Person→Account (holder_role) in
// another, and an edge must be validated against the schema for its own
// endpoints rather than whichever pack happened to be read last (#322).
//
// The coerced map matters for element types as much as scalar ones: a create
// that stores the caller's raw map would keep ["3"] as a string element even
// though the array declared numeric items and validation passed by coercing it.
func validateRelationshipProperties(
	relType string,
	srcObjType string,
	dstObjType string,
	props map[string]any,
	schemas *ExtractionSchemas,
) (map[string]any, error) {
	if schemas == nil || schemas.RelationshipSchemas == nil {
		return props, nil
	}

	relSchema, ok := relationshipSchemaFor(schemas.RelationshipSchemas, relType, srcObjType, dstObjType)
	if !ok {
		// Unknown relationship types are allowed — the schema defines constraints
		// for known types but does not act as an allowlist. Users may create
		// relationships with any type name, including domain-specific ones like
		// requires, calls, has_step, etc.
		return props, nil
	}

	// SourceTypes and TargetTypes in the schema are informational — they document
	// the intended source/target object types but do not act as hard enforcement
	// gates. Users may connect any object types via a known relationship type
	// (e.g. Scenario --contains--> Action) without needing every combination
	// pre-registered in the schema.

	if len(relSchema.Properties) > 0 || len(relSchema.Required) > 0 {
		objSchema := agents.ObjectSchema{
			Properties: relSchema.Properties,
			Required:   relSchema.Required,
		}
		validated, err := validateProperties(props, objSchema)
		if err != nil {
			return nil, err
		}
		return validated, nil
	}

	return props, nil
}
