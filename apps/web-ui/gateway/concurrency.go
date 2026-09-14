package main

import "golang.org/x/sync/errgroup"

// concurrentFanout bounds the parallel backend calls used to collapse
// sequential N+1 round trips. Kept small so one page load never floods memory
// with simultaneous requests.
const concurrentFanout = 8

// runConcurrently runs fn(i) for every i in [0,n) with at most
// concurrentFanout workers and waits for all to finish. It is for best-effort
// enrichment where per-item errors are intentionally ignored; fn must write
// only to its own index to stay race-free.
func runConcurrently(n int, fn func(i int)) {
	g := new(errgroup.Group)
	g.SetLimit(concurrentFanout)
	for i := range n {
		g.Go(func() error {
			fn(i)
			return nil
		})
	}
	_ = g.Wait()
}
