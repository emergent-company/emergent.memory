package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is loaded from environment. Memory credentials are server-side only.
type Config struct {
	Port               string
	MemoryURL          string
	MemoryToken        string
	MemoryProjectID    string
	ClientAPIKey       string // optional admin key (TOKEN_API_KEY); per-device keys are issued via the QR setup flow
	BridgeBin          string
	BridgeArgs         []string
	BridgeWorkdir      string
	SupervisorInterval time.Duration
	BlueprintsDir      string // local bundled blueprint packs (default blueprints/)

	LiveKitAPIKey     string
	LiveKitAPISecret  string
	LiveKitURL        string
	LiveKitPublicURL  string
	TokenAllowedRooms string
	DefaultAgent      string
	// PublicBaseURL is the externally-reachable base URL (scheme://host[:port])
	// used for the client setup/config URLs. Empty = derive from request Host.
	PublicBaseURL string

	// Zitadel OIDC configuration for the web-session auth path. Empty issuer =
	// no sign-in offered.
	ZitadelIssuer      string
	ZitadelClientID    string
	ZitadelRedirectURI string
	// AuthMode selects the web-auth posture. "session" is the DEFAULT and
	// requires Zitadel config; "dev" is an explicit opt-in, unauthenticated
	// local-dev escape hatch.
	AuthMode string
	// SessionSecret is the HMAC signing key for the session cookie. Empty =
	// sessions cannot be issued (dev mode without sign-in).
	SessionSecret string
	// SessionMaxAge bounds how long the browser retains the session cookie.
	// Decoupled from the short-lived access token: the signed claims still
	// enforce access-token expiry server-side, and the refresh token silently
	// renews it. A longer value keeps users signed in (the Zitadel refresh
	// token lifetime is the true ceiling). Zero = derive from access-token
	// expiry (legacy behavior, used by tests).
	SessionMaxAge time.Duration
	// WorkerInternalKey is the shared secret the gateway injects into bridge
	// workers so they can fetch their per-room voice binding from the internal
	// endpoint. Empty = the internal endpoint rejects every request.
	WorkerInternalKey string
	// TTSProvider is the TTS strategy: "cartesia" (server-side) | "none"/"client"
	// (text-only, client-side TTS).
	TTSProvider string

	// Voice-flow configuration (read-only display on Project Settings; the
	// worker reads the same env). Defaults match memory_bridge/config.py.
	CartesiaModel        string
	CartesiaVoice        string
	CartesiaAPIKey       string // secret — only used to report set/unset
	DeepgramModel        string
	DeepgramLang         string
	DeepgramAPIKey       string // secret — only used to report set/unset
	ExitKeywords         string
	GoodbyeText          string
	UserAwayTimeout      string // raw string — display only
	EndpointMinDelay     string // raw string — display only
	EndpointMaxDelay     string // raw string — display only
	AllowInterruptions   bool
	PreemptiveGeneration bool

	SentryDSN         string // Sentry project DSN; empty = error tracking disabled
	SentryEnvironment string // Sentry environment tag (default "development")

	// Sentry tracing/replay sample rates. Defaults are environment-aware:
	// development samples everything (1.0); other environments use production
	// rates. Overridable via SENTRY_TRACES_SAMPLE_RATE /
	// SENTRY_REPLAY_SESSION_SAMPLE_RATE / SENTRY_REPLAY_ON_ERROR_SAMPLE_RATE.
	SentryTracesSampleRate        float64
	SentryReplaySessionSampleRate float64
	SentryReplayOnErrorSampleRate float64

	// GitHub webhook ingress (see webhook_github.go). GitHubWebhookSecret is
	// the HMAC-SHA256 key used to verify X-Hub-Signature-256; empty disables
	// the endpoint (503). GitHubReviewAgentID is the Memory runtime agent id
	// triggered for relevant pull_request events; empty = log + no-op.
	// GitHubReviewRepos is a comma-separated owner/repo allowlist; empty =
	// ignore every event.
	GitHubWebhookSecret string
	GitHubReviewAgentID string
	GitHubReviewRepos   string
}

func LoadConfig() Config {
	env := envOr("SENTRY_ENVIRONMENT", "development")
	tracesRate, replayRate := 0.2, 0.1
	if env == "development" {
		tracesRate, replayRate = 1.0, 1.0
	}
	return Config{
		Port:               envOr("MEMORY_PORT", "8095"),
		MemoryURL:          envOr("MEMORY_URL", "https://memory.emergent-company.ai"),
		MemoryToken:        os.Getenv("MEMORY_TOKEN"),
		MemoryProjectID:    os.Getenv("MEMORY_PROJECT_ID"),
		ClientAPIKey:       os.Getenv("TOKEN_API_KEY"),
		BridgeBin:          envOr("BRIDGE_BIN", "python"),
		BridgeArgs:         strings.Fields(envOr("BRIDGE_ARGS", "-m memory_bridge start")),
		BridgeWorkdir:      os.Getenv("BRIDGE_WORKDIR"),
		SupervisorInterval: durationOr("SUPERVISOR_INTERVAL", 5*time.Second),
		BlueprintsDir:      os.Getenv("BLUEPRINTS_DIR"), // empty = use embedded packs

		LiveKitAPIKey:     os.Getenv("LIVEKIT_API_KEY"),
		LiveKitAPISecret:  os.Getenv("LIVEKIT_API_SECRET"),
		LiveKitURL:        envOr("LIVEKIT_URL", "ws://localhost:7880"),
		LiveKitPublicURL:  os.Getenv("LIVEKIT_PUBLIC_URL"),
		TokenAllowedRooms: os.Getenv("TOKEN_ALLOWED_ROOMS"),
		DefaultAgent:      envOr("LIVEKIT_AGENT_NAME", "memory"),
		PublicBaseURL:     os.Getenv("PUBLIC_BASE_URL"),
		TTSProvider:       envOr("TTS_PROVIDER", "cartesia"),

		ZitadelIssuer:      os.Getenv("ZITADEL_ISSUER"),
		ZitadelClientID:    os.Getenv("ZITADEL_CLIENT_ID"),
		ZitadelRedirectURI: os.Getenv("ZITADEL_REDIRECT_URI"),
		AuthMode:           envOr("AUTH_MODE", "session"),
		SessionSecret:      os.Getenv("SESSION_SECRET"),
		SessionMaxAge:      durationOr("SESSION_MAX_AGE", 30*24*time.Hour),
		WorkerInternalKey:  os.Getenv("WORKER_INTERNAL_KEY"),

		CartesiaModel:        envOr("CARTESIA_MODEL", "sonic-3.5"),
		CartesiaVoice:        envOr("CARTESIA_VOICE", "9626c31c-bec5-4cca-baa8-f8ba9e84c8bc"),
		CartesiaAPIKey:       os.Getenv("CARTESIA_API_KEY"),
		DeepgramModel:        envOr("DEEPGRAM_MODEL", "nova-3"),
		DeepgramLang:         envOr("DEEPGRAM_LANG", "multi"),
		DeepgramAPIKey:       os.Getenv("DEEPGRAM_API_KEY"),
		ExitKeywords:         envOr("EXIT_KEYWORDS", "stop,goodbye,end,that's all,do widzenia,koniec"),
		GoodbyeText:          envOr("GOODBYE_TEXT", "Goodbye."),
		UserAwayTimeout:      envOr("USER_AWAY_TIMEOUT", "20"),
		EndpointMinDelay:     envOr("ENDPOINT_MIN_DELAY", "0.4"),
		EndpointMaxDelay:     envOr("ENDPOINT_MAX_DELAY", "3"),
		AllowInterruptions:   envBoolOr("ALLOW_INTERRUPTIONS", true),
		PreemptiveGeneration: envBoolOr("PREEMPTIVE_GENERATION", true),

		SentryDSN:                     os.Getenv("SENTRY_DSN"),
		SentryEnvironment:             env,
		SentryTracesSampleRate:        floatOr("SENTRY_TRACES_SAMPLE_RATE", tracesRate),
		SentryReplaySessionSampleRate: floatOr("SENTRY_REPLAY_SESSION_SAMPLE_RATE", replayRate),
		SentryReplayOnErrorSampleRate: floatOr("SENTRY_REPLAY_ON_ERROR_SAMPLE_RATE", 1.0),

		GitHubWebhookSecret: os.Getenv("GITHUB_WEBHOOK_SECRET"),
		GitHubReviewAgentID: os.Getenv("GITHUB_REVIEW_AGENT_ID"),
		GitHubReviewRepos:   os.Getenv("GITHUB_REVIEW_REPOS"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func durationOr(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("config: ignoring invalid %s=%q: %v", key, v, err)
		return fallback
	}
	return d
}

// envBoolOr reads a boolean env var ("1"/"true"/"yes"/"on" = true, case-
// insensitive), matching the worker's memory_bridge/config.py _env_bool.
func envBoolOr(key string, fallback bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	return strings.Contains(" 1 true yes on ", " "+strings.ToLower(strings.TrimSpace(raw))+" ")
}

// floatOr reads a float env var in [0,1] (Sentry sample rates); an invalid or
// out-of-range value falls back to the default.
func floatOr(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			return f
		}
	}
	return fallback
}

// Validate fails fast on an auth posture that cannot be safely served. The
// default posture is "session"; "dev" must be chosen explicitly and is warned
// about at startup.
func (c Config) Validate() error {
	switch c.AuthMode {
	case "session":
		var missing []string
		if c.SessionSecret == "" {
			missing = append(missing, "SESSION_SECRET")
		}
		if c.ZitadelIssuer == "" {
			missing = append(missing, "ZITADEL_ISSUER")
		}
		if c.ZitadelClientID == "" {
			missing = append(missing, "ZITADEL_CLIENT_ID")
		}
		if c.ZitadelRedirectURI == "" {
			missing = append(missing, "ZITADEL_REDIRECT_URI")
		}
		if c.PublicBaseURL == "" {
			missing = append(missing, "PUBLIC_BASE_URL")
		}
		if c.MemoryURL == "" {
			missing = append(missing, "MEMORY_URL")
		}
		if len(missing) > 0 {
			return fmt.Errorf("AUTH_MODE=session requires: %s", strings.Join(missing, ", "))
		}
		if len(c.SessionSecret) < 32 {
			log.Printf("WARNING: SESSION_SECRET is shorter than 32 bytes; use a longer random value")
		}
		if !strings.HasPrefix(c.PublicBaseURL, "https://") {
			if c.SentryEnvironment == "development" {
				log.Printf("WARNING: PUBLIC_BASE_URL=%q is not https; acceptable in development only", c.PublicBaseURL)
			} else {
				return fmt.Errorf("AUTH_MODE=session requires an https PUBLIC_BASE_URL in %q (got %q)", c.SentryEnvironment, c.PublicBaseURL)
			}
		}
	case "dev":
		// Explicit insecure opt-in; caller logs a warning.
	default:
		return fmt.Errorf("unknown AUTH_MODE %q (want \"session\" or \"dev\")", c.AuthMode)
	}
	return nil
}
