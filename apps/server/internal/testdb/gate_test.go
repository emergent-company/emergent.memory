package testdb

import (
	"os"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/config"
)

func TestRequired(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"no", false},
		{"random", false},
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"yes", true},
		{"on", true},
		{" 1 ", true},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv(RequireEnv, tc.value)
			if got := Required(); got != tc.want {
				t.Fatalf("Required() with %s=%q = %v, want %v", RequireEnv, tc.value, got, tc.want)
			}
		})
	}
}

func TestApplyNoURLKeepsConfig(t *testing.T) {
	os.Unsetenv(URLEnv)
	cfg := config.DatabaseConfig{Host: "db.internal", Port: 6543, User: "app", Password: "pw", Database: "prod", SSLMode: "require"}
	if err := Apply(&cfg); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := config.DatabaseConfig{Host: "db.internal", Port: 6543, User: "app", Password: "pw", Database: "prod", SSLMode: "require"}
	if cfg != want {
		t.Fatalf("Apply with no %s mutated config: got %+v want %+v", URLEnv, cfg, want)
	}
}

func TestApplyOverridesFromURL(t *testing.T) {
	t.Setenv(URLEnv, "postgres://testuser:s3cret@127.0.0.1:54329/testdb?sslmode=disable")
	cfg := config.DatabaseConfig{Host: "localhost", Port: 5432, User: "emergent", Database: "emergent"}
	if err := Apply(&cfg); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 54329 || cfg.User != "testuser" ||
		cfg.Password != "s3cret" || cfg.Database != "testdb" || cfg.SSLMode != "disable" {
		t.Fatalf("Apply did not override from %s: %+v", URLEnv, cfg)
	}
}

func TestApplyRejectsBadScheme(t *testing.T) {
	t.Setenv(URLEnv, "mysql://user@localhost:3306/db")
	if err := Apply(&config.DatabaseConfig{}); err == nil {
		t.Fatalf("Apply accepted non-postgres scheme")
	}
}

func TestURL(t *testing.T) {
	os.Unsetenv(URLEnv)
	if _, ok := URL(); ok {
		t.Fatalf("URL reported set for empty %s", URLEnv)
	}
	t.Setenv(URLEnv, "postgres://u@h:5/d")
	if dsn, ok := URL(); !ok || dsn != "postgres://u@h:5/d" {
		t.Fatalf("URL = (%q, %v), want the configured DSN", dsn, ok)
	}
}
