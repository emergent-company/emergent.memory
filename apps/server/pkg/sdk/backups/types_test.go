package backups

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestCursorEncodeRoundTrip(t *testing.T) {
	c := &Cursor{CreatedAt: "2026-01-01T00:00:00Z", ID: "backup_1"}
	encoded := c.Encode()

	data, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("Encode() produced invalid base64: %v", err)
	}
	var decoded Cursor
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Encode() produced invalid JSON: %v", err)
	}
	if decoded.CreatedAt != c.CreatedAt || decoded.ID != c.ID {
		t.Errorf("round-trip mismatch: got %+v, want %+v", decoded, *c)
	}
}

func TestCursorEncodeNil(t *testing.T) {
	var c *Cursor
	if got := c.Encode(); got != "" {
		t.Errorf("nil Cursor Encode() = %q, want empty string", got)
	}
}
