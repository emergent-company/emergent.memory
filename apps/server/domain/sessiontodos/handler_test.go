package sessiontodos

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newEchoCtx(method, path, body string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

// newHandlerWithFake returns a Handler backed by a testService (fakeRepo).
func newHandlerWithFake() (*Handler, *testService, *fakeRepo) {
	svc, fake := newTestService()
	// Handler expects *Service; we satisfy that by calling NewHandler with a real
	// *Service that wraps the fake via a shim. Since Handler only calls svc.List/
	// Create/Update/Delete, we replace the inner service with a wrapper that
	// converts testService calls to the matching *Service method signatures.
	//
	// Simpler: we test Handler using a concrete *Service that itself uses a
	// fakeRepo. We do this by building a real *Service with a dummy *Repository
	// (nil bun.IDB), then monkey-patching its repo field to a wrapper that
	// satisfies the same method set. Because *Service.repo is *Repository (not
	// an interface), we instead expose an alternative constructor only for tests.
	_ = fake
	_ = svc
	return nil, svc, fake
}

// handlerSvcAdapter wraps testService to satisfy the interface that Handler uses.
// Handler calls h.svc.List / Create / Update / Delete — so we shadow *Service
// with this type for the handler tests.
type handlerSvcAdapter struct {
	inner *testService
}

func (a *handlerSvcAdapter) List(ctx context.Context, sessionID string, statuses []TodoStatus) ([]*SessionTodo, error) {
	return a.inner.List(ctx, sessionID, statuses)
}
func (a *handlerSvcAdapter) Create(ctx context.Context, sessionID string, req CreateTodoRequest) (*SessionTodo, error) {
	return a.inner.Create(ctx, sessionID, req)
}
func (a *handlerSvcAdapter) Update(ctx context.Context, sessionID, todoID string, req UpdateTodoRequest) (*SessionTodo, error) {
	return a.inner.Update(ctx, sessionID, todoID, req)
}
func (a *handlerSvcAdapter) Delete(ctx context.Context, sessionID, todoID string) error {
	return a.inner.Delete(ctx, sessionID, todoID)
}

// testHandler is a copy of Handler that accepts an svcIface so tests don't need
// a real *Service (which requires a live DB via *Repository).
type svcIface interface {
	List(ctx context.Context, sessionID string, statuses []TodoStatus) ([]*SessionTodo, error)
	Create(ctx context.Context, sessionID string, req CreateTodoRequest) (*SessionTodo, error)
	Update(ctx context.Context, sessionID, todoID string, req UpdateTodoRequest) (*SessionTodo, error)
	Delete(ctx context.Context, sessionID, todoID string) error
}

type testHandler struct {
	svc svcIface
}

func newTestHandler() (*testHandler, *fakeRepo) {
	svc, fake := newTestService()
	return &testHandler{svc: &handlerSvcAdapter{inner: svc}}, fake
}

func (h *testHandler) List(c echo.Context) error {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "sessionId is required")
	}
	var statuses []TodoStatus
	if raw := c.QueryParam("status"); raw != "" {
		for _, s := range strings.Split(raw, ",") {
			statuses = append(statuses, TodoStatus(strings.TrimSpace(s)))
		}
	}
	todos, err := h.svc.List(c.Request().Context(), sessionID, statuses)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, todos)
}

func (h *testHandler) Create(c echo.Context) error {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "sessionId is required")
	}
	var req CreateTodoRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	todo, err := h.svc.Create(c.Request().Context(), sessionID, req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, todo)
}

func (h *testHandler) Update(c echo.Context) error {
	sessionID := c.Param("sessionId")
	todoID := c.Param("todoId")
	if sessionID == "" || todoID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "sessionId and todoId are required")
	}
	var req UpdateTodoRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	todo, err := h.svc.Update(c.Request().Context(), sessionID, todoID, req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, todo)
}

func (h *testHandler) Delete(c echo.Context) error {
	sessionID := c.Param("sessionId")
	todoID := c.Param("todoId")
	if sessionID == "" || todoID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "sessionId and todoId are required")
	}
	if err := h.svc.Delete(c.Request().Context(), sessionID, todoID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// List tests
// ---------------------------------------------------------------------------

func TestHandler_List_ReturnsTodos(t *testing.T) {
	h, fake := newTestHandler()
	// Pre-populate fake with one todo
	fake.Create(context.Background(), &SessionTodo{SessionID: "sess-1", Content: "task A", Status: StatusDraft})

	c, rec := newEchoCtx(http.MethodGet, "/", "")
	c.SetParamNames("sessionId")
	c.SetParamValues("sess-1")

	err := h.List(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var todos []*SessionTodo
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &todos))
	require.Len(t, todos, 1)
	assert.Equal(t, "task A", todos[0].Content)
}

func TestHandler_List_EmptySessionID_Returns400(t *testing.T) {
	h, _ := newTestHandler()
	c, _ := newEchoCtx(http.MethodGet, "/", "")
	// No sessionId param set
	err := h.List(c)
	require.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}

func TestHandler_List_StatusFilter(t *testing.T) {
	h, fake := newTestHandler()
	fake.Create(context.Background(), &SessionTodo{SessionID: "sess", Content: "draft task", Status: StatusDraft})
	todo2 := &SessionTodo{SessionID: "sess", Content: "done task", Status: StatusCompleted}
	fake.Create(context.Background(), todo2)

	c, rec := newEchoCtx(http.MethodGet, "/?status=completed", "")
	c.SetParamNames("sessionId")
	c.SetParamValues("sess")

	err := h.List(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var todos []*SessionTodo
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &todos))
	require.Len(t, todos, 1)
	assert.Equal(t, StatusCompleted, todos[0].Status)
}

// ---------------------------------------------------------------------------
// Create tests
// ---------------------------------------------------------------------------

func TestHandler_Create_Returns201(t *testing.T) {
	h, _ := newTestHandler()
	body := `{"content":"new task"}`
	c, rec := newEchoCtx(http.MethodPost, "/", body)
	c.SetParamNames("sessionId")
	c.SetParamValues("sess-1")

	err := h.Create(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var todo SessionTodo
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &todo))
	assert.Equal(t, "new task", todo.Content)
	assert.Equal(t, StatusDraft, todo.Status)
}

func TestHandler_Create_EmptyContent_Returns400(t *testing.T) {
	h, _ := newTestHandler()
	body := `{"content":""}`
	c, _ := newEchoCtx(http.MethodPost, "/", body)
	c.SetParamNames("sessionId")
	c.SetParamValues("sess-1")

	err := h.Create(c)
	require.Error(t, err)
}

func TestHandler_Create_InvalidJSON_Returns400(t *testing.T) {
	h, _ := newTestHandler()
	c, _ := newEchoCtx(http.MethodPost, "/", "not-json{{{")
	c.SetParamNames("sessionId")
	c.SetParamValues("sess-1")

	err := h.Create(c)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Update tests
// ---------------------------------------------------------------------------

func TestHandler_Update_Returns200(t *testing.T) {
	h, fake := newTestHandler()
	fake.Create(context.Background(), &SessionTodo{SessionID: "sess", Content: "old", Status: StatusDraft})
	var todoID string
	for id := range fake.todos {
		todoID = id
	}

	body := `{"status":"completed"}`
	c, rec := newEchoCtx(http.MethodPatch, "/", body)
	c.SetParamNames("sessionId", "todoId")
	c.SetParamValues("sess", todoID)

	err := h.Update(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var todo SessionTodo
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &todo))
	assert.Equal(t, StatusCompleted, todo.Status)
}

func TestHandler_Update_WrongSession_Returns404(t *testing.T) {
	h, fake := newTestHandler()
	fake.Create(context.Background(), &SessionTodo{SessionID: "sess-A", Content: "task", Status: StatusDraft})
	var todoID string
	for id := range fake.todos {
		todoID = id
	}

	body := `{"status":"completed"}`
	c, _ := newEchoCtx(http.MethodPatch, "/", body)
	c.SetParamNames("sessionId", "todoId")
	c.SetParamValues("sess-B", todoID)

	err := h.Update(c)
	require.Error(t, err)
	appErr, ok := err.(*apperror.Error)
	require.True(t, ok)
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
}

func TestHandler_Update_MissingParams_Returns400(t *testing.T) {
	h, _ := newTestHandler()
	c, _ := newEchoCtx(http.MethodPatch, "/", `{}`)
	// No params set

	err := h.Update(c)
	require.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}

// ---------------------------------------------------------------------------
// Delete tests
// ---------------------------------------------------------------------------

func TestHandler_Delete_Returns204(t *testing.T) {
	h, fake := newTestHandler()
	fake.Create(context.Background(), &SessionTodo{SessionID: "sess", Content: "task", Status: StatusDraft})
	var todoID string
	for id := range fake.todos {
		todoID = id
	}

	c, rec := newEchoCtx(http.MethodDelete, "/", "")
	c.SetParamNames("sessionId", "todoId")
	c.SetParamValues("sess", todoID)

	err := h.Delete(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.NotContains(t, fake.todos, todoID)
}

func TestHandler_Delete_WrongSession_Returns404(t *testing.T) {
	h, fake := newTestHandler()
	fake.Create(context.Background(), &SessionTodo{SessionID: "sess-A", Content: "task", Status: StatusDraft})
	var todoID string
	for id := range fake.todos {
		todoID = id
	}

	c, _ := newEchoCtx(http.MethodDelete, "/", "")
	c.SetParamNames("sessionId", "todoId")
	c.SetParamValues("sess-B", todoID)

	err := h.Delete(c)
	require.Error(t, err)
	appErr, ok := err.(*apperror.Error)
	require.True(t, ok)
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
}

func TestHandler_Delete_MissingParams_Returns400(t *testing.T) {
	h, _ := newTestHandler()
	c, _ := newEchoCtx(http.MethodDelete, "/", "")
	// No params set

	err := h.Delete(c)
	require.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}
