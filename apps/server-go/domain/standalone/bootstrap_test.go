package standalone

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent/emergent-core/internal/config"
)

func TestBootstrapService_Initialize_Disabled(t *testing.T) {
	os.Setenv("STANDALONE_MODE", "false")
	defer os.Unsetenv("STANDALONE_MODE")

	log := slog.Default()
	cfg, err := config.NewConfig(log)
	require.NoError(t, err)

	bs := &BootstrapService{
		cfg: cfg,
		log: log,
	}

	err = bs.Initialize(context.Background())
	assert.NoError(t, err)
}

func TestBootstrapService_IsEnabled(t *testing.T) {
	tests := []struct {
		name           string
		standaloneMode string
		want           bool
	}{
		{
			name:           "disabled",
			standaloneMode: "false",
			want:           false,
		},
		{
			name:           "enabled",
			standaloneMode: "true",
			want:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("STANDALONE_MODE", tt.standaloneMode)
			defer os.Unsetenv("STANDALONE_MODE")

			log := slog.Default()
			cfg, err := config.NewConfig(log)
			require.NoError(t, err)

			got := cfg.Standalone.IsEnabled()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBootstrapService_IsConfigured(t *testing.T) {
	tests := []struct {
		name           string
		standaloneMode string
		apiKey         string
		userEmail      string
		want           bool
	}{
		{
			name:           "disabled",
			standaloneMode: "false",
			apiKey:         "test-key",
			userEmail:      "admin@localhost",
			want:           false,
		},
		{
			name:           "enabled but no API key",
			standaloneMode: "true",
			apiKey:         "",
			userEmail:      "admin@localhost",
			want:           false,
		},
		{
			name:           "enabled with API key",
			standaloneMode: "true",
			apiKey:         "test-key",
			userEmail:      "admin@localhost",
			want:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("STANDALONE_MODE", tt.standaloneMode)
			os.Setenv("STANDALONE_API_KEY", tt.apiKey)
			os.Setenv("STANDALONE_USER_EMAIL", tt.userEmail)
			defer func() {
				os.Unsetenv("STANDALONE_MODE")
				os.Unsetenv("STANDALONE_API_KEY")
				os.Unsetenv("STANDALONE_USER_EMAIL")
			}()

			log := slog.Default()
			cfg, err := config.NewConfig(log)
			require.NoError(t, err)

			got := cfg.Standalone.IsConfigured()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBootstrapService_DefaultValues(t *testing.T) {
	os.Setenv("STANDALONE_MODE", "true")
	os.Setenv("STANDALONE_API_KEY", "test-key")
	defer func() {
		os.Unsetenv("STANDALONE_MODE")
		os.Unsetenv("STANDALONE_API_KEY")
	}()

	log := slog.Default()
	cfg, err := config.NewConfig(log)
	require.NoError(t, err)

	assert.Equal(t, "admin@localhost", cfg.Standalone.UserEmail)
	assert.Equal(t, "Default Organization", cfg.Standalone.OrgName)
	assert.Equal(t, "Default Project", cfg.Standalone.ProjectName)
}
