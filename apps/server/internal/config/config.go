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

	// Database settings
	Database DatabaseConfig

	// Zitadel authentication
	Zitadel ZitadelConfig

	// Embeddings configuration
	Embeddings EmbeddingsConfig

	// LLM configuration (for chat completions)
	LLM LLMConfig

	// LLMProvider holds multi-tenant LLM provider credential management configuration
	LLMProvider LLMProviderConfig

	// Email configuration
	Email EmailConfig

	// Kreuzberg document parsing configuration
	Kreuzberg KreuzbergConfig

	// Whisper audio transcription configuration
	Whisper WhisperConfig

	// Storage configuration
	Storage StorageConfig

	// Standalone mode configuration (minimal deployment)
	Standalone StandaloneConfig

	// Agent sandbox configuration
	Sandbox SandboxConfig

	// Agent worker pool configuration
	AgentWorkerPoolSize     int           `env:"AGENT_WORKER_POOL_SIZE" envDefault:"5"`
	AgentWorkerPollInterval time.Duration `env:"AGENT_WORKER_POLL_INTERVAL" envDefault:"5s"`

	// Agent safeguards configuration
	AgentSafeguards AgentSafeguardsConfig

	// Brave Search API configuration
	BraveSearch BraveSearchConfig

	// OpenTelemetry tracing configuration
	Otel OtelConfig

	// Graph knowledge base configuration
	Graph GraphConfig

	// AskV2 enables the code-generation variant of the CLI assistant agent.
	// When true, POST /api/ask and /api/projects/:id/ask use EnsureCliAssistantAgentV2
	// which generates Python SDK scripts instead of calling 57 individual MCP tools.
	AskV2 bool `env:"MEMORY_ASK_V2" envDefault:"false"`

	// Server timeouts
	ReadTimeout     time.Duration `env:"SERVER_READ_TIMEOUT" envDefault:"3600s"`   // 1 hour for large file uploads
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
	Model string `env:"EMBEDDING_MODEL" envDefault:"gemini-embedding-2-preview"`

	// Embedding dimension (768 for gemini-embedding-2-preview with MRL)
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
	FromName string `env:"EMAIL_FROM_NAME" envDefault:"Memory"`
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

// WhisperConfig holds Whisper audio transcription service configuration
type WhisperConfig struct {
	// Enabled determines if Whisper transcription is enabled
	Enabled bool `env:"WHISPER_ENABLED" envDefault:"false"`
	// ServiceURL is the Whisper service URL
	ServiceURL string `env:"WHISPER_SERVICE_URL" envDefault:"http://localhost:9000"`
	// TimeoutMs is the request timeout in milliseconds (default: 600000 = 10 minutes)
	TimeoutMs int `env:"WHISPER_SERVICE_TIMEOUT" envDefault:"600000"`
	// Model is the Whisper model to use (e.g., "base", "medium", "large-v3")
	Model string `env:"WHISPER_MODEL" envDefault:"base"`
	// Language is the language hint for transcription (empty = auto-detect)
	Language string `env:"WHISPER_LANGUAGE" envDefault:""`
	// MaxFileSizeMB is the maximum audio file size for transcription
	MaxFileSizeMB int `env:"WHISPER_MAX_FILE_SIZE_MB" envDefault:"500"`
	// LargeFileThresholdMB - files above this are treated as "large" (default: 50)
	LargeFileThresholdMB int `env:"WHISPER_LARGE_FILE_THRESHOLD_MB" envDefault:"50"`
	// AudioBytesPerSecond - assumed bitrate for timeout estimation: 128kbps = 16000 B/s (default: 16000)
	AudioBytesPerSecond int `env:"WHISPER_AUDIO_BYTES_PER_SECOND" envDefault:"16000"`
	// TimeoutSafetyFactor - multiplier over estimated duration (default: 2.0)
	TimeoutSafetyFactor float64 `env:"WHISPER_TIMEOUT_SAFETY_FACTOR" envDefault:"2.0"`
}

// Timeout returns the request timeout as a Duration
func (w *WhisperConfig) Timeout() time.Duration {
	return time.Duration(w.TimeoutMs) * time.Millisecond
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

// SandboxConfig holds configuration for agent sandboxes
type SandboxConfig struct {
	// Enabled determines if agent sandbox endpoints and cleanup are active
	Enabled bool `env:"ENABLE_AGENT_SANDBOXES" envDefault:"true"`
	// MaxConcurrent is the maximum number of concurrent active workspaces
	MaxConcurrent int `env:"WORKSPACE_MAX_CONCURRENT" envDefault:"10"`
	// DefaultTTLDays is the default TTL for ephemeral workspaces in days
	DefaultTTLDays int `env:"WORKSPACE_DEFAULT_TTL_DAYS" envDefault:"30"`
	// CleanupIntervalMin is the cleanup job interval in minutes
	CleanupIntervalMin int `env:"WORKSPACE_CLEANUP_INTERVAL_MIN" envDefault:"60"`
	// AlertThresholdPct is the usage threshold for resource alerts (0-100)
	AlertThresholdPct int `env:"WORKSPACE_ALERT_THRESHOLD_PCT" envDefault:"80"`
	// WarmPoolSize is the number of pre-booted containers to keep ready (0 = disabled, default: 2)
	WarmPoolSize int `env:"WORKSPACE_WARM_POOL_SIZE" envDefault:"2"`
	// WarmPoolTargetImage is the Docker image to pre-boot warm containers with.
	// Should match the base_image your primary agent uses so warm containers are
	// immediately compatible. Empty means use the provider's default image.
	WarmPoolTargetImage string `env:"WORKSPACE_WARM_POOL_TARGET_IMAGE" envDefault:""`
	// WarmPoolExtraImages is a comma-separated list of additional Docker images to
	// pre-boot (one warm container each). Useful for secondary runtimes such as
	// the Go SDK image. Example: "emergent-memory-go-sdk:latest"
	WarmPoolExtraImages string `env:"WORKSPACE_WARM_POOL_EXTRA_IMAGES" envDefault:""`
	// DefaultProvider is the default sandbox provider (gvisor, firecracker, e2b)
	DefaultProvider string `env:"WORKSPACE_DEFAULT_PROVIDER" envDefault:"gvisor"`
	// DefaultCPU is the default CPU limit for workspaces (e.g. "2")
	DefaultCPU string `env:"WORKSPACE_DEFAULT_CPU" envDefault:"2"`
	// DefaultMemory is the default memory limit for workspaces (e.g. "4G")
	DefaultMemory string `env:"WORKSPACE_DEFAULT_MEMORY" envDefault:"4G"`
	// DefaultDisk is the default disk limit for workspaces (e.g. "10G")
	DefaultDisk string `env:"WORKSPACE_DEFAULT_DISK" envDefault:"10G"`
	// E2BAPIKey is the API key for E2B managed sandbox provider
	E2BAPIKey string `env:"E2B_API_KEY" envDefault:""`
	// GitHubAppEncryptionKey is the AES-256 encryption key for GitHub App credentials (64-char hex)
	GitHubAppEncryptionKey string `env:"GITHUB_APP_ENCRYPTION_KEY" envDefault:""`
	// NetworkName is the Docker network to attach workspace containers to for isolation
	NetworkName string `env:"WORKSPACE_NETWORK_NAME" envDefault:""`
	// DefaultImage is the Docker image for workspace containers (e.g. ghcr.io/emergent-company/memory-workspace:latest)
	DefaultImage string `env:"WORKSPACE_DEFAULT_IMAGE" envDefault:""`
	// FirecrackerDataDir is the directory containing Firecracker rootfs and kernel files
	FirecrackerDataDir string `env:"WORKSPACE_FIRECRACKER_DATA_DIR" envDefault:"/var/lib/firecracker"`
}

// IsEnabled returns true if agent sandboxes are enabled
func (w *SandboxConfig) IsEnabled() bool {
	return w.Enabled
}

// BraveSearchConfig holds Brave Search API configuration
type BraveSearchConfig struct {
	// APIKey is the Brave Search API subscription token
	APIKey string `env:"BRAVE_SEARCH_API_KEY" envDefault:""`
	// Timeout is the HTTP request timeout for Brave Search API calls
	Timeout time.Duration `env:"BRAVE_SEARCH_TIMEOUT" envDefault:"15s"`
}

// IsConfigured returns true if Brave Search API key is set
func (b *BraveSearchConfig) IsConfigured() bool {
	return b.APIKey != ""
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

// GraphConfig holds configuration for the knowledge graph domain.
type GraphConfig struct {
	// MaxBatchObjects is the maximum number of objects allowed in a single bulk-create or subgraph call.
	// Default: 500.
	MaxBatchObjects int `env:"GRAPH_MAX_BATCH_OBJECTS" envDefault:"500"`

	// MaxBatchRelationships is the maximum number of relationships allowed in a single bulk-create or subgraph call.
	// Default: 500.
	MaxBatchRelationships int `env:"GRAPH_MAX_BATCH_RELATIONSHIPS" envDefault:"500"`

	// MaxListLimit is the maximum number of items returned by list endpoints.
	// Default: 1000.
	MaxListLimit int `env:"GRAPH_MAX_LIST_LIMIT" envDefault:"1000"`

	// DefaultListLimit is the default number of items returned by list endpoints when no limit is specified.
	// Default: 100.
	DefaultListLimit int `env:"GRAPH_DEFAULT_LIST_LIMIT" envDefault:"100"`
}

// AgentSafeguardsConfig holds configuration for agent queue explosion safeguards.
type AgentSafeguardsConfig struct {
	// MaxPendingJobs is the maximum number of pending/processing jobs allowed per agent.
	// New runs are rejected when this limit is reached. Default: 10.
	MaxPendingJobs int `env:"AGENT_MAX_PENDING_JOBS" envDefault:"10"`

	// ConsecutiveFailureThreshold is the number of consecutive failures before auto-disabling an agent.
	// The agent is set to enabled=false after this many failures in a row. Default: 5.
	ConsecutiveFailureThreshold int `env:"AGENT_CONSECUTIVE_FAILURE_THRESHOLD" envDefault:"5"`

	// MinCronIntervalMinutes is the minimum allowed interval in minutes for cron-scheduled agents.
	// Cron expressions that fire more frequently than this are rejected. Default: 15.
	MinCronIntervalMinutes int `env:"AGENT_MIN_CRON_INTERVAL_MINUTES" envDefault:"15"`

	// BudgetEnforcementEnabled controls whether budget limits block agent execution.
	// When false, budget checks still run but won't block runs. Default: true.
	BudgetEnforcementEnabled bool `env:"BUDGET_ENFORCEMENT_ENABLED" envDefault:"true"`

	// ExecutionEnabled is an emergency kill switch to disable all agent execution system-wide.
	// When false, all Execute() calls return immediately with an error. Default: true.
	ExecutionEnabled bool `env:"AGENT_EXECUTION_ENABLED" envDefault:"true"`
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
