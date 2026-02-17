package scheduler

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestScheduler_IsRunning(t *testing.T) {
	log := slog.Default()
	s := NewScheduler(log)

	// Initially should not be running
	if s.IsRunning() {
		t.Error("New scheduler should not be running")
	}

	// After Start, should be running
	// Note: We can't easily test Start/Stop without a context,
	// but we can test the internal running field
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	if !s.IsRunning() {
		t.Error("Scheduler should be running after setting running=true")
	}

	s.mu.Lock()
	s.running = false
	s.mu.Unlock()

	if s.IsRunning() {
		t.Error("Scheduler should not be running after setting running=false")
	}
}

func TestScheduler_ListTasks(t *testing.T) {
	log := slog.Default()
	s := NewScheduler(log)

	// Initially should have no tasks
	tasks := s.ListTasks()
	if len(tasks) != 0 {
		t.Errorf("New scheduler should have 0 tasks, got %d", len(tasks))
	}

	// Manually add a task entry
	s.mu.Lock()
	s.tasks["task1"] = 1
	s.tasks["task2"] = 2
	s.mu.Unlock()

	tasks = s.ListTasks()
	if len(tasks) != 2 {
		t.Errorf("Expected 2 tasks, got %d", len(tasks))
	}

	// Check that both tasks are present
	hasTask1, hasTask2 := false, false
	for _, name := range tasks {
		if name == "task1" {
			hasTask1 = true
		}
		if name == "task2" {
			hasTask2 = true
		}
	}

	if !hasTask1 {
		t.Error("Expected task1 in list")
	}
	if !hasTask2 {
		t.Error("Expected task2 in list")
	}
}

func TestScheduler_ListTasks_Empty(t *testing.T) {
	log := slog.Default()
	s := NewScheduler(log)

	tasks := s.ListTasks()
	if tasks == nil {
		t.Error("ListTasks should return non-nil slice")
	}
	if len(tasks) != 0 {
		t.Errorf("ListTasks should return empty slice, got %d items", len(tasks))
	}
}

func TestNewScheduler(t *testing.T) {
	log := slog.Default()
	s := NewScheduler(log)

	if s == nil {
		t.Fatal("NewScheduler returned nil")
	}
	if s.cron == nil {
		t.Error("Scheduler cron should not be nil")
	}
	if s.tasks == nil {
		t.Error("Scheduler tasks map should not be nil")
	}
	if s.running {
		t.Error("New scheduler should not be running")
	}
}

func TestTaskInfo_Struct(t *testing.T) {
	// Test that TaskInfo struct has the expected fields
	info := TaskInfo{
		Name:     "test-task",
		Schedule: "@every 1h",
	}

	if info.Name != "test-task" {
		t.Errorf("Name = %q, want %q", info.Name, "test-task")
	}
	if info.Schedule != "@every 1h" {
		t.Errorf("Schedule = %q, want %q", info.Schedule, "@every 1h")
	}
	if !info.NextRun.IsZero() {
		t.Error("NextRun should be zero value")
	}
	if !info.PrevRun.IsZero() {
		t.Error("PrevRun should be zero value")
	}
}

func TestScheduler_GetTaskInfo_Empty(t *testing.T) {
	log := slog.Default()
	s := NewScheduler(log)

	info := s.GetTaskInfo()
	// GetTaskInfo returns nil for empty scheduler (not an empty slice)
	if len(info) != 0 {
		t.Errorf("GetTaskInfo should return empty result, got %d items", len(info))
	}
}

func TestScheduler_GetTaskInfo_WithTasks(t *testing.T) {
	log := slog.Default()
	s := NewScheduler(log)

	// Add a cron task - this adds an entry to both s.tasks and s.cron
	dummyTask := func(ctx context.Context) error {
		return nil
	}

	// Add task with a simple cron schedule
	err := s.AddCronTask("test-task", "@every 1h", dummyTask)
	if err != nil {
		t.Fatalf("Failed to add cron task: %v", err)
	}

	// Now GetTaskInfo should return the task info
	info := s.GetTaskInfo()
	if len(info) != 1 {
		t.Fatalf("GetTaskInfo should return 1 item, got %d", len(info))
	}

	if info[0].Name != "test-task" {
		t.Errorf("TaskInfo.Name = %q, want %q", info[0].Name, "test-task")
	}
	// Schedule should contain a valid time string
	if info[0].Schedule == "" {
		t.Error("TaskInfo.Schedule should not be empty")
	}
}

func TestScheduler_GetTaskInfo_MultipleTasks(t *testing.T) {
	log := slog.Default()
	s := NewScheduler(log)

	dummyTask := func(ctx context.Context) error {
		return nil
	}

	// Add multiple tasks
	err := s.AddCronTask("task-a", "@every 30m", dummyTask)
	if err != nil {
		t.Fatalf("Failed to add task-a: %v", err)
	}

	err = s.AddIntervalTask("task-b", 15*time.Minute, dummyTask)
	if err != nil {
		t.Fatalf("Failed to add task-b: %v", err)
	}

	info := s.GetTaskInfo()
	if len(info) != 2 {
		t.Fatalf("GetTaskInfo should return 2 items, got %d", len(info))
	}

	// Check both tasks are present (order is not guaranteed due to map iteration)
	taskNames := make(map[string]bool)
	for _, ti := range info {
		taskNames[ti.Name] = true
	}

	if !taskNames["task-a"] {
		t.Error("Expected task-a in GetTaskInfo result")
	}
	if !taskNames["task-b"] {
		t.Error("Expected task-b in GetTaskInfo result")
	}
}

func TestScheduler_AddCronTask_ReplaceExisting(t *testing.T) {
	log := slog.Default()
	s := NewScheduler(log)

	dummyTask := func(ctx context.Context) error {
		return nil
	}

	// Add a task
	err := s.AddCronTask("task1", "@every 1h", dummyTask)
	if err != nil {
		t.Fatalf("Failed to add task: %v", err)
	}

	// Verify task exists
	tasks := s.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("Expected 1 task, got %d", len(tasks))
	}

	// Replace with a new task (same name)
	err = s.AddCronTask("task1", "@every 30m", dummyTask)
	if err != nil {
		t.Fatalf("Failed to replace task: %v", err)
	}

	// Should still have only 1 task (replaced)
	tasks = s.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("Expected 1 task after replace, got %d", len(tasks))
	}
}

func TestScheduler_AddIntervalTask_ReplaceExisting(t *testing.T) {
	log := slog.Default()
	s := NewScheduler(log)

	dummyTask := func(ctx context.Context) error {
		return nil
	}

	// Add a task
	err := s.AddIntervalTask("task1", 1*time.Hour, dummyTask)
	if err != nil {
		t.Fatalf("Failed to add task: %v", err)
	}

	// Verify task exists
	tasks := s.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("Expected 1 task, got %d", len(tasks))
	}

	// Replace with a new task (same name)
	err = s.AddIntervalTask("task1", 30*time.Minute, dummyTask)
	if err != nil {
		t.Fatalf("Failed to replace task: %v", err)
	}

	// Should still have only 1 task (replaced)
	tasks = s.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("Expected 1 task after replace, got %d", len(tasks))
	}
}

func TestScheduler_AddCronTask_InvalidSchedule(t *testing.T) {
	log := slog.Default()
	s := NewScheduler(log)

	dummyTask := func(ctx context.Context) error {
		return nil
	}

	// Try to add task with invalid cron schedule
	err := s.AddCronTask("task1", "not a valid schedule", dummyTask)
	if err == nil {
		t.Error("Expected error for invalid schedule, got nil")
	}

	// Verify no task was added
	tasks := s.ListTasks()
	if len(tasks) != 0 {
		t.Errorf("Expected 0 tasks after failed add, got %d", len(tasks))
	}
}

// =============================================================================
// Config Helper Functions Tests
// =============================================================================

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		envValue   string
		setEnv     bool
		defaultVal bool
		want       bool
	}{
		{
			name:       "env not set returns default true",
			key:        "TEST_BOOL_NOT_SET",
			setEnv:     false,
			defaultVal: true,
			want:       true,
		},
		{
			name:       "env not set returns default false",
			key:        "TEST_BOOL_NOT_SET_FALSE",
			setEnv:     false,
			defaultVal: false,
			want:       false,
		},
		{
			name:       "env set to true",
			key:        "TEST_BOOL_TRUE",
			envValue:   "true",
			setEnv:     true,
			defaultVal: false,
			want:       true,
		},
		{
			name:       "env set to false",
			key:        "TEST_BOOL_FALSE",
			envValue:   "false",
			setEnv:     true,
			defaultVal: true,
			want:       false,
		},
		{
			name:       "env set to 1 (truthy)",
			key:        "TEST_BOOL_ONE",
			envValue:   "1",
			setEnv:     true,
			defaultVal: false,
			want:       true,
		},
		{
			name:       "env set to 0 (falsy)",
			key:        "TEST_BOOL_ZERO",
			envValue:   "0",
			setEnv:     true,
			defaultVal: true,
			want:       false,
		},
		{
			name:       "env set to invalid value returns default",
			key:        "TEST_BOOL_INVALID",
			envValue:   "invalid",
			setEnv:     true,
			defaultVal: true,
			want:       true,
		},
		{
			name:       "env set to empty string returns default",
			key:        "TEST_BOOL_EMPTY",
			envValue:   "",
			setEnv:     true,
			defaultVal: false,
			want:       false,
		},
		{
			name:       "env set to TRUE (uppercase)",
			key:        "TEST_BOOL_UPPER",
			envValue:   "TRUE",
			setEnv:     true,
			defaultVal: false,
			want:       true,
		},
		{
			name:       "env set to False (mixed case)",
			key:        "TEST_BOOL_MIXED",
			envValue:   "False",
			setEnv:     true,
			defaultVal: true,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up env var before and after test
			origVal, hadOrig := os.LookupEnv(tt.key)
			defer func() {
				if hadOrig {
					os.Setenv(tt.key, origVal)
				} else {
					os.Unsetenv(tt.key)
				}
			}()

			if tt.setEnv {
				os.Setenv(tt.key, tt.envValue)
			} else {
				os.Unsetenv(tt.key)
			}

			got := getEnvBool(tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("getEnvBool(%q, %v) = %v, want %v", tt.key, tt.defaultVal, got, tt.want)
			}
		})
	}
}

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		envValue   string
		setEnv     bool
		defaultVal int
		want       int
	}{
		{
			name:       "env not set returns default",
			key:        "TEST_INT_NOT_SET",
			setEnv:     false,
			defaultVal: 42,
			want:       42,
		},
		{
			name:       "env set to valid int",
			key:        "TEST_INT_VALID",
			envValue:   "100",
			setEnv:     true,
			defaultVal: 0,
			want:       100,
		},
		{
			name:       "env set to zero",
			key:        "TEST_INT_ZERO",
			envValue:   "0",
			setEnv:     true,
			defaultVal: 99,
			want:       0,
		},
		{
			name:       "env set to negative",
			key:        "TEST_INT_NEG",
			envValue:   "-50",
			setEnv:     true,
			defaultVal: 0,
			want:       -50,
		},
		{
			name:       "env set to invalid value returns default",
			key:        "TEST_INT_INVALID",
			envValue:   "not-a-number",
			setEnv:     true,
			defaultVal: 30,
			want:       30,
		},
		{
			name:       "env set to float returns default",
			key:        "TEST_INT_FLOAT",
			envValue:   "3.14",
			setEnv:     true,
			defaultVal: 5,
			want:       5,
		},
		{
			name:       "env set to empty string returns default",
			key:        "TEST_INT_EMPTY",
			envValue:   "",
			setEnv:     true,
			defaultVal: 10,
			want:       10,
		},
		{
			name:       "env set to large int",
			key:        "TEST_INT_LARGE",
			envValue:   "999999",
			setEnv:     true,
			defaultVal: 0,
			want:       999999,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origVal, hadOrig := os.LookupEnv(tt.key)
			defer func() {
				if hadOrig {
					os.Setenv(tt.key, origVal)
				} else {
					os.Unsetenv(tt.key)
				}
			}()

			if tt.setEnv {
				os.Setenv(tt.key, tt.envValue)
			} else {
				os.Unsetenv(tt.key)
			}

			got := getEnvInt(tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("getEnvInt(%q, %d) = %d, want %d", tt.key, tt.defaultVal, got, tt.want)
			}
		})
	}
}

func TestGetEnvDuration(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		envValue   string
		setEnv     bool
		defaultVal time.Duration
		want       time.Duration
	}{
		{
			name:       "env not set returns default",
			key:        "TEST_DUR_NOT_SET",
			setEnv:     false,
			defaultVal: 5 * time.Minute,
			want:       5 * time.Minute,
		},
		{
			name:       "env set to milliseconds",
			key:        "TEST_DUR_MS",
			envValue:   "1000",
			setEnv:     true,
			defaultVal: 0,
			want:       1 * time.Second,
		},
		{
			name:       "env set to zero",
			key:        "TEST_DUR_ZERO",
			envValue:   "0",
			setEnv:     true,
			defaultVal: time.Minute,
			want:       0,
		},
		{
			name:       "env set to large value (1 hour in ms)",
			key:        "TEST_DUR_HOUR",
			envValue:   "3600000",
			setEnv:     true,
			defaultVal: 0,
			want:       time.Hour,
		},
		{
			name:       "env set to invalid value returns default",
			key:        "TEST_DUR_INVALID",
			envValue:   "not-a-number",
			setEnv:     true,
			defaultVal: 10 * time.Second,
			want:       10 * time.Second,
		},
		{
			name:       "env set to empty string returns default",
			key:        "TEST_DUR_EMPTY",
			envValue:   "",
			setEnv:     true,
			defaultVal: 30 * time.Second,
			want:       30 * time.Second,
		},
		{
			name:       "env set to 500ms",
			key:        "TEST_DUR_500",
			envValue:   "500",
			setEnv:     true,
			defaultVal: 0,
			want:       500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origVal, hadOrig := os.LookupEnv(tt.key)
			defer func() {
				if hadOrig {
					os.Setenv(tt.key, origVal)
				} else {
					os.Unsetenv(tt.key)
				}
			}()

			if tt.setEnv {
				os.Setenv(tt.key, tt.envValue)
			} else {
				os.Unsetenv(tt.key)
			}

			got := getEnvDuration(tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("getEnvDuration(%q, %v) = %v, want %v", tt.key, tt.defaultVal, got, tt.want)
			}
		})
	}
}

func TestNewConfig(t *testing.T) {
	// Save original env vars
	envVars := []string{
		"SCHEDULER_ENABLED",
		"REVISION_COUNT_REFRESH_INTERVAL_MS",
		"TAG_CLEANUP_INTERVAL_MS",
		"CACHE_CLEANUP_INTERVAL",
		"STALE_JOB_CLEANUP_INTERVAL_MS",
		"STALE_JOB_MINUTES",
	}
	origVals := make(map[string]string)
	hadOrig := make(map[string]bool)

	for _, key := range envVars {
		val, exists := os.LookupEnv(key)
		origVals[key] = val
		hadOrig[key] = exists
	}

	// Restore original env vars after test
	defer func() {
		for _, key := range envVars {
			if hadOrig[key] {
				os.Setenv(key, origVals[key])
			} else {
				os.Unsetenv(key)
			}
		}
	}()

	t.Run("default values when no env vars set", func(t *testing.T) {
		// Unset all env vars
		for _, key := range envVars {
			os.Unsetenv(key)
		}

		cfg := NewConfig()

		if !cfg.Enabled {
			t.Error("Enabled should default to true")
		}
		if cfg.RevisionCountRefreshInterval != 5*time.Minute {
			t.Errorf("RevisionCountRefreshInterval = %v, want 5m", cfg.RevisionCountRefreshInterval)
		}
		if cfg.TagCleanupInterval != 5*time.Minute {
			t.Errorf("TagCleanupInterval = %v, want 5m", cfg.TagCleanupInterval)
		}
		if cfg.CacheCleanupInterval != 15*time.Minute {
			t.Errorf("CacheCleanupInterval = %v, want 15m", cfg.CacheCleanupInterval)
		}
		if cfg.StaleJobCleanupInterval != 10*time.Minute {
			t.Errorf("StaleJobCleanupInterval = %v, want 10m", cfg.StaleJobCleanupInterval)
		}
		if cfg.StaleJobMinutes != 30 {
			t.Errorf("StaleJobMinutes = %d, want 30", cfg.StaleJobMinutes)
		}
	})

	t.Run("custom values from env vars", func(t *testing.T) {
		os.Setenv("SCHEDULER_ENABLED", "false")
		os.Setenv("REVISION_COUNT_REFRESH_INTERVAL_MS", "60000") // 1 minute
		os.Setenv("TAG_CLEANUP_INTERVAL_MS", "120000")           // 2 minutes
		os.Setenv("CACHE_CLEANUP_INTERVAL", "300000")            // 5 minutes
		os.Setenv("STALE_JOB_CLEANUP_INTERVAL_MS", "600000")     // 10 minutes
		os.Setenv("STALE_JOB_MINUTES", "60")

		cfg := NewConfig()

		if cfg.Enabled {
			t.Error("Enabled should be false when SCHEDULER_ENABLED=false")
		}
		if cfg.RevisionCountRefreshInterval != time.Minute {
			t.Errorf("RevisionCountRefreshInterval = %v, want 1m", cfg.RevisionCountRefreshInterval)
		}
		if cfg.TagCleanupInterval != 2*time.Minute {
			t.Errorf("TagCleanupInterval = %v, want 2m", cfg.TagCleanupInterval)
		}
		if cfg.CacheCleanupInterval != 5*time.Minute {
			t.Errorf("CacheCleanupInterval = %v, want 5m", cfg.CacheCleanupInterval)
		}
		if cfg.StaleJobCleanupInterval != 10*time.Minute {
			t.Errorf("StaleJobCleanupInterval = %v, want 10m", cfg.StaleJobCleanupInterval)
		}
		if cfg.StaleJobMinutes != 60 {
			t.Errorf("StaleJobMinutes = %d, want 60", cfg.StaleJobMinutes)
		}
	})
}

func TestAddScheduledTask_CronOverridesInterval(t *testing.T) {
	log := slog.Default()
	s := NewScheduler(log)

	task := func(ctx context.Context) error { return nil }

	// With cron schedule set, should use AddCronTask
	err := addScheduledTask(s, log, "test_cron", "0 0 2 * * *", 5*time.Minute, task)
	if err != nil {
		t.Fatalf("addScheduledTask with cron schedule failed: %v", err)
	}

	tasks := s.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0] != "test_cron" {
		t.Errorf("task name = %q, want test_cron", tasks[0])
	}
}

func TestAddScheduledTask_FallbackToInterval(t *testing.T) {
	log := slog.Default()
	s := NewScheduler(log)

	task := func(ctx context.Context) error { return nil }

	// With empty cron schedule, should use AddIntervalTask
	err := addScheduledTask(s, log, "test_interval", "", 5*time.Minute, task)
	if err != nil {
		t.Fatalf("addScheduledTask with interval fallback failed: %v", err)
	}

	tasks := s.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0] != "test_interval" {
		t.Errorf("task name = %q, want test_interval", tasks[0])
	}
}

func TestNewConfig_CronScheduleEnvVars(t *testing.T) {
	// Set cron schedule env vars
	os.Setenv("REVISION_COUNT_REFRESH_SCHEDULE", "0 */5 * * *")
	os.Setenv("TAG_CLEANUP_SCHEDULE", "0 2 * * *")
	os.Setenv("CACHE_CLEANUP_SCHEDULE", "0 */15 * * *")
	os.Setenv("STALE_JOB_CLEANUP_SCHEDULE", "0 */10 * * *")
	defer func() {
		os.Unsetenv("REVISION_COUNT_REFRESH_SCHEDULE")
		os.Unsetenv("TAG_CLEANUP_SCHEDULE")
		os.Unsetenv("CACHE_CLEANUP_SCHEDULE")
		os.Unsetenv("STALE_JOB_CLEANUP_SCHEDULE")
	}()

	cfg := NewConfig()

	if cfg.RevisionCountRefreshSchedule != "0 */5 * * *" {
		t.Errorf("RevisionCountRefreshSchedule = %q, want %q", cfg.RevisionCountRefreshSchedule, "0 */5 * * *")
	}
	if cfg.TagCleanupSchedule != "0 2 * * *" {
		t.Errorf("TagCleanupSchedule = %q, want %q", cfg.TagCleanupSchedule, "0 2 * * *")
	}
	if cfg.CacheCleanupSchedule != "0 */15 * * *" {
		t.Errorf("CacheCleanupSchedule = %q, want %q", cfg.CacheCleanupSchedule, "0 */15 * * *")
	}
	if cfg.StaleJobCleanupSchedule != "0 */10 * * *" {
		t.Errorf("StaleJobCleanupSchedule = %q, want %q", cfg.StaleJobCleanupSchedule, "0 */10 * * *")
	}
}

func TestNewConfig_DefaultCronScheduleEmpty(t *testing.T) {
	// Ensure no env vars set
	os.Unsetenv("REVISION_COUNT_REFRESH_SCHEDULE")
	os.Unsetenv("TAG_CLEANUP_SCHEDULE")
	os.Unsetenv("CACHE_CLEANUP_SCHEDULE")
	os.Unsetenv("STALE_JOB_CLEANUP_SCHEDULE")

	cfg := NewConfig()

	if cfg.RevisionCountRefreshSchedule != "" {
		t.Errorf("RevisionCountRefreshSchedule should be empty by default, got %q", cfg.RevisionCountRefreshSchedule)
	}
	if cfg.TagCleanupSchedule != "" {
		t.Errorf("TagCleanupSchedule should be empty by default, got %q", cfg.TagCleanupSchedule)
	}
	if cfg.CacheCleanupSchedule != "" {
		t.Errorf("CacheCleanupSchedule should be empty by default, got %q", cfg.CacheCleanupSchedule)
	}
	if cfg.StaleJobCleanupSchedule != "" {
		t.Errorf("StaleJobCleanupSchedule should be empty by default, got %q", cfg.StaleJobCleanupSchedule)
	}
}
