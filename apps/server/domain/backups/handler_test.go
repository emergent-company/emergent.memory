package backups

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/labstack/echo/v4"
)

// newImportEchoCtx builds an echo context with an authenticated user.
func newImportEchoCtx(t *testing.T, method, target string, body *bytes.Buffer, contentType string) echo.Context {
	t.Helper()
	e := echo.New()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, body)
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	if contentType != "" {
		req.Header.Set(echo.HeaderContentType, contentType)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "user-1"})
	return c
}

// multipartFileBody builds a multipart/form-data body carrying a `file` part.
func multipartFileBody(t *testing.T, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

// multipartNoFileBody builds a multipart body with only a non-file field, so a
// `file` part is absent.
func multipartNoFileBody(t *testing.T) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormField("other")
	if err != nil {
		t.Fatalf("CreateFormField: %v", err)
	}
	if _, err := fw.Write([]byte("x")); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

func TestParseRetentionDays(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		want      int
		wantError bool
	}{
		{name: "absent defaults to 30", target: "/import", want: 30},
		{name: "valid", target: "/import?retentionDays=45", want: 45},
		{name: "zero", target: "/import?retentionDays=0", wantError: true},
		{name: "too high", target: "/import?retentionDays=366", wantError: true},
		{name: "non-integer", target: "/import?retentionDays=abc", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newImportEchoCtx(t, http.MethodGet, tt.target, nil, "")
			got, aerr := parseRetentionDays(c)
			if tt.wantError {
				if aerr == nil {
					t.Fatalf("expected error, got %d", got)
				}
				if aerr.HTTPStatus != http.StatusBadRequest {
					t.Errorf("status = %d, want 400", aerr.HTTPStatus)
				}
				return
			}
			if aerr != nil {
				t.Fatalf("unexpected error: %v", aerr)
			}
			if got != tt.want {
				t.Errorf("retentionDays = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReadArchiveUpload(t *testing.T) {
	validZip := buildTestArchive(t, testProjectID, "Test Project", testProjectID, true, true, nil)

	t.Run("valid zip", func(t *testing.T) {
		body, ct := multipartFileBody(t, "backup.zip", validZip)
		c := newImportEchoCtx(t, http.MethodPost, "/import", body, ct)
		data, aerr := readArchiveUpload(c)
		if aerr != nil {
			t.Fatalf("unexpected error: %v", aerr)
		}
		if !bytes.Equal(data, validZip) {
			t.Error("returned bytes differ from uploaded bytes")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		body, ct := multipartNoFileBody(t)
		c := newImportEchoCtx(t, http.MethodPost, "/import", body, ct)
		_, aerr := readArchiveUpload(c)
		if aerr == nil {
			t.Fatal("expected error")
		}
		if aerr.HTTPStatus != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", aerr.HTTPStatus)
		}
	})

	t.Run("non-zip bytes", func(t *testing.T) {
		body, ct := multipartFileBody(t, "backup.zip", []byte("this is not a zip archive"))
		c := newImportEchoCtx(t, http.MethodPost, "/import", body, ct)
		_, aerr := readArchiveUpload(c)
		if aerr == nil {
			t.Fatal("expected error")
		}
		if aerr.HTTPStatus != http.StatusUnsupportedMediaType {
			t.Errorf("status = %d, want 415", aerr.HTTPStatus)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		body, ct := multipartFileBody(t, "backup.zip", []byte{})
		c := newImportEchoCtx(t, http.MethodPost, "/import", body, ct)
		_, aerr := readArchiveUpload(c)
		if aerr == nil {
			t.Fatal("expected error")
		}
		if aerr.HTTPStatus != http.StatusUnsupportedMediaType {
			t.Errorf("status = %d, want 415", aerr.HTTPStatus)
		}
	})
}
