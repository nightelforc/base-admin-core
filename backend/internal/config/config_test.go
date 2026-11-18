package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadLayering(t *testing.T) {
	path := writeConfig(t, `
app:
  name: yaml-app
http:
  request_timeout: 12s
`)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("APP_NAME", "env-app")
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.App.Name != "env-app" {
		t.Fatalf("APP_NAME = %q, want env-app", cfg.App.Name)
	}
	if cfg.HTTP.RequestTimeout != 12*time.Second {
		t.Fatalf("HTTP_REQUEST_TIMEOUT = %s, want 12s", cfg.HTTP.RequestTimeout)
	}
	if cfg.HTTP.ReadTimeout != 30*time.Second {
		t.Fatalf("HTTP_READ_TIMEOUT = %s, want default 30s", cfg.HTTP.ReadTimeout)
	}
	if cfg.Source() != path {
		t.Fatalf("Source() = %q, want %q", cfg.Source(), path)
	}
}

func TestLoadEnvironmentZeroWinsOverDefault(t *testing.T) {
	path := writeConfig(t, `
http:
  request_timeout: 12s
`)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("HTTP_REQUEST_TIMEOUT", "0s")
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTP.RequestTimeout != 0 {
		t.Fatalf("HTTP_REQUEST_TIMEOUT = %s, want 0s", cfg.HTTP.RequestTimeout)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := writeConfig(t, `
app:
  nmae: typo
`)
	t.Setenv(ConfigFileEnv, path)

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "nmae") {
		t.Fatalf("Load error = %v, want unknown field error", err)
	}
}

func TestLoadRejectsExampleJWTSecretInProduction(t *testing.T) {
	path := writeConfig(t, `
app:
  env: production
jwt:
  secret: change-me-to-a-random-secret-at-least-32-chars
`)
	t.Setenv(ConfigFileEnv, path)

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("Load error = %v, want JWT_SECRET rejection", err)
	}
}

func TestLoadValidatesRequestTimeout(t *testing.T) {
	path := writeConfig(t, `
http:
  request_timeout: -1s
`)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "HTTP_REQUEST_TIMEOUT") {
		t.Fatalf("Load error = %v, want HTTP_REQUEST_TIMEOUT validation error", err)
	}
}

func TestRepositoryConfigFile(t *testing.T) {
	path := filepath.Join("..", "..", "config.yaml")
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(%s): %v", path, err)
	}
	if cfg.HTTP.RequestTimeout != 30*time.Second {
		t.Fatalf("request_timeout = %s, want 30s", cfg.HTTP.RequestTimeout)
	}
	if cfg.Security.OperationLogBodyLimit != 2048 {
		t.Fatalf("operation_log_body_limit = %d, want 2048", cfg.Security.OperationLogBodyLimit)
	}
}

func TestLoadRequiresConfigFile(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(ConfigFileEnv, "")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "config file not found") {
		t.Fatalf("Load error = %v, want config file not found", err)
	}
}
