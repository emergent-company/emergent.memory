package agents

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestA2AErrorEnvelope_ShapeDomainReason(t *testing.T) {
	err := NewA2AError(A2ACodeTaskNotFound, A2AReasonTaskNotFound, "task not found")
	env := err.Envelope()

	assert.Equal(t, -32001, env.Error.Code)
	assert.Equal(t, "NOT_FOUND", env.Error.Status)
	assert.Equal(t, "task not found", env.Error.Message)
	require.Len(t, env.Error.Details, 1)

	detail := env.Error.Details[0]
	assert.Equal(t, "TASK_NOT_FOUND", detail.Reason)
	assert.Equal(t, A2AErrorDomain, detail.Domain)
	assert.Equal(t, "type.googleapis.com/lf.a2a.v1.TaskNotFoundError", detail.Type)
}

func TestA2AErrorEnvelope_JSONWireShape(t *testing.T) {
	err := NewA2AError(A2ACodeUnsupportedOperation, A2AReasonUnsupportedOperation, "not implemented")
	b, marshalErr := json.Marshal(err.Envelope())
	require.NoError(t, marshalErr)

	j := string(b)
	assert.Contains(t, j, `"error"`)
	assert.Contains(t, j, `"@type"`)
	assert.Contains(t, j, `"reason":"UNSUPPORTED_OPERATION"`)
	assert.Contains(t, j, `"domain":"a2a-protocol.org"`)
}

func TestA2AErrorCode_HTTPStatusMapping(t *testing.T) {
	cases := map[A2AErrorCode]int{
		A2ACodeTaskNotFound:                 http.StatusNotFound,
		A2ACodeTaskNotCancelable:            http.StatusBadRequest,
		A2ACodePushNotificationNotSupported: http.StatusBadRequest,
		A2ACodeUnsupportedOperation:         http.StatusBadRequest,
		A2ACodeInvalidAgentResponse:         http.StatusInternalServerError,
		A2ACodeVersionNotSupported:          http.StatusBadRequest,
	}
	for code, want := range cases {
		assert.Equal(t, want, code.HTTPStatus(), "code %d", code)
	}
}

func TestResolveA2AVersion_Default(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	v, err := ResolveA2AVersion(c)
	assert.Nil(t, err)
	assert.Equal(t, "1.0", v)
}

func TestResolveA2AVersion_Header(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(A2AVersionHeader, "1.0")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	v, err := ResolveA2AVersion(c)
	assert.Nil(t, err)
	assert.Equal(t, "1.0", v)
}

func TestResolveA2AVersion_QueryParam(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?A2A-Version=1.0", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	v, err := ResolveA2AVersion(c)
	assert.Nil(t, err)
	assert.Equal(t, "1.0", v)
}

func TestResolveA2AVersion_Unsupported(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(A2AVersionHeader, "2.0")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	v, err := ResolveA2AVersion(c)
	assert.Equal(t, "", v)
	require.NotNil(t, err)
	assert.Equal(t, A2ACodeVersionNotSupported, err.Code)
	assert.Equal(t, A2AReasonVersionNotSupported, err.Reason)
	assert.Equal(t, http.StatusBadRequest, err.Code.HTTPStatus())
}

func TestWriteA2AError_ContentTypeAndStatus(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := NewA2AError(A2ACodeTaskNotFound, A2AReasonTaskNotFound, "nope")
	require.NoError(t, writeA2AError(c, err))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, A2AContentType, rec.Header().Get(echo.HeaderContentType))

	var body A2AErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, -32001, body.Error.Code)
	assert.Equal(t, "TASK_NOT_FOUND", body.Error.Details[0].Reason)
}
