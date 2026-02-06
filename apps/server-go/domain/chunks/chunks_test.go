package chunks

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFloatsToVectorLiteral(t *testing.T) {
	tests := []struct {
		name     string
		input    []float32
		expected string
	}{
		{
			name:     "empty slice",
			input:    []float32{},
			expected: "[]",
		},
		{
			name:     "nil slice",
			input:    nil,
			expected: "[]",
		},
		{
			name:     "single element",
			input:    []float32{1.5},
			expected: "[1.5]",
		},
		{
			name:     "multiple elements",
			input:    []float32{1.0, 2.5, 3.75},
			expected: "[1,2.5,3.75]",
		},
		{
			name:     "integer values",
			input:    []float32{1, 2, 3},
			expected: "[1,2,3]",
		},
		{
			name:     "negative values",
			input:    []float32{-1.5, 0, 1.5},
			expected: "[-1.5,0,1.5]",
		},
		{
			name:     "very small values",
			input:    []float32{0.001, 0.0001},
			expected: "[0.001,0.0001]",
		},
		{
			name:     "large values",
			input:    []float32{1000000, 2000000.5},
			expected: "[1e+06,2.0000005e+06]",
		},
		{
			name:     "zero values",
			input:    []float32{0, 0, 0},
			expected: "[0,0,0]",
		},
		{
			name:     "mixed precision",
			input:    []float32{0.123456789, 1.0, -0.5},
			expected: "[0.12345679,1,-0.5]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := floatsToVectorLiteral(tt.input)
			if result != tt.expected {
				t.Errorf("floatsToVectorLiteral(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestChunkMetadataScan(t *testing.T) {
	tests := []struct {
		name        string
		input       interface{}
		wantErr     bool
		checkResult func(*ChunkMetadata) bool
	}{
		{
			name:    "nil input",
			input:   nil,
			wantErr: false,
			checkResult: func(m *ChunkMetadata) bool {
				// Should not modify the metadata
				return true
			},
		},
		{
			name:    "valid JSON bytes",
			input:   []byte(`{"startOffset": 100, "endOffset": 200}`),
			wantErr: false,
			checkResult: func(m *ChunkMetadata) bool {
				return m.StartOffset == 100 && m.EndOffset == 200
			},
		},
		{
			name:    "valid JSON string",
			input:   `{"startOffset": 50, "endOffset": 150}`,
			wantErr: false,
			checkResult: func(m *ChunkMetadata) bool {
				return m.StartOffset == 50 && m.EndOffset == 150
			},
		},
		{
			name:    "empty JSON object bytes",
			input:   []byte(`{}`),
			wantErr: false,
			checkResult: func(m *ChunkMetadata) bool {
				return m.StartOffset == 0 && m.EndOffset == 0
			},
		},
		{
			name:    "empty JSON object string",
			input:   `{}`,
			wantErr: false,
			checkResult: func(m *ChunkMetadata) bool {
				return m.StartOffset == 0 && m.EndOffset == 0
			},
		},
		{
			name:    "invalid JSON bytes",
			input:   []byte(`{invalid json`),
			wantErr: true,
		},
		{
			name:    "invalid JSON string",
			input:   `{invalid json`,
			wantErr: true,
		},
		{
			name:    "unsupported type int",
			input:   123,
			wantErr: false, // Returns nil for unsupported types
		},
		{
			name:    "unsupported type bool",
			input:   true,
			wantErr: false, // Returns nil for unsupported types
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &ChunkMetadata{}
			err := m.Scan(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Error("Scan() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Scan() unexpected error: %v", err)
				return
			}

			if tt.checkResult != nil && !tt.checkResult(m) {
				t.Errorf("Scan() result check failed for input %v", tt.input)
			}
		})
	}
}

func TestChunkMetadataScanWithAllFields(t *testing.T) {
	jsonData := []byte(`{
		"strategy": "sentence",
		"startOffset": 0,
		"endOffset": 500,
		"boundaryType": "paragraph"
	}`)

	m := &ChunkMetadata{}
	err := m.Scan(jsonData)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if m.Strategy != "sentence" {
		t.Errorf("Strategy = %v, want sentence", m.Strategy)
	}
	if m.StartOffset != 0 {
		t.Errorf("StartOffset = %v, want 0", m.StartOffset)
	}
	if m.EndOffset != 500 {
		t.Errorf("EndOffset = %v, want 500", m.EndOffset)
	}
	if m.BoundaryType != "paragraph" {
		t.Errorf("BoundaryType = %v, want paragraph", m.BoundaryType)
	}
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

func TestChunkWithDocInfoToDTO(t *testing.T) {
	fixedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	chunkID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	docID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	tests := []struct {
		name     string
		chunk    ChunkWithDocInfo
		expected ChunkDTO
	}{
		{
			name: "with filename as title",
			chunk: ChunkWithDocInfo{
				Chunk: Chunk{
					ID:         chunkID,
					DocumentID: docID,
					ChunkIndex: 0,
					Text:       "Hello world",
					Embedding:  []byte{1, 2, 3, 4},
					Metadata:   &ChunkMetadata{Strategy: "sentence"},
					CreatedAt:  fixedTime,
				},
				DocumentFilename:  strPtr("document.pdf"),
				DocumentSourceURL: strPtr("https://example.com/doc"),
				TotalChars:        intPtr(1000),
				ChunkCount:        intPtr(5),
				EmbeddedChunks:    intPtr(3),
			},
			expected: ChunkDTO{
				ID:             chunkID.String(),
				DocumentID:     docID.String(),
				DocumentTitle:  "document.pdf", // Filename takes priority
				Index:          0,
				Size:           11, // len("Hello world")
				HasEmbedding:   true,
				Text:           "Hello world",
				CreatedAt:      "2024-01-15T10:30:00Z",
				Metadata:       &ChunkMetadata{Strategy: "sentence"},
				TotalChars:     intPtr(1000),
				ChunkCount:     intPtr(5),
				EmbeddedChunks: intPtr(3),
			},
		},
		{
			name: "with source_url as title when no filename",
			chunk: ChunkWithDocInfo{
				Chunk: Chunk{
					ID:         chunkID,
					DocumentID: docID,
					ChunkIndex: 2,
					Text:       "Test content",
					CreatedAt:  fixedTime,
				},
				DocumentFilename:  nil,
				DocumentSourceURL: strPtr("https://example.com/page"),
			},
			expected: ChunkDTO{
				ID:            chunkID.String(),
				DocumentID:    docID.String(),
				DocumentTitle: "https://example.com/page",
				Index:         2,
				Size:          12, // len("Test content")
				HasEmbedding:  false,
				Text:          "Test content",
				CreatedAt:     "2024-01-15T10:30:00Z",
			},
		},
		{
			name: "with empty filename falls back to source_url",
			chunk: ChunkWithDocInfo{
				Chunk: Chunk{
					ID:         chunkID,
					DocumentID: docID,
					ChunkIndex: 1,
					Text:       "Content",
					CreatedAt:  fixedTime,
				},
				DocumentFilename:  strPtr(""),
				DocumentSourceURL: strPtr("https://fallback.com"),
			},
			expected: ChunkDTO{
				ID:            chunkID.String(),
				DocumentID:    docID.String(),
				DocumentTitle: "https://fallback.com",
				Index:         1,
				Size:          7,
				HasEmbedding:  false,
				Text:          "Content",
				CreatedAt:     "2024-01-15T10:30:00Z",
			},
		},
		{
			name: "with neither filename nor source_url",
			chunk: ChunkWithDocInfo{
				Chunk: Chunk{
					ID:         chunkID,
					DocumentID: docID,
					ChunkIndex: 0,
					Text:       "No title",
					CreatedAt:  fixedTime,
				},
				DocumentFilename:  nil,
				DocumentSourceURL: nil,
			},
			expected: ChunkDTO{
				ID:            chunkID.String(),
				DocumentID:    docID.String(),
				DocumentTitle: "",
				Index:         0,
				Size:          8,
				HasEmbedding:  false,
				Text:          "No title",
				CreatedAt:     "2024-01-15T10:30:00Z",
			},
		},
		{
			name: "with embedding bytes",
			chunk: ChunkWithDocInfo{
				Chunk: Chunk{
					ID:         chunkID,
					DocumentID: docID,
					ChunkIndex: 0,
					Text:       "Has embedding",
					Embedding:  []byte{0x01, 0x02, 0x03},
					CreatedAt:  fixedTime,
				},
			},
			expected: ChunkDTO{
				ID:            chunkID.String(),
				DocumentID:    docID.String(),
				DocumentTitle: "",
				Index:         0,
				Size:          13,
				HasEmbedding:  true,
				Text:          "Has embedding",
				CreatedAt:     "2024-01-15T10:30:00Z",
			},
		},
		{
			name: "without embedding",
			chunk: ChunkWithDocInfo{
				Chunk: Chunk{
					ID:         chunkID,
					DocumentID: docID,
					ChunkIndex: 0,
					Text:       "No embedding",
					Embedding:  nil,
					CreatedAt:  fixedTime,
				},
			},
			expected: ChunkDTO{
				ID:            chunkID.String(),
				DocumentID:    docID.String(),
				DocumentTitle: "",
				Index:         0,
				Size:          12,
				HasEmbedding:  false,
				Text:          "No embedding",
				CreatedAt:     "2024-01-15T10:30:00Z",
			},
		},
		{
			name: "with empty embedding slice",
			chunk: ChunkWithDocInfo{
				Chunk: Chunk{
					ID:         chunkID,
					DocumentID: docID,
					ChunkIndex: 0,
					Text:       "Empty embedding",
					Embedding:  []byte{},
					CreatedAt:  fixedTime,
				},
			},
			expected: ChunkDTO{
				ID:            chunkID.String(),
				DocumentID:    docID.String(),
				DocumentTitle: "",
				Index:         0,
				Size:          15,
				HasEmbedding:  false,
				Text:          "Empty embedding",
				CreatedAt:     "2024-01-15T10:30:00Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.chunk.ToDTO()

			if result.ID != tt.expected.ID {
				t.Errorf("ID = %v, want %v", result.ID, tt.expected.ID)
			}
			if result.DocumentID != tt.expected.DocumentID {
				t.Errorf("DocumentID = %v, want %v", result.DocumentID, tt.expected.DocumentID)
			}
			if result.DocumentTitle != tt.expected.DocumentTitle {
				t.Errorf("DocumentTitle = %v, want %v", result.DocumentTitle, tt.expected.DocumentTitle)
			}
			if result.Index != tt.expected.Index {
				t.Errorf("Index = %v, want %v", result.Index, tt.expected.Index)
			}
			if result.Size != tt.expected.Size {
				t.Errorf("Size = %v, want %v", result.Size, tt.expected.Size)
			}
			if result.HasEmbedding != tt.expected.HasEmbedding {
				t.Errorf("HasEmbedding = %v, want %v", result.HasEmbedding, tt.expected.HasEmbedding)
			}
			if result.Text != tt.expected.Text {
				t.Errorf("Text = %v, want %v", result.Text, tt.expected.Text)
			}
			if result.CreatedAt != tt.expected.CreatedAt {
				t.Errorf("CreatedAt = %v, want %v", result.CreatedAt, tt.expected.CreatedAt)
			}
		})
	}
}
