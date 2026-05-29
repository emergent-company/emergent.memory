package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// SSEOptions controls the behaviour of StreamSSE.
type SSEOptions struct {
	// LivePrint causes token events to be printed to stdout as they arrive.
	// Set to false when the caller wants to buffer and render the full response
	// itself (e.g. ask renders markdown after the stream ends).
	LivePrint bool

	// ShowTools causes [Tool: X] lines to be printed to stderr when
	// mcp_tool/started events arrive.
	ShowTools bool

	// JSONMode suppresses stderr error lines (the caller will emit them as
	// part of a JSON envelope instead).
	JSONMode bool
}

// SSEResult holds the aggregated output of a completed SSE stream.
type SSEResult struct {
	Response  string
	Tools     []string
	SessionID string
	StreamErr string
	Elapsed   time.Duration
}

// StreamSSE executes httpReq (which must target an SSE endpoint) and reads
// the event stream until EOF or an error occurs.  It returns the aggregated
// result; the HTTP client has no timeout because the server enforces its own
// agent timeout.
func StreamSSE(httpReq *http.Request, opts SSEOptions) (*SSEResult, error) {
	httpClient := &http.Client{Timeout: 0}
	start := time.Now()

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, parseAPIError(resp.StatusCode, body)
	}

	result := &SSEResult{}
	var response strings.Builder
	reader := bufio.NewReader(resp.Body)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				return nil, fmt.Errorf("error reading response: %w", err)
			}
			break
		}
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)
		switch eventType {
		case "meta":
			if id, ok := event["conversationId"].(string); ok && id != "" {
				result.SessionID = id
			}
		case "token":
			if token, ok := event["token"].(string); ok {
				response.WriteString(token)
				if opts.LivePrint {
					fmt.Print(token)
				}
			}
		case "mcp_tool":
			if status, ok := event["status"].(string); ok && status == "started" {
				if tool, ok := event["tool"].(string); ok {
					result.Tools = append(result.Tools, tool)
					if opts.ShowTools {
						fmt.Fprintf(os.Stderr, "\n[Tool: %s]\n", tool)
					}
				}
			}
		case "error":
			if errMsg, ok := event["error"].(string); ok {
				result.StreamErr = errMsg
				if !opts.JSONMode {
					fmt.Fprintf(os.Stderr, "\nError: %s\n", errMsg)
				}
			}
		}
	}

	result.Response = response.String()
	result.Elapsed = time.Since(start)
	return result, nil
}
