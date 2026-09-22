package testdb

import (
	"net/url"
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

// TestApplyDefaultsOmittedHostAndPort pins that an explicit test DSN which
// omits host/port does not silently keep the ambient POSTGRES_HOST/POSTGRES_PORT
// it is meant to override (libpq defaults apply instead).
func TestApplyDefaultsOmittedHostAndPort(t *testing.T) {
	t.Setenv(URLEnv, "postgres://testuser:pw@/testdb")
	cfg := config.DatabaseConfig{Host: "ambient-host", Port: 9999, User: "ambient", Database: "ambientdb"}
	if err := Apply(&cfg); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if cfg.Host != "localhost" || cfg.Port != 5432 {
		t.Fatalf("Apply left ambient host/port: host=%q port=%d", cfg.Host, cfg.Port)
	}
	if cfg.Database != "testdb" {
		t.Fatalf("Apply did not take the URL database: %q", cfg.Database)
	}
}

// TestApplyRoundTripsEscapedCredentials pins that percent-encoded credentials in
// the DSN are decoded, and that re-building the DSN re-escapes them.
func TestApplyRoundTripsEscapedCredentials(t *testing.T) {
	t.Setenv(URLEnv, "postgres://us%20er:p%40ss%3Aw%2Frd@127.0.0.1:54329/testdb")
	cfg := config.DatabaseConfig{}
	if err := Apply(&cfg); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if cfg.User != "us er" || cfg.Password != "p@ss:w/rd" {
		t.Fatalf("credentials not decoded: user=%q pass=%q", cfg.User, cfg.Password)
	}
	u, err := url.Parse(cfg.DSN())
	if err != nil {
		t.Fatalf("re-parse DSN %q: %v", cfg.DSN(), err)
	}
	pw, _ := u.User.Password()
	if u.User.Username() != "us er" || pw != "p@ss:w/rd" {
		t.Fatalf("DSN did not round-trip credentials: %q", cfg.DSN())
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
