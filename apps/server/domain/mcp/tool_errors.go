package mcp

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/authz"
	"github.com/emergent-company/emergent.memory/pkg/logger"
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

// notFoundf reports a resource the caller named that does not exist — a tool
// name, a branch, a schema, a skill, an entity key. A miss is caller-addressable
// (the caller can correct the reference, and the message says how), so the text
// is public and jsonRPCErrorCodeFor maps the 404 to JSON-RPC not-found instead of
// reporting a server fault. Reporting it as internal tells the caller to retry a
// lookup that will fail the same way forever.
func notFoundf(code, format string, args ...any) error {
	return apperror.New(http.StatusNotFound, code, fmt.Errorf(format, args...).Error())
}

// toolNotFoundf reports a tool name the catalog does not register. Unknown tool
// names are caller-addressable — a model may still emit a retired name — so the
// "tool not found" text is public and maps to JSON-RPC not-found. The e2e skip
// helper keys on that phrase (e2e/tests/api/mcp_new_tools_test.go).
func toolNotFoundf(format string, args ...any) error {
	return notFoundf("tool_not_found", format, args...)
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

// logToolFailure records the internal cause of a per-item failure. A batch
// result carries only the public text (see the call sites), so this is where the
// driver detail survives; a Service built without a logger (tests) is tolerated.
//
// A caller-addressable miss is expected traffic, not a fault, so it is logged at
// debug; anything else — a store or driver failure — is a warning.
func (s *Service) logToolFailure(msg string, err error, args ...any) {
	if s.log == nil {
		return
	}
	attrs := append(args, logger.Error(err))
	if refErr, ok := errors.AsType[*apperror.Error](err); ok && refErr.HTTPStatus < http.StatusInternalServerError {
		s.log.Debug(msg, attrs...)
		return
	}
	s.log.Warn(msg, attrs...)
}

// invalidEntityRefError renders a failed entity_id / start_entity_id
// resolution. A key miss is the caller's to fix, so it becomes a validation
// error carrying the resolver's message (with fuzzy suggestions). Any other
// lookup failure is a store fault: folding it into an invalid-parameter message
// would blame the caller for an outage and print "not found" for a DB error.
//
// The miss/fault split is the resolver's 404: resolveEntityIDByKey returns a
// *not-found* app error for a key it could not find and an internal app error for
// a store failure, so this helper classifies by status rather than by string.
func invalidEntityRefError(label, idStr string, keyErr error) error {
	refErr, ok := errors.AsType[*apperror.Error](keyErr)
	if !ok || refErr.HTTPStatus != http.StatusNotFound {
		return apperror.NewInternal("An internal error occurred",
			fmt.Errorf("resolve %s %q: %w", label, idStr, keyErr))
	}
	return toolArgErrorf("invalid %s: %q is not a UUID and key lookup failed: %s", label, idStr, refErr.Message)
}
