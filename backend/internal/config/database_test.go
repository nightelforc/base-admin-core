package config

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestDatabaseAppDSNAddsStatementTimeout(t *testing.T) {
	db := Database{
		Host:             "127.0.0.1",
		Port:             5432,
		User:             "app",
		Password:         "secret",
		Name:             "base_admin",
		SSLMode:          "disable",
		TimeZone:         "Asia/Shanghai",
		StatementTimeout: 15 * time.Second,
		sessionLabel:     "base-admin",
	}

	// 迁移用 MigrateDSN()：大表的 DDL 可能合法地跑过应用查询的时限。
	plain := db.MigrateDSN()
	if strings.Contains(plain, "statement_timeout") {
		t.Fatalf("MigrateDSN() must not carry statement_timeout: %s", plain)
	}

	app := db.AppDSN()
	if !strings.Contains(app, "statement_timeout=15000") {
		t.Fatalf("AppDSN() must carry statement_timeout in milliseconds: %s", app)
	}
	// 斜杠不能被转义，否则 PostgreSQL 会把它当成未知时区。
	if !strings.Contains(app, "TimeZone=Asia/Shanghai") {
		t.Fatalf("time zone must stay unescaped: %s", app)
	}
}

func TestDatabaseAppDSNOmitsDisabledStatementTimeout(t *testing.T) {
	db := Database{Host: "127.0.0.1", Port: 5432, User: "app", Name: "base_admin", SSLMode: "disable"}
	if strings.Contains(db.AppDSN(), "statement_timeout") {
		t.Fatalf("zero StatementTimeout must not be sent: %s", db.AppDSN())
	}
}

func TestValidateTimeoutsRejectsStatementTimeoutLongerThanRequest(t *testing.T) {
	cfg := &Config{
		HTTP:     HTTP{RequestTimeout: 10 * time.Second},
		Database: Database{StatementTimeout: 10 * time.Second},
	}

	err := cfg.validateTimeouts().err()
	if err == nil {
		t.Fatal("expected a cross-timeout validation error")
	}
	if !strings.Contains(err.Error(), "DB_STATEMENT_TIMEOUT") {
		t.Fatalf("unexpected message: %v", err)
	}
}

func TestValidateTimeoutsAcceptsShorterStatementTimeout(t *testing.T) {
	cfg := &Config{
		HTTP:     HTTP{RequestTimeout: 30 * time.Second},
		Database: Database{StatementTimeout: 5 * time.Second},
	}
	if err := cfg.validateTimeouts().err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDatabaseDSNsCarrySessionLabelAndConnectTimeout(t *testing.T) {
	db := Database{
		Host:           "127.0.0.1",
		Port:           5432,
		User:           "app",
		Password:       "secret",
		Name:           "base_admin",
		SSLMode:        "disable",
		TimeZone:       "Asia/Shanghai",
		ConnectTimeout: 5 * time.Second,
		sessionLabel:   "base-admin",
	}

	app := db.AppDSN()
	for _, want := range []string{"application_name=base-admin", "connect_timeout=5"} {
		if !strings.Contains(app, want) {
			t.Errorf("AppDSN() = %q, want it to contain %q", app, want)
		}
	}
	if strings.Contains(app, "-migrate") {
		t.Errorf("AppDSN() must not carry the migrate suffix: %s", app)
	}

	migrate := db.MigrateDSN()
	for _, want := range []string{"application_name=base-admin-migrate", "connect_timeout=5"} {
		if !strings.Contains(migrate, want) {
			t.Errorf("MigrateDSN() = %q, want it to contain %q", migrate, want)
		}
	}
}

func TestDatabaseDSNOmitsUnsetSessionLabel(t *testing.T) {
	// 零值的 Database 没有标签（只有 Load 会写入），此时不发 application_name，
	// 而不是发一个空串。
	db := Database{Host: "127.0.0.1", Port: 5432, User: "app", Name: "base_admin", SSLMode: "disable"}
	if strings.Contains(db.AppDSN(), "application_name") {
		t.Fatalf("empty session label must not be sent: %s", db.AppDSN())
	}
	if strings.Contains(db.MigrateDSN(), "application_name") {
		t.Fatalf("empty session label must not be sent: %s", db.MigrateDSN())
	}
}

func TestDatabaseDSNEscapesSessionLabel(t *testing.T) {
	// 标签里可能出现空格和分隔符；不编码会让查询串提前结束或串到下一个参数。
	db := Database{
		Host: "127.0.0.1", Port: 5432, User: "app", Name: "base_admin", SSLMode: "disable",
		sessionLabel: "base admin&x=1",
	}
	if want := "application_name=base%20admin%26x%3D1"; !strings.Contains(db.AppDSN(), want) {
		t.Fatalf("AppDSN() = %q, want it to contain %q", db.AppDSN(), want)
	}
}

func TestDatabaseValidateRejectsBadConnectTimeout(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		want    string
	}{
		{name: "negative", timeout: -time.Second, want: "must not be negative"},
		{name: "sub-second", timeout: 500 * time.Millisecond, want: "whole number of seconds"},
		{name: "not whole", timeout: 1500 * time.Millisecond, want: "whole number of seconds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := Database{ConnectTimeout: tc.timeout}
			err := db.validate().err()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validate() = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestAppValidateRejectsOversizedName(t *testing.T) {
	// app.name 会原样成为 PostgreSQL 会话标签，迁移连接还要再追加 -migrate，
	// 超长会被服务端静默截断，启动时就该拒绝。
	limit := sessionLabelMaxBytes - len(migrateSessionSuffix)
	app := App{Name: strings.Repeat("a", limit+1), Timezone: "UTC"}
	err := app.validate().err()
	if err == nil || !strings.Contains(err.Error(), "APP_NAME") {
		t.Fatalf("validate() = %v, want it to reject an oversized app name", err)
	}
}

func TestDatabaseDSNIsParseableByPgx(t *testing.T) {
	// 这条测试盯的是“我们的拼串”与“驱动的解析”之间的契约：单位写错（5s vs 5）、
	// 参数名拼错、时区被转义，都只有真正交给 pgx 才能发现。
	db := Database{
		Host: "127.0.0.1", Port: 5432, User: "app", Password: "secret", Name: "base_admin",
		SSLMode: "disable", TimeZone: "Asia/Shanghai",
		ConnectTimeout: 5 * time.Second, StatementTimeout: 15 * time.Second,
		sessionLabel: "base-admin",
	}

	parsed, err := pgconn.ParseConfig(db.AppDSN())
	if err != nil {
		t.Fatalf("pgx cannot parse the DSN: %v", err)
	}
	if got := parsed.RuntimeParams["application_name"]; got != "base-admin" {
		t.Errorf("application_name = %q, want %q", got, "base-admin")
	}
	if got := parsed.RuntimeParams["statement_timeout"]; got != "15000" {
		t.Errorf("statement_timeout = %q, want milliseconds", got)
	}
	if got := parsed.RuntimeParams["TimeZone"]; got != "Asia/Shanghai" {
		t.Errorf("TimeZone = %q, want the raw IANA name", got)
	}
	if parsed.ConnectTimeout != 5*time.Second {
		t.Errorf("ConnectTimeout = %v, want 5s", parsed.ConnectTimeout)
	}
}

func TestValidateTimeoutsRejectsConnectTimeoutLongerThanRequest(t *testing.T) {
	cfg := &Config{
		HTTP:     HTTP{RequestTimeout: 10 * time.Second},
		Database: Database{ConnectTimeout: 10 * time.Second},
	}
	err := cfg.validateTimeouts().err()
	if err == nil || !strings.Contains(err.Error(), "DB_CONNECT_TIMEOUT") {
		t.Fatalf("validateTimeouts() = %v, want a DB_CONNECT_TIMEOUT error", err)
	}
}

func TestLoadUsesAppNameAsSessionLabel(t *testing.T) {
	// 会话标签不单独配置：配置里只有 app.name，日志和 PostgreSQL 用的是同一个值。
	path := writeConfig(t, `
app:
  name: base-admin
  env: development
`)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", strings.Repeat("s", 32))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	app := cfg.Database.AppDSN()
	if !strings.Contains(app, "application_name=base-admin") {
		t.Fatalf("AppDSN() = %q, want app.name as session label", app)
	}
	migrate := cfg.Database.MigrateDSN()
	if !strings.Contains(migrate, "application_name=base-admin-migrate") {
		t.Fatalf("MigrateDSN() = %q, want the -migrate suffix", migrate)
	}
}

func TestLoadTrimsAppNameForSessionLabel(t *testing.T) {
	path := writeConfig(t, `
app:
  name: "  base-admin  "
  env: development
`)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", strings.Repeat("s", 32))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if app := cfg.Database.AppDSN(); !strings.Contains(app, "application_name=base-admin") {
		t.Fatalf("AppDSN() = %q, want a trimmed label (no %%20 padding)", app)
	}
}

func TestAppValidateRejectsBlankName(t *testing.T) {
	// 名字同时是日志身份和会话标签，空白名会让两处都无法辨识。
	app := App{Name: "   ", Timezone: "UTC"}
	err := app.validate().err()
	if err == nil || !strings.Contains(err.Error(), "APP_NAME") {
		t.Fatalf("validate() = %v, want it to reject a blank app name", err)
	}
}
