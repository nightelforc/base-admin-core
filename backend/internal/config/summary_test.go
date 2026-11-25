package config

import (
	"fmt"
	"strings"
	"testing"
)

// loadForSummary 以仓库 config.yaml 为基线加载一次配置，只覆盖测试关心的项。
func loadForSummary(t *testing.T, overlay string) *Config {
	t.Helper()

	t.Setenv("JWT_SECRET", strings.Repeat("s", 32))
	t.Setenv(ConfigFileEnv, writeConfig(t, overlay))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

// TestSummaryCoversEveryComponent 盯住"补全"这件事本身：新增组件或关键开关时，
// 如果忘了往 Summary 里加，这里会先红。
func TestSummaryCoversEveryComponent(t *testing.T) {
	got := loadForSummary(t, "").Summary()

	for _, key := range []string{
		"config_file",
		"app.name", "app.env", "app.timezone", "app.debug",
		"log.level", "log.format", "log.add_source",
		"monitor.enabled",
		"http.addr", "http.read_header_timeout", "http.request_timeout",
		"http.idle_timeout", "http.shutdown_timeout", "http.max_body_bytes",
		"http.trusted_proxies",
		"database.host", "database.port", "database.name", "database.user",
		"database.sslmode", "database.table_prefix", "database.max_open_conns",
		"database.max_idle_conns", "database.statement_timeout",
		"database.connect_timeout", "database.ping_timeout", "database.log_level",
		"redis.mode", "redis.addrs", "redis.db", "redis.key_prefix",
		"redis.pool_size", "redis.pool_timeout", "redis.max_retries", "redis.tls_enabled",
		"jwt.issuer", "jwt.audience", "jwt.access_ttl", "jwt.refresh_ttl",
		"security.bcrypt_cost", "security.password_min_length",
		"security.login_max_attempts", "security.login_window",
		"security.operation_log_enabled", "security.operation_log_body_limit",
		"cors.allowed_origins", "cors.allow_credentials",
		"bootstrap.admin_username", "bootstrap.password_generated",
		"secrets.from_files",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("Summary() is missing %q", key)
		}
	}
}

// TestSummaryRendersDisabledAndEmptyValues 检查"关闭"和"没配"不会被渲染成
// 看起来像真实值的 0s / 空串——那正是运维看日志时最容易误判的两种形态。
func TestSummaryRendersDisabledAndEmptyValues(t *testing.T) {
	got := loadForSummary(t, "").Summary()

	if v, ok := got["http.request_timeout"].(string); !ok || v == "" {
		t.Errorf("http.request_timeout = %v, want a duration string", got["http.request_timeout"])
	}
	if v := got["http.write_timeout"]; v != "off" {
		t.Errorf("http.write_timeout = %v, want \"off\" for a disabled timeout", v)
	}
	if v := got["http.trusted_proxies"]; v != "none" {
		t.Errorf("http.trusted_proxies = %v, want \"none\"", v)
	}
	if v := got["secrets.from_files"]; v != "none" {
		t.Errorf("secrets.from_files = %v, want \"none\"", v)
	}
}

// TestSummaryOmitsSecrets 是防止启动日志泄漏凭据的回归测试：既检查值不出现，
// 也检查键名里不会长出 password / secret 这类字段。
func TestSummaryOmitsSecrets(t *testing.T) {
	const (
		jwtSecret  = "jwt-secret-value-should-never-be-logged"
		dbPassword = "db-password-value-should-never-be-logged"
		redisPass  = "redis-password-value-should-never-be-logged"
		adminPass  = "admin-password-value-should-never-be-logged"
	)

	t.Setenv("JWT_SECRET", jwtSecret)
	t.Setenv("DB_PASSWORD", dbPassword)
	t.Setenv("REDIS_PASSWORD", redisPass)
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", adminPass)
	t.Setenv(ConfigFileEnv, writeConfig(t, ""))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	summary := cfg.Summary()
	dump := fmt.Sprint(summary)
	for _, secret := range []string{jwtSecret, dbPassword, redisPass, adminPass} {
		if strings.Contains(dump, secret) {
			t.Errorf("Summary() leaks a secret value in %q", dump)
		}
	}

	for key := range summary {
		// 只看最后一段：secrets.from_files 记录的是变量名，
		// security.password_min_length 是策略值，都不是凭据本身。
		segments := strings.Split(key, ".")
		switch leaf := segments[len(segments)-1]; leaf {
		case "password", "passwd", "secret", "token", "credential":
			t.Errorf("Summary() key %q looks like a credential field", key)
		}
	}
}

// TestSummaryReportsSecretFiles 证明"哪些项走了挂载文件"是可见的：排查
// "改了 Secret 却没生效"这类问题时，先要能确认进程读的是文件还是环境变量。
func TestSummaryReportsSecretFiles(t *testing.T) {
	t.Setenv(ConfigFileEnv, writeConfig(t, ""))
	t.Setenv("JWT_SECRET_FILE", writeSecretFile(t, "jwt_secret", strings.Repeat("k", 32)))
	t.Setenv("DB_PASSWORD_FILE", writeSecretFile(t, "db_password", "db-pass-1"))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := cfg.Summary()["secrets.from_files"]; got != "DB_PASSWORD,JWT_SECRET" {
		t.Fatalf("secrets.from_files = %v, want sorted env names", got)
	}
}
