package relay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/emergent-company/memory.web-ui/connector/internal/toolreg"
)

// Default tunables. Keepalive and timeout cadence sit below the hub's idle
// reap (90s) and pong wait (60s); the hub pings every 45s, so a 25s client
// ping keeps traffic flowing through proxies at least as often as the hub's.
const (
	DefaultPingPeriod        = 25 * time.Second
	DefaultReadWait          = 60 * time.Second
	DefaultWriteTimeout      = 10 * time.Second
	DefaultAckTimeout        = 10 * time.Second
	DefaultCallTimeout       = 30 * time.Second
	DefaultBackoffBase       = 30 * time.Second
	DefaultBackoffMax        = 5 * time.Minute
	DefaultBackoffFactor     = 2.0
	DefaultBackoffResetAfter = 30 * time.Second
)

// ToolRegistry supplies the tool list for registration and dispatch. The
// toolreg.Registry satisfies it.
type ToolRegistry interface {
	Lookup(name string) (toolreg.Tool, bool)
	ToolsListPayload() map[string]any
}

// State is a point-in-time snapshot for the status command.
type State struct {
	Connected      bool
	InstanceID     string
	Version        string
	RegisterCount  int
	LastError      error
	LastConnected  time.Time
	LastRegistered time.Time
}

// Client is one outbound MCP relay connection with reconnect, keepalive, and
// tool-call dispatch. Run owns the reconnect loop; a single Client keeps one
// WebSocket at a time.
type Client struct {
	serverURL  string
	token      string
	projectID  string
	instanceID string
	version    string
	registry   ToolRegistry
	dialer     *websocket.Dialer
	log        *slog.Logger

	pingPeriod  time.Duration
	readWait    time.Duration
	writeTo     time.Duration
	ackTimeout  time.Duration
	callTimeout time.Duration

	backoffBase       time.Duration
	backoffMax        time.Duration
	backoffFactor     float64
	backoffResetAfter time.Duration

	mu             sync.Mutex
	conn           *websocket.Conn
	writeMu        sync.Mutex
	closed         bool
	connected      bool
	registerCount  int
	lastError      error
	lastConnected  time.Time
	lastRegistered time.Time
}

// Option configures a Client. Timers are injectable so tests run fast.
type Option func(*Client)

// WithPingPeriod sets the WS control ping interval (default 25s).
func WithPingPeriod(d time.Duration) Option { return func(c *Client) { c.pingPeriod = d } }

// WithReadWait sets how long the client waits for inbound traffic before
// treating the connection as dead (default 60s).
func WithReadWait(d time.Duration) Option { return func(c *Client) { c.readWait = d } }

// WithWriteTimeout bounds each WebSocket write (default 10s).
func WithWriteTimeout(d time.Duration) Option { return func(c *Client) { c.writeTo = d } }

// WithAckTimeout bounds the wait for the hub's registered acknowledgment
// (default 10s).
func WithAckTimeout(d time.Duration) Option { return func(c *Client) { c.ackTimeout = d } }

// WithCallTimeout bounds each relayed tool call (default 30s).
func WithCallTimeout(d time.Duration) Option { return func(c *Client) { c.callTimeout = d } }

// WithBackoff sets the capped exponential reconnect backoff
// (defaults 30s base, 5m cap, x2).
func WithBackoff(base, max time.Duration, factor float64) Option {
	return func(c *Client) {
		c.backoffBase = base
		c.backoffMax = max
		c.backoffFactor = factor
	}
}

// WithBackoffResetAfter sets the connection uptime that resets the backoff
// counter (default 30s).
func WithBackoffResetAfter(d time.Duration) Option {
	return func(c *Client) { c.backoffResetAfter = d }
}

// WithDialer overrides the WebSocket dialer.
func WithDialer(d *websocket.Dialer) Option { return func(c *Client) { c.dialer = d } }

// WithLogger sets the logger (default slog.Default()).
func WithLogger(l *slog.Logger) Option { return func(c *Client) { c.log = l } }

// New creates a relay client. instanceID and version identify the connector
// in the hub's session list; token must be non-empty (project context comes
// from the token).
func New(serverURL, token, projectID, instanceID, version string, registry ToolRegistry, opts ...Option) (*Client, error) {
	if serverURL == "" {
		return nil, errors.New("relay: server_url required")
	}
	if token == "" {
		return nil, errors.New("relay: token required")
	}
	if instanceID == "" {
		return nil, errors.New("relay: instance_id required")
	}
	if registry == nil {
		return nil, errors.New("relay: tool registry required")
	}
	c := &Client{
		serverURL:         serverURL,
		token:             token,
		projectID:         projectID,
		instanceID:        instanceID,
		version:           version,
		registry:          registry,
		dialer:            &websocket.Dialer{HandshakeTimeout: 10 * time.Second},
		log:               slog.Default(),
		pingPeriod:        DefaultPingPeriod,
		readWait:          DefaultReadWait,
		writeTo:           DefaultWriteTimeout,
		ackTimeout:        DefaultAckTimeout,
		callTimeout:       DefaultCallTimeout,
		backoffBase:       DefaultBackoffBase,
		backoffMax:        DefaultBackoffMax,
		backoffFactor:     DefaultBackoffFactor,
		backoffResetAfter: DefaultBackoffResetAfter,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// ConnectURL derives the relay WebSocket endpoint from a server base URL:
// https→wss, http→ws, trailing slash stripped, /api/mcp-relay/connect
// appended. projectId is added as a query parameter only when non-empty
// (project context normally comes from the token).
func ConnectURL(serverURL, projectID string) (string, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", fmt.Errorf("relay: parse server URL: %w", err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("relay: unsupported server URL scheme %q (want http/https)", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/mcp-relay/connect"
	u.Fragment = ""
	if projectID != "" {
		q := u.Query()
		q.Set("projectId", projectID)
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

// State returns a point-in-time snapshot.
func (c *Client) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return State{
		Connected:      c.connected,
		InstanceID:     c.instanceID,
		Version:        c.version,
		RegisterCount:  c.registerCount,
		LastError:      c.lastError,
		LastConnected:  c.lastConnected,
		LastRegistered: c.lastRegistered,
	}
}

// Close forces the current connection closed. Run (which owns the reconnect
// loop) exits when its context is cancelled.
func (c *Client) Close() {
	c.mu.Lock()
	conn := c.conn
	c.closed = true
	c.mu.Unlock()
	if conn != nil {
		_ = c.writeControl(conn, websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		_ = conn.Close()
	}
}

// RefreshRegistration re-sends the register frame on the current live
// connection so the hub immediately sees an updated tool list after a config
// reload, without waiting for a reconnect. It is a no-op (returning nil) when
// there is no live connection or the client is closed; the next (re)connect
// registers the current list anyway. Writes go through the client's single
// writer, so this is safe to call concurrently with the read loop, keepalive,
// and dispatch.
func (c *Client) RefreshRegistration() error {
	c.mu.Lock()
	conn := c.conn
	closed := c.closed
	c.mu.Unlock()
	if conn == nil || closed {
		return nil
	}
	if err := c.sendRegister(conn); err != nil {
		return fmt.Errorf("relay: refresh registration: %w", err)
	}
	return nil
}

// Run dials, registers, and serves relayed tool calls until ctx is cancelled.
// The first connection attempt is immediate; reconnects follow a capped
// exponential backoff. Returns nil on clean cancellation.
func (c *Client) Run(ctx context.Context) error {
	wsURL, err := ConnectURL(c.serverURL, c.projectID)
	if err != nil {
		return err
	}

	attempt := 0
	for {
		if ctx.Err() != nil || c.isClosed() {
			return nil
		}
		c.log.Info("relay: connecting", "url", wsURL, "instance", c.instanceID)

		serveErr := c.serveOnce(ctx, wsURL)

		if ctx.Err() != nil {
			c.setLastError(nil)
			return nil
		}
		if serveErr != nil {
			c.setLastError(serveErr)
			c.log.Warn("relay: connection ended", "err", serveErr)
		}

		uptime := time.Since(c.lastConnectedAt())
		delay := c.retryDelay(attempt)
		if uptime >= c.backoffResetAfter {
			attempt = 0
		} else {
			attempt++
		}
		c.log.Info("relay: reconnecting", "in", delay, "attempt", attempt)

		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil
		}
	}
}

// retryDelay returns the backoff delay for a given failure attempt
// (base * factor^attempt, capped at max).
func (c *Client) retryDelay(attempt int) time.Duration {
	d := float64(c.backoffBase) * math.Pow(c.backoffFactor, float64(attempt))
	if d > float64(c.backoffMax) {
		d = float64(c.backoffMax)
	}
	if d < 0 {
		return c.backoffBase
	}
	return time.Duration(d)
}

// serveOnce manages one connection lifecycle: dial, register (first frame),
// wait for the registered ack, then serve requests and keepalive until the
// connection drops or ctx is cancelled.
func (c *Client) serveOnce(ctx context.Context, wsURL string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	started := time.Now()
	c.setLastConnected(started)

	conn, resp, err := c.dialer.DialContext(ctx, wsURL, c.handshakeHeaders())
	// gorilla returns a non-nil response only on handshake failure (so the
	// caller can read error details); close it in every case.
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("relay: dial: %w", err)
	}
	conn.SetReadLimit(1 << 20)
	c.installConn(conn)
	defer c.uninstallConn()

	// Reader-side keepalive state. Handlers run inside the read goroutine, so
	// deadline updates need no extra locking.
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(c.readWait))
	})
	conn.SetPingHandler(func(string) error {
		if err := c.writeControl(conn, websocket.PongMessage, nil); err != nil {
			return err
		}
		return conn.SetReadDeadline(time.Now().Add(c.readWait))
	})
	// Deadline is best-effort; a read-deadline error surfaces via the next
	// ReadMessage call in the read loop.
	_ = conn.SetReadDeadline(time.Now().Add(c.ackTimeout))

	connDone := make(chan struct{})
	var wg sync.WaitGroup
	cleanup := func() {
		close(connDone)
		cancel()
		wg.Wait()
	}
	defer cleanup()

	// Watcher: unblock the read loop when the parent context is cancelled.
	// Started before register so a cancellation while awaiting the ack still
	// shuts down promptly.
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			_ = c.writeControl(conn, websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			_ = conn.Close()
		case <-connDone:
		}
	}()

	// First frame must be register.
	if err := c.sendRegister(conn); err != nil {
		return fmt.Errorf("relay: send register: %w", err)
	}
	if err := c.waitForAck(ctx, conn); err != nil {
		return fmt.Errorf("relay: register: %w", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(c.readWait))

	// Keepalive: WS control ping at ~25s.
	wg.Add(1)
	go func() {
		defer wg.Done()
		period := c.pingPeriod
		if period <= 0 {
			period = DefaultPingPeriod
		}
		ticker := time.NewTicker(period)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := c.writeControl(conn, websocket.PingMessage, nil); err != nil {
					return
				}
			case <-connDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			_ = conn.Close()
			return fmt.Errorf("relay: read: %w", err)
		}
		if msgType != websocket.TextMessage {
			continue
		}
		c.handleTextFrame(ctx, conn, data, &wg)
	}
}

// handleTextFrame decodes one inbound text frame and acts on it. Request
// frames are dispatched on a fresh goroutine (bounded by callTimeout and the
// connection context) so the read loop stays responsive. Everything else is
// handled inline or ignored — never fatal.
func (c *Client) handleTextFrame(ctx context.Context, conn *websocket.Conn, data []byte, wg *sync.WaitGroup) {
	frame, err := Parse(data)
	if err != nil {
		switch {
		case errors.Is(err, ErrUnknownFrame):
			c.log.Debug("relay: ignoring unknown frame type", "err", err)
		default:
			c.log.Debug("relay: ignoring malformed frame", "err", err)
		}
		return
	}

	switch f := frame.(type) {
	case *RequestFrame:
		callCtx, cancel := context.WithTimeout(ctx, c.callTimeout)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			c.dispatch(callCtx, conn, f)
		}()
	case *PingFrame:
		// App-level ping from the hub; answer with an app-level pong.
		if data, err := Marshal(&PongFrame{Type: FramePong}); err == nil {
			_ = c.writeText(conn, data)
		}
	case *PongFrame, *ResponseFrame, *RegisteredFrame:
		// The client issues no requests in v1; response frames and acks to a
		// re-register are noise here.
		c.log.Debug("relay: ignoring unsolicited frame", "type", frameTypeOf(frame))
	case *ErrorFrame:
		c.log.Warn("relay: hub error frame", "message", f.Message)
	default:
		c.log.Debug("relay: ignoring frame", "type", frameTypeOf(frame))
	}
}

func (c *Client) dispatch(ctx context.Context, conn *websocket.Conn, req *RequestFrame) {
	sendErr := func(msg string) {
		c.sendResponse(conn, req.ID, nil, msg)
	}

	method, _ := req.Payload["method"].(string)
	if method != "tools/call" {
		sendErr(fmt.Sprintf("unsupported method %q", method))
		return
	}
	params, _ := req.Payload["params"].(map[string]any)
	if params == nil {
		sendErr("missing params")
		return
	}
	name, _ := params["name"].(string)
	if name == "" {
		sendErr("missing tool name")
		return
	}

	tool, ok := c.registry.Lookup(name)
	if !ok {
		sendErr(fmt.Sprintf("unknown tool: %q", name))
		return
	}

	args, _ := params["arguments"].(map[string]any)
	if args == nil {
		args = map[string]any{}
	}

	result, err := c.invoke(ctx, tool, args)
	if err != nil {
		sendErr(err.Error())
		return
	}
	c.sendResponse(conn, req.ID, result, "")
}

// invoke runs a tool handler, converting panics into errors so one bad tool
// can never kill the connector.
func (c *Client) invoke(ctx context.Context, tool toolreg.Tool, args map[string]any) (result map[string]any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("tool %q panicked: %v", tool.Name, r)
		}
	}()
	return tool.Handler(ctx, args)
}

// sendResponse echoes the request id back to the hub with a result payload or
// an error string.
func (c *Client) sendResponse(conn *websocket.Conn, id string, payload map[string]any, errMsg string) {
	resp := &ResponseFrame{
		Type:    FrameResponse,
		ID:      rawID(id),
		Payload: payload,
		Error:   errMsg,
	}
	data, err := Marshal(resp)
	if err != nil {
		c.log.Error("relay: marshal response", "err", err)
		return
	}
	if err := c.writeText(conn, data); err != nil {
		c.log.Debug("relay: write response", "err", err)
	}
}

func (c *Client) sendRegister(conn *websocket.Conn) error {
	reg := &RegisterFrame{
		Type:       FrameRegister,
		InstanceID: c.instanceID,
		Version:    c.version,
		Tools:      c.registry.ToolsListPayload(),
	}
	data, err := Marshal(reg)
	if err != nil {
		return err
	}
	return c.writeText(conn, data)
}

// waitForAck reads until the hub acknowledges registration. An error frame
// (e.g. "first frame must be register") fails registration.
func (c *Client) waitForAck(ctx context.Context, conn *websocket.Conn) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("ack read: %w", err)
		}
		frame, err := Parse(data)
		if err != nil {
			continue
		}
		switch f := frame.(type) {
		case *RegisteredFrame:
			c.bumpRegister(time.Now())
			return nil
		case *ErrorFrame:
			return errors.New(f.Message)
		case *PingFrame:
			if pong, err := Marshal(&PongFrame{Type: FramePong}); err == nil {
				_ = c.writeText(conn, pong)
			}
		default:
			// Ignore stray frames while waiting for the ack.
		}
	}
}

// handshakeHeaders returns the auth header for the WebSocket handshake.
func (c *Client) handshakeHeaders() http.Header {
	h := make(http.Header)
	h.Set("Authorization", "Bearer "+c.token)
	return h
}

// --- connection state helpers -------------------------------------------------

func (c *Client) installConn(conn *websocket.Conn) {
	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()
}

func (c *Client) uninstallConn() {
	c.mu.Lock()
	c.conn = nil
	c.connected = false
	c.mu.Unlock()
}

func (c *Client) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *Client) setLastError(err error) {
	c.mu.Lock()
	c.lastError = err
	c.mu.Unlock()
}

func (c *Client) setLastConnected(t time.Time) {
	c.mu.Lock()
	c.lastConnected = t
	c.mu.Unlock()
}

func (c *Client) lastConnectedAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastConnected
}

func (c *Client) bumpRegister(t time.Time) {
	c.mu.Lock()
	c.registerCount++
	c.lastRegistered = t
	c.mu.Unlock()
}

// --- writes (single writer) ---------------------------------------------------

// writeText serializes text writes with the connection-level write mutex.
func (c *Client) writeText(conn *websocket.Conn, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(c.writeTo))
	return conn.WriteMessage(websocket.TextMessage, data)
}

// writeControl serializes control writes (ping/pong/close) with the same
// mutex, honouring gorilla's single-writer rule across the keepalive ticker,
// control handlers, and dispatch goroutines.
func (c *Client) writeControl(conn *websocket.Conn, msgType int, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(c.writeTo))
	return conn.WriteMessage(msgType, data)
}

func frameTypeOf(frame any) string {
	switch f := frame.(type) {
	case *RequestFrame:
		return string(f.Type)
	case *ResponseFrame:
		return string(f.Type)
	case *PingFrame:
		return string(f.Type)
	case *PongFrame:
		return string(f.Type)
	case *ErrorFrame:
		return string(f.Type)
	case *RegisteredFrame:
		return string(f.Type)
	case *RegisterFrame:
		return string(f.Type)
	default:
		return "unknown"
	}
}
