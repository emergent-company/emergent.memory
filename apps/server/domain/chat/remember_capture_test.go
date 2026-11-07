package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A share extension capture is a document with an origin, not an anonymous
// remember: source_url is what lets the stored document point back at the
// article instead of only carrying the URL inside its text.
func TestRememberDocumentParams_SharedLinkCarriesOrigin(t *testing.T) {
	params := rememberDocumentParams("p1", "u1", "great read https://example.com/a", "https://example.com/a")

	require.NotNil(t, params.SourceType)
	assert.Equal(t, "share", *params.SourceType)
	require.NotNil(t, params.SourceURL)
	assert.Equal(t, "https://example.com/a", *params.SourceURL)
	require.NotNil(t, params.Filename)
	assert.Equal(t, "shared-link.txt", *params.Filename)
	require.NotNil(t, params.Content)
	assert.Equal(t, "great read https://example.com/a", *params.Content)
}

// An ordinary remember (no source URL, e.g. the CLI or web chat) keeps the
// manual document shape it has always had.
func TestRememberDocumentParams_PlainTextStaysManual(t *testing.T) {
	params := rememberDocumentParams("p1", "u1", "task: ship the share sheet", "")

	require.NotNil(t, params.SourceType)
	assert.Equal(t, "manual", *params.SourceType)
	assert.Nil(t, params.SourceURL)
	require.NotNil(t, params.Filename)
	assert.Equal(t, "remember.txt", *params.Filename)
}

// Surrounding whitespace in a supplied origin must not turn a real link into a
// non-empty-but-bogus source, nor a blank one into a share.
func TestRememberDocumentParams_OriginIsTrimmed(t *testing.T) {
	params := rememberDocumentParams("p1", "u1", "note", "  https://example.com/x  ")
	require.NotNil(t, params.SourceURL)
	assert.Equal(t, "https://example.com/x", *params.SourceURL)

	blank := rememberDocumentParams("p1", "u1", "note", "   ")
	require.NotNil(t, blank.SourceType)
	assert.Equal(t, "manual", *blank.SourceType)
	assert.Nil(t, blank.SourceURL)
}

func TestRememberDocumentParams_NoUserKeepsAttributionEmpty(t *testing.T) {
	params := rememberDocumentParams("p1", "", "note", "")
	assert.Nil(t, params.CreatedByUserID)
}

// A document photo converts to OCR text, and that text wins over the comment.
func TestRememberFileText_ConvertedContentWins(t *testing.T) {
	content := "  INVOICE 123 — total 42.00  \n"
	assert.Equal(t, "INVOICE 123 — total 42.00", rememberFileText(&content, "my comment"))
}

// A photo with no text in it still captures what the user said about it.
func TestRememberFileText_FallsBackToGuide(t *testing.T) {
	empty := "   \n\t "
	assert.Equal(t, "my comment", rememberFileText(&empty, "  my comment  "))
	assert.Equal(t, "my comment", rememberFileText(nil, "my comment"))
}

// Neither converted text nor a comment means there is genuinely nothing to
// remember, which the handler turns into no_content.
func TestRememberFileText_NothingToRemember(t *testing.T) {
	empty := ""
	assert.Equal(t, "", rememberFileText(&empty, ""))
	assert.Equal(t, "", rememberFileText(nil, "   "))
}
