package main

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// maxCaptureUploadSize bounds one shared file. Photos leave the iOS share
// extension downsampled to a couple of megabytes; this headroom is for scanned
// PDFs and long screenshots, and it is deliberately its own number rather than
// documents' 10 MB cap because a capture is the one upload a phone produces
// without the user choosing a file.
const maxCaptureUploadSize = 25 * 1024 * 1024

// captureBody is the device-facing share payload. The iOS share extension posts
// what it collected — the link, the page title the system gave it, the shared
// text, and the user's comment — and the gateway frames the prose. A client
// never composes the message the remember agent reads.
type captureBody struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Text    string `json:"text"`
	Comment string `json:"comment"`
}

// frameCapture turns a share payload into the message memory remembers and the
// origin it records on the document.
//
// The comment comes first because it is the part the user wrote and the part
// they will search for later; the shared text follows verbatim; the link closes
// the message as `title` over the URL, which reads well in a document and keeps
// the title inside the indexed text. A non-http(s) "url" is not treated as an
// origin (it is not one memory can point at), so it is ignored rather than
// recorded as a bogus source.
//
// ok is false when the payload carries nothing to remember.
func frameCapture(body captureBody) (message, sourceURL string, ok bool) {
	comment := strings.TrimSpace(body.Comment)
	text := strings.TrimSpace(body.Text)
	title := strings.TrimSpace(body.Title)
	link := strings.TrimSpace(body.URL)
	if !strings.HasPrefix(link, "http://") && !strings.HasPrefix(link, "https://") {
		link = ""
	}

	parts := make([]string, 0, 3)
	if comment != "" {
		parts = append(parts, comment)
	}
	if text != "" && text != comment {
		parts = append(parts, text)
	}
	if link != "" {
		if title != "" {
			parts = append(parts, title+"\n"+link)
		} else {
			parts = append(parts, link)
		}
	}

	message = strings.Join(parts, "\n\n")
	return message, link, message != ""
}

// rememberCapture handles POST /api/projects/:projectId/remember — a shared
// link, selection or note. It answers with memory's async body (202 with the
// run id) so the share extension can dismiss instead of holding the run open.
func (s *Server) rememberCapture(c echo.Context) error {
	projectID := c.Param("projectId")
	if projectID == "" {
		return captureFailure(c, http.StatusBadRequest, "invalid_project", "projectId is required")
	}

	var body captureBody
	if err := c.Bind(&body); err != nil {
		return captureFailure(c, http.StatusBadRequest, "invalid_request", "the share payload could not be read")
	}

	message, sourceURL, ok := frameCapture(body)
	if !ok {
		return captureFailure(c, http.StatusBadRequest, "empty_capture",
			"nothing to remember: share a link, some text, or a comment")
	}

	status, raw, err := s.memory.RememberCapture(c.Request().Context(), projectID, captureRequest{
		Message:   message,
		SourceURL: sourceURL,
	})
	if err != nil {
		captureError(err)
		return captureFailure(c, http.StatusBadGateway, "memory_unavailable",
			"the memory service is unreachable")
	}
	return writeCaptureResponse(c, status, raw)
}

// rememberFileCapture handles POST /api/projects/:projectId/remember/file — a
// shared photo, scanned document or audio memo. The file bytes go to memory
// unchanged (its parser owns OCR, PDF text and transcription); the comment
// rides along as the remember guide, which is also the document's fallback text
// when the parser finds none.
func (s *Server) rememberFileCapture(c echo.Context) error {
	projectID := c.Param("projectId")
	if projectID == "" {
		return captureFailure(c, http.StatusBadRequest, "invalid_project", "projectId is required")
	}

	fh, err := c.FormFile("file")
	if err != nil {
		return captureFailure(c, http.StatusBadRequest, "missing_file", "a shared file is required")
	}
	if fh.Size > maxCaptureUploadSize {
		return captureFailure(c, http.StatusRequestEntityTooLarge, "file_too_large",
			"the shared file is larger than the 25 MB capture limit")
	}
	src, err := fh.Open()
	if err != nil {
		return captureFailure(c, http.StatusBadRequest, "unreadable_file",
			"the shared file could not be read")
	}
	defer func() { _ = src.Close() }()

	comment := strings.TrimSpace(c.FormValue("comment"))
	status, raw, err := s.memory.RememberCaptureFile(
		c.Request().Context(), projectID, fh.Filename, src, comment)
	if err != nil {
		captureError(err)
		return captureFailure(c, http.StatusBadGateway, "memory_unavailable",
			"the memory service is unreachable")
	}
	return writeCaptureResponse(c, status, raw)
}

// writeCaptureResponse hands memory's own JSON back to the device: `{run_id,
// status}` on success, `{error: {code, message}}` on refusal. The iOS client
// shows memory's reason verbatim (unsupported_media_type, no_content,
// no_provider), so the gateway must not replace it with its own wording.
func writeCaptureResponse(c echo.Context, status int, raw []byte) error {
	if len(raw) == 0 {
		if status == 0 {
			status = http.StatusOK
		}
		return c.NoContent(status)
	}
	return c.Blob(status, echo.MIMEApplicationJSONCharsetUTF8, raw)
}

// captureFailure answers in memory's error envelope so a device client parses
// one shape whether the refusal came from the gateway or from memory.
func captureFailure(c echo.Context, status int, code, message string) error {
	return c.JSON(status, map[string]any{
		"error": map[string]any{"code": code, "message": message},
	})
}
