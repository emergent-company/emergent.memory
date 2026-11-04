package idgen

import (
	"errors"
	"strings"
	"sync"
	"testing"
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
	id := FallbackID("sess")
	if !strings.HasPrefix(id, "sess-") {
		t.Fatalf("FallbackID = %q, want sess- prefix", id)
	}

	// Concurrent fallback calls must all produce distinct ids without relying
	// on the wall clock advancing between calls.
	const n = 1000
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i] = FallbackID("sess")
		}(i)
	}
	wg.Wait()

	seen := make(map[string]struct{}, n)
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate fallback id %q", id)
		}
		seen[id] = struct{}{}
	}
}
