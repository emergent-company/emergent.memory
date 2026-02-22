package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateSecret(t *testing.T) {
	tests := []struct {
		name  string
		bytes int
	}{
		{"16 bytes", 16},
		{"32 bytes", 32},
		{"64 bytes", 64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret, err := GenerateSecret(tt.bytes)
			if err != nil {
				t.Fatalf("GenerateSecret(%d) returned error: %v", tt.bytes, err)
			}

			expectedLen := tt.bytes * 2
			if len(secret) != expectedLen {
				t.Errorf("expected length %d, got %d", expectedLen, len(secret))
			}

			for _, c := range secret {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
					t.Errorf("unexpected character in hex string: %c", c)
				}
			}
		})
	}
}

func TestGenerateSecretUniqueness(t *testing.T) {
	secrets := make(map[string]bool)
	for i := 0; i < 100; i++ {
		secret, err := GenerateSecret(32)
		if err != nil {
			t.Fatalf("GenerateSecret failed: %v", err)
		}
		if secrets[secret] {
			t.Error("generated duplicate secret")
		}
		secrets[secret] = true
	}
}

func TestNewInstaller(t *testing.T) {
	cfg := Config{
		InstallDir:   "/tmp/test-emergent",
		ServerPort:   8080,
		GoogleAPIKey: "test-key",
		SkipStart:    true,
		Force:        false,
	}

	inst := New(cfg)
	if inst == nil {
		t.Fatal("New() returned nil")
	}

	if inst.config.InstallDir != cfg.InstallDir {
		t.Errorf("expected InstallDir %s, got %s", cfg.InstallDir, inst.config.InstallDir)
	}
	if inst.config.ServerPort != cfg.ServerPort {
		t.Errorf("expected ServerPort %d, got %d", cfg.ServerPort, inst.config.ServerPort)
	}
}

func TestCreateDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	installDir := filepath.Join(tmpDir, "emergent")

	cfg := Config{InstallDir: installDir}
	inst := New(cfg)

	if err := inst.CreateDirectories(); err != nil {
		t.Fatalf("CreateDirectories failed: %v", err)
	}

	expectedDirs := []string{
		filepath.Join(installDir, "bin"),
		filepath.Join(installDir, "config"),
		filepath.Join(installDir, "docker"),
	}

	for _, dir := range expectedDirs {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("directory %s not created: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
	}
}

func TestIsInstalled(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("not installed", func(t *testing.T) {
		cfg := Config{InstallDir: tmpDir}
		inst := New(cfg)
		if inst.IsInstalled() {
			t.Error("expected IsInstalled to return false for empty directory")
		}
	})

	t.Run("installed", func(t *testing.T) {
		dockerDir := filepath.Join(tmpDir, "docker")
		if err := os.MkdirAll(dockerDir, 0755); err != nil {
			t.Fatal(err)
		}
		composePath := filepath.Join(dockerDir, "docker-compose.yml")
		if err := os.WriteFile(composePath, []byte("version: '3'"), 0644); err != nil {
			t.Fatal(err)
		}

		cfg := Config{InstallDir: tmpDir}
		inst := New(cfg)
		if !inst.IsInstalled() {
			t.Error("expected IsInstalled to return true when docker-compose.yml exists")
		}
	})
}

func TestGenerateEnvFile(t *testing.T) {
	tmpDir := t.TempDir()
	installDir := filepath.Join(tmpDir, "emergent")

	cfg := Config{
		InstallDir:   installDir,
		ServerPort:   3002,
		GoogleAPIKey: "test-google-key",
	}
	inst := New(cfg)

	if err := inst.CreateDirectories(); err != nil {
		t.Fatal(err)
	}

	apiKey, err := inst.GenerateEnvFile()
	if err != nil {
		t.Fatalf("GenerateEnvFile failed: %v", err)
	}

	if len(apiKey) != 64 {
		t.Errorf("expected API key length 64, got %d", len(apiKey))
	}

	envPath := filepath.Join(installDir, "config", ".env.local")
	content, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("failed to read env file: %v", err)
	}

	envContent := string(content)
	checks := []string{
		"POSTGRES_USER=emergent",
		"STANDALONE_MODE=true",
		"SERVER_PORT=3002",
		"GOOGLE_API_KEY=test-google-key",
	}

	for _, check := range checks {
		if !strings.Contains(envContent, check) {
			t.Errorf("env file missing: %s", check)
		}
	}
}

func TestWriteDockerCompose(t *testing.T) {
	tmpDir := t.TempDir()
	installDir := filepath.Join(tmpDir, "emergent")

	cfg := Config{InstallDir: installDir}
	inst := New(cfg)

	if err := inst.CreateDirectories(); err != nil {
		t.Fatal(err)
	}

	if err := inst.WriteDockerCompose(); err != nil {
		t.Fatalf("WriteDockerCompose failed: %v", err)
	}

	composePath := filepath.Join(installDir, "docker", "docker-compose.yml")
	content, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("failed to read docker-compose.yml: %v", err)
	}

	composeContent := string(content)
	checks := []string{
		"services:",
		"emergent-server",
		"emergent-db",
		"emergent-minio",
		"emergent-kreuzberg",
		"pgvector/pgvector:pg17",
	}

	for _, check := range checks {
		if !strings.Contains(composeContent, check) {
			t.Errorf("docker-compose.yml missing: %s", check)
		}
	}
}

func TestWriteInitSQL(t *testing.T) {
	tmpDir := t.TempDir()
	installDir := filepath.Join(tmpDir, "emergent")

	cfg := Config{InstallDir: installDir}
	inst := New(cfg)

	if err := inst.CreateDirectories(); err != nil {
		t.Fatal(err)
	}

	if err := inst.WriteInitSQL(); err != nil {
		t.Fatalf("WriteInitSQL failed: %v", err)
	}

	initPath := filepath.Join(installDir, "docker", "init.sql")
	content, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("failed to read init.sql: %v", err)
	}

	sqlContent := string(content)
	checks := []string{
		"CREATE EXTENSION IF NOT EXISTS vector",
		"CREATE EXTENSION IF NOT EXISTS pgcrypto",
		"app_rls",
	}

	for _, check := range checks {
		if !strings.Contains(sqlContent, check) {
			t.Errorf("init.sql missing: %s", check)
		}
	}
}

func TestWriteConfigYAML(t *testing.T) {
	tmpDir := t.TempDir()
	installDir := filepath.Join(tmpDir, "emergent")

	cfg := Config{
		InstallDir: installDir,
		ServerPort: 8080,
	}
	inst := New(cfg)

	if err := inst.CreateDirectories(); err != nil {
		t.Fatal(err)
	}

	apiKey := "test-api-key-12345"
	if err := inst.WriteConfigYAML(apiKey); err != nil {
		t.Fatalf("WriteConfigYAML failed: %v", err)
	}

	configPath := filepath.Join(installDir, "config.yaml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config.yaml: %v", err)
	}

	configContent := string(content)
	if !strings.Contains(configContent, "server_url: http://localhost:8080") {
		t.Error("config.yaml missing server_url")
	}
	if !strings.Contains(configContent, "api_key: test-api-key-12345") {
		t.Error("config.yaml missing api_key")
	}
}

func TestGetServerPort(t *testing.T) {
	tmpDir := t.TempDir()
	installDir := filepath.Join(tmpDir, "emergent")
	configDir := filepath.Join(installDir, "config")

	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	t.Run("default when no env file", func(t *testing.T) {
		cfg := Config{InstallDir: installDir}
		inst := New(cfg)
		port := inst.GetServerPort()
		if port != 3002 {
			t.Errorf("expected default port 3002, got %d", port)
		}
	})

	t.Run("reads from env file", func(t *testing.T) {
		envPath := filepath.Join(configDir, ".env.local")
		envContent := "SOME_VAR=value\nSERVER_PORT=9999\nOTHER=thing\n"
		if err := os.WriteFile(envPath, []byte(envContent), 0644); err != nil {
			t.Fatal(err)
		}

		cfg := Config{InstallDir: installDir}
		inst := New(cfg)
		port := inst.GetServerPort()
		if port != 9999 {
			t.Errorf("expected port 9999, got %d", port)
		}
	})
}

func TestGetEnvPath(t *testing.T) {
	cfg := Config{InstallDir: "/home/user/.emergent"}
	inst := New(cfg)

	expected := "/home/user/.emergent/config/.env.local"
	if got := inst.GetEnvPath(); got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}

type mockOutput struct {
	infos     []string
	successes []string
	warns     []string
	errors    []string
	steps     []string
}

func (m *mockOutput) Info(format string, args ...interface{}) { m.infos = append(m.infos, format) }
func (m *mockOutput) Success(format string, args ...interface{}) {
	m.successes = append(m.successes, format)
}
func (m *mockOutput) Warn(format string, args ...interface{})  { m.warns = append(m.warns, format) }
func (m *mockOutput) Error(format string, args ...interface{}) { m.errors = append(m.errors, format) }
func (m *mockOutput) Step(format string, args ...interface{})  { m.steps = append(m.steps, format) }

func TestSetOutput(t *testing.T) {
	cfg := Config{InstallDir: "/tmp/test"}
	inst := New(cfg)

	mock := &mockOutput{}
	inst.SetOutput(mock)

	if inst.output != mock {
		t.Error("SetOutput did not set the output writer")
	}
}
