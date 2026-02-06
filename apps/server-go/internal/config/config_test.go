package config

import (
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

func TestKreuzbergConfig_Timeout(t *testing.T) {
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
			cfg := KreuzbergConfig{TimeoutMs: tt.timeoutMs}
			got := cfg.Timeout()
			if got != tt.want {
				t.Errorf("Timeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKreuzbergConfig_WorkerInterval(t *testing.T) {
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
			cfg := KreuzbergConfig{WorkerIntervalMs: tt.workerIntervalMs}
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
