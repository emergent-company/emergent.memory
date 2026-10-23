package config

import (
	"fmt"
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
	AutoUpdate   AutoUpdateConfig `mapstructure:"auto_update" yaml:"auto_update"`
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

type AutoUpdateConfig struct {
	Enabled       bool   `mapstructure:"enabled" yaml:"enabled"`
	Mode          string `mapstructure:"mode" yaml:"mode"`                     // "notify" or "auto"
	CheckInterval string `mapstructure:"check_interval" yaml:"check_interval"` // e.g. "24h"
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

// configFileMode is the mode Save creates a config file with. config.yaml can
// hold an account API key (`memory config set api_key`), so it must never be
// created world-readable.
const configFileMode os.FileMode = 0o600

// Save writes cfg to path.
//
// os.WriteFile applies its perm argument only when it creates the file, so an
// existing file keeps its mode. Creating at 0600 covers the credential case;
// what to do about an over-permissive existing file depends on who owns it:
//
//   - The default path (~/.memory/config.yaml) is per-user by construction, so
//     an existing file that is wider than 0600 is tightened before the write
//     (tighten first, so a new key is never briefly readable at the old mode).
//   - An explicit --config / MEMORY_CONFIG path may be a file shared between
//     users (e.g. a shared admin location). Forcing 0600 on every save there
//     would lock the other users out, so its existing mode is left alone; the
//     owner can chmod it if it should be restrictive.
func Save(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	if isDefaultPath(path) {
		if err := tightenExistingFile(path, configFileMode); err != nil {
			return err
		}
	}

	return os.WriteFile(path, data, configFileMode)
}

// tightenExistingFile forces path to mode when it already exists and is a
// regular file. A symlink is left alone: chmod would follow it and change the
// mode of a target that may belong to someone else.
func tightenExistingFile(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	if info.Mode().Perm()&0o077 == 0 {
		return nil
	}
	if err := os.Chmod(path, mode); err != nil {
		// Fail closed: writing a credential into a file we failed to tighten
		// would leave it readable at its previous mode.
		return fmt.Errorf("failed to tighten permissions on %s: %w", path, err)
	}
	return nil
}

// isDefaultPath reports whether path is the default config path,
// ~/.memory/config.yaml.
//
// Both sides are made absolute and cleaned before comparison rather than
// compared as strings, so "." / ".." segments and a relative path still match.
// Symlinks in the path are deliberately not resolved (no EvalSymlinks): when the
// home directory is itself reached through a symlink, resolving the argument
// would stop it matching the default path we compute, and the file would
// silently stay world-readable.
func isDefaultPath(path string) bool {
	defaultPath, ok := defaultConfigPath()
	if !ok {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absDefault, err := filepath.Abs(defaultPath)
	if err != nil {
		return false
	}
	return filepath.Clean(absPath) == filepath.Clean(absDefault)
}

func defaults() *Config {
	return &Config{
		ServerURL: "https://memory.emergent-company.ai",
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
		AutoUpdate: AutoUpdateConfig{
			Enabled:       true,
			Mode:          "notify",
			CheckInterval: "24h",
		},
	}
}

func DiscoverPath(flagPath string) string {
	if flagPath != "" {
		if _, err := os.Stat(flagPath); err == nil {
			return flagPath
		}
	}

	if envPath := os.Getenv("MEMORY_CONFIG"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
	}

	defaultPath, ok := defaultConfigPath()
	if !ok {
		return ".memory/config.yaml"
	}
	return defaultPath
}

// defaultConfigPath returns the default config path, ~/.memory/config.yaml.
// ok is false when the home directory cannot be determined.
func defaultConfigPath() (string, bool) {
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		return "", false
	}
	return filepath.Join(homeDir, ".memory", "config.yaml"), true
}

func LoadWithEnv(path string) (*Config, error) {
	v := viper.New()

	v.SetEnvPrefix("MEMORY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	_ = v.BindEnv("server_url")
	_ = v.BindEnv("api_key", "MEMORY_ACCOUNT_API_KEY", "MEMORY_API_KEY")
	_ = v.BindEnv("email")
	_ = v.BindEnv("org_id")
	_ = v.BindEnv("project_id")
	_ = v.BindEnv("project_token", "MEMORY_PROJECT_API_KEY", "MEMORY_PROJECT_TOKEN")
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
	_ = v.BindEnv("auto_update.enabled")
	_ = v.BindEnv("auto_update.mode")
	_ = v.BindEnv("auto_update.check_interval")

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

	// MEMORY_PROJECT holds a project name or slug and is used to scope the
	// CLI to a single project directory-wide. It is resolved to a project ID
	// at runtime — it is NOT a token. For a project-scoped API token use
	// MEMORY_PROJECT_API_KEY (emt_... value) instead.
	if cfg.ProjectID == "" {
		if name := os.Getenv("MEMORY_PROJECT"); name != "" {
			// Store the name in ProjectName so callers can display it before
			// the ID has been resolved (resolution happens in root.go).
			if cfg.ProjectName == "" {
				cfg.ProjectName = name
			}
		}
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
	if cfg.AutoUpdate.CheckInterval == "" {
		cfg.AutoUpdate = defaults().AutoUpdate
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
