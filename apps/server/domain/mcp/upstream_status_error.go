package mcp

import (
	"errors"
	"fmt"
)

// upstreamStatusError reports a non-200 response from a third-party HTTP API a
// tool calls (Brave, Reddit). Error() carries the response body so the failure
// reaches the log with its detail; nothing else in this package should render
// that body into a tool result — it is upstream content and, for a search API,
// can echo the subscription token.
type upstreamStatusError struct {
	Service string
	Status  int
	Detail  string
}

func (e *upstreamStatusError) Error() string {
	return fmt.Sprintf("%s returned status %d: %s", e.Service, e.Status, e.Detail)
}

// upstreamFailureText is the tool result's text for a failed upstream call. It
// never returns the error's own text: that is a transport error (carrying the
// resolved address) or the upstream body (which can carry credentials). Only the
// upstream status crosses over, because it is part of the API's contract and
// 401/403 tells the operator the key is the problem.
func upstreamFailureText(service string, err error) string {
	var statusErr *upstreamStatusError
	if errors.As(err, &statusErr) {
		return fmt.Sprintf("%s rejected the request (upstream status %d).", service, statusErr.Status)
	}
	return fmt.Sprintf("%s request failed. The failure was logged; retrying later may succeed.", service)
}
