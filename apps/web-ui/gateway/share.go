package main

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/emergent-company/go-daisy/render"
	"github.com/labstack/echo/v4"
)

// --- anonymous share cookie -------------------------------------------------

// shareCookieName is the sealed cookie the exchange sets for an anonymous end
// user. It is HttpOnly, Secure (over https), SameSite=Lax, scoped to /share,
// and carries the share key ENCRYPTED (AES-256-GCM) plus the end-user ref. The
// raw key is never recoverable by the client and never appears in logs.
const shareCookieName = "memory_share"

// shareCookieMaxAge is how long the browser retains the share cookie. Decoupled
// from the link's own expiry: the server still rejects an expired/revoked key on
// every upstream call.
const shareCookieMaxAge = 30 * 24 * time.Hour

// shareCookieClaims is the sealed payload of the share cookie.
type shareCookieClaims struct {
	Token      string `json:"token"`      // share key (bearer) — never exposed to the client
	EndUserRef string `json:"endUserRef"` // anonymous UUID identifying this visitor
}

// sealShareCookie encrypts claims with AES-256-GCM keyed by SHA-256(secret) and
// returns base64url(nonce || ciphertext || tag). The GCM tag authenticates the
// payload, so a tampered cookie fails to open.
func sealShareCookie(secret string, claims shareCookieClaims) (string, error) {
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	plaintext, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// openShareCookie decrypts and authenticates a sealed share cookie value.
func openShareCookie(secret, value string) (*shareCookieClaims, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("share: cookie too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, err
	}
	var claims shareCookieClaims
	if err := json.Unmarshal(plaintext, &claims); err != nil {
		return nil, err
	}
	if claims.Token == "" || claims.EndUserRef == "" {
		return nil, errors.New("share: empty cookie claims")
	}
	return &claims, nil
}

// shareRefSig computes base64url-nopad(HMAC-SHA256(key=SHARE_REF_SECRET,
// message=<the exact end_user_ref string>)). The server verifies it with
// constant-time comparison.
func shareRefSig(secret, endUserRef string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(endUserRef))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// errShareNotConfigured is returned when the share surface is missing a required
// secret and must fail closed.
var errShareNotConfigured = errors.New("share: not configured")

// shareConfigured reports whether the public share surface can safely serve
// requests: both the cookie-sealing key and the end-user-ref signing key must be
// present. Without the ref key the gateway would have to send unsigned refs.
func (s *Server) shareConfigured() bool {
	return s.cfg.ShareCookieSecret != "" && s.cfg.ShareRefSecret != ""
}

// shareNotConfigured is the 503 body for an unconfigured share surface.
func shareNotConfigured(c echo.Context) error {
	return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "sharing is not configured", "code": "invalid"})
}

// shareClaims reads and decrypts the share cookie for the request.
func (s *Server) shareClaims(c echo.Context) (*shareCookieClaims, error) {
	if !s.shareConfigured() {
		return nil, errShareNotConfigured
	}
	ck, err := c.Cookie(shareCookieName)
	if err != nil {
		return nil, err
	}
	return openShareCookie(s.cfg.ShareCookieSecret, ck.Value)
}

// shareClaimsOrFail resolves the share claims and writes the right error
// response when they cannot be resolved. ok=false means the response was already
// written.
func (s *Server) shareClaimsOrFail(c echo.Context) (*shareCookieClaims, bool) {
	claims, err := s.shareClaims(c)
	if err == nil {
		return claims, true
	}
	if errors.Is(err, errShareNotConfigured) {
		_ = shareNotConfigured(c)
	} else {
		_ = shareUnauthorized(c)
	}
	return nil, false
}

// shareCookieSecure reports whether the share cookie should carry the Secure
// flag: pinned by the share public base URL (http:// disables it), else derived
// from the request scheme so a TLS-terminating proxy still gets Secure cookies.
func (s *Server) shareCookieSecure(c echo.Context) bool {
	if b := s.cfg.sharePublicBase(); b != "" {
		return !strings.HasPrefix(b, "http://")
	}
	return c.Scheme() == "https"
}

// newShareEndUserRef returns a random RFC 4122 v4 UUID for the anonymous end
// user. The server validates endUserRef as UUID-form before writing it to a
// uuid column.
func newShareEndUserRef() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// sharePublicBase returns the public share host used for copyable owner URLs,
// falling back to PublicBaseURL when SharePublicBaseURL is unset.
func (c Config) sharePublicBase() string {
	if b := strings.TrimSpace(c.SharePublicBaseURL); b != "" {
		return strings.TrimRight(b, "/")
	}
	return strings.TrimRight(c.PublicBaseURL, "/")
}

// sharePublicURL builds the full public link for a share key (the key rides the
// URL fragment, so it never reaches a server and is never logged).
func (s *Server) sharePublicURL(token string) string {
	base := s.cfg.sharePublicBase()
	if token == "" {
		return ""
	}
	return base + "/share/agent#" + token
}

// --- error mapping ----------------------------------------------------------

// shareExchangeErrorCode maps an upstream share failure to the client-facing
// terminal code: invalid | revoked | expired | rate-limited | budget-exceeded.
func shareExchangeErrorCode(err error) string {
	var he *memoryHTTPError
	if errors.As(err, &he) {
		switch he.Code {
		case "share_link_revoked":
			return "revoked"
		case "share_link_expired":
			return "expired"
		case "share_budget_exceeded":
			return "budget-exceeded"
		}
	}
	if memoryStatus(err) == http.StatusTooManyRequests {
		return "rate-limited"
	}
	return "invalid"
}

// shareExchangeErrorResponse writes the exchange failure body with the mapped
// terminal code.
func shareExchangeErrorResponse(c echo.Context, err error) error {
	code := shareExchangeErrorCode(err)
	status := http.StatusUnauthorized
	switch code {
	case "revoked", "expired":
		status = http.StatusGone
	case "rate-limited", "budget-exceeded":
		status = http.StatusTooManyRequests
	}
	return c.JSON(status, map[string]string{"error": "could not open this link", "code": code})
}

// shareUnauthorized rejects a share API call with no/invalid cookie.
func shareUnauthorized(c echo.Context) error {
	return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized", "code": "invalid"})
}

// shareProxyErrorResponse maps an upstream share-call failure to a gateway JSON
// error (the client treats any non-2xx as a degraded state). A missing ref
// secret fails closed as 503 rather than leaking an unsigned ref upstream.
func (s *Server) shareProxyErrorResponse(c echo.Context, err error) error {
	if errors.Is(err, errShareRefSecretUnset) || errors.Is(err, errShareNotConfigured) {
		return shareNotConfigured(c)
	}
	status := memoryStatus(err)
	if status == 0 {
		captureError(err)
		status = http.StatusBadGateway
	}
	return c.JSON(status, map[string]string{"error": "share request failed", "code": shareExchangeErrorCode(err)})
}

// --- exchange ---------------------------------------------------------------

// shareExchange handles POST /share/api/exchange {key}. It validates the key
// against the upstream share surface, mints an anonymous end-user ref, seals the
// key + ref into an HttpOnly cookie, and returns the sanitized public config.
func (s *Server) shareExchange(c echo.Context) error {
	if !s.shareConfigured() {
		return shareNotConfigured(c)
	}
	var in struct {
		Key string `json:"key"`
	}
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body", "code": "invalid"})
	}
	key := strings.TrimSpace(in.Key)
	if key == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "key required", "code": "invalid"})
	}
	cfg, err := s.memory.SharePublicConfig(c.Request().Context(), key)
	if err != nil {
		return shareExchangeErrorResponse(c, err)
	}
	sanitizeShareConfig(cfg)
	endUserRef, err := newShareEndUserRef()
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not open this link", "code": "invalid"})
	}
	sealed, err := sealShareCookie(s.cfg.ShareCookieSecret, shareCookieClaims{Token: key, EndUserRef: endUserRef})
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not open this link", "code": "invalid"})
	}
	c.SetCookie(&http.Cookie{
		Name:     shareCookieName,
		Value:    sealed,
		Path:     "/share",
		HttpOnly: true,
		Secure:   s.shareCookieSecure(c),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(shareCookieMaxAge.Seconds()),
	})
	return c.JSON(http.StatusOK, cfg)
}

// shareConfig handles GET /share/api/config. It requires a valid sealed
// memory_share cookie (the exchange already minted it) and returns the sanitized
// public config for the bound link — never the key/token and never project/org
// identifiers. It exists so a plain refresh or return visit (no URL fragment)
// can rehydrate the header identity and composer placeholder from the cookie
// alone, without re-running the key exchange.
func (s *Server) shareConfig(c echo.Context) error {
	claims, ok := s.shareClaimsOrFail(c)
	if !ok {
		return nil
	}
	cfg, err := s.memory.SharePublicConfig(c.Request().Context(), claims.Token)
	if err != nil {
		return shareExchangeErrorResponse(c, err)
	}
	sanitizeShareConfig(cfg)
	return c.JSON(http.StatusOK, cfg)
}

// sanitizeShareConfig normalizes the display fields of the public config before
// it is returned to the anonymous client. The icon is the risky field: an
// unknown ASCII value would otherwise render as a bogus "lucide--…" class with
// no compiled CSS, so it is resolved against the icon catalog (or dropped to
// the client's bot fallback) here rather than trusting the client to validate
// it against a catalog it does not hold.
func sanitizeShareConfig(cfg *SharePublicConfig) {
	if cfg == nil {
		return
	}
	cfg.Icon = shareIconName(cfg.Icon)
}

// --- page -------------------------------------------------------------------

// sharePage renders the public share document (GET /share/agent). The key
// travels in the URL fragment, so the page always renders the ready state with
// unknown agent identity; the client exchanges the key and reveals terminal
// states itself. ShowSessionList is optimistically true so the session rail
// exists for the client to populate. Sentry is intentionally not wired here: the
// strict share CSP (see shareSecurityHeaders) disallows third-party scripts and
// outbound reporting on this anonymous surface.
func (s *Server) sharePage(c echo.Context) error {
	props := ShareAgentPageProps{
		State:           ShareStateReady,
		ShowSessionList: true,
	}
	render.RenderPage(c.Response().Writer, c.Request(), ShareAgentPage(props))
	return nil
}

// --- sessions ---------------------------------------------------------------

// shareSessionJSON is the client-facing session shape (camelCase), distinct
// from the server's ShareSession DTO. The server does not expose a message
// count, so MessageCount is omitted.
type shareSessionJSON struct {
	ID           string `json:"id"`
	Title        string `json:"title,omitempty"`
	LastActivity string `json:"lastActivity,omitempty"`
	Archived     bool   `json:"archived"`
}

func shareSessionToJSON(sess ShareSession) shareSessionJSON {
	title := ""
	if sess.Title != nil {
		title = *sess.Title
	}
	last := sess.CreatedAt
	if sess.LastActivityAt != nil {
		last = *sess.LastActivityAt
	}
	return shareSessionJSON{
		ID:           sess.ID,
		Title:        title,
		LastActivity: relTime(last.Format(time.RFC3339)),
		Archived:     sess.IsArchived,
	}
}

// shareSessionItems maps server sessions to the rail item type used by the
// session-list partial (ShareSessionList in share_page.templ).
func shareSessionItems(sessions []ShareSession) []ShareSessionItem {
	items := make([]ShareSessionItem, 0, len(sessions))
	for _, sess := range sessions {
		title := ""
		if sess.Title != nil {
			title = *sess.Title
		}
		last := sess.CreatedAt
		if sess.LastActivityAt != nil {
			last = *sess.LastActivityAt
		}
		items = append(items, ShareSessionItem{
			ID:           sess.ID,
			Title:        title,
			LastActivity: relTime(last.Format(time.RFC3339)),
			Archived:     sess.IsArchived,
		})
	}
	return items
}

// shareListSessions handles GET /share/api/sessions?filter=active|all|archived.
// The filter is passed through to the backend, which defaults to "active".
func (s *Server) shareListSessions(c echo.Context) error {
	claims, ok := s.shareClaimsOrFail(c)
	if !ok {
		return nil
	}
	sessions, err := s.memory.ShareListSessions(c.Request().Context(), claims.Token, claims.EndUserRef, c.QueryParam("filter"))
	if err != nil {
		return s.shareProxyErrorResponse(c, err)
	}
	out := make([]shareSessionJSON, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, shareSessionToJSON(sess))
	}
	return c.JSON(http.StatusOK, map[string]any{"sessions": out})
}

// shareCreateSession handles POST /share/api/sessions.
func (s *Server) shareCreateSession(c echo.Context) error {
	claims, ok := s.shareClaimsOrFail(c)
	if !ok {
		return nil
	}
	var in struct {
		Email string `json:"email"`
		Title string `json:"title"`
	}
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	sess, err := s.memory.ShareCreateSession(c.Request().Context(), claims.Token, claims.EndUserRef, ShareCreateSessionInput{
		Email: in.Email,
		Title: in.Title,
	})
	if err != nil {
		return s.shareProxyErrorResponse(c, err)
	}
	return c.JSON(http.StatusCreated, shareSessionToJSON(*sess))
}

// shareGetSession handles GET /share/api/sessions/:id. It returns the session
// detail including the transcript messages, so resuming a session renders its
// history (the client reuses its own bubble renderer).
func (s *Server) shareGetSession(c echo.Context) error {
	claims, ok := s.shareClaimsOrFail(c)
	if !ok {
		return nil
	}
	sess, err := s.memory.ShareGetSession(c.Request().Context(), claims.Token, claims.EndUserRef, c.Param("id"))
	if err != nil {
		return s.shareProxyErrorResponse(c, err)
	}
	// Render markdown server-side for assistant messages only; user messages
	// stay plain text and are never interpreted as HTML by the client.
	for i := range sess.Messages {
		if sess.Messages[i].Role == "assistant" {
			sess.Messages[i].HTML = renderMarkdown(sess.Messages[i].Content)
		}
	}
	return c.JSON(http.StatusOK, sess)
}

// shareArchiveSession handles POST /share/api/sessions/:id/archive.
func (s *Server) shareArchiveSession(c echo.Context) error {
	claims, ok := s.shareClaimsOrFail(c)
	if !ok {
		return nil
	}
	if err := s.memory.ShareArchiveSession(c.Request().Context(), claims.Token, claims.EndUserRef, c.Param("id")); err != nil {
		return s.shareProxyErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "archived"})
}

// sharePartialSessions handles GET /share/api/partial/sessions — the
// server-rendered rail fragment swapped into #share-session-list. The filter is
// passed through so the archived filter works.
func (s *Server) sharePartialSessions(c echo.Context) error {
	claims, ok := s.shareClaimsOrFail(c)
	if !ok {
		return nil
	}
	sessions, err := s.memory.ShareListSessions(c.Request().Context(), claims.Token, claims.EndUserRef, c.QueryParam("filter"))
	if err != nil {
		return s.shareProxyErrorResponse(c, err)
	}
	render.RenderPartial(c.Response().Writer, c.Request(), ShareSessionList(shareSessionItems(sessions)))
	return nil
}

// --- questions / approvals --------------------------------------------------

// shareQuestionJSON is the client-facing question shape. The server's question
// DTO carries no session id, so the gateway fetches per-session and tags each
// question with the session that owns it.
type shareQuestionJSON struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Title     string `json:"title,omitempty"`
	Tool      string `json:"tool,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

func mapShareQuestion(q ShareQuestion, sessionID string) shareQuestionJSON {
	var tool string
	if len(q.Proposal) > 0 {
		if b, err := json.Marshal(q.Proposal); err == nil {
			tool = string(b)
		}
	}
	return shareQuestionJSON{
		ID:        q.ID,
		SessionID: sessionID,
		Title:     q.Question,
		Tool:      tool,
	}
}

// shareQuestions handles GET /share/api/questions: pending approvals for every
// session the end user owns on this link, each tagged with its session id.
func (s *Server) shareQuestions(c echo.Context) error {
	claims, ok := s.shareClaimsOrFail(c)
	if !ok {
		return nil
	}
	sessions, err := s.memory.ShareListSessions(c.Request().Context(), claims.Token, claims.EndUserRef, "")
	if err != nil {
		return s.shareProxyErrorResponse(c, err)
	}
	out := []shareQuestionJSON{}
	for _, sess := range sessions {
		qs, qerr := s.memory.ShareListQuestions(c.Request().Context(), claims.Token, claims.EndUserRef, sess.ID)
		if qerr != nil {
			continue // best-effort: one session's failure must not drop the rest
		}
		for _, q := range qs {
			out = append(out, mapShareQuestion(q, sess.ID))
		}
	}
	return c.JSON(http.StatusOK, map[string]any{"questions": out})
}

// shareApprove handles POST /share/api/sessions/:id/approvals/:questionId. The
// client's {decision} maps onto the server's {response} ("approve"/"deny").
func (s *Server) shareApprove(c echo.Context) error {
	claims, ok := s.shareClaimsOrFail(c)
	if !ok {
		return nil
	}
	var in struct {
		Decision string `json:"decision"`
		Message  string `json:"message"`
	}
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if in.Decision == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "decision required"})
	}
	if err := s.memory.ShareApprove(c.Request().Context(), claims.Token, claims.EndUserRef, c.Param("id"), c.Param("questionId"), ShareApproveInput{
		Response: in.Decision,
		Message:  in.Message,
	}); err != nil {
		return s.shareProxyErrorResponse(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}

// --- chat (SSE proxy) -------------------------------------------------------

// shareChat handles POST /share/api/chat. When the client has no session id yet
// it creates one first, then proxies the upstream SSE stream — injecting a
// leading `meta` frame with the session id and rewriting token/error frames to
// the client's contract.
func (s *Server) shareChat(c echo.Context) error {
	claims, ok := s.shareClaimsOrFail(c)
	if !ok {
		return nil
	}
	var in struct {
		Message   string `json:"message"`
		SessionID string `json:"sessionId"`
		Email     string `json:"email"`
	}
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if strings.TrimSpace(in.Message) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "message required"})
	}
	ctx := c.Request().Context()
	sessionID := in.SessionID
	if sessionID == "" {
		// A link that requires an email is rejected upstream with
		// share_email_required unless the session carries one, so the implicit
		// first-message session creation must forward any email the page
		// collected (POST /share/api/sessions carries it on the explicit path).
		sess, cerr := s.memory.ShareCreateSession(ctx, claims.Token, claims.EndUserRef, ShareCreateSessionInput{Email: strings.TrimSpace(in.Email)})
		if cerr != nil {
			return s.shareProxyErrorResponse(c, cerr)
		}
		sessionID = sess.ID
	}
	body, err := s.memory.ShareChatStream(ctx, claims.Token, claims.EndUserRef, ShareStreamInput{
		SessionID: sessionID,
		Message:   in.Message,
	})
	if err != nil {
		return s.shareProxyErrorResponse(c, err)
	}
	defer func() { _ = body.Close() }()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-s.shutdownCh:
			_ = body.Close()
		case <-stop:
		}
	}()
	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = pw.Close() }()
		if err := rewriteShareStream(pw, body, sessionID); err != nil {
			captureError(err)
		}
	}()
	return c.Stream(http.StatusOK, "text/event-stream", pr)
}

// rewriteShareStream proxies the share SSE stream: it emits a leading `meta`
// frame carrying the session id, rewrites `token` frames to the client's
// `delta` field, annotates known `error` frames with a terminal `code`, and
// passes everything else through verbatim.
func rewriteShareStream(w io.Writer, r io.Reader, sessionID string) error {
	meta, err := marshalNoEscape(map[string]string{"type": "meta", "sessionId": sessionID})
	if err != nil {
		return err
	}
	if _, err := fmtEvent(w, meta); err != nil {
		return err
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	sc.Split(splitSSEEvent)
	var sb strings.Builder
	var snapshotEmitted bool // true once this turn's html snapshot was emitted
	for sc.Scan() {
		raw := sc.Bytes()
		data := extractSSEData(raw)
		if data == "" {
			continue
		}
		var ev struct {
			Type  string `json:"type"`
			Token string `json:"token"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			if _, werr := w.Write(raw); werr != nil {
				return werr
			}
			continue
		}
		switch ev.Type {
		case "token":
			sb.WriteString(ev.Token)
			payload, merr := marshalNoEscape(map[string]string{"type": "token", "delta": ev.Token})
			if merr != nil {
				return merr
			}
			if _, werr := fmtEvent(w, payload); werr != nil {
				return werr
			}
		case "done":
			// Emit the authoritative markdown snapshot before passing done
			// through, then reset the turn buffer for any later turn.
			if err := emitMarkdownSnapshot(w, &sb, &snapshotEmitted); err != nil {
				return err
			}
			if _, werr := w.Write(raw); werr != nil {
				return werr
			}
			sb.Reset()
			snapshotEmitted = false
		case "error":
			if code := shareSSEErrorCode(ev.Error); code != "" {
				payload, merr := marshalNoEscape(map[string]string{"type": "error", "error": ev.Error, "code": code})
				if merr != nil {
					return merr
				}
				if _, werr := fmtEvent(w, payload); werr != nil {
					return werr
				}
				continue
			}
			if _, werr := w.Write(raw); werr != nil {
				return werr
			}
		default:
			if _, werr := w.Write(raw); werr != nil {
				return werr
			}
		}
	}
	// Fallback: a turn that produced text but never saw `done` (error/EOF)
	// still gets its rendered snapshot before termination.
	if err := emitMarkdownSnapshot(w, &sb, &snapshotEmitted); err != nil {
		return err
	}
	return sc.Err()
}

// shareSSEErrorCode maps an upstream share error message prefix to the
// client-facing terminal code (the server emits "code: message" error text).
func shareSSEErrorCode(msg string) string {
	code, _, _ := strings.Cut(msg, ":")
	switch strings.TrimSpace(code) {
	case "share_budget_exceeded":
		return "budget-exceeded"
	case "share_busy", "share_session_limit", "share_approval_limit":
		return "rate-limited"
	case "share_link_revoked":
		return "revoked"
	case "share_link_expired":
		return "expired"
	default:
		return ""
	}
}
