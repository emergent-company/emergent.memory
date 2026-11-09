package documents

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAutoExtractReader is a fixed-outcome ProjectAutoExtractReader.
type fakeAutoExtractReader struct {
	enabled bool
	err     error
}

func (f fakeAutoExtractReader) AutoExtractObjectsForProject(context.Context, string) (bool, error) {
	return f.enabled, f.err
}

func discardHandlerLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestResolveAutoExtract_TriState pins the override semantics of the project
// auto-extract default: an explicit request (true or false) wins; with no
// request the project setting is used; a nil reader, a reader error, or an
// unknown project all resolve to false.
func TestResolveAutoExtract_TriState(t *testing.T) {
	ctx := context.Background()
	log := discardHandlerLog()

	boolPtr := func(v bool) *bool { return &v }

	tests := []struct {
		name      string
		requested *bool
		reader    ProjectAutoExtractReader
		want      bool
	}{
		{"no request, project true => true", nil, fakeAutoExtractReader{enabled: true}, true},
		{"explicit false overrides project true", boolPtr(false), fakeAutoExtractReader{enabled: true}, false},
		{"explicit true overrides project false", boolPtr(true), fakeAutoExtractReader{enabled: false}, true},
		{"no request, project false => false", nil, fakeAutoExtractReader{enabled: false}, false},
		{"nil reader => false", nil, nil, false},
		{"reader error => false", nil, fakeAutoExtractReader{err: errors.New("db down")}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveAutoExtract(ctx, log, tc.reader, "project-1", tc.requested)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestRequestedAutoExtractFromQuery pins presence detection: absence is nil,
// an explicit "false" is a non-nil false (not the same as absence).
func TestRequestedAutoExtractFromQuery(t *testing.T) {
	e := echo.New()

	absent := e.NewContext(httptest.NewRequest("POST", "/api/documents", nil), httptest.NewRecorder())
	assert.Nil(t, requestedAutoExtractFromQuery(absent))

	explicitTrue := e.NewContext(httptest.NewRequest("POST", "/api/documents?autoExtract=true", nil), httptest.NewRecorder())
	got := requestedAutoExtractFromQuery(explicitTrue)
	require.NotNil(t, got)
	assert.True(t, *got)

	explicitFalse := e.NewContext(httptest.NewRequest("POST", "/api/documents?autoExtract=false", nil), httptest.NewRecorder())
	got = requestedAutoExtractFromQuery(explicitFalse)
	require.NotNil(t, got)
	assert.False(t, *got)
}

// TestHandlerEffectiveAutoExtract_UsesProjectDefault ensures the Handler method
// threads the project reader through.
func TestHandlerEffectiveAutoExtract_UsesProjectDefault(t *testing.T) {
	h := &Handler{projectAutoExtract: fakeAutoExtractReader{enabled: true}, log: discardHandlerLog()}
	assert.True(t, h.effectiveAutoExtract(context.Background(), "project-1", nil))

	f := false
	assert.False(t, h.effectiveAutoExtract(context.Background(), "project-1", &f))
}
