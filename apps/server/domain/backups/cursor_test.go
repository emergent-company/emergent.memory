package backups

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestParseCursorRoundTrip(t *testing.T) {
	// Mirrors the SDK's Cursor.Encode output.
	encoded := base64.URLEncoding.EncodeToString([]byte(`{"createdAt":"2026-01-01T00:00:00Z","id":"backup_1"}`))
	cursor, err := ParseCursor(encoded)
	if err != nil {
		t.Fatalf("ParseCursor() error = %v", err)
	}
	if cursor.ID != "backup_1" {
		t.Errorf("expected id backup_1, got %q", cursor.ID)
	}
	want := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if !cursor.CreatedAt.Equal(want) {
		t.Errorf("expected createdAt %v, got %v", want, cursor.CreatedAt)
	}
}

func TestParseCursorEmpty(t *testing.T) {
	cursor, err := ParseCursor("")
	if err != nil {
		t.Fatalf("ParseCursor(\"\") error = %v", err)
	}
	if cursor != nil {
		t.Errorf("expected nil cursor for empty string, got %v", cursor)
	}
}

func TestParseCursorInvalid(t *testing.T) {
	if _, err := ParseCursor("not-base64!!"); err == nil {
		t.Error("expected error for invalid base64")
	}
}
