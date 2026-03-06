package apperror

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestHTTPErrorHandler_AppError(t *testing.T) {
	e := echo.New()
	log := slog.Default()
	handler := HTTPErrorHandler(log)

	// Test with custom app error
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	appErr := NewBadRequest("invalid input")
	handler(appErr, c)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	errObj := resp["error"].(map[string]any)
	if errObj["code"] != "bad_request" {
		t.Errorf("Code = %v, want bad_request", errObj["code"])
	}
	if errObj["message"] != "invalid input" {
		t.Errorf("Message = %v, want 'invalid input'", errObj["message"])
	}
}

func TestHTTPErrorHandler_EchoError(t *testing.T) {
	e := echo.New()
	log := slog.Default()
	handler := HTTPErrorHandler(log)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Test with Echo HTTP error (string message)
	echoErr := echo.NewHTTPError(http.StatusNotFound, "resource not found")
	handler(echoErr, c)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	errObj := resp["error"].(map[string]any)
	if errObj["code"] != "not_found" {
		t.Errorf("Code = %v, want not_found", errObj["code"])
	}
	if errObj["message"] != "resource not found" {
		t.Errorf("Message = %v, want 'resource not found'", errObj["message"])
	}
}

func TestHTTPErrorHandler_EchoError_AllStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantCode   string
	}{
		{"unauthorized", http.StatusUnauthorized, "unauthorized"},
		{"forbidden", http.StatusForbidden, "forbidden"},
		{"not_found", http.StatusNotFound, "not_found"},
		{"bad_request", http.StatusBadRequest, "bad_request"},
		{"conflict", http.StatusConflict, "conflict"},
		{"unprocessable_entity", http.StatusUnprocessableEntity, "validation_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			log := slog.Default()
			handler := HTTPErrorHandler(log)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			echoErr := echo.NewHTTPError(tt.status, "test message")
			handler(echoErr, c)

			if rec.Code != tt.status {
				t.Errorf("Status = %d, want %d", rec.Code, tt.status)
			}

			var resp map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Failed to parse response: %v", err)
			}

			errObj := resp["error"].(map[string]any)
			if errObj["code"] != tt.wantCode {
				t.Errorf("Code = %v, want %v", errObj["code"], tt.wantCode)
			}
		})
	}
}

func TestHTTPErrorHandler_StructuredMessage(t *testing.T) {
	e := echo.New()
	log := slog.Default()
	handler := HTTPErrorHandler(log)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Test with structured error map (like from RequireScopes middleware)
	structuredMsg := map[string]any{
		"error": map[string]any{
			"code":    "insufficient_scope",
			"message": "Missing required scope: admin",
			"details": []string{"admin"},
		},
	}
	echoErr := echo.NewHTTPError(http.StatusForbidden, structuredMsg)
	handler(echoErr, c)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	errObj := resp["error"].(map[string]any)
	if errObj["code"] != "insufficient_scope" {
		t.Errorf("Code = %v, want insufficient_scope", errObj["code"])
	}
	if errObj["message"] != "Missing required scope: admin" {
		t.Errorf("Message = %v, want 'Missing required scope: admin'", errObj["message"])
	}
}

func TestHTTPErrorHandler_InternalError(t *testing.T) {
	e := echo.New()
	log := slog.Default()
	handler := HTTPErrorHandler(log)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Test with generic error (should be internal server error)
	genericErr := echo.NewHTTPError(http.StatusInternalServerError, "something went wrong")
	handler(genericErr, c)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestHTTPErrorHandler_HeadRequest(t *testing.T) {
	e := echo.New()
	log := slog.Default()
	handler := HTTPErrorHandler(log)

	// Test HEAD request - should return no content
	req := httptest.NewRequest(http.MethodHead, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	appErr := NewNotFound("resource", "123")
	handler(appErr, c)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	// HEAD request should have empty body
	if rec.Body.Len() != 0 {
		t.Errorf("Body should be empty for HEAD request, got %d bytes", rec.Body.Len())
	}
}

func TestHTTPErrorHandler_CommittedResponse(t *testing.T) {
	e := echo.New()
	log := slog.Default()
	handler := HTTPErrorHandler(log)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Simulate committed response by writing something first
	c.Response().WriteHeader(http.StatusOK)
	c.Response().Write([]byte("already written"))

	// Handler should return early without modifying response
	appErr := NewBadRequest("should not appear")
	handler(appErr, c)

	// Status should still be OK (not changed to bad request)
	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d (committed response)", rec.Code, http.StatusOK)
	}
}
