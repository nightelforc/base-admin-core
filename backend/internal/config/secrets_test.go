package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSecretFile 在独立临时目录里放一个密钥文件，模拟 Secret 卷挂载。
func writeSecretFile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}
	return path
}

func TestSecretFileEnvNamesFollowSuffix(t *testing.T) {
	// 变量名在 secretTargets 里逐条手写（这样全仓能直接搜到 DB_PASSWORD_FILE），
	// 这里保证它们没有漂离 X → X_FILE 的约定。
	for _, target := range secretTargets(&Config{}) {
		if target.env == "" || target.fileEnv == "" {
			t.Errorf("target %+v: env and fileEnv must both be set", target)
			continue
		}
		if want := target.env + secretFileSuffix; target.fileEnv != want {
			t.Errorf("fileEnv = %q, want %q", target.fileEnv, want)
		}
	}
}

func TestLoadReadsSecretsFromFiles(t *testing.T) {
	// 生产环境下占位符会被拒绝，正好用来证明文件内容确实填进了字段，
	// 并且和直接设置环境变量走的是同一套校验。
	path := writeConfig(t, `
app:
  env: production
`)
	jwtSecret := strings.Repeat("j", 40)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET_FILE", writeSecretFile(t, "jwt_secret", jwtSecret+"\n"))
	t.Setenv("DB_PASSWORD_FILE", writeSecretFile(t, "db_password", "db-pass-1\n"))
	t.Setenv("REDIS_PASSWORD_FILE", writeSecretFile(t, "redis_password", "redis-pass-1\r\n"))
	t.Setenv("REDIS_SENTINEL_PASSWORD_FILE", writeSecretFile(t, "sentinel_password", "sentinel-pass-1"))
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD_FILE", writeSecretFile(t, "admin_password", "admin-pass-1\n"))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.JWT.Secret != jwtSecret {
		t.Fatalf("JWT.Secret = %q, want file content without trailing newline", cfg.JWT.Secret)
	}
	if cfg.Database.Password != "db-pass-1" {
		t.Fatalf("Database.Password = %q, want db-pass-1", cfg.Database.Password)
	}
	if cfg.Redis.Password != "redis-pass-1" {
		t.Fatalf("Redis.Password = %q, want redis-pass-1 (CRLF stripped)", cfg.Redis.Password)
	}
	if cfg.Redis.SentinelPassword != "sentinel-pass-1" {
		t.Fatalf("Redis.SentinelPassword = %q, want sentinel-pass-1", cfg.Redis.SentinelPassword)
	}
	if cfg.Bootstrap.AdminPassword != "admin-pass-1" {
		t.Fatalf("Bootstrap.AdminPassword = %q, want admin-pass-1", cfg.Bootstrap.AdminPassword)
	}

	// 文件只是输入的另一种形态：消费方拿到的仍然是普通配置值。
	if dsn := cfg.Database.AppDSN(); !strings.Contains(dsn, "db-pass-1") {
		t.Fatalf("AppDSN() = %q, want it to carry the password from file", dsn)
	}
	if cfg.Redis.Options().SentinelPassword != "sentinel-pass-1" {
		t.Fatalf("Redis.Options().SentinelPassword did not receive the file value")
	}
}

func TestLoadRejectsBothSecretSources(t *testing.T) {
	path := writeConfig(t, "")
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))
	t.Setenv("JWT_SECRET_FILE", writeSecretFile(t, "jwt_secret", strings.Repeat("f", 40)))

	_, err := Load()
	if err == nil {
		t.Fatal("Load succeeded, want conflict between JWT_SECRET and JWT_SECRET_FILE")
	}
	for _, want := range []string{"JWT_SECRET", "JWT_SECRET_FILE", "both set"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Load error = %v, want it to mention %s", err, want)
		}
	}
}

func TestLoadPrefersSecretFileWhenEnvIsEmpty(t *testing.T) {
	// 流水线变量缺失时会注入空串；此时不应挡住文件注入。
	path := writeConfig(t, "")
	secret := strings.Repeat("f", 40)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", "")
	t.Setenv("JWT_SECRET_FILE", writeSecretFile(t, "jwt_secret", secret))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.JWT.Secret != secret {
		t.Fatalf("JWT.Secret = %q, want file content", cfg.JWT.Secret)
	}
}

func TestLoadRejectsMissingSecretFile(t *testing.T) {
	path := writeConfig(t, "")
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET_FILE", filepath.Join(t.TempDir(), "does-not-exist"))

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "JWT_SECRET_FILE") {
		t.Fatalf("Load error = %v, want JWT_SECRET_FILE read failure", err)
	}
}

func TestLoadRejectsEmptySecretFile(t *testing.T) {
	// 只有换行的文件等价于空文件：绝不能退化成空密码。
	for _, content := range []string{"", "\n", " \r\n"} {
		t.Run("content="+strings.ReplaceAll(content, "\n", `\n`), func(t *testing.T) {
			path := writeConfig(t, "")
			t.Setenv(ConfigFileEnv, path)
			t.Setenv("JWT_SECRET_FILE", writeSecretFile(t, "jwt_secret", content))

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), "is empty") {
				t.Fatalf("Load error = %v, want empty secret file rejection", err)
			}
		})
	}
}

func TestLoadRejectsBlankSecretFilePath(t *testing.T) {
	path := writeConfig(t, "")
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET_FILE", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "JWT_SECRET_FILE is set but empty") {
		t.Fatalf("Load error = %v, want blank JWT_SECRET_FILE rejection", err)
	}
}

func TestLoadAggregatesSecretFileProblems(t *testing.T) {
	path := writeConfig(t, "")
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET_FILE", filepath.Join(t.TempDir(), "missing-jwt"))
	t.Setenv("DB_PASSWORD_FILE", filepath.Join(t.TempDir(), "missing-db"))

	_, err := Load()
	if err == nil {
		t.Fatal("Load succeeded, want aggregated secret file errors")
	}
	for _, want := range []string{"JWT_SECRET_FILE", "DB_PASSWORD_FILE"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Load error = %v, want it to mention %s", err, want)
		}
	}
}

func TestSecretFileContentStillValidated(t *testing.T) {
	t.Run("short jwt secret", func(t *testing.T) {
		path := writeConfig(t, "")
		t.Setenv(ConfigFileEnv, path)
		t.Setenv("JWT_SECRET_FILE", writeSecretFile(t, "jwt_secret", "short"))

		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "JWT") {
			t.Fatalf("Load error = %v, want JWT length validation", err)
		}
	})

	t.Run("placeholder db password in production", func(t *testing.T) {
		path := writeConfig(t, `
app:
  env: production
`)
		t.Setenv(ConfigFileEnv, path)
		t.Setenv("JWT_SECRET", strings.Repeat("a", 32))
		t.Setenv("DB_PASSWORD_FILE", writeSecretFile(t, "db_password", "change-me\n"))

		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "DB_PASSWORD") {
			t.Fatalf("Load error = %v, want DB_PASSWORD placeholder rejection", err)
		}
	})
}
