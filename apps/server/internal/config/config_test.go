package config

import (
	"io"
	"log/slog"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDatabaseConfig_DSN(t *testing.T) {
	tests := []struct {
		name     string
		config   DatabaseConfig
		expected string
	}{
		{
			name: "basic config",
			config: DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "user",
				Password: "pass",
				Database: "testdb",
				SSLMode:  "disable",
			},
			expected: "postgres://user:pass@localhost:5432/testdb?sslmode=disable",
		},
		{
			name: "production config",
			config: DatabaseConfig{
				Host:     "db.example.com",
				Port:     5433,
				User:     "admin",
				Password: "secretpass",
				Database: "production",
				SSLMode:  "require",
			},
			expected: "postgres://admin:secretpass@db.example.com:5433/production?sslmode=require",
		},
		{
			name: "empty password",
			config: DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "user",
				Password: "",
				Database: "testdb",
				SSLMode:  "disable",
			},
			expected: "postgres://user:@localhost:5432/testdb?sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.DSN()
			if got != tt.expected {
				t.Errorf("DSN() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestDatabaseConfig_DSNRoundTripsEscapedCredentials pins that a password (or
// user) containing URL-reserved characters survives DSN() → url.Parse() intact:
// the characters must be escaped when building the DSN and decoded on parse,
// rather than truncating the DSN at the first '@', ':' or '/'.
func TestDatabaseConfig_DSNRoundTripsEscapedCredentials(t *testing.T) {
	cfg := DatabaseConfig{
		Host:     "127.0.0.1",
		Port:     54329,
		User:     "test user",
		Password: "p@ss:w/rd%40",
		Database: "testdb",
		SSLMode:  "disable",
	}

	u, err := url.Parse(cfg.DSN())
	if err != nil {
		t.Fatalf("parse DSN %q: %v", cfg.DSN(), err)
	}
	if got := u.User.Username(); got != cfg.User {
		t.Errorf("username = %q, want %q", got, cfg.User)
	}
	pw, _ := u.User.Password()
	if pw != cfg.Password {
		t.Errorf("password = %q, want %q", pw, cfg.Password)
	}
	if u.Hostname() != cfg.Host || u.Port() != "54329" {
		t.Errorf("host:port = %q:%q, want %q:54329", u.Hostname(), u.Port(), cfg.Host)
	}
	if u.Path != "/testdb" {
		t.Errorf("path = %q, want /testdb", u.Path)
	}
}

// TestDatabaseConfig_DSNApplicationName pins how application_name is rendered
// in the DSN: included when set, escaped when it contains reserved characters,
// and omitted when empty.
func TestDatabaseConfig_DSNApplicationName(t *testing.T) {
	tests := []struct {
		name     string
		config   DatabaseConfig
		wantSub  string
		wantSkip bool // assert application_name is absent
	}{
		{
			name: "application name included",
			config: DatabaseConfig{
				Host: "localhost", Port: 5432, User: "u", Password: "p",
				Database: "db", SSLMode: "disable", ApplicationName: "memory-server",
			},
			wantSub: "application_name=memory-server",
		},
		{
			name: "reserved characters escaped",
			config: DatabaseConfig{
				Host: "localhost", Port: 5432, User: "u", Password: "p",
				Database: "db", SSLMode: "disable", ApplicationName: "my app/name?x",
			},
			wantSub: "application_name=my+app%2Fname%3Fx",
		},
		{
			name: "omitted when empty",
			config: DatabaseConfig{
				Host: "localhost", Port: 5432, User: "u", Password: "p",
				Database: "db", SSLMode: "disable",
			},
			wantSub:  "?sslmode=disable",
			wantSkip: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.DSN()
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("DSN() = %q, want it to contain %q", got, tt.wantSub)
			}
			if tt.wantSkip && strings.Contains(got, "application_name") {
				t.Errorf("DSN() = %q, must not contain application_name", got)
			}
		})
	}
}

// TestDatabaseConfig_ApplicationNameEnv pins the DB_APPLICATION_NAME override
// and the memory-server default via NewConfig.
func TestDatabaseConfig_ApplicationNameEnv(t *testing.T) {
	t.Setenv("DB_APPLICATION_NAME", "my-custom-app")
	cfg, err := NewConfig(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if cfg.Database.ApplicationName != "my-custom-app" {
		t.Errorf("ApplicationName = %q, want %q", cfg.Database.ApplicationName, "my-custom-app")
	}
	if !strings.Contains(cfg.Database.DSN(), "application_name=my-custom-app") {
		t.Errorf("DSN() = %q, want it to contain application_name=my-custom-app", cfg.Database.DSN())
	}
}

func TestEmbeddingsConfig_IsEnabled(t *testing.T) {
	tests := []struct {
		name   string
		config EmbeddingsConfig
		want   bool
	}{
		{
			name: "enabled with Vertex AI",
			config: EmbeddingsConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "us-central1",
			},
			want: true,
		},
		{
			name: "enabled with Google API Key",
			config: EmbeddingsConfig{
				GoogleAPIKey: "test-api-key",
			},
			want: true,
		},
		{
			name: "disabled when network disabled",
			config: EmbeddingsConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "us-central1",
				NetworkDisabled:  true,
			},
			want: false,
		},
		{
			name: "disabled with missing project ID",
			config: EmbeddingsConfig{
				GCPProjectID:     "",
				VertexAILocation: "us-central1",
			},
			want: false,
		},
		{
			name: "disabled with missing location",
			config: EmbeddingsConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "",
			},
			want: false,
		},
		{
			name:   "disabled with empty config",
			config: EmbeddingsConfig{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.IsEnabled()
			if got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEmbeddingsConfig_UseVertexAI(t *testing.T) {
	tests := []struct {
		name   string
		config EmbeddingsConfig
		want   bool
	}{
		{
			name: "true with both project and location",
			config: EmbeddingsConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "us-central1",
			},
			want: true,
		},
		{
			name: "false without project ID",
			config: EmbeddingsConfig{
				GCPProjectID:     "",
				VertexAILocation: "us-central1",
			},
			want: false,
		},
		{
			name: "false without location",
			config: EmbeddingsConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "",
			},
			want: false,
		},
		{
			name:   "false with empty config",
			config: EmbeddingsConfig{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.UseVertexAI()
			if got != tt.want {
				t.Errorf("UseVertexAI() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLLMConfig_IsEnabled(t *testing.T) {
	tests := []struct {
		name   string
		config LLMConfig
		want   bool
	}{
		{
			name: "enabled with both project and location",
			config: LLMConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "us-central1",
			},
			want: true,
		},
		{
			name: "disabled when network disabled",
			config: LLMConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "us-central1",
				NetworkDisabled:  true,
			},
			want: false,
		},
		{
			name: "disabled without project ID",
			config: LLMConfig{
				GCPProjectID:     "",
				VertexAILocation: "us-central1",
			},
			want: false,
		},
		{
			name: "disabled without location",
			config: LLMConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "",
			},
			want: false,
		},
		{
			name:   "disabled with empty config",
			config: LLMConfig{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.IsEnabled()
			if got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEmailConfig_IsConfigured(t *testing.T) {
	tests := []struct {
		name   string
		config EmailConfig
		want   bool
	}{
		{
			name: "configured with domain and API key",
			config: EmailConfig{
				MailgunDomain: "mg.example.com",
				MailgunAPIKey: "key-12345",
			},
			want: true,
		},
		{
			name: "not configured without domain",
			config: EmailConfig{
				MailgunDomain: "",
				MailgunAPIKey: "key-12345",
			},
			want: false,
		},
		{
			name: "not configured without API key",
			config: EmailConfig{
				MailgunDomain: "mg.example.com",
				MailgunAPIKey: "",
			},
			want: false,
		},
		{
			name:   "not configured with empty config",
			config: EmailConfig{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.IsConfigured()
			if got != tt.want {
				t.Errorf("IsConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestZitadelConfig_GetIssuer(t *testing.T) {
	tests := []struct {
		name   string
		config ZitadelConfig
		want   string
	}{
		{
			name: "uses explicit issuer",
			config: ZitadelConfig{
				Domain: "zitadel.example.com",
				Issuer: "https://custom-issuer.example.com",
			},
			want: "https://custom-issuer.example.com",
		},
		{
			name: "defaults to https domain",
			config: ZitadelConfig{
				Domain: "zitadel.example.com",
			},
			want: "https://zitadel.example.com",
		},
		{
			name: "uses http when insecure",
			config: ZitadelConfig{
				Domain:   "localhost:8080",
				Insecure: true,
			},
			want: "http://localhost:8080",
		},
		{
			name: "explicit issuer takes precedence over insecure",
			config: ZitadelConfig{
				Domain:   "localhost:8080",
				Issuer:   "https://explicit-issuer.com",
				Insecure: true,
			},
			want: "https://explicit-issuer.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetIssuer()
			if got != tt.want {
				t.Errorf("GetIssuer() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestXbergConfig_Timeout(t *testing.T) {
	tests := []struct {
		name      string
		timeoutMs int
		want      time.Duration
	}{
		{"default 300s", 300000, 300 * time.Second},
		{"10 seconds", 10000, 10 * time.Second},
		{"1 second", 1000, time.Second},
		{"zero", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := XbergConfig{TimeoutMs: tt.timeoutMs}
			got := cfg.Timeout()
			if got != tt.want {
				t.Errorf("Timeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestXbergConfig_WorkerInterval(t *testing.T) {
	tests := []struct {
		name             string
		workerIntervalMs int
		want             time.Duration
	}{
		{"5 seconds", 5000, 5 * time.Second},
		{"10 seconds", 10000, 10 * time.Second},
		{"1 second", 1000, time.Second},
		{"zero", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := XbergConfig{WorkerIntervalMs: tt.workerIntervalMs}
			got := cfg.WorkerInterval()
			if got != tt.want {
				t.Errorf("WorkerInterval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStorageConfig_IsConfigured(t *testing.T) {
	tests := []struct {
		name   string
		config StorageConfig
		want   bool
	}{
		{
			name: "fully configured",
			config: StorageConfig{
				Endpoint:        "localhost:9000",
				AccessKeyID:     "minioadmin",
				SecretAccessKey: "minioadmin",
			},
			want: true,
		},
		{
			name: "missing endpoint",
			config: StorageConfig{
				Endpoint:        "",
				AccessKeyID:     "minioadmin",
				SecretAccessKey: "minioadmin",
			},
			want: false,
		},
		{
			name: "missing access key",
			config: StorageConfig{
				Endpoint:        "localhost:9000",
				AccessKeyID:     "",
				SecretAccessKey: "minioadmin",
			},
			want: false,
		},
		{
			name: "missing secret key",
			config: StorageConfig{
				Endpoint:        "localhost:9000",
				AccessKeyID:     "minioadmin",
				SecretAccessKey: "",
			},
			want: false,
		},
		{
			name:   "empty config",
			config: StorageConfig{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.IsConfigured()
			if got != tt.want {
				t.Errorf("IsConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMCPRegistryConfig_BaseURL(t *testing.T) {
	t.Setenv("MCP_REGISTRY_BASE_URL", "")
	cfg, err := NewConfig(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if cfg.MCPRegistry.BaseURL != "" {
		t.Errorf("BaseURL = %q, want empty (default)", cfg.MCPRegistry.BaseURL)
	}

	t.Setenv("MCP_REGISTRY_BASE_URL", "http://stub:8080")
	cfg, err = NewConfig(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if cfg.MCPRegistry.BaseURL != "http://stub:8080" {
		t.Errorf("BaseURL = %q, want %q", cfg.MCPRegistry.BaseURL, "http://stub:8080")
	}
}

func TestZitadelOIDCScopeConfig(t *testing.T) {
	clearScopePolicyEnv(t)
	t.Setenv("MEMORY_OIDC_DEFAULT_SCOPES", "data:read,search")

	cfg, err := NewConfig(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}

	if len(cfg.Zitadel.OIDCDefaultScopes) != 2 ||
		cfg.Zitadel.OIDCDefaultScopes[0] != "data:read" ||
		cfg.Zitadel.OIDCDefaultScopes[1] != "search" {
		t.Fatalf("OIDCDefaultScopes = %v, want [data:read search]", cfg.Zitadel.OIDCDefaultScopes)
	}
}

// TestShutdownAndPoolDefaults pins the hardened defaults from issue #1481 by
// reading the struct tags directly, so ambient CI environment variables cannot
// mask a regression in the declared defaults.
func TestShutdownAndPoolDefaults(t *testing.T) {
	envDefault := func(t *testing.T, typ reflect.Type, field string) string {
		t.Helper()
		f, ok := typ.FieldByName(field)
		if !ok {
			t.Fatalf("field %s not found on %s", field, typ)
		}
		return f.Tag.Get("envDefault")
	}

	tests := []struct {
		name  string
		typ   reflect.Type
		field string
		want  string
	}{
		{"shutdown timeout", reflect.TypeOf(Config{}), "ShutdownTimeout", "30s"},
		{"db max open conns", reflect.TypeOf(DatabaseConfig{}), "MaxOpenConns", "100"},
		{"db max idle conns", reflect.TypeOf(DatabaseConfig{}), "MaxIdleConns", "25"},
		{"db application name", reflect.TypeOf(DatabaseConfig{}), "ApplicationName", "memory-server"},
		{"db ping max attempts", reflect.TypeOf(DatabaseConfig{}), "PingMaxAttempts", "10"},
		{"db ping initial backoff", reflect.TypeOf(DatabaseConfig{}), "PingInitialBackoff", "250ms"},
		{"db ping max backoff", reflect.TypeOf(DatabaseConfig{}), "PingMaxBackoff", "5s"},
		{"db bulk statement timeout", reflect.TypeOf(DatabaseConfig{}), "BulkStatementTimeout", "10m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := envDefault(t, tt.typ, tt.field); got != tt.want {
				t.Fatalf("%s envDefault = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

// TestSourceOAuthConfigGoogleProvidersShareDeploymentApp pins the provider map
// that backs "Gmail reuses the deployment Google app": both Google-backed
// providers resolve the one deployment client and the one env-var hint, and a
// half-configured app yields neither (a client id without its secret cannot
// authorize).
func TestSourceOAuthConfigGoogleProvidersShareDeploymentApp(t *testing.T) {
	cfg := SourceOAuthConfig{GoogleClientID: "cid", GoogleClientSecret: "cs"}
	byProvider := cfg.OAuthClientsByProvider()
	for _, providerType := range []string{"google_drive", "gmail"} {
		got, ok := byProvider[providerType]
		if !ok {
			t.Fatalf("OAuthClientsByProvider() has no %q entry", providerType)
		}
		if got.ClientID != "cid" || got.ClientSecret != "cs" {
			t.Errorf("%s client = %+v, want the deployment Google app", providerType, got)
		}
		if hint := cfg.DeploymentEnvVarHint(providerType); hint != "SOURCES_OAUTH_GOOGLE_CLIENT_ID/SECRET" {
			t.Errorf("DeploymentEnvVarHint(%q) = %q", providerType, hint)
		}
	}
	if len(byProvider) != 2 {
		t.Errorf("providers = %v, want exactly the two Google providers", byProvider)
	}

	partial := SourceOAuthConfig{GoogleClientID: "cid"}
	if got := partial.OAuthClientsByProvider(); len(got) != 0 {
		t.Errorf("OAuthClientsByProvider() = %v for a client id without a secret, want empty", got)
	}
	if hint := partial.DeploymentEnvVarHint("github"); hint != "" {
		t.Errorf("DeploymentEnvVarHint(github) = %q, want empty", hint)
	}
}
