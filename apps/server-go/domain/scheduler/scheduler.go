package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/emergent-company/emergent/pkg/logger"
)

// Scheduler manages scheduled tasks using robfig/cron.
// It supports both cron expressions and interval-based scheduling.
type Scheduler struct {
	cron    *cron.Cron
	log     *slog.Logger
	tasks   map[string]cron.EntryID
	mu      sync.RWMutex
	running bool
}

// NewScheduler creates a new scheduler
func NewScheduler(log *slog.Logger) *Scheduler {
	// Create cron with seconds precision
	c := cron.New(cron.WithSeconds())
	
	return &Scheduler{
		cron:  c,
		log:   log.With(logger.Scope("scheduler")),
		tasks: make(map[string]cron.EntryID),
	}
}

// Start begins the scheduler
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	s.cron.Start()
	s.running = true
	s.log.Info("scheduler started", slog.Int("tasks", len(s.tasks)))

	return nil
}

// Stop gracefully stops the scheduler
func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	// Stop with context timeout
	stopCtx := s.cron.Stop()
	select {
	case <-stopCtx.Done():
		s.log.Info("scheduler stopped gracefully")
	case <-ctx.Done():
		s.log.Warn("scheduler stop timeout")
	}

	s.running = false
	return nil
}

// AddCronTask adds a task with a cron expression
// Cron format: "second minute hour day-of-month month day-of-week"
func (s *Scheduler) AddCronTask(name string, schedule string, task TaskFunc) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing task if any
	if entryID, ok := s.tasks[name]; ok {
		s.cron.Remove(entryID)
		delete(s.tasks, name)
	}

	entryID, err := s.cron.AddFunc(schedule, func() {
		s.runTask(name, task)
	})
	if err != nil {
		return err
	}

	s.tasks[name] = entryID
	s.log.Info("added cron task",
		slog.String("name", name),
		slog.String("schedule", schedule))

	return nil
}

// AddIntervalTask adds a task that runs at a fixed interval
func (s *Scheduler) AddIntervalTask(name string, interval time.Duration, task TaskFunc) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing task if any
	if entryID, ok := s.tasks[name]; ok {
		s.cron.Remove(entryID)
		delete(s.tasks, name)
	}

	// Convert interval to cron schedule
	// Use @every directive for simple intervals
	schedule := "@every " + interval.String()

	entryID, err := s.cron.AddFunc(schedule, func() {
		s.runTask(name, task)
	})
	if err != nil {
		return err
	}

	s.tasks[name] = entryID
	s.log.Info("added interval task",
		slog.String("name", name),
		slog.Duration("interval", interval))

	return nil
}

// RemoveTask removes a scheduled task
func (s *Scheduler) RemoveTask(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, ok := s.tasks[name]; ok {
		s.cron.Remove(entryID)
		delete(s.tasks, name)
		s.log.Info("removed task", slog.String("name", name))
	}
}

// runTask executes a task with error handling
func (s *Scheduler) runTask(name string, task TaskFunc) {
	startTime := time.Now()
	s.log.Debug("running scheduled task", slog.String("name", name))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	if err := task(ctx); err != nil {
		s.log.Error("scheduled task failed",
			slog.String("name", name),
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(startTime)))
		return
	}

	s.log.Debug("scheduled task completed",
		slog.String("name", name),
		slog.Duration("duration", time.Since(startTime)))
}

// ListTasks returns the names of all scheduled tasks
func (s *Scheduler) ListTasks() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.tasks))
	for name := range s.tasks {
		names = append(names, name)
	}
	return names
}

// TaskInfo represents information about a scheduled task
type TaskInfo struct {
	Name     string    `json:"name"`
	NextRun  time.Time `json:"next_run"`
	PrevRun  time.Time `json:"prev_run,omitempty"`
	Schedule string    `json:"schedule"`
}

// GetTaskInfo returns information about all scheduled tasks
func (s *Scheduler) GetTaskInfo() []TaskInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var info []TaskInfo
	entries := s.cron.Entries()

	for name, entryID := range s.tasks {
		for _, entry := range entries {
			if entry.ID == entryID {
				info = append(info, TaskInfo{
					Name:     name,
					NextRun:  entry.Next,
					PrevRun:  entry.Prev,
					Schedule: entry.Schedule.Next(time.Now()).String(),
				})
				break
			}
		}
	}

	return info
}

// IsRunning returns whether the scheduler is running
func (s *Scheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// TaskFunc is the function signature for scheduled tasks
type TaskFunc func(ctx context.Context) error
