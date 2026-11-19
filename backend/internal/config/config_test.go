package config

import (
	"fmt"
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

func TestHasPlaceholderSecret(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"change-me", true},
		{"CHANGE-ME", true},
		{"placeholder-value", true},
		{"example-secret", true},
		{"replace-me", true},
		{"<JWT_SECRET>", true},
		{" <DB_PASSWORD> ", true},
		{"${DB_PASSWORD}", true},
		{"${JWT_SECRET}", true},
		{"", false},
		{strings.Repeat("a", 32), false},
	}
	for _, tc := range cases {
		if got := hasPlaceholderSecret(tc.value); got != tc.want {
			t.Errorf("hasPlaceholderSecret(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestProductionRejectsAngleBracketPassword(t *testing.T) {
	path := writeConfig(t, "app:\n  env: production\njwt:\n  secret: "+strings.Repeat("a", 32)+"\n")
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("DB_PASSWORD", "<DB_PASSWORD>")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "DB_PASSWORD") {
		t.Fatalf("Load error = %v, want DB_PASSWORD placeholder rejection", err)
	}
}

func TestEnvironmentConstantsMatchAllowedValues(t *testing.T) {
	if EnvDevelopment != "development" || EnvTest != "test" || EnvProduction != "production" {
		t.Fatalf("environment constants = %q/%q/%q, want development/test/production",
			EnvDevelopment, EnvTest, EnvProduction)
	}
	if !(App{Env: EnvProduction}).IsProduction() {
		t.Fatal("production environment must be detected as production")
	}
	if (App{Env: EnvTest}).IsProduction() || (App{Env: EnvDevelopment}).IsProduction() {
		t.Fatal("test/development environments must not be treated as production")
	}
}

func TestNonProductionSkipsProductionPolicy(t *testing.T) {
	for _, env := range []string{EnvDevelopment, EnvTest} {
		t.Run(env, func(t *testing.T) {
			path := writeConfig(t, fmt.Sprintf(`
app:
  env: %s
  debug: true
jwt:
  secret: change-me-to-a-random-secret-at-least-32-chars
`, env))
			t.Setenv(ConfigFileEnv, path)
			t.Setenv("CORS_ALLOW_CREDENTIALS", "true")
			t.Setenv("CORS_ALLOWED_ORIGINS", "*")
			t.Setenv("DB_PASSWORD", "change-me")
			t.Setenv("REDIS_PASSWORD", "change-me")
			t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "change-me")

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load in %s environment must not apply production policy: %v", env, err)
			}
			if cfg.App.Env != env {
				t.Fatalf("APP_ENV = %q, want %q", cfg.App.Env, env)
			}
		})
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

func TestLoadAggregatesComponentValidationProblems(t *testing.T) {
	path := writeConfig(t, `
app:
  timezone: Mars/Base
redis:
  key_prefix: ""
jwt:
  access_ttl: 1m
  refresh_ttl: 30s
security:
  login_window: 0s
  login_block_duration: 0s
`)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))

	_, err := Load()
	if err == nil {
		t.Fatal("Load succeeded, want aggregated validation error")
	}

	// 根 Validate 只负责编排，每个组件的 validate() 都应把问题汇总到同一份错误里。
	for _, want := range []string{
		"APP_TIMEZONE",
		"REDIS_KEY_PREFIX",
		"JWT_REFRESH_TTL",
		"SECURITY_LOGIN_WINDOW",
		"SECURITY_LOGIN_BLOCK_DURATION",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Load error = %v, want it to mention %s", err, want)
		}
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

func TestLoadSentinelModeRequiresMasterName(t *testing.T) {
	path := writeConfig(t, `
redis:
  mode: sentinel
  addrs: ["127.0.0.1:26379"]
`)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "REDIS_MASTER_NAME") {
		t.Fatalf("Load error = %v, want REDIS_MASTER_NAME rejection", err)
	}
}

func TestLoadSentinelModeRequiresAddrs(t *testing.T) {
	path := writeConfig(t, `
redis:
  mode: sentinel
  master_name: mymaster
  addrs: []
`)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "REDIS_ADDRS") {
		t.Fatalf("Load error = %v, want REDIS_ADDRS rejection", err)
	}
}

func TestLoadStandaloneRejectsMultipleAddrs(t *testing.T) {
	path := writeConfig(t, `
redis:
  addrs: ["127.0.0.1:6379", "127.0.0.1:6380"]
`)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "exactly one address") {
		t.Fatalf("Load error = %v, want single-address rejection", err)
	}
}

func TestLoadAcceptsSentinelMode(t *testing.T) {
	path := writeConfig(t, `
redis:
  mode: sentinel
  master_name: mymaster
  addrs: ["10.0.0.11:26379", "10.0.0.12:26379"]
  sentinel_password: sentinel-secret
`)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Redis.Addrs) != 2 {
		t.Fatalf("addrs = %v, want 2 addresses", cfg.Redis.Addrs)
	}

	opts := cfg.Redis.Options()
	if opts.Mode != "sentinel" || opts.MasterName != "mymaster" || len(opts.Addrs) != 2 {
		t.Fatalf("Options() = %+v, want sentinel mapping", opts)
	}
}

func TestLoadRedisAddrsFromEnvironment(t *testing.T) {
	path := writeConfig(t, `
redis:
  mode: sentinel
  master_name: mymaster
`)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))
	t.Setenv("REDIS_ADDRS", "10.0.0.11:26379,10.0.0.12:26379")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Redis.Addrs) != 2 || cfg.Redis.Addrs[1] != "10.0.0.12:26379" {
		t.Fatalf("addrs = %v, want env value split into 2 addresses", cfg.Redis.Addrs)
	}
}

func TestLoadRejectsUnknownRedisMode(t *testing.T) {
	path := writeConfig(t, `
redis:
  mode: cluster
`)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "Mode") {
		t.Fatalf("Load error = %v, want mode validation error", err)
	}
}

func TestLoadMapsRedisTLSIntoOptions(t *testing.T) {
	path := writeConfig(t, `
redis:
  tls_enabled: true
  tls_ca_file: /etc/ssl/certs/redis-ca.pem
  tls_server_name: redis.internal
`)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	opts := cfg.Redis.Options()
	if !opts.TLS.Enabled {
		t.Fatal("Options().TLS.Enabled = false, want true")
	}
	if opts.TLS.CAFile != "/etc/ssl/certs/redis-ca.pem" {
		t.Fatalf("Options().TLS.CAFile = %q", opts.TLS.CAFile)
	}
	if opts.TLS.ServerName != "redis.internal" {
		t.Fatalf("Options().TLS.ServerName = %q", opts.TLS.ServerName)
	}
}

func TestLoadRedisTLSFromEnvironment(t *testing.T) {
	path := writeConfig(t, `
redis: {}
`)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))
	t.Setenv("REDIS_TLS_ENABLED", "true")
	t.Setenv("REDIS_TLS_INSECURE_SKIP_VERIFY", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	opts := cfg.Redis.Options().TLS
	if !opts.Enabled || !opts.InsecureSkipVerify {
		t.Fatalf("Options().TLS = %+v, want enabled with skip-verify from env", opts)
	}
}

func TestLoadRejectsRedisTLSOptionsWithoutEnabled(t *testing.T) {
	// 证书配了却没开开关，等于静默走明文：这里必须报错而不是忽略。
	path := writeConfig(t, `
redis:
  tls_ca_file: /etc/ssl/certs/redis-ca.pem
`)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "REDIS_TLS_ENABLED") {
		t.Fatalf("Load error = %v, want TLS-enabled requirement", err)
	}
}

func TestLoadRejectsRedisTLSClientCertWithoutKey(t *testing.T) {
	path := writeConfig(t, `
redis:
  tls_enabled: true
  tls_cert_file: /etc/ssl/certs/redis-client.pem
`)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "must be set together") {
		t.Fatalf("Load error = %v, want cert/key pairing error", err)
	}
}
