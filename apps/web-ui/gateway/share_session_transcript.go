package main

import (
	"github.com/labstack/echo/v4"
)

// uiShareSessionTranscript renders a read-only transcript of an owner-shared
// session (created by an anonymous end user via an agent-share link). Unknown
// sessions render the empty state (not an error); other failures render the
// error state. There is no composer — this surface is inspect-only.
func (s *Server) uiShareSessionTranscript(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	messages, err := s.memory.GetShareSessionTranscript(ctx, id)
	if err != nil {
		if isMemoryNotFound(err) {
			return s.page(c, pageTitle("Shared session"), ShareSessionTranscriptPage(id, nil, nil))
		}
		return s.page(c, pageTitle("Shared session"), ShareSessionTranscriptPage(id, nil, err))
	}
	return s.page(c, pageTitle("Shared session"), ShareSessionTranscriptPage(id, messages, nil))
}
