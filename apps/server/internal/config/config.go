package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
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
	ServerPort      int    `env:"SERVER_PORT" envDefault:"3002"`
	ServerAddress   string `env:"SERVER_ADDRESS" envDefault:"0.0.0.0"`
	InternalBaseURL string `env:"INTERNAL_BASE_URL" envDefault:""`
	Environment     string `env:"ENVIRONMENT" envDefault:"local"`
	Debug           bool   `env:"DEBUG" envDefault:"false"`
	LogLevel        string `env:"LOG_LEVEL" envDefault:"info"`

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

	// Xberg document parsing configuration
	Xberg XbergConfig

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
	// AgentWorkerQueueRefreshInterval is how often the worker supervisor
	// reconciles the named queue configuration (kb.agent_queues).
	AgentWorkerQueueRefreshInterval time.Duration `env:"AGENT_WORKER_QUEUE_REFRESH_INTERVAL" envDefault:"30s"`

	// Object-driven work maintenance. The reaper returns stranded in-progress
	// work objects to ready/blocked; the reconciler enqueues ready objects with
	// no live run/job. The reaper threshold matches the stale-run reaper's
	// threshold so the two never race.
	WorkStatusReaperInterval  time.Duration `env:"WORK_STATUS_REAPER_INTERVAL" envDefault:"5m"`
	WorkStatusReaperThreshold time.Duration `env:"WORK_STATUS_REAPER_THRESHOLD" envDefault:"30m"`
	WorkReconcilerInterval    time.Duration `env:"WORK_RECONCILER_INTERVAL" envDefault:"1m"`

	// Agent safeguards configuration
	AgentSafeguards AgentSafeguardsConfig

	// Brave Search API configuration
	BraveSearch BraveSearchConfig

	// MCP tool guardrails (entity-query timeout and payload bounds).
	MCP MCPConfig

	// OpenTelemetry tracing configuration
	Otel OtelConfig

	// Graph knowledge base configuration
	Graph GraphConfig

	// Journal audit log configuration
	Journal JournalConfig

	// Chunking configuration
	Chunking ChunkingConfig

	// Embedding job queue configuration
	EmbeddingQueue EmbeddingQueueConfig

	// Resource governor / admission control configuration
	Governor GovernorConfig

	// Skills configuration
	Skills SkillsConfig

	// MCPRegistry configures the official MCP registry client.
	MCPRegistry MCPRegistryConfig

	// Feedback configures user/agent feedback intake. When a token is present,
	// feedback submitted through POST /api/feedback or the feedback-submit MCP
	// tool is filed as a GitHub issue in Feedback.Repo.
	Feedback FeedbackConfig

	// SourceOAuth configures the deployment-default OAuth clients used by the
	// data-source connect flow when an org has no client of its own.
	SourceOAuth SourceOAuthConfig

	// GitHubSource configures the first-class GitHub data-source connector.
	GitHubSource GitHubSourceConfig

	// GmailSource configures the first-class Gmail data-source connector.
	GmailSource GmailSourceConfig

	// WebSource configures the first-class web-scraper data-source connector.
	WebSource WebSourceConfig

	// OAuthAS configures the built-in OAuth 2.1 authorization server that lets
	// third-party apps obtain narrow, project-scoped Memory API tokens after a
	// user consents. Zitadel remains the login provider.
	OAuthAS OAuthASConfig

	// AppURL is the base URL of the API host, e.g.
	// https://memory.emergent-company.ai. A2A discovery advertises it, and it
	// is the fallback for browser-facing links when WEB_UI_URL is unset.
	AppURL string `env:"APP_URL" envDefault:"https://memory.emergent-company.ai"`

	// WebUIURL is the base URL of the web UI host that serves the signed-in
	// app, e.g. https://app.memory.emergent-company.ai. Invite links in emails
	// and in-app notifications point here because only the web UI owns a user
	// session — the API host authenticates Bearer tokens, so a browser arriving
	// there has no identity to accept the invitation with. When unset, links
	// fall back to AppURL.
	WebUIURL string `env:"WEB_UI_URL" envDefault:""`

	// A2AOrigin is the public origin advertised in A2A AgentCards
	// (supportedInterfaces[].url). When empty, the interface URL is derived from
	// the incoming request (respecting X-Forwarded-Proto/Host for proxied
	// deployments). Set A2A_ORIGIN to override, e.g. https://api.example.com.
	A2AOrigin string `env:"A2A_ORIGIN" envDefault:""`

	// AskV2 enables the code-generation variant of the CLI assistant agent.
	// When true, POST /api/ask and /api/projects/:id/ask use EnsureCliAssistantAgentV2
	// which generates Python SDK scripts instead of calling 57 individual MCP tools.
	AskV2 bool `env:"MEMORY_ASK_V2" envDefault:"false"`

	// GitHubTokenEncryptionKey is the AES-256 encryption key (64-char hex) for
	// project-scoped GitHub personal access tokens. Falls back to
	// LLMProvider.EncryptionKey (LLM_ENCRYPTION_KEY) when unset.
	GitHubTokenEncryptionKey string `env:"GITHUB_TOKEN_ENCRYPTION_KEY" envDefault:""`
	// BlueprintUpdateCheckSchedule is the cron schedule (seconds precision) for
	// the always-on sweep that enqueues one check job per applied GitHub-imported
	// blueprint. Default: daily at 02:00.
	BlueprintUpdateCheckSchedule string `env:"BLUEPRINT_UPDATE_CHECK_SCHEDULE" envDefault:"0 0 2 * * *"`
	// BlueprintUpdateJobInterval is how often the job runner claims and executes
	// pending update-check jobs. Default: 30s.
	BlueprintUpdateJobInterval time.Duration `env:"BLUEPRINT_UPDATE_JOB_INTERVAL" envDefault:"30s"`

	// Features controls which optional domain modules are loaded at startup.
	// See FeatureSet for per-flag documentation and defaults.
	Features FeatureSet

	// ExtractionSkipStagingBranch disables the staging branch for extraction jobs.
	// When true, extracted objects are written directly to the main graph.
	// Useful for development/benchmark environments where review workflows are not needed.
	ExtractionSkipStagingBranch bool `env:"EXTRACTION_SKIP_STAGING" envDefault:"false"`

	// Server timeouts
	ReadTimeout  time.Duration `env:"SERVER_READ_TIMEOUT" envDefault:"3600s"`   // 1 hour for large file uploads
	WriteTimeout time.Duration `env:"SERVER_WRITE_TIMEOUT" envDefault:"28800s"` // 8 hours for SSE
	IdleTimeout  time.Duration `env:"SERVER_IDLE_TIMEOUT" envDefault:"28800s"`  // 8 hours for SSE
	// ShutdownTimeout bounds how long the HTTP server waits for in-flight
	// requests to drain on shutdown before the DB pool is closed. Kept well
	// above the old 10s so long-running retrieval/embedding/graph requests
	// finish rather than being killed mid-flight (issue #1481). The effective
	// ceiling is fx's own stop timeout, which cmd/server sets to
	// ShutdownTimeout+5s — see cmd/server/main.go.
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"30s"`

	// ShareRefSecret is the server-side HMAC key used to verify gateway-minted
	// end-user references (X-End-User-Ref-Sig) on public agent-share routes.
	// When unset, share end-user identity verification fails closed (503).
	ShareRefSecret string `env:"SHARE_REF_SECRET" envDefault:""`
}

// DatabaseConfig holds PostgreSQL connection settings
type DatabaseConfig struct {
	Host     string `env:"POSTGRES_HOST" envDefault:"localhost"`
	Port     int    `env:"POSTGRES_PORT" envDefault:"5432"`
	User     string `env:"POSTGRES_USER" envDefault:"emergent"`
	Password string `env:"POSTGRES_PASSWORD" envDefault:""`
	Database string `env:"POSTGRES_DB" envDefault:"emergent"`
	SSLMode  string `env:"POSTGRES_SSL_MODE" envDefault:"disable"`
	// MaxOpenConns is the pgx pool maximum connections. For multi-tenant deployments
	// with concurrent retrieval + embedding + graph writes, 100 is the default to
	// avoid pool saturation under load (raise/lower via env as needed).
	MaxOpenConns int `env:"DB_MAX_OPEN_CONNS" envDefault:"100"`
	// MaxIdleConns keeps a warm pool so bursts do not pay connection setup;
	// a quarter of MaxOpenConns is a sensible steady-state default.
	MaxIdleConns int           `env:"DB_MAX_IDLE_CONNS" envDefault:"25"`
	MaxIdleTime  time.Duration `env:"DB_MAX_IDLE_TIME" envDefault:"5m"`
	QueryDebug   bool          `env:"DB_QUERY_DEBUG" envDefault:"false"`
	// ApplicationName is reported as the PostgreSQL application_name for every
	// connection in this pool, so pg_stat_activity can attribute connections to
	// their client. Set DB_APPLICATION_NAME to override.
	ApplicationName string `env:"DB_APPLICATION_NAME" envDefault:"memory-server"`
	// PingMaxAttempts bounds how many times the startup connectivity check
	// pings the database before the pool construction fails. A transient DB
	// blip during a restart-flap used to hard-fail immediately; a bounded retry
	// lets a restart ride out a short outage. Values < 1 are treated as 1.
	PingMaxAttempts int `env:"DB_PING_MAX_ATTEMPTS" envDefault:"10"`
	// PingInitialBackoff is the delay before the second startup ping attempt.
	// Each subsequent delay doubles, capped at PingMaxBackoff.
	PingInitialBackoff time.Duration `env:"DB_PING_INITIAL_BACKOFF" envDefault:"250ms"`
	// PingMaxBackoff caps the exponential startup-ping retry delay.
	PingMaxBackoff time.Duration `env:"DB_PING_MAX_BACKOFF" envDefault:"5s"`
	// BulkStatementTimeout bounds a single SQL statement issued from a bulk
	// (batch) endpoint. It is attached to each item's transaction via
	// `set_config('statement_timeout', …, true)`, so one runaway statement
	// fails cleanly instead of holding a pool connection (and starving other
	// requests) past the 10-minute per-request bulk budget. 0 disables it.
	BulkStatementTimeout time.Duration `env:"DB_BULK_STATEMENT_TIMEOUT" envDefault:"10m"`
}

// DSN returns the PostgreSQL connection string
// DSN returns the PostgreSQL connection string.
//
// Userinfo is escaped with net/url so a password containing reserved
// characters ('@', ':', '/', '%', …) round-trips through pgx's URL parser
// instead of corrupting the DSN. Ordinary credentials are unchanged.
func (d *DatabaseConfig) DSN() string {
	userinfo := url.UserPassword(d.User, d.Password).String()
	query := url.Values{}
	query.Set("sslmode", d.SSLMode)
	if d.ApplicationName != "" {
		query.Set("application_name", d.ApplicationName)
	}
	return fmt.Sprintf(
		"postgres://%s@%s:%d/%s?%s",
		userinfo, d.Host, d.Port, d.Database, query.Encode(),
	)
}

// ZitadelConfig holds Zitadel/OIDC authentication settings
type ZitadelConfig struct {
	// Domain for Zitadel instance (e.g., "zitadel.example.com")
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

	// AdminPAT is a personal access token for an IAM_OWNER service user.
	// Used by the backend to call the Zitadel Management API (e.g. fetch user profiles).
	AdminPAT string `env:"ZITADEL_ADMIN_PAT"`

	// OIDCDefaultScopes is the scope set granted to an authenticated OIDC user
	// with no application entitlement and no project membership. Comma-separated.
	// Empty (the default) grants nothing, which preserves the fail-closed
	// behaviour of the introspection path.
	// The application-owned name is MEMORY_OIDC_DEFAULT_SCOPES.
	OIDCDefaultScopes []string `env:"MEMORY_OIDC_DEFAULT_SCOPES"`

	// TrustRoleSuperadmin governs whether a standing Zitadel project role maps to
	// the superadmin_full entitlement (issue #812 Q6). Default OFF; the role must
	// match the exact (issuer, SuperadminOrgID, SuperadminRole) triple and maps to
	// superadmin_full only. Fail-closed when disabled or mismatched.
	TrustRoleSuperadmin bool `env:"MEMORY_OIDC_TRUST_ROLE_SUPERADMIN" envDefault:"false"`

	// SuperadminRole is the exact Zitadel project role name that resolves to
	// superadmin_full when TrustRoleSuperadmin is enabled.
	SuperadminRole string `env:"MEMORY_OIDC_SUPERADMIN_ROLE"`

	// SuperadminOrgID is the exact Zitadel organization that owns the superadmin
	// project role. Together with SuperadminRole and the configured issuer it
	// forms the exact triple that must match before a role-derived superadmin is
	// granted.
	SuperadminOrgID string `env:"MEMORY_OIDC_SUPERADMIN_ORG_ID"`
}

// IntrospectionConfigured reports whether RFC 7662 introspection is enabled and
// has client credentials — the same precondition ZitadelService.Introspect uses.
// DisableIntrospection forces false even when credentials are present.
func (z *ZitadelConfig) IntrospectionConfigured() bool {
	if z == nil || z.DisableIntrospection {
		return false
	}
	return z.ClientJWT != "" || z.ClientJWTPath != ""
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

	// Chat model name — must include provider prefix (e.g. "deepseek/deepseek-v4-flash").
	// Intentionally no default: model must be configured explicitly via project/org DB config
	// or via VERTEX_AI_MODEL / DEEPSEEK_MODEL / OPENAI_MODEL env vars.
	Model string `env:"VERTEX_AI_MODEL" envDefault:""`

	// Max output tokens for chat completions (65536 for Gemini 3 thinking models)
	MaxOutputTokens int `env:"LLM_MAX_OUTPUT_TOKENS" envDefault:"65536"`

	// Temperature for chat completions (0.0-1.0)
	Temperature float64 `env:"LLM_TEMPERATURE" envDefault:"0"`

	// Request timeout
	Timeout time.Duration `env:"LLM_TIMEOUT" envDefault:"120s"`

	// Google API Key for Google AI (standalone/development fallback)
	GoogleAPIKey string `env:"GOOGLE_API_KEY" envDefault:""`

	// DeepSeek provider configuration
	DeepSeekAPIKey string `env:"DEEPSEEK_API_KEY" envDefault:""`
	// DeepSeekModel must include provider prefix: "deepseek/<model-name>"
	DeepSeekModel string `env:"DEEPSEEK_MODEL" envDefault:""`

	// OpenAI provider configuration
	OpenAIAPIKey string `env:"OPENAI_API_KEY" envDefault:""`
	// OpenAIBaseURL overrides the default https://api.openai.com/v1 endpoint
	OpenAIBaseURL string `env:"OPENAI_BASE_URL" envDefault:""`
	// OpenAIModel must include provider prefix: "openai/<model-name>"
	OpenAIModel string `env:"OPENAI_MODEL" envDefault:""`

	// Disable LLM network calls (for testing)
	NetworkDisabled bool `env:"LLM_NETWORK_DISABLED" envDefault:"false"`
}

// IsEnabled returns true if LLM is configured
func (l *LLMConfig) IsEnabled() bool {
	if l.NetworkDisabled {
		return false
	}
	return l.UseVertexAI() || l.GoogleAPIKey != "" || l.DeepSeekAPIKey != "" || l.OpenAIAPIKey != ""
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
	// MailgunSigningKey is the Mailgun webhook signing key. It is required to
	// verify the HMAC signature on the public delivery webhook; when unset, the
	// webhook rejects every request (fail closed).
	MailgunSigningKey string `env:"MAILGUN_SIGNING_KEY" envDefault:""`
	// MailgunWebhookTolerance is the maximum age (and future skew) accepted for
	// a webhook signature timestamp. Mailgun's HMAC binds only timestamp+token,
	// not the body, so a timestamp outside this window is rejected as a replay.
	// Default: 5m.
	MailgunWebhookTolerance time.Duration `env:"MAILGUN_WEBHOOK_TOLERANCE" envDefault:"5m"`
	// MailgunWebhookRatePerMin caps webhook requests per client IP per minute.
	// Default: 120.
	MailgunWebhookRatePerMin int `env:"MAILGUN_WEBHOOK_RATE_PER_MIN" envDefault:"120"`
	// MailgunWebhookRateBurst is the per-IP burst allowance. Default: 40.
	MailgunWebhookRateBurst int `env:"MAILGUN_WEBHOOK_RATE_BURST" envDefault:"40"`
	// MailgunWebhookGlobalRatePerMin caps total webhook requests per minute
	// across all client IPs, backstopping a spoofed X-Forwarded-For. Default: 1200.
	MailgunWebhookGlobalRatePerMin int `env:"MAILGUN_WEBHOOK_GLOBAL_RATE_PER_MIN" envDefault:"1200"`
	// MailgunWebhookGlobalRateBurst is the global burst allowance. Default: 300.
	MailgunWebhookGlobalRateBurst int `env:"MAILGUN_WEBHOOK_GLOBAL_RATE_BURST" envDefault:"300"`
	// FromEmail is the default from email address
	FromEmail string `env:"EMAIL_FROM_ADDRESS" envDefault:"noreply@example.com"`
	// FromName is the default from name
	FromName string `env:"EMAIL_FROM_NAME" envDefault:"emergent.memory"`
	// MaxRetries is the maximum number of retry attempts (default: 3)
	MaxRetries int `env:"EMAIL_MAX_RETRIES" envDefault:"3"`
	// RetryDelaySec is the base delay in seconds for retries (default: 60)
	RetryDelaySec int `env:"EMAIL_RETRY_DELAY_SEC" envDefault:"60"`
	// WorkerIntervalMs is the polling interval in milliseconds (default: 5000)
	WorkerIntervalMs int `env:"EMAIL_WORKER_INTERVAL_MS" envDefault:"5000"`
	// WorkerBatchSize is the number of jobs to process per poll (default: 10)
	WorkerBatchSize int `env:"EMAIL_WORKER_BATCH_SIZE" envDefault:"10"`
	// MailgunRegion is the Mailgun region ("us" or "eu", default: "us")
	MailgunRegion string `env:"MAILGUN_REGION" envDefault:"us"`
	// MailgunAPIBase overrides the Mailgun API base URL (the value passed to the
	// Mailgun SDK's SetAPIBase, including the /v3 path segment). When empty the
	// SDK default is used, or the EU base when MailgunRegion is "eu". This is
	// used by the e2e harness to point the Mailgun transport at a local stub.
	MailgunAPIBase string `env:"MAILGUN_API_BASE" envDefault:""`
	// Transport selects the email transport ("mailgun" or "smtp", default: "mailgun")
	Transport string `env:"EMAIL_TRANSPORT" envDefault:"mailgun"`
	// SMTPHost is the SMTP server host
	SMTPHost string `env:"SMTP_HOST" envDefault:""`
	// SMTPPort is the SMTP server port (default: 1025)
	SMTPPort int `env:"SMTP_PORT" envDefault:"1025"`
	// SMTPUsername is the SMTP auth username (optional)
	SMTPUsername string `env:"SMTP_USERNAME" envDefault:""`
	// SMTPPassword is the SMTP auth password (optional)
	SMTPPassword string `env:"SMTP_PASSWORD" envDefault:""`
	// SMTPTLS is the SMTP TLS mode ("none", "starttls", or "tls", default: "none")
	SMTPTLS string `env:"SMTP_TLS" envDefault:"none"`
}

// IsConfigured returns true if Mailgun is configured
func (e *EmailConfig) IsConfigured() bool {
	return e.MailgunDomain != "" && e.MailgunAPIKey != ""
}

// XbergConfig holds Xberg document parsing service configuration
type XbergConfig struct {
	// Enabled determines if Xberg parsing is enabled
	Enabled bool `env:"XBERG_ENABLED" envDefault:"true"`
	// ServiceURL is the Xberg service URL
	ServiceURL string `env:"XBERG_SERVICE_URL" envDefault:"http://localhost:8000"`
	// Timeout is the request timeout in milliseconds (default: 300000 = 5 minutes)
	TimeoutMs int `env:"XBERG_SERVICE_TIMEOUT" envDefault:"300000"`
	// MaxFileSizeMB is the maximum file size for document parsing
	MaxFileSizeMB int `env:"XBERG_MAX_FILE_SIZE_MB" envDefault:"100"`
	// WorkerIntervalMs is the polling interval in milliseconds (default: 5000)
	WorkerIntervalMs int `env:"DOCUMENT_PARSING_WORKER_INTERVAL_MS" envDefault:"5000"`
	// WorkerBatchSize is the number of jobs to process per poll (default: 5)
	WorkerBatchSize int `env:"DOCUMENT_PARSING_WORKER_BATCH_SIZE" envDefault:"5"`
}

// Timeout returns the request timeout as a Duration
func (k *XbergConfig) Timeout() time.Duration {
	return time.Duration(k.TimeoutMs) * time.Millisecond
}

// WorkerInterval returns the worker interval as a Duration
func (k *XbergConfig) WorkerInterval() time.Duration {
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
	// AllowedMIMETypes is a comma-separated list of permitted MIME types for uploads.
	// When empty, all MIME types are accepted (no restriction).
	// Example: "application/pdf,image/jpeg,image/png,text/plain"
	AllowedMIMETypes string `env:"ALLOWED_MIME_TYPES" envDefault:""`
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
	// PersistentIdleTTLDays enables idle reclamation of persistent MCP server
	// workspaces. A server whose last_used_at is older than this many days is
	// destroyed by the cleanup cycle. 0 (the default) disables the policy and
	// preserves current persistent semantics — nothing is reclaimed by idleness.
	PersistentIdleTTLDays int `env:"WORKSPACE_PERSISTENT_IDLE_TTL_DAYS" envDefault:"0"`
	// WarmPoolSize is the number of pre-booted containers to keep ready (0 = disabled,
	// default: 2). Containers are image-only: repository checkout and setup still
	// run per session after acquire, so this cuts boot time, not clone time.
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
	// MemoryAPIURL is the API endpoint injected into sandbox containers as
	// MEMORY_API_URL / MEMORY_SERVER_URL so that agent tools can call back into
	// this server. Defaults to the host gateway for single-host Docker
	// deployments. When sandboxes run on a dedicated Docker network, set this to
	// a URL the sandbox can actually reach (e.g. http://memory-server:3002 or the
	// bridge gateway) — otherwise host.docker.internal:3002 times out because the
	// server port is usually published on loopback only.
	MemoryAPIURL string `env:"WORKSPACE_MEMORY_API_URL" envDefault:"http://host.docker.internal:3002"`
	// DefaultImage is the Docker image for workspace containers (e.g. ghcr.io/emergent-company/memory-workspace:latest)
	DefaultImage string `env:"WORKSPACE_DEFAULT_IMAGE" envDefault:""`
	// FirecrackerDataDir is the directory containing Firecracker rootfs and kernel files
	FirecrackerDataDir string `env:"WORKSPACE_FIRECRACKER_DATA_DIR" envDefault:"/var/lib/firecracker"`
	// ReconcileGraceMin is the grace period, in minutes, before an ownerless
	// sandbox container or volume may be destroyed by orphan reconciliation.
	ReconcileGraceMin int `env:"WORKSPACE_RECONCILE_GRACE_MIN" envDefault:"15"`
	// ReconcileEnabled toggles the label-driven orphan reconciliation pass.
	ReconcileEnabled bool `env:"WORKSPACE_RECONCILE_ENABLED" envDefault:"true"`
	// OwnerHeartbeatMin is the warm-pool owner liveness heartbeat interval, in
	// minutes. A warm-pool container whose owner heartbeat is older than
	// 3 × this value is treated as ownerless and is reapable.
	OwnerHeartbeatMin int `env:"WORKSPACE_OWNER_HEARTBEAT_MIN" envDefault:"2"`
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

// MCPConfig holds guardrails for MCP tool calls (issue #1148).
type MCPConfig struct {
	// PublicURL is the deployment's externally reachable API origin (scheme +
	// host, no trailing slash), set by the deployment. MCP share invite emails
	// and client snippets are built from it: a call proxied from the web UI
	// carries the internal compose service name in Host, which an outside MCP
	// client cannot resolve. Empty falls back to the request-derived base URL.
	PublicURL string `env:"MCP_PUBLIC_URL" envDefault:""`

	// EntityQueryTimeout is the hard per-call deadline for the entity-query
	// tool. A single filtered query must never stall an agent turn for minutes.
	// Default: 30s.
	EntityQueryTimeout time.Duration `env:"MCP_ENTITY_QUERY_TIMEOUT" envDefault:"30s"`

	// EntityQueryFullMaxLimit caps the effective `limit` when
	// field_strategy="full" is requested, because a full provision/paragraph
	// payload is large (~14 KB per row) and a high limit multiplies it. Default: 25.
	EntityQueryFullMaxLimit int `env:"MCP_ENTITY_QUERY_FULL_MAX_LIMIT" envDefault:"25"`

	// ToolResultMaxBytes caps the JSON size of a single tool result as seen by
	// the model. The full result is still persisted (kb.agent_run_tool_calls,
	// kb.agent_run_messages) and streamed to the UI; only the model context is
	// truncated, with an actionable marker. Default: 128 KiB (131072 ≈ 32k
	// tokens). A negative value disables per-result truncation for every tool
	// (and ignores ToolResultMaxBytesOverrides).
	ToolResultMaxBytes int `env:"MCP_TOOL_RESULT_MAX_BYTES" envDefault:"131072"`

	// ToolResultMaxBytesOverrides overrides ToolResultMaxBytes for named tools,
	// as a comma-separated "tool=bytes" list (e.g.
	// "entity-edges-get=262144,session-get-messages=262144"). Applied only while
	// ToolResultMaxBytes is enabled (>= 0); a positive override does not
	// re-enable a globally disabled per-result cap. Default: empty.
	ToolResultMaxBytesOverrides string `env:"MCP_TOOL_RESULT_MAX_BYTES_OVERRIDES" envDefault:""`

	// ToolResultTotalBudgetBytes caps the SUM of all tool results in one model
	// request. When exceeded, the oldest results are elided (most-recent
	// retained) so per-step context does not grow with run length. Default:
	// 512 KiB (524288 ≈ 128k tokens) — with the 128 KiB per-result default this
	// only trims the extreme tail (>4 capped results in one request). The budget
	// is soft: the most recent result is never elided, so a single newest result
	// larger than the budget is retained whole. A negative value disables it.
	ToolResultTotalBudgetBytes int `env:"MCP_TOOL_RESULT_TOTAL_BUDGET_BYTES" envDefault:"524288"`
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

	// APIKey2 is the static API key for the secondary standalone user (invitee in tests)
	APIKey2 string `env:"STANDALONE_API_KEY_2" envDefault:""`

	// UserEmail2 is the email for the secondary standalone user
	UserEmail2 string `env:"STANDALONE_USER_EMAIL_2" envDefault:""`
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

	// MigrationScanMaxObjects is the hard cap on the number of objects a
	// synchronous schema migrate/rollback request may scan. When a scan would
	// exceed it the request aborts with a 4xx telling the caller to narrow the
	// operation. Default: 10000. Ops note: this is a behaviour change for very
	// large projects — previously these requests were unbounded; if a legitimate
	// migration/rollback scans more than 10000 objects, raise
	// GRAPH_MIGRATION_SCAN_MAX_OBJECTS rather than splitting the operation.
	MigrationScanMaxObjects int `env:"GRAPH_MIGRATION_SCAN_MAX_OBJECTS" envDefault:"10000"`

	// BulkConcurrency bounds how many items a single bulk create/upsert/update
	// request processes in parallel. Each in-flight item holds one database
	// connection for the duration of its item transaction, so the effective
	// worker count is clamped to the connection pool minus one
	// (DB_MAX_OPEN_CONNS-1, floor 1), reserving one connection for other
	// traffic (#1482).
	// Default: 8 — enough to overlap the per-object version-write I/O wait
	// (issue #1573) while leaving pool headroom for concurrent requests.
	BulkConcurrency int `env:"GRAPH_BULK_CONCURRENCY" envDefault:"8"`

	// BulkUpdateMaxItems is the maximum number of items accepted by a single
	// bulk-update request. Default: 100.
	BulkUpdateMaxItems int `env:"BULK_UPDATE_MAX_ITEMS" envDefault:"100"`

	// BulkUpdateMaxDuration bounds the detached context used by BulkUpdateObjects
	// so a client disconnect cannot leave a batch holding DB connections
	// indefinitely. Default: 60s.
	BulkUpdateMaxDuration time.Duration `env:"BULK_UPDATE_MAX_DURATION" envDefault:"60s"`
}

// JournalConfig holds configuration for the journal audit log's bounded async
// writer. The journal is an append-only audit log of knowledge-graph mutations;
// entries are buffered in-process and flushed in multi-row batches by a fixed
// pool of workers rather than one goroutine + INSERT per mutation.
type JournalConfig struct {
	// WriterConcurrency is the number of background workers that flush journal
	// entries to the database. Default: 8.
	WriterConcurrency int `env:"JOURNAL_WRITER_CONCURRENCY" envDefault:"8"`
	// WriterQueueSize is the buffered queue capacity. When the queue is full,
	// Log blocks (with ctx cancellation as backpressure release) rather than
	// dropping audit records. Default: 4096.
	WriterQueueSize int `env:"JOURNAL_WRITE_QUEUE_SIZE" envDefault:"4096"`
	// WriterBatchSize is the maximum number of entries per multi-row INSERT.
	// Default: 256.
	WriterBatchSize int `env:"JOURNAL_WRITE_BATCH_SIZE" envDefault:"256"`
}

// ChunkingConfig holds configuration for text chunking.
type ChunkingConfig struct {
	// ChunkSize is the default chunk size in characters. Default: 1000.
	ChunkSize int `env:"CHUNK_SIZE" envDefault:"1000"`
	// ChunkOverlap is the default chunk overlap in characters. Default: 200.
	ChunkOverlap int `env:"CHUNK_OVERLAP" envDefault:"200"`
}

// EmbeddingQueueConfig holds configuration for the embedding job worker queues.
type EmbeddingQueueConfig struct {
	// GraphConcurrency is the graph embedding worker concurrency. Default: 200.
	GraphConcurrency int `env:"GRAPH_EMBEDDING_CONCURRENCY" envDefault:"200"`
	// GraphBatchSize is the graph embedding batch size. Default: 200.
	GraphBatchSize int `env:"GRAPH_EMBEDDING_BATCH_SIZE" envDefault:"200"`
	// GraphRequestBatchSize is the maximum number of graph objects sent in a
	// single multi-object embedding request. Default: 100.
	GraphRequestBatchSize int `env:"GRAPH_EMBEDDING_REQUEST_BATCH_SIZE" envDefault:"100"`
	// ChunkConcurrency is the chunk embedding worker concurrency. Default: 10.
	ChunkConcurrency int `env:"CHUNK_EMBEDDING_CONCURRENCY" envDefault:"10"`
	// ChunkBatchSize is the chunk embedding batch size. Default: 10.
	ChunkBatchSize int `env:"CHUNK_EMBEDDING_BATCH_SIZE" envDefault:"10"`
	// AdaptiveScaling enables dynamic concurrency adjustment based on system health.
	// Default: true.
	AdaptiveScaling bool `env:"EMBEDDING_ADAPTIVE_SCALING" envDefault:"true"`
}

// GovernorConfig holds configuration for the resource governor / admission
// control. Shedding is driven by attributable capacity (DB pool utilization +
// in-flight bulk admissions); host I/O/CPU pressure only throttles.
type GovernorConfig struct {
	// Enabled toggles the resource governor. Default: true.
	Enabled bool `env:"GOVERNOR_ENABLED" envDefault:"true"`
	// ThrottleIOWaitPercent is the I/O wait percentage at/above which the
	// governor throttles. Default: 30.
	ThrottleIOWaitPercent float64 `env:"GOVERNOR_THROTTLE_IOWAIT_PERCENT" envDefault:"30"`
	// WarningCPULoadFactor is the load multiple of CPU cores at/above which the
	// governor throttles. Default: 2.
	WarningCPULoadFactor float64 `env:"GOVERNOR_WARNING_LOAD_FACTOR" envDefault:"2"`
	// ShedDBPoolPercent is the DB pool utilization percentage at/above which
	// (combined with ShedBulkInflightBound) the governor sheds. Default: 90.
	ShedDBPoolPercent float64 `env:"GOVERNOR_SHED_DB_POOL_PERCENT" envDefault:"90"`
	// ShedBulkInflightBound is the in-flight bulk admissions count at/above
	// which (combined with ShedDBPoolPercent) the governor sheds. Default: 1.
	ShedBulkInflightBound int `env:"GOVERNOR_SHED_BULK_INFLIGHT_BOUND" envDefault:"1"`
	// SustainedSamples is the number of consecutive elevated samples before
	// escalating to shed. Default: 2.
	SustainedSamples int `env:"GOVERNOR_SUSTAINED_SAMPLES" envDefault:"2"`
	// RecoverSamples is the number of consecutive healthy samples before
	// de-escalating to normal. Default: 4.
	RecoverSamples int `env:"GOVERNOR_RECOVER_SAMPLES" envDefault:"4"`
	// MaxInflightPerProject is the max concurrent admitted requests per
	// project. Default: 1.
	MaxInflightPerProject int `env:"GOVERNOR_MAX_INFLIGHT_PER_PROJECT" envDefault:"1"`
	// MaxInflightGlobalInteractive is the max concurrent admitted interactive
	// requests globally. Default: 1.
	MaxInflightGlobalInteractive int `env:"GOVERNOR_MAX_INFLIGHT_GLOBAL_INTERACTIVE" envDefault:"1"`
	// MaxInflightGlobalBulk is the max concurrent admitted bulk requests
	// globally. Default: 1.
	MaxInflightGlobalBulk int `env:"GOVERNOR_MAX_INFLIGHT_GLOBAL_BULK" envDefault:"1"`
	// RatePerMinutePerProject is the per-project sustained interactive admission
	// rate. Default: 30.
	RatePerMinutePerProject int `env:"GOVERNOR_RATE_PER_MINUTE_PER_PROJECT" envDefault:"30"`
	// RatePerMinutePerProjectBulk is the per-project sustained bulk admission
	// rate. Default: 0 (disabled — bulk is governed by inflight + backpressure).
	RatePerMinutePerProjectBulk int `env:"GOVERNOR_RATE_PER_MINUTE_PER_PROJECT_BULK" envDefault:"0"`
	// RateBurst is the per-project rate limiter burst. Default: 5.
	RateBurst int `env:"GOVERNOR_RATE_BURST" envDefault:"5"`
	// BulkWaitMs is how long a bulk admission waits for capacity under
	// throttle before 429 + Retry-After. Default: 2000.
	BulkWaitMs int `env:"GOVERNOR_BULK_ADMIT_WAIT_MS" envDefault:"2000"`
	// MaxWaiters is the max concurrent bulk admissions waiting for capacity.
	// Default: 100.
	MaxWaiters int `env:"GOVERNOR_MAX_WAITERS" envDefault:"100"`
	// RetryAfterSeconds is the Retry-After header value on rejected requests.
	// Default: 15.
	RetryAfterSeconds int `env:"GOVERNOR_RETRY_AFTER_SECONDS" envDefault:"15"`
}

// SkillsConfig holds configuration for the skills domain.
type SkillsConfig struct {
	// MaxContentSizeBytes is the maximum allowed skill content size in bytes.
	// Default: 1 MiB (1048576). Values <= 0 fall back to the default.
	MaxContentSizeBytes int `env:"SKILL_MAX_CONTENT_SIZE_BYTES" envDefault:"1048576"`
}

// MaxContentSize returns the effective content size cap (normalized to the
// default when unset or non-positive).
func (s SkillsConfig) MaxContentSize() int {
	if s.MaxContentSizeBytes <= 0 {
		return 1 << 20
	}
	return s.MaxContentSizeBytes
}

// MCPRegistryConfig configures the client for the official MCP registry
// (registry.modelcontextprotocol.io). It exists so tests can point the
// server at a local stub instead of the live upstream.
type MCPRegistryConfig struct {
	// BaseURL overrides the official MCP registry endpoint. Empty means the
	// default DefaultRegistryBaseURL (https://registry.modelcontextprotocol.io).
	BaseURL string `env:"MCP_REGISTRY_BASE_URL" envDefault:""`
	// OAuthRedirectBaseURL is the externally reachable origin of this API. The
	// OAuth redirect URI registered with an authorization server is
	// <OAuthRedirectBaseURL>/api/mcp-servers/oauth/callback, so the value must be
	// stable per deployment (dynamic client registration binds it). Empty
	// disables the MCP OAuth connect flow with an actionable error.
	OAuthRedirectBaseURL string `env:"MCP_REGISTRY_OAUTH_REDIRECT_BASE_URL" envDefault:""`
	// OAuthLandingURL is the browser URL the OAuth callback redirects to after
	// the flow. It should be the gateway's scope-agnostic landing route,
	// e.g. https://admin.example.com/oauth/landing/mcp-servers: the callback is
	// issued by this API server, which has no org/project scope, so a scoped UI
	// path cannot be hardcoded here. The callback appends mcp_oauth=<outcome>
	// and, when the flow reached the point of knowing it, scope_project=<id> as
	// an untrusted hint the gateway validates against the caller's access tree.
	// Empty falls back to <OAuthRedirectBaseURL>/oauth/landing/mcp-servers,
	// which is only correct when the API and UI share an origin.
	OAuthLandingURL string `env:"MCP_REGISTRY_OAUTH_LANDING_URL" envDefault:""`
	// OAuthClientName is the client_name presented at dynamic client
	// registration. Empty defaults to "Memory".
	OAuthClientName string `env:"MCP_REGISTRY_OAUTH_CLIENT_NAME" envDefault:""`
}

// FeedbackConfig configures the user-facing feedback intake (REST endpoint and
// MCP tool). Feedback is filed as a GitHub issue; the token needs only
// issues:write on the target repository. An empty token disables the feature,
// which is the expected state for self-hosted deployments (there is no Memory
// team to receive the issue).
type FeedbackConfig struct {
	// Enabled is the explicit kill switch. The feature is still off when
	// Enabled is true but GitHubToken or Repo is empty.
	Enabled bool `env:"FEEDBACK_ENABLED" envDefault:"true"`
	// GitHubToken is a fine-grained personal access token (or GitHub App
	// installation token) with issues:write on Repo. Never log this value.
	GitHubToken string `env:"FEEDBACK_GITHUB_TOKEN" envDefault:""`
	// Repo is the "owner/name" repository that receives the issues. Empty
	// disables the feature: there is no built-in destination, because the
	// operator of a deployment is the only one who can say where its users'
	// feedback should go. A deployment that sets a token must set this too.
	Repo string `env:"FEEDBACK_GITHUB_REPO" envDefault:""`
	// APIBaseURL overrides the GitHub REST origin (GitHub Enterprise).
	APIBaseURL string `env:"FEEDBACK_GITHUB_API_BASE_URL" envDefault:"https://api.github.com"`
	// Label is applied to every created issue. If the repository has no such
	// label the issue is still created, without labels.
	Label string `env:"FEEDBACK_GITHUB_LABEL" envDefault:"feedback"`
	// RateLimitPerHour caps successful submissions per user per rolling hour.
	// Zero falls back to the package default.
	RateLimitPerHour int `env:"FEEDBACK_RATE_LIMIT_PER_HOUR" envDefault:"5"`
}

// SourceOAuthConfig holds the deployment-default OAuth client material and the
// public base URL used to build data-source OAuth redirect URIs. Per-org client
// config (BYO) is layered on top in a later phase; this is the fallback.
type SourceOAuthConfig struct {
	// RedirectBaseURL is the public origin of the API (e.g.
	// https://api.example.com). The OAuth redirect URI is
	// <RedirectBaseURL>/api/data-source-integrations/oauth/<provider>/callback,
	// and the post-callback browser landing path is <RedirectBaseURL>/data-sources.
	// Empty disables the connect flow with an actionable error.
	RedirectBaseURL string `env:"SOURCES_OAUTH_REDIRECT_BASE_URL" envDefault:""`

	// LandingURL is the browser URL the OAuth callback redirects to after the
	// flow. It should be the gateway's scope-agnostic landing route,
	// e.g. https://admin.example.com/oauth/landing/data-sources: the callback is
	// issued by this API server, which has no org/project scope, so a scoped UI
	// path cannot be hardcoded here. The callback appends connected=<id> or
	// oauth_error=<code> and, when the flow reached the point of knowing it,
	// scope_project=<id> as an untrusted hint the gateway validates against the
	// caller's access tree. Empty falls back to
	// <RedirectBaseURL>/oauth/landing/data-sources, which is only correct when
	// the API and UI share an origin.
	LandingURL string `env:"SOURCES_OAUTH_LANDING_URL" envDefault:""`

	// GoogleClientID/GoogleClientSecret are the deployment-default Google OAuth
	// client credentials. Empty means a connect requires an org-level or
	// project-level client.
	GoogleClientID     string `env:"SOURCES_OAUTH_GOOGLE_CLIENT_ID" envDefault:""`
	GoogleClientSecret string `env:"SOURCES_OAUTH_GOOGLE_CLIENT_SECRET" envDefault:""`
}

// GitHubSourceConfig holds the deployment-level settings for the GitHub
// data-source connector.
type GitHubSourceConfig struct {
	// APIBaseURL overrides the GitHub REST API origin. Empty means
	// https://api.github.com. Set it for GitHub Enterprise
	// (https://<host>/api/v3) or a hermetic test wrapper. This is a deployment
	// setting only: source configs can never override it, so a source cannot
	// point the server at an arbitrary host.
	APIBaseURL string `env:"GITHUB_API_BASE_URL" envDefault:""`
}

// GmailSourceConfig holds the deployment-level settings for the Gmail
// data-source connector.
type GmailSourceConfig struct {
	// APIBaseURL overrides the Gmail API origin (default
	// https://gmail.googleapis.com). It exists so a hermetic test wrapper can
	// stand in for Google; it is a deployment setting only, so a source config
	// can never point the connector at an arbitrary host.
	APIBaseURL string `env:"GMAIL_API_BASE_URL" envDefault:""`
	// TokenURL overrides the OAuth2 token endpoint used to refresh a Gmail
	// source's access token (default https://oauth2.googleapis.com/token). It
	// completes the hermetic seam APIBaseURL starts: a test wrapper that serves
	// the Gmail API must also serve the refresh grant, or the client still
	// reaches Google. Deployment setting only, never source-configurable.
	TokenURL string `env:"GMAIL_TOKEN_URL" envDefault:""`
}

// WebSourceConfig holds the deployment-level settings for the web-scraper
// data-source connector. It feeds the shared outbound SSRF guard (see
// internal/outbound.Policy) so an operator can scope which hosts the crawler is
// allowed to fetch.
type WebSourceConfig struct {
	// AllowedHosts lists hostnames the crawler may always fetch, matched as an
	// exact host or a "*.suffix" wildcard. This is the escape hatch for
	// operators who host crawlable content in private space; it never relaxes
	// the always-blocked link-local/metadata ranges.
	AllowedHosts []string `env:"WEB_SOURCE_ALLOWED_HOSTS" envDefault:""`
	// AllowPrivate permits loopback and RFC1918/ULA private addresses for the
	// crawler (test/dev). Link-local/metadata, multicast, unspecified, CGNAT,
	// and reserved ranges stay blocked regardless.
	AllowPrivate bool `env:"WEB_SOURCE_ALLOW_PRIVATE_HOSTS" envDefault:"false"`
}

// OAuthASConfig configures the built-in OAuth 2.1 authorization server (the
// server side of the third-party app consent flow). Values are read from the
// environment; the authorize URL points at the gateway consent page.
type OAuthASConfig struct {
	// Enabled gates registration of the whole authorization-server route set.
	// Token minting is unaffected: existing OAuth tokens keep working.
	Enabled bool `env:"OAUTH_AS_ENABLED" envDefault:"true"`

	// IssuerURL is the advertised RFC 8414 issuer. Empty derives the issuer from
	// the request scheme + host (honouring X-Forwarded-Proto/Host) so proxied
	// deployments work without extra configuration.
	IssuerURL string `env:"OAUTH_ISSUER_URL" envDefault:""`

	// AuthorizeURL is the gateway base URL hosting the consent page,
	// e.g. https://app.example.com. The metadata authorization_endpoint is
	// <AuthorizeURL>/oauth/authorize. Empty falls back to
	// <issuer>/oauth/authorize.
	AuthorizeURL string `env:"OAUTH_AUTHORIZE_URL" envDefault:""`

	// RegistrationEnabled gates POST /api/oauth/register.
	RegistrationEnabled bool `env:"OAUTH_REGISTRATION_ENABLED" envDefault:"true"`

	// RegistrationSecret, when set, must be presented in the
	// X-Registration-Secret header on registration.
	RegistrationSecret string `env:"OAUTH_REGISTRATION_SECRET" envDefault:""`

	// AllowedRedirectHosts is an optional comma-separated allowlist of hostnames.
	// When set, every registered redirect URI host must match one entry.
	AllowedRedirectHosts string `env:"OAUTH_ALLOWED_REDIRECT_HOSTS" envDefault:""`
}

// OAuthClientDefaults is the deployment-default OAuth client material for one
// provider.
type OAuthClientDefaults struct {
	ClientID     string
	ClientSecret string
}

// OAuthClientsByProvider returns the deployment-default OAuth client material
// keyed by data-source provider type. It is the fallback layer beneath the
// per-org client configs (design D3); adding a provider appends one entry.
// google_drive and gmail are both backed by the one Google vendor app, so the
// same client id/secret serves both (the app must have both APIs enabled and
// both scopes on its consent screen).
func (c SourceOAuthConfig) OAuthClientsByProvider() map[string]OAuthClientDefaults {
	out := map[string]OAuthClientDefaults{}
	if c.GoogleClientID != "" && c.GoogleClientSecret != "" {
		google := OAuthClientDefaults{ClientID: c.GoogleClientID, ClientSecret: c.GoogleClientSecret}
		out["google_drive"] = google
		out["gmail"] = google
	}
	return out
}

// DeploymentEnvVarHint returns the deployment-default env-var hint for a data
// source provider's OAuth client, or "" when the provider has no deployment
// default. The Google-backed providers (google_drive, gmail) share
// SOURCES_OAUTH_GOOGLE_CLIENT_ID/SECRET; a provider without a default gets no
// hint, so remediation never names an unrelated provider's env vars (e.g. it
// must never tell a github user to set the Google vars). Keep this aligned with
// OAuthClientsByProvider: every provider with a hint must also appear there, and
// vice versa.
func (c SourceOAuthConfig) DeploymentEnvVarHint(providerType string) string {
	switch providerType {
	case "google_drive", "gmail":
		return "SOURCES_OAUTH_GOOGLE_CLIENT_ID/SECRET"
	default:
		return ""
	}
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

// FrontendURL returns the base URL browser-facing links should point at:
// WebUIURL when configured, otherwise AppURL. Trailing slashes are removed so
// callers can append paths directly.
func (c *Config) FrontendURL() string {
	if u := strings.TrimRight(strings.TrimSpace(c.WebUIURL), "/"); u != "" {
		return u
	}
	return strings.TrimRight(strings.TrimSpace(c.AppURL), "/")
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
