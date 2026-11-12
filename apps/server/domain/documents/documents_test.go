package documents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

func TestToDocumentSummary(t *testing.T) {
	// Helper to create string pointer
	strPtr := func(s string) *string { return &s }
	int64Ptr := func(i int64) *int64 { return &i }

	testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		doc      *Document
		expected *DocumentSummary
	}{
		{
			name: "full document",
			doc: &Document{
				ID:               "doc-123",
				Filename:         strPtr("test.pdf"),
				MimeType:         strPtr("application/pdf"),
				FileSizeBytes:    int64Ptr(12345),
				ConversionStatus: strPtr("completed"),
				ConversionError:  nil,
				StorageKey:       strPtr("storage/key/123"),
				CreatedAt:        testTime,
			},
			expected: &DocumentSummary{
				ID:               "doc-123",
				Name:             "test.pdf",
				MimeType:         strPtr("application/pdf"),
				FileSizeBytes:    int64Ptr(12345),
				ConversionStatus: "completed",
				ConversionError:  nil,
				StorageKey:       strPtr("storage/key/123"),
				CreatedAt:        "2024-01-15T10:30:00Z",
			},
		},
		{
			name: "document with nil filename",
			doc: &Document{
				ID:               "doc-456",
				Filename:         nil,
				MimeType:         strPtr("text/plain"),
				FileSizeBytes:    int64Ptr(100),
				ConversionStatus: strPtr("not_required"),
				CreatedAt:        testTime,
			},
			expected: &DocumentSummary{
				ID:               "doc-456",
				Name:             "",
				MimeType:         strPtr("text/plain"),
				FileSizeBytes:    int64Ptr(100),
				ConversionStatus: "not_required",
				CreatedAt:        "2024-01-15T10:30:00Z",
			},
		},
		{
			name: "document with nil conversion status",
			doc: &Document{
				ID:               "doc-789",
				Filename:         strPtr("report.docx"),
				MimeType:         strPtr("application/vnd.openxmlformats-officedocument.wordprocessingml.document"),
				ConversionStatus: nil,
				CreatedAt:        testTime,
			},
			expected: &DocumentSummary{
				ID:               "doc-789",
				Name:             "report.docx",
				MimeType:         strPtr("application/vnd.openxmlformats-officedocument.wordprocessingml.document"),
				ConversionStatus: "not_required",
				CreatedAt:        "2024-01-15T10:30:00Z",
			},
		},
		{
			name: "document with conversion error",
			doc: &Document{
				ID:               "doc-err",
				Filename:         strPtr("broken.pdf"),
				MimeType:         strPtr("application/pdf"),
				ConversionStatus: strPtr("failed"),
				ConversionError:  strPtr("Failed to parse PDF"),
				CreatedAt:        testTime,
			},
			expected: &DocumentSummary{
				ID:               "doc-err",
				Name:             "broken.pdf",
				MimeType:         strPtr("application/pdf"),
				ConversionStatus: "failed",
				ConversionError:  strPtr("Failed to parse PDF"),
				CreatedAt:        "2024-01-15T10:30:00Z",
			},
		},
		{
			name: "minimal document",
			doc: &Document{
				ID:        "doc-min",
				CreatedAt: testTime,
			},
			expected: &DocumentSummary{
				ID:               "doc-min",
				Name:             "",
				ConversionStatus: "not_required",
				CreatedAt:        "2024-01-15T10:30:00Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toDocumentSummary(tt.doc)
			assert.Equal(t, tt.expected.ID, result.ID)
			assert.Equal(t, tt.expected.Name, result.Name)
			assert.Equal(t, tt.expected.ConversionStatus, result.ConversionStatus)
			assert.Equal(t, tt.expected.CreatedAt, result.CreatedAt)

			// Compare pointers
			if tt.expected.MimeType == nil {
				assert.Nil(t, result.MimeType)
			} else {
				assert.Equal(t, *tt.expected.MimeType, *result.MimeType)
			}
			if tt.expected.FileSizeBytes == nil {
				assert.Nil(t, result.FileSizeBytes)
			} else {
				assert.Equal(t, *tt.expected.FileSizeBytes, *result.FileSizeBytes)
			}
			if tt.expected.ConversionError == nil {
				assert.Nil(t, result.ConversionError)
			} else {
				assert.Equal(t, *tt.expected.ConversionError, *result.ConversionError)
			}
			if tt.expected.StorageKey == nil {
				assert.Nil(t, result.StorageKey)
			} else {
				assert.Equal(t, *tt.expected.StorageKey, *result.StorageKey)
			}
		})
	}
}

func TestDetermineConversionStatus(t *testing.T) {
	tests := []struct {
		name     string
		mimeType string
		want     string
	}{
		// Text types - no conversion needed
		{
			name:     "plain text",
			mimeType: "text/plain",
			want:     "not_required",
		},
		{
			name:     "html",
			mimeType: "text/html",
			want:     "not_required",
		},
		{
			name:     "markdown",
			mimeType: "text/markdown",
			want:     "not_required",
		},
		{
			name:     "csv",
			mimeType: "text/csv",
			want:     "not_required",
		},
		// Document types - need conversion
		{
			name:     "PDF",
			mimeType: "application/pdf",
			want:     "pending",
		},
		{
			name:     "Word doc",
			mimeType: "application/msword",
			want:     "pending",
		},
		{
			name:     "Word docx",
			mimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			want:     "pending",
		},
		{
			name:     "Excel xls",
			mimeType: "application/vnd.ms-excel",
			want:     "pending",
		},
		{
			name:     "Excel xlsx",
			mimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			want:     "pending",
		},
		{
			name:     "PowerPoint ppt",
			mimeType: "application/vnd.ms-powerpoint",
			want:     "pending",
		},
		{
			name:     "PowerPoint pptx",
			mimeType: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
			want:     "pending",
		},
		// Unknown types - default to not required
		{
			name:     "application/json",
			mimeType: "application/json",
			want:     "not_required",
		},
		{
			name:     "image/png",
			mimeType: "image/png",
			want:     "not_required",
		},
		{
			name:     "application/octet-stream",
			mimeType: "application/octet-stream",
			want:     "not_required",
		},
		{
			name:     "empty string",
			mimeType: "",
			want:     "not_required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineConversionStatus(tt.mimeType, "")
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestComputeContentHash(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "empty string",
			content: "",
		},
		{
			name:    "simple content",
			content: "hello world",
		},
		{
			name:    "multiline content",
			content: "line 1\nline 2\nline 3",
		},
		{
			name:    "unicode content",
			content: "Hello 世界 🌍",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := computeContentHash(tt.content)
			// SHA-256 produces 64 hex characters
			assert.Len(t, hash, 64)
			// Should only contain hex characters
			assert.Regexp(t, "^[0-9a-f]+$", hash)

			// Same content should produce same hash
			hash2 := computeContentHash(tt.content)
			assert.Equal(t, hash, hash2)
		})
	}
}

func TestComputeContentHashDifferent(t *testing.T) {
	hash1 := computeContentHash("hello")
	hash2 := computeContentHash("world")
	assert.NotEqual(t, hash1, hash2, "different content should produce different hashes")
}

func TestPopulateContentHash(t *testing.T) {
	t.Run("nil content leaves hash nil", func(t *testing.T) {
		doc := &Document{}
		populateContentHash(doc)
		assert.Nil(t, doc.ContentHash, "a document without content has no content identity yet")
	})

	t.Run("non-nil content sets sha256 hex", func(t *testing.T) {
		content := "hello world"
		doc := &Document{Content: &content}
		populateContentHash(doc)

		sum := sha256.Sum256([]byte(content))
		want := hex.EncodeToString(sum[:])
		if assert.NotNil(t, doc.ContentHash) {
			assert.Equal(t, want, *doc.ContentHash)
			assert.Len(t, *doc.ContentHash, 64)
		}
	})

	t.Run("overwrites a wrong caller-supplied hash", func(t *testing.T) {
		content := "repository owns the content identity invariant"
		wrong := "caller-supplied-nonsense"
		doc := &Document{Content: &content, ContentHash: &wrong}
		populateContentHash(doc)

		sum := sha256.Sum256([]byte(content))
		want := hex.EncodeToString(sum[:])
		if assert.NotNil(t, doc.ContentHash) {
			assert.Equal(t, want, *doc.ContentHash)
			assert.NotEqual(t, wrong, *doc.ContentHash)
		}
	})
}

func TestComputeFileHash(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "empty bytes",
			data: []byte{},
		},
		{
			name: "nil bytes",
			data: nil,
		},
		{
			name: "simple content",
			data: []byte("hello world"),
		},
		{
			name: "binary content",
			data: []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD},
		},
		{
			name: "unicode content",
			data: []byte("Hello 世界 🌍"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := computeFileHash(tt.data)
			// SHA-256 produces 64 hex characters
			if len(hash) != 64 {
				t.Errorf("computeFileHash() = %q, want 64 characters, got %d", hash, len(hash))
			}
			// Should only contain hex characters
			for _, c := range hash {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
					t.Errorf("computeFileHash() = %q, contains non-hex character %c", hash, c)
				}
			}
			// Same content should produce same hash (deterministic)
			hash2 := computeFileHash(tt.data)
			if hash != hash2 {
				t.Errorf("computeFileHash() not deterministic: %q != %q", hash, hash2)
			}
		})
	}
}

func TestComputeFileHashDifferent(t *testing.T) {
	hash1 := computeFileHash([]byte("hello"))
	hash2 := computeFileHash([]byte("world"))
	if hash1 == hash2 {
		t.Error("different content should produce different hashes")
	}
}

func TestComputeFileHashMatchesKnown(t *testing.T) {
	// SHA-256 of "hello world" is well-known
	data := []byte("hello world")
	hash := computeFileHash(data)
	expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if hash != expected {
		t.Errorf("computeFileHash(%q) = %q, want %q", string(data), hash, expected)
	}
}

func TestParsePositiveInt(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		min     int
		max     int
		want    int
		wantErr bool
	}{
		{
			name:    "valid number in range",
			s:       "50",
			min:     1,
			max:     100,
			want:    50,
			wantErr: false,
		},
		{
			name:    "minimum value",
			s:       "1",
			min:     1,
			max:     100,
			want:    1,
			wantErr: false,
		},
		{
			name:    "maximum value",
			s:       "100",
			min:     1,
			max:     100,
			want:    100,
			wantErr: false,
		},
		{
			name:    "zero when allowed",
			s:       "0",
			min:     0,
			max:     100,
			want:    0,
			wantErr: false,
		},
		{
			name:    "below minimum",
			s:       "0",
			min:     1,
			max:     100,
			want:    0,
			wantErr: true,
		},
		{
			name:    "above maximum",
			s:       "101",
			min:     1,
			max:     100,
			want:    0,
			wantErr: true,
		},
		{
			name:    "non-numeric",
			s:       "abc",
			min:     1,
			max:     100,
			want:    0,
			wantErr: true,
		},
		{
			name:    "negative number",
			s:       "-5",
			min:     1,
			max:     100,
			want:    0,
			wantErr: true,
		},
		{
			name:    "decimal",
			s:       "5.5",
			min:     1,
			max:     100,
			want:    0,
			wantErr: true,
		},
		{
			name:    "empty string with min 0",
			s:       "",
			min:     0,
			max:     100,
			want:    0,
			wantErr: false, // empty string parses to 0, which is >= min(0)
		},
		{
			name:    "empty string with min 1",
			s:       "",
			min:     1,
			max:     100,
			want:    0,
			wantErr: true, // empty string parses to 0, which is < min(1)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePositiveInt(tt.s, tt.min, tt.max)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestValidateCreateRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *CreateDocumentRequest
		wantErr bool
	}{
		{
			name:    "valid empty request",
			req:     &CreateDocumentRequest{},
			wantErr: false,
		},
		{
			name: "valid filename",
			req: &CreateDocumentRequest{
				Filename: "test-document.pdf",
			},
			wantErr: false,
		},
		{
			name: "valid filename at max length (512)",
			req: &CreateDocumentRequest{
				Filename: string(make([]byte, 512)),
			},
			wantErr: false,
		},
		{
			name: "filename too long (513)",
			req: &CreateDocumentRequest{
				Filename: string(make([]byte, 513)),
			},
			wantErr: true,
		},
		{
			name: "valid content",
			req: &CreateDocumentRequest{
				Filename: "doc.txt",
				Content:  "Hello, world!",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCreateRequest(tt.req)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseCursor(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name    string
		encoded string
		want    *Cursor
		wantErr bool
	}{
		{
			name:    "empty string returns nil",
			encoded: "",
			want:    nil,
			wantErr: false,
		},
		{
			name:    "legacy cursor normalizes to the created keyset",
			encoded: "eyJjcmVhdGVkQXQiOiIyMDI0LTAxLTE1VDEwOjMwOjAwWiIsImlkIjoiZG9jLTEyMyJ9",
			want: &Cursor{
				Sort:  SortCreated,
				Value: testTime,
				ID:    "doc-123",
			},
			wantErr: false,
		},
		{
			name:    "keyset cursor keeps its sort",
			encoded: "eyJzb3J0IjoidXBkYXRlZCIsInZhbHVlIjoiMjAyNC0wMS0xNVQxMDozMDowMFoiLCJpZCI6ImRvYy0xMjMifQ==",
			want: &Cursor{
				Sort:  SortUpdated,
				Value: testTime,
				ID:    "doc-123",
			},
			wantErr: false,
		},
		{
			name:    "unknown sort is rejected",
			encoded: "eyJzb3J0IjoiZmlsZW5hbWUiLCJ2YWx1ZSI6IjIwMjQtMDEtMTVUMTA6MzA6MDBaIiwiaWQiOiJkb2MtMTIzIn0=",
			wantErr: true,
		},
		{
			name:    "missing keyset is rejected",
			encoded: "e30=", // "{}" in base64
			wantErr: true,
		},
		{
			name:    "sort without a value is rejected",
			encoded: "eyJzb3J0IjoiY3JlYXRlZCIsImlkIjoiZG9jLTEyMyJ9",
			wantErr: true,
		},
		{
			name:    "invalid base64 encoding",
			encoded: "not-valid-base64!!!",
			wantErr: true,
		},
		{
			name:    "valid base64 but invalid JSON",
			encoded: "bm90LWpzb24=", // "not-json" in base64
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCursor(tt.encoded)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.NotNil(t, got)
			assert.Equal(t, tt.want.Sort, got.Sort)
			assert.Equal(t, tt.want.ID, got.ID)
			assert.True(t, tt.want.Value.Equal(got.Value), "value: want %s, got %s", tt.want.Value, got.Value)
			assert.Nil(t, got.CreatedAt, "legacy field should be normalized away")
		})
	}
}

func TestCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 28, 9, 12, 0, 0, time.UTC)
	for _, sortKey := range []string{SortCreated, SortUpdated} {
		encoded, err := encodeCursor(Cursor{Sort: sortKey, Value: at, ID: "doc-1"})
		if err != nil {
			t.Fatalf("encodeCursor: %v", err)
		}
		got, err := ParseCursor(encoded)
		if err != nil {
			t.Fatalf("ParseCursor: %v", err)
		}
		assert.Equal(t, sortKey, got.Sort)
		assert.Equal(t, "doc-1", got.ID)
		assert.True(t, at.Equal(got.Value))
	}
}

func TestNormalizeStatusBucket(t *testing.T) {
	for _, bucket := range []string{StatusBucketProcessing, StatusBucketReady, StatusBucketExtracted, StatusBucketFailed} {
		got, err := NormalizeStatusBucket(" " + strings.ToUpper(bucket) + " ")
		assert.NoError(t, err)
		assert.Equal(t, bucket, got)
	}
	got, err := NormalizeStatusBucket("")
	assert.NoError(t, err)
	assert.Empty(t, got)

	_, err = NormalizeStatusBucket("pendings")
	requireAppError(t, err, http.StatusBadRequest, "status must be one of")
}

func TestNormalizeSortAndOrder(t *testing.T) {
	sortKey, err := NormalizeSort("")
	assert.NoError(t, err)
	assert.Equal(t, SortCreated, sortKey)

	sortKey, err = NormalizeSort("UPDATED")
	assert.NoError(t, err)
	assert.Equal(t, SortUpdated, sortKey)

	_, err = NormalizeSort("filename")
	requireAppError(t, err, http.StatusBadRequest, "sort must be one of")

	order, err := NormalizeOrder("")
	assert.NoError(t, err)
	assert.Equal(t, OrderDesc, order)

	order, err = NormalizeOrder("ASC")
	assert.NoError(t, err)
	assert.Equal(t, OrderAsc, order)

	_, err = NormalizeOrder("sideways")
	requireAppError(t, err, http.StatusBadRequest, "order must be one of")
}

// requireAppError asserts err is a client-facing app error: the vocabulary
// rejection is the response's message, so it survives to the client instead of
// being replaced by a generic 500 (and it carries nothing but the vocabulary).
func requireAppError(t *testing.T, err error, wantStatus int, wantMessagePart string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error for an unknown value")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error is not a client-facing app error: %T %v", err, err)
	}
	assert.Equal(t, wantStatus, appErr.HTTPStatus)
	assert.Contains(t, appErr.Message, wantMessagePart)
}

// TestStatusBucketsPartitionEveryState pins the bucket mapping to the six states
// the derived-status CASE can produce: every state belongs to exactly one bucket.
// A state added to the CASE without a bucket would silently vanish from the
// filter and the counts, so it is spelled out here rather than inferred.
func TestStatusBucketsPartitionEveryState(t *testing.T) {
	states := []string{"converting", "conversion_failed", "extracting", "extraction_failed", "completed", "ready_for_extraction"}
	seen := map[string]string{}
	for bucket, bucketStates := range statusBucketStates {
		for _, state := range bucketStates {
			if prev, dup := seen[state]; dup {
				t.Fatalf("state %q is in both %q and %q", state, prev, bucket)
			}
			seen[state] = bucket
		}
	}
	for _, state := range states {
		if _, ok := seen[state]; !ok {
			t.Fatalf("state %q is in no bucket", state)
		}
	}
	if len(seen) != len(states) {
		t.Fatalf("buckets cover %d states, CASE produces %d", len(seen), len(states))
	}
}

func TestNormalizeDocumentSearch(t *testing.T) {
	assert.Equal(t, "spec", NormalizeDocumentSearch("  spec  "))
	assert.Empty(t, NormalizeDocumentSearch("   "))

	long := strings.Repeat("a", MaxDocumentSearchTerm+50)
	assert.Len(t, []rune(NormalizeDocumentSearch(long)), MaxDocumentSearchTerm)
}

func TestEscapeLikePattern(t *testing.T) {
	assert.Equal(t, `100\%\_done\\x`, escapeLikePattern(`100%_done\x`))
	assert.Equal(t, "plain", escapeLikePattern("plain"))
}

func TestStatusCountsAdd(t *testing.T) {
	var c StatusCounts
	c.add("converting", 2)
	c.add("extracting", 1)
	c.add("ready_for_extraction", 5)
	c.add("completed", 7)
	c.add("conversion_failed", 1)
	c.add("extraction_failed", 2)
	c.add("some_future_state", 3) // counted in All, in no bucket

	assert.Equal(t, 21, c.All)
	assert.Equal(t, 3, c.Processing)
	assert.Equal(t, 5, c.Ready)
	assert.Equal(t, 7, c.Extracted)
	assert.Equal(t, 3, c.Failed)
}

// TestListRejectsUnknownStatusAndMismatchedCursor pins the repository's own
// guards. The HTTP handler already answers 400 for both, but a direct caller
// (a job, a future exporter) must not get a silently unfiltered page from an
// unknown bucket, nor a page walked with the wrong keyset comparison.
func TestListRejectsUnknownStatusAndMismatchedCursor(t *testing.T) {
	repo := NewRepository(nil, slog.Default())

	_, err := repo.List(context.Background(), ListParams{ProjectID: "p1", Status: "done"})
	if err == nil || !strings.Contains(err.Error(), "unknown status bucket") {
		t.Fatalf("unknown status: err = %v, want unknown status bucket", err)
	}

	cursor := &Cursor{Sort: SortUpdated, Value: time.Now(), ID: "d1"}
	_, err = repo.List(context.Background(), ListParams{ProjectID: "p1", Sort: SortCreated, Cursor: cursor})
	if err == nil || !strings.Contains(err.Error(), "cursor was minted for sort") {
		t.Fatalf("mismatched cursor: err = %v, want a sort-mismatch error", err)
	}
}
