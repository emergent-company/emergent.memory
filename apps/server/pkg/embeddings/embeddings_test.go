package embeddings

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

func TestNoopClient_EmbedQuery(t *testing.T) {
	client := NewNoopClient()
	result, err := client.EmbedQuery(context.Background(), "test query")

	if err != nil {
		t.Errorf("EmbedQuery() error = %v, want nil", err)
	}
	if result != nil {
		t.Errorf("EmbedQuery() = %v, want nil", result)
	}
}

func TestNoopClient_EmbedDocuments(t *testing.T) {
	client := NewNoopClient()
	result, err := client.EmbedDocuments(context.Background(), []string{"doc1", "doc2"})

	if err != nil {
		t.Errorf("EmbedDocuments() error = %v, want nil", err)
	}
	if result != nil {
		t.Errorf("EmbedDocuments() = %v, want nil", result)
	}
}

func TestNewNoopClient(t *testing.T) {
	client := NewNoopClient()
	if client == nil {
		t.Error("NewNoopClient() returned nil")
	}
}

func TestNewNoopService(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := NewNoopService(logger)

	if svc == nil {
		t.Fatal("NewNoopService() returned nil")
	}

	// Should not be enabled
	if svc.IsEnabled() {
		t.Error("NewNoopService().IsEnabled() = true, want false")
	}
}

func TestService_IsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{
			name:     "enabled service",
			enabled:  true,
			expected: true,
		},
		{
			name:     "disabled service",
			enabled:  false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{
				enabled: tt.enabled,
			}
			if svc.IsEnabled() != tt.expected {
				t.Errorf("IsEnabled() = %v, want %v", svc.IsEnabled(), tt.expected)
			}
		})
	}
}

func TestService_EmbedQuery_WithNoopClient(t *testing.T) {
	svc := &Service{
		client: NewNoopClient(),
	}

	result, err := svc.EmbedQuery(context.Background(), "test query")
	if err != nil {
		t.Errorf("EmbedQuery() error = %v, want nil", err)
	}
	if result != nil {
		t.Errorf("EmbedQuery() = %v, want nil", result)
	}
}

func TestService_EmbedDocuments_WithNoopClient(t *testing.T) {
	svc := &Service{
		client: NewNoopClient(),
	}

	result, err := svc.EmbedDocuments(context.Background(), []string{"doc1", "doc2"})
	if err != nil {
		t.Errorf("EmbedDocuments() error = %v, want nil", err)
	}
	if result != nil {
		t.Errorf("EmbedDocuments() = %v, want nil", result)
	}
}

func TestEmbeddingDimensionConstant(t *testing.T) {
	if EmbeddingDimension != 768 {
		t.Errorf("EmbeddingDimension = %d, want 768", EmbeddingDimension)
	}
}
