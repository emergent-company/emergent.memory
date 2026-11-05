package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

func TestNormalizeInviteEmails(t *testing.T) {
	t.Run("trims and dedupes case-insensitively", func(t *testing.T) {
		got, err := normalizeInviteEmails([]string{" alice@example.com ", "ALICE@example.com", "bob@example.com"})
		require.NoError(t, err)
		assert.Equal(t, []string{"alice@example.com", "bob@example.com"}, got)
	})

	t.Run("ignores blank entries", func(t *testing.T) {
		got, err := normalizeInviteEmails([]string{"", "   ", "\t"})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("nil input", func(t *testing.T) {
		got, err := normalizeInviteEmails(nil)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("rejects a malformed address", func(t *testing.T) {
		_, err := normalizeInviteEmails([]string{"alice@example.com", "not-an-email"})
		require.Error(t, err)
		var appErr *apperror.Error
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusUnprocessableEntity, appErr.HTTPStatus)
		assert.Equal(t, "invalid_email", appErr.Code)
	})
}

// TestHandleCreateShareInstanceInvalidEmail proves a malformed recipient is
// rejected at the HTTP boundary before any instance or token is created.
func TestHandleCreateShareInstanceInvalidEmail(t *testing.T) {
	store := newFakeShareStore()
	tok := &fakeTokenSvc{createToken: "emt_secret", createID: "tok-1"}
	h := newTestHandler(store, tok, &fakeAgentDir{})

	c, _ := newJSONContext(t, http.MethodPost,
		`{"name":"Team A","tools":["entity-search"],"emails":["not-an-email"]}`,
		adminUser(), []string{"projectId"}, []string{"proj-1"})
	err := h.HandleCreateShareInstance(*c)
	assertAppError(t, err, 422)
	assert.Empty(t, store.byID, "no instance may be created for a rejected invite list")
	assert.Zero(t, tok.createCalls, "no token may be minted for a rejected invite list")
}

// TestHandleCreateShareInstanceEmailUnavailable proves the share still succeeds
// when the invite cannot be queued: the response carries the key plus a
// delivery problem the UI can surface.
func TestHandleCreateShareInstanceEmailUnavailable(t *testing.T) {
	store := newFakeShareStore()
	tok := &fakeTokenSvc{createToken: "emt_secret", createID: "tok-1"}
	h := newTestHandler(store, tok, &fakeAgentDir{}) // nil email service

	c, rec := newJSONContext(t, http.MethodPost,
		`{"name":"Team A","tools":["entity-search"],"emails":["alice@example.com"]}`,
		adminUser(), []string{"projectId"}, []string{"proj-1"})
	require.NoError(t, h.HandleCreateShareInstance(*c))
	require.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateShareInstanceResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "emt_secret", resp.Token)
	assert.Empty(t, resp.EmailsSent)
	assert.NotEmpty(t, resp.EmailError)
	assert.NotEmpty(t, store.byID)
}

// TestHandleRotateShareInstanceInvalidEmail proves the optional rotate body is
// validated before the token is rotated.
func TestHandleRotateShareInstanceInvalidEmail(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "old"}
	tok := &fakeTokenSvc{regenerateID: "new", regenerateToken: "emt_new"}
	h := newTestHandler(store, tok, &fakeAgentDir{})

	c, _ := newJSONContext(t, http.MethodPost, `{"emails":["nope"]}`, adminUser(), []string{"projectId", "id"}, []string{"proj-1", "i1"})
	err := h.HandleRotateShareInstance(*c)
	assertAppError(t, err, 422)
	assert.Equal(t, "old", store.byID["i1"].TokenID, "token must not rotate on a rejected invite list")
}

// --- ad-hoc invite of an already-created share ---

// TestRevealShareInstanceKey covers the recovery path the invite action uses:
// the plaintext key is read back from encrypted storage, never re-minted.
func TestRevealShareInstanceKey(t *testing.T) {
	newStore := func() *fakeShareStore {
		s := newFakeShareStore()
		s.byID["i1"] = &MCPShareInstance{ID: "i1", ProjectID: "proj", Name: "Team A", TokenID: "tok-1"}
		return s
	}

	t.Run("returns the stored key without rotating", func(t *testing.T) {
		store := newStore()
		tok := &fakeTokenSvc{getByIDToken: "emt_existing_key", getByIDTokenID: "tok-1"}
		svc := newTestService(store, tok, &fakeAgentDir{})

		key, err := svc.RevealShareInstanceKey(context.Background(), "proj", "user", "i1")
		require.NoError(t, err)
		assert.Equal(t, "emt_existing_key", key)
		assert.False(t, tok.regenerateCalled, "revealing a key must never rotate it")
		assert.Equal(t, "tok-1", store.byID["i1"].TokenID)
	})

	t.Run("not recoverable when no ciphertext was stored", func(t *testing.T) {
		store := newStore()
		tok := &fakeTokenSvc{getByIDToken: ""}
		svc := newTestService(store, tok, &fakeAgentDir{})

		_, err := svc.RevealShareInstanceKey(context.Background(), "proj", "user", "i1")
		var appErr *apperror.Error
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusUnprocessableEntity, appErr.HTTPStatus)
		assert.Equal(t, "not_recoverable", appErr.Code)
		assert.False(t, tok.regenerateCalled)
	})

	t.Run("not recoverable when the token row is gone", func(t *testing.T) {
		store := newStore()
		svc := newTestService(store, &fakeTokenSvc{getByIDNil: true}, &fakeAgentDir{})

		_, err := svc.RevealShareInstanceKey(context.Background(), "proj", "user", "i1")
		var appErr *apperror.Error
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, "not_recoverable", appErr.Code)
	})

	t.Run("requires a project admin", func(t *testing.T) {
		svc := newTestService(newStore(), &fakeTokenSvc{role: "project_viewer", getByIDToken: "emt_x"}, &fakeAgentDir{})

		_, err := svc.RevealShareInstanceKey(context.Background(), "proj", "user", "i1")
		var appErr *apperror.Error
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus)
	})

	t.Run("rejects a revoked share", func(t *testing.T) {
		store := newStore()
		revokedAt := time.Now().UTC()
		store.byID["i1"].RevokedAt = &revokedAt
		svc := newTestService(store, &fakeTokenSvc{getByIDToken: "emt_x"}, &fakeAgentDir{})

		_, err := svc.RevealShareInstanceKey(context.Background(), "proj", "user", "i1")
		var appErr *apperror.Error
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusConflict, appErr.HTTPStatus)
		assert.Equal(t, "share_instance_revoked", appErr.Code)
	})

	t.Run("unknown share", func(t *testing.T) {
		svc := newTestService(newFakeShareStore(), &fakeTokenSvc{getByIDToken: "emt_x"}, &fakeAgentDir{})

		_, err := svc.RevealShareInstanceKey(context.Background(), "proj", "user", "missing")
		var appErr *apperror.Error
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
	})
}

// TestHandleInviteShareInstanceEmailsExistingKey proves the invite endpoint
// sends the share's CURRENT key (its prefix identifies it) and never mints a
// replacement, so existing clients keep working.
func TestHandleInviteShareInstanceEmailsExistingKey(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "tok-1"}
	tok := &fakeTokenSvc{getByIDToken: "emt_existing_key_value", getByIDTokenID: "tok-1"}
	h := newTestHandler(store, tok, &fakeAgentDir{})

	c, rec := newJSONContext(t, http.MethodPost, `{"emails":["alice@example.com"]}`,
		adminUser(), []string{"projectId", "id"}, []string{"proj-1", "i1"})
	require.NoError(t, h.HandleInviteShareInstance(*c))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp InviteShareInstanceResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "emt_existing", resp.TokenPrefix, "prefix must name the existing key")
	assert.False(t, tok.regenerateCalled, "an invite must not rotate the key")
	assert.Equal(t, "tok-1", store.byID["i1"].TokenID)
	// The test handler has no email service, so delivery is reported, not fatal.
	assert.NotEmpty(t, resp.EmailError)
	assert.Empty(t, resp.EmailsSent)
}

// TestHandleInviteShareInstanceInvalidEmail proves address validation runs
// before the key is recovered.
func TestHandleInviteShareInstanceInvalidEmail(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "tok-1"}
	tok := &fakeTokenSvc{getByIDToken: "emt_existing_key_value"}
	h := newTestHandler(store, tok, &fakeAgentDir{})

	c, _ := newJSONContext(t, http.MethodPost, `{"emails":["nope"]}`,
		adminUser(), []string{"projectId", "id"}, []string{"proj-1", "i1"})
	err := h.HandleInviteShareInstance(*c)
	assertAppError(t, err, 422)
	assert.Zero(t, tok.getByIDCalls, "a rejected invite list must not recover the key")
}

// TestHandleInviteShareInstanceNoRecipients proves an empty list is a client
// error rather than a silent no-op.
func TestHandleInviteShareInstanceNoRecipients(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "tok-1"}
	tok := &fakeTokenSvc{getByIDToken: "emt_existing_key_value"}
	h := newTestHandler(store, tok, &fakeAgentDir{})

	for _, body := range []string{`{"emails":[]}`, `{}`, `{"emails":["","  "]}`} {
		c, _ := newJSONContext(t, http.MethodPost, body,
			adminUser(), []string{"projectId", "id"}, []string{"proj-1", "i1"})
		err := h.HandleInviteShareInstance(*c)
		assertAppError(t, err, 400)
	}
	assert.Zero(t, tok.getByIDCalls)
}
