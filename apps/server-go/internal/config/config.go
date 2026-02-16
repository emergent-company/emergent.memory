package config

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/caarlos0/env/v11"
	"go.uber.org/fx"
)

var Module = fx.Module("config",
	fx.Provide(NewConfig),
)

// Config holds all application configuration
type Config struct {
	// Server settings
	ServerPort    int    `env:"SERVER_PORT" envDefault:"3002"`
	ServerAddress string `env:"SERVER_ADDRESS" envDefault:"0.0.0.0"`
	Environment   string `env:"ENVIRONMENT" envDefault:"local"`
	Debug         bool   `env:"DEBUG" envDefault:"false"`
	LogLevel      string `env:"LOG_LEVEL" envDefault:"info"`

	// Database settings (matches NestJS POSTGRES_* vars)
	Database DatabaseConfig

	// Zitadel authentication
	Zitadel ZitadelConfig

	// Embeddings configuration
	Embeddings EmbeddingsConfig

	// LLM configuration (for chat completions)
	LLM LLMConfig

	// Email configuration
	Email EmailConfig

	// Kreuzberg document parsing configuration
	Kreuzberg KreuzbergConfig

	// Storage configuration
	Storage StorageConfig

	// Standalone mode configuration (minimal deployment)
	Standalone StandaloneConfig

	// Server timeouts
	ReadTimeout     time.Duration `env:"SERVER_READ_TIMEOUT" envDefault:"5s"`
	WriteTimeout    time.Duration `env:"SERVER_WRITE_TIMEOUT" envDefault:"28800s"` // 8 hours for SSE
	IdleTimeout     time.Duration `env:"SERVER_IDLE_TIMEOUT" envDefault:"28800s"`  // 8 hours for SSE
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
}

// DatabaseConfig holds PostgreSQL connection settings
type DatabaseConfig struct {
	Host         string        `env:"POSTGRES_HOST" envDefault:"localhost"`
	Port         int           `env:"POSTGRES_PORT" envDefault:"5432"`
	User         string        `env:"POSTGRES_USER" envDefault:"emergent"`
	Password     string        `env:"POSTGRES_PASSWORD" envDefault:""`
	Database     string        `env:"POSTGRES_DB" envDefault:"emergent"`
	SSLMode      string        `env:"POSTGRES_SSL_MODE" envDefault:"disable"`
	MaxOpenConns int           `env:"DB_MAX_OPEN_CONNS" envDefault:"25"`
	MaxIdleConns int           `env:"DB_MAX_IDLE_CONNS" envDefault:"5"`
	MaxIdleTime  time.Duration `env:"DB_MAX_IDLE_TIME" envDefault:"5m"`
	QueryDebug   bool          `env:"DB_QUERY_DEBUG" envDefault:"false"`
}

// DSN returns the PostgreSQL connection string
func (d *DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		d.User, d.Password, d.Host, d.Port, d.Database, d.SSLMode,
	)
}

// ZitadelConfig holds Zitadel/OIDC authentication settings
type ZitadelConfig struct {
	// Domain for Zitadel instance (e.g., "zitadel.dev.emergent-company.ai")
	Domain string `env:"ZITADEL_DOMAIN" envDefault:"localhost:8080"`

	// Issuer URL for OIDC (defaults to https://{Domain} if not set)
	Issuer string `env:"ZITADEL_ISSUER"`

	// Service account JWT key for introspection (JSON key file content)
	ClientJWT string `env:"ZITADEL_CLIENT_JWT"`

	// Path to JWT key file (alternative to ZITADEL_CLIENT_JWT)
	ClientJWTPath string `env:"ZITADEL_CLIENT_JWT_PATH"`

	// API JWT for management API calls (JSON key file content)
	APIJWT string `env:"ZITADEL_API_JWT"`

	// Path to API JWT key file (alternative to ZITADEL_API_JWT)
	APIJWTPath string `env:"ZITADEL_API_JWT_PATH"`

	// Organization ID for role checks
	MainOrgID string `env:"ZITADEL_MAIN_ORG_ID"`

	// Project ID for scopes
	ProjectID string `env:"ZITADEL_PROJECT_ID"`

	// Organization ID (alias for compatibility)
	OrgID string `env:"ZITADEL_ORG_ID"`

	// Disable token introspection (for testing)
	DisableIntrospection bool `env:"DISABLE_ZITADEL_INTROSPECTION" envDefault:"false"`

	// Introspection cache TTL
	IntrospectCacheTTL time.Duration `env:"ZITADEL_INTROSPECT_CACHE_TTL" envDefault:"5m"`

	// Debug token for development (bypasses auth)
	DebugToken string `env:"ZITADEL_DEBUG_TOKEN"`

	// Insecure mode (HTTP instead of HTTPS)
	Insecure bool `env:"ZITADEL_INSECURE" envDefault:"false"`
}

// EmbeddingsConfig holds embedding service configuration
type EmbeddingsConfig struct {
	// Provider: "vertex" (production) or "genai" (development)
	Provider string `env:"EMBEDDING_PROVIDER" envDefault:""`

	// GCP Project ID for Vertex AI
	GCPProjectID string `env:"GCP_PROJECT_ID" envDefault:""`

	// Vertex AI location (e.g., "us-central1")
	VertexAILocation string `env:"VERTEX_AI_LOCATION" envDefault:"us-central1"`

	// Embedding model name
	Model string `env:"EMBEDDING_MODEL" envDefault:"text-embedding-004"`

	// Embedding dimension (768 for text-embedding-004)
	Dimension int `env:"EMBEDDING_DIMENSION" envDefault:"768"`

	// Google API Key for Generative AI (development)
	GoogleAPIKey string `env:"GOOGLE_API_KEY" envDefault:""`

	// Disable embeddings network calls (for testing)
	NetworkDisabled bool `env:"EMBEDDINGS_NETWORK_DISABLED" envDefault:"false"`
}

// IsEnabled returns true if embeddings are configured
func (e *EmbeddingsConfig) IsEnabled() bool {
	if e.NetworkDisabled {
		return false
	}
	// Enabled if Vertex AI is configured OR Google API Key is set
	return (e.GCPProjectID != "" && e.VertexAILocation != "") || e.GoogleAPIKey != ""
}

// UseVertexAI returns true if Vertex AI should be used
func (e *EmbeddingsConfig) UseVertexAI() bool {
	return e.GCPProjectID != "" && e.VertexAILocation != ""
}

// LLMConfig holds LLM (chat completion) configuration
type LLMConfig struct {
	// GCP Project ID for Vertex AI (reuses EMBEDDING_PROVIDER's GCP_PROJECT_ID)
	GCPProjectID string `env:"GCP_PROJECT_ID" envDefault:""`

	// Vertex AI location ("global" required for Gemini 3 models)
	VertexAILocation string `env:"VERTEX_AI_LOCATION" envDefault:"global"`

	// Chat model name
	Model string `env:"VERTEX_AI_MODEL" envDefault:"gemini-3-flash-preview"`

	// Max output tokens for chat completions (65536 for Gemini 3 thinking models)
	MaxOutputTokens int `env:"LLM_MAX_OUTPUT_TOKENS" envDefault:"65536"`

	// Temperature for chat completions (0.0-1.0)
	Temperature float64 `env:"LLM_TEMPERATURE" envDefault:"0"`

	// Request timeout
	Timeout time.Duration `env:"LLM_TIMEOUT" envDefault:"120s"`

	// Google API Key for Google AI (standalone/development fallback)
	GoogleAPIKey string `env:"GOOGLE_API_KEY" envDefault:""`

	// Disable LLM network calls (for testing)
	NetworkDisabled bool `env:"LLM_NETWORK_DISABLED" envDefault:"false"`
}

// IsEnabled returns true if LLM is configured
func (l *LLMConfig) IsEnabled() bool {
	if l.NetworkDisabled {
		return false
	}
	return l.UseVertexAI() || l.GoogleAPIKey != ""
}

// UseVertexAI returns true if Vertex AI should be used (GCP credentials available)
func (l *LLMConfig) UseVertexAI() bool {
	return l.GCPProjectID != "" && l.VertexAILocation != ""
}

// EmailConfig holds email service configuration
type EmailConfig struct {
	// Enabled determines if email sending is enabled
	Enabled bool `env:"EMAIL_ENABLED" envDefault:"false"`
	// MailgunDomain is the Mailgun domain
	MailgunDomain string `env:"MAILGUN_DOMAIN" envDefault:""`
	// MailgunAPIKey is the Mailgun API key
	MailgunAPIKey string `env:"MAILGUN_API_KEY" envDefault:""`
	// FromEmail is the default from email address
	FromEmail string `env:"EMAIL_FROM_ADDRESS" envDefault:"noreply@example.com"`
	// FromName is the default from name
	FromName string `env:"EMAIL_FROM_NAME" envDefault:"Emergent"`
	// MaxRetries is the maximum number of retry attempts (default: 3)
	MaxRetries int `env:"EMAIL_MAX_RETRIES" envDefault:"3"`
	// RetryDelaySec is the base delay in seconds for retries (default: 60)
	RetryDelaySec int `env:"EMAIL_RETRY_DELAY_SEC" envDefault:"60"`
	// WorkerIntervalMs is the polling interval in milliseconds (default: 5000)
	WorkerIntervalMs int `env:"EMAIL_WORKER_INTERVAL_MS" envDefault:"5000"`
	// WorkerBatchSize is the number of jobs to process per poll (default: 10)
	WorkerBatchSize int `env:"EMAIL_WORKER_BATCH_SIZE" envDefault:"10"`
}

// IsConfigured returns true if Mailgun is configured
func (e *EmailConfig) IsConfigured() bool {
	return e.MailgunDomain != "" && e.MailgunAPIKey != ""
}

// KreuzbergConfig holds Kreuzberg document parsing service configuration
type KreuzbergConfig struct {
	// Enabled determines if Kreuzberg parsing is enabled
	Enabled bool `env:"KREUZBERG_ENABLED" envDefault:"true"`
	// ServiceURL is the Kreuzberg service URL
	ServiceURL string `env:"KREUZBERG_SERVICE_URL" envDefault:"http://localhost:8000"`
	// Timeout is the request timeout in milliseconds (default: 300000 = 5 minutes)
	TimeoutMs int `env:"KREUZBERG_SERVICE_TIMEOUT" envDefault:"300000"`
	// MaxFileSizeMB is the maximum file size for document parsing
	MaxFileSizeMB int `env:"KREUZBERG_MAX_FILE_SIZE_MB" envDefault:"100"`
	// WorkerIntervalMs is the polling interval in milliseconds (default: 5000)
	WorkerIntervalMs int `env:"DOCUMENT_PARSING_WORKER_INTERVAL_MS" envDefault:"5000"`
	// WorkerBatchSize is the number of jobs to process per poll (default: 5)
	WorkerBatchSize int `env:"DOCUMENT_PARSING_WORKER_BATCH_SIZE" envDefault:"5"`
}

// Timeout returns the request timeout as a Duration
func (k *KreuzbergConfig) Timeout() time.Duration {
	return time.Duration(k.TimeoutMs) * time.Millisecond
}

// WorkerInterval returns the worker interval as a Duration
func (k *KreuzbergConfig) WorkerInterval() time.Duration {
	return time.Duration(k.WorkerIntervalMs) * time.Millisecond
}

// StorageConfig holds storage (MinIO/S3) configuration
type StorageConfig struct {
	// Endpoint is the MinIO/S3 endpoint URL
	Endpoint string `env:"MINIO_ENDPOINT" envDefault:"localhost:9000"`
	// AccessKeyID is the access key ID
	AccessKeyID string `env:"MINIO_ACCESS_KEY" envDefault:""`
	// SecretAccessKey is the secret access key
	SecretAccessKey string `env:"MINIO_SECRET_KEY" envDefault:""`
	// Bucket is the bucket name
	Bucket string `env:"MINIO_BUCKET" envDefault:"emergent"`
	// UseSSL determines if SSL should be used
	UseSSL bool `env:"MINIO_USE_SSL" envDefault:"false"`
	// Region is the bucket region (for S3 compatibility)
	Region string `env:"MINIO_REGION" envDefault:"us-east-1"`
}

// IsConfigured returns true if storage is configured
func (s *StorageConfig) IsConfigured() bool {
	return s.Endpoint != "" && s.AccessKeyID != "" && s.SecretAccessKey != ""
}

// StandaloneConfig holds configuration for standalone minimal deployment mode
type StandaloneConfig struct {
	// Enabled determines if standalone mode is active
	Enabled bool `env:"STANDALONE_MODE" envDefault:"false"`

	// APIKey is the static API key for authentication (via X-API-Key header)
	APIKey string `env:"STANDALONE_API_KEY" envDefault:""`

	// UserEmail is the email for the default user to create
	UserEmail string `env:"STANDALONE_USER_EMAIL" envDefault:"admin@localhost"`

	// OrgName is the default organization name
	OrgName string `env:"STANDALONE_ORG_NAME" envDefault:"Default Organization"`

	// ProjectName is the default project name
	ProjectName string `env:"STANDALONE_PROJECT_NAME" envDefault:"Default Project"`
}

// IsEnabled returns true if standalone mode is active
func (s *StandaloneConfig) IsEnabled() bool {
	return s.Enabled
}

// IsConfigured returns true if standalone mode is properly configured
func (s *StandaloneConfig) IsConfigured() bool {
	return s.Enabled && s.APIKey != ""
}

// GetIssuer returns the issuer URL, defaulting to https://{Domain}
func (z *ZitadelConfig) GetIssuer() string {
	if z.Issuer != "" {
		return z.Issuer
	}
	if z.Insecure {
		return fmt.Sprintf("http://%s", z.Domain)
	}
	return fmt.Sprintf("https://%s", z.Domain)
}

// NewConfig loads configuration from environment variables
func NewConfig(log *slog.Logger) (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	log.Info("configuration loaded",
		slog.String("environment", cfg.Environment),
		slog.Int("port", cfg.ServerPort),
		slog.String("db_host", cfg.Database.Host),
		slog.String("zitadel_domain", cfg.Zitadel.Domain),
	)

	return cfg, nil
}
