package journal

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidCursor is returned when a pagination cursor cannot be decoded.
// Handlers map it to a 400 Bad Request.
var ErrInvalidCursor = errors.New("invalid cursor")

// encodeCursor encodes a keyset cursor as "<RFC3339Nano created_at>|<uuid id>".
// Neither component may contain '|' (RFC3339Nano timestamps and UUIDs never do),
// so the split is unambiguous.
func encodeCursor(createdAt time.Time, id uuid.UUID) string {
	return createdAt.UTC().Format(time.RFC3339Nano) + "|" + id.String()
}

// decodeCursor decodes a cursor produced by encodeCursor. It validates both
// components and returns ErrInvalidCursor on any malformed input.
func decodeCursor(s string) (time.Time, uuid.UUID, error) {
	tsStr, idStr, ok := strings.Cut(s, "|")
	if !ok || tsStr == "" || idStr == "" {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, tsStr)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	return createdAt, id, nil
}
