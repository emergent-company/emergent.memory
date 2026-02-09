package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type Config struct {
	ServerURL string `mapstructure:"server_url" yaml:"server_url"`
	APIKey    string `mapstructure:"api_key" yaml:"api_key"`
	Email     string `mapstructure:"email" yaml:"email"`
	OrgID     string `mapstructure:"org_id" yaml:"org_id"`
	ProjectID string `mapstructure:"project_id" yaml:"project_id"`
	Debug     bool   `mapstructure:"debug" yaml:"debug"`
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
	_ = v.BindEnv("debug")

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

	return cfg, nil
}
