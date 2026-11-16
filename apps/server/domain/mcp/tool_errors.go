package mcp

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/authz"
)

// Error classes for a tools/call failure, alongside toolArgErrorf in args.go.
//
// The ExecuteTool channel relays only what PublicMessage returns, and the
// JSON-RPC code comes from jsonRPCErrorCodeFor, so an error constructed here
// decides both what the caller sees and which JSON-RPC code it carries (#668).
// Anything that stays a plain error is reported as an internal fault — correct
// for a driver message, wrong for a refusal the caller can act on.

// toolForbiddenf reports a tool refused by an authorization or scope decision
// (share-instance allowlist, superadmin grant, agent object-type scope). The
// text names the boundary; it leaks no policy state beyond the tool or type the
// caller already named. Mapped to -32002 by jsonRPCErrorCodeFor.
func toolForbiddenf(format string, args ...any) error {
	return apperror.New(http.StatusForbidden, "forbidden", fmt.Errorf(format, args...).Error())
}

// toolNotFoundf reports a tool name the catalog does not register. Unknown tool
// names are caller-addressable — a model may still emit a retired name — so the
// "tool not found" text is public and maps to JSON-RPC not-found. The e2e skip
// helper keys on that phrase (e2e/tests/api/mcp_new_tools_test.go).
func toolNotFoundf(format string, args ...any) error {
	return apperror.New(http.StatusNotFound, "tool_not_found", fmt.Errorf(format, args...).Error())
}

// toolAuthorizationError adapts authz.AuthorizeTool's typed refusal to the
// tools/call channel. The authz package carries no transport or app-error
// imports, so the conversion lives at this seam: a *authz.Denial becomes a 403
// app error whose message is the denial text, and any other error passes
// through unchanged (an authorization *lookup* failure is genuinely internal).
func toolAuthorizationError(err error) error {
	denial, ok := errors.AsType[*authz.Denial](err)
	if !ok {
		return err
	}
	return toolForbiddenf("%s", denial.Message)
}

// entityKeyNotFound is the typed "no object in this project carries that key"
// answer from resolveEntityIDByKey. It exists so a caller can tell the one
// lookup failure that is its own input problem (report the key and the fuzzy
// suggestions) from a store failure (stay internal).
type entityKeyNotFound struct {
	message string
}

func (e *entityKeyNotFound) Error() string { return e.message }

// invalidEntityRefError renders a failed entity_id / start_entity_id
// resolution. A key miss is the caller's to fix, so it becomes a validation
// error carrying the resolver's message (with fuzzy suggestions). Any other
// lookup failure is a store fault: folding it into an invalid-parameter message
// would blame the caller for an outage and print "not found" for a DB error.
func invalidEntityRefError(label, idStr string, keyErr error) error {
	notFound, ok := errors.AsType[*entityKeyNotFound](keyErr)
	if !ok {
		return apperror.NewInternal("An internal error occurred",
			fmt.Errorf("resolve %s %q: %w", label, idStr, keyErr))
	}
	return toolArgErrorf("invalid %s: %q is not a UUID and key lookup failed: %s", label, idStr, notFound.message)
}
