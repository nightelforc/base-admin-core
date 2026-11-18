package config

import (
	"strings"
	"testing"
	"time"
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
	}

	// 迁移用 DSN()：大表的 DDL 可能合法地跑过应用查询的时限。
	plain := db.DSN()
	if strings.Contains(plain, "statement_timeout") {
		t.Fatalf("DSN() must not carry statement_timeout: %s", plain)
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
