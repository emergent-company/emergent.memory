package mcp

import (
	"fmt"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// toolArgErrorf reports an argument the caller can correct. It returns a
// validation app error, not a plain one, because the ExecuteTool channel has to
// tell the two classes apart (issue #668):
//
//   - handleToolsCall renders apperror.PublicMessage for that channel. A domain
//     failure keeps its client-facing message; anything that is not an app
//     error is collapsed to "An internal error occurred" so a driver message
//     cannot leak. A tool's own argument validation must therefore *be* an app
//     error, or the caller would lose the one detail it can act on.
//   - jsonRPCErrorCodeFor maps it to -32602 (invalid params) instead of -32603,
//     so a mistyped argument is not reported as a server fault.
//
// %w is formatted into the message rather than attached as an internal cause:
// the wrapped error here describes the caller's own input (a UUID parse, a JSON
// decode), and that text is the part the caller needs.
func toolArgErrorf(format string, args ...any) error {
	return apperror.NewValidation(fmt.Errorf(format, args...).Error())
}

// boolToolArg reads an optional boolean tool argument. An absent or null
// argument takes the zero value; a value that is present but not a JSON boolean
// is refused, never silently coerced — a caller that sent "true" as a string
// used to get the false branch with no way to notice (#24).
func boolToolArg(args map[string]any, name string) (bool, error) {
	ptr, err := boolToolArgPtr(args, name)
	if err != nil || ptr == nil {
		return false, err
	}
	return *ptr, nil
}

// boolToolArgPtr is boolToolArg for handlers that distinguish "absent" from
// "explicit false" (e.g. a PATCH-style update where absence means keep).
func boolToolArgPtr(args map[string]any, name string) (*bool, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return nil, nil
	}
	v, ok := raw.(bool)
	if !ok {
		return nil, toolArgErrorf("%s must be a boolean, got %T", name, raw)
	}
	return &v, nil
}

// validateBooleanArgs enforces the boolean properties a tool declares in its
// InputSchema. It is the catalogue-wide pass for the #24 argument class: every
// handler used to read args[name].(bool), so a string-typed value silently
// became false on any of them. Doing it once at the dispatch boundary means a
// new boolean argument is covered when it is declared, not only when someone
// remembers to use the strict helper.
//
// Only top-level boolean properties are checked; nested objects/arrays are the
// schema owner's concern. Absent and null values keep their documented defaults.
func validateBooleanArgs(def *ToolDefinition, args map[string]any) error {
	if def == nil || len(args) == 0 {
		return nil
	}
	for name, prop := range def.InputSchema.Properties {
		if prop.Type != "boolean" {
			continue
		}
		raw, ok := args[name]
		if !ok || raw == nil {
			continue
		}
		if _, ok := raw.(bool); !ok {
			return toolArgErrorf("%s must be a boolean, got %T", name, raw)
		}
	}
	return nil
}
