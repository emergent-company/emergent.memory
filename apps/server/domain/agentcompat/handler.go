package agentcompat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Handler exposes the OpenAI-compatible endpoints.
type Handler struct {
	svc *Service
}

// NewHandler creates a Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ─── POST /v1/chat/completions ────────────────────────────────────────────

// ChatCompletion handles POST /v1/chat/completions.
// @Summary      OpenAI-compatible chat completions
// @Description  Invoke a Memory agent using the OpenAI Chat Completions API format. Supports streaming (SSE) and client-supplied tools via suspend/resume.
// @Tags         agentcompat
// @Accept       json
// @Produce      json
// @Param        request  body      ChatCompletionRequest  true  "Chat completion request"
// @Success      200      {object}  ChatCompletionResponse
// @Failure      400      {object}  APIError
// @Failure      401      {object}  APIError
// @Router       /v1/chat/completions [post]
func (h *Handler) ChatCompletion(c echo.Context) error {
	var req ChatCompletionRequest
	if err := c.Bind(&req); err != nil {
		return h.apiError(c, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("could not parse request body: %v", err))
	}

	if req.Model == "" {
		return h.apiError(c, http.StatusBadRequest, "invalid_request_error",
			"model is required")
	}
	if len(req.Messages) == 0 {
		return h.apiError(c, http.StatusBadRequest, "invalid_request_error",
			"messages must not be empty")
	}

	user := auth.GetUser(c)
	if user == nil {
		return h.apiError(c, http.StatusUnauthorized, "authentication_error", "unauthorized")
	}

	result, err := h.svc.HandleChatCompletion(c.Request().Context(), &req, user)
	if err != nil {
		return h.apiError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}

	if req.Stream {
		return h.writeStream(c, result, req.Model, req.StreamOptions)
	}
	return h.writeResponse(c, result)
}

// ─── GET /v1/models ───────────────────────────────────────────────────────

// ListModels handles GET /v1/models.
// @Summary      List available agent models
// @Description  Returns all agent definitions in the project as OpenAI-compatible model objects.
// @Tags         agentcompat
// @Produce      json
// @Success      200  {object}  ModelList
// @Failure      401  {object}  APIError
// @Router       /v1/models [get]
func (h *Handler) ListModels(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return h.apiError(c, http.StatusUnauthorized, "authentication_error", "unauthorized")
	}

	list, err := h.svc.HandleModelList(c.Request().Context(), user)
	if err != nil {
		return h.apiError(c, http.StatusInternalServerError, "server_error", err.Error())
	}
	return c.JSON(http.StatusOK, list)
}

// ─── Non-streaming response ───────────────────────────────────────────────

func (h *Handler) writeResponse(c echo.Context, result *Result) error {
	return c.JSON(http.StatusOK, result.Response)
}

// ─── SSE streaming response ───────────────────────────────────────────────

const ssePrefix = "data: "
const sseDone = "data: [DONE]\n\n"

func (h *Handler) writeStream(c echo.Context, result *Result, model string, opts *StreamOptions) error {
	rw := c.Response()
	rw.Header().Set(echo.HeaderContentType, "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")
	rw.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
	rw.WriteHeader(http.StatusOK)

	flusher, canFlush := rw.Writer.(http.Flusher)

	reqCtx := c.Request().Context()
	chunkID := "chatcmpl-" + shortID()
	created := time.Now().Unix()

	// Helper: write one SSE frame.
	writeChunk := func(chunk ChatCompletionChunk) error {
		b, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(rw, "%s%s\n\n", ssePrefix, b); err != nil {
			return err
		}
		if canFlush {
			flusher.Flush()
		}
		return nil
	}

	// Opening delta: role only.
	role := "assistant"
	openDelta := Delta{Role: role}
	if err := writeChunk(ChatCompletionChunk{
		ID:      chunkID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []ChunkChoice{{Index: 0, Delta: openDelta}},
	}); err != nil {
		return nil // client disconnected
	}

	var usage *Usage
	toolCallIndex := 0
	toolCallsStreamed := false
	pendingToolCallID := ""

	for {
		select {
		case <-reqCtx.Done():
			return nil
		case ev, ok := <-result.StreamEvents:
			if !ok {
				// Channel closed — write [DONE] and exit.
				if opts != nil && opts.IncludeUsage && usage != nil {
					_ = writeChunk(ChatCompletionChunk{
						ID:      chunkID,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   model,
						Choices: []ChunkChoice{},
						Usage:   usage,
					})
				}
				_, _ = fmt.Fprint(rw, sseDone)
				if canFlush {
					flusher.Flush()
				}
				return nil
			}

			if ev.Done {
				usage = ev.Usage

				// If the run errored, emit an SSE error chunk before [DONE] so the
				// client can detect the failure instead of seeing a silent empty stream.
				if ev.ErrorMsg != "" {
					errFinish := "error"
					_ = writeChunk(ChatCompletionChunk{
						ID:      chunkID,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   model,
						Choices: []ChunkChoice{{
							Index:        0,
							Delta:        Delta{Content: &ev.ErrorMsg},
							FinishReason: &errFinish,
						}},
					})
					_, _ = fmt.Fprint(rw, sseDone)
					if canFlush {
						flusher.Flush()
					}
					return nil
				}

				// Write final chunk with finish_reason.
				finishReason := "stop"
				if toolCallsStreamed {
					finishReason = "tool_calls"
				}
				// Carry the resume token from the Done event so streaming clients
				// can resume a paused run (fix: result.Response is nil in streaming path).
				var resumeFingerprint string
				if ev.ResumeRunID != "" {
					resumeFingerprint = resumeToken(ev.ResumeRunID)
				}
				_ = writeChunk(ChatCompletionChunk{
					ID:      chunkID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   model,
					Choices: []ChunkChoice{{
						Index:        0,
						Delta:        Delta{},
						FinishReason: &finishReason,
					}},
					SystemFingerprint: resumeFingerprint,
				})
				if opts != nil && opts.IncludeUsage && usage != nil {
					_ = writeChunk(ChatCompletionChunk{
						ID:      chunkID,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   model,
						Choices: []ChunkChoice{},
						Usage:   usage,
					})
				}
				_, _ = fmt.Fprint(rw, sseDone)
				if canFlush {
					flusher.Flush()
				}
				return nil
			}

			if ev.TextDelta != "" {
				content := ev.TextDelta
				_ = writeChunk(ChatCompletionChunk{
					ID:      chunkID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   model,
					Choices: []ChunkChoice{{
						Index: 0,
						Delta: Delta{Content: &content},
					}},
				})
			}

			if len(ev.ClientToolCalls) > 0 {
				toolCallsStreamed = true
				for _, tc := range ev.ClientToolCalls {
					// First chunk for this tool call: id + type + function name.
					if pendingToolCallID != tc.ID {
						pendingToolCallID = tc.ID
						tcType := "function"
						_ = writeChunk(ChatCompletionChunk{
							ID:      chunkID,
							Object:  "chat.completion.chunk",
							Created: created,
							Model:   model,
							Choices: []ChunkChoice{{
								Index: 0,
								Delta: Delta{
									ToolCalls: []DeltaToolCall{{
										Index: toolCallIndex,
										ID:    tc.ID,
										Type:  tcType,
										Function: &DeltaFunctionCall{
											Name: tc.Function.Name,
										},
									}},
								},
							}},
						})
					}
					// Arguments chunk.
					_ = writeChunk(ChatCompletionChunk{
						ID:      chunkID,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   model,
						Choices: []ChunkChoice{{
							Index: 0,
							Delta: Delta{
								ToolCalls: []DeltaToolCall{{
									Index: toolCallIndex,
									Function: &DeltaFunctionCall{
										Arguments: tc.Function.Arguments,
									},
								}},
							},
						}},
					})
					toolCallIndex++
				}
			}
		}
	}
}

// ─── Error helpers ────────────────────────────────────────────────────────

func (h *Handler) apiError(c echo.Context, status int, errType, msg string) error {
	return c.JSON(status, APIError{
		Error: APIErrorDetail{
			Message: msg,
			Type:    errType,
		},
	})
}
