package asr

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNewDispatch(t *testing.T) {
	tr, err := New(Config{API: APIOpenAITranscriptions})
	if err != nil {
		t.Fatalf("New(openai-transcriptions) error = %v", err)
	}
	if tr == nil {
		t.Fatal("New(openai-transcriptions) returned nil transcriber")
	}
	if _, err := New(Config{API: API("bogus")}); err == nil {
		t.Error("New(bogus) expected error, got nil")
	}
}

func TestOpenAITranscribeRequestShape(t *testing.T) {
	var (
		gotMethod   string
		gotPath     string
		gotAuth     string
		gotCustom   string
		gotCType    string
		gotForm     *multipart.Form
		gotFName    string
		gotFContent []byte
	)

	audio := []byte("fake-audio-bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCustom = r.Header.Get("X-Custom")
		gotCType = r.Header.Get("Content-Type")

		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			return
		}
		gotForm = r.MultipartForm
		files := r.MultipartForm.File["file"]
		if len(files) != 1 {
			t.Errorf("file parts = %d, want 1", len(files))
			return
		}
		gotFName = files[0].Filename
		f, err := files[0].Open()
		if err != nil {
			t.Errorf("open file: %v", err)
			return
		}
		defer f.Close()
		gotFContent, _ = io.ReadAll(f)

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"ok"}`)
	}))
	defer srv.Close()

	tr, err := New(Config{
		API:     APIOpenAITranscriptions,
		BaseURL: srv.URL,
		APIKey:  "test-key",
		Model:   "whisper-custom",
		Headers: map[string]string{"X-Custom": "custom-val"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Transcribe(context.Background(), audio, Options{
		Filename:       "recording.wav",
		Language:       "en",
		ResponseFormat: "verbose_json",
		Prompt:         "technical",
	}); err != nil {
		t.Fatal(err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/audio/transcriptions" {
		t.Errorf("path = %q, want /audio/transcriptions", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", gotAuth)
	}
	if gotCustom != "custom-val" {
		t.Errorf("X-Custom = %q, want custom-val", gotCustom)
	}
	if !strings.HasPrefix(gotCType, "multipart/form-data;") {
		t.Errorf("Content-Type = %q, want multipart/form-data", gotCType)
	}

	vals := gotForm.Value
	if got := vals["model"]; len(got) != 1 || got[0] != "whisper-custom" {
		t.Errorf("model = %v, want [whisper-custom]", got)
	}
	if got := vals["language"]; len(got) != 1 || got[0] != "en" {
		t.Errorf("language = %v, want [en]", got)
	}
	if got := vals["response_format"]; len(got) != 1 || got[0] != "verbose_json" {
		t.Errorf("response_format = %v, want [verbose_json]", got)
	}
	if got := vals["prompt"]; len(got) != 1 || got[0] != "technical" {
		t.Errorf("prompt = %v, want [technical]", got)
	}
	if gotFName != "recording.wav" {
		t.Errorf("file filename = %q, want recording.wav", gotFName)
	}
	if string(gotFContent) != string(audio) {
		t.Errorf("file content = %q, want %q", gotFContent, audio)
	}
}

func TestOpenAITranscribeOmitsEmptyFields(t *testing.T) {
	var gotForm *multipart.Form
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			return
		}
		gotForm = r.MultipartForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"ok"}`)
	}))
	defer srv.Close()

	tr, err := New(Config{API: APIOpenAITranscriptions, BaseURL: srv.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Transcribe(context.Background(), []byte("x"), Options{}); err != nil {
		t.Fatal(err)
	}

	vals := gotForm.Value
	if got := vals["language"]; len(got) != 0 {
		t.Errorf("language should be absent, got %v", got)
	}
	if got := vals["response_format"]; len(got) != 0 {
		t.Errorf("response_format should be absent, got %v", got)
	}
	if got := vals["prompt"]; len(got) != 0 {
		t.Errorf("prompt should be absent, got %v", got)
	}
	if got := vals["model"]; len(got) != 1 || got[0] != "m" {
		t.Errorf("model = %v, want [m]", got)
	}
}

func TestOpenAITranscribeJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"hello world","language":"en","duration":1.5}`)
	}))
	defer srv.Close()

	tr, err := New(Config{API: APIOpenAITranscriptions, BaseURL: srv.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := tr.Transcribe(context.Background(), []byte("x"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "hello world" {
		t.Errorf("Text = %q, want hello world", got.Text)
	}
	if got.Language != "en" {
		t.Errorf("Language = %q, want en", got.Language)
	}
	if got.Duration != 1.5 {
		t.Errorf("Duration = %v, want 1.5", got.Duration)
	}
}

func TestOpenAITranscribeTextResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, " raw text\n")
	}))
	defer srv.Close()

	tr, err := New(Config{API: APIOpenAITranscriptions, BaseURL: srv.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := tr.Transcribe(context.Background(), []byte("x"), Options{ResponseFormat: "text"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "raw text" {
		t.Errorf("Text = %q, want raw text", got.Text)
	}
}

func TestOpenAITranscribeNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	defer srv.Close()

	tr, err := New(Config{API: APIOpenAITranscriptions, BaseURL: srv.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = tr.Transcribe(context.Background(), []byte("x"), Options{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want containing 500", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %q, want containing boom", err)
	}
}

func TestOpenAITranscribeEmptyAudio(t *testing.T) {
	var called atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
	}))
	defer srv.Close()

	tr, err := New(Config{API: APIOpenAITranscriptions, BaseURL: srv.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = tr.Transcribe(context.Background(), nil, Options{})
	if err == nil {
		t.Fatal("expected error for empty audio, got nil")
	}
	if !strings.Contains(err.Error(), "empty audio") {
		t.Errorf("error = %q, want containing \"empty audio\"", err)
	}
	if called.Load() {
		t.Error("handler should not be called for empty audio")
	}
}

func TestOpenAITranscriberEndpointSuffix(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"ok"}`)
	}))
	defer srv.Close()

	base := srv.URL + "/audio/transcriptions/"
	tr, err := New(Config{API: APIOpenAITranscriptions, BaseURL: base, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Transcribe(context.Background(), []byte("x"), Options{}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/audio/transcriptions" {
		t.Errorf("path = %q, want /audio/transcriptions", gotPath)
	}
}

func TestOpenAITranscribePartContentType(t *testing.T) {
	cases := []struct {
		name     string
		mimeType string
		want     string
	}{
		{name: "declared mime type is carried on the part", mimeType: "audio/mpeg", want: "audio/mpeg"},
		{name: "unset mime type falls back to octet-stream", mimeType: "", want: "application/octet-stream"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var (
				gotType string
				gotName string
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseMultipartForm(32 << 20); err != nil {
					t.Errorf("ParseMultipartForm: %v", err)
					return
				}
				files := r.MultipartForm.File["file"]
				if len(files) != 1 {
					t.Errorf("file parts = %d, want 1", len(files))
					return
				}
				gotType = files[0].Header.Get("Content-Type")
				gotName = files[0].Filename
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"text":"ok"}`)
			}))
			defer srv.Close()

			tr, err := New(Config{API: APIOpenAITranscriptions, BaseURL: srv.URL, Model: "m"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tr.Transcribe(context.Background(), []byte("x"), Options{Filename: "a.mp3", MIMEType: c.mimeType}); err != nil {
				t.Fatal(err)
			}
			if gotType != c.want {
				t.Errorf("part Content-Type = %q, want %q", gotType, c.want)
			}
			if gotName != "a.mp3" {
				t.Errorf("part filename = %q, want a.mp3", gotName)
			}
		})
	}
}

func TestDefaultModelSentWhenEmpty(t *testing.T) {
	var gotForm *multipart.Form
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			return
		}
		gotForm = r.MultipartForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"ok"}`)
	}))
	defer srv.Close()

	tr, err := New(Config{API: APIOpenAITranscriptions, BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Transcribe(context.Background(), []byte("x"), Options{}); err != nil {
		t.Fatal(err)
	}
	vals := gotForm.Value
	if got := vals["model"]; len(got) != 1 || got[0] != "whisper-1" {
		t.Errorf("model = %v, want [whisper-1]", got)
	}
}
