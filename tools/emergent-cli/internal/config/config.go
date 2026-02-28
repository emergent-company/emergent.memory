package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type Config struct {
	ServerURL    string           `mapstructure:"server_url" yaml:"server_url"`
	APIKey       string           `mapstructure:"api_key" yaml:"api_key"`
	Email        string           `mapstructure:"email" yaml:"email"`
	OrgID        string           `mapstructure:"org_id" yaml:"org_id"`
	ProjectID    string           `mapstructure:"project_id" yaml:"project_id"`
	ProjectToken string           `mapstructure:"project_token" yaml:"project_token"`
	ProjectName  string           `mapstructure:"project_name" yaml:"project_name"`
	Debug        bool             `mapstructure:"debug" yaml:"debug"`
	Cache        CacheConfig      `mapstructure:"cache" yaml:"cache"`
	UI           UIConfig         `mapstructure:"ui" yaml:"ui"`
	Query        QueryConfig      `mapstructure:"query" yaml:"query"`
	Completion   CompletionConfig `mapstructure:"completion" yaml:"completion"`
}

type CacheConfig struct {
	TTL     string `mapstructure:"ttl" yaml:"ttl"`
	Enabled bool   `mapstructure:"enabled" yaml:"enabled"`
}

type UIConfig struct {
	Compact bool   `mapstructure:"compact" yaml:"compact"`
	Color   string `mapstructure:"color" yaml:"color"` // auto, always, never
	Pager   bool   `mapstructure:"pager" yaml:"pager"`
}

type QueryConfig struct {
	DefaultLimit int    `mapstructure:"default_limit" yaml:"default_limit"`
	DefaultSort  string `mapstructure:"default_sort" yaml:"default_sort"`
}

type CompletionConfig struct {
	Timeout string `mapstructure:"timeout" yaml:"timeout"`
}

func Load(path string) (*Config, error) {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return defaults(), nil
	}
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func Save(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func defaults() *Config {
	return &Config{
		ServerURL: "http://localhost:3002",
		Email:     "",
		OrgID:     "",
		ProjectID: "",
		Debug:     false,
		Cache: CacheConfig{
			TTL:     "5m",
			Enabled: true,
		},
		UI: UIConfig{
			Compact: false,
			Color:   "auto",
			Pager:   true,
		},
		Query: QueryConfig{
			DefaultLimit: 50,
			DefaultSort:  "updated_at:desc",
		},
		Completion: CompletionConfig{
			Timeout: "2s",
		},
	}
}

func DiscoverPath(flagPath string) string {
	if flagPath != "" {
		if _, err := os.Stat(flagPath); err == nil {
			return flagPath
		}
	}

	if envPath := os.Getenv("EMERGENT_CONFIG"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ".emergent/config.yaml"
	}

	defaultPath := filepath.Join(homeDir, ".emergent", "config.yaml")
	return defaultPath
}

func LoadWithEnv(path string) (*Config, error) {
	v := viper.New()

	v.SetEnvPrefix("EMERGENT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	_ = v.BindEnv("server_url")
	_ = v.BindEnv("api_key")
	_ = v.BindEnv("email")
	_ = v.BindEnv("org_id")
	_ = v.BindEnv("project_id")
	_ = v.BindEnv("project_token")
	_ = v.BindEnv("project_name")
	_ = v.BindEnv("debug")
	_ = v.BindEnv("cache.ttl")
	_ = v.BindEnv("cache.enabled")
	_ = v.BindEnv("ui.compact")
	_ = v.BindEnv("ui.color")
	_ = v.BindEnv("ui.pager")
	_ = v.BindEnv("query.default_limit")
	_ = v.BindEnv("query.default_sort")
	_ = v.BindEnv("completion.timeout")

	_, err := os.Stat(path)
	if err == nil {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, err
		}
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}

	if cfg.ServerURL == "" {
		cfg.ServerURL = defaults().ServerURL
	}

	// Apply defaults for new fields if not set
	if cfg.Cache.TTL == "" {
		cfg.Cache = defaults().Cache
	}
	if cfg.UI.Color == "" {
		cfg.UI = defaults().UI
	}
	if cfg.Query.DefaultLimit == 0 {
		cfg.Query = defaults().Query
	}
	if cfg.Completion.Timeout == "" {
		cfg.Completion = defaults().Completion
	}

	return cfg, nil
}

// GetCacheTTL returns the cache TTL as a duration.
func GetCacheTTL() time.Duration {
	ttlStr := viper.GetString("cache.ttl")
	if ttlStr == "" {
		ttlStr = "5m"
	}
	ttl, err := time.ParseDuration(ttlStr)
	if err != nil {
		return 5 * time.Minute
	}
	return ttl
}

// GetCompletionTimeout returns the completion timeout as a duration.
func GetCompletionTimeout() time.Duration {
	timeoutStr := viper.GetString("completion.timeout")
	if timeoutStr == "" {
		timeoutStr = "2s"
	}
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return 2 * time.Second
	}
	return timeout
}

// GetDefaultLimit returns the default query limit.
func GetDefaultLimit() int {
	limit := viper.GetInt("query.default_limit")
	if limit == 0 {
		return 50
	}
	return limit
}

// GetDefaultSort returns the default sort order.
func GetDefaultSort() string {
	sort := viper.GetString("query.default_sort")
	if sort == "" {
		return "updated_at:desc"
	}
	return sort
}

// ShouldUseColor determines if color output should be used.
func ShouldUseColor(noColorFlag bool) bool {
	if noColorFlag {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	colorMode := viper.GetString("ui.color")
	switch colorMode {
	case "never":
		return false
	case "always":
		return true
	case "auto":
		return true
	default:
		return true
	}
}

// ShouldUseCompact determines if compact output should be used.
func ShouldUseCompact() bool {
	return viper.GetBool("ui.compact")
}

// ShouldUsePager determines if pager should be used.
func ShouldUsePager() bool {
	return viper.GetBool("ui.pager")
}

// IsCacheEnabled returns true if completion caching is enabled.
func IsCacheEnabled() bool {
	return viper.GetBool("cache.enabled")
}
