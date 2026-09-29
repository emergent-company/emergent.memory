package email

import (
	"context"
	"database/sql"
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
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

func connectEmailTestDB(t *testing.T) *bun.DB {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "email")
	t.Cleanup(tdb.Close)
	return tdb.DB
}

// seedSentJob inserts a sent email job with the given stored Mailgun message id
// and returns its id.
func seedSentJob(t *testing.T, db bun.IDB, messageID string) string {
	t.Helper()
	var id string
	err := db.NewRaw(`INSERT INTO kb.email_jobs
			(template_name, to_email, subject, status, mailgun_message_id, processed_at)
		VALUES ('project-invitation', 'user@example.com', 'Hi', 'sent', ?, now())
		RETURNING id`, messageID).Scan(context.Background(), &id)
	require.NoError(t, err)
	return id
}

func countEmailLogs(t *testing.T, db bun.IDB, eventID string) int {
	t.Helper()
	var n int
	err := db.NewRaw(`SELECT count(*) FROM kb.email_logs WHERE mailgun_event_id = ?`, eventID).
		Scan(context.Background(), &n)
	require.NoError(t, err)
	return n
}

func jobDeliveryStatus(t *testing.T, db bun.IDB, jobID string) (string, sql.NullTime) {
	t.Helper()
	var status sql.NullString
	var at sql.NullTime
	err := db.NewRaw(`SELECT delivery_status, delivery_status_at FROM kb.email_jobs WHERE id = ?`, jobID).
		Scan(context.Background(), &status, &at)
	require.NoError(t, err)
	return status.String, at
}

// A signed bounce mapped through the store updates the originating job and is
// recorded once; a Mailgun retry of the same event id is a no-op, and an older
// event must not regress the newest state.
func TestDeliveryStore_IngestIsIdempotentAndNewestWins(t *testing.T) {
	db := connectEmailTestDB(t)
	store := NewDeliveryStore(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()

	// Stored id carries angle brackets (SDK send response); the webhook header
	// does not — the store must normalise both.
	jobID := seedSentJob(t, db, "<msg-1@example.com>")

	newest := time.Now().UTC().Truncate(time.Second)
	bounce := DeliveryEvent{
		EventID:   "evt-1",
		EventType: "failed",
		Severity:  "permanent",
		MessageID: "msg-1@example.com",
		Recipient: "user@example.com",
		Timestamp: newest,
		Details:   []byte(`{"event":"failed"}`),
	}

	applied, err := store.IngestDeliveryEvent(ctx, bounce)
	require.NoError(t, err)
	require.True(t, applied, "first delivery applies")

	status, at := jobDeliveryStatus(t, db, jobID)
	require.Equal(t, "bounced", status)
	require.True(t, at.Valid, "delivery_status_at must be set")

	// Retry: same event id must not double-apply.
	applied, err = store.IngestDeliveryEvent(ctx, bounce)
	require.NoError(t, err)
	require.False(t, applied, "retry is a duplicate and does not apply")
	require.Equal(t, 1, countEmailLogs(t, db, "evt-1"))

	// An older, different event must not regress the newer status.
	older := DeliveryEvent{
		EventID:   "evt-2",
		EventType: "delivered",
		MessageID: "msg-1@example.com",
		Timestamp: newest.Add(-time.Hour),
		Details:   []byte(`{"event":"delivered"}`),
	}
	applied, err = store.IngestDeliveryEvent(ctx, older)
	require.NoError(t, err)
	require.True(t, applied, "older event is fresh and audited")
	require.Equal(t, 1, countEmailLogs(t, db, "evt-2"))

	status, at = jobDeliveryStatus(t, db, jobID)
	require.Equal(t, "bounced", status, "older event must not overwrite the newer status")
	require.True(t, at.Valid)
}

// A complaint maps to complained; an unmatched message id is audited with a null
// job id and touches no job.
func TestDeliveryStore_ComplaintAndUnmatchedEvent(t *testing.T) {
	db := connectEmailTestDB(t)
	store := NewDeliveryStore(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()

	jobID := seedSentJob(t, db, "<msg-2@example.com>")

	applied, err := store.IngestDeliveryEvent(ctx, DeliveryEvent{
		EventID:   "evt-complaint",
		EventType: "complained",
		MessageID: "msg-2@example.com",
		Timestamp: time.Now().UTC(),
		Details:   []byte(`{"event":"complained"}`),
	})
	require.NoError(t, err)
	require.True(t, applied)
	status, _ := jobDeliveryStatus(t, db, jobID)
	require.Equal(t, "complained", status)

	applied, err = store.IngestDeliveryEvent(ctx, DeliveryEvent{
		EventID:   "evt-unmatched",
		EventType: "failed",
		Severity:  "permanent",
		MessageID: "does-not-exist@example.com",
		Timestamp: time.Now().UTC(),
		Details:   []byte(`{"event":"failed"}`),
	})
	require.NoError(t, err)
	require.True(t, applied, "unmatched event is still audited")

	var jobRef sql.NullString
	err = db.NewRaw(`SELECT email_job_id FROM kb.email_logs WHERE mailgun_event_id = ?`, "evt-unmatched").
		Scan(ctx, &jobRef)
	require.NoError(t, err)
	require.False(t, jobRef.Valid, "unmatched event records a null job id")
}

// The full webhook path: a validly signed request is accepted with 200; a
// Mailgun retry that re-signs with a fresh timestamp+token is accepted again
// without double-applying; replaying the original envelope (same token) is
// rejected as a replay.
func TestMailgunWebhook_EndToEndAcceptsVerifiedSignature(t *testing.T) {
	db := connectEmailTestDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &Config{MailgunSigningKey: "signing-key"}
	h := NewMailgunWebhookHandler(db, cfg, log)

	jobID := seedSentJob(t, db, "<msg-1@example.com>")

	ts := strconv.FormatInt(time.Now().Unix(), 10)

	send := func(token string) *httptest.ResponseRecorder {
		t.Helper()
		payload := buildWebhookPayload(t, signMailgunBody(cfg.MailgunSigningKey, ts, token), ts, token, "evt-e2e")
		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, "/api/webhooks/mailgun", strings.NewReader(payload))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		require.NoError(t, h.Handle(c))
		return rec
	}

	require.Equal(t, http.StatusOK, send("tok-e2e-1").Code)
	status, _ := jobDeliveryStatus(t, db, jobID)
	require.Equal(t, "bounced", status)

	// Mailgun retries the same event id with a fresh signature token; the event
	// is acknowledged again but not re-applied.
	require.Equal(t, http.StatusOK, send("tok-e2e-2").Code, "retry is acknowledged so Mailgun stops retrying")
	require.Equal(t, 1, countEmailLogs(t, db, "evt-e2e"), "retry does not double-apply")

	// Replaying the original envelope (same token) is refused.
	rec := httptest.NewRecorder()
	replayPayload := buildWebhookPayload(t, signMailgunBody(cfg.MailgunSigningKey, ts, "tok-e2e-1"), ts, "tok-e2e-1", "evt-e2e")
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/mailgun", strings.NewReader(replayPayload))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	err := h.Handle(echo.New().NewContext(req, rec))
	require.Error(t, err)
	statusCode, _ := apperror.ToHTTPError(err)
	require.Equal(t, http.StatusUnauthorized, statusCode)
	require.Equal(t, 1, countEmailLogs(t, db, "evt-e2e"), "replay adds no log row")
}

// A synthetic id is derived when Mailgun omits the event id, and it is stable so
// a retry still dedupes.
func TestDeliveryStore_SyntheticEventIDIsStable(t *testing.T) {
	ev := DeliveryEvent{
		EventType: "failed",
		MessageID: "msg-3@example.com",
		Recipient: "user@example.com",
		Timestamp: time.Unix(1700000000, 0).UTC(),
	}
	require.NotEmpty(t, SyntheticEventID(ev))
	require.Equal(t, SyntheticEventID(ev), SyntheticEventID(ev))
	other := ev
	other.EventType = "complained"
	require.NotEqual(t, SyntheticEventID(ev), SyntheticEventID(other))
}
