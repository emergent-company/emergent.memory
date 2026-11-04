package mcp

import (
	"sync"
	"testing"
)

// TestSSEStreamCloseIsIdempotent pins #2338: session termination (DELETE) and
// the GET handler's deferred cleanup both reach a stream, so closing must be
// safe twice.
func TestSSEStreamCloseIsIdempotent(t *testing.T) {
	stream := &SSEStream{Done: make(chan struct{})}
	stream.Close()
	stream.Close()

	select {
	case <-stream.Done:
	default:
		t.Fatal("Close did not close Done")
	}
}

// TestSSEStreamCloseConcurrent covers the real race shape: DELETE and the SSE
// handler's defer closing the same stream from two goroutines.
func TestSSEStreamCloseConcurrent(t *testing.T) {
	stream := &SSEStream{Done: make(chan struct{})}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stream.Close()
		}()
	}
	wg.Wait()

	select {
	case <-stream.Done:
	default:
		t.Fatal("Done was not closed")
	}
}
