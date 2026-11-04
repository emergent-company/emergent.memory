package journal

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestLog_BackpressureDoesNotDrop fills the bounded queue faster than a single
// blocked worker can drain it, then unblocks and stops. Every submitted entry
// must be flushed: Log must apply backpressure on a full queue, never drop.
//
// This is the regression guard for the queue policy — a mutant that replaces
// the blocking send with a non-blocking `default:` drop must fail this test.
func TestLog_BackpressureDoesNotDrop(t *testing.T) {
	repo := &mockRepo{blockInsert: make(chan struct{})}
	cfg := &config.Config{Journal: config.JournalConfig{
		WriterConcurrency: 1,
		WriterQueueSize:   2,
		WriterBatchSize:   1,
	}}
	svc := newService(repo, cfg, quietLogger())

	const n = 12
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.Log(context.Background(), LogParams{
				ProjectID: uuid.New(),
				EventType: EventTypeCreated,
				ActorType: ActorAgent,
			})
		}()
	}

	// Let the worker block on its first InsertBatch and the queue fill, so the
	// remaining Log calls are forced to block on backpressure.
	time.Sleep(100 * time.Millisecond)
	close(repo.blockInsert)
	wg.Wait()
	svc.Stop(context.Background())

	require.Equal(t, n, repo.totalInserted(),
		"all submitted entries must be flushed; none may be dropped when the queue is full")
}

// TestStop_NoEnqueueAfterReturn hammers Log concurrently with Stop. Once Stop
// and every Log call have returned, the queue must be empty: no entry may be
// stranded by a producer that enqueued after Stop had finished draining.
//
// This is the regression guard for the shutdown enqueue race — removing the
// enqueueMu exclusion in Log/Stop must (probabilistically) fail this test.
func TestStop_NoEnqueueAfterReturn(t *testing.T) {
	cfg := &config.Config{Journal: config.JournalConfig{
		WriterConcurrency: 2,
		WriterQueueSize:   8,
		WriterBatchSize:   4,
	}}
	for iter := 0; iter < 300; iter++ {
		repo := &mockRepo{}
		svc := newService(repo, cfg, quietLogger())

		var wg sync.WaitGroup
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				svc.Log(context.Background(), LogParams{
					ProjectID: uuid.New(),
					EventType: EventTypeCreated,
					ActorType: ActorAgent,
				})
			}()
		}
		svc.Stop(context.Background())
		wg.Wait()

		require.Zero(t, len(svc.queue),
			"iteration %d: queue must be empty after Stop and all Log calls returned", iter)
	}
}
