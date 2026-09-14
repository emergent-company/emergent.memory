package main

import (
	"sync/atomic"
	"testing"
)

func TestRunConcurrentlyRunsEveryIndexOnce(t *testing.T) {
	const n = 64
	var hits [n]atomic.Int32
	runConcurrently(n, func(i int) { hits[i].Add(1) })
	for i := range n {
		if got := hits[i].Load(); got != 1 {
			t.Fatalf("index %d ran %d times, want 1", i, got)
		}
	}
}

func TestRunConcurrentlyHandlesZero(t *testing.T) {
	runConcurrently(0, func(int) { t.Fatal("fn must not run for n=0") })
}
