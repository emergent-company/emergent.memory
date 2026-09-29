package email

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// invokeWebhook drives the handler directly (no route middleware) so a unit
// test can assert the rejection reason without a database.
func invokeWebhook(t *testing.T, h *MailgunWebhookHandler, payload string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/mailgun", strings.NewReader(payload))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return rec, h.Handle(c)
}

// The single-use token store must reject a second use and must release the
// token once its TTL has elapsed so memory cannot grow without bound.
func TestReplayGuard_SingleUseAndExpiry(t *testing.T) {
	now := time.Unix(1700000000, 0)
	g := newReplayGuard(5*time.Minute, 100, func() time.Time { return now })

	require.True(t, g.consume("tok-a"), "first use is accepted")
	require.False(t, g.consume("tok-a"), "reused token is rejected")
	require.True(t, g.consume("tok-b"), "a distinct token is accepted")

	now = now.Add(6 * time.Minute)
	require.True(t, g.consume("tok-a"), "after the TTL the token is forgotten and may be reused")
}

// A stale signature timestamp must be rejected before any ingestion.
func TestMailgunWebhook_RejectsStaleTimestamp(t *testing.T) {
	const key = "key"
	h := NewMailgunWebhookHandler(nil, &Config{MailgunSigningKey: key}, discardLogger())

	stale := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	payload := buildWebhookPayload(t, signMailgunBody(key, stale, "tok-stale"), stale, "tok-stale", "evt-stale")

	_, err := invokeWebhook(t, h, payload)
	require.Error(t, err)
	status, _ := apperror.ToHTTPError(err)
	require.Equal(t, http.StatusUnauthorized, status)
}

// A token that has already been consumed must be rejected as a replay.
func TestMailgunWebhook_RejectsReplayedToken(t *testing.T) {
	const key = "key"
	h := NewMailgunWebhookHandler(nil, &Config{MailgunSigningKey: key}, discardLogger())

	fresh := strconv.FormatInt(time.Now().Unix(), 10)
	require.True(t, h.replay.consume("tok-replay"), "simulate the first, legitimate use")

	payload := buildWebhookPayload(t, signMailgunBody(key, fresh, "tok-replay"), fresh, "tok-replay", "evt-replay")
	_, err := invokeWebhook(t, h, payload)
	require.Error(t, err)
	status, _ := apperror.ToHTTPError(err)
	require.Equal(t, http.StatusUnauthorized, status)
}

// webhookWithRateLimits builds an Echo app whose route carries the limiter. The
// error handler maps apperror statuses so 401/429 are observable over HTTP in
// the same way the real server presents them.
func webhookWithRateLimits(cfg *Config) *echo.Echo {
	h := NewMailgunWebhookHandler(nil, cfg, discardLogger())
	e := echo.New()
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		status, body := apperror.ToHTTPError(err)
		_ = c.JSON(status, body)
	}
	RegisterWebhookRoutes(e, h)
	return e
}

func signedRequest(t *testing.T, remoteAddr, signature, ts, token, eventID string) *http.Request {
	t.Helper()
	payload := buildWebhookPayload(t, signature, ts, token, eventID)
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/mailgun", strings.NewReader(payload))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.RemoteAddr = remoteAddr
	return req
}

// A flood from one IP must be shed with 429 once its burst is exhausted, and
// the limiter must run before signature verification (the excess requests are
// rejected without hashing).
func TestMailgunWebhookRateLimit_TriggersPerIP(t *testing.T) {
	e := webhookWithRateLimits(&Config{
		MailgunSigningKey:              "key",
		MailgunWebhookRatePerMin:       60,
		MailgunWebhookRateBurst:        3,
		MailgunWebhookGlobalRatePerMin: 100000,
		MailgunWebhookGlobalRateBurst:  100000,
	})
	now := time.Now()
	ts := strconv.FormatInt(now.Unix(), 10)

	saw429 := false
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, signedRequest(t, "203.0.113.7:1234", "deadbeef", ts, "tok", "evt"))
		if rec.Code == http.StatusTooManyRequests {
			saw429 = true
			continue
		}
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	}
	require.True(t, saw429, "the per-IP burst must eventually return 429")
}

// Traffic within the configured limits must be processed normally (never 429).
func TestMailgunWebhookRateLimit_AllowsNormalTraffic(t *testing.T) {
	e := webhookWithRateLimits(&Config{
		MailgunSigningKey:              "key",
		MailgunWebhookRatePerMin:       6000,
		MailgunWebhookRateBurst:        10,
		MailgunWebhookGlobalRatePerMin: 100000,
		MailgunWebhookGlobalRateBurst:  100000,
	})
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, signedRequest(t, "203.0.113.9:1234", "deadbeef", ts, "tok", "evt"))
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	}
}

// The global limiter backstops spoofed per-IP keys: distinct client IPs must
// still hit the global cap.
func TestMailgunWebhookRateLimit_GlobalBackstop(t *testing.T) {
	e := webhookWithRateLimits(&Config{
		MailgunSigningKey:              "key",
		MailgunWebhookRatePerMin:       100000,
		MailgunWebhookRateBurst:        100000,
		MailgunWebhookGlobalRatePerMin: 60,
		MailgunWebhookGlobalRateBurst:  2,
	})
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	saw429 := false
	for i := 0; i < 5; i++ {
		addr := "198.51.100." + strconv.Itoa(i+1) + ":1234"
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, signedRequest(t, addr, "deadbeef", ts, "tok", "evt"))
		if rec.Code == http.StatusTooManyRequests {
			saw429 = true
		}
	}
	require.True(t, saw429, "the global limiter must trigger across distinct IPs")
}

// buildWebhookPayloadEvent renders an envelope for a specific event type and
// message id, so a test can forge a body onto a captured signature.
func buildWebhookPayloadEvent(t *testing.T, signature, ts, token, eventID, event, messageID string) string {
	t.Helper()
	payload := map[string]any{
		"signature": map[string]string{
			"timestamp": ts,
			"token":     token,
			"signature": signature,
		},
		"event-data": map[string]any{
			"event":     event,
			"id":        eventID,
			"timestamp": 1700000000.5,
			"recipient": "user@example.com",
			"message": map[string]any{
				"headers": map[string]string{"message-id": messageID},
			},
		},
	}
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	return string(b)
}

// A captured viable token re-paired with a forged body after its first use must
// be rejected and must not change the matched job's delivery state (#1222).
func TestMailgunWebhook_BodySwapOnReplayedTokenRejected(t *testing.T) {
	db := connectEmailTestDB(t)
	cfg := &Config{MailgunSigningKey: "signing-key"}
	h := NewMailgunWebhookHandler(db, cfg, discardLogger())

	jobID := seedSentJob(t, db, "<msg-swap@example.com>")
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	token := "tok-swap"

	legit := buildWebhookPayloadEvent(t,
		signMailgunBody(cfg.MailgunSigningKey, ts, token), ts, token,
		"evt-legit", "delivered", "msg-swap@example.com")
	rec, err := invokeWebhook(t, h, legit)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	status, _ := jobDeliveryStatus(t, db, jobID)
	require.Equal(t, "delivered", status)

	forged := buildWebhookPayloadEvent(t,
		signMailgunBody(cfg.MailgunSigningKey, ts, token), ts, token,
		"evt-forged", "complained", "msg-swap@example.com")
	_, err = invokeWebhook(t, h, forged)
	require.Error(t, err)
	code, _ := apperror.ToHTTPError(err)
	require.Equal(t, http.StatusUnauthorized, code)

	status, _ = jobDeliveryStatus(t, db, jobID)
	require.Equal(t, "delivered", status, "a forged body must not change the delivery status")
	require.Equal(t, 0, countEmailLogs(t, db, "evt-forged"), "the forged event is never recorded")
}
