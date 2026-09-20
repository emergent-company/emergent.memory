package main

import (
	"strconv"
	"testing"
)

// TestKeyedRateLimiterDeniesAtCapacity fills the per-key map past its cap and
// asserts a NEW key is denied rather than silently allowed. Regression: the
// limiter used to return allow for every new key once at capacity, letting an
// attacker with enough distinct valid tokens disable the per-link limit.
func TestKeyedRateLimiterDeniesAtCapacity(t *testing.T) {
	l := newKeyedRateLimiter(60, 1)
	for i := range maxShareRateKeys {
		if !l.allow(strconv.Itoa(i)) {
			t.Fatalf("key %d unexpectedly denied while filling to capacity", i)
		}
	}
	if l.allow("overflow-key") {
		t.Fatal("limiter allowed a new key at capacity; expected it to deny")
	}
}
