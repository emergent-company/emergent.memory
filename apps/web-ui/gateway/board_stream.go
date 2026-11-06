package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
)

// The board's live refresh. The page holds one hx-sse connection open to
// /board/stream (see board.templ); this endpoint subscribes to the project's
// real-time event stream and, whenever something that affects the board
// changes, pushes server-rendered <hx-partial> frames that htmx morphs into
// #board, #board-header and #board-health.
//
// Why this exists: the previous client-side board.js subscribed to
// entity.created/updated/deleted only, but the work path emits
// entity.status_changed and entity.assigned (apps/server/domain/graph/
// work_object.go) — the one event that would have refreshed the board was
// never listened for, so agent-driven moves needed a manual reload. Rendering
// on the server also keeps the "no agent-produced content via innerHTML"
// property the board page had.
// Package-level vars, not consts: both intervals are timing knobs the tests
// shrink to make coalescing and the heartbeat deterministic (see
// board_stream_timing_test.go). Nothing outside a test reassigns them.
var (
	// boardStreamCoalesce bounds how often a burst of events re-renders: at most
	// one frame set per interval, always from the newest state.
	boardStreamCoalesce = 250 * time.Millisecond
	// boardStreamHeartbeat keeps proxies/CDNs from reaping an idle stream.
	boardStreamHeartbeat = 25 * time.Second
)

// boardStreamEventRelevant reports whether an upstream project event can change
// what the board renders. created/deleted/batch move items in or out of lanes,
// status_changed and assigned are the work-path events the old client missed,
// and agent_run changes move a card's latest-run badge.
func boardStreamEventRelevant(event, data string) bool {
	switch event {
	case "entity.created", "entity.updated", "entity.deleted",
		"entity.status_changed", "entity.assigned", "entity.batch":
	default:
		return false
	}
	var payload struct {
		Entity string `json:"entity"`
	}
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return false
	}
	return payload.Entity == "graph_object" || payload.Entity == "agent_run"
}

// boardStream serves GET /board/stream as text/event-stream. htmx's hx-sse
// extension takes over any response with that content type, so the frames here
// are plain HTML directives rather than JSON.
func (s *Server) boardStream(c echo.Context) error {
	ctx := c.Request().Context()
	projectID := activeProjectIDFromContext(ctx, s.cfg.MemoryProjectID)
	body, err := s.memory.NotificationEventStream(ctx, projectID)
	if err != nil {
		captureError(err)
		// Answer in SSE anyway. htmx's hx-sse only takes over a response whose
		// Content-Type is text/event-stream; a JSON error status would be handed
		// back to core, the connection would end for good, and the tab would
		// have no live board until a reload. Ending an event stream normally
		// instead leaves the extension's own reconnect loop in charge, so a
		// gateway restart or an upstream blip self-heals.
		return boardStreamUnavailable(c)
	}
	defer func() { _ = body.Close() }()

	w := c.Response()
	w.Header().Set(echo.HeaderContentType, "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	w.Flush()

	if s.shutdownCh != nil {
		go func() {
			select {
			case <-s.shutdownCh:
				_ = body.Close()
			case <-ctx.Done():
			}
		}()
	}

	base := ""
	if sc, ok := scopeFromContext(ctx); ok {
		base = sc.base()
	}
	statuses, allowances := s.boardSchema(ctx)
	st := &boardStreamState{
		s:          s,
		ctx:        ctx,
		w:          w,
		base:       base,
		statuses:   statuses,
		allowances: allowances,
		statusMap:  s.boardStatusMap(ctx),
	}
	// Catch-up frame: a reconnect (backgrounded tab, network blip, server
	// restart) must not leave the board stale until the next mutation, and a
	// morph of an unchanged board is a no-op for the user.
	if err := st.push(); err != nil {
		return nil
	}

	dirty := make(chan struct{}, 1)
	go boardStreamDrain(body, dirty)

	tick := time.NewTicker(boardStreamCoalesce)
	defer tick.Stop()
	lastWrite := time.Now()
	pending := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-dirty:
			pending = true
		case <-tick.C:
			if pending {
				pending = false
				if err := st.push(); err != nil {
					return nil
				}
				lastWrite = time.Now()
			} else if time.Since(lastWrite) >= boardStreamHeartbeat {
				// An agent-health change publishes no event (an operator
				// disabling an agent, a reaction agent failing with no work
				// item), so the idle tick is where the readout gets re-checked.
				// Otherwise the banner that exists to warn about a disabled
				// agent stays stale until some unrelated work-item change
				// happens to arrive (#2466). The columns are left alone: there
				// is nothing new to draw, and a re-morph mid-drag is the very
				// disturbance this stream avoids.
				if err := st.pushHealth(); err != nil {
					return nil
				}
				if _, err := io.WriteString(w, ": heartbeat\n\n"); err != nil {
					return nil
				}
				w.Flush()
				lastWrite = time.Now()
			}
		}
	}
}

// boardStreamUnavailable answers a failed upstream connection with an empty SSE
// stream instead of an HTTP error, so the client keeps retrying (see the call
// site). The retry hint is there for a raw EventSource consumer, which honours
// it; htmx's hx-sse only yields frames that carry data/id/event, so it ignores
// this one and reconnects on its own backoff — which is the behaviour the
// browser-level test asserts.
func boardStreamUnavailable(c echo.Context) error {
	w := c.Response()
	w.Header().Set(echo.HeaderContentType, "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(w, "retry: 5000\n: upstream event stream unavailable\n\n"); err != nil {
		return nil
	}
	w.Flush()
	return nil
}

// boardStreamDrain parses the upstream SSE stream and signals dirty when a
// board-relevant event lands. It never writes to the response.
func boardStreamDrain(r io.Reader, dirty chan<- struct{}) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var event, data string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if boardStreamEventRelevant(event, data) {
				select {
				case dirty <- struct{}{}:
				default: // a refresh is already queued; it will read the newest state
				}
			}
			event, data = "", ""
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			chunk := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				data = chunk
			} else {
				data += "\n" + chunk
			}
		}
	}
}

// boardStreamState carries per-connection render inputs: the lane order and
// status map come from the project schema/agent config and change with a page
// reload, so they are resolved once per connection rather than per frame.
type boardStreamState struct {
	s          *Server
	ctx        context.Context
	w          http.ResponseWriter
	base       string
	statuses   []string
	allowances boardAllowances
	statusMap  boardStatusMap

	headerSent bool
	lastCount  int
	healthSent bool
	lastHealth string
}

// push renders and writes one frame set from the newest state: always the
// columns, the header only when the count changed, and the health readout only
// when the set of unhealthy agents changed.
func (st *boardStreamState) push() error {
	items, itemsErr := st.s.memory.ListWorkItems(st.ctx, "", "", boardItemLimit)
	errMsg := ""
	if itemsErr != nil {
		captureError(itemsErr)
		errMsg = itemsErr.Error()
	}
	boardHTML, err := st.render(BoardColumnsFrame(items, st.statuses, st.allowances, st.statusMap, errMsg))
	if err != nil {
		return err
	}
	if err := writeSSEPartial(st.w, "#board", "outerMorph", boardHTML); err != nil {
		return err
	}

	if !st.headerSent || st.lastCount != len(items) {
		st.headerSent = true
		st.lastCount = len(items)
		headerHTML, herr := st.render(boardHeaderRegion())
		if herr != nil {
			return herr
		}
		if err := writeSSEPartial(st.w, "#board-header", "outerMorph", headerHTML); err != nil {
			return err
		}
	}

	return st.pushHealth()
}

// pushHealth sends the agent-health readout when its signature changed since the
// last frame. It is separate from push() so the stream can re-check the readout
// without re-rendering the columns: agent-health changes publish no event
// (#2466), so the idle tick is the only thing that can notice them.
func (st *boardStreamState) pushHealth() error {
	agents, err := st.s.memory.ListScheduledAgents(st.ctx)
	if err != nil {
		// A failed fetch is "unknown", not "healthy". boardUnhealthyAgents(nil)
		// is empty, so rendering the error as an empty list would assert an
		// all-clear and clear the warning banner while the scheduling service is
		// unreachable. Keep the last good readout instead and send nothing: the
		// banner goes stale rather than falsely disappearing, and a sustained
		// outage emits no frame per heartbeat (the signature never changes
		// because it is not recomputed from a failed call).
		captureError(err)
		return nil
	}
	sig := boardHealthSignature(agents)
	if st.healthSent && sig == st.lastHealth {
		return nil
	}
	st.healthSent = true
	st.lastHealth = sig
	healthHTML, herr := st.render(boardHealthRegion(agents))
	if herr != nil {
		return herr
	}
	return writeSSEPartial(st.w, "#board-health", "outerMorph", healthHTML)
}

// render renders one component to HTML with the request's scope base applied,
// so links and htmx actions inside swapped fragments stay in project context.
func (st *boardStreamState) render(comp templ.Component) (string, error) {
	var buf bytes.Buffer
	if err := comp.Render(st.ctx, &buf); err != nil {
		captureError(err)
		return "", err
	}
	return rewriteScopedHTML(buf.String(), st.base), nil
}

// boardHealthSignature identifies the rendered state of the health readout, so
// an unchanged readout is not re-sent on every event. The empty string means
// "every reaction agent is healthy"; a failed fetch must never be passed here,
// because nil agents produce exactly that signature and "unknown" and "healthy"
// are not the same state (pushHealth keeps the last good signature instead).
func boardHealthSignature(agents []ScheduledAgent) string {
	var b strings.Builder
	for _, a := range boardUnhealthyAgents(agents) {
		b.WriteString(a.Name)
		b.WriteByte('\x00')
		if a.Enabled {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
		b.WriteByte(byte('0' + min(a.ConsecutiveFailures, 9)))
		b.WriteByte('\x00')
	}
	return b.String()
}

// writeSSEPartial writes one <hx-partial> directive as an SSE frame. The SSE
// wire format cannot carry raw newlines, so each line of the fragment becomes
// its own `data:` line; htmx's parser rejoins them with "\n" before swapping.
func writeSSEPartial(w http.ResponseWriter, target, swap, html string) error {
	var frame strings.Builder
	frame.WriteString(`<hx-partial hx-target="`)
	frame.WriteString(target)
	frame.WriteString(`" hx-swap="`)
	frame.WriteString(swap)
	frame.WriteString(`">`)
	frame.WriteString(html)
	frame.WriteString(`</hx-partial>`)

	var out strings.Builder
	for i, line := range strings.Split(frame.String(), "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString("data: ")
		out.WriteString(line)
	}
	out.WriteString("\n\n")
	if _, err := io.WriteString(w, out.String()); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}
