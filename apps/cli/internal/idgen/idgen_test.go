package idgen

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHex(t *testing.T) {
	s, ok := Hex(16)
	if !ok {
		t.Fatal("Hex(16) failed on the normal path")
	}
	if len(s) != 32 {
		t.Fatalf("Hex(16) = %q, want 32 hex chars", s)
	}

	orig := RandRead
	RandRead = func([]byte) (int, error) { return 0, errors.New("entropy unavailable") }
	t.Cleanup(func() { RandRead = orig })

	if _, ok := Hex(16); ok {
		t.Fatal("Hex reported ok despite entropy failure")
	}
}

func TestFallbackID(t *testing.T) {
	orig := RandRead
	RandRead = func([]byte) (int, error) { return 0, errors.New("entropy unavailable") }
	t.Cleanup(func() { RandRead = orig })

	first := FallbackID("sess")
	if first == "sess-16" {
		t.Fatalf("fallback collapsed to the constant %q", first)
	}
	if !strings.HasPrefix(first, "sess-") {
		t.Fatalf("FallbackID = %q, want sess- prefix", first)
	}
	time.Sleep(time.Millisecond)
	second := FallbackID("sess")
	if first == second {
		t.Fatalf("fallback ids are not unique: both %q", first)
	}
}
