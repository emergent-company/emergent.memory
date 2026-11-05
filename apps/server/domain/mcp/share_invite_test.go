package mcp

import (
	"encoding/json"
	"net/http"
	"testing"

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
