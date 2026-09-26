package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// knowledgeQuerySSEEvent is one SSE data payload of POST
// /api/projects/:projectId/query: token deltas stream the grounded answer,
// meta carries the conversation id, and error terminates the stream with a
// message. `done` (and any unknown type) are ignored.
type knowledgeQuerySSEEvent struct {
	Type           string `json:"type"`
	Token          string `json:"token"`
	ConversationID string `json:"conversationId"`
	Error          string `json:"error"`
}

// QueryKnowledge runs the RAG "ask the graph" Q&A endpoint
// (POST /api/projects/:projectId/query, an SSE stream) and returns the
// concatenated answer plus the conversation id from the meta event. It reads
// the whole stream into memory (the answer is bounded by the ~120s timeout —
// matching the graph-query endpoint's 120s budget) — this is not the streaming
// chat path; it renders a single answer block.
func (m *MemoryClient) QueryKnowledge(ctx context.Context, question, branch string) (answer, sessionID string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	body := map[string]string{"message": question}
	if branch != "" {
		body["branch"] = branch
	}
	b, err := json.Marshal(body)
	if err != nil {
		return "", "", err
	}
	path := "/api/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/query"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+m.tokenFor(ctx))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range sessionHeaders(ctx) {
		req.Header.Set(k, v)
	}
	resp, err := m.streamHTTP.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxMemoryResponseBytes))
		return "", "", parseMemoryError(resp.StatusCode, raw)
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	sc.Split(splitSSEEvent)
	var sb strings.Builder
	seenDone := false
	for sc.Scan() {
		data := extractSSEData(sc.Bytes())
		if data == "" {
			continue
		}
		// A bare [DONE] sentinel line marks a clean completion, same as the
		// JSON type:"done" event below.
		if strings.TrimSpace(data) == "[DONE]" {
			seenDone = true
			continue
		}
		var ev knowledgeQuerySSEEvent
		if json.Unmarshal([]byte(data), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "token":
			sb.WriteString(ev.Token)
		case "meta":
			if ev.ConversationID != "" {
				sessionID = ev.ConversationID
			}
		case "done":
			seenDone = true
		case "error":
			if ev.Error != "" {
				return sb.String(), sessionID, errors.New(ev.Error)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", "", err
	}
	// A clean EOF without a terminal `done`/`[DONE]` event means the stream was
	// interrupted mid-answer (proxy/backend disconnect) — surface it as an
	// error rather than returning the partial text as a grounded answer.
	if !seenDone {
		return "", "", fmt.Errorf("query_knowledge: stream ended before completion")
	}
	return sb.String(), sessionID, nil
}
