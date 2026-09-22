package main

import (
	"strings"
	"testing"
)

// fakeEnv builds a getenv function backed by a map, for deterministic tests.
func fakeEnv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestResolveTargetPrecedence(t *testing.T) {
	tests := []struct {
		name         string
		env          map[string]string
		wantHost     string
		wantPort     string
		wantUser     string
		wantDB       string
		wantSSL      string
		wantSource   string
		wantExplicit bool
		wantConflict string
	}{
		{
			name:       "built-in defaults when nothing is set",
			env:        map[string]string{"POSTGRES_PASSWORD": "pw"},
			wantHost:   "localhost",
			wantPort:   "5432",
			wantUser:   "emergent",
			wantDB:     "emergent",
			wantSSL:    "disable",
			wantSource: "built-in default",
		},
		{
			name: "POSTGRES_HOST/PORT honored as first-class aliases",
			env: map[string]string{
				"POSTGRES_PASSWORD": "pw",
				"POSTGRES_HOST":     "db",
				"POSTGRES_PORT":     "6543",
			},
			wantHost:     "db",
			wantPort:     "6543",
			wantUser:     "emergent",
			wantDB:       "emergent",
			wantSSL:      "disable",
			wantSource:   "POSTGRES_HOST/POSTGRES_PORT",
			wantExplicit: true,
		},
		{
			name: "DB_HOST/DB_PORT wins over POSTGRES_*",
			env: map[string]string{
				"POSTGRES_PASSWORD": "pw",
				"DB_HOST":           "db-primary",
				"DB_PORT":           "6000",
				"POSTGRES_HOST":     "db",
				"POSTGRES_PORT":     "6543",
			},
			wantHost:     "db-primary",
			wantPort:     "6000",
			wantUser:     "emergent",
			wantDB:       "emergent",
			wantSSL:      "disable",
			wantSource:   "DB_HOST/DB_PORT",
			wantExplicit: true,
		},
		{
			name: "MEMORY_PG_* used when DB_* and POSTGRES_* host/port unset",
			env: map[string]string{
				"POSTGRES_PASSWORD": "pw",
				"MEMORY_PG_HOST":    "memory-db",
				"MEMORY_PG_PORT":    "7777",
			},
			wantHost:     "memory-db",
			wantPort:     "7777",
			wantUser:     "emergent",
			wantDB:       "emergent",
			wantSSL:      "disable",
			wantSource:   "MEMORY_PG_HOST/MEMORY_PG_PORT",
			wantExplicit: true,
		},
		{
			name: "POSTGRES_DB alias used when POSTGRES_DATABASE unset",
			env: map[string]string{
				"POSTGRES_PASSWORD": "pw",
				"POSTGRES_HOST":     "db",
				"POSTGRES_DB":       "memtest",
			},
			wantHost:     "db",
			wantPort:     "5432",
			wantUser:     "emergent",
			wantDB:       "memtest",
			wantSSL:      "disable",
			wantSource:   "POSTGRES_HOST/POSTGRES_PORT",
			wantExplicit: true,
		},
		{
			name: "POSTGRES_DATABASE wins over POSTGRES_DB",
			env: map[string]string{
				"POSTGRES_PASSWORD": "pw",
				"POSTGRES_HOST":     "db",
				"POSTGRES_DATABASE": "legacy-name",
				"POSTGRES_DB":       "other-name",
				"MEMORY_PG_DB":      "third-name",
				"POSTGRES_SSL_MODE": "require",
				"MEMORY_PG_USER":    "someone",
				"POSTGRES_USER":     "explicit-user",
				"DB_SSL_MODE":       "verify-full",
			},
			wantHost:     "db",
			wantPort:     "5432",
			wantUser:     "explicit-user",
			wantDB:       "legacy-name",
			wantSSL:      "verify-full",
			wantSource:   "POSTGRES_HOST/POSTGRES_PORT",
			wantExplicit: true,
		},
		{
			name: "DATABASE_URL fully overrides components",
			env: map[string]string{
				"DATABASE_URL":  "postgres://urluser:urlpass@urlhost:9000/urldb?sslmode=require",
				"DB_HOST":       "ignored",
				"POSTGRES_HOST": "ignored",
			},
			wantHost:     "urlhost",
			wantPort:     "9000",
			wantUser:     "urluser",
			wantDB:       "urldb",
			wantSSL:      "require",
			wantSource:   "DATABASE_URL",
			wantExplicit: true,
		},
		{
			name: "DATABASE_URL defaults port and sslmode when absent",
			env: map[string]string{
				"DATABASE_URL": "postgres://urluser:urlpass@urlhost/urldb",
			},
			wantHost:     "urlhost",
			wantPort:     "5432",
			wantUser:     "urluser",
			wantDB:       "urldb",
			wantSSL:      "disable",
			wantSource:   "DATABASE_URL",
			wantExplicit: true,
		},
		{
			name: "DB_HOST loopback conflicts with POSTGRES_HOST elsewhere",
			env: map[string]string{
				"POSTGRES_PASSWORD": "pw",
				"DB_HOST":           "localhost",
				"POSTGRES_HOST":     "db",
			},
			wantHost:     "localhost",
			wantPort:     "5432",
			wantUser:     "emergent",
			wantDB:       "emergent",
			wantSSL:      "disable",
			wantSource:   "DB_HOST/DB_PORT",
			wantExplicit: true,
			wantConflict: "POSTGRES_HOST=db",
		},
		{
			name: "DATABASE_URL loopback conflicts with POSTGRES_HOST elsewhere",
			env: map[string]string{
				"DATABASE_URL":  "postgres://u:p@localhost:5432/emergent",
				"POSTGRES_HOST": "db",
			},
			wantHost:     "localhost",
			wantPort:     "5432",
			wantUser:     "u",
			wantDB:       "emergent",
			wantSSL:      "disable",
			wantSource:   "DATABASE_URL",
			wantExplicit: true,
			wantConflict: "POSTGRES_HOST=db",
		},
		{
			name: "loopback POSTGRES_HOST is not a conflict",
			env: map[string]string{
				"POSTGRES_PASSWORD": "pw",
				"POSTGRES_HOST":     "localhost",
			},
			wantHost:     "localhost",
			wantPort:     "5432",
			wantUser:     "emergent",
			wantDB:       "emergent",
			wantSSL:      "disable",
			wantSource:   "POSTGRES_HOST/POSTGRES_PORT",
			wantExplicit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveTarget(fakeEnv(tt.env))
			if err != nil {
				t.Fatalf("resolveTarget() error = %v", err)
			}
			if got.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", got.Host, tt.wantHost)
			}
			if got.Port != tt.wantPort {
				t.Errorf("Port = %q, want %q", got.Port, tt.wantPort)
			}
			if got.User != tt.wantUser {
				t.Errorf("User = %q, want %q", got.User, tt.wantUser)
			}
			if got.Database != tt.wantDB {
				t.Errorf("Database = %q, want %q", got.Database, tt.wantDB)
			}
			if got.SSLMode != tt.wantSSL {
				t.Errorf("SSLMode = %q, want %q", got.SSLMode, tt.wantSSL)
			}
			if got.hostSource != tt.wantSource {
				t.Errorf("hostSource = %q, want %q", got.hostSource, tt.wantSource)
			}
			if got.explicitHost != tt.wantExplicit {
				t.Errorf("explicitHost = %v, want %v", got.explicitHost, tt.wantExplicit)
			}
			if got.conflictEnv != tt.wantConflict {
				t.Errorf("conflictEnv = %q, want %q", got.conflictEnv, tt.wantConflict)
			}
		})
	}
}

func TestResolveTargetErrors(t *testing.T) {
	t.Run("missing password without DATABASE_URL", func(t *testing.T) {
		if _, err := resolveTarget(fakeEnv(map[string]string{})); err == nil {
			t.Fatal("resolveTarget() error = nil, want error about missing password")
		} else if !strings.Contains(err.Error(), "POSTGRES_PASSWORD") {
			t.Errorf("error = %q, want it to mention POSTGRES_PASSWORD", err)
		}
	})

	t.Run("missing password still returns the resolved target", func(t *testing.T) {
		got, err := resolveTarget(fakeEnv(map[string]string{"DB_HOST": "db"}))
		if err == nil {
			t.Fatal("resolveTarget() error = nil, want missing-password error")
		}
		if got.Host != "db" || got.hostSource != "DB_HOST/DB_PORT" || !got.explicitHost {
			t.Errorf("target = %+v, want host/db host source resolved so the caller can log it and fail closed", got)
		}
	})

	t.Run("DATABASE_URL without userinfo does not panic", func(t *testing.T) {
		got, err := resolveTarget(fakeEnv(map[string]string{"DATABASE_URL": "postgres://host:5432/db"}))
		if err != nil {
			t.Fatalf("resolveTarget() error = %v", err)
		}
		if got.Host != "host" || got.User != "" {
			t.Errorf("target = %+v, want host=host user=\"\"", got)
		}
	})

	t.Run("unparseable DATABASE_URL", func(t *testing.T) {
		if _, err := resolveTarget(fakeEnv(map[string]string{"DATABASE_URL": "://not-a-url"})); err == nil {
			t.Fatal("resolveTarget() error = nil, want invalid DATABASE_URL error")
		}
	})

	t.Run("DATABASE_URL without host", func(t *testing.T) {
		if _, err := resolveTarget(fakeEnv(map[string]string{"DATABASE_URL": "postgres:///db"})); err == nil {
			t.Fatal("resolveTarget() error = nil, want missing host error")
		}
	})
}

func TestDecideTargetFailClosed(t *testing.T) {
	tests := []struct {
		name           string
		target         Target
		mutating       bool
		allowLocalhost bool
		wantRefuse     bool
		wantReasonSub  []string
		wantWarn       bool
	}{
		{
			name:     "read-only loopback default is allowed",
			target:   Target{Host: "localhost", Port: "5432"},
			mutating: false,
		},
		{
			name:     "non-loopback is always allowed",
			target:   Target{Host: "db", Port: "5432", explicitHost: true, hostSource: "POSTGRES_HOST/POSTGRES_PORT"},
			mutating: true,
		},
		{
			name:          "implicit localhost default is refused for mutating commands",
			target:        Target{Host: "localhost", Port: "5432", hostSource: "built-in default"},
			mutating:      true,
			wantRefuse:    true,
			wantReasonSub: []string{"no database host", "localhost:5432", "-allow-localhost"},
		},
		{
			name: "conflicting host env is refused even when localhost was explicit",
			target: Target{
				Host: "localhost", Port: "5432", explicitHost: true,
				hostSource: "DB_HOST/DB_PORT", conflictEnv: "POSTGRES_HOST=db",
			},
			mutating:      true,
			wantRefuse:    true,
			wantReasonSub: []string{"POSTGRES_HOST=db", "localhost:5432", "-allow-localhost"},
		},
		{
			name:          "127.0.0.1 default is refused",
			target:        Target{Host: "127.0.0.1", Port: "5432", hostSource: "built-in default"},
			mutating:      true,
			wantRefuse:    true,
			wantReasonSub: []string{"127.0.0.1:5432"},
		},
		{
			name:     "explicit loopback with no conflict is allowed with a warning",
			target:   Target{Host: "localhost", Port: "5432", explicitHost: true, hostSource: "POSTGRES_HOST/POSTGRES_PORT"},
			mutating: true,
			wantWarn: true,
		},
		{
			name:           "allow-localhost overrides the refusal",
			target:         Target{Host: "localhost", Port: "5432", hostSource: "built-in default"},
			mutating:       true,
			allowLocalhost: true,
			wantWarn:       true,
		},
		{
			name:           "allow-localhost overrides a conflict refusal",
			target:         Target{Host: "localhost", Port: "5432", hostSource: "built-in default", conflictEnv: "POSTGRES_HOST=db"},
			mutating:       true,
			allowLocalhost: true,
			wantWarn:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decideTarget(tt.target, tt.mutating, tt.allowLocalhost)
			if got.Refuse != tt.wantRefuse {
				t.Fatalf("Refuse = %v, want %v (reason=%q)", got.Refuse, tt.wantRefuse, got.Reason)
			}
			if (got.Warn != "") != tt.wantWarn {
				t.Errorf("Warn = %q, want present=%v", got.Warn, tt.wantWarn)
			}
			for _, sub := range tt.wantReasonSub {
				if !strings.Contains(got.Reason, sub) {
					t.Errorf("Reason = %q, want it to contain %q", got.Reason, sub)
				}
			}
		})
	}
}

func TestIsMutatingCommand(t *testing.T) {
	mutating := []string{"up", "up-to", "down", "mark-applied"}
	readOnly := []string{"status", "version", "create", "", "bogus"}
	for _, cmd := range mutating {
		if !isMutatingCommand(cmd) {
			t.Errorf("isMutatingCommand(%q) = false, want true", cmd)
		}
	}
	for _, cmd := range readOnly {
		if isMutatingCommand(cmd) {
			t.Errorf("isMutatingCommand(%q) = true, want false", cmd)
		}
	}
}

func TestLogLineNeverLeaksPassword(t *testing.T) {
	target, err := resolveTarget(fakeEnv(map[string]string{
		"POSTGRES_PASSWORD": "sup3r-s3cret",
		"POSTGRES_HOST":     "db.internal",
		"POSTGRES_PORT":     "6543",
		"POSTGRES_DB":       "memtest",
		"POSTGRES_USER":     "migrator",
	}))
	if err != nil {
		t.Fatalf("resolveTarget() error = %v", err)
	}

	line := target.LogLine()
	for _, want := range []string{"host=db.internal", "port=6543", "user=migrator", "database=memtest", "POSTGRES_HOST/POSTGRES_PORT"} {
		if !strings.Contains(line, want) {
			t.Errorf("LogLine() = %q, want it to contain %q", line, want)
		}
	}
	if strings.Contains(line, "sup3r-s3cret") {
		t.Errorf("LogLine() leaked the password: %q", line)
	}
	if strings.Contains(line, "password") {
		t.Errorf("LogLine() = %q, must not mention the password field", line)
	}
}

func TestDSN(t *testing.T) {
	target := Target{
		Host: "db", Port: "6543", User: "migrator",
		Database: "memtest", SSLMode: "require", Password: "pw",
	}
	want := "postgres://migrator:pw@db:6543/memtest?sslmode=require"
	if got := target.DSN(); got != want {
		t.Errorf("DSN() = %q, want %q", got, want)
	}
}

func TestDSNEncodesHostAndCredentials(t *testing.T) {
	tests := []struct {
		name   string
		target Target
		want   string
	}{
		{
			name:   "ipv6 loopback host is bracketed",
			target: Target{Host: "::1", Port: "5432", User: "emergent", Database: "emergent", SSLMode: "disable", Password: "pw"},
			want:   "postgres://emergent:pw@[::1]:5432/emergent?sslmode=disable",
		},
		{
			name:   "reserved characters in credentials are escaped",
			target: Target{Host: "db", Port: "5432", User: "user name", Database: "emergent", SSLMode: "disable", Password: "p@ss:/?#"},
			want:   "postgres://user%20name:p%40ss%3A%2F%3F%23@db:5432/emergent?sslmode=disable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.target.DSN(); got != tt.want {
				t.Errorf("DSN() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTargetIsLoopback(t *testing.T) {
	loopback := []string{"localhost", "LOCALHOST", "127.0.0.1", "::1", "[::1]", "0.0.0.0", " localhost "}
	remote := []string{"db", "db.internal", "10.0.0.5", "postgres", "host.docker.internal"}
	for _, host := range loopback {
		if !(Target{Host: host}).IsLoopback() {
			t.Errorf("IsLoopback(%q) = false, want true", host)
		}
	}
	for _, host := range remote {
		if (Target{Host: host}).IsLoopback() {
			t.Errorf("IsLoopback(%q) = true, want false", host)
		}
	}
}
