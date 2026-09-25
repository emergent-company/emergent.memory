package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ui "github.com/emergent-company/go-daisy/components/ui"
	"github.com/labstack/echo/v4"
)

func TestGraphObjectEmbeddingFieldsParse(t *testing.T) {
	raw := `{"id":"o1","type":"person","embedding_status":"embedded","embedding_updated_at":"2026-09-20T10:00:00Z"}`
	var o GraphObject
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if o.EmbeddingStatus != "embedded" {
		t.Errorf("EmbeddingStatus = %q, want embedded", o.EmbeddingStatus)
	}
	if o.EmbeddingUpdatedAt != "2026-09-20T10:00:00Z" {
		t.Errorf("EmbeddingUpdatedAt = %q, want 2026-09-20T10:00:00Z", o.EmbeddingUpdatedAt)
	}
}

func TestEmbeddingStatusBadgeMapping(t *testing.T) {
	cases := []struct {
		status  string
		label   string
		variant ui.BadgeIntent
	}{
		{"embedded", "Embedded", ui.BadgeSuccess},
		{"pending", "Embedding queued", ui.BadgeNeutral},
		{"processing", "Embedding…", ui.BadgePrimary},
		{"failed", "Embedding failed", ui.BadgeError},
		{"dead_letter", "Embedding failed", ui.BadgeWarning},
		{"missing", "Not embedded", ui.BadgeGhost},
		{"", "", ui.BadgeGhost},
		{"unknown", "", ui.BadgeGhost},
	}
	for _, c := range cases {
		label, variant := embeddingStatusBadge(c.status)
		if label != c.label || variant != c.variant {
			t.Errorf("embeddingStatusBadge(%q) = (%q, %v), want (%q, %v)", c.status, label, variant, c.label, c.variant)
		}
	}
}

func TestWorkerStateLabel(t *testing.T) {
	cases := []struct {
		name   string
		status EmbeddingWorkerStatus
		label  string
	}{
		{"running", EmbeddingWorkerStatus{Running: true}, "running"},
		{"paused", EmbeddingWorkerStatus{Paused: true}, "paused"},
		{"paused wins when both flags set", EmbeddingWorkerStatus{Running: true, Paused: true}, "paused"},
		{"idle", EmbeddingWorkerStatus{}, "idle"},
	}
	for _, c := range cases {
		label, _ := workerStateLabel(c.status)
		if label != c.label {
			t.Errorf("workerStateLabel(%+v) = %q, want %q", c.status, label, c.label)
		}
	}
}

func TestGetEmbeddingProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings/progress" {
			t.Errorf("path = %q, want /api/embeddings/progress", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("X-Project-ID"); got != "proj" {
			t.Errorf("X-Project-ID = %q, want proj", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"objects":{"pending":1,"processing":2,"completed":3,"failed":4,"staleFailed":11,"deadLetter":5},"relationships":{"pending":6,"processing":7,"completed":8,"failed":9,"staleFailed":12,"deadLetter":10}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	p, err := m.GetEmbeddingProgress(context.Background())
	if err != nil {
		t.Fatalf("GetEmbeddingProgress: %v", err)
	}
	if p.Objects.Pending != 1 || p.Objects.Processing != 2 || p.Objects.Completed != 3 || p.Objects.Failed != 4 || p.Objects.StaleFailed != 11 || p.Objects.DeadLetter != 5 {
		t.Errorf("objects = %+v", p.Objects)
	}
	if p.Relationships.Pending != 6 || p.Relationships.StaleFailed != 12 || p.Relationships.DeadLetter != 10 {
		t.Errorf("relationships = %+v", p.Relationships)
	}
}

func TestGetEmbeddingStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings/status" {
			t.Errorf("path = %q, want /api/embeddings/status", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"objects":{"running":true,"paused":false},"relationships":{"running":false,"paused":true},"sweep":{"running":false,"paused":false},"config":{"batch_size":100,"concurrency":4,"interval_ms":500,"stale_minutes":30,"enable_adaptive_scaling":true,"min_concurrency":1,"max_concurrency":8}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	st, err := m.GetEmbeddingStatus(context.Background())
	if err != nil {
		t.Fatalf("GetEmbeddingStatus: %v", err)
	}
	if !st.Objects.Running || st.Objects.Paused {
		t.Errorf("objects = %+v", st.Objects)
	}
	if st.Relationships.Running || !st.Relationships.Paused {
		t.Errorf("relationships = %+v", st.Relationships)
	}
	if st.Config.BatchSize != 100 || st.Config.Concurrency != 4 || st.Config.IntervalMs != 500 || st.Config.StaleMinutes != 30 || !st.Config.EnableAdaptiveScaling || st.Config.MinConcurrency != 1 || st.Config.MaxConcurrency != 8 {
		t.Errorf("config = %+v", st.Config)
	}
}

func TestGetEffectiveModelConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/projects/proj/model-config/effective" {
			t.Errorf("path = %q, want /api/v1/projects/proj/model-config/effective", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"generativeModel":"openai/gpt-x","generativeModelSource":"project","embeddingModel":"openai/embed-x","embeddingModelSource":"provider"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	mc, err := m.GetEffectiveModelConfig(context.Background())
	if err != nil {
		t.Fatalf("GetEffectiveModelConfig: %v", err)
	}
	if mc.EmbeddingModel != "openai/embed-x" || mc.EmbeddingModelSource != "provider" {
		t.Errorf("embedding = %q/%q, want openai/embed-x/provider", mc.EmbeddingModel, mc.EmbeddingModelSource)
	}
	if mc.GenerativeModel != "openai/gpt-x" || mc.GenerativeModelSource != "project" {
		t.Errorf("generative = %q/%q, want openai/gpt-x/project", mc.GenerativeModel, mc.GenerativeModelSource)
	}
}

func TestEmbeddingsRoute(t *testing.T) {
	newServer := func(f *fakeMemory) (*Server, *echo.Echo) {
		s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
		e := echo.New()
		e.GET("/embeddings", s.uiEmbeddings)
		return s, e
	}

	t.Run("renders queue counts and worker state", func(t *testing.T) {
		f := &fakeMemory{
			embeddingProgress: &EmbeddingProgress{Objects: EmbeddingQueueStats{Pending: 3, Processing: 1}},
			embeddingStatus:   &EmbeddingStatus{Objects: EmbeddingWorkerStatus{Running: true}},
		}
		_, e := newServer(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/embeddings", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{"Object embedding queue", "Pending", "Processing"} {
			if !strings.Contains(body, want) {
				t.Errorf("embeddings page missing %q", want)
			}
		}
	})

	t.Run("empty queues render the empty state", func(t *testing.T) {
		f := &fakeMemory{}
		_, e := newServer(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/embeddings", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "No embedding statistics yet") {
			t.Errorf("empty state missing: %s", rec.Body.String())
		}
	})

	t.Run("progress fetch failure renders the error state", func(t *testing.T) {
		f := &fakeMemory{embeddingProgErr: fmt.Errorf("boom")}
		_, e := newServer(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/embeddings", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Failed to load embedding statistics") {
			t.Errorf("error state missing: %s", rec.Body.String())
		}
	})

	t.Run("progress failure preserves worker state section", func(t *testing.T) {
		f := &fakeMemory{
			embeddingProgErr: fmt.Errorf("boom"),
			embeddingStatus:  &EmbeddingStatus{Objects: EmbeddingWorkerStatus{Running: true}},
		}
		_, e := newServer(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/embeddings", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Failed to load embedding statistics") {
			t.Errorf("queue error missing: %s", body)
		}
		if !strings.Contains(body, "Workers") {
			t.Errorf("worker section missing despite successful status fetch: %s", body)
		}
	})

	t.Run("status failure preserves queue counts section", func(t *testing.T) {
		f := &fakeMemory{
			embeddingProgress: &EmbeddingProgress{Objects: EmbeddingQueueStats{Pending: 3}},
			embeddingStatErr:  fmt.Errorf("boom"),
		}
		_, e := newServer(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/embeddings", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Object embedding queue") {
			t.Errorf("queue section missing despite successful progress fetch: %s", body)
		}
		if !strings.Contains(body, "Workers") {
			t.Errorf("worker error section missing: %s", body)
		}
		if !strings.Contains(body, "boom") {
			t.Errorf("worker error message missing: %s", body)
		}
	})

	t.Run("missing embedding model renders the warning", func(t *testing.T) {
		f := &fakeMemory{
			embeddingProgress: &EmbeddingProgress{Objects: EmbeddingQueueStats{Pending: 3}},
			embeddingStatus:   &EmbeddingStatus{Objects: EmbeddingWorkerStatus{Running: true}},
			effectiveModel:    &EffectiveModelConfig{EmbeddingModelSource: "none"},
		}
		_, e := newServer(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/embeddings", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "No embedding model is configured for this project") {
			t.Errorf("warning message missing: %s", body)
		}
		if !strings.Contains(body, `href="/settings/providers"`) {
			t.Errorf("warning CTA link missing: %s", body)
		}
	})

	t.Run("configured embedding model hides the warning", func(t *testing.T) {
		f := &fakeMemory{
			embeddingProgress: &EmbeddingProgress{Objects: EmbeddingQueueStats{Pending: 3}},
			embeddingStatus:   &EmbeddingStatus{Objects: EmbeddingWorkerStatus{Running: true}},
			effectiveModel:    &EffectiveModelConfig{EmbeddingModel: "openai/embed-x", EmbeddingModelSource: "provider"},
		}
		_, e := newServer(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/embeddings", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "No embedding model is configured") {
			t.Errorf("warning shown despite a configured embedding model: %s", rec.Body.String())
		}
	})

	t.Run("model-config fetch failure shows no warning and blanks nothing", func(t *testing.T) {
		f := &fakeMemory{
			embeddingProgress: &EmbeddingProgress{Objects: EmbeddingQueueStats{Pending: 3}},
			embeddingStatus:   &EmbeddingStatus{Objects: EmbeddingWorkerStatus{Running: true}},
			effectiveModelErr: fmt.Errorf("boom"),
		}
		_, e := newServer(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/embeddings", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		if strings.Contains(body, "No embedding model is configured") {
			t.Errorf("warning shown on unknown model state: %s", body)
		}
		if !strings.Contains(body, "Object embedding queue") || !strings.Contains(body, "Workers") {
			t.Errorf("model-config failure blanked the page: %s", body)
		}
	})
}

func TestRenderEmbeddingsPage(t *testing.T) {
	progress := &EmbeddingProgress{
		Objects:       EmbeddingQueueStats{Pending: 1, Processing: 2, Completed: 3, Failed: 4, StaleFailed: 11, DeadLetter: 5},
		Relationships: EmbeddingQueueStats{Pending: 6, Processing: 7, Completed: 8, Failed: 9, StaleFailed: 12, DeadLetter: 10},
	}
	status := &EmbeddingStatus{
		Objects:       EmbeddingWorkerStatus{Running: true},
		Relationships: EmbeddingWorkerStatus{Paused: true},
		Sweep:         EmbeddingWorkerStatus{},
		Config:        EmbeddingConfig{BatchSize: 100, Concurrency: 4, IntervalMs: 500, StaleMinutes: 30, EnableAdaptiveScaling: true},
	}
	html := renderHTML(t, EmbeddingsPage(embeddingPageData{Progress: progress, Status: status}))
	for _, want := range []string{
		"Object embedding queue", "Relationship embedding queue",
		"Pending", "Processing", "Completed", "Failed", "Stale failed", "Dead letter",
		"Objects: running", "Relationships: paused", "Sweep: idle",
		"Batch size", "Concurrency", "Interval", "Stale after",
		"Adaptive scaling", "enabled",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("embeddings page missing %q", want)
		}
	}

	htmlEmpty := renderHTML(t, EmbeddingsPage(embeddingPageData{Progress: &EmbeddingProgress{}, Empty: true}))
	if !strings.Contains(htmlEmpty, "No embedding statistics yet") {
		t.Error("empty state missing")
	}

	htmlErr := renderHTML(t, EmbeddingsPage(embeddingPageData{ProgressErr: errTest}))
	if !strings.Contains(htmlErr, "Failed to load embedding statistics") {
		t.Error("error state missing")
	}

	htmlStatusErr := renderHTML(t, EmbeddingsPage(embeddingPageData{
		Progress:  &EmbeddingProgress{Objects: EmbeddingQueueStats{Pending: 1}},
		StatusErr: errTest,
	}))
	if !strings.Contains(htmlStatusErr, "Object embedding queue") || !strings.Contains(htmlStatusErr, "Workers") {
		t.Errorf("section-scoped status error should keep the queue section: %s", htmlStatusErr)
	}

	htmlMissing := renderHTML(t, EmbeddingsPage(embeddingPageData{
		Progress:              &EmbeddingProgress{Objects: EmbeddingQueueStats{Pending: 1}},
		EmbeddingModelMissing: true,
	}))
	if !strings.Contains(htmlMissing, "No embedding model is configured for this project") {
		t.Errorf("missing-model warning not rendered: %s", htmlMissing)
	}
	if !strings.Contains(htmlMissing, `href="/settings/providers"`) {
		t.Errorf("missing-model warning CTA not rendered: %s", htmlMissing)
	}

	htmlConfigured := renderHTML(t, EmbeddingsPage(embeddingPageData{
		Progress:       &EmbeddingProgress{Objects: EmbeddingQueueStats{Pending: 1}},
		EmbeddingModel: "openai/embed-x",
	}))
	if strings.Contains(htmlConfigured, "No embedding model is configured") {
		t.Errorf("warning rendered with a configured embedding model: %s", htmlConfigured)
	}
}

func TestRenderObjectEmbeddingBadges(t *testing.T) {
	statuses := []struct {
		status string
		label  string
	}{
		{"embedded", "Embedded"},
		{"pending", "Embedding queued"},
		{"processing", "Embedding…"},
		{"failed", "Embedding failed"},
		{"dead_letter", "Embedding failed"},
		{"missing", "Not embedded"},
	}
	for _, c := range statuses {
		objects := []GraphObject{{ID: "o1", Type: "person", Key: "sam", EmbeddingStatus: c.status}}
		html := renderHTML(t, ObjectsPage(objectsPageData{Objects: objects}))
		if !strings.Contains(html, c.label) {
			t.Errorf("list card for %q missing label %q", c.status, c.label)
		}

		obj := &GraphObject{ID: "o1", Type: "person", Key: "sam", EmbeddingStatus: c.status}
		html = renderHTML(t, ObjectDetailPage(obj, nil, nil, nil, nil, nil, "", nil, nil, nil, nil))
		if !strings.Contains(html, c.label) {
			t.Errorf("detail header for %q missing label %q", c.status, c.label)
		}
	}

	// detail header renders the muted "Embedded <time>" text when updated_at is set
	obj := &GraphObject{ID: "o1", Type: "person", Key: "sam", EmbeddingStatus: "embedded", EmbeddingUpdatedAt: "2026-09-20T10:00:00Z"}
	html := renderHTML(t, ObjectDetailPage(obj, nil, nil, nil, nil, nil, "", nil, nil, nil, nil))
	if !strings.Contains(html, `class="text-base-content/50 text-xs">Embedded `) {
		t.Errorf("detail header missing muted embedded-at timestamp: %s", html)
	}

	// no timestamp when updated_at is empty
	objNoTime := &GraphObject{ID: "o1", Type: "person", Key: "sam", EmbeddingStatus: "embedded"}
	htmlNoTime := renderHTML(t, ObjectDetailPage(objNoTime, nil, nil, nil, nil, nil, "", nil, nil, nil, nil))
	if strings.Contains(htmlNoTime, `text-base-content/50 text-xs">Embedded `) {
		t.Errorf("detail header should omit the timestamp when updated_at is empty")
	}
}
