// Package idgen provides small helpers for generating unique identifiers.
package idgen

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"sync/atomic"
	"time"
)

// RandRead is the entropy source used by Hex. It is a variable so tests can
// force the fallback path.
var RandRead = rand.Read

// fallbackSeq is a process-local monotonic counter appended to timestamp-based
// fallback ids so concurrent fallback calls still produce distinct values.
var fallbackSeq atomic.Uint64

// Hex returns an n-byte random value hex-encoded. ok is false when the entropy
// source fails, letting callers apply their own fallback.
func Hex(n int) (s string, ok bool) {
	b := make([]byte, n)
	if _, err := RandRead(b); err != nil {
		return "", false
	}
	return hex.EncodeToString(b), true
}

// FallbackID returns a unique identifier with the given prefix, used when
// entropy is unavailable. It combines a nanosecond timestamp with a
// process-local atomic sequence so concurrent calls cannot collide.
func FallbackID(prefix string) string {
	return prefix + "-" +
		strconv.FormatInt(time.Now().UnixNano(), 10) + "-" +
		strconv.FormatUint(fallbackSeq.Add(1), 10)
}
