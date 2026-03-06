package logger

import (
	"errors"
	"log/slog"
	"os"
	"testing"
)

func TestScope(t *testing.T) {
	tests := []struct {
		name  string
		scope string
		want  string
	}{
		{"basic scope", "auth", "auth"},
		{"nested scope", "api.v1.users", "api.v1.users"},
		{"empty scope", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr := Scope(tt.scope)
			if attr.Key != "scope" {
				t.Errorf("Scope() key = %q, want %q", attr.Key, "scope")
			}
			if attr.Value.String() != tt.want {
				t.Errorf("Scope() value = %q, want %q", attr.Value.String(), tt.want)
			}
		})
	}
}

func TestError(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"simple error", errors.New("something went wrong")},
		{"nil error", nil},
		{"wrapped error", errors.Join(errors.New("outer"), errors.New("inner"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr := Error(tt.err)
			if attr.Key != "error" {
				t.Errorf("Error() key = %q, want %q", attr.Key, "error")
			}
			// The value should contain our error
			gotErr := attr.Value.Any()
			if gotErr != tt.err {
				t.Errorf("Error() value = %v, want %v", gotErr, tt.err)
			}
		})
	}
}

func TestNewLogger_DefaultLevel(t *testing.T) {
	// Unset LOG_LEVEL to test default
	os.Unsetenv("LOG_LEVEL")
	os.Unsetenv("GO_ENV")
	
	logger := NewLogger()
	
	if logger == nil {
		t.Fatal("NewLogger() returned nil")
	}
	
	// Default should be info level
	if !logger.Enabled(nil, slog.LevelInfo) {
		t.Error("NewLogger() should have info level enabled by default")
	}
}

func TestNewLogger_DebugLevel(t *testing.T) {
	// Save and restore env
	origLevel := os.Getenv("LOG_LEVEL")
	origEnv := os.Getenv("GO_ENV")
	defer func() {
		if origLevel == "" {
			os.Unsetenv("LOG_LEVEL")
		} else {
			os.Setenv("LOG_LEVEL", origLevel)
		}
		if origEnv == "" {
			os.Unsetenv("GO_ENV")
		} else {
			os.Setenv("GO_ENV", origEnv)
		}
	}()
	
	os.Setenv("LOG_LEVEL", "debug")
	os.Unsetenv("GO_ENV")
	
	logger := NewLogger()
	
	if logger == nil {
		t.Fatal("NewLogger() returned nil")
	}
	
	if !logger.Enabled(nil, slog.LevelDebug) {
		t.Error("NewLogger() should have debug level enabled when LOG_LEVEL=debug")
	}
}

func TestNewLogger_WarnLevel(t *testing.T) {
	origLevel := os.Getenv("LOG_LEVEL")
	defer func() {
		if origLevel == "" {
			os.Unsetenv("LOG_LEVEL")
		} else {
			os.Setenv("LOG_LEVEL", origLevel)
		}
	}()
	
	// Test both "warn" and "warning" values
	for _, level := range []string{"warn", "warning"} {
		os.Setenv("LOG_LEVEL", level)
		
		logger := NewLogger()
		
		if logger == nil {
			t.Fatal("NewLogger() returned nil")
		}
		
		if !logger.Enabled(nil, slog.LevelWarn) {
			t.Errorf("NewLogger() should have warn level enabled when LOG_LEVEL=%s", level)
		}
		if logger.Enabled(nil, slog.LevelInfo) {
			t.Errorf("NewLogger() should NOT have info level enabled when LOG_LEVEL=%s", level)
		}
	}
}

func TestNewLogger_ErrorLevel(t *testing.T) {
	origLevel := os.Getenv("LOG_LEVEL")
	defer func() {
		if origLevel == "" {
			os.Unsetenv("LOG_LEVEL")
		} else {
			os.Setenv("LOG_LEVEL", origLevel)
		}
	}()
	
	os.Setenv("LOG_LEVEL", "error")
	
	logger := NewLogger()
	
	if logger == nil {
		t.Fatal("NewLogger() returned nil")
	}
	
	if !logger.Enabled(nil, slog.LevelError) {
		t.Error("NewLogger() should have error level enabled when LOG_LEVEL=error")
	}
	if logger.Enabled(nil, slog.LevelWarn) {
		t.Error("NewLogger() should NOT have warn level enabled when LOG_LEVEL=error")
	}
}

func TestNewLogger_InfoLevel(t *testing.T) {
	origLevel := os.Getenv("LOG_LEVEL")
	defer func() {
		if origLevel == "" {
			os.Unsetenv("LOG_LEVEL")
		} else {
			os.Setenv("LOG_LEVEL", origLevel)
		}
	}()
	
	os.Setenv("LOG_LEVEL", "info")
	
	logger := NewLogger()
	
	if logger == nil {
		t.Fatal("NewLogger() returned nil")
	}
	
	if !logger.Enabled(nil, slog.LevelInfo) {
		t.Error("NewLogger() should have info level enabled when LOG_LEVEL=info")
	}
	if logger.Enabled(nil, slog.LevelDebug) {
		t.Error("NewLogger() should NOT have debug level enabled when LOG_LEVEL=info")
	}
}

func TestNewLogger_CaseInsensitive(t *testing.T) {
	origLevel := os.Getenv("LOG_LEVEL")
	defer func() {
		if origLevel == "" {
			os.Unsetenv("LOG_LEVEL")
		} else {
			os.Setenv("LOG_LEVEL", origLevel)
		}
	}()
	
	// Test case insensitivity
	testCases := []string{"DEBUG", "Debug", "dEbUg"}
	
	for _, level := range testCases {
		os.Setenv("LOG_LEVEL", level)
		
		logger := NewLogger()
		
		if !logger.Enabled(nil, slog.LevelDebug) {
			t.Errorf("NewLogger() should handle case-insensitive LOG_LEVEL=%s", level)
		}
	}
}

func TestNewLogger_InvalidLevel(t *testing.T) {
	origLevel := os.Getenv("LOG_LEVEL")
	defer func() {
		if origLevel == "" {
			os.Unsetenv("LOG_LEVEL")
		} else {
			os.Setenv("LOG_LEVEL", origLevel)
		}
	}()
	
	os.Setenv("LOG_LEVEL", "invalid")
	
	logger := NewLogger()
	
	if logger == nil {
		t.Fatal("NewLogger() returned nil")
	}
	
	// Invalid level should default to info
	if !logger.Enabled(nil, slog.LevelInfo) {
		t.Error("NewLogger() should default to info level for invalid LOG_LEVEL")
	}
}

func TestNewLogger_ProductionJSON(t *testing.T) {
	// Save and restore env
	origLevel := os.Getenv("LOG_LEVEL")
	origEnv := os.Getenv("GO_ENV")
	defer func() {
		if origLevel == "" {
			os.Unsetenv("LOG_LEVEL")
		} else {
			os.Setenv("LOG_LEVEL", origLevel)
		}
		if origEnv == "" {
			os.Unsetenv("GO_ENV")
		} else {
			os.Setenv("GO_ENV", origEnv)
		}
	}()
	
	os.Unsetenv("LOG_LEVEL")
	os.Setenv("GO_ENV", "production")
	
	logger := NewLogger()
	
	if logger == nil {
		t.Fatal("NewLogger() returned nil")
	}
	
	// Logger should work in production mode (uses JSON handler)
	if !logger.Enabled(nil, slog.LevelInfo) {
		t.Error("NewLogger() should have info level enabled in production")
	}
}
