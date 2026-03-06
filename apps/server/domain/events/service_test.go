package events

import (
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestNewService(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	assert.NotNil(t, svc)
	assert.NotNil(t, svc.log)
	assert.NotNil(t, svc.subscribers)
	assert.Empty(t, svc.subscribers)
}

func TestSubscribe(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	projectID := "project-123"
	callback := func(event EntityEvent) {}

	// Subscribe
	unsubscribe := svc.Subscribe(projectID, callback)
	assert.NotNil(t, unsubscribe)

	// Verify subscriber count
	assert.Equal(t, 1, svc.GetSubscriberCount(projectID))
	assert.Equal(t, 1, svc.GetTotalSubscriberCount())

	// Unsubscribe
	unsubscribe()

	// After unsubscribe, project should be removed from map
	assert.Equal(t, 0, svc.GetSubscriberCount(projectID))
	assert.Equal(t, 0, svc.GetTotalSubscriberCount())
}

func TestSubscribe_MultipleSubscribers(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	projectID := "project-123"
	
	unsub1 := svc.Subscribe(projectID, func(event EntityEvent) {})
	unsub2 := svc.Subscribe(projectID, func(event EntityEvent) {})
	unsub3 := svc.Subscribe(projectID, func(event EntityEvent) {})

	assert.Equal(t, 3, svc.GetSubscriberCount(projectID))
	assert.Equal(t, 3, svc.GetTotalSubscriberCount())

	// Unsubscribe one
	unsub2()
	assert.Equal(t, 2, svc.GetSubscriberCount(projectID))

	// Unsubscribe remaining
	unsub1()
	unsub3()
	assert.Equal(t, 0, svc.GetSubscriberCount(projectID))
}

func TestSubscribe_MultipleProjects(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	project1 := "project-1"
	project2 := "project-2"
	
	svc.Subscribe(project1, func(event EntityEvent) {})
	svc.Subscribe(project1, func(event EntityEvent) {})
	svc.Subscribe(project2, func(event EntityEvent) {})

	assert.Equal(t, 2, svc.GetSubscriberCount(project1))
	assert.Equal(t, 1, svc.GetSubscriberCount(project2))
	assert.Equal(t, 3, svc.GetTotalSubscriberCount())
}

func TestEmit(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	projectID := "project-123"
	var receivedEvent EntityEvent
	var wg sync.WaitGroup
	wg.Add(1)

	svc.Subscribe(projectID, func(event EntityEvent) {
		receivedEvent = event
		wg.Done()
	})

	// Emit event
	event := EntityEvent{
		Type:      EventTypeCreated,
		Entity:    EntityDocument,
		ProjectID: projectID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	svc.Emit(event)

	// Wait for async delivery
	wg.Wait()

	assert.Equal(t, EventTypeCreated, receivedEvent.Type)
	assert.Equal(t, EntityDocument, receivedEvent.Entity)
	assert.Equal(t, projectID, receivedEvent.ProjectID)
}

func TestEmit_NoSubscribers(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	// Emit to project with no subscribers - should not panic
	event := EntityEvent{
		Type:      EventTypeCreated,
		Entity:    EntityDocument,
		ProjectID: "no-subscribers",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	
	// Should not panic
	assert.NotPanics(t, func() {
		svc.Emit(event)
	})
}

func TestEmit_MultipleSubscribers(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	projectID := "project-123"
	var counter int32
	var wg sync.WaitGroup
	wg.Add(3)

	for i := 0; i < 3; i++ {
		svc.Subscribe(projectID, func(event EntityEvent) {
			atomic.AddInt32(&counter, 1)
			wg.Done()
		})
	}

	event := EntityEvent{
		Type:      EventTypeUpdated,
		Entity:    EntityChunk,
		ProjectID: projectID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	svc.Emit(event)

	wg.Wait()
	assert.Equal(t, int32(3), atomic.LoadInt32(&counter))
}

func TestEmit_OnlyTargetProject(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	project1 := "project-1"
	project2 := "project-2"
	
	var project1Called, project2Called int32
	var wg sync.WaitGroup
	wg.Add(1)

	svc.Subscribe(project1, func(event EntityEvent) {
		atomic.AddInt32(&project1Called, 1)
		wg.Done()
	})
	svc.Subscribe(project2, func(event EntityEvent) {
		atomic.AddInt32(&project2Called, 1)
	})

	// Emit to project1 only
	event := EntityEvent{
		Type:      EventTypeCreated,
		Entity:    EntityDocument,
		ProjectID: project1,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	svc.Emit(event)

	wg.Wait()
	// Give a little time to ensure project2 callback wasn't called
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, int32(1), atomic.LoadInt32(&project1Called))
	assert.Equal(t, int32(0), atomic.LoadInt32(&project2Called))
}

func TestEmitCreated(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	projectID := "project-123"
	entityID := "doc-456"
	var receivedEvent EntityEvent
	var wg sync.WaitGroup
	wg.Add(1)

	svc.Subscribe(projectID, func(event EntityEvent) {
		receivedEvent = event
		wg.Done()
	})

	svc.EmitCreated(EntityDocument, entityID, projectID, nil)

	wg.Wait()

	assert.Equal(t, EventTypeCreated, receivedEvent.Type)
	assert.Equal(t, EntityDocument, receivedEvent.Entity)
	require.NotNil(t, receivedEvent.ID)
	assert.Equal(t, entityID, *receivedEvent.ID)
	assert.Equal(t, projectID, receivedEvent.ProjectID)
	assert.NotEmpty(t, receivedEvent.Timestamp)
}

func TestEmitCreated_WithOptions(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	projectID := "project-123"
	entityID := "obj-789"
	var receivedEvent EntityEvent
	var wg sync.WaitGroup
	wg.Add(1)

	svc.Subscribe(projectID, func(event EntityEvent) {
		receivedEvent = event
		wg.Done()
	})

	version := 5
	opts := &EmitOptions{
		Data:       map[string]any{"key": "value"},
		Actor:      &ActorContext{ActorType: ActorUser, ActorID: "user-1"},
		Version:    &version,
		ObjectType: "Person",
	}

	svc.EmitCreated(EntityGraphObject, entityID, projectID, opts)

	wg.Wait()

	assert.Equal(t, EventTypeCreated, receivedEvent.Type)
	assert.Equal(t, EntityGraphObject, receivedEvent.Entity)
	assert.Equal(t, map[string]any{"key": "value"}, receivedEvent.Data)
	require.NotNil(t, receivedEvent.Actor)
	assert.Equal(t, ActorUser, receivedEvent.Actor.ActorType)
	assert.Equal(t, "user-1", receivedEvent.Actor.ActorID)
	require.NotNil(t, receivedEvent.Version)
	assert.Equal(t, 5, *receivedEvent.Version)
	assert.Equal(t, "Person", receivedEvent.ObjectType)
}

func TestEmitUpdated(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	projectID := "project-123"
	entityID := "doc-456"
	var receivedEvent EntityEvent
	var wg sync.WaitGroup
	wg.Add(1)

	svc.Subscribe(projectID, func(event EntityEvent) {
		receivedEvent = event
		wg.Done()
	})

	svc.EmitUpdated(EntityDocument, entityID, projectID, nil)

	wg.Wait()

	assert.Equal(t, EventTypeUpdated, receivedEvent.Type)
	assert.Equal(t, EntityDocument, receivedEvent.Entity)
	require.NotNil(t, receivedEvent.ID)
	assert.Equal(t, entityID, *receivedEvent.ID)
}

func TestEmitUpdated_WithOptions(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	projectID := "project-123"
	entityID := "obj-789"
	var receivedEvent EntityEvent
	var wg sync.WaitGroup
	wg.Add(1)

	svc.Subscribe(projectID, func(event EntityEvent) {
		receivedEvent = event
		wg.Done()
	})

	opts := &EmitOptions{
		Data:  map[string]any{"updated": true},
		Actor: &ActorContext{ActorType: ActorAgent, ActorID: "agent-1"},
	}

	svc.EmitUpdated(EntityChunk, entityID, projectID, opts)

	wg.Wait()

	assert.Equal(t, EventTypeUpdated, receivedEvent.Type)
	assert.Equal(t, map[string]any{"updated": true}, receivedEvent.Data)
	require.NotNil(t, receivedEvent.Actor)
	assert.Equal(t, ActorAgent, receivedEvent.Actor.ActorType)
}

func TestEmitDeleted(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	projectID := "project-123"
	entityID := "doc-456"
	var receivedEvent EntityEvent
	var wg sync.WaitGroup
	wg.Add(1)

	svc.Subscribe(projectID, func(event EntityEvent) {
		receivedEvent = event
		wg.Done()
	})

	svc.EmitDeleted(EntityDocument, entityID, projectID, nil)

	wg.Wait()

	assert.Equal(t, EventTypeDeleted, receivedEvent.Type)
	assert.Equal(t, EntityDocument, receivedEvent.Entity)
	require.NotNil(t, receivedEvent.ID)
	assert.Equal(t, entityID, *receivedEvent.ID)
	// Deleted events shouldn't have Data by default
	assert.Nil(t, receivedEvent.Data)
}

func TestEmitDeleted_WithOptions(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	projectID := "project-123"
	entityID := "obj-789"
	var receivedEvent EntityEvent
	var wg sync.WaitGroup
	wg.Add(1)

	svc.Subscribe(projectID, func(event EntityEvent) {
		receivedEvent = event
		wg.Done()
	})

	version := 10
	opts := &EmitOptions{
		Actor:      &ActorContext{ActorType: ActorSystem},
		Version:    &version,
		ObjectType: "Company",
	}

	svc.EmitDeleted(EntityGraphObject, entityID, projectID, opts)

	wg.Wait()

	assert.Equal(t, EventTypeDeleted, receivedEvent.Type)
	require.NotNil(t, receivedEvent.Actor)
	assert.Equal(t, ActorSystem, receivedEvent.Actor.ActorType)
	require.NotNil(t, receivedEvent.Version)
	assert.Equal(t, 10, *receivedEvent.Version)
	assert.Equal(t, "Company", receivedEvent.ObjectType)
}

func TestEmitBatch(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	projectID := "project-123"
	ids := []string{"doc-1", "doc-2", "doc-3"}
	var receivedEvent EntityEvent
	var wg sync.WaitGroup
	wg.Add(1)

	svc.Subscribe(projectID, func(event EntityEvent) {
		receivedEvent = event
		wg.Done()
	})

	data := map[string]any{"action": "bulk_delete"}
	svc.EmitBatch(EntityDocument, ids, projectID, data)

	wg.Wait()

	assert.Equal(t, EventTypeBatch, receivedEvent.Type)
	assert.Equal(t, EntityDocument, receivedEvent.Entity)
	assert.Nil(t, receivedEvent.ID) // Batch events don't have single ID
	assert.Equal(t, ids, receivedEvent.IDs)
	assert.Equal(t, data, receivedEvent.Data)
}

func TestGetSubscriberCount_EmptyProject(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	// Non-existent project should return 0
	assert.Equal(t, 0, svc.GetSubscriberCount("non-existent"))
}

func TestGetTotalSubscriberCount_Empty(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	assert.Equal(t, 0, svc.GetTotalSubscriberCount())
}

func TestConcurrentSubscribeUnsubscribe(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	projectID := "project-123"
	var wg sync.WaitGroup
	
	// Spawn multiple goroutines that subscribe and unsubscribe
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unsub := svc.Subscribe(projectID, func(event EntityEvent) {})
			time.Sleep(time.Millisecond)
			unsub()
		}()
	}

	wg.Wait()

	// After all goroutines complete, should have 0 subscribers
	assert.Equal(t, 0, svc.GetSubscriberCount(projectID))
}

func TestConcurrentEmit(t *testing.T) {
	log := newTestLogger()
	svc := NewService(log)

	projectID := "project-123"
	var counter int32
	var wg sync.WaitGroup

	// Add subscriber
	svc.Subscribe(projectID, func(event EntityEvent) {
		atomic.AddInt32(&counter, 1)
		wg.Done()
	})

	// Emit 50 events concurrently
	numEvents := 50
	wg.Add(numEvents)
	
	for i := 0; i < numEvents; i++ {
		go func(n int) {
			event := EntityEvent{
				Type:      EventTypeUpdated,
				Entity:    EntityDocument,
				ProjectID: projectID,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			}
			svc.Emit(event)
		}(i)
	}

	wg.Wait()
	assert.Equal(t, int32(numEvents), atomic.LoadInt32(&counter))
}
