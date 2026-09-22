// Package idgen provides small helpers for generating unique identifiers.
package idgen

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"
)

// RandRead is the entropy source used by Hex. It is a variable so tests can
// force the fallback path.
var RandRead = rand.Read

// Hex returns an n-byte random value hex-encoded. ok is false when the entropy
// source fails, letting callers apply their own fallback.
func Hex(n int) (s string, ok bool) {
	b := make([]byte, n)
	if _, err := RandRead(b); err != nil {
		return "", false
	}
	return hex.EncodeToString(b), true
}

// FallbackID returns a timestamp-based identifier with the given prefix, used
// when entropy is unavailable so ids stay unique rather than collapsing to a
// constant.
func FallbackID(prefix string) string {
	return prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
}
